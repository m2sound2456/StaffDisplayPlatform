package store

import (
	"strings"
	"testing"
)

func TestNormalizeSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"  My  Coffee Shop!! ", "my-coffee-shop"},
		{"--abc--", "abc"},
		{"Coffee", "coffee"},
		{"abc--def", "abc-def"},
		{"abc-123", "abc-123"},
		{"", ""},
		{"   ", ""},
		{"!!!", ""},
		{strings.Repeat("A", 64), strings.Repeat("a", 64)},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := NormalizeSlug(tc.input); got != tc.want {
				t.Errorf("NormalizeSlug(%q) = %q, want %q", tc.input, got, tc.want)
			}
			// Normalising twice must be a no-op (idempotent, safe to re-run).
			if got := NormalizeSlug(tc.want); got != tc.want {
				t.Errorf("NormalizeSlug is not idempotent for %q: %q", tc.want, got)
			}
		})
	}
}

func TestValidateSlugAcceptsValidSlugs(t *testing.T) {
	valid := []string{
		"a",
		"abc",
		"coffee",
		"shop001",
		"abc-123",
		"a-b-c",
		"a" + strings.Repeat("b", 61) + "c", // 63 characters
	}

	for _, slug := range valid {
		t.Run(slug, func(t *testing.T) {
			if err := ValidateSlug(slug); err != nil {
				t.Errorf("ValidateSlug(%q) = %v, want nil", slug, err)
			}
		})
	}
}

func TestValidateSlugRejectsInvalidSlugs(t *testing.T) {
	cases := []struct {
		name    string
		slug    string
		wantMsg string
	}{
		{"empty", "", "is required"},
		{"blank", "   ", "is required"},
		{"uppercase", "ABC", "must be lowercase"},
		{"space", "coffee shop", "only contain lowercase letters"},
		{"leading hyphen", "-abc", "only contain lowercase letters"},
		{"trailing hyphen", "abc-", "only contain lowercase letters"},
		{"dot", ".abc", "only contain lowercase letters"},
		{"underscore", "a_b", "only contain lowercase letters"},
		{"too long", strings.Repeat("a", 64), "characters or fewer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.slug)
			if err == nil {
				t.Fatalf("ValidateSlug(%q) = nil, want an error", tc.slug)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("ValidateSlug(%q) = %q, want it to mention %q", tc.slug, err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestValidateSlugRejectsReservedPlatformPaths(t *testing.T) {
	if len(ReservedSlugs) == 0 {
		t.Fatal("ReservedSlugs must not be empty")
	}

	for _, slug := range ReservedSlugs {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, want a reserved slug error", slug)
		} else if !strings.Contains(err.Error(), "reserved") {
			t.Errorf("ValidateSlug(%q) = %q, want it to mention reserved", slug, err.Error())
		}
	}
}

func TestIsReservedSlugIsCaseInsensitive(t *testing.T) {
	cases := map[string]bool{
		"app":     true,
		"APP":     true,
		" Setup ": true,
		"coffee":  false,
		"":        false,
	}

	for slug, want := range cases {
		if got := IsReservedSlug(slug); got != want {
			t.Errorf("IsReservedSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

func TestSlugPatternRejectsReservedLookalikes(t *testing.T) {
	// Guard against an accidental pattern change that would suddenly accept
	// platform paths such as "s" (the display prefix) as a store slug.
	for _, slug := range []string{"s", "admin", "store"} {
		if IsReservedSlug(slug) {
			t.Errorf("%q must not be a reserved slug", slug)
		}
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", slug, err)
		}
	}
}

func TestTimezoneValidation(t *testing.T) {
	valid := []string{"UTC", "Asia/Bangkok", "America/Argentina/Buenos_Aires", "Etc/GMT+7", "America/New_York"}
	for _, timezone := range valid {
		if err := timezoneError(timezone); err != nil {
			t.Errorf("timezoneError(%q) = %v, want nil", timezone, err)
		}
	}

	invalid := []string{"", " ", "1Asia/Bangkok", "Asia//Bangkok", "Asia/Bangkok ", strings.Repeat("A", 65), "Asia Bkk"}
	for _, timezone := range invalid {
		if err := timezoneError(timezone); err == nil {
			t.Errorf("timezoneError(%q) = nil, want an error", timezone)
		}
	}
}
