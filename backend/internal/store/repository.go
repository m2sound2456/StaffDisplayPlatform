package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
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

// ListFilter narrows a tenant scoped store listing (FG5 admin list).
type ListFilter struct {
	// Status selects one lifecycle state; nil means every state.
	Status *Status
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

// Repository is the tenant scoped persistence contract for stores.
//
// Every method that reads or writes store data takes a Scope. There is no
// "get by id only" method on purpose: the tenant scope is part of the query, so
// a foreign store id can never be resolved (it answers ErrNotFound instead).
type Repository interface {
	// Create inserts a store inside scope.TenantID (the scope wins over any
	// tenant id carried by the model).
	Create(ctx context.Context, scope Scope, store *Store) error
	// Get returns one store of the scope, or ErrNotFound.
	Get(ctx context.Context, scope Scope, id uuid.UUID) (*Store, error)
	// GetBySlug resolves the live store behind the public display path
	// /s/{slug}; the slug is case-insensitive and globally unique.
	GetBySlug(ctx context.Context, slug string) (*Store, error)
	// List returns the stores of the scope, ordered deterministically.
	List(ctx context.Context, scope Scope, filter ListFilter) ([]Store, error)
	// Update writes the mutable fields of one store of the scope. A row outside
	// the scope is never modified and answers ErrNotFound.
	Update(ctx context.Context, scope Scope, store *Store) error
	// Delete soft deletes one store of the scope (row + audit trail survive).
	Delete(ctx context.Context, scope Scope, id uuid.UUID) error
	// SlugTaken reports whether a live store already uses the slug. exceptID
	// excludes one row (rename flows).
	SlugTaken(ctx context.Context, slug string, exceptID *uuid.UUID) (bool, error)
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

// Create inserts a new store.
func (r *GORMRepository) Create(ctx context.Context, scope Scope, store *Store) error {
	if err := ensureTenantScope(scope); err != nil {
		return err
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}
	if store == nil {
		return newValidationError(fieldError("store", "is required"))
	}

	// The scope is the only source of the tenant id.
	store.TenantID = scope.TenantID
	store.prepareForCreate()
	if err := store.Validate(); err != nil {
		return err
	}

	if err := db.Create(store).Error; err != nil {
		return mapWriteError("create store", err)
	}
	return nil
}

// Get loads one store inside the tenant scope.
func (r *GORMRepository) Get(ctx context.Context, scope Scope, id uuid.UUID) (*Store, error) {
	if err := ensureTenantScope(scope); err != nil {
		return nil, err
	}
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, newValidationError(fieldError("id", "is required"))
	}

	var found Store
	result := scope.Apply(db.Model(&Store{})).Where("id = ?", id).Take(&found)
	switch {
	case errors.Is(result.Error, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("%w: store %s", ErrNotFound, id)
	case result.Error != nil:
		return nil, fmt.Errorf("load store %s: %w", id, result.Error)
	default:
		return &found, nil
	}
}

// GetBySlug resolves the public display route /s/{slug}.
//
// Soft deleted stores are invisible (GORM adds deleted_at IS NULL) so a retired
// store stops serving its URL immediately. The status decision (active vs
// inactive/archived) belongs to display routing in FG5/FG10.
func (r *GORMRepository) GetBySlug(ctx context.Context, slug string) (*Store, error) {
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	normalized := strings.TrimSpace(slug)
	if normalized == "" {
		return nil, newValidationError(fieldError("slug", "is required"))
	}

	var found Store
	// slug is citext: the comparison is case-insensitive without lower(slug).
	result := db.Model(&Store{}).Where("slug = ?", normalized).Take(&found)
	switch {
	case errors.Is(result.Error, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("%w: store slug %q", ErrNotFound, normalized)
	case result.Error != nil:
		return nil, fmt.Errorf("load store by slug %q: %w", normalized, result.Error)
	default:
		return &found, nil
	}
}

// List returns the stores of one tenant.
func (r *GORMRepository) List(ctx context.Context, scope Scope, filter ListFilter) ([]Store, error) {
	if err := ensureTenantScope(scope); err != nil {
		return nil, err
	}
	db, err := r.session(ctx)
	if err != nil {
		return nil, err
	}

	filter = filter.normalized()
	query := scope.Apply(db.Model(&Store{}))
	if filter.Status != nil {
		query = query.Where("status = ?", string(*filter.Status))
	}

	stores := make([]Store, 0, filter.Limit)
	result := query.
		Order("name ASC, id ASC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&stores)
	if result.Error != nil {
		return nil, fmt.Errorf("list stores: %w", result.Error)
	}
	return stores, nil
}

// Update writes the mutable fields of a scoped store.
func (r *GORMRepository) Update(ctx context.Context, scope Scope, store *Store) error {
	if err := ensureTenantScope(scope); err != nil {
		return err
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}
	if store == nil {
		return newValidationError(fieldError("store", "is required"))
	}
	if store.ID == uuid.Nil {
		return newValidationError(fieldError("id", "is required"))
	}
	// A caller holding a store of another tenant must never be able to write it.
	if store.TenantID != uuid.Nil && store.TenantID != scope.TenantID {
		return fmt.Errorf("%w: store %s", ErrNotFound, store.ID)
	}

	store.TenantID = scope.TenantID
	store.Normalize()
	if err := store.Validate(); err != nil {
		return err
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"name":          store.Name,
		"slug":          store.Slug,
		"logo_url":      store.LogoURL,
		"status":        store.Status,
		"timezone":      store.Timezone,
		"opening_hours": store.OpeningHours,
		"updated_at":    now,
	}
	// tenant_id is part of the WHERE clause, so a foreign store id can never be
	// updated even if the payload claims this tenant.
	result := db.Model(&Store{}).
		Where("id = ? AND "+TenantColumn+" = ?", store.ID, scope.TenantID).
		Updates(updates)
	if result.Error != nil {
		return mapWriteError("update store", result.Error)
	}
	if result.RowsAffected == 0 {
		// Unknown id, foreign store or already deleted: never disclose which.
		return fmt.Errorf("%w: store %s", ErrNotFound, store.ID)
	}

	store.UpdatedAt = now
	return nil
}

// Delete soft deletes a scoped store.
func (r *GORMRepository) Delete(ctx context.Context, scope Scope, id uuid.UUID) error {
	if err := ensureTenantScope(scope); err != nil {
		return err
	}
	db, err := r.session(ctx)
	if err != nil {
		return err
	}
	if id == uuid.Nil {
		return newValidationError(fieldError("id", "is required"))
	}

	result := db.Where("id = ? AND "+TenantColumn+" = ?", id, scope.TenantID).Delete(&Store{})
	if result.Error != nil {
		return fmt.Errorf("delete store %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: store %s", ErrNotFound, id)
	}
	return nil
}

// SlugTaken reports whether a live store already owns the slug.
func (r *GORMRepository) SlugTaken(ctx context.Context, slug string, exceptID *uuid.UUID) (bool, error) {
	db, err := r.session(ctx)
	if err != nil {
		return false, err
	}

	normalized := NormalizeSlug(slug)
	if normalized == "" {
		return false, newValidationError(fieldError("slug", "is required"))
	}

	query := db.Model(&Store{}).Where("slug = ?", normalized)
	if exceptID != nil && *exceptID != uuid.Nil {
		query = query.Where("id <> ?", *exceptID)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("count stores by slug %q: %w", normalized, err)
	}
	return count > 0, nil
}

// mapWriteError translates PostgreSQL constraint violations into domain errors
// so a duplicate slug becomes ErrSlugTaken (HTTP 409) and an unknown tenant
// becomes ErrTenantNotFound instead of a driver string.
func mapWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	switch pgErr.Code {
	case pgCodeUniqueViolation:
		if strings.Contains(pgErr.ConstraintName, "slug") {
			return fmt.Errorf("%w: %s", ErrSlugTaken, pgErr.ConstraintName)
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
