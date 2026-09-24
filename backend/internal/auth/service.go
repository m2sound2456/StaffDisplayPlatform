package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m2sound2456/staffdisplay/backend/internal/audit"
	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// LoginMethodRefreshToken is the method recorded for a refresh exchange.
const LoginMethodRefreshToken = "refresh_token"

// ServiceOptions wires the authentication service. Every dependency is optional
// at construction time so the HTTP router can be built before a database
// exists; a call without its dependency answers ErrNotConfigured instead of
// panicking (the FG2 convention).
type ServiceOptions struct {
	// Repository persists users and sessions.
	Repository Repository
	// Audit records the authentication events (nil = no audit trail).
	Audit audit.Recorder
	// Signer signs and verifies access tokens.
	Signer *Signer
	// RefreshTokenTTL is the lifetime of a refresh token / session.
	RefreshTokenTTL time.Duration
	// Now overrides the clock (tests); production uses time.Now.
	Now func() time.Time
}

// Service implements login, refresh, logout and request authentication.
type Service struct {
	repository Repository
	auditor    audit.Recorder
	signer     *Signer
	refreshTTL time.Duration
	now        func() time.Time
}

// NewService builds the service; it never fails, so a misconfigured dependency
// surfaces as ErrNotConfigured at request time (and as a startup warning in the
// server wiring) rather than as a panic.
func NewService(opts ServiceOptions) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		repository: opts.Repository,
		auditor:    opts.Audit,
		signer:     opts.Signer,
		refreshTTL: opts.RefreshTokenTTL,
		now:        now,
	}
}

// Now returns the service clock (UTC).
func (s *Service) Now() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

// RefreshTTL returns the refresh token lifetime.
func (s *Service) RefreshTTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.refreshTTL
}

// ensureReady guards every entry point.
func (s *Service) ensureReady() error {
	switch {
	case s == nil || s.repository == nil || s.signer == nil:
		return ErrNotConfigured
	case s.refreshTTL <= 0:
		return fmt.Errorf("%w: refresh token TTL must be > 0", ErrNotConfigured)
	default:
		return nil
	}
}

// Tokens is the credential pair handed to a client.
type Tokens struct {
	// AccessToken is the JWT bearer token (short lived, always signed with the
	// current signing key).
	AccessToken string
	// AccessExpiresAt is the exp claim of the access token.
	AccessExpiresAt time.Time
	// RefreshToken is the opaque, single-use credential of the session. It is
	// only ever returned to the client that just authenticated.
	RefreshToken string
	// RefreshExpiresAt is the refresh expiry of the session.
	RefreshExpiresAt time.Time
	// TokenType is the Authorization scheme ("Bearer").
	TokenType string
}

// SessionResult is the outcome of a login or a refresh.
type SessionResult struct {
	User    *User
	Session *Session
	Tokens  Tokens
}

// Principal is the authenticated caller of one request. It is built from the
// users row (the database is authoritative), not from the token claims: a role
// or scope change takes effect on the next request, and a token can never widen
// the scope it was issued for.
type Principal struct {
	// User is the account row as it is stored now.
	User *User
	// SessionID is the user_sessions row behind the credential.
	SessionID uuid.UUID
	// Role is the current role of the account.
	Role Role
	// TenantID is the current tenant scope (nil UUID for a platform account).
	TenantID uuid.UUID
	// StoreID is the current store binding (nil UUID when not bound).
	StoreID uuid.UUID
	// KeySource reports which signing key verified the token.
	KeySource KeySource
	// AccessExpiresAt is the exp claim of the presented access token.
	AccessExpiresAt time.Time
}

// UserID returns the account id.
func (p *Principal) UserID() uuid.UUID {
	if p == nil || p.User == nil {
		return uuid.Nil
	}
	return p.User.ID
}

// Email returns the account address.
func (p *Principal) Email() string {
	if p == nil || p.User == nil {
		return ""
	}
	return p.User.Email
}

// DisplayName returns the account display name.
func (p *Principal) DisplayName() string {
	if p == nil || p.User == nil {
		return ""
	}
	return p.User.DisplayName
}

// IsPlatformAdmin reports whether the caller acts platform-wide.
func (p *Principal) IsPlatformAdmin() bool { return p != nil && p.Role.IsPlatform() }

// HasRole reports whether the caller holds one of the given roles.
func (p *Principal) HasRole(roles ...Role) bool {
	if p == nil {
		return false
	}
	for _, role := range roles {
		if p.Role == role {
			return true
		}
	}
	return false
}

