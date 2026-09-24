// Package audit owns the append-only audit trail skeleton (BLUEPRINT §11.10,
// migrations/0004_create_audit_logs.sql).
//
// FG2 ships the model and the write path only: no HTTP endpoint and no caller
// yet. Every privileged action introduced from FG4 onwards (login, store CRUD,
// device pairing/revoke, employee changes) records one Entry here, so the audit
// trail is complete by the time it becomes a compliance requirement.
//
// An audit entry references a tenant and/or store but never owns them: the
// foreign keys are ON DELETE SET NULL so history survives deletion, which is
// also why the ids are pointers.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// ActionPattern is the dotted snake_case action rule, mirrored by the DB
	// CHECK constraint in 0004_create_audit_logs.sql.
	ActionPattern = `^[a-z0-9_]+(\.[a-z0-9_]+)*$`

	// MinActionLength and MaxActionLength bound the action column.
	MinActionLength = 3
	MaxActionLength = 120

	// DefaultMetadata is stored when an entry carries no metadata.
	DefaultMetadata = "{}"
)

var actionRegexp = regexp.MustCompile(ActionPattern)

// ActorType identifies who performed an action. A display device is a first
// class actor with its own id (FG16–FG21): it is never recorded as a store.
type ActorType string

// Actor types recorded in audit_logs.
const (
	ActorUser   ActorType = "user"
	ActorDevice ActorType = "device"
	ActorSystem ActorType = "system"
)

// Valid reports whether the actor type is documented.
func (a ActorType) Valid() bool {
	switch a {
	case ActorUser, ActorDevice, ActorSystem:
		return true
	default:
		return false
	}
}

// AllActorTypes lists the documented actor types.
func AllActorTypes() []ActorType {
	return []ActorType{ActorUser, ActorDevice, ActorSystem}
}

// Sentinel errors of the audit package.
var (
	// ErrValidation reports an entry that can never be written.
	ErrValidation = errors.New("audit: invalid entry")
	// ErrNotConfigured reports a repository built without a database handle.
	ErrNotConfigured = errors.New("audit: repository is not configured")
)

