package auth

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Test secrets: obviously fake, long enough to look like a generated key.
const (
	testCurrentSecret  = "test-current-signing-key-0123456789abcdef"
	testPreviousSecret = "test-previous-signing-key-0123456789abcdef"
	testOlderSecret    = "test-oldest-signing-key-0123456789abcdef"
	testUnknownSecret  = "test-unknown-signing-key-0123456789abcdef"
)

// fixedNow is a deterministic clock for the token tests.
var fixedNow = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

// testPasswordHash caches one bcrypt hash: hashing with the production cost on
// every test user would dominate the runtime, and the tests only ever need a
// valid digest.
var testPasswordHash = sync.OnceValue(func() string {
	hash, err := HashPassword(strongPassword)
	if err != nil {
		panic(err)
	}
	return hash
})

// newTestUser builds an active account (never persisted).
func newTestUser(t *testing.T, tenantID, storeID uuid.UUID, role Role) *User {
	t.Helper()

	user := &User{
		ID:           uuid.New(),
		Email:        "admin@example.com",
		DisplayName:  "Store Admin",
		PasswordHash: testPasswordHash(),
		Role:         role,
		Status:       StatusActive,
		CreatedAt:    fixedNow,
		UpdatedAt:    fixedNow,
	}
	if tenantID != uuid.Nil {
		user.TenantID = cloneUUID(&tenantID)
	}
	if storeID != uuid.Nil {
		user.StoreID = cloneUUID(&storeID)
	}
	return user
}

// newTestSigner builds a signer with a frozen clock.
func newTestSigner(t *testing.T, ttl time.Duration, previous ...string) *Signer {
	t.Helper()

	signer, err := NewSigner(SignerOptions{
		Secret:          testCurrentSecret,
		PreviousSecrets: previous,
		AccessTokenTTL:  ttl,
		Now:             func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	return signer
}

// signClaimsWithSecret builds a token with an explicit key, which is how these
// tests produce tokens the production signer would never issue.
func signClaimsWithSecret(t *testing.T, secret string, registered jwt.RegisteredClaims, claims Claims) string {
	t.Helper()

	claims.RegisteredClaims = registered
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return token
}

// signWithAlgorithm builds a token with an arbitrary JWS algorithm header, to
// prove the parser pins HS256.
func signWithAlgorithm(t *testing.T, alg string, registered jwt.RegisteredClaims, claims Claims) string {
	t.Helper()

	claims.RegisteredClaims = registered
	method := jwt.GetSigningMethod(alg)
	if method == nil {
		t.Fatalf("unknown signing method %q", alg)
	}
	token := jwt.NewWithClaims(method, claims)
	if alg == "none" {
		signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("sign none token: %v", err)
		}
		return signed
	}
	signed, err := token.SignedString([]byte(testCurrentSecret))
	if err != nil {
		t.Fatalf("sign %s token: %v", alg, err)
	}
	return signed
}

// validRegisteredClaims builds the registered part of a usable test token.
func validRegisteredClaims(userID uuid.UUID) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Issuer:    TokenIssuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(fixedNow),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(time.Hour)),
	}
}

func TestNewSignerValidatesItsInput(t *testing.T) {
	if _, err := NewSigner(SignerOptions{AccessTokenTTL: time.Minute}); err == nil {
		t.Error("NewSigner() = nil error without a signing key")
	}
	if _, err := NewSigner(SignerOptions{Secret: "   ", AccessTokenTTL: time.Minute}); err == nil {
		t.Error("NewSigner() = nil error for a blank signing key")
	}
	if _, err := NewSigner(SignerOptions{Secret: testCurrentSecret}); err == nil {
		t.Error("NewSigner() = nil error for a non-positive access token TTL")
	}
	if _, err := NewSigner(SignerOptions{Secret: testCurrentSecret, AccessTokenTTL: time.Minute, PreviousSecrets: []string{testCurrentSecret}}); err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	signer := newTestSigner(t, 15*time.Minute)
	if signer.Issuer() != TokenIssuer {
		t.Errorf("Issuer() = %q, want %q", signer.Issuer(), TokenIssuer)
	}
	if signer.TTL() != 15*time.Minute {
		t.Errorf("TTL() = %s, want 15m", signer.TTL())
	}
	if signer.RotationWindow() != 0 {
		t.Errorf("RotationWindow() = %d, want 0", signer.RotationWindow())
	}
}

