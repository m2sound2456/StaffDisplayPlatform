package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionRulesMatchDatabaseConstraint(t *testing.T) {
	sql := migrationSQL(t, "0006_create_user_sessions.sql")

	if !strings.Contains(sql, `^[0-9a-f]{64}$`) {
		t.Error("0006_create_user_sessions.sql must pin the sha256 shape of refresh_token_hash")
	}
	for _, reason := range RevocationReasons() {
		if !strings.Contains(sql, "'"+reason+"'") {
			t.Errorf("0006_create_user_sessions.sql must allow the revocation reason %q", reason)
		}
	}
	for _, name := range []string{
		"user_sessions_refresh_token_hash_key",
		"user_sessions_user_created_idx",
		"user_sessions_revocation_is_atomic",
	} {
		if !strings.Contains(sql, name) {
			t.Errorf("0006_create_user_sessions.sql must define %s", name)
		}
	}
	if !strings.Contains(sql, "char_length(user_agent) <= 400") {
		t.Errorf("0006_create_user_sessions.sql must bound user_agent by %d characters", MaxUserAgentLength)
	}
	// Sessions are hard deleted, never soft deleted: a revoked row stays visible
	// for the audit trail instead.
	if strings.Contains(sql, "deleted_at") {
		t.Error("0006_create_user_sessions.sql must not soft delete sessions")
	}
}

func TestSessionLifecycleHelpers(t *testing.T) {
	now := fixedNow
	session := &Session{CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(time.Hour)}

	if !session.IsLive(now) || session.IsExpired(now) || session.IsRevoked() {
		t.Error("a fresh session must be live, not expired and not revoked")
	}

	expired := &Session{CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if !expired.IsExpired(now.Add(2 * time.Minute)) {
		t.Error("a session past its expiry must be reported as expired")
	}
	if expired.IsLive(now.Add(2 * time.Minute)) {
		t.Error("an expired session must not be live")
	}

	reason := RevocationLogout
	revokedAt := now.Add(time.Minute)
	revoked := &Session{
		CreatedAt:     now,
		LastUsedAt:    now,
		ExpiresAt:     now.Add(time.Hour),
		RevokedAt:     &revokedAt,
		RevokedReason: &reason,
	}
	if !revoked.IsRevoked() || revoked.IsLive(now) {
		t.Error("a revoked session must never be live")
	}
}

func TestNewSessionStoresOnlyADigest(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.New(), RoleStoreAdmin)
	token, digest, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}

	session, err := NewSession(user, digest, testRefreshTTL, "  Mozilla/5.0  ", "203.0.113.7", fixedNow)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if session.RefreshTokenHash == token {
		t.Error("the session must store the digest, never the token")
	}
	if !TokenDigest(session.RefreshTokenHash).Matches(token) {
		t.Error("the stored digest must match the issued token")
	}
	if session.UserAgent == nil || *session.UserAgent != "Mozilla/5.0" {
		t.Errorf("user agent = %v, want the trimmed value", session.UserAgent)
	}
	if session.IP == nil || *session.IP != "203.0.113.7" {
		t.Errorf("ip = %v, want the caller address", session.IP)
	}
	if session.ExpiresAt.Sub(session.CreatedAt) != testRefreshTTL {
		t.Errorf("expiry = %s, want created_at + %s", session.ExpiresAt, testRefreshTTL)
	}
	if session.TenantIDValue() != user.TenantIDValue() || session.StoreIDValue() != user.StoreIDValue() {
		t.Error("the session must snapshot the tenant and store of the account")
	}
	if session.LastUsedAt != session.CreatedAt {
		t.Error("last_used_at must start at created_at")
	}
	if session.CreatedAt.Location() != time.UTC {
		t.Error("the session timestamps must be UTC")
	}
}

