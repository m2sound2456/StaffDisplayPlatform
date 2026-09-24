package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// frontendSlugModule is the third place the slug rules live (BLUEPRINT §4):
// the admin UI validates the same rule before the API ever sees it.
var frontendSlugModule = filepath.Join("..", "..", "..", "frontend", "src", "lib", "storeSlug.ts")

// migrationSQL returns the content of one embedded migration.
func migrationSQL(t *testing.T, name string) string {
	t.Helper()

	content, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(content)
}

// TestSlugRulesMatchDatabaseConstraints guards the three way parity between
// backend/internal/store, migrations/0002+0003 and the CHECK constraints they
// install: changing one without the others must fail here.
func TestSlugRulesMatchDatabaseConstraints(t *testing.T) {
	for _, name := range []string{"0002_create_tenants.sql", "0003_create_stores.sql"} {
		sql := migrationSQL(t, name)

		if !strings.Contains(sql, "slug::text ~ '"+SlugPattern+"'") {
			t.Errorf("%s must enforce the shared slug pattern %q on the text form (a bare citext match would be case-insensitive)", name, SlugPattern)
		}
		for _, reserved := range ReservedSlugs {
			if !strings.Contains(sql, "'"+reserved+"'") {
				t.Errorf("%s must reject the reserved platform slug %q", name, reserved)
			}
		}
	}
}

func TestTenantsMigrationInstallsSlugKeyAndTrigger(t *testing.T) {
	sql := migrationSQL(t, "0002_create_tenants.sql")

	checks := []struct {
		name    string
		pattern string
	}{
		{"citext slug", `slug\s+citext\s+NOT NULL`},
		{"globally unique slug", `CREATE UNIQUE INDEX IF NOT EXISTS tenants_slug_key\s+ON tenants \(slug\)`},
		{"updated_at trigger", `CREATE TRIGGER trg_tenants_updated_at`},
		{"status values", `status IN \('active', 'suspended', 'archived'\)`},
	}

	for _, check := range checks {
		if !regexp.MustCompile(check.pattern).MatchString(sql) {
			t.Errorf("0002_create_tenants.sql is missing the %s rule", check.name)
		}
	}
}

func TestStoresMigrationKeepsTenantIsolationAndPublicSlugKey(t *testing.T) {
	sql := migrationSQL(t, "0003_create_stores.sql")

	checks := []struct {
		name    string
		pattern string
	}{
		{"tenant isolation root", `tenant_id\s+uuid\s+NOT NULL REFERENCES tenants\(id\) ON DELETE CASCADE`},
		{"globally unique live slug", `CREATE UNIQUE INDEX IF NOT EXISTS stores_slug_active_key\s+ON stores \(slug\)\s+WHERE deleted_at IS NULL`},
		{"soft delete column", `deleted_at\s+timestamptz`},
		{"updated_at trigger", `CREATE TRIGGER trg_stores_updated_at`},
		{"citext slug", `slug\s+citext\s+NOT NULL`},
		{"timezone rule", `timezone ~ '\^\[A-Za-z\]`},
		{"opening hours must be an object", `jsonb_typeof\(opening_hours\) = 'object'`},
	}

	for _, check := range checks {
		if !regexp.MustCompile(check.pattern).MatchString(sql) {
			t.Errorf("0003_create_stores.sql is missing the %s rule", check.name)
		}
	}

	// The reserved path list must cover the SPA routes served by the same domain.
	for _, reserved := range []string{"app", "setup", "api", "ws"} {
		if !strings.Contains(sql, "'"+reserved+"'") {
			t.Errorf("0003_create_stores.sql must reserve the platform path %q", reserved)
		}
	}
}

func TestAuditLogKeepsHistoryOnDelete(t *testing.T) {
	sql := migrationSQL(t, "0004_create_audit_logs.sql")

	checks := []struct {
		name    string
		pattern string
	}{
		{"tenant history", `tenant_id\s+uuid\s+REFERENCES tenants\(id\) ON DELETE SET NULL`},
		{"store history", `store_id\s+uuid\s+REFERENCES stores\(id\) ON DELETE SET NULL`},
		{"actor types", `actor_type IN \('user', 'device', 'system'\)`},
		{"readable by store", `CREATE INDEX IF NOT EXISTS audit_logs_store_created_idx`},
	}

	for _, check := range checks {
		if !regexp.MustCompile(check.pattern).MatchString(sql) {
			t.Errorf("0004_create_audit_logs.sql is missing the %s rule", check.name)
		}
	}
}

// TestSlugRulesMatchFrontend fails when the admin UI and the API drift apart:
// a slug accepted by the form must be accepted by the database and vice versa.
func TestSlugRulesMatchFrontend(t *testing.T) {
	content, err := os.ReadFile(frontendSlugModule)
	if err != nil {
		t.Fatalf("cannot read %s: %v (did the frontend slug module move? update this parity test)", frontendSlugModule, err)
	}
	source := string(content)

	patternMatch := regexp.MustCompile(`STORE_SLUG_PATTERN\s*=\s*/\^(.*?)\$/`).FindStringSubmatch(source)
	if patternMatch == nil {
		t.Fatalf("STORE_SLUG_PATTERN not found in %s", frontendSlugModule)
	}
	// JavaScript uses a non-capturing group; Go and PostgreSQL do not support it.
	frontendPattern := "^" + strings.ReplaceAll(patternMatch[1], "(?:", "(") + "$"
	if frontendPattern != SlugPattern {
		t.Errorf("frontend slug pattern %q != backend pattern %q", frontendPattern, SlugPattern)
	}

	listMatch := regexp.MustCompile(`RESERVED_STORE_SLUGS[^=]*=\s*\[([^\]]*)\]`).FindStringSubmatch(source)
	if listMatch == nil {
		t.Fatalf("RESERVED_STORE_SLUGS not found in %s", frontendSlugModule)
	}

	frontendReserved := make(map[string]bool)
	for _, match := range regexp.MustCompile(`'([a-z0-9-]+)'`).FindAllStringSubmatch(listMatch[1], -1) {
		frontendReserved[match[1]] = true
	}

	if len(frontendReserved) != len(ReservedSlugs) {
		t.Errorf("frontend reserves %d slugs, backend reserves %d", len(frontendReserved), len(ReservedSlugs))
	}
	for _, reserved := range ReservedSlugs {
		if !frontendReserved[reserved] {
			t.Errorf("backend reserves %q but the frontend does not", reserved)
		}
	}
}
