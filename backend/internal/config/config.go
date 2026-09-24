// Package config loads, merges and validates the Staff Display Platform
// configuration.
//
// Precedence, lowest to highest:
//
//  1. built-in defaults (Default)
//  2. YAML file — CONFIG_PATH, else configs/config.<APP_ENV>.yaml, else configs/config.yaml
//  3. environment variables (a local .env file is loaded first when present)
//
// The package keeps no global state: callers own the returned *Config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/m2sound2456/staffdisplay/backend/internal/version"
)

const (
	// EnvDevelopment is the default environment when APP_ENV is unset.
	EnvDevelopment = "development"
	// EnvProduction enables strict validation and production defaults.
	EnvProduction = "production"

	// DefaultConfigDir holds config.<env>.yaml / config.yaml.
	DefaultConfigDir = "configs"

	// DevJWTSecret is the placeholder signing key used in development only.
	// Production validation rejects it.
	DevJWTSecret = "dev-secret-change-me"

	// MinProductionSecretLength is the minimum JWT secret length in production.
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
	JWTSecret       string        `mapstructure:"jwt_secret"`
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
}

// CORSConfig holds the browser origin allow-list.
type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// ActiveEnv returns the environment selected by APP_ENV (default development).
func ActiveEnv() string {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if env == "" {
		return EnvDevelopment
	}
	return env
}

// IsProduction reports whether cfg targets production.
func IsProduction(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.App.Environment), EnvProduction)
}

// Default returns the built-in development configuration.
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
	}
}

// Load resolves the effective configuration and validates it.
func Load() (*Config, error) {
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		// A malformed .env is worth surfacing; a missing one is expected in CI,
		// in tests and in production (systemd EnvironmentFile).
		if !strings.Contains(err.Error(), "no such file") {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	cfg := Default()
	cfg.App.Environment = ActiveEnv()

	path, explicit, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	switch {
	case path != "":
		if err := mergeYAML(&cfg, path); err != nil {
			return nil, err
		}
	case explicit:
		return nil, fmt.Errorf("config file %q does not exist", os.Getenv("CONFIG_PATH"))
	}

	if err := applyEnv(&cfg); err != nil {
		return nil, err
	}
	// APP_ENV always wins over the YAML file so one config file can be promoted
	// between environments.
	cfg.App.Environment = ActiveEnv()

	if cfg.App.Version == "" {
		cfg.App.Version = version.Version
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// resolveConfigPath returns the YAML path to read and whether it was requested
// explicitly through CONFIG_PATH.
func resolveConfigPath() (path string, explicit bool, err error) {
	if fromEnv := strings.TrimSpace(os.Getenv("CONFIG_PATH")); fromEnv != "" {
		if !fileExists(fromEnv) {
			return "", true, nil
		}
		return fromEnv, true, nil
	}

	env := ActiveEnv()
	candidates := []string{
		filepath.Join(DefaultConfigDir, "config."+env+".yaml"),
		filepath.Join(DefaultConfigDir, "config.yaml"),
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, false, nil
		}
	}
	return "", false, nil
}

func fileExists(path string) bool {
	info, statErr := os.Stat(path)
	return statErr == nil && !info.IsDir()
}

// mergeYAML overlays the YAML file on top of the provided defaults. Only keys
// present in the file are overwritten.
func mergeYAML(cfg *Config, path string) error {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	return nil
}
