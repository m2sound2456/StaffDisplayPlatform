package config

import (
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// TestStoreDefaultsComeFromConfiguration pins the per-environment store policy:
// the base file sets the deployment default and the profile may tighten it.
func TestStoreDefaultsComeFromConfiguration(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)
	t.Chdir(t.TempDir())

	writeProfile(t, BaseConfigFile, strings.Join([]string{
		"store:",
		"  default_timezone: Europe/Berlin",
		"  default_status: inactive",
		"  slug:",
		"    min_length: 3",
		"    max_length: 20",
		"    auto_generate: false",
		"    extra_reserved_slugs:",
		"      - admin",
		"      - portal",
		"",
	}, "\n"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Store.DefaultTimezone != "Europe/Berlin" {
		t.Errorf("store.default_timezone = %q, want Europe/Berlin", cfg.Store.DefaultTimezone)
	}
	if cfg.Store.DefaultStatus != store.StatusInactive {
		t.Errorf("store.default_status = %q, want %q", cfg.Store.DefaultStatus, store.StatusInactive)
	}
	if cfg.Store.Slug.MinLength != 3 || cfg.Store.Slug.MaxLength != 20 {
		t.Errorf("slug bounds = %d..%d, want 3..20", cfg.Store.Slug.MinLength, cfg.Store.Slug.MaxLength)
	}
	if cfg.Store.Slug.AutoGenerate {
		t.Error("store.slug.auto_generate = true, want false")
	}
	if len(cfg.Store.Slug.ExtraReservedSlugs) != 2 {
		t.Fatalf("extra reserved slugs = %v, want two entries", cfg.Store.Slug.ExtraReservedSlugs)
	}
}

func TestStorePolicyEnvironmentOverrides(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)
	t.Chdir(t.TempDir())

	t.Setenv("STORE_DEFAULT_TIMEZONE", "Europe/Berlin")
	t.Setenv("STORE_DEFAULT_STATUS", "archived")
	t.Setenv("STORE_SLUG_MIN_LENGTH", "4")
	t.Setenv("STORE_SLUG_MAX_LENGTH", "32")
	t.Setenv("STORE_SLUG_AUTO_GENERATE", "false")
	t.Setenv("STORE_SLUG_EXTRA_RESERVED_SLUGS", "admin, ops")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Store.DefaultTimezone != "Europe/Berlin" || cfg.Store.DefaultStatus != store.StatusArchived {
		t.Errorf("store defaults = %q/%q, want Europe/Berlin/archived", cfg.Store.DefaultTimezone, cfg.Store.DefaultStatus)
	}
	if cfg.Store.Slug.MinLength != 4 || cfg.Store.Slug.MaxLength != 32 {
		t.Errorf("slug bounds = %d..%d, want 4..32", cfg.Store.Slug.MinLength, cfg.Store.Slug.MaxLength)
	}
	if cfg.Store.Slug.AutoGenerate {
		t.Error("store.slug.auto_generate = true, want the environment override false")
	}
	if len(cfg.Store.Slug.ExtraReservedSlugs) != 2 || cfg.Store.Slug.ExtraReservedSlugs[1] != "ops" {
		t.Errorf("extra reserved slugs = %v, want [admin ops]", cfg.Store.Slug.ExtraReservedSlugs)
	}
}

// TestStorePolicyCannotRelaxTheDatabaseRules protects the FG2 invariant: the
// slug pattern, the reserved platform paths and the 63 character ceiling come
// from the migration set and must not be loosened from configuration.
func TestStorePolicyAppliesTheDatabaseRuleFirst(t *testing.T) {
	policy := SlugPolicyConfig{MinLength: 3, MaxLength: 10, ExtraReservedSlugs: []string{"admin"}}

	cases := []struct {
		slug    string
		wantErr string
	}{
		{"abc", ""},
		{"ab", "at least 3"},
		{"abcdefghijk", "or fewer"},
		{"admin", "reserved by this deployment"},
		{"setup", "reserved by the platform"},
		{"ABCDE", "lowercase"},
		{"-bad-", "hyphens"},
		{"", "is required"},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			err := policy.ValidateSlug(tc.slug)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSlug(%q) error = %v", tc.slug, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateSlug(%q) must fail", tc.slug)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}

	if !policy.IsExtraReservedSlug("ADMIN") {
		t.Error("IsExtraReservedSlug() must be case-insensitive")
	}
	if policy.IsExtraReservedSlug("shop") {
		t.Error("IsExtraReservedSlug(shop) = true, want false")
	}
}

func TestStoreTimezoneOrDefault(t *testing.T) {
	cases := []struct {
		name  string
		store StoreConfig
		value string
		want  string
	}{
		{"store value wins", StoreConfig{DefaultTimezone: "Asia/Bangkok"}, "Europe/Berlin", "Europe/Berlin"},
		{"deployment default", StoreConfig{DefaultTimezone: "Asia/Bangkok"}, "  ", "Asia/Bangkok"},
		{"domain fallback", StoreConfig{}, "", store.DefaultTimezone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.store.TimezoneOrDefault(tc.value); got != tc.want {
				t.Fatalf("TimezoneOrDefault(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