func TestSessionValidateMirrorsTheConstraints(t *testing.T) {
	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	_, digest, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	base := func(t *testing.T) *Session {
		t.Helper()

		session, newErr := NewSession(user, digest, testRefreshTTL, "test-agent", "203.0.113.7", fixedNow)
		if newErr != nil {
			t.Fatalf("NewSession() error = %v", newErr)
		}
		return session
	}

	if err := base(t).Validate(); err != nil {
		t.Fatalf("Validate() error = %v for a valid session", err)
	}

	reason := RevocationLogout
	unknownReason := "timeout"
	late := fixedNow.Add(time.Minute)
	early := fixedNow.Add(-time.Minute)

	cases := []struct {
		name   string
		mutate func(*Session)
	}{
		{"missing user", func(s *Session) { s.UserID = uuid.Nil }},
		{"plaintext refresh token", func(s *Session) { s.RefreshTokenHash = "not-a-digest" }},
		{"empty digest", func(s *Session) { s.RefreshTokenHash = "" }},
		{"missing creation time", func(s *Session) { s.CreatedAt = time.Time{} }},
		{"expiry not after creation", func(s *Session) { s.ExpiresAt = s.CreatedAt }},
		{"reason without revocation", func(s *Session) { s.RevokedReason = &reason }},
		{"revocation without reason", func(s *Session) { s.RevokedAt = &late }},
		{"revocation before creation", func(s *Session) { s.RevokedAt = &early; s.RevokedReason = &reason }},
		{"undocumented revocation reason", func(s *Session) { s.RevokedAt = &late; s.RevokedReason = &unknownReason }},
		{"oversized user agent", func(s *Session) { agent := strings.Repeat("x", MaxUserAgentLength+1); s.UserAgent = &agent }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := base(t)
			tc.mutate(session)
			if err := session.Validate(); err == nil {
				t.Fatal("Validate() = nil error, want a validation error")
			}
		})
	}
}

func TestNewSessionValidatesItsInput(t *testing.T) {
	_, digest, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	if _, err := NewSession(nil, digest, testRefreshTTL, "", "", fixedNow); err == nil {
		t.Error("NewSession(nil user) = nil error, want a validation error")
	}

	user := newTestUser(t, uuid.New(), uuid.Nil, RoleStoreAdmin)
	if _, err := NewSession(user, digest, 0, "", "", fixedNow); err == nil {
		t.Error("NewSession(zero TTL) = nil error, want a validation error")
	}
}

func TestSessionNormalizeBoundsTheUserAgent(t *testing.T) {
	session := &Session{
		RefreshTokenHash: DigestToken("token"),
		ExpiresAt:        fixedNow.Add(time.Hour),
	}
	session.Normalize()

	if session.ID == uuid.Nil {
		t.Error("Normalize() must assign an id")
	}
	if session.CreatedAt.IsZero() || session.LastUsedAt.IsZero() {
		t.Error("Normalize() must assign the timestamps")
	}
	if session.UserAgent != nil || session.IP != nil {
		t.Error("Normalize() must turn empty values into NULL")
	}

	agent := strings.Repeat("x", MaxUserAgentLength+10)
	session.UserAgent = &agent
	session.Normalize()
	if session.UserAgent == nil || len(*session.UserAgent) != MaxUserAgentLength {
		t.Errorf("user agent length = %d, want %d", len(pointerValue(session.UserAgent)), MaxUserAgentLength)
	}
}

func TestRefreshTokensAreUniqueAndHashed(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		token, digest, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("NewRefreshToken() error = %v", err)
		}
		if seen[token] {
			t.Fatal("NewRefreshToken() returned a duplicate token")
		}
		seen[token] = true

		if token == string(digest) {
			t.Fatal("the digest must not be the token itself")
		}
		if !digest.IsValid() {
			t.Errorf("digest %q is not a sha256 hex value", digest)
		}
		if !digest.Matches(token) {
			t.Error("Matches() = false for the token the digest was built from")
		}
		if digest.Matches(token + "x") {
			t.Error("Matches() = true for a different token")
		}
	}
	if TokenDigest("short").IsValid() {
		t.Error("IsValid() = true for a malformed digest")
	}
	if !TokenDigest(DigestToken("anything")).IsValid() {
		t.Error("IsValid() = false for a valid sha256 hex digest")
	}
}
