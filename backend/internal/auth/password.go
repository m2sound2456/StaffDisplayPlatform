package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Password handling (BLUEPRINT §11.5). Plaintext passwords never reach the
// database: the users.password_hash CHECK constraint only accepts a bcrypt
// digest, and this package is the only place that produces one.
const (
	// PasswordAlgorithm names the hash algorithm in diagnostics.
	PasswordAlgorithm = "bcrypt"

	// BcryptCost is the work factor of a new hash. 12 is the current
	// recommendation for a server side login: cheap for a login, expensive for
	// an offline attack.
	BcryptCost = 12

	// MinPasswordLength is the shortest accepted password. It is a policy
	// minimum for humans, not a technical limit.
	MinPasswordLength = 12

	// MaxPasswordLength is bcrypt's hard limit: input beyond 72 bytes is
	// silently ignored by the algorithm, so a longer password is rejected
	// instead of being truncated.
	MaxPasswordLength = 72

	// bcryptDigestLength is the length of a bcrypt hash: $2a$ + cost + $ + 53.
	bcryptDigestLength = 60
)

// dummyHash is the hash compared against when an account does not exist, so an
// unknown address costs the same as a wrong password (no user enumeration by
// response time). It is derived lazily and is a throwaway digest of a random
// string: it can never authenticate anybody.
var dummyHash = sync.OnceValue(func() []byte {
	fallback := []byte("$2a$12$0000000000000000000000000000000000000000000000000000000")
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return fallback
	}
	hash, err := bcrypt.GenerateFromPassword(random, BcryptCost)
	if err != nil {
		return fallback
	}
	return hash
})

// HashPassword validates the policy and returns a bcrypt digest of password.
func HashPassword(password string) (string, error) {
	if err := ValidatePasswordPolicy(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches the stored bcrypt digest. A
// malformed or empty hash is treated like a mismatch — never as an
// authentication.
func VerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// VerifyPasswordConstantTime performs comparable work even when hash is empty,
// so a caller that did not find an account does not leak that fact through the
// response time. It still reports false for an empty hash.
func VerifyPasswordConstantTime(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return false
	}
	return VerifyPassword(hash, password)
}

// IsPasswordHash reports whether value looks like a bcrypt digest. It mirrors
// the users.password_hash CHECK constraint of migration 0005, so a plaintext
// value is rejected by the domain layer *and* by the database.
func IsPasswordHash(value string) bool {
	if len(value) != bcryptDigestLength {
		return false
	}
	if !strings.HasPrefix(value, "$2a$") && !strings.HasPrefix(value, "$2b$") && !strings.HasPrefix(value, "$2y$") {
		return false
	}
	if value[6] != '$' {
		return false
	}
	for _, char := range value[4:6] {
		if char < '0' || char > '9' {
			return false
		}
	}
	for _, char := range value[7:] {
		if !isBcryptAlphabet(char) {
			return false
		}
	}
	return true
}

// isBcryptAlphabet reports whether char belongs to the base64 variant bcrypt
// uses (./A-Za-z0-9).
func isBcryptAlphabet(char rune) bool {
	switch {
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		return true
	case char == '.' || char == '/':
		return true
	default:
		return false
	}
}

// ValidatePasswordPolicy enforces the password rules and returns a field level
// error suitable for a 422 response.
func ValidatePasswordPolicy(password string) error {
	switch {
	case password == "":
		return newValidationError(fieldError("password", "is required"))
	case len([]rune(password)) < MinPasswordLength:
		return newValidationError(fieldError("password", "must be at least %d characters", MinPasswordLength))
	case len([]byte(password)) > MaxPasswordLength:
		return newValidationError(fieldError("password", "must be %d bytes or fewer", MaxPasswordLength))
	}
	return nil
}
