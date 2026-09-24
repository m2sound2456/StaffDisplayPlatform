// Package config loads, merges and validates the Staff Display Platform
// configuration.
//
// Precedence, lowest to highest:
//
//  1. built-in defaults (Default)
//  2. shared YAML base — configs/config.yaml (environment neutral values)
//  3. environment YAML profile — configs/config.development|staging|production.yaml
//  4. environment variables (a local .env file is loaded first when present)
//  5. secret files referenced by *_FILE variables (AUTH_JWT_SECRET_FILE, …)
//
// CONFIG_PATH replaces steps 2 and 3 with one explicit file. Secrets are never
// read from YAML: the signing key and the database password come from the
// environment or from a secret file, and a config file that carries one is
// rejected at startup instead of being used quietly.
//
// The package keeps no global state: callers own the returned *Config.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
	"github.com/m2sound2456/staffdisplay/backend/internal/version"
)

const (
	// EnvDevelopment is the default environment when APP_ENV is unset.
	EnvDevelopment = "development"
	// EnvStaging is the pre-production acceptance environment: production
	// secret rules, a staging host and no tenant data of its own.
	EnvStaging = "staging"
	// EnvProduction enables strict validation and production defaults.
	EnvProduction = "production"

	// DefaultConfigDir holds config.yaml and config.<env>.yaml.
	DefaultConfigDir = "configs"

	// DevJWTSecret is the placeholder signing key used in development only.
	// Hardened tiers (staging, production) reject it.
	DevJWTSecret = "dev-secret-change-me"

	// MinProductionSecretLength is the minimum JWT secret length in a hardened
	// tier (staging, production).
	MinProductionSecretLength = 32
)

// Config is the fully resolved application configuration.
type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	CORS     CORSConfig     `mapstructure:"cors"`
	Store    StoreConfig    `mapstructure:"store"`

	// Files lists the YAML files that were merged, lowest precedence first.
	// It is filled by Load and is never read from a config file.
	Files []string `mapstructure:"-"`
}

// AppConfig describes the service identity and runtime niceties.
type AppConfig struct {
	Name          string        `mapstructure:"name"`
	Environment   string        `mapstructure:"environment"`
	Version       string        `mapstructure:"version"`
	Timezone      string        `mapstructure:"timezone"`
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace"`
}

// ServerConfig describes the HTTP listener.
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
	TLS          TLSConfig     `mapstructure:"tls"`
}

// TLSConfig enables application level TLS. When a reverse proxy terminates TLS
// (the default deployment) this stays disabled.
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

// LoggingConfig configures the zap logger.
type LoggingConfig struct {
	Development bool   `mapstructure:"development"`
	Level       string `mapstructure:"level"`
	Encoding    string `mapstructure:"encoding"`
}

// DatabaseConfig describes the PostgreSQL connection and pool.
type DatabaseConfig struct {
	Host                  string        `mapstructure:"host"`
	Port                  int           `mapstructure:"port"`
	User                  string        `mapstructure:"user"`
	Password              string        `mapstructure:"password"`
	Name                  string        `mapstructure:"name"`
	SSLMode               string        `mapstructure:"sslmode"`
	Timezone              string        `mapstructure:"timezone"`
	MaxOpenConnections    int           `mapstructure:"max_open_connections"`
	MaxIdleConnections    int           `mapstructure:"max_idle_connections"`
	ConnectionMaxLifetime time.Duration `mapstructure:"connection_max_lifetime"`
}

// AuthConfig holds the FG4 authentication settings.
type AuthConfig struct {
	JWTSecret string `mapstructure:"jwt_secret"`
	// PreviousSecrets are signing keys kept during a rotation window: tokens
	// signed with them are still accepted until they expire (FG4 verifies with
	// the current secret first). The list is environment only — see Secrets().
	PreviousSecrets []string      `mapstructure:"previous_secrets"`
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
}

// CORSConfig holds the browser origin allow-list.
type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// StoreConfig holds the deployment policy for new stores. It is the only part
// of the configuration that is overridable per environment and it always stays
// inside the database rules (docs/DATABASE.md §2): the slug pattern, the
// reserved platform paths and the 63 character maximum are fixed by the
// migration set and cannot be relaxed from configuration.
//
// The store service consumes it from FG5 onwards; FG3 ships the overrides, their
// validation and the helpers FG5 will call.
type StoreConfig struct {
	// DefaultTimezone is applied when a store is created without one.
	DefaultTimezone string `mapstructure:"default_timezone"`
	// DefaultStatus is the lifecycle state of a new store.
	DefaultStatus store.Status `mapstructure:"default_status"`
	// Slug carries the slug policy of this deployment.
	Slug SlugPolicyConfig `mapstructure:"slug"`
}

// SlugPolicyConfig is the per-deployment slug policy. Every rule here can only
// make the database rule stricter: a slug has to satisfy store.ValidateSlug
// first and the policy afterwards.
type SlugPolicyConfig struct {
	// MinLength is the shortest slug this deployment accepts (>= 1).
	MinLength int `mapstructure:"min_length"`
	// MaxLength is the longest slug this deployment accepts (<= 63).
	MaxLength int `mapstructure:"max_length"`
	// AutoGenerate derives a slug from the store name when the admin leaves the
	// field empty (FG5 form behaviour).
	AutoGenerate bool `mapstructure:"auto_generate"`
	// ExtraReservedSlugs are deployment specific paths that must not become a
	// store slug (e.g. "shop", "portal"). Platform paths are always reserved.
	ExtraReservedSlugs []string `mapstructure:"extra_reserved_slugs"`
}

