package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/audit"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// fakeRepository is an in-memory Repository for the service tests. It mirrors
// the observable behaviour that matters here — not found, tenant scope, revoked
// and expired sessions — without PostgreSQL. The PostgreSQL implementation is
// covered by repository_integration_test.go.
type fakeRepository struct {
	mu       sync.Mutex
	users    []User
	sessions []Session

	// err injects a dependency failure into every call.
	err error
	// touchErr injects a failure into the last_login stamp only.
	touchErr error
}

var _ Repository = (*fakeRepository)(nil)

// seedUser stores a copy of user.
func (f *fakeRepository) seedUser(user *User) *User {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := *user
	f.users = append(f.users, stored)
	return &stored
}

// seedSession stores a copy of session.
func (f *fakeRepository) seedSession(session *Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, *session)
}

// failWith makes every following call fail with err.
func (f *fakeRepository) failWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// sessionCount returns the number of stored sessions.
func (f *fakeRepository) sessionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

// findSession returns a copy of the session with the given id.
func (f *fakeRepository) findSession(id uuid.UUID) (Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, session := range f.sessions {
		if session.ID == id {
			return session, true
		}
	}
	return Session{}, false
}

// mutateUser applies a change to a stored account (role change, disable, …).
func (f *fakeRepository) mutateUser(id uuid.UUID, mutate func(*User)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.users {
		if f.users[index].ID == id {
			mutate(&f.users[index])
			return
		}
	}
}

// expireSession moves the refresh expiry of a stored session into the past.
func (f *fakeRepository) expireSession(id uuid.UUID, expiry time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.sessions {
		if f.sessions[index].ID == id {
			f.sessions[index].ExpiresAt = expiry.UTC()
		}
	}
}

func (f *fakeRepository) fail() error { return f.err }

func (f *fakeRepository) FindUserByEmail(_ context.Context, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return nil, err
	}
	normalized := NormalizeEmail(email)
	for index := range f.users {
		if NormalizeEmail(f.users[index].Email) == normalized && !f.users[index].IsDeleted() {
			found := f.users[index]
			return &found, nil
		}
	}
	return nil, fmt.Errorf("%w: user %s", ErrNotFound, normalized)
}

func (f *fakeRepository) GetUser(_ context.Context, scope store.Scope, id uuid.UUID) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return nil, err
	}
	if !scope.HasTenant() {
		return nil, fmt.Errorf("%w: tenant scope is required", ErrMissingTenantScope)
	}
	for index := range f.users {
		user := f.users[index]
		if user.ID == id && user.TenantIDValue() == scope.TenantID {
			return &user, nil
		}
	}
	return nil, fmt.Errorf("%w: user %s", ErrNotFound, id)
}

func (f *fakeRepository) ListUsers(_ context.Context, scope store.Scope, filter ListFilter) ([]User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return nil, err
	}
	if !scope.HasTenant() {
		return nil, fmt.Errorf("%w: tenant scope is required", ErrMissingTenantScope)
	}

	out := make([]User, 0, len(f.users))
	for _, user := range f.users {
		if user.TenantIDValue() == scope.TenantID {
			out = append(out, user)
		}
	}
	return clampUsers(out, filter), nil
}

func (f *fakeRepository) ListPlatformUsers(_ context.Context, filter ListFilter) ([]User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return nil, err
	}
	return clampUsers(append([]User(nil), f.users...), filter), nil
}

// clampUsers applies the listing filter.
func clampUsers(users []User, filter ListFilter) []User {
	filter = filter.normalized()
	if filter.Offset >= len(users) {
		return []User{}
	}
	users = users[filter.Offset:]
	if len(users) > filter.Limit {
		users = users[:filter.Limit]
	}
	return users
}

func (f *fakeRepository) CreateUser(_ context.Context, scope store.Scope, user *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return err
	}
	if user == nil {
		return newValidationError(fieldError("user", "is required"))
	}
	if !user.Role.IsPlatform() {
		if !scope.HasTenant() {
			return fmt.Errorf("%w: tenant scope is required", ErrMissingTenantScope)
		}
		user.TenantID = cloneUUID(&scope.TenantID)
	}
	for _, existing := range f.users {
		if NormalizeEmail(existing.Email) == NormalizeEmail(user.Email) && !existing.IsDeleted() {
			return fmt.Errorf("%w: %s", ErrEmailTaken, user.Email)
		}
	}
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	user.Normalize()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	if err := user.Validate(); err != nil {
		return err
	}
	f.users = append(f.users, *user)
	return nil
}

