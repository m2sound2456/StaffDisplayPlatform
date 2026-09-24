package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// connectTestDatabase dials the dedicated test database. The test is skipped
// unless TEST_DATABASE_INTEGRATION=1 so the default `go test ./...` run never
// needs a live PostgreSQL server.
func connectTestDatabase(t *testing.T) *Database {
	t.Helper()

	if os.Getenv("TEST_DATABASE_INTEGRATION") != "1" {
		t.Skip("set TEST_DATABASE_INTEGRATION=1 (plus DATABASE_PASSWORD) to run database integration tests")
	}

	cfg := config.Default()
	if name := strings.TrimSpace(os.Getenv("TEST_DATABASE_NAME")); name != "" {
		cfg.Database.Name = name
	} else {
		cfg.Database.Name = "staffdisplay_test"
	}
	if password := os.Getenv("DATABASE_PASSWORD"); password != "" {
		cfg.Database.Password = password
	}
	if host := strings.TrimSpace(os.Getenv("TEST_DATABASE_HOST")); host != "" {
		cfg.Database.Host = host
	}

	db, err := Open(OptionsFromConfig(cfg.Database))
	if err != nil {
		t.Skipf("test database unreachable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// resetSchema removes every object the migration test creates. Children are
// dropped before their parents and with CASCADE so the shared set_updated_at()
// helper can be removed even while the FG2 tables still have triggers on it.
func resetSchema(t *testing.T, db *Database) {
	t.Helper()

	statements := []string{
		"DROP TABLE IF EXISTS audit_logs CASCADE",
		"DROP TABLE IF EXISTS stores CASCADE",
		"DROP TABLE IF EXISTS tenants CASCADE",
		"DROP TABLE IF EXISTS " + MigrationTable + " CASCADE",
		"DROP FUNCTION IF EXISTS set_updated_at() CASCADE",
	}
	for _, statement := range statements {
		if err := db.Gorm().Exec(statement).Error; err != nil {
			t.Fatalf("reset schema (%s): %v", statement, err)
		}
	}
}

// lockSchemaForTest takes the shared schema lock (the same advisory lock
// `cmd/migrate up` uses) and releases it when the test ends. Without it this
// suite could drop the FG2 tables while another package's integration test is
// mid migration.
func lockSchemaForTest(t *testing.T, db *Database) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	release, err := db.LockSchema(ctx)
	if err != nil {
		t.Fatalf("lock schema: %v", err)
	}
	t.Cleanup(release)
}

func TestMigrateAppliesEmbeddedMigrations(t *testing.T) {
	db := connectTestDatabase(t)
	lockSchemaForTest(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resetSchema(t, db)
	t.Cleanup(func() { resetSchema(t, db) })

	loaded, err := LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	statuses, err := db.Status(ctx, loaded)
	if err != nil {
		t.Fatalf("status before migrate: %v", err)
	}
	for _, status := range statuses {
		if status.Applied {
			t.Fatalf("migration %s reported as applied before the first run", status.Filename)
		}
	}

	applied, err := db.Migrate(ctx, loaded)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) != len(loaded) {
		t.Fatalf("applied %d migrations, want %d", len(applied), len(loaded))
	}

	// Running again must be a no-op (idempotent, bookkeeping respected).
	again, err := db.Migrate(ctx, loaded)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second migrate applied %d migrations, want 0", len(again))
	}

	version, err := db.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if version != loaded[len(loaded)-1].Version {
		t.Fatalf("current version = %d, want %d", version, loaded[len(loaded)-1].Version)
	}

	// The foundation migration installed the shared trigger helper.
	var helperCount int64
	if err := db.Gorm().Raw("SELECT count(*) FROM pg_proc WHERE proname = 'set_updated_at'").Scan(&helperCount).Error; err != nil {
		t.Fatalf("query pg_proc: %v", err)
	}
	if helperCount != 1 {
		t.Fatalf("set_updated_at() count = %d, want 1", helperCount)
	}
}

func TestMigrateDetectsChecksumDrift(t *testing.T) {
	db := connectTestDatabase(t)
	lockSchemaForTest(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resetSchema(t, db)
	t.Cleanup(func() { resetSchema(t, db) })

	loaded, err := LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if _, err := db.Migrate(ctx, loaded); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	// Simulate "someone edited an applied migration file": the file content (and
	// therefore its checksum) differs from the recorded bookkeeping row.
	tamperedSQL := loaded[0].SQL + "\n-- edited after being applied\n"
	sum := sha256.Sum256([]byte(strings.TrimSpace(tamperedSQL)))

	tampered := make([]Migration, len(loaded))
	copy(tampered, loaded)
	tampered[0].SQL = tamperedSQL
	tampered[0].Checksum = hex.EncodeToString(sum[:])

	if _, err := db.Migrate(ctx, tampered); err == nil {
		t.Fatal("expected a checksum mismatch error for an edited applied migration")
	} else if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPingLatencyReportsConnectivity(t *testing.T) {
	db := connectTestDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	latency, err := db.PingLatency(ctx)
	if err != nil {
		t.Fatalf("PingLatency() error = %v", err)
	}
	if latency < 0 {
		t.Fatalf("latency = %s, want >= 0", latency)
	}
}

func TestOpenRejectsBadCredentials(t *testing.T) {
	if os.Getenv("TEST_DATABASE_INTEGRATION") != "1" {
		t.Skip("set TEST_DATABASE_INTEGRATION=1 to run database integration tests")
	}

	opts := Options{
		Host: "127.0.0.1", Port: 5432, User: "postgres",
		Password: "definitely-not-the-password", Name: "staffdisplay_test",
		SSLMode: "disable", Timezone: "Asia/Bangkok",
	}
	db, err := Open(opts)
	if err == nil {
		_ = db.Close()
		t.Fatal("expected an error for invalid credentials")
	}
	if strings.Contains(err.Error(), "definitely-not-the-password") {
		t.Fatalf("error message leaks the password: %v", err)
	}
	if !strings.Contains(err.Error(), "password=***") {
		t.Fatalf("error message should contain the redacted DSN: %v", err)
	}
}