func TestNewSignerDropsDuplicateAndBlankRotationKeys(t *testing.T) {
	signer := newTestSigner(t, time.Minute, "", "   ", testCurrentSecret, testPreviousSecret)
	if signer.RotationWindow() != 1 {
		t.Errorf("RotationWindow() = %d, want 1 (blank/duplicate keys must not widen the window)", signer.RotationWindow())
	}
}

// testSessionID returns a fresh session id.
func testSessionID() uuid.UUID { return uuid.New() }

func TestNewAccessTokenCarriesTheCallerIdentity(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()
	user := newTestUser(t, tenantID, storeID, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute)
	sessionID := testSessionID()

	token, claims, expiresAt, err := signer.NewAccessToken(user, sessionID, fixedNow)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("NewAccessToken() returned an empty token")
	}
	if want := fixedNow.Add(15 * time.Minute); !expiresAt.Equal(want) {
		t.Errorf("expiresAt = %s, want %s", expiresAt, want)
	}
	if claims.Subject != user.ID.String() {
		t.Errorf("sub = %q, want %q", claims.Subject, user.ID)
	}
	if claims.Role != RoleStoreAdmin {
		t.Errorf("role = %q, want %q", claims.Role, RoleStoreAdmin)
	}
	if claims.TenantID != tenantID.String() || claims.StoreID != storeID.String() {
		t.Errorf("scope claims = (%q, %q), want (%q, %q)", claims.TenantID, claims.StoreID, tenantID, storeID)
	}
	if claims.Issuer != TokenIssuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, TokenIssuer)
	}
	if claims.SessionID != sessionID.String() {
		t.Errorf("sid = %q, want %q", claims.SessionID, sessionID)
	}

	verified, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.UsedPreviousKey() {
		t.Error("a freshly signed token must verify with the current key")
	}
	if verified.Claims.SessionID != claims.SessionID {
		t.Error("the sid claim must survive a round trip")
	}
}

func TestNewAccessTokenRequiresAUserAndSession(t *testing.T) {
	signer := newTestSigner(t, time.Minute)
	if _, _, _, err := signer.NewAccessToken(nil, testSessionID(), fixedNow); err == nil {
		t.Error("NewAccessToken(nil user) = nil error, want a validation error")
	}
	if _, _, _, err := signer.NewAccessToken(newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin), uuid.Nil, fixedNow); err == nil {
		t.Error("NewAccessToken(nil session) = nil error, want a validation error")
	}
}

func TestVerifyRejectsAnUnknownSigningKey(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute)

	foreign, err := NewSigner(SignerOptions{Secret: testUnknownSecret, AccessTokenTTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	token, _, _, err := foreign.NewAccessToken(user, testSessionID(), fixedNow)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	if _, err := signer.Verify(token); !errors.Is(err, ErrTokenSignature) {
		t.Fatalf("Verify() error = %v, want ErrTokenSignature", err)
	}
	if reason := ReasonOf(ErrTokenSignature); reason != ReasonSignature {
		t.Errorf("ReasonOf() = %q, want %q", reason, ReasonSignature)
	}
}

func TestVerifyRejectsMalformedTokens(t *testing.T) {
	signer := newTestSigner(t, 15*time.Minute, testPreviousSecret)

	cases := map[string]string{
		"empty":              "",
		"whitespace":         "   ",
		"not a jws":          "definitely-not-a-token",
		"two segments":       "header.payload",
		"four segments":      "a.b.c.d",
		"oversized":          strings.Repeat("a", MaxTokenLength+1),
		"base64 garbage":     "!!!.???.###",
		"bearer prefix kept": TokenTypeBearer + " abc.def.ghi",
	}
	for name, token := range cases {
		_, err := signer.Verify(token)
		if err == nil {
			t.Errorf("%s: Verify() = nil error, want a rejection", name)
			continue
		}
		if !errors.Is(err, ErrTokenMalformed) && !errors.Is(err, ErrUnauthenticated) && !errors.Is(err, ErrTokenSignature) {
			t.Errorf("%s: Verify() error = %v, want a malformed/unknown-signature rejection", name, err)
		}
		if strings.Contains(err.Error(), testCurrentSecret) || strings.Contains(err.Error(), testPreviousSecret) {
			t.Errorf("%s: the error message must never contain a signing key", name)
		}
	}
}

func TestVerifyRejectsAnExpiredToken(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute)

	expired := signClaimsWithSecret(t, testCurrentSecret, jwt.RegisteredClaims{
		Issuer:    TokenIssuer,
		Subject:   user.ID.String(),
		IssuedAt:  jwt.NewNumericDate(fixedNow.Add(-2 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(-time.Hour)),
	}, Claims{SessionID: testSessionID().String(), Role: user.Role})

	if _, err := signer.Verify(expired); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify() error = %v, want ErrTokenExpired", err)
	}
}

