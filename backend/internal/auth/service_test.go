package auth

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/m2sound2456/staffdisplay/backend/internal/audit"
)

// testRefreshTTL is the refresh lifetime used by the service tests.
const testRefreshTTL = 30 * 24 * time.Hour

// testClock is a controllable clock, so a test can advance time between two
// calls of the service (token rotation, sliding expiry) deterministically.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

// newTestClock starts a clock at the fixed reference time.
func newTestClock() *testClock { return &testClock{now: fixedNow} }

// Now returns the current instant (UTC).
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.UTC()
}

// advance moves the clock forward.
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newTestService wires a service on the given repository with a frozen clock.
func newTestService(t *testing.T, repository Repository, auditor audit.Recorder, previous ...string) *Service {
	t.Helper()

	return newTestServiceWithClock(t, repository, auditor, newTestClock(), previous...)
}

// newTestServiceWithClock wires a service on an explicit clock.
func newTestServiceWithClock(t *testing.T, repository Repository, auditor audit.Recorder, clock *testClock, previous ...string) *Service {
	t.Helper()

	signer, err := NewSigner(SignerOptions{
		Secret:          testCurrentSecret,
		PreviousSecrets: previous,
		AccessTokenTTL:  15 * time.Minute,
		Now:             clock.Now,
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	return NewService(ServiceOptions{
		Repository:      repository,
		Audit:           auditor,
		Signer:          signer,
		RefreshTokenTTL: testRefreshTTL,
		Now:             clock.Now,
	})
}

// loginInput builds a login attempt for an address.
func loginInput(email, password string) LoginInput {
	return LoginInput{
		Email:     email,
		Password:  password,
		UserAgent: "Mozilla/5.0 (test)",
		IP:        "203.0.113.7",
	}
}

// metadataValue reads one string value of an audit entry metadata object.
func metadataValue(t *testing.T, entry *audit.Entry, key string) string {
	t.Helper()

	if entry == nil {
		t.Fatal("no audit entry recorded")
	}
	var values map[string]string
	if err := json.Unmarshal(entry.Metadata, &values); err != nil {
		t.Fatalf("audit metadata is not a JSON object: %v", err)
	}
	return values[key]
}

// auditEntryFor returns the most recent entry with the given action.
func auditEntryFor(t *testing.T, auditor *recordingAuditor, action string) *audit.Entry {
	t.Helper()

	var found *audit.Entry
	for _, entry := range auditor.entries {
		if entry.Action == action {
			found = entry
		}
	}
	if found == nil {
		t.Fatalf("no %q entry recorded (got %v)", action, auditor.actions())
	}
	return found
}

func TestServiceRequiresItsDependencies(t *testing.T) {
	ctx := context.Background()
	service := NewService(ServiceOptions{})

	if _, err := service.Login(ctx, loginInput("a@example.com", strongPassword)); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Login() error = %v, want ErrNotConfigured", err)
	}
	if _, err := service.Refresh(ctx, RefreshInput{RefreshToken: "token"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Refresh() error = %v, want ErrNotConfigured", err)
	}
	if _, err := service.Authenticate(ctx, "token"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Authenticate() error = %v, want ErrNotConfigured", err)
	}
	if err := service.Logout(ctx, &Principal{}, ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Logout() error = %v, want ErrNotConfigured", err)
	}
	if _, err := service.ListUsers(ctx, &Principal{}, ListFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ListUsers() error = %v, want ErrNotConfigured", err)
	}
}

func TestServiceLoginSucceedsAndAudits(t *testing.T) {
	ctx := context.Background()
	tenantID, storeID := uuid.New(), uuid.New()
	user := newTestUser(t, tenantID, storeID, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	result, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if result.User.ID != user.ID {
		t.Errorf("user = %s, want %s", result.User.ID, user.ID)
	}
	if result.Tokens.TokenType != TokenTypeBearer {
		t.Errorf("token type = %q, want %q", result.Tokens.TokenType, TokenTypeBearer)
	}
	if result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" {
		t.Fatal("Login() must return an access token and a refresh token")
	}
	if want := fixedNow.Add(15 * time.Minute); !result.Tokens.AccessExpiresAt.Equal(want) {
		t.Errorf("access expiry = %s, want %s", result.Tokens.AccessExpiresAt, want)
	}
	if want := fixedNow.Add(testRefreshTTL); !result.Tokens.RefreshExpiresAt.Equal(want) {
		t.Errorf("refresh expiry = %s, want %s", result.Tokens.RefreshExpiresAt, want)
	}

	// The refresh token is stored hashed only and the session snapshots the
	// scope of the account.
	if repository.sessionCount() != 1 {
		t.Fatalf("sessions = %d, want 1", repository.sessionCount())
	}
	session, ok := repository.findSession(result.Session.ID)
	if !ok {
		t.Fatal("the session of the login must exist")
	}
	if session.RefreshTokenHash == result.Tokens.RefreshToken {
		t.Error("the refresh token must never be stored in plaintext")
	}
	if !TokenDigest(session.RefreshTokenHash).Matches(result.Tokens.RefreshToken) {
		t.Error("the stored digest must match the issued refresh token")
	}
	if agent := pointerValue(session.UserAgent); !strings.Contains(agent, "Mozilla") {
		t.Errorf("user agent = %q, want the client user agent", agent)
	}
	if session.TenantIDValue() != tenantID || session.StoreIDValue() != storeID {
		t.Error("the session must snapshot the tenant and store of the account")
	}

	// The access token authenticates the same identity and scope.
	principal, err := service.Authenticate(ctx, result.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.UserID() != user.ID || principal.TenantID != tenantID || principal.StoreID != storeID {
		t.Error("the token must resolve to the account and scope of the login")
	}
	if principal.KeySource != KeySourceCurrent {
		t.Errorf("KeySource = %q, want %q", principal.KeySource, KeySourceCurrent)
	}
	scope, err := principal.Scope()
	if err != nil {
		t.Fatalf("Scope() error = %v", err)
	}
	if scope.TenantID != tenantID || scope.StoreID != storeID {
		t.Errorf("scope = %v, want tenant %s / store %s", scope, tenantID, storeID)
	}
	if principal.IsPlatformAdmin() || !principal.HasRole(RoleStoreAdmin) {
		t.Error("a store admin must not be reported as a platform account")
	}

	// last_login_at is stamped in the response and in the database.
	if result.User.LastLoginAt == nil || !result.User.LastLoginAt.Equal(fixedNow) {
		t.Errorf("login response last_login_at = %v, want %v", result.User.LastLoginAt, fixedNow)
	}
	loginAt := ""
	repository.mutateUser(user.ID, func(u *User) {
		if u.LastLoginAt != nil {
			loginAt = u.LastLoginAt.UTC().Format(time.RFC3339)
		}
	})
	if loginAt != fixedNow.Format(time.RFC3339) {
		t.Errorf("last_login_at = %q, want %q", loginAt, fixedNow.Format(time.RFC3339))
	}

	// Exactly one audit entry, with the actor and tenant of the account.
	if actions := auditor.actions(); len(actions) != 1 || actions[0] != ActionLoginSucceeded {
		t.Fatalf("audit actions = %v, want [%s]", actions, ActionLoginSucceeded)
	}
	entry := auditor.last()
	if entry.ActorType != audit.ActorUser || entry.ActorID == nil || *entry.ActorID != user.ID {
		t.Error("the audit entry must name the account as the actor")
	}
	if entry.TenantID == nil || *entry.TenantID != tenantID {
		t.Error("the audit entry must carry the tenant of the account")
	}
	if entry.IP == nil || *entry.IP != "203.0.113.7" {
		t.Errorf("audit ip = %v, want 203.0.113.7", entry.IP)
	}
}

func TestServiceLoginRejectsAWrongPassword(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	_, err := service.Login(ctx, loginInput(user.Email, "wrong-password-1234"))
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
	if repository.sessionCount() != 0 {
		t.Error("a failed login must not open a session")
	}
	if actions := auditor.actions(); len(actions) != 1 || actions[0] != ActionLoginFailed {
		t.Fatalf("audit actions = %v, want [%s]", actions, ActionLoginFailed)
	}
	if reason := metadataValue(t, auditor.last(), MetadataKeyReason); reason != ReasonMissing {
		t.Errorf("audit reason = %q, want %q", reason, ReasonMissing)
	}
	if email := metadataValue(t, auditor.last(), MetadataKeyEmail); email != user.Email {
		t.Errorf("audit email = %q, want %q", email, user.Email)
	}
}

func TestServiceLoginRejectsAnUnknownAccountWithoutDisclosingIt(t *testing.T) {
	ctx := context.Background()
	repository := &fakeRepository{}
	repository.seedUser(newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin))
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	_, err := service.Login(ctx, loginInput("nobody@example.com", strongPassword))
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials (the same error as a wrong password)", err)
	}
	if repository.sessionCount() != 0 {
		t.Error("an unknown account must not open a session")
	}

	entry := auditor.last()
	if entry == nil || entry.Action != ActionLoginFailed {
		t.Fatalf("audit = %v, want [%s]", auditor.actions(), ActionLoginFailed)
	}
	// An unknown address has no actor_id, so the entry is a system entry.
	if entry.ActorType != audit.ActorSystem || entry.ActorID != nil {
		t.Error("an unknown account must be audited as a system event without an actor id")
	}
}

func TestServiceLoginRejectsAnInactiveAccount(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	user.Status = StatusDisabled
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	_, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("Login() error = %v, want ErrAccountInactive", err)
	}
	if repository.sessionCount() != 0 {
		t.Error("a disabled account must not open a session")
	}
	if reason := metadataValue(t, auditor.last(), MetadataKeyReason); reason != ReasonAccountInactive {
		t.Errorf("audit reason = %q, want %q", reason, ReasonAccountInactive)
	}
}

