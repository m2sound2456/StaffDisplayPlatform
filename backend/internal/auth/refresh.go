package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Opaque credential helpers: a refresh token is a random 256 bit value, and the
// database only ever sees its sha256 digest (docs/AI_RULES.md §5.2).

// RefreshTokenBytes is the entropy of a refresh token.
const RefreshTokenBytes = 32

// TokenDigest is the storage form of an opaque credential: sha256 hex. The
// plaintext credential is never stored, logged or written to audit metadata.
type TokenDigest string

// String implements fmt.Stringer.
func (d TokenDigest) String() string { return string(d) }

// IsValid reports whether the digest has the documented shape
// (^[0-9a-f]{64}$, mirrored by the user_sessions CHECK constraint).
func (d TokenDigest) IsValid() bool {
	if len(d) != sha256.Size*2 {
		return false
	}
	for _, char := range string(d) {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

// Matches reports whether the digest belongs to token, in constant time.
func (d TokenDigest) Matches(token string) bool {
	return constantTimeEqual([]byte(d), []byte(DigestToken(token)))
}

// DigestToken returns the storage digest of an opaque token.
func DigestToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewRefreshToken returns a fresh opaque refresh token and its storage digest.
// The token is base64url without padding, so it needs no escaping in a JSON
// body, a header or a cookie.
func NewRefreshToken() (token string, digest TokenDigest, err error) {
	raw := make([]byte, RefreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, TokenDigest(DigestToken(token)), nil
}

// constantTimeEqual compares two values without leaking their contents through
// timing.
func constantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
