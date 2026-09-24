package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfigFile creates a temporary YAML config file and returns its path.
func writeConfigFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// isolateEnvironment clears the variables Load consults so every test states its
// own preconditions. t.Setenv restores previous values automatically.
func isolateEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV", "CONFIG_PATH", "APP_NAME", "APP_TIMEZONE", "APP_SHUTDOWN_TIMEOUT",
		"SERVER_HOST", "SERVER_PORT", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT",
		"SERVER_IDLE_TIMEOUT", "SERVER_TLS_ENABLED", "SERVER_TLS_CERT_FILE", "SERVER_TLS_KEY_FILE",
		"LOGGING_DEVELOPMENT", "LOGGING_LEVEL", "LOGGING_ENCODING",
		"DATABASE_HOST", "DATABASE_PORT", "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_NAME",
		"DATABASE_SSLMODE", "DATABASE_TIMEZONE", "DATABASE_MAX_OPEN_CONNECTIONS",
		"DATABASE_MAX_IDLE_CONNECTIONS", "DATABASE_CONNECTION_MAX_LIFETIME",
		"AUTH_JWT_SECRET", "AUTH_JWT_SECRET_FILE", "AUTH_ACCESS_TOKEN_TTL", "AUTH_REFRESH_TOKEN_TTL",
		"AUTH_JWT_PREVIOUS_SECRETS", "AUTH_JWT_PREVIOUS_SECRETS_FILE",
		"DATABASE_PASSWORD_FILE",
		"CORS_ALLOWED_ORIGINS",
		"STORE_DEFAULT_TIMEZONE", "STORE_DEFAULT_STATUS",
		"STORE_SLUG_MIN_LENGTH", "STORE_SLUG_MAX_LENGTH", "STORE_SLUG_AUTO_GENERATE",
		"STORE_SLUG_EXTRA_RESERVED_SLUGS",
	} {
		t.Setenv(key, "")
	}
}

func TestDefaultConfigurationIsValid(t *testing.T) {
	cfg := Default()

	if err := Validate(&cfg); err != nil {
		t.Fatalf("built-in defaults must validate, got: %v", err)
	}
	if cfg.App.Name != "staffdisplay" {
		t.Errorf("app.name = %q, want staffdisplay", cfg.App.Name)
	}
	if cfg.Database.Name != "staffdisplay" {
		t.Errorf("database.name = %q, want staffdisplay", cfg.Database.Name)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.App.Environment != EnvDevelopment {
		t.Errorf("app.environment = %q, want %q", cfg.App.Environment, EnvDevelopment)
	}
}

func TestLoadUsesDefaultsWithoutConfigFile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)
	t.Chdir(t.TempDir()) // no configs/ directory here

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 8080 || cfg.Database.Name != "staffdisplay" {
		t.Fatalf("expected defaults, got port=%d database=%s", cfg.Server.Port, cfg.Database.Name)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)
	t.Setenv("CONFIG_PATH", writeConfigFile(t, "config.yaml", "server:\n  port: 9191\n  host: 10.0.0.1\n"))
	t.Setenv("SERVER_PORT", "7000")
	t.Setenv("DATABASE_NAME", "staffdisplay_env")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://one.example.com, https://two.example.com")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "20s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 7000 {
		t.Errorf("server.port = %d, want 7000 (environment wins)", cfg.Server.Port)
	}
	if cfg.Server.Host != "10.0.0.1" {
		t.Errorf("server.host = %q, want the file value 10.0.0.1", cfg.Server.Host)
	}
	if cfg.Database.Name != "staffdisplay_env" {
		t.Errorf("database.name = %q, want staffdisplay_env", cfg.Database.Name)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 || cfg.CORS.AllowedOrigins[1] != "https://two.example.com" {
		t.Errorf("cors.allowed_origins = %v, want two trimmed entries", cfg.CORS.AllowedOrigins)
	}
	if cfg.App.ShutdownGrace != 20*time.Second {
		t.Errorf("app.shutdown_grace = %s, want 20s", cfg.App.ShutdownGrace)
	}
}
