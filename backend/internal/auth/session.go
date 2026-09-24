package auth

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Session lifetimes and revocation reasons.
const (
	// RevocationLogout is the reason stamped by POST /auth/logout.
	RevocationLogout = "logout"
	// RevocationRevoked is the reason stamped by an administrative revocation.
	RevocationRevoked = "revoked"

	// MaxUserAgentLength bounds the stored User-Agent (mirrors the CHECK
	// constraint of migration 0006).
	MaxUserAgentLength = 400
)

// RevocationReasons lists the documented values.
func RevocationReasons() []string { return []string{RevocationLogout, RevocationRevoked} }

// Session is the GORM entity of user_sessions (migrations/0006). It carries the
// hashed refresh token and the revocation state behind one access token.
type Session struct {
	ID               uuid.UUID  `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID           uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	TenantID         *uuid.UUID `gorm:"column:tenant_id;type:uuid"`
	StoreID          *uuid.UUID `gorm:"column:store_id;type:uuid"`
	RefreshTokenHash string     `gorm:"column:refresh_token_hash;not null" json:"-"`
	UserAgent        *string    `gorm:"column:user_agent"`
	IP               *string    `gorm:"column:ip;type:inet"`
	CreatedAt        time.Time  `gorm:"column:created_at;not null"`
	LastUsedAt       time.Time  `gorm:"column:last_used_at;not null"`
	ExpiresAt        time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt        *time.Time `gorm:"column:revoked_at"`
	RevokedReason    *string    `gorm:"column:revoked_reason"`
}

// TableName implements gorm.Tabler.
func (Session) TableName() string { return "user_sessions" }

// IsRevoked reports whether the session was logged out or revoked.
func (s *Session) IsRevoked() bool { return s != nil && s.RevokedAt != nil }

// IsExpired reports whether the session passed its refresh expiry.
func (s *Session) IsExpired(now time.Time) bool { return s != nil && !now.Before(s.ExpiresAt) }

// IsLive reports whether the session can still authenticate a request.
func (s *Session) IsLive(now time.Time) bool {
	return s != nil && !s.IsRevoked() && !s.IsExpired(now)
}

// TenantIDValue returns the snapshotted tenant id or the nil UUID.
func (s *Session) TenantIDValue() uuid.UUID {
	if s == nil || s.TenantID == nil {
		return uuid.Nil
	}
	return *s.TenantID
}

// StoreIDValue returns the snapshotted store id or the nil UUID.
func (s *Session) StoreIDValue() uuid.UUID {
	if s == nil || s.StoreID == nil {
		return uuid.Nil
	}
	return *s.StoreID
}

// NewSession opens a session for a user. The tenant/store scope is snapshotted
// from the user row, so a session always describes the scope the user had at
// login time; the middleware still re-reads the current scope from the user.
func NewSession(user *User, digest TokenDigest, ttl time.Duration, userAgent, ip string, now time.Time) (*Session, error) {
	if user == nil || user.ID == uuid.Nil {
		return nil, newValidationError(fieldError("user", "is required"))
	}
	if ttl <= 0 {
		return nil, newValidationError(fieldError("expires_at", "must be in the future"))
	}

	createdAt := now.UTC()
	session := &Session{
		ID:               uuid.New(),
		UserID:           user.ID,
		TenantID:         cloneUUID(user.TenantID),
		StoreID:          cloneUUID(user.StoreID),
		RefreshTokenHash: string(digest),
		CreatedAt:        createdAt,
		LastUsedAt:       createdAt,
		ExpiresAt:        createdAt.Add(ttl),
		UserAgent:        trimmedOrNil(userAgent, MaxUserAgentLength),
		IP:               trimmedOrNil(ip, 0),
	}
	session.Normalize()
	if err := session.Validate(); err != nil {
		return nil, err
	}
	return session, nil
}

// Normalize applies the canonical form (trimmed user agent, empty values nil) —
// idempotent.
func (s *Session) Normalize() {
	if s == nil {
		return
	}
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.LastUsedAt.IsZero() {
		s.LastUsedAt = s.CreatedAt
	}
	s.UserAgent = trimmedOrNil(pointerValue(s.UserAgent), MaxUserAgentLength)
	s.IP = trimmedOrNil(pointerValue(s.IP), 0)
}

// Validate mirrors the database constraints of migration 0006 so a bad session
// fails with a domain error instead of a raw driver error.
func (s *Session) Validate() error {
	if s == nil {
		return newValidationError(fieldError("session", "is required"))
	}

	fields := make([]FieldError, 0, 4)

	if s.ID == uuid.Nil {
		fields = append(fields, fieldError("id", "is required"))
	}
	if s.UserID == uuid.Nil {
		fields = append(fields, fieldError("user_id", "is required"))
	}
	if !TokenDigest(s.RefreshTokenHash).IsValid() {
		fields = append(fields, fieldError("refresh_token_hash", "must be a sha256 hex digest"))
	}
	if s.CreatedAt.IsZero() {
		fields = append(fields, fieldError("created_at", "is required"))
	}
	if !s.ExpiresAt.After(s.CreatedAt) {
		fields = append(fields, fieldError("expires_at", "must be after created_at"))
	}
	if s.UserAgent != nil && len(*s.UserAgent) > MaxUserAgentLength {
		fields = append(fields, fieldError("user_agent", "must be %d characters or fewer", MaxUserAgentLength))
	}
	switch {
	case s.RevokedAt == nil && s.RevokedReason != nil:
		fields = append(fields, fieldError("revoked_reason", "must be empty unless the session is revoked"))
	case s.RevokedAt != nil && s.RevokedReason == nil:
		fields = append(fields, fieldError("revoked_reason", "is required for a revoked session"))
	case s.RevokedAt != nil && !validRevocationReason(s.RevokedReason):
		fields = append(fields, fieldError("revoked_reason", "must be one of %s", strings.Join(RevocationReasons(), ", ")))
	case s.RevokedAt != nil && s.RevokedAt.Before(s.CreatedAt):
		fields = append(fields, fieldError("revoked_at", "must not precede created_at"))
	}

	if len(fields) == 0 {
		return nil
	}
	return newValidationError(fields...)
}

// validRevocationReason reports whether a revocation reason is documented.
func validRevocationReason(reason *string) bool {
	if reason == nil {
		return false
	}
	for _, candidate := range RevocationReasons() {
		if *reason == candidate {
			return true
		}
	}
	return false
}

// trimmedOrNil trims a value, truncates it to limit (when > 0) and returns nil
// for an empty result.
func trimmedOrNil(value string, limit int) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if limit > 0 && len(trimmed) > limit {
		trimmed = trimmed[:limit]
	}
	return &trimmed
}

// pointerValue dereferences a *string without panicking.
func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
