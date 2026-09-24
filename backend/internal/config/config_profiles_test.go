package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkdirConfigs creates the configs/ directory in the current (temporary) working
// directory, exactly like the repository layout Load expects.
func mkdirConfigs(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(DefaultConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", DefaultConfigDir, err)
	}
}

// writeProfile writes a YAML file into configs/.
func writeProfile(t *testing.T, name, content string) {
	t.Helper()
	mkdirConfigs(t)
	if err := os.WriteFile(filepath.Join(DefaultConfigDir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// strongSecret returns a signing key that satisfies the hardened tier rule.
func strongSecret() string {
	return strings.Repeat("k", MinProductionSecretLength)
}

// hardenedCredentials sets the credentials and origin a hardened tier requires.
func hardenedCredentials(t *testing.T, origin string) {
	t.Helper()
	t.Setenv("AUTH_JWT_SECRET", strongSecret())
	t.Setenv("DATABASE_PASSWORD", strings.Repeat("p", MinProductionPasswordLength))
	t.Setenv("CORS_ALLOWED_ORIGINS", origin)
}

func TestEnvironmentProfilesAreWellFormed(t *testing.T) {
	envs := Environments()
	want := []string{EnvDevelopment, EnvStaging, EnvProduction}

	if len(envs) != len(want) {
		t.Fatalf("Environments() returned %d profiles, want %d", len(envs), len(want))
	}
	for index, env := range envs {
		if env.Name != want[index] {
			t.Errorf("Environments()[%d].Name = %q, want %q", index, env.Name, want[index])
		}
		if env.Tier == "" {
			t.Errorf("%s has no validation tier", env.Name)
		}
		if env.ProfileFile != "config."+env.Name+".yaml" {
			t.Errorf("%s profile file = %q, want config.%s.yaml", env.Name, env.ProfileFile, env.Name)
		}
		if strings.TrimSpace(env.Purpose) == "" {
			t.Errorf("%s has no purpose", env.Name)
		}
	}

	if got := EnvironmentNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("EnvironmentNames() = %v, want %v", got, want)
	}
	// The profile table is a copy: a caller must not be able to edit it.
	envs[0].Name = "mutated"
	if Environments()[0].Name != EnvDevelopment {
		t.Error("Environments() returned the internal slice instead of a copy")
	}
}

func TestTierForEnvironment(t *testing.T) {
	cases := map[string]Tier{
		EnvDevelopment: TierRelaxed,
		EnvStaging:     TierHardened,
		EnvProduction:  TierStrict,
		"PRODUCTION":   TierStrict,
		" staging ":    TierHardened,
		"nonsense":     TierRelaxed,
	}
	for name, want := range cases {
		if got := TierFor(name); got != want {
			t.Errorf("TierFor(%q) = %q, want %q", name, got, want)
		}
	}

	if got := ActiveTier(validProduction()); got != TierStrict {
		t.Errorf("ActiveTier(production) = %q, want %q", got, TierStrict)
	}
	if got := ActiveTier(validStaging()); got != TierHardened {
		t.Errorf("ActiveTier(staging) = %q, want %q", got, TierHardened)
	}
	if got := ActiveTier(nil); got != TierRelaxed {
		t.Errorf("ActiveTier(nil) = %q, want %q", got, TierRelaxed)
	}
}

func TestIsKnownEnvironmentRejectsTypos(t *testing.T) {
	for _, name := range []string{EnvDevelopment, EnvStaging, EnvProduction, "Staging", " staging "} {
		if !IsKnownEnvironment(name) {
			t.Errorf("IsKnownEnvironment(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "dev", "prod", "stagingx", "production2"} {
		if IsKnownEnvironment(name) {
			t.Errorf("IsKnownEnvironment(%q) = true, want false", name)
		}
	}
	if !IsStaging(validStaging()) || IsStaging(validProduction()) {
		t.Error("IsStaging() must be true for staging and false for production")
	}
	if IsStaging(nil) {
		t.Error("IsStaging(nil) = true, want false")
	}
}

// TestLoadRejectsUnknownAPPENV pins the guard against a silent fallback: a typo
// such as APP_ENV=prod must never run with development defaults.
func TestLoadRejectsUnknownAPPENV(t *testing.T) {
	isolateEnvironment(t)
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "prod")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for an unsupported APP_ENV")
	}
	for _, want := range []string{"prod", EnvDevelopment, EnvStaging, EnvProduction} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestLoadMergesBaseThenEnvironmentProfile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvStaging)
	t.Chdir(t.TempDir())
	hardenedCredentials(t, "https://staging.example.com")

	writeProfile(t, BaseConfigFile, "server:\n  port: 9191\n  read_timeout: 25s\nstore:\n  slug:\n    min_length: 5\n")
	writeProfile(t, ProfileFileName(EnvStaging), "server:\n  port: 9555\nstore:\n  slug:\n    min_length: 7\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 9555 {
		t.Errorf("server.port = %d, want 9555 (the profile wins over the base)", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 25*time.Second {
		t.Errorf("server.read_timeout = %s, want the base value 25s to survive", cfg.Server.ReadTimeout)
	}
	if cfg.Store.Slug.MinLength != 7 {
		t.Errorf("store.slug.min_length = %d, want 7", cfg.Store.Slug.MinLength)
	}
	if cfg.Store.Slug.MaxLength != Default().Store.Slug.MaxLength {
		t.Errorf("store.slug.max_length = %d, want the built-in default", cfg.Store.Slug.MaxLength)
	}
	if cfg.App.Environment != EnvStaging || !IsStaging(cfg) {
		t.Errorf("app.environment = %q, want %q", cfg.App.Environment, EnvStaging)
	}
	if len(cfg.Files) != 2 {
		t.Fatalf("Files = %v, want the shared base and the staging profile", cfg.Files)
	}
	if cfg.Files[0] != filepath.Join(DefaultConfigDir, BaseConfigFile) {
		t.Errorf("Files[0] = %q, want the shared base first", cfg.Files[0])
	}
	if cfg.Files[1] != filepath.Join(DefaultConfigDir, ProfileFileName(EnvStaging)) {
		t.Errorf("Files[1] = %q, want the staging profile second", cfg.Files[1])
	}
}

func TestLoadUsesBaseWhenTheProfileIsMissing(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvDevelopment)
	t.Chdir(t.TempDir())
	writeProfile(t, BaseConfigFile, "server:\n  port: 9191\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 9191 {
		t.Errorf("server.port = %d, want 9191", cfg.Server.Port)
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != filepath.Join(DefaultConfigDir, BaseConfigFile) {
		t.Errorf("Files = %v, want only the shared base", cfg.Files)
	}
}

func TestConfigPathReplacesBaseAndProfile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvStaging)
	t.Chdir(t.TempDir())
	hardenedCredentials(t, "https://staging.example.com")

	writeProfile(t, BaseConfigFile, "server:\n  port: 1111\n")
	writeProfile(t, ProfileFileName(EnvStaging), "server:\n  port: 2222\n")

	explicit := writeConfigFile(t, "promoted.yaml", "server:\n  port: 3333\n")
	t.Setenv("CONFIG_PATH", explicit)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 3333 {
		t.Errorf("server.port = %d, want the CONFIG_PATH value 3333", cfg.Server.Port)
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != explicit {
		t.Errorf("Files = %v, want only the explicit CONFIG_PATH file", cfg.Files)
	}
}

// TestSecretsInConfigFilesAreRejected pins the secret handling rule: a value in
// YAML is refused even though it would work, because config files are committed
// and read by more processes than a secret file is.
func TestSecretsInConfigFilesAreRejected(t *testing.T) {
	cases := map[string]string{
		"auth.jwt_secret":       "auth:\n  jwt_secret: leaked-secret-value\n",
		"database.password":     "database:\n  password: leaked-secret-value\n",
		"auth.previous_secrets": "auth:\n  previous_secrets:\n    - leaked-secret-value\n",
	}
	for want, yaml := range cases {
		t.Run(want, func(t *testing.T) {
			isolateEnvironment(t)
			t.Setenv("CONFIG_PATH", writeConfigFile(t, "config.yaml", yaml))

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to reject %s in a YAML file", want)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not mention %q", err.Error(), want)
			}
		})
	}
}

