package config

import (
	"fmt"
	"strings"
)

// Environment specific validation rules. The generic rules live in validate.go;
// everything here depends on the tier of the active environment (profiles.go):
//
//   - TierRelaxed (development) adds nothing: local defaults must work.
//   - TierHardened (staging) adds the credential and exposure rules.
//   - TierStrict (production) adds the hardened rules plus the deployment rules.
//
// Keeping them in one file makes the matrix in docs/DEPLOYMENT.md §1 easy to
// audit against the code.

// validateHardened enforces the credential and exposure rules shared by the
// hardened tiers: staging and production never run with a missing, placeholder
// or short secret, and never trust an empty or wildcard origin list.
func validateHardened(cfg *Config, report func(string, ...any)) {
	env := environmentLabel(cfg)

	if err := secretStrengthError("auth.jwt_secret", cfg.Auth.JWTSecret); err != nil {
		report("%s in %s", err, env)
	}
	if err := passwordStrengthError("database.password", cfg.Database.Password); err != nil {
		report("%s in %s", err, env)
	}

	if len(cfg.Auth.PreviousSecrets) > MaxPreviousSecrets {
		report("auth.previous_secrets must hold at most %d rotated values in %s", MaxPreviousSecrets, env)
	}
	for index, previous := range cfg.Auth.PreviousSecrets {
		field := fmt.Sprintf("auth.previous_secrets[%d]", index)
		if err := secretStrengthError(field, previous); err != nil {
			report("%s in %s", err, env)
			continue
		}
		if previous == cfg.Auth.JWTSecret {
			report("%s must differ from auth.jwt_secret in %s", field, env)
		}
	}

	if len(cfg.CORS.AllowedOrigins) == 0 {
		report("cors.allowed_origins must list the platform origin in %s", env)
	}
}

// validateStrict adds the production-only rules: transport to a remote database
// must be verified, logs must be structured for journald/vector, and every
// browser origin must be HTTPS.
func validateStrict(cfg *Config, report func(string, ...any)) {
	env := environmentLabel(cfg)

	sslMode := strings.ToLower(strings.TrimSpace(cfg.Database.SSLMode))
	switch {
	case sslMode == "disable":
		report("database.sslmode must not be 'disable' in %s; use require, verify-ca or verify-full", env)
	case !isLoopbackHost(cfg.Database.Host) && !isVerifiedSSLMode(sslMode):
		report("database.sslmode must be verify-ca or verify-full in %s when database.host %q is not loopback", env, cfg.Database.Host)
	}

	if cfg.Logging.Development {
		report("logging.development must be false in %s", env)
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Logging.Encoding), "json") {
		report("logging.encoding must be json in %s so journald/vector can collect structured logs", env)
	}

	for _, origin := range cfg.CORS.AllowedOrigins {
		if origin == "*" {
			continue // already reported by validateCORS
		}
		if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://localhost") {
			report("cors.allowed_origins entry %q must use https:// (http:// is only allowed for localhost)", origin)
		}
	}
}

// isLoopbackHost reports whether a database host stays on this machine (a unix
// socket directory counts as local as well).
func isLoopbackHost(host string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(host))
	switch trimmed {
	case "", "127.0.0.1", "::1", "[::1]", "localhost":
		return true
	default:
		return strings.HasPrefix(trimmed, "/")
	}
}

// isVerifiedSSLMode reports whether a TLS mode validates the server identity.
func isVerifiedSSLMode(sslMode string) bool {
	switch sslMode {
	case "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// environmentLabel renders the environment for a validation message.
func environmentLabel(cfg *Config) string {
	if name := strings.TrimSpace(cfg.App.Environment); name != "" {
		return name
	}
	return "this environment"
}