// Scope returns the tenant isolation scope of the caller for repository calls.
// A platform account fails closed with ErrMissingTenantScope: platform-wide
// queries go through an explicitly named platform method instead.
func (p *Principal) Scope() (store.Scope, error) {
	if p == nil || p.User == nil {
		return store.Scope{}, ErrUnauthenticated
	}
	return p.User.Scope()
}

// LoginInput is a sign-in attempt.
type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
	IP        string
}

// RefreshInput is a refresh-token exchange.
type RefreshInput struct {
	RefreshToken string
	UserAgent    string
	IP           string
}

// Login authenticates an e-mail/password pair and opens a session.
//
// The tenant scope of the resulting session is taken from the users row, never
// from the request: a store admin cannot point a login at another tenant, and
// the login form itself carries no tenant or store identifier.
func (s *Service) Login(ctx context.Context, in LoginInput) (*SessionResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}

	email := NormalizeEmail(in.Email)
	password := in.Password

	fields := make([]FieldError, 0, 2)
	if email == "" {
		fields = append(fields, fieldError("email", "is required"))
	}
	if password == "" {
		fields = append(fields, fieldError("password", "is required"))
	}
	if len(fields) > 0 {
		return nil, newValidationError(fields...)
	}

	now := s.Now()
	ip := ParseAddr(in.IP)

	user, err := s.repository.FindUserByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if user == nil {
		// An unknown address costs the same as a wrong password: the response
		// never discloses whether the account exists.
		VerifyPasswordConstantTime("", password)
		s.record(ctx, loginFailed(user, email, ReasonMissing), ip, now)
		return nil, ErrInvalidCredentials
	}

	if !VerifyPasswordConstantTime(user.PasswordHash, password) {
		s.record(ctx, loginFailed(user, email, ReasonMissing), ip, now)
		return nil, ErrInvalidCredentials
	}
	// The password was correct, so naming the state of the account leaks
	// nothing a caller does not already know.
	if !user.IsActive() {
		s.record(ctx, loginFailed(user, email, ReasonAccountInactive), ip, now)
		return nil, ErrAccountInactive
	}

	result, err := s.openSession(ctx, user, in.UserAgent, in.IP, now)
	if err != nil {
		return nil, err
	}
	// The response describes the account as of this login.
	stamp := now.UTC()
	result.User.LastLoginAt = &stamp
	if err := s.touchLastLogin(ctx, user, now); err != nil {
		// A convenience stamp must not turn a valid login into a failure.
		logger.Warn("auth_last_login_not_recorded",
			zap.String("user_id", user.ID.String()),
			zap.String("reason", ReasonOf(err)),
		)
	}
	s.record(ctx, loginSucceeded(user, result.Session.ID), ip, now)
	return result, nil
}

// openSession creates the session row and issues the token pair.
func (s *Service) openSession(ctx context.Context, user *User, userAgent, ip string, now time.Time) (*SessionResult, error) {
	refreshToken, digest, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	session, err := NewSession(user, digest, s.refreshTTL, userAgent, ip, now)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	accessToken, _, accessExpiresAt, err := s.signer.NewAccessToken(user, session.ID, now)
	if err != nil {
		return nil, err
	}

	return &SessionResult{
		User:    user,
		Session: session,
		Tokens: Tokens{
			AccessToken:      accessToken,
			AccessExpiresAt:  accessExpiresAt,
			RefreshToken:     refreshToken,
			RefreshExpiresAt: session.ExpiresAt,
			TokenType:        TokenTypeBearer,
		},
	}, nil
}

// Refresh exchanges a refresh token for a new token pair (rotation).
//
// The presented digest is replaced in place, so the old refresh token stops
// working immediately while the session (and therefore the access token) keeps
// its identity. The refresh TTL is a sliding window: an active client stays
// signed in, an idle one expires.
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (*SessionResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}

	token := strings.TrimSpace(in.RefreshToken)
	switch {
	case token == "":
		return nil, newValidationError(fieldError("refresh_token", "is required"))
	case len(token) > MaxTokenLength:
		return nil, ErrUnauthenticated
	}

	now := s.Now()
	ip := ParseAddr(in.IP)

	authenticated, err := s.repository.FindSessionByRefreshDigest(ctx, TokenDigest(DigestToken(token)))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			s.record(ctx, refreshRejected(nil, ReasonSessionMissing), ip, now)
			return nil, ErrUnauthenticated
		}
		return nil, err
	}

	session, user := authenticated.Session, authenticated.User
	switch {
	case session.IsRevoked():
		s.record(ctx, refreshRejected(user, ReasonSessionRevoked), ip, now)
		return nil, ErrUnauthenticated
	case session.IsExpired(now):
		s.record(ctx, refreshRejected(user, ReasonExpired), ip, now)
		return nil, ErrUnauthenticated
	case user.IsDeleted():
		s.record(ctx, refreshRejected(user, ReasonSessionMissing), ip, now)
		return nil, ErrUnauthenticated
	case !user.IsActive():
		s.record(ctx, refreshRejected(user, ReasonAccountInactive), ip, now)
		return nil, ErrAccountInactive
	}

	refreshToken, digest, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	expiresAt := now.Add(s.refreshTTL)
	if err := s.repository.RotateSession(ctx, session.ID, digest, expiresAt, now); err != nil {
		return nil, err
	}

	accessToken, _, accessExpiresAt, err := s.signer.NewAccessToken(user, session.ID, now)
	if err != nil {
		return nil, err
	}

	session.RefreshTokenHash = string(digest)
	session.ExpiresAt = expiresAt
	session.LastUsedAt = now
	s.record(ctx, tokenRefreshed(user, session.ID), ip, now)

	return &SessionResult{
		User:    user,
		Session: session,
		Tokens: Tokens{
			AccessToken:      accessToken,
			AccessExpiresAt:  accessExpiresAt,
			RefreshToken:     refreshToken,
			RefreshExpiresAt: expiresAt,
			TokenType:        TokenTypeBearer,
		},
	}, nil
}

