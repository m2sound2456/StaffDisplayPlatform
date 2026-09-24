package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewAppliesDefaultsAndNormalises(t *testing.T) {
	tenantID := uuid.New()

	created, err := New(TenantScope(tenantID), CreateInput{
		Name:         "  My Coffee  ",
		Slug:         "  My Coffee!! ",
		LogoURL:      "  /media/logo.png ",
		OpeningHours: json.RawMessage(`{"mon":"09:00-18:00"}`),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if created.TenantID != tenantID {
		t.Errorf("TenantID = %s, want the scope tenant %s", created.TenantID, tenantID)
	}
	if created.Name != "My Coffee" {
		t.Errorf("Name = %q, want %q", created.Name, "My Coffee")
	}
	if created.Slug != "my-coffee" {
		t.Errorf("Slug = %q, want %q", created.Slug, "my-coffee")
	}
	if created.Status != StatusActive {
		t.Errorf("Status = %q, want %q", created.Status, StatusActive)
	}
	if created.Timezone != DefaultTimezone {
		t.Errorf("Timezone = %q, want %q", created.Timezone, DefaultTimezone)
	}
	if string(created.OpeningHours) != `{"mon":"09:00-18:00"}` {
		t.Errorf("OpeningHours = %s, want the supplied document", created.OpeningHours)
	}
	if created.LogoURL == nil || *created.LogoURL != "/media/logo.png" {
		t.Errorf("LogoURL = %v, want /media/logo.png", created.LogoURL)
	}
	if created.ID == uuid.Nil {
		t.Error("ID must be assigned by New()")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps must be assigned by New()")
	}
	if created.IsDeleted() {
		t.Error("a new store must not be marked deleted")
	}
	if got := created.TableName(); got != "stores" {
		t.Errorf("TableName() = %q, want stores", got)
	}
}

func TestNewDefaultsOpeningHoursToEmptyObject(t *testing.T) {
	created, err := New(TenantScope(uuid.New()), CreateInput{Name: "Shop", Slug: "shop"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if string(created.OpeningHours) != DefaultOpeningHours {
		t.Errorf("OpeningHours = %s, want %s", created.OpeningHours, DefaultOpeningHours)
	}
	if created.LogoURL != nil {
		t.Errorf("LogoURL = %v, want nil for a blank input", created.LogoURL)
	}
}

func TestNewRequiresTenantScope(t *testing.T) {
	_, err := New(Scope{}, CreateInput{Name: "Shop", Slug: "shop"})
	if !errors.Is(err, ErrMissingTenantScope) {
		t.Fatalf("New() error = %v, want ErrMissingTenantScope", err)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name  string
		input CreateInput
		field string
	}{
		{"empty name", CreateInput{Name: "  ", Slug: "shop"}, "name"},
		{"name too long", CreateInput{Name: strings.Repeat("n", MaxNameLength+1), Slug: "shop"}, "name"},
		{"slug empty after normalising", CreateInput{Name: "Shop", Slug: "!!!"}, "slug"},
		{"reserved slug", CreateInput{Name: "Shop", Slug: "app"}, "slug"},
		{"bad timezone", CreateInput{Name: "Shop", Slug: "shop", Timezone: "Asia Bkk"}, "timezone"},
		{"opening hours not an object", CreateInput{Name: "Shop", Slug: "shop", OpeningHours: json.RawMessage(`[1,2]`)}, "opening_hours"},
		{"unknown status", CreateInput{Name: "Shop", Slug: "shop", Status: Status("closed")}, "status"},
		{"logo url too long", CreateInput{Name: "Shop", Slug: "shop", LogoURL: strings.Repeat("l", MaxLogoURLLength+1)}, "logo_url"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(TenantScope(uuid.New()), tc.input)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %v, want *ValidationError", err)
			}
			if _, ok := validationErr.FieldMessages()[tc.field]; !ok {
				t.Errorf("field %q missing from %v", tc.field, validationErr.FieldMessages())
			}
		})
	}
}

func TestValidateReportsEveryFieldProblem(t *testing.T) {
	s := &Store{
		Name:         "",
		Slug:         "-bad-",
		Status:       Status("closed"),
		Timezone:     "",
		OpeningHours: json.RawMessage(`"not-an-object"`),
	}

	err := s.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want *ValidationError", err)
	}

	fields := validationErr.FieldMessages()
	for _, want := range []string{"id", "tenant_id", "name", "slug", "status", "timezone", "opening_hours"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("field %q missing from %v", want, fields)
		}
	}
	if err.Error() == "" || validationErr.Unwrap() != ErrValidation {
		t.Error("ValidationError must unwrap to ErrValidation and render a message")
	}
}

func TestValidateAcceptsStoreLoadedFromDatabase(t *testing.T) {
	s := &Store{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		Name:         "Shop",
		Slug:         "shop",
		Status:       StatusActive,
		Timezone:     "Asia/Bangkok",
		OpeningHours: json.RawMessage(`{}`),
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestStatusHelpers(t *testing.T) {
	for _, status := range AllStatuses() {
		if !status.Valid() {
			t.Errorf("AllStatuses() returned the invalid status %q", status)
		}
	}
	if Status("closed").Valid() {
		t.Error("Status(closed).Valid() = true, want false")
	}
	if len(AllStatuses()) != 3 {
		t.Errorf("len(AllStatuses()) = %d, want 3", len(AllStatuses()))
	}
}

func TestFieldErrorAndValidationErrorMessage(t *testing.T) {
	singular := &ValidationError{Fields: []FieldError{{Field: "slug", Message: "is required"}}}
	if got := singular.Error(); !strings.Contains(got, "slug: is required") {
		t.Errorf("Error() = %q, want it to mention the field", got)
	}

	empty := &ValidationError{}
	if got := empty.Error(); got != ErrValidation.Error() {
		t.Errorf("Error() = %q, want %q", got, ErrValidation.Error())
	}
	if len(empty.FieldMessages()) != 0 {
		t.Error("FieldMessages() of an empty error must be an empty map")
	}

	if got := (FieldError{Message: "broken"}).Error(); got != "broken" {
		t.Errorf("FieldError.Error() = %q, want broken", got)
	}
}

func TestOpeningHoursHelpersDoNotMutateInput(t *testing.T) {
	raw := json.RawMessage(`{"mon":"09:00-18:00"}`)
	cloned := cloneOpeningHours(raw)
	if string(cloned) != string(raw) {
		t.Fatalf("cloneOpeningHours = %s, want %s", cloned, raw)
	}
	cloned[2] = 'X'
	if string(raw) == string(cloned) {
		t.Error("cloneOpeningHours must return an independent copy")
	}
	if cloneOpeningHours(nil) != nil {
		t.Error("cloneOpeningHours(nil) must be nil")
	}
}
