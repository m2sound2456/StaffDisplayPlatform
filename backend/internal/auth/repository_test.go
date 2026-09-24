package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

func TestRepositoryWithoutDatabaseAnswersNotConfigured(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(nil)
	scope := store.TenantScope(uuid.New())

	if _, err := repository.FindUserByEmail(ctx, "admin@example.com"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("FindUserByEmail() error = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.GetUser(ctx, scope, uuid.New()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetUser() error = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.ListUsers(ctx, scope, ListFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ListUsers() error = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.ListPlatformUsers(ctx, ListFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ListPlatformUsers() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.CreateUser(ctx, scope, &User{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("CreateUser() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.TouchLastLogin(ctx, scope, uuid.New(), time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("TouchLastLogin() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.TouchPlatformLogin(ctx, uuid.New(), time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("TouchPlatformLogin() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.CreateSession(ctx, &Session{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("CreateSession() error = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.LoadSession(ctx, uuid.New()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("LoadSession() error = %v, want ErrNotConfigured", err)
	}
	if _, err := repository.FindSessionByRefreshDigest(ctx, TokenDigest(DigestToken("token"))); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("FindSessionByRefreshDigest() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.RotateSession(ctx, uuid.New(), TokenDigest(DigestToken("token")), time.Now(), time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("RotateSession() error = %v, want ErrNotConfigured", err)
	}
	if err := repository.RevokeSession(ctx, uuid.New(), RevocationLogout, time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("RevokeSession() error = %v, want ErrNotConfigured", err)
	}
}

func TestNilRepositoryIsSafe(t *testing.T) {
	var repository *GORMRepository

	if _, err := repository.ListPlatformUsers(context.Background(), ListFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil repository ListPlatformUsers() error = %v, want ErrNotConfigured", err)
	}
}

func TestRepositoryRequiresTenantScope(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(nil)
	scope := store.TenantScope(uuid.New())

	// The scope guard runs before the (missing) database: a scope problem is a
	// programming error, not a connection problem.
	if _, err := repository.GetUser(ctx, store.Scope{}, uuid.New()); !errors.Is(err, ErrMissingTenantScope) {
		t.Errorf("GetUser() error = %v, want ErrMissingTenantScope", err)
	}
	if _, err := repository.ListUsers(ctx, store.Scope{}, ListFilter{}); !errors.Is(err, ErrMissingTenantScope) {
		t.Errorf("ListUsers() error = %v, want ErrMissingTenantScope", err)
	}
	if err := repository.TouchLastLogin(ctx, store.Scope{}, uuid.New(), time.Now()); !errors.Is(err, ErrMissingTenantScope) {
		t.Errorf("TouchLastLogin() error = %v, want ErrMissingTenantScope", err)
	}
	if err := repository.CreateUser(ctx, store.Scope{}, &User{Role: RoleStoreAdmin}); !errors.Is(err, ErrMissingTenantScope) {
		t.Errorf("CreateUser() error = %v, want ErrMissingTenantScope", err)
	}

	// A valid scope reaches the database check instead.
	if _, err := repository.GetUser(ctx, scope, uuid.New()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetUser() with a scope error = %v, want ErrNotConfigured", err)
	}
}

func TestListFilterNormalization(t *testing.T) {
	cases := []struct {
		name   string
		filter ListFilter
		want   ListFilter
	}{
		{"defaults", ListFilter{}, ListFilter{Limit: DefaultListLimit}},
		{"negative offset", ListFilter{Limit: 10, Offset: -5}, ListFilter{Limit: 10}},
		{"limit capped", ListFilter{Limit: MaxListLimit + 1}, ListFilter{Limit: MaxListLimit}},
		{"kept", ListFilter{Limit: 10, Offset: 20}, ListFilter{Limit: 10, Offset: 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.normalized(); got != tc.want {
				t.Errorf("normalized() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRepositoryValidatesItsInput(t *testing.T) {
	ctx := context.Background()
	repository := NewRepository(nil)
	scope := store.TenantScope(uuid.New())

	if _, err := repository.FindUserByEmail(ctx, "  "); !errors.Is(err, ErrValidation) {
		t.Errorf("FindUserByEmail(blank) error = %v, want ErrValidation", err)
	}
	if _, err := repository.GetUser(ctx, scope, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("GetUser(nil id) error = %v, want ErrValidation", err)
	}
	if err := repository.CreateUser(ctx, scope, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("CreateUser(nil) error = %v, want ErrValidation", err)
	}
	if err := repository.CreateSession(ctx, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("CreateSession(nil) error = %v, want ErrValidation", err)
	}
	if _, err := repository.LoadSession(ctx, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("LoadSession(nil id) error = %v, want ErrValidation", err)
	}
	if _, err := repository.FindSessionByRefreshDigest(ctx, TokenDigest("nope")); !errors.Is(err, ErrValidation) {
		t.Errorf("FindSessionByRefreshDigest(malformed) error = %v, want ErrValidation", err)
	}
	if err := repository.RotateSession(ctx, uuid.New(), TokenDigest("nope"), time.Now(), time.Now()); !errors.Is(err, ErrValidation) {
		t.Errorf("RotateSession(malformed digest) error = %v, want ErrValidation", err)
	}
	if err := repository.RevokeSession(ctx, uuid.New(), "timeout", time.Now()); !errors.Is(err, ErrValidation) {
		t.Errorf("RevokeSession(unknown reason) error = %v, want ErrValidation", err)
	}
}

func TestReasonOfVocabulary(t *testing.T) {
	cases := map[error]string{
		ErrTokenMalformed:       ReasonMalformed,
		ErrTokenSignature:       ReasonSignature,
		ErrTokenExpired:         ReasonExpired,
		ErrSessionExpired:       ReasonExpired,
		ErrTokenNotYetValid:     ReasonNotYetValid,
		ErrTokenInvalid:         ReasonClaims,
		ErrSessionNotFound:      ReasonSessionMissing,
		ErrSessionRevoked:       ReasonSessionRevoked,
		ErrAccountInactive:      ReasonAccountInactive,
		ErrUnauthenticated:      ReasonMissing,
		ErrInvalidCredentials:   ReasonMissing,
		errors.New("something"): ReasonInvalid,
	}
	for err, want := range cases {
		if got := ReasonOf(err); got != want {
			t.Errorf("ReasonOf(%v) = %q, want %q", err, got, want)
		}
	}
	if got := ReasonOf(nil); got != "" {
		t.Errorf("ReasonOf(nil) = %q, want an empty reason", got)
	}
}
