// Package auth owns human authentication and authorization (FG4, BLUEPRINT §5
// and §11): password hashing, JWT access tokens with signing-key rotation,
// refresh-token sessions, roles and the tenant scope of a caller.
//
// Boundaries:
//
//   - It authenticates **users** (super admin, store admin). Display devices are
//     a separate identity (device_id + hashed device token, FG19–FG21) and never
//     appear here; a store-admin credential is never stored on a tablet.
//   - The tenant scope of a request comes from the users row, re-read on every
//     request: a JWT claim only mirrors it, so a client cannot widen its scope
//     by editing an id, a slug or a request parameter (BLUEPRINT §3.2).
//   - Passwords are bcrypt hashes; refresh tokens are stored as sha256 digests.
//     No secret, token or password is ever logged or written to audit metadata.
package auth

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// Role is the authorization role of a human account (BLUEPRINT §5). Stored as
// text with a matching DB CHECK constraint (migrations/0005_create_users.sql),
// so adding a value stays a forward migration.
type Role string

// Platform roles. A display device is *not* a role: it is a separate actor with
// its own identity and token (FG16–FG21).
const (
	// RoleSuperAdmin manages the platform: tenants, stores, devices, settings.
	RoleSuperAdmin Role = "super_admin"
	// RoleStoreAdmin manages exactly one tenant/store: employees, media,
	// display settings and devices.
	RoleStoreAdmin Role = "store_admin"
)