// Entry is the GORM entity of one audit_logs row. Rows are immutable: the
// package only ever inserts (retention jobs delete, they never rewrite).
type Entry struct {
	ID         uuid.UUID       `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	TenantID   *uuid.UUID      `gorm:"column:tenant_id;type:uuid"`
	StoreID    *uuid.UUID      `gorm:"column:store_id;type:uuid"`
	ActorType  ActorType       `gorm:"column:actor_type;not null"`
	ActorID    *uuid.UUID      `gorm:"column:actor_id;type:uuid"`
	Action     string          `gorm:"column:action;not null"`
	EntityType *string         `gorm:"column:entity_type"`
	EntityID   *uuid.UUID      `gorm:"column:entity_id;type:uuid"`
	Metadata   json.RawMessage `gorm:"column:metadata;type:jsonb;not null;default:{}"`
	// IP is the caller address. The column is inet (PostgreSQL validates it) and
	// the value round-trips as its canonical text form, e.g. "203.0.113.7".
	IP        *string   `gorm:"column:ip;type:inet"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

// TableName implements gorm.Tabler.
func (Entry) TableName() string { return "audit_logs" }

// New starts an entry: who did it (actor) and what happened (action).
//
//	audit.New(audit.ActorUser, &userID, "store.created").ForTenant(tenantID).ForStore(store.ID)
func New(actorType ActorType, actorID *uuid.UUID, action string) *Entry {
	return &Entry{
		ActorType: actorType,
		ActorID:   cloneUUID(actorID),
		Action:    action,
	}
}

// ForTenant binds the entry to a tenant.
func (e *Entry) ForTenant(tenantID uuid.UUID) *Entry {
	if e == nil || tenantID == uuid.Nil {
		return e
	}
	e.TenantID = UUID(tenantID)
	return e
}

// ForStore binds the entry to a store.
func (e *Entry) ForStore(storeID uuid.UUID) *Entry {
	if e == nil || storeID == uuid.Nil {
		return e
	}
	e.StoreID = UUID(storeID)
	return e
}

// OnEntity records the affected resource (for example "employee" and its id).
func (e *Entry) OnEntity(entityType string, entityID uuid.UUID) *Entry {
	if e == nil {
		return e
	}
	normalized := strings.TrimSpace(entityType)
	if normalized != "" {
		e.EntityType = &normalized
	}
	if entityID != uuid.Nil {
		e.EntityID = UUID(entityID)
	}
	return e
}

// WithMetadata attaches action specific detail. Never put secrets, tokens or
// passwords in here (docs/AI_RULES.md §5.7).
func (e *Entry) WithMetadata(raw json.RawMessage) *Entry {
	if e == nil {
		return e
	}
	e.Metadata = append(json.RawMessage(nil), raw...)
	return e
}

// FromAddr records the caller IP address.
func (e *Entry) FromAddr(addr netip.Addr) *Entry {
	if e == nil || !addr.IsValid() {
		return e
	}
	value := addr.String()
	e.IP = &value
	return e
}

// Normalize applies defaults and canonical form (idempotent).
func (e *Entry) Normalize() {
	if e == nil {
		return
	}
	e.Action = strings.ToLower(strings.TrimSpace(e.Action))
	if e.EntityType != nil {
		trimmed := strings.TrimSpace(*e.EntityType)
		if trimmed == "" {
			e.EntityType = nil
		} else {
			e.EntityType = &trimmed
		}
	}
	if len(e.Metadata) == 0 || string(e.Metadata) == "null" {
		e.Metadata = json.RawMessage(DefaultMetadata)
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
}

// Validate enforces the table constraints before a row reaches PostgreSQL.
func (e *Entry) Validate() error {
	if e == nil {
		return fmt.Errorf("%w: entry is required", ErrValidation)
	}

	problems := make([]string, 0, 3)

	if !e.ActorType.Valid() {
		problems = append(problems, "actor_type must be one of user, device, system")
	}
	if (e.ActorType == ActorUser || e.ActorType == ActorDevice) && (e.ActorID == nil || *e.ActorID == uuid.Nil) {
		problems = append(problems, "actor_id is required for user and device actors")
	}
	switch {
	case len(e.Action) < MinActionLength:
		problems = append(problems, fmt.Sprintf("action must be at least %d characters", MinActionLength))
	case len(e.Action) > MaxActionLength:
		problems = append(problems, fmt.Sprintf("action must be %d characters or fewer", MaxActionLength))
	case !actionRegexp.MatchString(e.Action):
		problems = append(problems, "action must be dotted snake_case, e.g. store.created")
	}

	if len(e.Metadata) > 0 && string(e.Metadata) != "null" {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(e.Metadata, &probe); err != nil {
			problems = append(problems, "metadata must be a JSON object")
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrValidation, strings.Join(problems, "; "))
}

// Recorder is the narrow interface feature groups depend on, so they can be
// tested without PostgreSQL.
type Recorder interface {
	Record(ctx context.Context, entry *Entry) error
}

// Repository writes audit entries. It never reads or updates: the table is
// append-only by design.
type Repository struct {
	db *gorm.DB
}

// compile time proof that the repository implements Recorder.
var _ Recorder = (*Repository)(nil)

// NewRepository builds the writer on top of a GORM handle. A nil handle is
// tolerated: Record answers ErrNotConfigured instead of panicking.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Record validates and inserts one entry.
func (r *Repository) Record(ctx context.Context, entry *Entry) error {
	if r == nil || r.db == nil {
		return ErrNotConfigured
	}
	if entry == nil {
		return fmt.Errorf("%w: entry is required", ErrValidation)
	}

	entry.Normalize()
	if err := entry.Validate(); err != nil {
		return err
	}

	if err := r.db.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("write audit entry %q: %w", entry.Action, err)
	}
	return nil
}

// UUID returns a pointer to a copy of id, for the pointer typed id columns.
func UUID(id uuid.UUID) *uuid.UUID { return cloneUUID(&id) }

func cloneUUID(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := *id
	return &value
}
