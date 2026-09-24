package store

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Status is the lifecycle state of a store. Stored as text with a matching DB
// CHECK constraint (migrations/0003_create_stores.sql), so adding a value is a
// forward migration instead of a PostgreSQL enum rewrite.
type Status string

// Store status values. Only StatusActive is meant to serve the display
// (/s/{slug}); the routing decision itself belongs to FG5/FG10.
const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
	StatusArchived Status = "archived"
)

// Valid reports whether the status is one of the documented values.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusInactive, StatusArchived:
		return true
	default:
		return false
	}
}

// AllStatuses lists the documented status values (validation messages, admin UI).
func AllStatuses() []Status {
	return []Status{StatusActive, StatusInactive, StatusArchived}
}

// Store is the GORM entity of the stores table (migrations/0003).
//
// Device identity is deliberately absent: a device has its own identity
// (device_id + hashed token) and its own display configuration — stand or
// handheld, 1/4/8/12 items per page, auto slide, slide interval, loop and
// employee status visibility — and is bound to a store only through store_id
// (FG16–FG21).
type Store struct {
	ID           uuid.UUID       `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	TenantID     uuid.UUID       `gorm:"column:tenant_id;type:uuid;not null;index"`
	Name         string          `gorm:"column:name;not null"`
	Slug         string          `gorm:"column:slug;type:citext;not null"`
	LogoURL      *string         `gorm:"column:logo_url"`
	Status       Status          `gorm:"column:status;not null;default:active"`
	Timezone     string          `gorm:"column:timezone;not null;default:UTC"`
	OpeningHours json.RawMessage `gorm:"column:opening_hours;type:jsonb;not null;default:{}"`
	CreatedAt    time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time       `gorm:"column:updated_at;not null"`
	DeletedAt    gorm.DeletedAt  `gorm:"column:deleted_at"`
}

// TableName implements gorm.Tabler.
func (Store) TableName() string { return "stores" }

// IsDeleted reports whether the store was soft deleted. Deleted stores are
// invisible to every repository query but keep their rows and audit trail.
func (s *Store) IsDeleted() bool { return s != nil && s.DeletedAt.Valid }

// CreateInput is the admin supplied payload for a new store (FG5 wires the HTTP
// layer on top of it). Tenant scoping comes from the caller's Scope, never from
// this struct.
type CreateInput struct {
	Name         string
	Slug         string
	LogoURL      string
	Status       Status
	Timezone     string
	OpeningHours json.RawMessage
}

// New builds a validated store for the given tenant scope. The slug is
// normalised first, defaults are applied (status, timezone, opening hours) and
// the result is validated exactly as the database validates it.
func New(scope Scope, in CreateInput) (*Store, error) {
	if err := ensureTenantScope(scope); err != nil {
		return nil, err
	}

	s := &Store{
		TenantID:     scope.TenantID,
		Name:         in.Name,
		Slug:         in.Slug,
		Status:       in.Status,
		Timezone:     in.Timezone,
		OpeningHours: cloneOpeningHours(in.OpeningHours),
	}
	if logo := strings.TrimSpace(in.LogoURL); logo != "" {
		s.LogoURL = &logo
	}

	s.prepareForCreate()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Normalize applies the canonical form of every input field: trimmed name and
// timezone, normalised slug, defaulted status/timezone/opening hours.
func (s *Store) Normalize() {
	if s == nil {
		return
	}

	s.Name = strings.TrimSpace(s.Name)
	s.Slug = NormalizeSlug(s.Slug)

	s.Timezone = strings.TrimSpace(s.Timezone)
	if s.Timezone == "" {
		s.Timezone = DefaultTimezone
	}
	if s.Status == "" {
		s.Status = StatusActive
	}
	if len(s.OpeningHours) == 0 || string(s.OpeningHours) == "null" {
		s.OpeningHours = json.RawMessage(DefaultOpeningHours)
	}

	if logo := strings.TrimSpace(derefString(s.LogoURL)); logo == "" {
		s.LogoURL = nil
	} else {
		s.LogoURL = &logo
	}
}

// Validate mirrors the database constraints so a bad payload fails with a field
// level 422 instead of a raw driver error.
func (s *Store) Validate() error {
	if s == nil {
		return newValidationError(fieldError("store", "is required"))
	}

	fields := make([]FieldError, 0, 6)

	if s.ID == uuid.Nil {
		fields = append(fields, fieldError("id", "is required"))
	}
	if s.TenantID == uuid.Nil {
		fields = append(fields, fieldError("tenant_id", "is required"))
	}

	name := strings.TrimSpace(s.Name)
	switch {
	case name == "":
		fields = append(fields, fieldError("name", "is required"))
	case len([]rune(name)) > MaxNameLength:
		fields = append(fields, fieldError("name", "must be %d characters or fewer", MaxNameLength))
	}

	if err := slugError(s.Slug); err != nil {
		fields = append(fields, FieldError{Field: "slug", Message: err.Error()})
	}

	if !s.Status.Valid() {
		fields = append(fields, fieldError("status", "must be one of %s", joinStatuses()))
	}

	if err := timezoneError(s.Timezone); err != nil {
		fields = append(fields, FieldError{Field: "timezone", Message: err.Error()})
	}

	if err := logoURLError(s.LogoURL); err != nil {
		fields = append(fields, FieldError{Field: "logo_url", Message: err.Error()})
	}

	if err := openingHoursError(s.OpeningHours); err != nil {
		fields = append(fields, FieldError{Field: "opening_hours", Message: err.Error()})
	}

	if len(fields) == 0 {
		return nil
	}
	return newValidationError(fields...)
}

// prepareForCreate applies defaults and canonical form to a store that is about
// to be inserted (idempotent; also used by New).
func (s *Store) prepareForCreate() {
	if s == nil {
		return
	}
	s.Normalize()

	now := time.Now().UTC()
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
}

// logoURLError validates the optional logo reference.
func logoURLError(value *string) error {
	logo := strings.TrimSpace(derefString(value))
	if logo == "" {
		return nil
	}
	if len([]rune(logo)) > MaxLogoURLLength {
		return fieldError("logo_url", "must be %d characters or fewer", MaxLogoURLLength)
	}
	return nil
}

// openingHoursError requires a JSON object (the DB enforces jsonb_typeof = 'object').
func openingHoursError(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		// Normalize already replaced this with "{}".
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return errors.New("must be a JSON object")
	}
	return nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func joinStatuses() string {
	values := AllStatuses()
	parts := make([]string, 0, len(values))
	for _, status := range values {
		parts = append(parts, string(status))
	}
	return strings.Join(parts, ", ")
}

func cloneOpeningHours(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
