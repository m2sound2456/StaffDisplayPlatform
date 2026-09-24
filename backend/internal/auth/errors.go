package auth

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors of the authentication domain. The HTTP layer maps them to the
// API envelope (docs/API.md §1): unauthenticated → 401, account/role → 403,
// validation → 422, conflict → 409, unknown target → 404.
//
// The token sentinels deliberately carry no detail: a diagnostic that echoes the
// offending token, its payload or the signing key would be a leak. Callers use
// ReasonOf to obtain a short, fixed vocabulary for logs and audit metadata.
var (
	// ErrNotConfigured reports a service or repository built without its
	// dependency (nil wiring) instead of panicking at request time.
	ErrNotConfigured = errors.New("auth: service is not configured")
	// ErrValidation wraps field level validation failures (422).
	ErrValidation = errors.New("auth: invalid input")
	// ErrInvalidCredentials reports a wrong e-mail/password pair (401). It is
	// also returned for an unknown account so a login cannot enumerate users.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrAccountInactive reports a correct password on a disabled account (403).
	ErrAccountInactive = errors.New("auth: account is not active")
	// ErrUnauthenticated reports a missing, unrecoverable or revoked credential
	// (401): the caller cannot be identified at all.
	ErrUnauthenticated = errors.New("auth: unauthenticated")
	// ErrForbidden reports an authenticated caller outside its authorization
	// boundary (403), e.g. a wrong role or a resource of another tenant.
	ErrForbidden = errors.New("auth: not permitted")

	// ErrTokenMalformed reports a token that is not a well formed JWS (401).
	ErrTokenMalformed = errors.New("auth: token is malformed")
	// ErrTokenSignature reports a signature that matches no configured signing
	// key (401): the current key and the whole rotation window were tried.
	ErrTokenSignature = errors.New("auth: token signature does not match any configured signing key")
	// ErrTokenExpired reports an expired token (401).
	ErrTokenExpired = errors.New("auth: token has expired")
	// ErrTokenNotYetValid reports a token used before its nbf (401).
	ErrTokenNotYetValid = errors.New("auth: token is not valid yet")
	// ErrTokenInvalid reports a well formed token with unusable claims (401):
	// wrong issuer, missing exp/sid/sub, an unparsable id or an unknown role.
	ErrTokenInvalid = errors.New("auth: token claims are not valid")

	// ErrSessionNotFound reports an unknown or rotated-away session (401).
	ErrSessionNotFound = errors.New("auth: session not found")
	// ErrSessionRevoked reports a session ended by logout or revocation (401).
	ErrSessionRevoked = errors.New("auth: session has been revoked")
	// ErrSessionExpired reports a session past its refresh expiry (401).
	ErrSessionExpired = errors.New("auth: session has expired")

	// ErrNotFound is returned for unknown ids *and* for users outside the
	// caller's tenant scope: cross-tenant access never discloses existence
	// (BLUEPRINT §3, FG2 convention).
	ErrNotFound = errors.New("auth: not found")
	// ErrEmailTaken reports a duplicate (live) e-mail address.
	ErrEmailTaken = errors.New("auth: e-mail address is already in use")
	// ErrTenantNotFound reports that the tenant scope does not exist.
	ErrTenantNotFound = errors.New("auth: tenant not found")
	// ErrConflict reports any other constraint conflict.
	ErrConflict = errors.New("auth: conflicting write")
	// ErrMissingTenantScope protects the tenant isolation invariant: a
	// tenant-scoped repository call without a tenant scope is a programming
	// error, never an unscoped query.
	ErrMissingTenantScope = errors.New("auth: tenant scope is required")
)

// FieldError is one validation failure of a single field.
type FieldError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e FieldError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// ValidationError aggregates every field level problem of one payload so the API
// can answer a single 422 with all details (docs/API.md §1). It mirrors
// store.ValidationError so the HTTP layer treats both domains the same way.
type ValidationError struct {
	Fields []FieldError
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return ErrValidation.Error()
	}
	parts := make([]string, 0, len(e.Fields))
	for _, field := range e.Fields {
		parts = append(parts, field.Error())
	}
	return ErrValidation.Error() + ": " + strings.Join(parts, "; ")
}

// Unwrap makes errors.Is(err, ErrValidation) work for callers.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// FieldMessages returns the failures keyed by field name, ready to be rendered
// as the "details" object of an error envelope.
func (e *ValidationError) FieldMessages() map[string]string {
	if e == nil {
		return nil
	}
	details := make(map[string]string, len(e.Fields))
	for _, field := range e.Fields {
		details[field.Field] = field.Message
	}
	return details
}

// newValidationError builds a ValidationError; used by the domain validators.
func newValidationError(fields ...FieldError) *ValidationError {
	return &ValidationError{Fields: fields}
}

// fieldError is a tiny constructor that keeps the validators readable.
func fieldError(field, format string, args ...any) FieldError {
	return FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}
