package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// PostgreSQL error codes mapped to domain errors so the HTTP layer never has to
// interpret driver messages (docs/API.md §1).
const (
	pgCodeUniqueViolation     = "23505"
	pgCodeForeignKeyViolation = "23503"
	pgCodeCheckViolation      = "23514"
)

// Listing defaults.
const (
	// DefaultListLimit is used when a caller passes no limit.
	DefaultListLimit = 50
	// MaxListLimit caps a caller supplied limit.
	MaxListLimit = 200
)

// ListFilter narrows a user listing.
type ListFilter struct {
	// Limit caps the page size (0 → DefaultListLimit, > MaxListLimit → MaxListLimit).
	Limit int
	// Offset skips rows for paging (negative → 0).
	Offset int
}

// normalized clamps the filter into a safe, deterministic query.
func (f ListFilter) normalized() ListFilter {
	if f.Limit <= 0 {
		f.Limit = DefaultListLimit
	}
	if f.Limit > MaxListLimit {
		f.Limit = MaxListLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f
}

// AuthenticatedSession couples a session with the account it belongs to, so
// authentication never has to trust an id taken from a token: the tenant, store,
// role and status are read from the users row on every request.
type AuthenticatedSession struct {
	Session *Session
	User    *User
}

// Repository is the persistence contract of the auth package.
//
// Every method that reads or writes a tenant's users takes a store.Scope, with
// the single documented exception of FindUserByEmail: the login form is served
// from the single platform origin and carries no tenant hint, so the e-mail
// address (globally unique among live rows) is the one lookup that resolves the
// scope instead of being narrowed by it. The row it returns is authoritative —
// the caller's scope is derived from it, never from the request.
type Repository interface {
	// FindUserByEmail resolves a login candidate.
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	// GetUser returns one user of the scope, or ErrNotFound.
	GetUser(ctx context.Context, scope store.Scope, id uuid.UUID) (*User, error)
	// ListUsers returns the users of the scope, ordered deterministically.
	ListUsers(ctx context.Context, scope store.Scope, filter ListFilter) ([]User, error)
	// ListPlatformUsers returns every user of the platform. It is the platform
	// scoped counterpart of ListUsers and may only be reached through the super
	// admin role (BLUEPRINT §5: platform scope).
	ListPlatformUsers(ctx context.Context, filter ListFilter) ([]User, error)
	// CreateUser inserts a user inside scope (the scope wins over the model).
	CreateUser(ctx context.Context, scope store.Scope, user *User) error
	// TouchLastLogin stamps last_login_at of a tenant user of the scope.
	TouchLastLogin(ctx context.Context, scope store.Scope, id uuid.UUID, at time.Time) error
	// TouchPlatformLogin stamps last_login_at of a platform (super admin) user;
	// the statement is additionally guarded by tenant_id IS NULL.
	TouchPlatformLogin(ctx context.Context, id uuid.UUID, at time.Time) error

	// CreateSession opens a session row.
	CreateSession(ctx context.Context, session *Session) error
	// LoadSession returns a session and its user by session id (the sid claim of
	// a verified access token).
	LoadSession(ctx context.Context, id uuid.UUID) (*AuthenticatedSession, error)
	// FindSessionByRefreshDigest resolves a session by the digest of its refresh
	// token.
	FindSessionByRefreshDigest(ctx context.Context, digest TokenDigest) (*AuthenticatedSession, error)
	// RotateSession replaces the refresh digest of a live session.
	RotateSession(ctx context.Context, id uuid.UUID, digest TokenDigest, expiresAt, now time.Time) error
	// RevokeSession stamps revoked_at/revoked_reason of a session.
	RevokeSession(ctx context.Context, id uuid.UUID, reason string, at time.Time) error
}

// GORMRepository is the PostgreSQL implementation of Repository.
type GORMRepository struct {
	db *gorm.DB
}

// compile time proof that the implementation satisfies the contract.
var _ Repository = (*GORMRepository)(nil)

// NewRepository builds a repository on top of a GORM handle. A nil handle is
// tolerated: every method answers ErrNotConfigured instead of panicking.
func NewRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// session returns the request scoped GORM handle.
func (r *GORMRepository) session(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, ErrNotConfigured
	}
	return r.db.WithContext(ctx), nil
}

