package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// newPgError builds the driver error PostgreSQL would return for a constraint
// violation, so the mapping can be asserted without a database.
func newPgError(code, constraint string) error {
	return &pgconn.PgError{
		Code:           code,
		ConstraintName: constraint,
		Message:        "constraint " + constraint + " violated",
		Severity:       "ERROR",
	}
}

func TestRepositoryWithoutDatabaseAnswersNotConfigured(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(nil)
	scope := TenantScope(uuid.New())

	if err := repository.Create(ctx, scope, &Store{Name: "Shop", Slug: "shop"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Create() = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.Get(ctx, scope, uuid.New()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Get() = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.GetBySlug(ctx, "shop"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetBySlug() = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.List(ctx, scope, ListFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("List() = %v, want ErrNotConfigured", err)
	}
	if err := repository.Update(ctx, scope, &Store{ID: uuid.New()}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Update() = %v, want ErrNotConfigured", err)
	}
	if err := repository.Delete(ctx, scope, uuid.New()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Delete() = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.SlugTaken(ctx, "shop", nil); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("SlugTaken() = %v, want ErrNotConfigured", err)
	}
}

func TestNilRepositoryIsSafe(t *testing.T) {
	var repository *GORMRepository

	err := repository.Create(context.Background(), TenantScope(uuid.New()), &Store{Name: "Shop", Slug: "shop"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil repository Create() = %v, want ErrNotConfigured", err)
	}
}

// TestRepositoryRequiresTenantScope proves the isolation guard runs before any
// query: a scope without a tenant can never reach the database.
func TestRepositoryRequiresTenantScope(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(nil)
	empty := Scope{}

	calls := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { return repository.Create(ctx, empty, &Store{Name: "Shop", Slug: "shop"}) }},
		{"Get", func() error {
			_, err := repository.Get(ctx, empty, uuid.New())
			return err
		}},
		{"List", func() error {
			_, err := repository.List(ctx, empty, ListFilter{})
			return err
		}},
		{"Update", func() error { return repository.Update(ctx, empty, &Store{ID: uuid.New()}) }},
		{"Delete", func() error { return repository.Delete(ctx, empty, uuid.New()) }},
	}

	for _, tc := range calls {
		if err := tc.call(); !errors.Is(err, ErrMissingTenantScope) {
			t.Errorf("%s(empty scope) = %v, want ErrMissingTenantScope", tc.name, err)
		}
	}

	// GetBySlug serves the public display route /s/{slug}: the slug itself is the
	// lookup key, so there is no tenant scope to enforce.
	if _, err := repository.GetBySlug(ctx, "shop"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetBySlug() = %v, want ErrNotConfigured", err)
	}
}

func TestListFilterNormalization(t *testing.T) {
	cases := []struct {
		name       string
		filter     ListFilter
		wantLimit  int
		wantOffset int
	}{
		{"defaults", ListFilter{}, DefaultListLimit, 0},
		{"explicit limit", ListFilter{Limit: 10, Offset: 5}, 10, 5},
		{"negative limit", ListFilter{Limit: -3}, DefaultListLimit, 0},
		{"limit above cap", ListFilter{Limit: MaxListLimit + 50}, MaxListLimit, 0},
		{"negative offset", ListFilter{Limit: 10, Offset: -1}, 10, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.filter.normalized()
			if got.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", got.Limit, tc.wantLimit)
			}
			if got.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", got.Offset, tc.wantOffset)
			}
		})
	}
}

func TestMapWriteErrorTranslatesConstraintViolations(t *testing.T) {
	cases := []struct {
		code       string
		constraint string
		want       error
	}{
		{pgCodeUniqueViolation, "stores_slug_active_key", ErrSlugTaken},
		{pgCodeUniqueViolation, "some_other_key", ErrConflict},
		{pgCodeForeignKeyViolation, "stores_tenant_id_fkey", ErrTenantNotFound},
		{pgCodeForeignKeyViolation, "audit_logs_store_id_fkey", ErrConflict},
		{pgCodeCheckViolation, "stores_slug_format", ErrValidation},
	}

	for _, tc := range cases {
		t.Run(tc.constraint, func(t *testing.T) {
			err := mapWriteError("create store", newPgError(tc.code, tc.constraint))
			if !errors.Is(err, tc.want) {
				t.Errorf("mapWriteError(%s, %s) = %v, want %v", tc.code, tc.constraint, err, tc.want)
			}
		})
	}

	plain := errors.New("connection reset")
	if mapped := mapWriteError("create store", plain); !errors.Is(mapped, plain) {
		t.Errorf("mapWriteError(plain error) = %v, want the original error", mapped)
	}
}
