package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tier describes how strictly an environment is validated.
//
// The tier — not the environment name — is what the validation rules are keyed
// on, so a new environment only has to pick a tier instead of duplicating rules.
// The tiers exist to make *one* configuration promotable between environments:
// a key may be relaxed in development, hardened in staging and rejected in
// production.
type Tier string

const (
	// TierRelaxed is development: local defaults must work out of the box.
	TierRelaxed Tier = "relaxed"

	// TierHardened is staging: credentials and public exposure are treated like
	// production (no placeholders, no short secrets, no wildcard CORS) while
	// transport and observability settings may still be loose.
	TierHardened Tier = "hardened"

	// TierStrict is production: every hardening rule applies.
	TierStrict Tier = "strict"
)

// BaseConfigFile is the shared profile every environment inherits from. It holds
// only environment neutral values: no credentials, no environment specific host,
// log format or origin list.
const BaseConfigFile = "config.yaml"

// Environment describes one supported APP_ENV value.
type Environment struct {
	// Name is the value accepted by APP_ENV.
	Name string
	// Tier is the validation tier applied to a resolved configuration.
	Tier Tier
	// ProfileFile is the YAML file that carries the overrides of the profile.
	ProfileFile string
	// Purpose is a one line description used by documentation and diagnostics.
	Purpose string
}

// environments is the single source of truth for the supported profiles. Order
// matters: it is the order used by documentation and by the error message that
// lists the accepted values.
var environments = []Environment{
	{
		Name:        EnvDevelopment,
		Tier:        TierRelaxed,
		ProfileFile: "config.development.yaml",
		Purpose:     "local development on an engineer's machine (.env, console logs, localhost origins)",
	},
	{
		Name:        EnvStaging,
		Tier:        TierHardened,
		ProfileFile: "config.staging.yaml",
		Purpose:     "pre-production acceptance on a staging host (production secrets, staging origin)",
	},
	{
		Name:        EnvProduction,
		Tier:        TierStrict,
		ProfileFile: "config.production.yaml",
		Purpose:     "live single-domain deployment behind nginx (strict validation)",
	},
}

// Environments returns the supported environments in documentation order. The
// slice is a copy so callers cannot mutate the profile table.
func Environments() []Environment {
	out := make([]Environment, len(environments))
	copy(out, environments)
	return out
}

// EnvironmentNames returns the supported APP_ENV values in order.
func EnvironmentNames() []string {
	names := make([]string, 0, len(environments))
	for _, env := range environments {
		names = append(names, env.Name)
	}
	return names
}

// LookupEnvironment resolves an APP_ENV value (case-insensitive) to its profile.
func LookupEnvironment(name string) (Environment, bool) {
	candidate := normalizeEnvName(name)
	for _, env := range environments {
		if env.Name == candidate {
			return env, true
		}
	}
	return Environment{}, false
}

// IsKnownEnvironment reports whether APP_ENV names a supported profile. An
// unknown value is a startup error: a typo such as APP_ENV=prod must never
// silently fall back to the development profile.
func IsKnownEnvironment(name string) bool {
	_, ok := LookupEnvironment(name)
	return ok
}

// TierFor returns the validation tier of an environment. An unknown environment
// is reported as TierRelaxed because Load/Load-File reject it before validation
// ever runs; Validate therefore stays usable on hand built configurations.
func TierFor(name string) Tier {
	if env, ok := LookupEnvironment(name); ok {
		return env.Tier
	}
	return TierRelaxed
}

// ActiveTier returns the tier the loaded configuration is validated with.
func ActiveTier(cfg *Config) Tier {
	if cfg == nil {
		return TierRelaxed
	}
	return TierFor(cfg.App.Environment)
}

// ProfileFileName returns the YAML profile file name of an environment.
func ProfileFileName(name string) string {
	if env, ok := LookupEnvironment(name); ok {
		return env.ProfileFile
	}
	return "config." + normalizeEnvName(name) + ".yaml"
}

// resolveConfigFiles returns the YAML files to merge for env, lowest precedence
// first: the shared base (config.yaml) and then the environment profile
// (config.<env>.yaml). A missing file is simply not merged, so an environment
// variable only deployment (systemd, Kubernetes) needs no YAML at all.
//
// CONFIG_PATH replaces both with one explicit file — and a configured but
// missing file stays a startup error rather than a silent fallback.
func resolveConfigFiles(env string) ([]string, error) {
	if explicit := strings.TrimSpace(os.Getenv("CONFIG_PATH")); explicit != "" {
		if !fileExists(explicit) {
			return nil, fmt.Errorf("config file %q does not exist", explicit)
		}
		return []string{explicit}, nil
	}

	candidates := []string{
		filepath.Join(DefaultConfigDir, BaseConfigFile),
		filepath.Join(DefaultConfigDir, ProfileFileName(env)),
	}

	files := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if fileExists(candidate) {
			files = append(files, candidate)
		}
	}
	return files, nil
}

// normalizeEnvName lowercases and trims an APP_ENV value.
func normalizeEnvName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