// mapWriteError translates PostgreSQL constraint violations into domain errors
// so a duplicate address becomes ErrEmailTaken and an unknown tenant becomes
// ErrTenantNotFound instead of a driver string.
func mapWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	switch pgErr.Code {
	case pgCodeUniqueViolation:
		if strings.Contains(pgErr.ConstraintName, "email") {
			return fmt.Errorf("%w: %s", ErrEmailTaken, pgErr.ConstraintName)
		}
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	case pgCodeForeignKeyViolation:
		if strings.Contains(pgErr.ConstraintName, "tenant") {
			return fmt.Errorf("%w: %s", ErrTenantNotFound, pgErr.ConstraintName)
		}
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	case pgCodeCheckViolation:
		return fmt.Errorf("%w: %s", ErrValidation, pgErr.ConstraintName)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

// FindUserByEmail resolves the login candidate with the given address. Live rows
// only: a soft deleted account can never sign in, and its address is free again.
func (r *GORMRepository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	normalized := NormalizeEmail(email)
	if normalized == "" {
		return nil, newValidationError(fieldError("email", "is required"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	var found User
	// email is citext: the comparison is case-insensitive without lower(email).
	result := db.Model(&User{}).Where("email = ?", normalized).Take(&found)
	switch {
	case errors.Is(result.Error, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("%w: user %s", ErrNotFound, normalized)
	case result.Error != nil:
		return nil, fmt.Errorf("load user by email: %w", result.Error)
	default:
		return &found, nil
	}
}

// GetUser loads one user inside the tenant scope.
func (r *GORMRepository) GetUser(ctx context.Context, scope store.Scope, id uuid.UUID) (*User, error) {
	if err := tenantScopeOnly(scope); err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, newValidationError(fieldError("id", "is required"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	var found User
	result := scope.Apply(db.Model(&User{})).Where("id = ?", id).Take(&found)
	switch {
	case errors.Is(result.Error, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("%w: user %s", ErrNotFound, id)
	case result.Error != nil:
		return nil, fmt.Errorf("load user %s: %w", id, result.Error)
	default:
		return &found, nil
	}
}

// ListUsers returns the users of one tenant.
func (r *GORMRepository) ListUsers(ctx context.Context, scope store.Scope, filter ListFilter) ([]User, error) {
	if err := tenantScopeOnly(scope); err != nil {
		return nil, err
	}
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	filter = filter.normalized()
	users := make([]User, 0, filter.Limit)
	result := scope.Apply(db.Model(&User{})).
		Order("email ASC, id ASC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&users)
	if result.Error != nil {
		return nil, fmt.Errorf("list users: %w", result.Error)
	}
	return users, nil
}

// ListPlatformUsers returns every user of the platform.
//
// This is the platform scope of a super admin (BLUEPRINT §5), not a bypass of
// tenant isolation: the HTTP layer reaches it only through the super_admin role,
// and a store admin request never calls it (the role middleware answers 403).
func (r *GORMRepository) ListPlatformUsers(ctx context.Context, filter ListFilter) ([]User, error) {
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	filter = filter.normalized()
	users := make([]User, 0, filter.Limit)
	result := db.Model(&User{}).
		Order("email ASC, id ASC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&users)
	if result.Error != nil {
		return nil, fmt.Errorf("list platform users: %w", result.Error)
	}
	return users, nil
}

// CreateUser inserts a user inside the scope. The scope is the only source of the
// tenant id for a tenant role.
func (r *GORMRepository) CreateUser(ctx context.Context, scope store.Scope, user *User) error {
	if user == nil {
		return newValidationError(fieldError("user", "is required"))
	}
	if !user.Role.IsPlatform() {
		if err := tenantScopeOnly(scope); err != nil {
			return err
		}
		user.TenantID = cloneUUID(&scope.TenantID)
		if scope.HasStore() {
			user.StoreID = cloneUUID(&scope.StoreID)
		} else if !user.HasStore() {
			user.StoreID = nil
		}
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	user.Normalize()
	now := time.Now().UTC()
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
	if err := user.Validate(); err != nil {
		return err
	}

	if err := db.Create(user).Error; err != nil {
		return mapWriteError("create user", err)
	}
	return nil
}

// TouchLastLogin stamps last_login_at for a tenant user of the scope.
func (r *GORMRepository) TouchLastLogin(ctx context.Context, scope store.Scope, id uuid.UUID, at time.Time) error {
	if err := tenantScopeOnly(scope); err != nil {
		return err
	}
	if id == uuid.Nil {
		return newValidationError(fieldError("id", "is required"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	result := db.Model(&User{}).
		Where("id = ? AND "+store.TenantColumn+" = ?", id, scope.TenantID).
		Update("last_login_at", at.UTC())
	if result.Error != nil {
		return fmt.Errorf("update last_login_at of user %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	return nil
}

// TouchPlatformLogin stamps last_login_at of a platform (super admin) account.
// The statement is guarded by tenant_id IS NULL, so a tenant row can never be
// touched through the platform path.
func (r *GORMRepository) TouchPlatformLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	if id == uuid.Nil {
		return newValidationError(fieldError("id", "is required"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	result := db.Model(&User{}).
		Where("id = ? AND "+store.TenantColumn+" IS NULL", id).
		Update("last_login_at", at.UTC())
	if result.Error != nil {
		return fmt.Errorf("update last_login_at of platform user %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: platform user %s", ErrNotFound, id)
	}
	return nil
}

// CreateSession opens a session row (the refresh token is already hashed).
func (r *GORMRepository) CreateSession(ctx context.Context, session *Session) error {
	if session == nil {
		return newValidationError(fieldError("session", "is required"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	session.Normalize()
	if err := session.Validate(); err != nil {
		return err
	}
	if err := db.Create(session).Error; err != nil {
		return mapWriteError("create session", err)
	}
	return nil
}

// LoadSession returns a session and its user by session id.
func (r *GORMRepository) LoadSession(ctx context.Context, id uuid.UUID) (*AuthenticatedSession, error) {
	if id == uuid.Nil {
		return nil, newValidationError(fieldError("session_id", "is required"))
	}
	var session Session
	return r.loadAuthenticatedSession(ctx, "id = ?", id, &session)
}

// FindSessionByRefreshDigest resolves a session by the digest of a refresh
// token. The token itself is never stored, so this is the only lookup that can
// validate it.
func (r *GORMRepository) FindSessionByRefreshDigest(ctx context.Context, digest TokenDigest) (*AuthenticatedSession, error) {
	if !digest.IsValid() {
		return nil, newValidationError(fieldError("refresh_token", "is not a valid token"))
	}
	var session Session
	return r.loadAuthenticatedSession(ctx, "refresh_token_hash = ?", string(digest), &session)
}

// loadAuthenticatedSession loads a session by one condition and then its user.
func (r *GORMRepository) loadAuthenticatedSession(ctx context.Context, where string, value any, session *Session) (*AuthenticatedSession, error) {
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	result := db.Model(&Session{}).Where(where, value).Take(session)
	switch {
	case errors.Is(result.Error, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("%w", ErrSessionNotFound)
	case result.Error != nil:
		return nil, fmt.Errorf("load session: %w", result.Error)
	}

	var user User
	userResult := db.Model(&User{}).Where("id = ?", session.UserID).Take(&user)
	switch {
	case errors.Is(userResult.Error, gorm.ErrRecordNotFound):
		// The account was deleted (soft delete) or the row is gone: the session
		// can no longer authenticate anything.
		return nil, fmt.Errorf("%w: account of session %s no longer exists", ErrUnauthenticated, session.ID)
	case userResult.Error != nil:
		return nil, fmt.Errorf("load user of session %s: %w", session.ID, userResult.Error)
	default:
		return &AuthenticatedSession{Session: session, User: &user}, nil
	}
}

// RotateSession replaces the refresh digest of a live session. The old digest
// stops working immediately, which is what makes a replayed refresh token fail.
func (r *GORMRepository) RotateSession(ctx context.Context, id uuid.UUID, digest TokenDigest, expiresAt, now time.Time) error {
	if !digest.IsValid() {
		return newValidationError(fieldError("refresh_token", "is not a valid token"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	result := db.Model(&Session{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{
			"refresh_token_hash": string(digest),
			"expires_at":         expiresAt.UTC(),
			"last_used_at":       now.UTC(),
		})
	if result.Error != nil {
		return mapWriteError("rotate session", result.Error)
	}
	if result.RowsAffected == 0 {
		// Unknown or already revoked: never disclose which.
		return fmt.Errorf("%w: session %s", ErrSessionNotFound, id)
	}
	return nil
}

// RevokeSession stamps revoked_at and revoked_reason. A session is revoked at
// most once; a second attempt reports ErrSessionNotFound like an unknown id.
func (r *GORMRepository) RevokeSession(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	if !validRevocationReason(&reason) {
		return newValidationError(fieldError("reason", "must be one of the documented revocation reasons"))
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}

	result := db.Model(&Session{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{
			"revoked_at":     at.UTC(),
			"revoked_reason": reason,
			"last_used_at":   at.UTC(),
		})
	if result.Error != nil {
		return mapWriteError("revoke session", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: session %s", ErrSessionNotFound, id)
	}
	return nil
}

// tenantScopeOnly guards the tenant isolation invariant of the auth repository:
// a tenant scoped call without a tenant scope is a programming error, never an
// unscoped query (FG2 convention).
func tenantScopeOnly(scope store.Scope) error {
	_, err := tenantScope(scope)
	return err
}