// Logout revokes the session behind the presented credential. The access token
// stops working on the very next request (the middleware rejects a revoked
// session), which is what makes logout immediate despite a stateless JWT.
func (s *Service) Logout(ctx context.Context, principal *Principal, ip string) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if principal == nil || principal.SessionID == uuid.Nil {
		return ErrUnauthenticated
	}

	now := s.Now()
	err := s.repository.RevokeSession(ctx, principal.SessionID, RevocationLogout, now)
	switch {
	case err == nil:
		s.record(ctx, logout(principal, ""), ParseAddr(ip), now)
		return nil
	case errors.Is(err, ErrSessionNotFound):
		// Already revoked or gone: logout is idempotent by intent, so a retry
		// is reported as success and the attempt is still audited.
		s.record(ctx, logout(principal, ReasonSessionMissing), ParseAddr(ip), now)
		return nil
	default:
		return err
	}
}

// Authenticate resolves the caller of a request from an access token.
//
// Order: verify the token (current key, then the rotation window), load its
// session together with the account, then re-read role, status, tenant and store
// from the database. Nothing that decides authorization comes from the token
// alone, so revoking a session, disabling an account or moving it to another
// store takes effect immediately.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (*Principal, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}

	verified, err := s.signer.Verify(accessToken)
	if err != nil {
		return nil, err
	}
	claims := verified.Claims
	userID, err := claims.UserUUID()
	if err != nil {
		return nil, err
	}
	sessionID, err := claims.SessionUUID()
	if err != nil {
		return nil, err
	}

	authenticated, err := s.repository.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	session, user := authenticated.Session, authenticated.User

	now := s.Now()
	switch {
	case session.IsRevoked():
		return nil, ErrSessionRevoked
	case session.IsExpired(now):
		return nil, ErrSessionExpired
	case user.IsDeleted():
		// The account was removed: the row can still be referenced by the
		// session, so this is checked explicitly as well.
		return nil, fmt.Errorf("%w: account of session %s no longer exists", ErrUnauthenticated, session.ID)
	case user.ID != userID:
		// The sid of the token does not belong to the sub of the token.
		return nil, ErrUnauthenticated
	case !user.IsActive():
		return nil, ErrAccountInactive
	}

	return &Principal{
		User:            user,
		SessionID:       session.ID,
		Role:            user.Role,
		TenantID:        user.TenantIDValue(),
		StoreID:         user.StoreIDValue(),
		KeySource:       verified.KeySource,
		AccessExpiresAt: claimTime(claims.ExpiresAt),
	}, nil
}

// ListUsers returns the accounts the caller may see: its own tenant for a store
// admin, every tenant for a platform super admin (BLUEPRINT §5).
func (s *Service) ListUsers(ctx context.Context, principal *Principal, filter ListFilter) ([]User, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if principal == nil || principal.User == nil {
		return nil, ErrUnauthenticated
	}
	if principal.IsPlatformAdmin() {
		return s.repository.ListPlatformUsers(ctx, filter)
	}

	scope, err := principal.Scope()
	if err != nil {
		return nil, err
	}
	return s.repository.ListUsers(ctx, scope, filter)
}

// touchLastLogin stamps the last successful sign-in.
func (s *Service) touchLastLogin(ctx context.Context, user *User, at time.Time) error {
	if user == nil {
		return newValidationError(fieldError("user", "is required"))
	}
	if !user.HasTenant() {
		return s.repository.TouchPlatformLogin(ctx, user.ID, at)
	}
	scope, err := user.Scope()
	if err != nil {
		return err
	}
	return s.repository.TouchLastLogin(ctx, scope, user.ID, at)
}

