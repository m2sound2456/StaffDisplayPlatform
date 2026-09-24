package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// Server level auth test constants (obviously fake values).
const (
	testJWTCurrentSecret  = "server-test-current-secret-0123456789abcdef"
	testJWTPreviousSecret = "server-test-previous-secret-0123456789abcdef"
	testJWTUnknownSecret  = "server-test-unknown-secret-0123456789abcdef"
	testUserPassword      = "correct-horse-battery-staple"
	testAccessTokenTTL    = 15 * time.Minute
	testRefreshTokenTTL   = 30 * 24 * time.Hour
)

// testClock is a controllable clock for the HTTP layer.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newServerTestClock() *testClock {
	return &testClock{now: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.UTC()
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testPasswordHash caches one bcrypt hash for the HTTP tests.
var testPasswordHash = sync.OnceValue(func() string {
	hash, err := auth.HashPassword(testUserPassword)
	if err != nil {
		panic(err)
	}
	return hash
})

// newTestAuthUser builds an account for the HTTP tests.
func newTestAuthUser(t *testing.T, tenantID, storeID uuid.UUID, email string, role auth.Role) *auth.User {
	t.Helper()

	user := &auth.User{
		ID:           uuid.New(),
		Email:        email,
		DisplayName:  strings.Split(email, "@")[0],
		PasswordHash: testPasswordHash(),
		Role:         role,
		Status:       auth.StatusActive,
		CreatedAt:    time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
	}
	if tenantID != uuid.Nil {
		user.TenantID = &tenantID
	}
	if storeID != uuid.Nil {
		user.StoreID = &storeID
	}
	return user
}

// authTestRepository is a minimal in-memory auth.Repository for the HTTP tests:
// the PostgreSQL implementation is covered by the internal/auth integration
// tests, this one only has to answer the handler and middleware questions.
type authTestRepository struct {
	mu        sync.Mutex
	users     []*auth.User
	sessions  []*auth.Session
	nextError error
}

var _ auth.Repository = (*authTestRepository)(nil)

func (r *authTestRepository) seed(user *auth.User) *auth.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *user
	r.users = append(r.users, &copied)
	return &copied
}

func (r *authTestRepository) failWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextError = err
}

// mutate changes a stored account (role change, disable, …).
func (r *authTestRepository) mutate(id uuid.UUID, change func(*auth.User)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.ID == id {
			change(user)
			return
		}
	}
}

func (r *authTestRepository) FindUserByEmail(_ context.Context, email string) (*auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return nil, r.nextError
	}
	wanted := auth.NormalizeEmail(email)
	for _, user := range r.users {
		if auth.NormalizeEmail(user.Email) == wanted && !user.DeletedAt.Valid {
			copied := *user
			return &copied, nil
		}
	}
	return nil, fmt.Errorf("%w: user %s", auth.ErrNotFound, wanted)
}

func (r *authTestRepository) GetUser(_ context.Context, scope store.Scope, id uuid.UUID) (*auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return nil, r.nextError
	}
	if !scope.HasTenant() {
		return nil, fmt.Errorf("%w: tenant scope is required", auth.ErrMissingTenantScope)
	}
	for _, user := range r.users {
		if user.ID == id && user.TenantIDValue() == scope.TenantID {
			copied := *user
			return &copied, nil
		}
	}
	return nil, fmt.Errorf("%w: user %s", auth.ErrNotFound, id)
}

func (r *authTestRepository) ListUsers(_ context.Context, scope store.Scope, _ auth.ListFilter) ([]auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return nil, r.nextError
	}
	if !scope.HasTenant() {
		return nil, fmt.Errorf("%w: tenant scope is required", auth.ErrMissingTenantScope)
	}
	out := make([]auth.User, 0, len(r.users))
	for _, user := range r.users {
		if user.TenantIDValue() == scope.TenantID {
			out = append(out, *user)
		}
	}
	return out, nil
}

func (r *authTestRepository) ListPlatformUsers(_ context.Context, _ auth.ListFilter) ([]auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return nil, r.nextError
	}
	out := make([]auth.User, 0, len(r.users))
	for _, user := range r.users {
		out = append(out, *user)
	}
	return out, nil
}

func (r *authTestRepository) CreateUser(_ context.Context, scope store.Scope, user *auth.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return r.nextError
	}
	if !user.Role.IsPlatform() {
		if !scope.HasTenant() {
			return fmt.Errorf("%w: tenant scope is required", auth.ErrMissingTenantScope)
		}
		tenant := scope.TenantID
		user.TenantID = &tenant
	}
	r.users = append(r.users, user)
	return nil
}

func (r *authTestRepository) TouchLastLogin(_ context.Context, scope store.Scope, id uuid.UUID, at time.Time) error {
	return r.touch(id, at, func(user *auth.User) bool { return user.TenantIDValue() == scope.TenantID })
}

func (r *authTestRepository) TouchPlatformLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	return r.touch(id, at, func(user *auth.User) bool { return !user.HasTenant() })
}

