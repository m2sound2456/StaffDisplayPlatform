package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/audit"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/testsupport"
)

// mustAddr parses an IP address for the FromAddr builder.
func mustAddr(t *testing.T, address string) netip.Addr {
	t.Helper()

	parsed, err := netip.ParseAddr(address)
	if err != nil {
		t.Fatalf("parse address %q: %v", address, err)
	}
	return parsed
}

// setup migrates the test schema and returns a tenant with one store.
func setup(t *testing.T) (*database.Database, uuid.UUID, uuid.UUID) {
	t.Helper()

	db := testsupport.Open(t)
	testsupport.MigrateSchema(t, db)

	tenantID := testsupport.NewTenant(t, db, "Tenant A")
	storeID := testsupport.NewStore(t, db, tenantID, "Shop A", "shop-a")
	return db, tenantID, storeID
}

func TestRepositoryRecordsEntry(t *testing.T) {
	db, tenantID, storeID := setup(t)
	recorder := audit.NewRepository(db.Gorm())
	ctx := context.Background()
	userID := uuid.New()

	entry := audit.New(audit.ActorUser, &userID, "store.created").
		ForTenant(tenantID).
		ForStore(storeID).
		OnEntity("store", storeID).
		WithMetadata(json.RawMessage(`{"slug":"shop-a"}`))

	if err := recorder.Record(ctx, entry); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if entry.ID == uuid.Nil {
		t.Error("Record() must assign an id")
	}

	var stored audit.Entry
	if err := db.Gorm().WithContext(ctx).Where("id = ?", entry.ID).Take(&stored).Error; err != nil {
		t.Fatalf("read back entry: %v", err)
	}

	if stored.TenantID == nil || *stored.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %s", stored.TenantID, tenantID)
	}
	if stored.StoreID == nil || *stored.StoreID != storeID {
		t.Errorf("StoreID = %v, want %s", stored.StoreID, storeID)
	}
	if stored.ActorID == nil || *stored.ActorID != userID {
		t.Errorf("ActorID = %v, want %s", stored.ActorID, userID)
	}
	if stored.ActorType != audit.ActorUser {
		t.Errorf("ActorType = %q, want %q", stored.ActorType, audit.ActorUser)
	}
	if stored.Action != "store.created" {
		t.Errorf("Action = %q, want store.created", stored.Action)
	}
	if stored.EntityType == nil || *stored.EntityType != "store" {
		t.Errorf("EntityType = %v, want store", stored.EntityType)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("CreatedAt must be persisted")
	}

	var metadata map[string]string
	if err := json.Unmarshal(stored.Metadata, &metadata); err != nil {
		t.Fatalf("metadata is not a JSON object (%s): %v", stored.Metadata, err)
	}
	if metadata["slug"] != "shop-a" {
		t.Errorf("metadata = %v, want slug=shop-a", metadata)
	}
}

func TestRepositoryRecordsWithIPv4Address(t *testing.T) {
	db, tenantID, _ := setup(t)
	recorder := audit.NewRepository(db.Gorm())
	ctx := context.Background()
	address := "203.0.113.7"

	entry := audit.New(audit.ActorDevice, audit.UUID(uuid.New()), "device.paired").
		ForTenant(tenantID).
		FromAddr(mustAddr(t, address))

	if err := recorder.Record(ctx, entry); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var stored audit.Entry
	if err := db.Gorm().WithContext(ctx).Where("id = ?", entry.ID).Take(&stored).Error; err != nil {
		t.Fatalf("read back entry: %v", err)
	}
	if stored.IP == nil {
		t.Fatal("IP must round-trip through the inet column")
	}
	if *stored.IP != address {
		t.Errorf("IP = %q, want %q", *stored.IP, address)
	}
}

func TestRepositoryRecordsPlatformLevelEntry(t *testing.T) {
	db, _, _ := setup(t)
	recorder := audit.NewRepository(db.Gorm())
	ctx := context.Background()

	entry := audit.New(audit.ActorSystem, nil, "platform.retention_run")
	if err := recorder.Record(ctx, entry); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var stored audit.Entry
	if err := db.Gorm().WithContext(ctx).Where("id = ?", entry.ID).Take(&stored).Error; err != nil {
		t.Fatalf("read back entry: %v", err)
	}
	if stored.TenantID != nil || stored.StoreID != nil {
		t.Errorf("platform entry must not reference a tenant or store: %+v", stored)
	}
	if string(stored.Metadata) != "{}" {
		t.Errorf("Metadata = %s, want {}", stored.Metadata)
	}
}

func TestRepositoryRejectsInvalidEntriesWithoutWriting(t *testing.T) {
	db, _, _ := setup(t)
	recorder := audit.NewRepository(db.Gorm())
	ctx := context.Background()

	invalid := []*audit.Entry{
		audit.New(audit.ActorUser, nil, "store.created"),   // an actor that must carry an id
		audit.New(audit.ActorSystem, nil, "Store Created"), // not dotted snake_case
	}

	for _, entry := range invalid {
		if err := recorder.Record(ctx, entry); !errors.Is(err, audit.ErrValidation) {
			t.Errorf("Record(%q) = %v, want ErrValidation", entry.Action, err)
		}
	}

	var count int64
	if err := db.Gorm().WithContext(ctx).Model(&audit.Entry{}).Count(&count).Error; err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid entries must not be written, found %d rows", count)
	}
}

// TestAuditEntrySurvivesTenantDeletion documents the ON DELETE SET NULL design:
// a hard tenant delete removes the stores but never rewrites history.
func TestAuditEntrySurvivesTenantDeletion(t *testing.T) {
	db, tenantID, storeID := setup(t)
	recorder := audit.NewRepository(db.Gorm())
	ctx := context.Background()

	entry := audit.New(audit.ActorSystem, nil, "store.deleted").ForTenant(tenantID).ForStore(storeID)
	if err := recorder.Record(ctx, entry); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	if err := db.Gorm().WithContext(ctx).Exec("DELETE FROM tenants WHERE id = ?", tenantID.String()).Error; err != nil {
		t.Fatalf("delete tenant: %v", err)
	}

	var stores int64
	if err := db.Gorm().WithContext(ctx).Raw("SELECT count(*) FROM stores WHERE tenant_id = ?", tenantID.String()).Scan(&stores).Error; err != nil {
		t.Fatalf("count stores: %v", err)
	}
	if stores != 0 {
		t.Errorf("deleting a tenant must cascade to its stores, found %d", stores)
	}

	var stored struct {
		TenantID *uuid.UUID
		StoreID  *uuid.UUID
	}
	if err := db.Gorm().WithContext(ctx).Raw("SELECT tenant_id, store_id FROM audit_logs WHERE id = ?", entry.ID.String()).Scan(&stored).Error; err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if stored.TenantID != nil || stored.StoreID != nil {
		t.Errorf("audit row must survive with nulled references, got %+v", stored)
	}
}
