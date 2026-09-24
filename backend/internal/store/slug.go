// Package store owns the store domain model, its validation rules and the
// tenant-scoped repository used by every feature group that touches store data
// (BLUEPRINT §3, §4, §12, docs/DATABASE.md §2).
//
// Architecture invariants this package protects:
//
//   - A store is a plain database record addressed publicly as /s/{slug} on the
//     single platform domain — no per-store folder, host, service or database.
//   - Every query is scoped by tenant_id (and, for child resources, store_id);
//     the scope always comes from the authenticated identity, never from a
//     client supplied id alone. Cross-tenant access reports ErrNotFound so
//     existence never leaks.
//   - Device identity is *not* modelled here. A device has its own id, hashed
//     token and per-device display configuration (FG16–FG21); it never derives
//     its identity from the store URL.
package store

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// SlugPattern is the canonical store slug rule (BLUEPRINT §4). Three places
	// must stay in sync and are guarded by schema_parity_test.go:
	//   1. this constant,
	//   2. the CHECK constraints in migrations/0002 and 0003,
	//   3. frontend/src/lib/storeSlug.ts.
	SlugPattern = "^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$"

	// TimezonePattern accepts IANA zone names (Asia/Bangkok, UTC, Etc/GMT+7).
	TimezonePattern = "^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$"

	// MinSlugLength and MaxSlugLength bound the public display key.
	MinSlugLength = 1
	MaxSlugLength = 63

	// MaxNameLength bounds store (and tenant) names, mirrored by the DB CHECK.
	MaxNameLength = 120

	// MaxLogoURLLength bounds the optional logo reference.
	MaxLogoURLLength = 512

	// MinTimezoneLength and MaxTimezoneLength bound the IANA zone name.
	MinTimezoneLength = 2
	MaxTimezoneLength = 64

	// DefaultTimezone is used when a store does not configure one.
	DefaultTimezone = "UTC"

	// DefaultOpeningHours is the empty opening-hours document.
	DefaultOpeningHours = "{}"
)

var (
	slugRegexp     = regexp.MustCompile(SlugPattern)
	timezoneRegexp = regexp.MustCompile(TimezonePattern)
)

// ReservedSlugs are the platform paths a store may never occupy: they would
// collide with the single-domain routing table (BLUEPRINT §4). The list is
// mirrored by the admin UI and by the DB CHECK constraints.
var ReservedSlugs = []string{
	"app",
	"setup",
	"api",
	"ws",
	"healthz",
	"readyz",
	"assets",
	"icons",
	"static",
}

// IsReservedSlug reports whether a slug collides with a reserved platform path.
func IsReservedSlug(slug string) bool {
	candidate := strings.ToLower(strings.TrimSpace(slug))
	for _, reserved := range ReservedSlugs {
		if candidate == reserved {
			return true
		}
	}
	return false
}

// NormalizeSlug turns user input into the canonical stored form: lowercase,
// separators collapsed to single hyphens, no leading/trailing hyphen. It mirrors
// frontend/src/lib/storeSlug.ts#normalizeStoreSlug.
func NormalizeSlug(value string) string {
	lowered := strings.ToLower(strings.TrimSpace(value))

	var builder strings.Builder
	builder.Grow(len(lowered))
	for _, r := range lowered {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}

	normalized := builder.String()
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	return strings.Trim(normalized, "-")
}

// ValidateSlug reports whether a slug is a valid, non-reserved store slug.
// The returned message is user facing and identical to the frontend wording.
func ValidateSlug(value string) error { return slugError(value) }

// slugError implements the slug rule as a single user facing message.
func slugError(value string) error {
	slug := strings.TrimSpace(value)

	switch {
	case slug == "":
		return fmt.Errorf("is required")
	case len([]rune(slug)) > MaxSlugLength:
		return fmt.Errorf("must be %d characters or fewer", MaxSlugLength)
	case slug != strings.ToLower(slug):
		return fmt.Errorf("must be lowercase")
	case !slugRegexp.MatchString(slug):
		return fmt.Errorf("may only contain lowercase letters, digits and interior hyphens")
	case IsReservedSlug(slug):
		return fmt.Errorf("%q is reserved by the platform", slug)
	default:
		return nil
	}
}

// timezoneError validates an IANA zone name without depending on the local
// tzdata (deterministic on Windows CI as well). The value must already be
// normalised (Store.Normalize trims surrounding whitespace).
func timezoneError(value string) error {
	switch {
	case value == "":
		return fmt.Errorf("is required")
	case len([]rune(value)) < MinTimezoneLength:
		return fmt.Errorf("must be at least %d characters", MinTimezoneLength)
	case len([]rune(value)) > MaxTimezoneLength:
		return fmt.Errorf("must be %d characters or fewer", MaxTimezoneLength)
	case !timezoneRegexp.MatchString(value):
		return fmt.Errorf("must be an IANA timezone name such as Asia/Bangkok")
	default:
		return nil
	}
}