func (r *authTestRepository) touch(id uuid.UUID, at time.Time, match func(*auth.User) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return r.nextError
	}
	for _, user := range r.users {
		if user.ID == id && match(user) {
			stamp := at.UTC()
			user.LastLoginAt = &stamp
			return nil
		}
	}
	return fmt.Errorf("%w: user %s", auth.ErrNotFound, id)
}

func (r *authTestRepository) CreateSession(_ context.Context, session *auth.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return r.nextError
	}
	copied := *session
	r.sessions = append(r.sessions, &copied)
	return nil
}

func (r *authTestRepository) LoadSession(_ context.Context, id uuid.UUID) (*auth.AuthenticatedSession, error) {
	return r.load(func(session *auth.Session) bool { return session.ID == id })
}

func (r *authTestRepository) FindSessionByRefreshDigest(_ context.Context, digest auth.TokenDigest) (*auth.AuthenticatedSession, error) {
	return r.load(func(session *auth.Session) bool { return session.RefreshTokenHash == string(digest) })
}

func (r *authTestRepository) load(match func(*auth.Session) bool) (*auth.AuthenticatedSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return nil, r.nextError
	}
	for _, session := range r.sessions {
		if !match(session) {
			continue
		}
		for _, user := range r.users {
			if user.ID == session.UserID {
				sessionCopy, userCopy := *session, *user
				return &auth.AuthenticatedSession{Session: &sessionCopy, User: &userCopy}, nil
			}
		}
		return nil, fmt.Errorf("%w: account of session %s no longer exists", auth.ErrUnauthenticated, session.ID)
	}
	return nil, fmt.Errorf("%w", auth.ErrSessionNotFound)
}

func (r *authTestRepository) RotateSession(_ context.Context, id uuid.UUID, digest auth.TokenDigest, expiresAt, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return r.nextError
	}
	for _, session := range r.sessions {
		if session.ID == id && !session.IsRevoked() {
			session.RefreshTokenHash = string(digest)
			session.ExpiresAt = expiresAt.UTC()
			session.LastUsedAt = now.UTC()
			return nil
		}
	}
	return fmt.Errorf("%w: session %s", auth.ErrSessionNotFound, id)
}

func (r *authTestRepository) RevokeSession(_ context.Context, id uuid.UUID, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nextError != nil {
		return r.nextError
	}
	for _, session := range r.sessions {
		if session.ID == id && !session.IsRevoked() {
			stamp := at.UTC()
			session.RevokedAt = &stamp
			session.RevokedReason = &reason
			return nil
		}
	}
	return fmt.Errorf("%w: session %s", auth.ErrSessionNotFound, id)
}

// newAuthTestService builds an auth service on the fake repository.
func newAuthTestService(t *testing.T, repository auth.Repository, clock *testClock, previousSecrets ...string) *auth.Service {
	t.Helper()

	signer, err := auth.NewSigner(auth.SignerOptions{
		Secret:          testJWTCurrentSecret,
		PreviousSecrets: previousSecrets,
		AccessTokenTTL:  testAccessTokenTTL,
		Now:             clock.Now,
	})
	if err != nil {
		t.Fatalf("auth.NewSigner() error = %v", err)
	}
	return auth.NewService(auth.ServiceOptions{
		Repository:      repository,
		Signer:          signer,
		RefreshTokenTTL: testRefreshTokenTTL,
		Now:             clock.Now,
	})
}

// newAuthTestRouter builds the real router around the fake auth dependencies, so
// the HTTP surface (routes, middleware, envelopes) is exercised end to end
// without PostgreSQL.
func newAuthTestRouter(t *testing.T, repository auth.Repository, clock *testClock, previousSecrets ...string) *gin.Engine {
	t.Helper()

	cfg := testConfig()
	service := newAuthTestService(t, repository, clock, previousSecrets...)
	return newRouter(cfg, newHandlersWithAuth(cfg, nil, service))
}

// errTestDatabaseDown stands in for an unreachable database.
var errTestDatabaseDown = errors.New("test: database is unreachable")

// signTestToken signs an access token for a session with an explicit key, which
// is how a test produces a credential the production signer would not issue. The
// claims are relative to the service clock, so the token is valid for the same
// instant the verifier uses.
func signTestToken(t *testing.T, clock *testClock, secret, sessionID string, user *auth.User) string {
	t.Helper()

	now := clock.Now()
	return signTestTokenWithClaims(t, secret, auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(testAccessTokenTTL)),
		},
		SessionID: sessionID,
		Role:      user.Role,
	})
}

// signTestTokenWithClaims signs explicitly built claims.
func signTestTokenWithClaims(t *testing.T, secret string, claims auth.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return token
}

// signExpiredTestToken signs an access token that expired an hour ago (relative
// to the service clock).
func signExpiredTestToken(t *testing.T, clock *testClock, sessionID string, user *auth.User) string {
	t.Helper()

	now := clock.Now()
	return signTestTokenWithClaims(t, testJWTCurrentSecret, auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
		SessionID: sessionID,
		Role:      user.Role,
	})
}
