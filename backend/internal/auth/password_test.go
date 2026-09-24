package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// strongPassword is a policy compliant test credential. It is never a real one.
const strongPassword = "correct-horse-battery-staple"

func TestHashPasswordProducesBcryptDigest(t *testing.T) {
	hash, err := HashPassword(strongPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == strongPassword {
		t.Fatal("the stored value must never be the plaintext password")
	}
	if !IsPasswordHash(hash) {
		t.Errorf("IsPasswordHash(%q) = false, want true", hash)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash = %q, want a bcrypt digest", hash)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost() error = %v", err)
	}
	if cost != BcryptCost {
		t.Errorf("bcrypt cost = %d, want %d", cost, BcryptCost)
	}
}

func TestHashPasswordIsSaltedPerCall(t *testing.T) {
	first, err := HashPassword(strongPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	second, err := HashPassword(strongPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if first == second {
		t.Error("two hashes of the same password must differ (each hash carries its own salt)")
	}
	if !VerifyPassword(first, strongPassword) || !VerifyPassword(second, strongPassword) {
		t.Error("both hashes must verify the password they were built from")
	}
}

func TestVerifyPasswordAcceptsOnlyTheRightPassword(t *testing.T) {
	hash, err := HashPassword(strongPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !VerifyPassword(hash, strongPassword) {
		t.Error("VerifyPassword() = false for the correct password")
	}
	if VerifyPassword(hash, strongPassword+"x") {
		t.Error("VerifyPassword() = true for a wrong password")
	}
	if VerifyPassword(hash, "") {
		t.Error("VerifyPassword() = true for an empty password")
	}
}

func TestVerifyPasswordRejectsUnusableHashes(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"plaintext":  strongPassword,
		"truncated":  "$2a$12$abcdefghijklmnopqrstuv",
		"wrong cost": "$2a$99$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123",
	}
	for name, hash := range cases {
		if VerifyPassword(hash, strongPassword) {
			t.Errorf("%s: VerifyPassword() = true, want false (a bad hash is never an authentication)", name)
		}
		if VerifyPasswordConstantTime(hash, strongPassword) {
			t.Errorf("%s: VerifyPasswordConstantTime() = true, want false", name)
		}
	}
}

func TestVerifyPasswordConstantTimeAnswersFalseWithoutHash(t *testing.T) {
	// The empty hash path still performs bcrypt work, so an unknown account and
	// a wrong password take a comparable amount of time; the result is always
	// false.
	if VerifyPasswordConstantTime("", strongPassword) {
		t.Error("VerifyPasswordConstantTime(\"\") = true, want false")
	}
}

func TestIsPasswordHashMirrorsTheDigestShape(t *testing.T) {
	hash, err := HashPassword(strongPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !IsPasswordHash(hash) {
		t.Errorf("IsPasswordHash(%q) = false, want true", hash)
	}
	rejects := []string{
		"",
		"plaintext-password-123",
		hash[:len(hash)-1],                  // too short
		hash + "x",                          // too long
		"$2a$12$" + strings.Repeat("*", 53), // wrong alphabet
		"$2x$12$" + hash[7:],                // unknown version marker
		"$2a$1x$" + hash[7:],                // non numeric cost
	}
	for _, value := range rejects {
		if IsPasswordHash(value) {
			t.Errorf("IsPasswordHash(%q) = true, want false", value)
		}
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"empty", "", true},
		{"too short", strings.Repeat("a", MinPasswordLength-1), true},
		{"minimum length", strings.Repeat("a", MinPasswordLength), false},
		{"long but within bcrypt", strings.Repeat("a", MaxPasswordLength), false},
		{"beyond bcrypt limit", strings.Repeat("a", MaxPasswordLength+1), true},
		{"multi byte beyond byte limit", strings.Repeat("ก", MaxPasswordLength), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePasswordPolicy(tc.password)
			if tc.wantErr && err == nil {
				t.Fatal("ValidatePasswordPolicy() = nil, want an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidatePasswordPolicy() = %v, want nil", err)
			}
			if tc.wantErr && err != nil && strings.Contains(err.Error(), tc.password) && tc.password != "" {
				t.Error("the validation message must not echo the password")
			}
		})
	}
}

func TestHashPasswordRejectsAnOutOfPolicyPassword(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("HashPassword() = nil error for a password below the policy minimum")
	}
}
