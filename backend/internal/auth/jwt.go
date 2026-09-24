package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// JWT access tokens (BLUEPRINT §5/§11): HS256, signed with the hardened
// AUTH_JWT_SECRET and verified against it. A signing-key rotation is supported
// through AUTH_JWT_PREVIOUS_SECRETS (FG3): a token signed with a rotated key is
// still accepted until it expires, but new tokens are **always** signed with the
// current key.
const (
	// TokenIssuer is the iss claim. It is a platform constant, not a domain:
	// nothing here is deployment specific (docs/AI_RULES.md §5.8).
	TokenIssuer = "staffdisplay"

	// SigningMethod is the only accepted JWS algorithm. The parser pins it, so
	// "alg: none" and asymmetric confusion attacks are rejected before the
	// signature is even considered.
	SigningMethod = "HS256"

	// TokenTypeBearer is the Authorization scheme of every authenticated call.
	TokenTypeBearer = "Bearer"

	// MaxTokenLength bounds a bearer token before it is parsed.
	MaxTokenLength = 4096

	// DefaultClockSkew tolerates a small clock difference between the app and a
	// client that inspects exp/nbf itself.
	DefaultClockSkew = 30 * time.Second
)

// signingMethod is the only method that may sign or verify a token.
var signingMethod = jwt.SigningMethodHS256

// Claims is the payload of an access token.
//
// The tenant and store claims are **mirrors** for diagnostics and for the client
// UI; the authoritative scope is re-read from the users row on every request
// (see Service.Authenticate), so editing a claim can never widen access.
type Claims struct {
	jwt.RegisteredClaims
	// SessionID (sid) identifies the user_sessions row; logout revokes it.
	SessionID string `json:"sid"`
	// Role mirrors users.role.
	Role Role `json:"role"`
	// TenantID is empty for a platform (super admin) account.
	TenantID string `json:"tenant_id,omitempty"`
	// StoreID is empty when the account is not bound to one store.
	StoreID string `json:"store_id,omitempty"`
}

// UserUUID returns the sub claim as a UUID.
func (c Claims) UserUUID() (uuid.UUID, error) { return parseClaimUUID("sub", c.Subject) }

// SessionUUID returns the sid claim as a UUID.
func (c Claims) SessionUUID() (uuid.UUID, error) { return parseClaimUUID("sid", c.SessionID) }

// TenantUUID returns the tenant claim or the nil UUID.
func (c Claims) TenantUUID() uuid.UUID { return optionalClaimUUID(c.TenantID) }

// StoreUUID returns the store claim or the nil UUID.
func (c Claims) StoreUUID() uuid.UUID { return optionalClaimUUID(c.StoreID) }

// SignerOptions configures a Signer. The server maps config.AuthConfig onto it,
// which keeps this package independent from the configuration types.
type SignerOptions struct {
	// Secret is the current signing key (AUTH_JWT_SECRET). Required.
	Secret string
	// PreviousSecrets is the rotation window (AUTH_JWT_PREVIOUS_SECRETS): keys
	// that are still accepted for verification but never sign a new token.
	PreviousSecrets []string
	// AccessTokenTTL is the lifetime of an access token (exp = now + TTL).
	AccessTokenTTL time.Duration
	// Issuer overrides the iss claim (defaults to TokenIssuer).
	Issuer string
	// ClockSkew tolerates a small clock difference (defaults to DefaultClockSkew).
	ClockSkew time.Duration
	// Now overrides the clock (tests); production uses time.Now.
	Now func() time.Time
}

// Signer signs access tokens and verifies them against the current key and the
// configured rotation window.
type Signer struct {
	current   []byte
	previous  [][]byte
	ttl       time.Duration
	issuer    string
	clockSkew time.Duration
	now       func() time.Time
}

