package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadReadsSelectedConfigFile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)

	t.Chdir(t.TempDir())
	if err := os.MkdirAll("configs", 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	devYAML := "server:\n  port: 9191\ndatabase:\n  name: staffdisplay_dev\n  max_open_connections: 7\nlogging:\n  level: debug\n"
	if err := os.WriteFile(filepath.Join("configs", "config.yaml"), []byte(devYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join("configs", "config.production.yaml"), []byte("server:\n  port: 9999\n"), 0o600); err != nil {
		t.Fatalf("write config.production.yaml: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 9191 {
		t.Errorf("server.port = %d, want 9191 (from config.yaml)", cfg.Server.Port)
	}
	if cfg.Database.Name != "staffdisplay_dev" {
		t.Errorf("database.name = %q, want staffdisplay_dev", cfg.Database.Name)
	}
	if cfg.Database.MaxOpenConnections != 7 {
		t.Errorf("database.max_open_connections = %d, want 7", cfg.Database.MaxOpenConnections)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level = %q, want debug", cfg.Logging.Level)
	}
	// Keys absent from the file keep their defaults.
	if cfg.Server.ReadTimeout != 15*time.Second {
		t.Errorf("server.read_timeout = %s, want the 15s default", cfg.Server.ReadTimeout)
	}

	// APP_ENV=production switches to config.production.yaml.
	t.Setenv("APP_ENV", EnvProduction)
	t.Setenv("DATABASE_PASSWORD", "production-secret")
	t.Setenv("AUTH_JWT_SECRET", strings.Repeat("x", MinProductionSecretLength))
	t.Setenv("DATABASE_SSLMODE", "require")
	t.Setenv("LOGGING_DEVELOPMENT", "false")
	t.Setenv("LOGGING_ENCODING", "json")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://display.example.com")

	prodCfg, err := Load()
	if err != nil {
		t.Fatalf("production Load() error = %v", err)
	}
	if prodCfg.Server.Port != 9999 {
		t.Errorf("production server.port = %d, want 9999", prodCfg.Server.Port)
	}
	if !IsProduction(prodCfg) {
		t.Error("IsProduction() = false for APP_ENV=production")
	}
}

func TestLoadRejectsMissingExplicitConfigFile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an explicitly configured but missing config file")
	}
}

func TestLoadRejectsInvalidEnvironmentValues(t *testing.T) {
	cases := map[string]string{
		"SERVER_PORT":                      "not-a-number",
		"DATABASE_PORT":                    "70000",
		"APP_SHUTDOWN_TIMEOUT":             "soon",
		"SERVER_TLS_ENABLED":               "yes-please",
		"DATABASE_CONNECTION_MAX_LIFETIME": "10 light years",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			isolateEnvironment(t)
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatalf("expected an error for %s=%q", key, value)
			}
		})
	}
}

func TestMergeYAMLRejectsMalformedFile(t *testing.T) {
	path := writeConfigFile(t, "broken.yaml", "server:\n  port: [not, a, number\n")
	cfg := Default()
	if err := mergeYAML(&cfg, path); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestSplitListTrimsAndDropsEmptyEntries(t *testing.T) {
	got := splitList(" https://a.example.com ,, https://b.example.com , ")
	want := []string{"https://a.example.com", "https://b.example.com"}
	if len(got) != len(want) {
		t.Fatalf("splitList() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitList()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestActiveEnvNormalizesAndDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	if got := ActiveEnv(); got != EnvDevelopment {
		t.Errorf("ActiveEnv() = %q, want %q", got, EnvDevelopment)
	}
	t.Setenv("APP_ENV", "PRODUCTION")
	if got := ActiveEnv(); got != EnvProduction {
		t.Errorf("ActiveEnv() = %q, want %q after normalization", got, EnvProduction)
	}
	if !IsProduction(&Config{App: AppConfig{Environment: EnvProduction}}) {
		t.Error("IsProduction() = false for a production config")
	}
	if IsProduction(nil) {
		t.Error("IsProduction(nil) = true, want false")
	}
}
