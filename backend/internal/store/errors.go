package store

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors of the store domain. The HTTP layer (FG5) maps them to the
// API envelope: not_found / conflict / validation_failed
// (see docs/API.md §1).
var (
	// ErrNotFound is returned for unknown ids *and* for rows outside the caller's
	// tenant scope: cross-tenant access never discloses existence (BLUEPRINT §3.3).
	ErrNotFound = errors.New("store: not found")
	// ErrTenantNotFound reports that the tenant scope does not exist (FK violation).
	ErrTenantNotFound = errors.New("store: tenant not found")
	// ErrSlugTaken reports a duplicate store slug (case-insensitive, live rows only).
	ErrSlugTaken = errors.New("store: slug already in use")
	// ErrConflict reports any other uniqueness/constraint conflict.
	ErrConflict = errors.New("store: conflicting write")
	// ErrValidation wraps field level validation failures.
	ErrValidation = errors.New("store: invalid input")
	// ErrMissingTenantScope protects the tenant isolation invariant: a repository
	// call without tenant scope is a programming error, never an unscoped query.
	ErrMissingTenantScope = errors.New("store: tenant scope is required")
	// ErrNotConfigured reports a repository that was built without a database
	// handle (nil wiring), instead of panicking at query time.
	ErrNotConfigured = errors.New("store: repository is not configured")
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

// ValidationError aggregates every field level problem of one payload so the
// API can answer a single 422 with all details (docs/API.md §1).
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
