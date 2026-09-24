package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// MigrationTable is the bookkeeping table for applied migrations.
const MigrationTable = "schema_migrations"

// migrationFilePattern enforces NNNN_snake_case_description.sql.
var migrationFilePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9][a-z0-9_]*)\.sql$`)

// Migration is one versioned SQL file.
type Migration struct {
	Version  int
	Name     string
	Filename string
	SQL      string
	Checksum string
}

// AppliedMigration mirrors a row of schema_migrations.
type AppliedMigration struct {
	Version   int       `gorm:"column:version;primaryKey"`
	Name      string    `gorm:"column:name;not null"`
	Checksum  string    `gorm:"column:checksum;not null"`
	AppliedAt time.Time `gorm:"column:applied_at;not null"`
}

// TableName implements gorm.Tabler.
func (AppliedMigration) TableName() string { return MigrationTable }

// MigrationStatus merges a migration file with its bookkeeping row.
type MigrationStatus struct {
	Migration
	Applied   bool
	AppliedAt *time.Time
}

// LoadMigrations reads *.sql files from fsys ordered by version.
//
// dir selects a sub-directory inside fsys ("." or "" reads the root). Invalid
// names, duplicate versions and empty files are rejected so a broken migration
// set can never reach the database.
func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	if fsys == nil {
		return nil, errors.New("migrations filesystem is nil")
	}

	pattern := "*.sql"
	if trimmed := strings.TrimSpace(dir); trimmed != "" && trimmed != "." {
		pattern = path.Join(trimmed, "*.sql")
	}

	names, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, fmt.Errorf("list migrations (%s): %w", pattern, err)
	}
	sort.Strings(names)

	migrations := make([]Migration, 0, len(names))
	seen := make(map[int]string, len(names))
	for _, name := range names {
		filename := path.Base(name)
		match := migrationFilePattern.FindStringSubmatch(filename)
		if match == nil {
			return nil, fmt.Errorf("migration %q must match NNNN_snake_case_description.sql", filename)
		}
		version, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			return nil, fmt.Errorf("migration %q has an invalid version: %w", filename, convErr)
		}
		if other, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf("duplicate migration version %04d (%s and %s)", version, other, filename)
		}
		seen[version] = filename

		content, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", filename, readErr)
		}
		sqlText := strings.TrimSpace(string(content))
		if sqlText == "" {
			return nil, fmt.Errorf("migration %s is empty", filename)
		}
		sum := sha256.Sum256([]byte(sqlText))

		migrations = append(migrations, Migration{
			Version:  version,
			Name:     match[2],
			Filename: filename,
			SQL:      sqlText,
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// EnsureMigrationTable creates schema_migrations when it does not exist.
func (d *Database) EnsureMigrationTable(ctx context.Context) error {
	if d == nil || d.gormDB == nil {
		return errors.New("database is not initialised")
	}
	if err := d.gormDB.WithContext(ctx).AutoMigrate(&AppliedMigration{}); err != nil {
		return fmt.Errorf("create %s: %w", MigrationTable, err)
	}
	return nil
}

// Status reports every known migration together with its bookkeeping state.
func (d *Database) Status(ctx context.Context, migrations []Migration) ([]MigrationStatus, error) {
	if err := d.EnsureMigrationTable(ctx); err != nil {
		return nil, err
	}
	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make([]MigrationStatus, 0, len(migrations))
	for _, migration := range migrations {
		status := MigrationStatus{Migration: migration}
		if row, ok := applied[migration.Version]; ok {
			appliedAt := row.AppliedAt
			status.Applied = true
			status.AppliedAt = &appliedAt
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Migrate applies every pending migration in ascending version order and returns
// the migrations that were applied (empty when the schema is already current).
//
// A migration whose recorded checksum no longer matches its file aborts the run:
// silently editing applied history would corrupt shared production data.
func (d *Database) Migrate(ctx context.Context, migrations []Migration) ([]Migration, error) {
	if err := d.EnsureMigrationTable(ctx); err != nil {
		return nil, err
	}
	appliedRows, err := d.appliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	applied := make([]Migration, 0, len(migrations))
	for _, migration := range migrations {
		if row, ok := appliedRows[migration.Version]; ok {
			if row.Checksum != migration.Checksum {
				return applied, fmt.Errorf(
					"migration %s changed after it was applied (checksum mismatch): create a new migration instead of editing history",
					migration.Filename,
				)
			}
			continue
		}
		if err := d.applyMigration(ctx, migration); err != nil {
			return applied, err
		}
		applied = append(applied, migration)
	}
	return applied, nil
}

// CurrentVersion returns the highest applied migration version (0 when none).
func (d *Database) CurrentVersion(ctx context.Context) (int, error) {
	if err := d.EnsureMigrationTable(ctx); err != nil {
		return 0, err
	}
	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return 0, err
	}
	highest := 0
	for version := range applied {
		if version > highest {
			highest = version
		}
	}
	return highest, nil
}

func (d *Database) appliedMigrations(ctx context.Context) (map[int]AppliedMigration, error) {
	var rows []AppliedMigration
	if err := d.gormDB.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read %s: %w", MigrationTable, err)
	}
	applied := make(map[int]AppliedMigration, len(rows))
	for _, row := range rows {
		applied[row.Version] = row
	}
	return applied, nil
}

// applyMigration runs one migration and its bookkeeping row in a single
// transaction so a failure leaves no half-applied version behind.
func (d *Database) applyMigration(ctx context.Context, migration Migration) error {
	return d.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(migration.SQL).Error; err != nil {
			return fmt.Errorf("apply %s: %w", migration.Filename, err)
		}
		record := AppliedMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			Checksum:  migration.Checksum,
			AppliedAt: time.Now().UTC(),
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("record %s: %w", migration.Filename, err)
		}
		return nil
	})
}