func TestVerifyAcceptsATokenSignedWithAPreviousSecret(t *testing.T) {
	tenantID := uuid.New()
	user := newTestUser(t, tenantID, uuid.Nil, RoleStoreAdmin)
	sessionID := testSessionID()

	// The rotation: the old key is now a previous secret, the new one is current.
	signer := newTestSigner(t, 15*time.Minute, testPreviousSecret)
	rotated := signClaimsWithSecret(t, testPreviousSecret, validRegisteredClaims(user.ID), Claims{
		SessionID: sessionID.String(),
		Role:      user.Role,
		TenantID:  tenantID.String(),
	})

	verified, err := signer.Verify(rotated)
	if err != nil {
		t.Fatalf("Verify() error = %v, want a token of the rotation window to be accepted", err)
	}
	if !verified.UsedPreviousKey() {
		t.Error("UsedPreviousKey() = false, want true for a token signed with a previous key")
	}
	if verified.KeySource != KeySourcePrevious {
		t.Errorf("KeySource = %q, want %q", verified.KeySource, KeySourcePrevious)
	}
	if verified.PreviousIndex != 0 {
		t.Errorf("PreviousIndex = %d, want 0", verified.PreviousIndex)
	}
	if verified.Claims.SessionID != sessionID.String() {
		t.Error("the claims of a rotated token must be returned unchanged")
	}
}

func TestVerifyReportsWhichRotationKeyMatched(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute, testPreviousSecret, testOlderSecret)

	older := signClaimsWithSecret(t, testOlderSecret, validRegisteredClaims(user.ID), Claims{
		SessionID: testSessionID().String(),
		Role:      user.Role,
	})

	verified, err := signer.Verify(older)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.PreviousIndex != 1 {
		t.Errorf("PreviousIndex = %d, want 1 (the second configured previous key)", verified.PreviousIndex)
	}
}

func TestVerifyRejectsARotatedTokenOnceTheRotationIsComplete(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	rotated := signClaimsWithSecret(t, testPreviousSecret, validRegisteredClaims(user.ID), Claims{
		SessionID: testSessionID().String(),
		Role:      user.Role,
	})

	duringRotation := newTestSigner(t, 15*time.Minute, testPreviousSecret)
	if _, err := duringRotation.Verify(rotated); err != nil {
		t.Fatalf("Verify() during the rotation window error = %v", err)
	}

	// Dropping the previous key finishes the rotation: the old token is refused.
	afterRotation := newTestSigner(t, 15*time.Minute)
	if _, err := afterRotation.Verify(rotated); !errors.Is(err, ErrTokenSignature) {
		t.Fatalf("Verify() after the rotation error = %v, want ErrTokenSignature", err)
	}
}