// Valid reports whether the role is one of the documented values.
func (r Role) Valid() bool {
	switch r {
	case RoleSuperAdmin, RoleStoreAdmin:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (r Role) String() string { return string(r) }

// IsPlatform reports whether the role acts platform-wide (no tenant scope).
func (r Role) IsPlatform() bool { return r == RoleSuperAdmin }

// AllRoles lists the documented roles in documentation order.
func AllRoles() []Role { return []Role{RoleSuperAdmin, RoleStoreAdmin} }

// RoleNames renders the roles for a validation message.
func RoleNames() string {
	names := make([]string, 0, len(AllRoles()))
	for _, role := range AllRoles() {
		names = append(names, string(role))
	}
	return strings.Join(names, ", ")
}

// Status is the lifecycle state of an account.
type Status string

// Account statuses.
const (
	// StatusActive can sign in.
	StatusActive Status = "active"
	// StatusDisabled keeps the data but refuses sign-in and invalidates every
	// existing session on the next request.
	StatusDisabled Status = "disabled"
)

// Valid reports whether the status is a documented value.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusDisabled:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (s Status) String() string { return string(s) }

// AllStatuses lists the documented account statuses.
func AllStatuses() []Status { return []Status{StatusActive, StatusDisabled} }

// StatusNames renders the account statuses for a validation message.
func StatusNames() string {
	names := make([]string, 0, len(AllStatuses()))
	for _, status := range AllStatuses() {
		names = append(names, string(status))
	}
	return strings.Join(names, ", ")
}

// Field limits, mirrored by the CHECK constraints of 0005_create_users.sql.
const (
	// MaxDisplayNameLength bounds display_name.
	MaxDisplayNameLength = 120
	// MaxEmailLength bounds email (practical RFC 5321 limit).
	MaxEmailLength = 254
)

// EmailPattern is the practical e-mail shape rule of the database CHECK
// constraint (POSIX syntax); emailRegexp is its Go equivalent and
// TestEmailRulesMatchDatabaseConstraint keeps the two in sync.
const EmailPattern = `^[^@[:space:]]+@[^@[:space:]]+[.][^@[:space:]]+$`

var emailRegexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// User is the GORM entity of the users table (migrations/0005_create_users.sql).
//
// The password hash can never leave the API: it carries `json:"-"` and the HTTP
// layer serialises an explicit DTO on top of this entity.
type User struct {
	ID           uuid.UUID      `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	TenantID     *uuid.UUID     `gorm:"column:tenant_id;type:uuid"`
	StoreID      *uuid.UUID     `gorm:"column:store_id;type:uuid"`
	Email        string         `gorm:"column:email;type:citext;not null"`
	DisplayName  string         `gorm:"column:display_name;not null"`
	PasswordHash string         `gorm:"column:password_hash;not null" json:"-"`
	Role         Role           `gorm:"column:role;not null"`
	Status       Status         `gorm:"column:status;not null;default:active"`
	LastLoginAt  *time.Time     `gorm:"column:last_login_at"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;not null"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

// TableName implements gorm.Tabler.
func (User) TableName() string { return "users" }

// IsActive reports whether the account may sign in.
func (u *User) IsActive() bool { return u != nil && u.Status == StatusActive }

// IsDeleted reports whether the account was soft deleted. Deleted accounts are
// invisible to every query but keep their rows and audit trail.
func (u *User) IsDeleted() bool { return u != nil && u.DeletedAt.Valid }

// HasTenant reports whether the account carries a tenant scope.
func (u *User) HasTenant() bool { return u != nil && u.TenantID != nil && *u.TenantID != uuid.Nil }

// HasStore reports whether the account is bound to one store.
func (u *User) HasStore() bool { return u != nil && u.StoreID != nil && *u.StoreID != uuid.Nil }

// TenantIDValue returns the tenant id or the nil UUID for a platform account.
func (u *User) TenantIDValue() uuid.UUID {
	if !u.HasTenant() {
		return uuid.Nil
	}
	return *u.TenantID
}

// StoreIDValue returns the store id or the nil UUID when the account is not
// bound to a single store.
func (u *User) StoreIDValue() uuid.UUID {
	if !u.HasStore() {
		return uuid.Nil
	}
	return *u.StoreID
}

// Scope returns the tenant isolation scope of the account. A platform account
// (super admin) has no tenant scope: the call fails closed with
// ErrMissingTenantScope instead of producing an unscoped query (FG2 invariant).
func (u *User) Scope() (store.Scope, error) {
	if u == nil {
		return store.Scope{}, newValidationError(fieldError("user", "is required"))
	}
	if !u.HasTenant() {
		return store.Scope{}, fmt.Errorf("%w: user %s has no tenant (platform account)", ErrMissingTenantScope, u.ID)
	}
	if u.HasStore() {
		return store.StoreScope(*u.TenantID, *u.StoreID), nil
	}
	return store.TenantScope(*u.TenantID), nil
}

// NewUserInput is the payload for creating an account (FG5 wires the admin HTTP
// layer on top of it). The tenant scope comes from the caller's Scope and never
// from this struct.
type NewUserInput struct {
	Email        string
	DisplayName  string
	PasswordHash string
	Role         Role
	Status       Status
	StoreID      *uuid.UUID
}

// NewUser builds a validated account inside scope, applying the documented
// defaults. The password hash must already be a bcrypt digest produced by
// HashPassword — plaintext can never reach the database.
func NewUser(scope store.Scope, in NewUserInput) (*User, error) {
	user := &User{
		Email:        in.Email,
		DisplayName:  in.DisplayName,
		PasswordHash: in.PasswordHash,
		Role:         in.Role,
		Status:       in.Status,
		StoreID:      cloneUUID(in.StoreID),
	}
	if in.Role.IsPlatform() {
		if !scope.IsZero() {
			return nil, newValidationError(fieldError("role", "%s is platform scoped and must not carry a tenant", RoleSuperAdmin))
		}
	} else {
		guard, err := tenantScope(scope)
		if err != nil {
			return nil, err
		}
		user.TenantID = cloneUUID(&guard.TenantID)
		if guard.HasStore() {
			user.StoreID = cloneUUID(&guard.StoreID)
		}
	}

	user.Normalize()
	if user.ID == uuid.Nil {
		// The database default (gen_random_uuid) is mirrored here so the
		// validation of a new account sees a complete row.
		user.ID = uuid.New()
	}
	if err := user.Validate(); err != nil {
		return nil, err
	}
	return user, nil
}

// Normalize applies the canonical form (trimmed lowercase e-mail, trimmed
// display name, default status) — idempotent.
func (u *User) Normalize() {
	if u == nil {
		return
	}
	u.Email = NormalizeEmail(u.Email)
	u.DisplayName = strings.TrimSpace(u.DisplayName)
	if u.Status == "" {
		u.Status = StatusActive
	}
	if u.StoreID != nil && (*u.StoreID == uuid.Nil || !u.HasTenant()) {
		u.StoreID = nil
	}
}

// Validate mirrors the database constraints so a bad payload fails with a field
// level 422 instead of a raw driver error.
func (u *User) Validate() error {
	if u == nil {
		return newValidationError(fieldError("user", "is required"))
	}

	fields := make([]FieldError, 0, 6)

	if u.ID == uuid.Nil {
		fields = append(fields, fieldError("id", "is required"))
	}

	switch {
	case u.Email == "":
		fields = append(fields, fieldError("email", "is required"))
	case len(u.Email) > MaxEmailLength:
		fields = append(fields, fieldError("email", "must be %d characters or fewer", MaxEmailLength))
	case !emailRegexp.MatchString(u.Email):
		fields = append(fields, fieldError("email", "must be a valid e-mail address"))
	}

	switch name := strings.TrimSpace(u.DisplayName); {
	case name == "":
		fields = append(fields, fieldError("display_name", "is required"))
	case len([]rune(name)) > MaxDisplayNameLength:
		fields = append(fields, fieldError("display_name", "must be %d characters or fewer", MaxDisplayNameLength))
	}

	if !IsPasswordHash(u.PasswordHash) {
		fields = append(fields, fieldError("password_hash", "must be a bcrypt hash produced by HashPassword"))
	}

	if !u.Role.Valid() {
		fields = append(fields, fieldError("role", "must be one of %s", RoleNames()))
	}
	if !u.Status.Valid() {
		fields = append(fields, fieldError("status", "must be one of %s", StatusNames()))
	}

	// The role decides the scope; an inconsistent row is a bug, not user input.
	switch {
	case u.Role.IsPlatform():
		if u.HasTenant() || u.HasStore() {
			fields = append(fields, fieldError("role", "%s must not carry a tenant or store", RoleSuperAdmin))
		}
	case u.Role == RoleStoreAdmin && !u.HasTenant():
		fields = append(fields, fieldError("tenant_id", "is required for %s", RoleStoreAdmin))
	}
	if u.HasStore() && !u.HasTenant() {
		fields = append(fields, fieldError("store_id", "requires a tenant"))
	}

	if len(fields) == 0 {
		return nil
	}
	return newValidationError(fields...)
}

// NormalizeEmail trims and lowercases an address so a login is case-insensitive
// even on a database without citext (the column is citext as well).
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// tenantScope guards the isolation invariant: no tenant id, no query.
func tenantScope(scope store.Scope) (store.Scope, error) {
	if scope.IsZero() {
		return store.Scope{}, fmt.Errorf("%w: build the scope with store.TenantScope(...) or store.StoreScope(...)", ErrMissingTenantScope)
	}
	return scope, nil
}

// cloneUUID copies a pointer typed id (never shares the caller's storage).
func cloneUUID(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := *id
	return &value
}
