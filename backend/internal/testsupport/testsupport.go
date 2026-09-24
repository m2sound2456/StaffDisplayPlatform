// Package testsupport provides the shared PostgreSQL setup for integration
// tests (docs/AI_RULES.md §6.4).
//
// It is deliberately small and is only imported from *_test.go files:
//
//   - every helper skips the calling test unless TEST_DATABASE_INTEGRATION=1,
//     so the default `go test ./...` run never needs a live database;
//   - MigrateSchema serialises schema resets across packages and processes with
//     a PostgreSQL advisory lock, so `go test ./...` can run the integration
//     packages in parallel without them resetting each other's schema.
package testsupport

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// IntegrationEnvVar gates every database integration test.
const IntegrationEnvVar = "TEST_DATABASE_INTEGRATION"

// schemaSetupTimeout bounds one reset + migrate cycle.
const schemaSetupTimeout = 90 * time.Second

// Open connects to the dedicated test database (staffdisplay_test by default).
//
// The test is skipped when integration tests are disabled, and also when the
// database is unreachable, so a contributor without PostgreSQL still gets a
// green `go test ./...` — the run reports the skips.
func Open(t *testing.T) *database.Database {
	t.Helper()

	if os.Getenv(IntegrationEnvVar) != "1" {
		t.Skipf("set %s=1 (plus DATABASE_PASSWORD) to run PostgreSQL integration tests", IntegrationEnvVar)
	}

	cfg := config.Default().Database
	cfg.Name = envOr("TEST_DATABASE_NAME", "staffdisplay_test")
	if password := os.Getenv("DATABASE_PASSWORD"); password != "" {
		cfg.Password = password
	}
	cfg.Host = envOr("TEST_DATABASE_HOST", cfg.Host)
	cfg.User = envOr("TEST_DATABASE_USER", cfg.User)
	if port := envInt("TEST_DATABASE_PORT"); port > 0 {
		cfg.Port = port
	}

	db, err := database.Open(database.OptionsFromConfig(cfg))
	if err != nil {
		t.Skipf("test database unreachable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// MigrateSchema drops every object this project creates and applies the full
// embedded migration set, so a test always starts from a known schema.
//
// The schema lock is held until the test finishes, which serialises the
// integration packages against each other.
func MigrateSchema(t *testing.T, db *database.Database) {
	t.Helper()

	lockSchema(t, db)

	ctx, cancel := context.WithTimeout(t.Context(), schemaSetupTimeout)
	defer cancel()

	Reset(t, db)

	loaded, err := database.LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	applied, err := db.Migrate(ctx, loaded)
	if err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}
	if len(applied) != len(loaded) {
		t.Fatalf("applied %d migrations, want %d (was the schema really reset?)", len(applied), len(loaded))
	}
}

// Reset drops the FG2 tables plus the FG1 helper function, children first.
func Reset(t *testing.T, db *database.Database) {
	t.Helper()

	if db == nil || db.Gorm() == nil {
		t.Fatal("testsupport.Reset needs an open database")
	}

	statements := []string{
		"DROP TABLE IF EXISTS audit_logs CASCADE",
		"DROP TABLE IF EXISTS stores CASCADE",
		"DROP TABLE IF EXISTS tenants CASCADE",
		"DROP TABLE IF EXISTS " + database.MigrationTable + " CASCADE",
		"DROP FUNCTION IF EXISTS set_updated_at() CASCADE",
	}
	ctx, cancel := context.WithTimeout(t.Context(), schemaSetupTimeout)
	defer cancel()

	for _, statement := range statements {
		if err := db.Gorm().WithContext(ctx).Exec(statement).Error; err != nil {
			t.Fatalf("testsupport reset: %s: %v", statement, err)
		}
	}
}

// NewTenant inserts a tenant with a unique slug and returns its id. Platform
// tenant CRUD is not part of FG2 (it lands with FG5), so tests create tenants
// directly.
func NewTenant(t *testing.T, db *database.Database, name string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	slug := "tenant-" + strings.ReplaceAll(id.String()[:8], "-", "")
	if err := db.Gorm().WithContext(t.Context()).
		Exec("INSERT INTO tenants (id, name, slug) VALUES (?, ?, ?)", id, name, slug).Error; err != nil {
		t.Fatalf("insert tenant %q: %v", name, err)
	}
	return id
}

// NewStore inserts a store row for a tenant and returns its id. Integration
// tests of the repository use the repository itself; this helper exists for
// schema level tests (constraints, cascades, triggers).
func NewStore(t *testing.T, db *database.Database, tenantID uuid.UUID, name, slug string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if err := db.Gorm().WithContext(t.Context()).
		Exec("INSERT INTO stores (id, tenant_id, name, slug) VALUES (?, ?, ?, ?)", id, tenantID, name, slug).Error; err != nil {
		t.Fatalf("insert store %q: %v", slug, err)
	}
	return id
}

// LockSchema acquires the shared advisory lock on a dedicated connection and
// keeps it until the test ends, so the integration suites never reset each
// other's schema while a sibling package is mid migration (it is the same lock
// `cmd/migrate up` takes).
func lockSchema(t *testing.T, db *database.Database) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), schemaSetupTimeout)
	defer cancel()

	release, err := db.LockSchema(ctx)
	if err != nil {
		t.Fatalf("lock schema: %v", err)
	}
	t.Cleanup(release)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
