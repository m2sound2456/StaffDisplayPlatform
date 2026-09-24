// Package database owns the PostgreSQL connection (GORM) and the versioned SQL
// migration runner.
//
// One database serves every tenant (BLUEPRINT §3): tenant isolation is enforced
// by store/tenant scoped queries, never by separate databases.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
)

// pingTimeout bounds a single connectivity check (health probes, startup).
const pingTimeout = 5 * time.Second

// Database wraps the GORM handle and the underlying *sql.DB pool.
type Database struct {
	gormDB *gorm.DB
	sqlDB  *sql.DB
	opts   Options
}

// New opens a connection using the application configuration.
func New(cfg config.DatabaseConfig, debug bool) (*Database, error) {
	return Open(OptionsFromConfig(cfg).WithDebug(debug))
}

// Open connects to PostgreSQL, configures the pool and verifies connectivity.
func Open(opts Options) (*Database, error) {
	logMode := gormlogger.Warn
	if opts.Debug {
		logMode = gormlogger.Info
	}

	// PreferSimpleProtocol lets the migration runner execute multi-statement SQL
	// files (dollar-quoted function bodies, several statements per file) which the
	// extended protocol rejects. It is the pgx-recommended mode for DDL scripts.
	gormDB, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{DSN: opts.DSN(), PreferSimpleProtocol: true}),
		&gorm.Config{
			Logger:                 gormlogger.Default.LogMode(logMode),
			SkipDefaultTransaction: true,
			NowFunc:                func() time.Time { return time.Now().UTC() },
		},
	)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", opts.RedactedDSN(), err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("obtain sql pool: %w", err)
	}

	if opts.MaxOpenConnections > 0 {
		sqlDB.SetMaxOpenConns(opts.MaxOpenConnections)
	}
	if opts.MaxIdleConnections >= 0 {
		sqlDB.SetMaxIdleConns(opts.MaxIdleConnections)
	}
	if opts.ConnectionMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(opts.ConnectionMaxLifetime)
	}

	db := &Database{gormDB: gormDB, sqlDB: sqlDB, opts: opts}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if _, err := db.PingLatency(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database %s: %w", opts.RedactedDSN(), err)
	}

	return db, nil
}

// PingLatency verifies connectivity and returns the round-trip duration.
func (d *Database) PingLatency(ctx context.Context) (time.Duration, error) {
	if d == nil || d.sqlDB == nil {
		return 0, errors.New("database is not initialised")
	}
	start := time.Now()
	if err := d.sqlDB.PingContext(ctx); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

// Ping verifies connectivity.
func (d *Database) Ping(ctx context.Context) error {
	_, err := d.PingLatency(ctx)
	return err
}

// Gorm exposes the ORM handle used by repositories (FG2+).
func (d *Database) Gorm() *gorm.DB {
	if d == nil {
		return nil
	}
	return d.gormDB
}

// SQL exposes the underlying pool for health probes and administrative tooling.
func (d *Database) SQL() *sql.DB {
	if d == nil {
		return nil
	}
	return d.sqlDB
}

// Options returns the resolved connection options (password redacted on print).
func (d *Database) Options() Options {
	if d == nil {
		return Options{}
	}
	return d.opts
}

// Close releases the connection pool.
func (d *Database) Close() error {
	if d == nil || d.sqlDB == nil {
		return nil
	}
	if err := d.sqlDB.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}