// NewSigner builds a signer. It fails when the current key is empty or the TTL
// is not positive: configuration validation already guarantees both, so this is
// the last line of defence against a silently unauthenticated deployment.
func NewSigner(opts SignerOptions) (*Signer, error) {
	secret := strings.TrimSpace(opts.Secret)
	if secret == "" {
		return nil, fmt.Errorf("auth: signing key is required")
	}
	if opts.AccessTokenTTL <= 0 {
		return nil, fmt.Errorf("auth: access token TTL must be > 0")
	}

	issuer := strings.TrimSpace(opts.Issuer)
	if issuer == "" {
		issuer = TokenIssuer
	}
	skew := opts.ClockSkew
	if skew < 0 {
		skew = DefaultClockSkew
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	// A previous key equal to the current one is dropped: it would only widen
	// the accepted key set without enabling any rotation.
	previous := make([][]byte, 0, len(opts.PreviousSecrets))
	for _, candidate := range opts.PreviousSecrets {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" || trimmed == secret {
			continue
		}
		previous = append(previous, []byte(trimmed))
	}

	return &Signer{
		current:   []byte(secret),
		previous:  previous,
		ttl:       opts.AccessTokenTTL,
		issuer:    issuer,
		clockSkew: skew,
		now:       now,
	}, nil
}

// TTL returns the access token lifetime.
func (s *Signer) TTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.ttl
}

// Issuer returns the iss claim value.
func (s *Signer) Issuer() string {
	if s == nil {
		return ""
	}
	return s.issuer
}

// RotationWindow returns the number of accepted previous signing keys.
func (s *Signer) RotationWindow() int {
	if s == nil {
		return 0
	}
	return len(s.previous)
}

// Now returns the signer clock (UTC).
func (s *Signer) Now() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

// NewAccessToken issues an access token for a user and its session. The token is
// always signed with the current key.
func (s *Signer) NewAccessToken(user *User, sessionID uuid.UUID, now time.Time) (token string, claims Claims, expiresAt time.Time, err error) {
	if s == nil {
		return "", Claims{}, time.Time{}, ErrNotConfigured
	}
	if user == nil || user.ID == uuid.Nil {
		return "", Claims{}, time.Time{}, newValidationError(fieldError("user", "is required"))
	}
	if sessionID == uuid.Nil {
		return "", Claims{}, time.Time{}, newValidationError(fieldError("session_id", "is required"))
	}

	issuedAt := now.UTC()
	expiresAt = issuedAt.Add(s.ttl)
	claims = Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		SessionID: sessionID.String(),
		Role:      user.Role,
	}
	if user.HasTenant() {
		claims.TenantID = user.TenantID.String()
	}
	if user.HasStore() {
		claims.StoreID = user.StoreID.String()
	}

	token, err = s.sign(claims)
	if err != nil {
		return "", Claims{}, time.Time{}, err
	}
	return token, claims, expiresAt, nil
}

// KeySource names the configured key that verified a token.
type KeySource string

// Key sources.
const (
	// KeySourceCurrent is AUTH_JWT_SECRET: the key that signs new tokens.
	KeySourceCurrent KeySource = "current"
	// KeySourcePrevious is a key of the AUTH_JWT_PREVIOUS_SECRETS rotation
	// window: accepted for verification until the token expires.
	KeySourcePrevious KeySource = "previous"
)

// VerifiedToken is the result of a successful verification.
type VerifiedToken struct {
	// Claims is the verified payload.
	Claims Claims
	// KeySource reports whether the current key or a rotated key verified the
	// signature. A rotated match is normal during a rotation window and can be
	// logged so an operator sees when it is safe to drop the old key.
	KeySource KeySource
	// PreviousIndex is the position in the rotation window (-1 for the current
	// key).
	PreviousIndex int
}

// UsedPreviousKey reports whether a rotated key verified the token.
func (v VerifiedToken) UsedPreviousKey() bool { return v.KeySource == KeySourcePrevious }

