package database_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/testsupport"
	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// setupSchema applies the embedded migration set to a freshly reset test schema.
func setupSchema(t *testing.T) *database.Database {
	t.Helper()

	db := testsupport.Open(t)
	testsupport.MigrateSchema(t, db)
	return db
}

// TestMigrateAppliesEveryEmbeddedMigration checks the FG2 schema is what
// cmd/migrate would install on a production database.
func TestMigrateAppliesEveryEmbeddedMigration(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	loaded, err := database.LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}

	version, err := db.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if want := loaded[len(loaded)-1].Version; version != want {
		t.Errorf("schema version = %d, want %d", version, want)
	}

	for _, table := range []string{"schema_migrations", "tenants", "stores", "audit_logs"} {
		var name *string
		if err := db.Gorm().WithContext(ctx).Raw("SELECT to_regclass('public." + table + "')::text").Scan(&name).Error; err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}
		if name == nil {
			t.Errorf("table %s was not created", table)
		}
	}
}

// TestTenantScopedTablesExposeTheirIsolationKeys documents the columns every
// tenant/store scoped query depends on.
func TestTenantScopedTablesExposeTheirIsolationKeys(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	want := map[string]map[string]string{
		"tenants": {
			"id": "uuid", "name": "text", "slug": "citext", "status": "text",
			"created_at": "timestamptz", "updated_at": "timestamptz",
		},
		"stores": {
			"id": "uuid", "tenant_id": "uuid", "name": "text", "slug": "citext",
			"logo_url": "text", "status": "text", "timezone": "text",
			"opening_hours": "jsonb", "created_at": "timestamptz",
			"updated_at": "timestamptz", "deleted_at": "timestamptz",
		},
		"audit_logs": {
			"id": "uuid", "tenant_id": "uuid", "store_id": "uuid",
			"actor_type": "text", "actor_id": "uuid", "action": "text",
			"entity_type": "text", "entity_id": "uuid", "metadata": "jsonb",
			"ip": "inet", "created_at": "timestamptz",
		},
	}

	for table, columns := range want {
		var rows []struct {
			ColumnName string
			DataType   string
		}
		if err := db.Gorm().WithContext(ctx).Raw(`
			SELECT column_name, udt_name AS data_type
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = ?`, table).Scan(&rows).Error; err != nil {
			t.Fatalf("read columns of %s: %v", table, err)
		}

		found := make(map[string]string, len(rows))
		for _, row := range rows {
			found[row.ColumnName] = row.DataType
		}
		for column, dataType := range columns {
			if got, ok := found[column]; !ok {
				t.Errorf("%s.%s is missing", table, column)
			} else if got != dataType {
				t.Errorf("%s.%s has type %s, want %s", table, column, got, dataType)
			}
		}
		if table == "stores" && len(found) != len(columns) {
			t.Errorf("stores has %d columns, want %d: %v", len(found), len(columns), found)
		}
	}
}

// TestStoresSlugKeyIsGloballyUniqueAndPartial proves that /s/{slug} can only
// ever resolve one live store, while a soft deleted store releases its slug.
func TestStoresSlugKeyIsGloballyUniqueAndPartial(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	tenantA := testsupport.NewTenant(t, db, "Tenant A")
	tenantB := testsupport.NewTenant(t, db, "Tenant B")
	testsupport.NewStore(t, db, tenantA, "Shop A", "coffee")

	// Unique across tenants: the display URL is not tenant qualified.
	if err := db.Gorm().WithContext(ctx).Exec(
		"INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", tenantB.String(), "Shop B", "coffee",
	).Error; err == nil {
		t.Fatal("expected the global slug uniqueness index to reject the duplicate")
	} else if !strings.Contains(err.Error(), "stores_slug_active_key") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The comparison is case-insensitive (citext): the same lookup the
	// repository performs for /s/{slug}.
	var matches int64
	if err := db.Gorm().WithContext(ctx).Raw(
		"SELECT count(*) FROM stores WHERE slug = ?", "COFFEE",
	).Scan(&matches).Error; err != nil {
		t.Fatalf("case-insensitive lookup: %v", err)
	}
	if matches != 1 {
		t.Errorf("case-insensitive slug lookup found %d rows, want 1", matches)
	}

	// Soft delete the store: the slug becomes free again for another tenant.
	if err := db.Gorm().WithContext(ctx).Exec(
		"UPDATE stores SET deleted_at = now() WHERE slug = ?", "coffee",
	).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := db.Gorm().WithContext(ctx).Exec(
		"INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", tenantB.String(), "Shop B", "coffee",
	).Error; err != nil {
		t.Fatalf("a soft deleted store must release its slug: %v", err)
	}
}