func TestServiceLoginValidatesThePayload(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, &fakeRepository{}, nil)

	_, err := service.Login(ctx, LoginInput{Email: "  ", Password: ""})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Login() error = %v, want ErrValidation", err)
	}
	var detailed *ValidationError
	if !errors.As(err, &detailed) {
		t.Fatal("the validation error must expose its fields")
	}
	fields := detailed.FieldMessages()
	if fields["email"] == "" || fields["password"] == "" {
		t.Errorf("field details = %v, want email and password", fields)
	}
}

func TestServiceLoginSurvivesAnAuditOrStampFailure(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	repository.touchErr = errors.New("database is unreachable")
	auditor := &recordingAuditor{}
	auditor.failWith(errors.New("audit table missing"))
	service := newTestService(t, repository, auditor)

	result, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v, want the login to succeed", err)
	}
	if result.Tokens.AccessToken == "" {
		t.Error("Login() must still return tokens")
	}
}

func TestServiceRefreshRotatesTheRefreshToken(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.New(), RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	clock := newTestClock()
	service := newTestServiceWithClock(t, repository, auditor, clock)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// One minute later the client rotates its refresh token.
	clock.advance(time.Minute)
	refreshed, err := service.Refresh(ctx, RefreshInput{
		RefreshToken: login.Tokens.RefreshToken,
		UserAgent:    "Mozilla/5.0 (test)",
		IP:           "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if refreshed.Tokens.RefreshToken == login.Tokens.RefreshToken {
		t.Error("Refresh() must rotate the refresh token")
	}
	if refreshed.Tokens.AccessToken == login.Tokens.AccessToken {
		t.Error("Refresh() must issue a new access token")
	}
	if want := clock.Now().Add(15 * time.Minute); !refreshed.Tokens.AccessExpiresAt.Equal(want) {
		t.Errorf("access expiry = %s, want %s", refreshed.Tokens.AccessExpiresAt, want)
	}
	// Sliding refresh window: an active client keeps its session alive.
	if want := clock.Now().Add(testRefreshTTL); !refreshed.Tokens.RefreshExpiresAt.Equal(want) {
		t.Errorf("refresh expiry = %s, want %s", refreshed.Tokens.RefreshExpiresAt, want)
	}
	if refreshed.Session.ID != login.Session.ID {
		t.Error("a rotation must keep the session identity (the access token stays valid)")
	}
	if repository.sessionCount() != 1 {
		t.Errorf("sessions = %d, want 1 (a rotation never creates a second session)", repository.sessionCount())
	}

	// The old refresh token is dead, the new one works.
	if _, err := service.Refresh(ctx, RefreshInput{RefreshToken: login.Tokens.RefreshToken}); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("replaying the rotated-away token: error = %v, want ErrUnauthenticated", err)
	}
	if _, err := service.Refresh(ctx, RefreshInput{RefreshToken: refreshed.Tokens.RefreshToken}); err != nil {
		t.Fatalf("Refresh() with the new token error = %v", err)
	}

	// The old access token still authenticates: it stays valid until it expires
	// (15 minutes), which is the documented behaviour of a rotation.
	if _, err := service.Authenticate(ctx, login.Tokens.AccessToken); err != nil {
		t.Fatalf("Authenticate() with the previous access token error = %v", err)
	}

	if actions := auditor.actions(); !contains(actions, ActionTokenRefreshed) || !contains(actions, ActionRefreshRejected) {
		t.Errorf("audit actions = %v, want a token_refreshed and a refresh_rejected entry", actions)
	}
}

