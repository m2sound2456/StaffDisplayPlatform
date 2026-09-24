package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// timezonePattern mirrors the store domain rule (store.TimezonePattern) so every
// timezone in the configuration is validated the same way: deterministic, with
// no dependency on the tzdata of the host.
var timezonePattern = regexp.MustCompile(store.TimezonePattern)

// Validate checks a resolved configuration and reports every problem it finds,
// so an operator can fix them in one pass instead of one restart at a time.
//
// Two groups of rules exist:
//
//   - generic rules that always hold (required values, ranges, formats), and
//   - tier rules selected by the environment: relaxed (development), hardened
//     (staging, production credentials) and strict (production deployment).
//
// See profiles.go for the tier table and docs/DEPLOYMENT.md §1 for the matrix.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateApp(cfg, report)
	validateServer(cfg, report)
	validateLogging(cfg, report)
	validateDatabase(cfg, report)
	validateAuth(cfg, report)
	validateCORS(cfg, report)
	validateStore(cfg, report)

	// Environment specific rules: the hardened tier adds the credential and
	// exposure rules, the strict tier adds the deployment rules on top of them.
	switch ActiveTier(cfg) {
	case TierHardened:
		validateHardened(cfg, report)
	case TierStrict:
		validateHardened(cfg, report)
		validateStrict(cfg, report)
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// validateApp checks the service identity and the platform timezone.
func validateApp(cfg *Config, report func(string, ...any)) {
	if strings.TrimSpace(cfg.App.Name) == "" {
		report("app.name is required")
	}
	if !IsKnownEnvironment(cfg.App.Environment) {
		report("app.environment %q must be one of: %s", cfg.App.Environment, strings.Join(EnvironmentNames(), ", "))
	}
	switch {
	case strings.TrimSpace(cfg.App.Timezone) == "":
		report("app.timezone is required")
	case !timezonePattern.MatchString(cfg.App.Timezone):
		report("app.timezone %q must be an IANA timezone name such as Asia/Bangkok", cfg.App.Timezone)
	}
	if cfg.App.ShutdownGrace <= 0 {
		report("app.shutdown_grace must be > 0")
	}
}

// validateServer checks the listener, its timeouts and the optional app TLS.
func validateServer(cfg *Config, report func(string, ...any)) {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		report("server.port must be between 1 and 65535")
	}
	if cfg.Server.ReadTimeout <= 0 {
		report("server.read_timeout must be > 0")
	}
	if cfg.Server.WriteTimeout <= 0 {
		report("server.write_timeout must be > 0")
	}
	if cfg.Server.IdleTimeout <= 0 {
		report("server.idle_timeout must be > 0")
	}
	if cfg.Server.TLS.Enabled {
		if strings.TrimSpace(cfg.Server.TLS.CertFile) == "" {
			report("server.tls.cert_file is required when server.tls.enabled is true")
		}
		if strings.TrimSpace(cfg.Server.TLS.KeyFile) == "" {
			report("server.tls.key_file is required when server.tls.enabled is true")
		}
	}
}

// validateLogging checks the zap logger settings.
func validateLogging(cfg *Config, report func(string, ...any)) {
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case "debug", "info", "warn", "warning", "error", "fatal":
	default:
		report("logging.level must be one of: debug, info, warn, error, fatal")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Encoding)) {
	case "json", "console":
	default:
		report("logging.encoding must be one of: json, console")
	}
}