// claimTime converts an optional numeric date claim to a UTC time.
func claimTime(date *jwt.NumericDate) time.Time {
	if date == nil {
		return time.Time{}
	}
	return date.Time.UTC()
}

// ParseAddr parses a caller address without ever failing the request: an
// unusable value simply means "no ip recorded".
func ParseAddr(raw string) netip.Addr {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return netip.Addr{}
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		return addr
	}
	// Tolerate host:port as well.
	if addrPort, err := netip.ParseAddrPort(trimmed); err == nil {
		return addrPort.Addr()
	}
	return netip.Addr{}
}

// AuthMetadataKeySession is the audit metadata key carrying the session id (an
// opaque identifier, never the credential itself).
const AuthMetadataKeySession = "session_id"

// record writes one audit entry.
//
// An audit failure is logged and swallowed: an unavailable audit table must not
// lock every administrator out of the platform, and the outcome itself is
// already reported to the caller. The entry never contains a password, a token
// or a signing key (docs/AI_RULES.md §5.7).
func (s *Service) record(ctx context.Context, entry *audit.Entry, ip netip.Addr, now time.Time) {
	if s == nil || s.auditor == nil || entry == nil {
		return
	}
	entry.CreatedAt = now.UTC()
	if ip.IsValid() {
		entry.FromAddr(ip)
	}
	if err := s.auditor.Record(ctx, entry); err != nil {
		logger.Warn("auth_audit_record_failed",
			zap.String("action", entry.Action),
			zap.Error(err),
		)
	}
}

// loginSucceeded builds the entry of a successful sign-in.
func loginSucceeded(user *User, sessionID uuid.UUID) *audit.Entry {
	entry := actorEntry(ActionLoginSucceeded, user)
	values := map[string]string{MetadataKeyMethod: LoginMethodPassword}
	if sessionID != uuid.Nil {
		values[AuthMetadataKeySession] = sessionID.String()
	}
	return entry.WithMetadata(metadata(values))
}

// loginFailed builds the entry of a rejected sign-in. It records the attempted
// address (so brute force is investigable) but never the password.
func loginFailed(user *User, email, reason string) *audit.Entry {
	entry := actorEntry(ActionLoginFailed, user)
	return entry.WithMetadata(metadata(map[string]string{
		MetadataKeyMethod: LoginMethodPassword,
		MetadataKeyReason: reason,
		MetadataKeyEmail:  NormalizeEmail(email),
	}))
}

// logout builds the entry of a logout.
func logout(principal *Principal, reason string) *audit.Entry {
	entry := audit.New(audit.ActorUser, audit.UUID(principal.UserID()), ActionLogout).
		ForTenant(principal.TenantID).
		ForStore(principal.StoreID).
		OnEntity("user", principal.UserID())
	values := map[string]string{
		MetadataKeyMethod:      LoginMethodPassword,
		AuthMetadataKeySession: principal.SessionID.String(),
	}
	if reason != "" {
		values[MetadataKeyReason] = reason
	}
	return entry.WithMetadata(metadata(values))
}

// tokenRefreshed builds the entry of a rotated refresh token.
func tokenRefreshed(user *User, sessionID uuid.UUID) *audit.Entry {
	entry := actorEntry(ActionTokenRefreshed, user)
	values := map[string]string{MetadataKeyMethod: LoginMethodRefreshToken}
	if sessionID != uuid.Nil {
		values[AuthMetadataKeySession] = sessionID.String()
	}
	return entry.WithMetadata(metadata(values))
}

// refreshRejected builds the entry of a refused refresh token.
func refreshRejected(user *User, reason string) *audit.Entry {
	entry := actorEntry(ActionRefreshRejected, user)
	return entry.WithMetadata(metadata(map[string]string{
		MetadataKeyMethod: LoginMethodRefreshToken,
		MetadataKeyReason: reason,
	}))
}

// actorEntry starts an entry whose actor is the account when it is known, and
// the system otherwise (an unknown address has no actor_id; the audit schema
// requires one for a user actor).
func actorEntry(action string, user *User) *audit.Entry {
	if user == nil || user.ID == uuid.Nil {
		return audit.New(audit.ActorSystem, nil, action)
	}
	return audit.New(audit.ActorUser, audit.UUID(user.ID), action).
		ForTenant(user.TenantIDValue()).
		ForStore(user.StoreIDValue()).
		OnEntity("user", user.ID)
}

// metadata marshals the action detail of an entry.
func metadata(values map[string]string) json.RawMessage {
	raw, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage(audit.DefaultMetadata)
	}
	return raw
}