func TestNewTokensAreAlwaysSignedWithTheCurrentSecret(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	sessionID := testSessionID()

	// A deployment in the middle of a rotation.
	rotating := newTestSigner(t, 15*time.Minute, testPreviousSecret)
	token, _, _, err := rotating.NewAccessToken(user, sessionID, fixedNow)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	// A signer that only knows the old key must not accept a newly issued token.
	previousOnly, err := NewSigner(SignerOptions{
		Secret:         testPreviousSecret,
		AccessTokenTTL: 15 * time.Minute,
		Now:            func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	if _, err := previousOnly.Verify(token); !errors.Is(err, ErrTokenSignature) {
		t.Fatalf("Verify() with the previous key only error = %v, want ErrTokenSignature", err)
	}

	// The current key accepts it, and it is not marked as a rotated token.
	verified, err := rotating.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.UsedPreviousKey() {
		t.Error("a new token must never be reported as signed with a previous key")
	}
}

func TestVerifyDoesNotFallBackForAnExpiredRotatedToken(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute, testPreviousSecret)

	expired := signClaimsWithSecret(t, testPreviousSecret, jwt.RegisteredClaims{
		Issuer:    TokenIssuer,
		Subject:   user.ID.String(),
		IssuedAt:  jwt.NewNumericDate(fixedNow.Add(-48 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(-24 * time.Hour)),
	}, Claims{SessionID: testSessionID().String(), Role: user.Role})

	if _, err := signer.Verify(expired); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify() error = %v, want ErrTokenExpired (the window never revives an expired token)", err)
	}
}

func TestVerifyPinsTheSigningAlgorithm(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	signer := newTestSigner(t, 15*time.Minute, testPreviousSecret)
	claims := Claims{SessionID: testSessionID().String(), Role: user.Role}

	// "none" and a different HMAC size must be refused before the signature is
	// even considered.
	for _, alg := range []string{"none", "HS512"} {
		token := signWithAlgorithm(t, alg, validRegisteredClaims(user.ID), claims)
		if _, err := signer.Verify(token); err == nil {
			t.Errorf("alg=%s: Verify() = nil error, want a rejection", alg)
		}
	}
}

func TestVerifyRequiresTheConfiguredIssuerAndExpiry(t *testing.T) {
	signer := newTestSigner(t, 15*time.Minute)
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	claims := Claims{SessionID: testSessionID().String(), Role: user.Role}

	noExpiry := signClaimsWithSecret(t, testCurrentSecret, jwt.RegisteredClaims{
		Issuer:   TokenIssuer,
		Subject:  user.ID.String(),
		IssuedAt: jwt.NewNumericDate(fixedNow),
	}, claims)
	if _, err := signer.Verify(noExpiry); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("token without exp: Verify() error = %v, want ErrTokenInvalid", err)
	}

	otherIssuer := signClaimsWithSecret(t, testCurrentSecret, jwt.RegisteredClaims{
		Issuer:    "somewhere-else",
		Subject:   user.ID.String(),
		IssuedAt:  jwt.NewNumericDate(fixedNow),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(time.Hour)),
	}, claims)
	if _, err := signer.Verify(otherIssuer); err == nil {
		t.Error("a token with a foreign issuer must be rejected")
	}
}

func TestVerifyRejectsUnusableClaims(t *testing.T) {
	signer := newTestSigner(t, 15*time.Minute)
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)

	cases := []struct {
		name   string
		mutate func(*Claims, *jwt.RegisteredClaims)
	}{
		{"missing sid", func(c *Claims, _ *jwt.RegisteredClaims) { c.SessionID = "" }},
		{"sid is not a uuid", func(c *Claims, _ *jwt.RegisteredClaims) { c.SessionID = "not-a-uuid" }},
		{"missing subject", func(_ *Claims, r *jwt.RegisteredClaims) { r.Subject = "" }},
		{"subject is not a uuid", func(_ *Claims, r *jwt.RegisteredClaims) { r.Subject = "42" }},
		{"unknown role", func(c *Claims, _ *jwt.RegisteredClaims) { c.Role = Role("owner") }},
		{"empty role", func(c *Claims, _ *jwt.RegisteredClaims) { c.Role = "" }},
		{"tenant is not a uuid", func(c *Claims, _ *jwt.RegisteredClaims) { c.TenantID = "not-a-uuid" }},
		{"store is not a uuid", func(c *Claims, _ *jwt.RegisteredClaims) { c.StoreID = "not-a-uuid" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registered := validRegisteredClaims(user.ID)
			claims := Claims{SessionID: testSessionID().String(), Role: user.Role}
			tc.mutate(&claims, &registered)

			token := signClaimsWithSecret(t, testCurrentSecret, registered, claims)
			if _, err := signer.Verify(token); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
			}
		})
	}
}

func TestParseBearer(t *testing.T) {
	token := "header.payload.signature"

	accepted := map[string]string{
		"plain":        TokenTypeBearer + " " + token,
		"lowercase":    "bearer " + token,
		"uppercase":    "BEARER " + token,
		"extra spaces": "Bearer   " + token,
		"surrounding":  "  Bearer " + token + "  ",
	}
	for name, header := range accepted {
		got, err := ParseBearer(header)
		if err != nil {
			t.Errorf("%s: ParseBearer() error = %v", name, err)
			continue
		}
		if got != token {
			t.Errorf("%s: ParseBearer() = %q, want %q", name, got, token)
		}
	}

	rejected := map[string]string{
		"empty":           "",
		"scheme only":     "Bearer",
		"other scheme":    "Basic " + token,
		"no scheme":       token,
		"trailing junk":   "Bearer " + token + " extra",
		"joined headers":  "Bearer " + token + ", Bearer other",
		"oversized token": "Bearer " + strings.Repeat("a", MaxTokenLength+1),
	}
	for name, header := range rejected {
		if _, err := ParseBearer(header); err == nil {
			t.Errorf("%s: ParseBearer(%q) = nil error, want a rejection", name, header)
		}
	}
}