// Verify checks a token against the configured keys.
//
// Rotation rule (FG3/FG4):
//
//  1. try the current signing secret;
//  2. if — and only if — the signature does not match, try the configured
//     previous secrets in order;
//  3. a previous secret never signs a new token (see NewAccessToken).
//
// A token rejected for any other reason (expired, malformed, wrong issuer,
// unusable claims) is never retried with another key: the rotation window only
// ever widens *which key* is accepted, never what a token may contain.
func (s *Signer) Verify(raw string) (VerifiedToken, error) {
	if s == nil {
		return VerifiedToken{}, ErrNotConfigured
	}

	token := strings.TrimSpace(raw)
	switch {
	case token == "":
		return VerifiedToken{}, ErrUnauthenticated
	case len(token) > MaxTokenLength:
		return VerifiedToken{}, ErrTokenMalformed
	}

	claims, err := s.parse(token, s.current)
	if err == nil {
		return VerifiedToken{Claims: *claims, KeySource: KeySourceCurrent, PreviousIndex: -1}, nil
	}
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		return VerifiedToken{}, classifyTokenError(err)
	}

	for index, previous := range s.previous {
		rotated, rotatedErr := s.parse(token, previous)
		if rotatedErr == nil {
			return VerifiedToken{Claims: *rotated, KeySource: KeySourcePrevious, PreviousIndex: index}, nil
		}
		if !errors.Is(rotatedErr, jwt.ErrTokenSignatureInvalid) {
			return VerifiedToken{}, classifyTokenError(rotatedErr)
		}
	}

	// No configured key (current or rotated) produced this signature.
	return VerifiedToken{}, ErrTokenSignature
}

// parse verifies and decodes a token with one candidate key.
func (s *Signer) parse(raw string, key []byte) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{SigningMethod}),
		jwt.WithIssuer(s.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(s.clockSkew),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return nil, err
	}
	if err := validateClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// sign encodes and signs claims with the current key only.
func (s *Signer) sign(claims Claims) (string, error) {
	if s == nil || len(s.current) == 0 {
		return "", ErrNotConfigured
	}
	signed, err := jwt.NewWithClaims(signingMethod, claims).SignedString(s.current)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// validateClaims rejects a well formed token whose identity claims are unusable.
func validateClaims(claims *Claims) error {
	if claims == nil {
		return ErrTokenInvalid
	}
	if _, err := claims.UserUUID(); err != nil {
		return err
	}
	if _, err := claims.SessionUUID(); err != nil {
		return err
	}
	if !claims.Role.Valid() {
		return fmt.Errorf("%w: role claim is not a documented role", ErrTokenInvalid)
	}
	if claims.TenantID != "" && claims.TenantUUID() == uuid.Nil {
		return fmt.Errorf("%w: tenant claim is not a UUID", ErrTokenInvalid)
	}
	if claims.StoreID != "" && claims.StoreUUID() == uuid.Nil {
		return fmt.Errorf("%w: store claim is not a UUID", ErrTokenInvalid)
	}
	return nil
}

// classifyTokenError maps a parser error to a sentinel. The original message is
// deliberately dropped: it could quote token material, and a caller only needs
// the reason (ReasonOf) for logs and audit metadata.
func classifyTokenError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrTokenExpired
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return ErrTokenNotYetValid
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrTokenSignature
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrTokenMalformed
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return fmt.Errorf("%w: issuer does not match", ErrTokenInvalid)
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return fmt.Errorf("%w: a required claim is missing", ErrTokenInvalid)
	case errors.Is(err, jwt.ErrTokenInvalidClaims):
		return ErrTokenInvalid
	default:
		return ErrTokenInvalid
	}
}

// parseClaimUUID parses a required id claim without echoing its value.
func parseClaimUUID(claim, value string) (uuid.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return uuid.Nil, fmt.Errorf("%w: %s claim is missing", ErrTokenInvalid, claim)
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s claim is not a UUID", ErrTokenInvalid, claim)
	}
	return parsed, nil
}

// optionalClaimUUID parses an optional id claim (nil UUID when empty/invalid).
func optionalClaimUUID(value string) uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

// ParseBearer extracts the token of an Authorization header value.
//
// Only the exact "Bearer <token>" shape is accepted: a comma joined value (two
// headers), another scheme or trailing garbage is rejected instead of being
// interpreted, so a proxy can never smuggle a second credential past the API.
func ParseBearer(header string) (string, error) {
	value := strings.TrimSpace(header)
	if value == "" {
		return "", ErrUnauthenticated
	}
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], TokenTypeBearer) {
		return "", ErrTokenMalformed
	}
	token := parts[1]
	switch {
	case token == "":
		return "", ErrTokenMalformed
	case len(token) > MaxTokenLength:
		return "", ErrTokenMalformed
	default:
		return token, nil
	}
}