// TestBusinessTablesInstallTheirIndexesAndTriggers guards the performance and
// bookkeeping contract of the FG2 schema (docs/DATABASE.md §2).
func TestBusinessTablesInstallTheirIndexesAndTriggers(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	indexes := []string{
		"tenants_slug_key",
		"stores_slug_active_key",
		"stores_tenant_id_idx",
		"stores_tenant_status_idx",
		"audit_logs_store_created_idx",
		"audit_logs_tenant_created_idx",
		"audit_logs_action_created_idx",
	}
	for _, index := range indexes {
		var found int64
		if err := db.Gorm().WithContext(ctx).Raw(
			"SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = ?", index,
		).Scan(&found).Error; err != nil {
			t.Fatalf("look up index %s: %v", index, err)
		}
		if found != 1 {
			t.Errorf("index %s is missing", index)
		}
	}

	for _, trigger := range []string{"trg_tenants_updated_at", "trg_stores_updated_at"} {
		var found int64
		if err := db.Gorm().WithContext(ctx).Raw(
			"SELECT count(*) FROM pg_trigger WHERE tgname = ?", trigger,
		).Scan(&found).Error; err != nil {
			t.Fatalf("look up trigger %s: %v", trigger, err)
		}
		if found != 1 {
			t.Errorf("trigger %s is missing", trigger)
		}
	}
}

// TestUpdatedAtTriggerRefreshesTimestamps proves the shared set_updated_at()
// helper is attached: the application never has to write updated_at by hand.
func TestUpdatedAtTriggerRefreshesTimestamps(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	tenantID := testsupport.NewTenant(t, db, "Tenant A")

	// Backdate the row, then touch it through a normal UPDATE.
	if err := db.Gorm().WithContext(ctx).Exec(
		"UPDATE tenants SET updated_at = now() - interval '2 days' WHERE id = ?", tenantID.String(),
	).Error; err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}

	if err := db.Gorm().WithContext(ctx).Exec(
		"UPDATE tenants SET name = 'Tenant A Renamed' WHERE id = ?", tenantID.String(),
	).Error; err != nil {
		t.Fatalf("update tenant: %v", err)
	}

	var stored struct {
		UpdatedAt time.Time
	}
	if err := db.Gorm().WithContext(ctx).Raw(
		"SELECT updated_at FROM tenants WHERE id = ?", tenantID.String(),
	).Scan(&stored).Error; err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !stored.UpdatedAt.After(time.Now().UTC().Add(-time.Minute)) {
		t.Errorf("updated_at = %s was not refreshed by the trigger", stored.UpdatedAt)
	}
}

// TestDatabaseConstraintsRejectInvalidRows is the last line of defence behind
// the Go validators: even a raw SQL client cannot store invalid data.
func TestDatabaseConstraintsRejectInvalidRows(t *testing.T) {
	db := setupSchema(t)
	ctx := context.Background()

	tenantID := testsupport.NewTenant(t, db, "Tenant A")
	testsupport.NewStore(t, db, tenantID, "Shop A", "shop-a")

	cases := []struct {
		name       string
		statement  string
		args       []any
		constraint string
	}{
		{"reserved slug", "INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", []any{tenantID.String(), "Reserved", "setup"}, "stores_slug_not_reserved"},
		{"uppercase slug", "INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", []any{tenantID.String(), "Upper", "SHOP"}, "stores_slug_format"},
		{"slug format", "INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", []any{tenantID.String(), "Format", "-bad-"}, "stores_slug_format"},
		{"blank name", "INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", []any{tenantID.String(), "   ", "blank-name"}, "stores_name_not_blank"},
		{"unknown status", "INSERT INTO stores (tenant_id, name, slug, status) VALUES (?, ?, ?, ?)", []any{tenantID.String(), "Status", "status-shop", "closed"}, "stores_status_valid"},
		{"bad timezone", "INSERT INTO stores (tenant_id, name, slug, timezone) VALUES (?, ?, ?, ?)", []any{tenantID.String(), "Zone", "zone-shop", "Asia Bkk"}, "stores_timezone_format"},
		{"opening hours array", "INSERT INTO stores (tenant_id, name, slug, opening_hours) VALUES (?, ?, ?, ?::jsonb)", []any{tenantID.String(), "Hours", "hours-shop", "[1,2]"}, "stores_opening_hours_is_object"},
		{"unknown tenant", "INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)", []any{uuid.New().String(), "Orphan", "orphan"}, "stores_tenant_id_fkey"},
		{"unknown actor type", "INSERT INTO audit_logs (actor_type, action) VALUES (?, ?)", []any{"robot", "store.created"}, "audit_logs_actor_type_valid"},
		{"bad action", "INSERT INTO audit_logs (actor_type, action) VALUES (?, ?)", []any{"system", "Store Created"}, "audit_logs_action_format"},
		{"metadata array", "INSERT INTO audit_logs (actor_type, action, metadata) VALUES (?, ?, ?::jsonb)", []any{"system", "store.created", "[1]"}, "audit_logs_metadata_is_object"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := db.Gorm().WithContext(ctx).Exec(tc.statement, tc.args...).Error
			if err == nil {
				t.Fatalf("expected the %s constraint to reject the row", tc.constraint)
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Errorf("error %v does not mention %s", err, tc.constraint)
			}
		})
	}
}
