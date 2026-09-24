package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestActorTypeHelpers(t *testing.T) {
	actorTypes := AllActorTypes()
	if len(actorTypes) != 3 {
		t.Fatalf("len(AllActorTypes()) = %d, want 3", len(actorTypes))
	}
	for _, actorType := range actorTypes {
		if !actorType.Valid() {
			t.Errorf("AllActorTypes() returned the invalid type %q", actorType)
		}
	}
	if ActorType("robot").Valid() {
		t.Error("ActorType(robot).Valid() = true, want false")
	}
}

func TestEntryNormalizeAppliesDefaults(t *testing.T) {
	entry := &Entry{ActorType: ActorUser, ActorID: UUID(uuid.New()), Action: "  STORE.CREATED  "}
	entry.Normalize()

	if entry.Action != "store.created" {
		t.Errorf("Action = %q, want store.created", entry.Action)
	}
	if string(entry.Metadata) != DefaultMetadata {
		t.Errorf("Metadata = %s, want %s", entry.Metadata, DefaultMetadata)
	}
	if entry.ID == uuid.Nil {
		t.Error("ID must be assigned by Normalize()")
	}
	if entry.CreatedAt.IsZero() {
		t.Error("CreatedAt must be assigned by Normalize()")
	}
	if got := entry.TableName(); got != "audit_logs" {
		t.Errorf("TableName() = %q, want audit_logs", got)
	}
}

func TestEntryBuilders(t *testing.T) {
	userID, tenantID, storeID, entityID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	entry := New(ActorUser, &userID, "employee.status_changed").
		ForTenant(tenantID).
		ForStore(storeID).
		OnEntity(" employee ", entityID).
		WithMetadata(json.RawMessage(`{"from":"available","to":"break"}`)).
		FromAddr(netip.MustParseAddr("203.0.113.7"))

	if entry.TenantID == nil || *entry.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %s", entry.TenantID, tenantID)
	}
	if entry.StoreID == nil || *entry.StoreID != storeID {
		t.Errorf("StoreID = %v, want %s", entry.StoreID, storeID)
	}
	if entry.EntityType == nil || *entry.EntityType != "employee" {
		t.Errorf("EntityType = %v, want employee", entry.EntityType)
	}
	if entry.EntityID == nil || *entry.EntityID != entityID {
		t.Errorf("EntityID = %v, want %s", entry.EntityID, entityID)
	}
	if entry.IP == nil || *entry.IP != "203.0.113.7" {
		t.Errorf("IP = %v, want 203.0.113.7", entry.IP)
	}

	entry.Normalize()
	if err := entry.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEntryBuildersIgnoreZeroValues(t *testing.T) {
	entry := New(ActorSystem, nil, "platform.retention_run").
		ForTenant(uuid.Nil).
		ForStore(uuid.Nil).
		OnEntity("   ", uuid.Nil).
		FromAddr(netip.Addr{})

	if entry.TenantID != nil || entry.StoreID != nil || entry.EntityType != nil || entry.EntityID != nil {
		t.Errorf("zero value builders must leave fields unset: %+v", entry)
	}
	if entry.IP != nil {
		t.Errorf("IP = %v, want nil for an invalid address", entry.IP)
	}
	if entry.ActorID != nil {
		t.Errorf("ActorID = %v, want nil for a zero id", entry.ActorID)
	}
}

func TestEntryValidateRejectsInvalidEntries(t *testing.T) {
	systemEntry := func() *Entry { return New(ActorSystem, nil, "store.created") }
	systemEntryWithAction := func(action string) *Entry {
		entry := systemEntry()
		entry.Action = action
		return entry
	}

	cases := []struct {
		name    string
		entry   *Entry
		wantMsg string
	}{
		{"unknown actor", New(ActorType("robot"), nil, "store.created"), "actor_type"},
		{"user without id", New(ActorUser, nil, "store.created"), "actor_id"},
		{"device without id", New(ActorDevice, nil, "store.created"), "actor_id"},
		{"action too short", systemEntryWithAction("ab"), "at least"},
		{"action too long", systemEntryWithAction(strings.Repeat("a", MaxActionLength+1)), "characters or fewer"},
		{"action format", systemEntryWithAction("store created"), "dotted snake_case"},
		{"metadata not an object", systemEntry().WithMetadata(json.RawMessage(`[1,2]`)), "metadata"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.entry.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantMsg)
			}
		})
	}

	var nilEntry *Entry
	if err := nilEntry.Validate(); !errors.Is(err, ErrValidation) {
		t.Errorf("nil entry Validate() = %v, want ErrValidation", err)
	}
}

func TestEntryValidateAcceptsDocumentedActorTypes(t *testing.T) {
	userID, deviceID := uuid.New(), uuid.New()

	entries := []*Entry{
		New(ActorUser, &userID, "store.created"),
		New(ActorDevice, &deviceID, "device.paired"),
		New(ActorSystem, nil, "platform.retention_run"),
	}

	for _, entry := range entries {
		entry.Normalize()
		if err := entry.Validate(); err != nil {
			t.Errorf("Validate(%s) error = %v", entry.Action, err)
		}
	}
}

func TestRepositoryWithoutDatabaseAnswersNotConfigured(t *testing.T) {
	ctx := context.Background()
	entry := New(ActorSystem, nil, "store.created")

	if err := NewRepository(nil).Record(ctx, entry); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Record() = %v, want ErrNotConfigured", err)
	}

	var nilRepository *Repository
	if err := nilRepository.Record(ctx, entry); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil repository Record() = %v, want ErrNotConfigured", err)
	}
}