// validateDatabase checks the PostgreSQL connection and pool.
func validateDatabase(cfg *Config, report func(string, ...any)) {
	if strings.TrimSpace(cfg.Database.Host) == "" {
		report("database.host is required")
	}
	if cfg.Database.Port <= 0 || cfg.Database.Port > 65535 {
		report("database.port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Database.User) == "" {
		report("database.user is required")
	}
	if strings.TrimSpace(cfg.Database.Name) == "" {
		report("database.name is required")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Database.SSLMode)) {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		report("database.sslmode must be one of: disable, allow, prefer, require, verify-ca, verify-full")
	}
	switch {
	case strings.TrimSpace(cfg.Database.Timezone) == "":
		report("database.timezone is required")
	case !timezonePattern.MatchString(cfg.Database.Timezone):
		report("database.timezone %q must be an IANA timezone name such as Asia/Bangkok", cfg.Database.Timezone)
	}
	if cfg.Database.MaxOpenConnections <= 0 {
		report("database.max_open_connections must be > 0")
	}
	if cfg.Database.MaxIdleConnections < 0 {
		report("database.max_idle_connections must be >= 0")
	}
	if cfg.Database.MaxIdleConnections > cfg.Database.MaxOpenConnections {
		report("database.max_idle_connections must not exceed database.max_open_connections")
	}
	if cfg.Database.ConnectionMaxLifetime < 0 {
		report("database.connection_max_lifetime must be >= 0")
	}
}

// validateAuth checks the FG4 token settings.
func validateAuth(cfg *Config, report func(string, ...any)) {
	if strings.TrimSpace(cfg.Auth.JWTSecret) == "" {
		report("auth.jwt_secret is required")
	}
	if cfg.Auth.AccessTokenTTL <= 0 {
		report("auth.access_token_ttl must be > 0")
	}
	if cfg.Auth.RefreshTokenTTL <= 0 {
		report("auth.refresh_token_ttl must be > 0")
	}
	if cfg.Auth.AccessTokenTTL > 0 && cfg.Auth.RefreshTokenTTL > 0 && cfg.Auth.AccessTokenTTL >= cfg.Auth.RefreshTokenTTL {
		report("auth.access_token_ttl must be shorter than auth.refresh_token_ttl")
	}
}

// validateCORS checks that every allowed origin is a bare absolute origin: the
// browser sends scheme://host[:port] in the Origin header and nothing else, and
// a wildcard would break the tenant boundary of the single domain.
func validateCORS(cfg *Config, report func(string, ...any)) {
	for _, origin := range cfg.CORS.AllowedOrigins {
		if origin == "*" {
			report("cors.allowed_origins must not contain the wildcard '*': every client is served from the platform origin (single domain + path)")
			continue
		}
		if !isAbsoluteOrigin(origin) {
			report("cors.allowed_origins entry %q must be an absolute origin such as https://display.example.com (no path, query or fragment)", origin)
		}
	}
}

// isAbsoluteOrigin reports whether value is scheme://host[:port] without a path,
// query, fragment or user info.
func isAbsoluteOrigin(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return false
	}
	return parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

// validateStore checks the store default overrides. They may only tighten the
// database rules (docs/DATABASE.md §2) and can never relax them.
func validateStore(cfg *Config, report func(string, ...any)) {
	switch {
	case strings.TrimSpace(cfg.Store.DefaultTimezone) == "":
		report("store.default_timezone is required")
	case !timezonePattern.MatchString(cfg.Store.DefaultTimezone):
		report("store.default_timezone %q must be an IANA timezone name such as Asia/Bangkok", cfg.Store.DefaultTimezone)
	}
	if !cfg.Store.DefaultStatus.Valid() {
		report("store.default_status must be one of %s", statusList())
	}

	policy := cfg.Store.Slug
	if policy.MinLength < store.MinSlugLength {
		report("store.slug.min_length must be at least %d", store.MinSlugLength)
	}
	if policy.MaxLength > store.MaxSlugLength {
		report("store.slug.max_length must be %d or fewer; the database CHECK constraint cannot be relaxed", store.MaxSlugLength)
	}
	if policy.MinLength > policy.MaxLength {
		report("store.slug.min_length must not exceed store.slug.max_length")
	}
	for _, reserved := range policy.ExtraReservedSlugs {
		if err := store.ValidateSlug(reserved); err != nil {
			report("store.slug.extra_reserved_slugs entry %q is not usable: %s", reserved, err)
		}
	}
}

// statusList renders the store statuses for a validation message.
func statusList() string {
	statuses := store.AllStatuses()
	values := make([]string, 0, len(statuses))
	for _, status := range statuses {
		values = append(values, string(status))
	}
	return strings.Join(values, ", ")
}