// TimezoneOrDefault returns the store timezone or, when empty, the deployment
// default. It is the FG5 fallback for a store created without a timezone.
func (s StoreConfig) TimezoneOrDefault(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	if trimmed = strings.TrimSpace(s.DefaultTimezone); trimmed != "" {
		return trimmed
	}
	return store.DefaultTimezone
}

// IsExtraReservedSlug reports whether the deployment reserved a slug on top of
// the platform paths.
func (s SlugPolicyConfig) IsExtraReservedSlug(slug string) bool {
	candidate := strings.ToLower(strings.TrimSpace(slug))
	for _, reserved := range s.ExtraReservedSlugs {
		if strings.ToLower(strings.TrimSpace(reserved)) == candidate {
			return true
		}
	}
	return false
}

// ValidateSlug applies the database rule first and the deployment policy
// afterwards, so a policy can never accept a slug the database would reject.
func (s SlugPolicyConfig) ValidateSlug(value string) error {
	if err := store.ValidateSlug(value); err != nil {
		return err
	}
	slug := strings.TrimSpace(value)
	if length := len([]rune(slug)); length < s.MinLength {
		return fmt.Errorf("must be at least %d characters", s.MinLength)
	}
	if length := len([]rune(slug)); length > s.MaxLength {
		return fmt.Errorf("must be %d characters or fewer", s.MaxLength)
	}
	if s.IsExtraReservedSlug(slug) {
		return fmt.Errorf("%q is reserved by this deployment", slug)
	}
	return nil
}

// ActiveEnv returns the environment selected by APP_ENV (default development).
func ActiveEnv() string {
	env := normalizeEnvName(os.Getenv("APP_ENV"))
	if env == "" {
		return EnvDevelopment
	}
	return env
}

// IsProduction reports whether cfg targets production (strict tier).
func IsProduction(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.App.Environment), EnvProduction)
}

// IsStaging reports whether cfg targets staging (hardened tier).
func IsStaging(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.App.Environment), EnvStaging)
}

// Default returns the built-in development configuration. Every value here must
// validate in the relaxed tier: the defaults are what a contributor gets without
// any configuration file, and what every YAML/secret overlay builds on.
func Default() Config {
	return Config{
		App: AppConfig{
			Name:          "staffdisplay",
			Environment:   EnvDevelopment,
			Version:       version.Version,
			Timezone:      "Asia/Bangkok",
			ShutdownGrace: 15 * time.Second,
		},
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		Logging: LoggingConfig{
			Development: true,
			Level:       "info",
			Encoding:    "console",
		},
		Database: DatabaseConfig{
			Host:                  "127.0.0.1",
			Port:                  5432,
			User:                  "postgres",
			Name:                  "staffdisplay",
			SSLMode:               "disable",
			Timezone:              "Asia/Bangkok",
			MaxOpenConnections:    25,
			MaxIdleConnections:    5,
			ConnectionMaxLifetime: 30 * time.Minute,
		},
		Auth: AuthConfig{
			JWTSecret:       DevJWTSecret,
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 30 * 24 * time.Hour,
		},
		CORS: CORSConfig{
			AllowedOrigins: []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		},
		Store: StoreConfig{
			DefaultTimezone: store.DefaultTimezone,
			DefaultStatus:   store.StatusActive,
			Slug: SlugPolicyConfig{
				MinLength:    store.MinSlugLength,
				MaxLength:    store.MaxSlugLength,
				AutoGenerate: true,
			},
		},
	}
}

// Load resolves the effective configuration and validates it.
//
// APP_ENV selects the profile, so it is validated before anything is read: an
// unknown value (a typo such as "prod") is an error instead of a silent fallback
// to development defaults.
func Load() (*Config, error) {
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		// A malformed .env is worth surfacing; a missing one is expected in CI,
		// in tests and in production (systemd EnvironmentFile).
		if !strings.Contains(err.Error(), "no such file") {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	env := ActiveEnv()
	if !IsKnownEnvironment(env) {
		return nil, fmt.Errorf("APP_ENV %q is not supported; use one of: %s",
			os.Getenv("APP_ENV"), strings.Join(EnvironmentNames(), ", "))
	}

	cfg := Default()
	cfg.App.Environment = env

	files, err := resolveConfigFiles(env)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if err := mergeYAML(&cfg, file); err != nil {
			return nil, err
		}
	}
	cfg.Files = files

	if err := applyEnv(&cfg); err != nil {
		return nil, err
	}
	if err := applySecretFiles(&cfg); err != nil {
		return nil, err
	}

	// APP_ENV always wins over the YAML profile so one config file can be
	// promoted between environments.
	cfg.App.Environment = env
	if cfg.App.Version == "" {
		cfg.App.Version = version.Version
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func fileExists(path string) bool {
	info, statErr := os.Stat(path)
	return statErr == nil && !info.IsDir()
}

// mergeYAML overlays the YAML file on top of the provided defaults. Only keys
// present in the file are overwritten.
//
// Credentials are rejected here instead of being merged: a secret that reaches
// the repository or a world readable config file is a leak even when the value
// still works, so the operator is told to use AUTH_JWT_SECRET(_FILE) /
// DATABASE_PASSWORD(_FILE).
func mergeYAML(cfg *Config, path string) error {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if err := rejectSecretsInConfig(v, path); err != nil {
		return err
	}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	return nil
}

// secretConfigKeys are the YAML paths that must never carry a value.
var secretConfigKeys = []string{
	"auth.jwt_secret",
	"auth.previous_secrets",
	"database.password",
}

// rejectSecretsInConfig fails when a config file sets a credential.
func rejectSecretsInConfig(v *viper.Viper, path string) error {
	for _, key := range secretConfigKeys {
		if v.IsSet(key) {
			return fmt.Errorf("config %q must not contain %s: secrets come from the environment or from a *_FILE secret file", path, key)
		}
	}
	return nil
}
