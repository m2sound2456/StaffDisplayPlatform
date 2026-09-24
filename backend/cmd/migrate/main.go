// Command migrate applies and inspects the SQL migrations of the shared
// PostgreSQL database.
//
// Usage:
//
//	go run ./cmd/migrate up                  # apply pending migrations
//	go run ./cmd/migrate status              # applied / pending overview
//	go run ./cmd/migrate version             # current schema version
//	go run ./cmd/migrate up --dir ./migrations
//
// Migrations are embedded in the binary (backend/migrations/embed.go); --dir
// exists for operators who prefer to ship raw SQL files.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

const migrationTimeout = 5 * time.Minute

const usage = `staffdisplay-migrate — SQL migration CLI

Usage:
  staffdisplay-migrate [command] [flags]

Commands:
  up        Apply every pending migration (default)
  status    Show applied and pending migrations with checksums
  version   Print the current schema version

Flags:
  --dir <path>   load NNNN_*.sql from this directory instead of the embedded set

Environment:
  APP_ENV, CONFIG_PATH, DATABASE_* (see backend/.env.example)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "staffdisplay-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "up"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}

	flags := flag.NewFlagSet("staffdisplay-migrate", flag.ContinueOnError)
	dir := flags.String("dir", "", "directory containing NNNN_*.sql files (default: embedded migrations)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	switch command {
	case "up":
		return runUp(*dir)
	case "status":
		return runStatus(*dir)
	case "version":
		return runVersion()
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", command)
	}
}

func runUp(dir string) error {
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	migrationList, err := loadMigrations(dir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	// One schema writer at a time: a second `migrate up` (or a test suite
	// resetting the schema) waits here instead of interleaving DDL.
	release, err := db.LockSchema(ctx)
	if err != nil {
		return err
	}
	defer release()

	fmt.Printf("target: %s\n", db.Options().RedactedDSN())
	applied, err := db.Migrate(ctx, migrationList)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Printf("no pending migrations — schema is up to date (%d known)\n", len(migrationList))
		return nil
	}
	for _, migration := range applied {
		fmt.Printf("applied  %04d_%s\n", migration.Version, migration.Name)
	}
	fmt.Printf("%d migration(s) applied\n", len(applied))
	return nil
}

func runStatus(dir string) error {
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	migrationList, err := loadMigrations(dir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	statuses, err := db.Status(ctx, migrationList)
	if err != nil {
		return err
	}

	fmt.Printf("target: %s\n\n", db.Options().RedactedDSN())
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "VERSION\tNAME\tSTATE\tAPPLIED AT\tCHECKSUM")
	pending := 0
	for _, status := range statuses {
		state := "pending"
		appliedAt := "-"
		if status.Applied {
			state = "applied"
			if status.AppliedAt != nil {
				appliedAt = status.AppliedAt.UTC().Format(time.RFC3339)
			}
		} else {
			pending++
		}
		_, _ = fmt.Fprintf(writer, "%04d\t%s\t%s\t%s\t%s\n",
			status.Version, status.Name, state, appliedAt, shortChecksum(status.Checksum))
	}
	_ = writer.Flush()

	fmt.Printf("\n%d applied, %d pending\n", len(statuses)-pending, pending)
	return nil
}

func runVersion() error {
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	version, err := db.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d\n", version)
	return nil
}

func openDatabase() (*database.Database, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	db, err := database.Open(database.OptionsFromConfig(cfg.Database))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func loadMigrations(dir string) ([]database.Migration, error) {
	if strings.TrimSpace(dir) == "" {
		return database.LoadMigrations(migrations.FS, ".")
	}
	return database.LoadMigrations(os.DirFS(dir), ".")
}

func shortChecksum(checksum string) string {
	if len(checksum) <= 12 {
		return checksum
	}
	return checksum[:12]
}