func (f *fakeRepository) TouchLastLogin(_ context.Context, scope store.Scope, id uuid.UUID, at time.Time) error {
	return f.touch(id, at, func(user User) bool { return user.TenantIDValue() == scope.TenantID })
}

func (f *fakeRepository) TouchPlatformLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	return f.touch(id, at, func(user User) bool { return !user.HasTenant() })
}

func (f *fakeRepository) touch(id uuid.UUID, at time.Time, match func(User) bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return err
	}
	if f.touchErr != nil {
		return f.touchErr
	}
	for index := range f.users {
		if f.users[index].ID == id && match(f.users[index]) {
			stamp := at.UTC()
			f.users[index].LastLoginAt = &stamp
			return nil
		}
	}
	return fmt.Errorf("%w: user %s", ErrNotFound, id)
}

func (f *fakeRepository) CreateSession(_ context.Context, session *Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return err
	}
	if session == nil {
		return newValidationError(fieldError("session", "is required"))
	}
	session.Normalize()
	if err := session.Validate(); err != nil {
		return err
	}
	f.sessions = append(f.sessions, *session)
	return nil
}

func (f *fakeRepository) LoadSession(_ context.Context, id uuid.UUID) (*AuthenticatedSession, error) {
	return f.load(func(session Session) bool { return session.ID == id })
}

func (f *fakeRepository) FindSessionByRefreshDigest(_ context.Context, digest TokenDigest) (*AuthenticatedSession, error) {
	return f.load(func(session Session) bool { return session.RefreshTokenHash == string(digest) })
}

func (f *fakeRepository) load(match func(Session) bool) (*AuthenticatedSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return nil, err
	}
	for index := range f.sessions {
		if !match(f.sessions[index]) {
			continue
		}
		session := f.sessions[index]
		for userIndex := range f.users {
			if f.users[userIndex].ID == session.UserID {
				user := f.users[userIndex]
				return &AuthenticatedSession{Session: &session, User: &user}, nil
			}
		}
		return nil, fmt.Errorf("%w: account of session %s no longer exists", ErrUnauthenticated, session.ID)
	}
	return nil, fmt.Errorf("%w", ErrSessionNotFound)
}

func (f *fakeRepository) RotateSession(_ context.Context, id uuid.UUID, digest TokenDigest, expiresAt, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return err
	}
	for index := range f.sessions {
		session := &f.sessions[index]
		if session.ID != id || session.IsRevoked() {
			continue
		}
		session.RefreshTokenHash = string(digest)
		session.ExpiresAt = expiresAt.UTC()
		session.LastUsedAt = now.UTC()
		return nil
	}
	return fmt.Errorf("%w: session %s", ErrSessionNotFound, id)
}

func (f *fakeRepository) RevokeSession(_ context.Context, id uuid.UUID, reason string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail(); err != nil {
		return err
	}
	for index := range f.sessions {
		session := &f.sessions[index]
		if session.ID != id || session.IsRevoked() {
			continue
		}
		stamp := at.UTC()
		session.RevokedAt = &stamp
		session.RevokedReason = &reason
		return nil
	}
	return fmt.Errorf("%w: session %s", ErrSessionNotFound, id)
}

// recordingAuditor captures the audit entries a service wrote.
type recordingAuditor struct {
	mu      sync.Mutex
	entries []*audit.Entry
	err     error
}

var _ audit.Recorder = (*recordingAuditor)(nil)

func (r *recordingAuditor) Record(_ context.Context, entry *audit.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	copied := *entry
	r.entries = append(r.entries, &copied)
	return nil
}

// actions returns the recorded action names in order.
func (r *recordingAuditor) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.Action)
	}
	return out
}

// last returns the most recent entry.
func (r *recordingAuditor) last() *audit.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return nil
	}
	return r.entries[len(r.entries)-1]
}

// dump renders every recorded entry as JSON, so a test can scan the whole audit
// trail for leaked credentials.
func (r *recordingAuditor) dump(t *testing.T) string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := json.Marshal(r.entries)
	if err != nil {
		t.Fatalf("marshal audit entries: %v", err)
	}
	return string(raw)
}

// failWith makes every following Record call fail.
func (r *recordingAuditor) failWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}