func TestServiceRefreshRejectsUnknownRevokedAndExpiredSessions(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		token   func(*SessionResult) string
		prepare func(*fakeRepository, *Session)
		reason  string
	}{
		{
			name:    "unknown token",
			token:   func(*SessionResult) string { return "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo" },
			prepare: func(*fakeRepository, *Session) {},
			reason:  ReasonSessionMissing,
		},
		{
			name:  "revoked session",
			token: func(login *SessionResult) string { return login.Tokens.RefreshToken },
			prepare: func(repo *fakeRepository, session *Session) {
				_ = repo.RevokeSession(ctx, session.ID, RevocationLogout, fixedNow)
			},
			reason: ReasonSessionRevoked,
		},
		{
			name:  "expired session",
			token: func(login *SessionResult) string { return login.Tokens.RefreshToken },
			prepare: func(repo *fakeRepository, session *Session) {
				repo.expireSession(session.ID, fixedNow.Add(-time.Minute))
			},
			reason: ReasonExpired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
			repository := &fakeRepository{}
			repository.seedUser(user)
			auditor := &recordingAuditor{}
			service := newTestService(t, repository, auditor)

			login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
			if err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			tc.prepare(repository, login.Session)

			_, err = service.Refresh(ctx, RefreshInput{RefreshToken: tc.token(login)})
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Refresh() error = %v, want ErrUnauthenticated", err)
			}
			if reason := metadataValue(t, auditor.last(), MetadataKeyReason); reason != tc.reason {
				t.Errorf("audit reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

func TestServiceRefreshRejectsAnInactiveAccount(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	repository.mutateUser(user.ID, func(u *User) { u.Status = StatusDisabled })

	if _, err := service.Refresh(ctx, RefreshInput{RefreshToken: login.Tokens.RefreshToken}); !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("Refresh() error = %v, want ErrAccountInactive", err)
	}
	if reason := metadataValue(t, auditor.last(), MetadataKeyReason); reason != ReasonAccountInactive {
		t.Errorf("audit reason = %q, want %q", reason, ReasonAccountInactive)
	}
}

// contains reports whether values holds want.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestServiceAuthenticateRejectsUnusableCredentials(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	user := newTestUser(t, tenantID, uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	service := newTestService(t, repository, nil, testPreviousSecret)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	otherUser := newTestUser(t, tenantID, uuid.Nil, RoleStoreAdmin)

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"empty", "", ErrUnauthenticated},
		{"garbage", "not-a-token", ErrTokenMalformed},
		{"foreign signature", signClaimsWithSecret(t, testUnknownSecret, validRegisteredClaims(user.ID), Claims{SessionID: login.Session.ID.String(), Role: user.Role}), ErrTokenSignature},
		{"unknown session", signClaimsWithSecret(t, testCurrentSecret, validRegisteredClaims(user.ID), Claims{SessionID: uuid.NewString(), Role: user.Role}), ErrSessionNotFound},
		{"session of another account", signClaimsWithSecret(t, testCurrentSecret, validRegisteredClaims(otherUser.ID), Claims{SessionID: login.Session.ID.String(), Role: user.Role}), ErrUnauthenticated},
		{"expired access token", expiredClaimToken(t, login.Session.ID.String(), user), ErrTokenExpired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Authenticate(ctx, tc.token); !errors.Is(err, tc.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// expiredClaimToken builds an already expired access token for a session.
func expiredClaimToken(t *testing.T, sessionID string, user *User) string {
	t.Helper()

	return signClaimsWithSecret(t, testCurrentSecret, jwt.RegisteredClaims{
		Issuer:    TokenIssuer,
		Subject:   user.ID.String(),
		IssuedAt:  jwt.NewNumericDate(fixedNow.Add(-48 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(-24 * time.Hour)),
	}, Claims{SessionID: sessionID, Role: user.Role})
}

func TestServiceAuthenticateAcceptsATokenOfTheRotationWindow(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	service := newTestService(t, repository, nil, testPreviousSecret)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// A token that was issued before the rotation: same session, old key.
	rotated := signClaimsWithSecret(t, testPreviousSecret, validRegisteredClaims(user.ID), Claims{
		SessionID: login.Session.ID.String(),
		Role:      user.Role,
	})

	principal, err := service.Authenticate(ctx, rotated)
	if err != nil {
		t.Fatalf("Authenticate() error = %v, want a token of the rotation window to be accepted", err)
	}
	if principal.KeySource != KeySourcePrevious {
		t.Errorf("KeySource = %q, want %q", principal.KeySource, KeySourcePrevious)
	}
}

func TestServiceAuthenticateUsesTheDatabaseIdentity(t *testing.T) {
	ctx := context.Background()
	tenantID, storeID := uuid.New(), uuid.New()
	newTenant, newStore := uuid.New(), uuid.New()
	user := newTestUser(t, tenantID, storeID, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	service := newTestService(t, repository, nil)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// A role and scope change in the database wins over the stale token claims:
	// authorization is never taken from a claim the client holds.
	repository.mutateUser(user.ID, func(u *User) {
		u.Role = RoleSuperAdmin
		u.TenantID = nil
		u.StoreID = nil
	})

	principal, err := service.Authenticate(ctx, login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.Role != RoleSuperAdmin {
		t.Errorf("role = %q, want the role stored in the database", principal.Role)
	}
	if principal.TenantID != uuid.Nil || principal.StoreID != uuid.Nil {
		t.Error("the server controlled scope must be re-read from the account, not from the token")
	}
	if !principal.IsPlatformAdmin() {
		t.Error("a super admin must be reported as a platform account")
	}
	// A platform account has no tenant scope: the call fails closed.
	if _, err := principal.Scope(); !errors.Is(err, ErrMissingTenantScope) {
		t.Errorf("Scope() error = %v, want ErrMissingTenantScope", err)
	}

	// Moving the account to another tenant is visible immediately as well.
	repository.mutateUser(user.ID, func(u *User) {
		u.Role = RoleStoreAdmin
		u.TenantID = cloneUUID(&newTenant)
		u.StoreID = cloneUUID(&newStore)
	})
	moved, err := service.Authenticate(ctx, login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	scope, err := moved.Scope()
	if err != nil {
		t.Fatalf("Scope() error = %v", err)
	}
	if scope.TenantID != newTenant || scope.StoreID != newStore {
		t.Errorf("scope = %v, want tenant %s / store %s", scope, newTenant, newStore)
	}
}

func TestServiceAuthenticateRejectsADisabledOrDeletedAccount(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		disable func(*User)
	}{
		{"disabled", func(u *User) { u.Status = StatusDisabled }},
		{"soft deleted", func(u *User) { u.DeletedAt = gorm.DeletedAt{Time: fixedNow, Valid: true} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
			fake := &fakeRepository{}
			fake.seedUser(user)
			service := newTestService(t, fake, nil)

			login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
			if err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			fake.mutateUser(user.ID, tc.disable)

			_, err = service.Authenticate(ctx, login.Tokens.AccessToken)
			if err == nil {
				t.Fatal("Authenticate() = nil error, want the account to be refused")
			}
			if tc.name == "disabled" && !errors.Is(err, ErrAccountInactive) {
				t.Errorf("Authenticate() error = %v, want ErrAccountInactive", err)
			}
		})
	}
}

func TestServiceLogoutRevokesTheSessionImmediately(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.New(), RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	principal, err := service.Authenticate(ctx, login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if err := service.Logout(ctx, principal, "203.0.113.7"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	session, ok := repository.findSession(login.Session.ID)
	if !ok {
		t.Fatal("the session row must survive a logout (append-only audit trail)")
	}
	if !session.IsRevoked() {
		t.Fatal("Logout() must revoke the session")
	}
	if session.RevokedReason == nil || *session.RevokedReason != RevocationLogout {
		t.Errorf("revoked reason = %v, want %q", session.RevokedReason, RevocationLogout)
	}

	// The credential of the revoked session is dead at once.
	if _, err := service.Authenticate(ctx, login.Tokens.AccessToken); !errors.Is(err, ErrSessionRevoked) {
		t.Errorf("Authenticate() after logout error = %v, want ErrSessionRevoked", err)
	}
	if _, err := service.Refresh(ctx, RefreshInput{RefreshToken: login.Tokens.RefreshToken}); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Refresh() after logout error = %v, want ErrUnauthenticated", err)
	}

	if actions := auditor.actions(); len(actions) < 2 || actions[0] != ActionLoginSucceeded || actions[1] != ActionLogout {
		t.Fatalf("audit actions = %v, want a login followed by a logout entry", actions)
	}
	entry := auditEntryFor(t, auditor, ActionLogout)
	if entry.ActorType != audit.ActorUser || entry.ActorID == nil || *entry.ActorID != user.ID {
		t.Error("the logout entry must name the account as the actor")
	}
	if sessionID := metadataValue(t, entry, AuthMetadataKeySession); sessionID != login.Session.ID.String() {
		t.Errorf("audit session_id = %q, want %q", sessionID, login.Session.ID)
	}
}

func TestServiceLogoutIsIdempotentForAnAlreadyRevokedSession(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	principal, err := service.Authenticate(ctx, login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if err := service.Logout(ctx, principal, ""); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	// A retry (the session is already revoked) is reported as success and is
	// still audited with the reason.
	if err := service.Logout(ctx, principal, ""); err != nil {
		t.Fatalf("Logout() retry error = %v, want nil", err)
	}
	if reason := metadataValue(t, auditor.last(), MetadataKeyReason); reason != ReasonSessionMissing {
		t.Errorf("audit reason = %q, want %q", reason, ReasonSessionMissing)
	}

	// Without a principal there is nothing to revoke.
	if err := service.Logout(ctx, nil, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Logout(nil) error = %v, want ErrUnauthenticated", err)
	}
}

func TestServiceListUsersIsScopedToTheCaller(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()

	adminA := newTestUser(t, tenantA, uuid.Nil, RoleStoreAdmin)
	adminA.Email = "admin-a@example.com"
	colleagueA := newTestUser(t, tenantA, uuid.Nil, RoleStoreAdmin)
	colleagueA.Email = "colleague-a@example.com"
	adminB := newTestUser(t, tenantB, uuid.Nil, RoleStoreAdmin)
	adminB.Email = "admin-b@example.com"
	super := newTestUser(t, uuid.Nil, uuid.Nil, RoleSuperAdmin)
	super.Email = "root@example.com"

	repository := &fakeRepository{}
	for _, user := range []*User{adminA, colleagueA, adminB, super} {
		repository.seedUser(user)
	}
	service := newTestService(t, repository, nil)

	// A store admin sees its own tenant only: tenant B never appears, even
	// though it exists and the request carries no filter.
	storeAdmin := &Principal{User: adminA, Role: RoleStoreAdmin, TenantID: tenantA}
	users, err := service.ListUsers(ctx, storeAdmin, ListFilter{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want the 2 accounts of tenant A", len(users))
	}
	for _, user := range users {
		if user.TenantIDValue() != tenantA {
			t.Errorf("user %s belongs to tenant %s, want only tenant %s", user.Email, user.TenantIDValue(), tenantA)
		}
	}

	// A platform super admin sees every tenant (BLUEPRINT §5).
	platformAdmin := &Principal{User: super, Role: RoleSuperAdmin}
	all, err := service.ListUsers(ctx, platformAdmin, ListFilter{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("users = %d, want the 4 accounts of the platform", len(all))
	}
}

func TestServiceListUsersIgnoresATamperedScope(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()

	adminA := newTestUser(t, tenantA, uuid.Nil, RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(adminA)
	repository.seedUser(newTestUser(t, tenantB, uuid.Nil, RoleStoreAdmin))
	service := newTestService(t, repository, nil)

	// A principal whose *fields* claim tenant B while the account row belongs to
	// tenant A (a stale or tampered in-memory value) must still resolve to the
	// tenant of the account: the scope comes from the database row.
	tampered := &Principal{
		User:     adminA,
		Role:     RoleStoreAdmin,
		TenantID: tenantB,
		StoreID:  uuid.New(),
	}
	users, err := service.ListUsers(ctx, tampered, ListFilter{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 || users[0].TenantIDValue() != tenantA {
		t.Fatalf("ListUsers() returned %d rows, want only tenant A", len(users))
	}

	scope, err := tampered.Scope()
	if err != nil {
		t.Fatalf("Scope() error = %v", err)
	}
	if scope.TenantID != tenantA {
		t.Errorf("scope.TenantID = %s, want the tenant of the account %s", scope.TenantID, tenantA)
	}
}

func TestServiceListUsersRequiresAPrincipal(t *testing.T) {
	service := newTestService(t, &fakeRepository{}, nil)
	if _, err := service.ListUsers(context.Background(), nil, ListFilter{}); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("ListUsers(nil) error = %v, want ErrUnauthenticated", err)
	}
}

func TestServiceNeverWritesCredentialsToTheAuditTrail(t *testing.T) {
	ctx := context.Background()
	user := newTestUser(t, uuid.New(), uuid.New(), RoleStoreAdmin)
	repository := &fakeRepository{}
	repository.seedUser(user)
	auditor := &recordingAuditor{}
	service := newTestService(t, repository, auditor, testPreviousSecret)

	login, err := service.Login(ctx, loginInput(user.Email, strongPassword))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	principal, err := service.Authenticate(ctx, login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if err := service.Logout(ctx, principal, "203.0.113.7"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	trail := auditor.dump(t)
	forbidden := map[string]string{
		"password":             strongPassword,
		"password hash":        user.PasswordHash,
		"access token":         login.Tokens.AccessToken,
		"refresh token":        login.Tokens.RefreshToken,
		"current signing key":  testCurrentSecret,
		"previous signing key": testPreviousSecret,
		"bearer scheme":        TokenTypeBearer,
	}
	for name, secret := range forbidden {
		if secret == "" {
			continue
		}
		if strings.Contains(trail, secret) {
			t.Errorf("the audit trail contains the %s: %s", name, trail)
		}
	}
}

func TestAuditActionsMatchTheAuditTrailRule(t *testing.T) {
	actions := AllAuditActions()
	if len(actions) == 0 {
		t.Fatal("AllAuditActions() must not be empty")
	}

	pattern := regexp.MustCompile(audit.ActionPattern)
	seen := make(map[string]bool, len(actions))
	for _, action := range actions {
		if seen[action] {
			t.Errorf("duplicate audit action %q", action)
		}
		seen[action] = true
		if !pattern.MatchString(action) {
			t.Errorf("audit action %q does not match %s", action, audit.ActionPattern)
		}
		if len(action) < audit.MinActionLength || len(action) > audit.MaxActionLength {
			t.Errorf("audit action %q outside the documented length range", action)
		}
	}

	// The entries the service builds must pass the audit validation itself.
	user := newTestUser(t, uuid.New(), uuid.New(), RoleStoreAdmin)
	principal := &Principal{
		User:      user,
		Role:      user.Role,
		TenantID:  user.TenantIDValue(),
		SessionID: uuid.New(),
	}
	entries := []*audit.Entry{
		loginSucceeded(user, uuid.New()),
		loginFailed(user, user.Email, ReasonMissing),
		loginFailed(nil, "nobody@example.com", ReasonMissing),
		logout(principal, ""),
		tokenRefreshed(user, uuid.New()),
		refreshRejected(user, ReasonSessionRevoked),
	}
	for _, entry := range entries {
		entry.Normalize()
		if err := entry.Validate(); err != nil {
			t.Errorf("entry %q: %v", entry.Action, err)
		}
	}
}