// TestShippedProfilesValidateForEveryEnvironment is the regression guard for the
// files in backend/configs/: every profile must load and pass its tier, so a
// profile cannot be shipped that the binary would refuse at startup.
func TestShippedProfilesValidateForEveryEnvironment(t *testing.T) {
	for _, env := range EnvironmentNames() {
		t.Run(env, func(t *testing.T) {
			isolateEnvironment(t)
			t.Chdir(filepath.Join("..", ".."))

			if !fileExists(filepath.Join(DefaultConfigDir, BaseConfigFile)) {
				t.Fatalf("%s is missing", filepath.Join(DefaultConfigDir, BaseConfigFile))
			}
			if !fileExists(filepath.Join(DefaultConfigDir, ProfileFileName(env))) {
				t.Fatalf("%s is missing", filepath.Join(DefaultConfigDir, ProfileFileName(env)))
			}

			t.Setenv("APP_ENV", env)
			hardenedCredentials(t, "https://staging.display.example.com")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("shipped %s profile must validate: %v", env, err)
			}
			if cfg.App.Environment != env {
				t.Fatalf("app.environment = %q, want %q", cfg.App.Environment, env)
			}
			if len(cfg.Files) != 2 {
				t.Fatalf("Files = %v, want the shared base plus the %s profile", cfg.Files, env)
			}
			if err := Validate(cfg); err != nil {
				t.Fatalf("shipped %s profile must pass Validate(): %v", env, err)
			}
		})
	}
}
