package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSecret writes a credential file with restrictive permissions and returns
// its path.
func writeSecret(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// productionEnvironment sets every non-secret variable a strict tier needs.
func productionEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_SSLMODE", "require")
	t.Setenv("LOGGING_DEVELOPMENT", "false")
	t.Setenv("LOGGING_ENCODING", "json")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://display.example.com")
}

func TestSecretStrengthRules(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"empty", "", "is required"},
		{"whitespace only", "   ", "is required"},
		{"too short", "short-secret", "at least 32"},
		{"trailing newline", strings.Repeat("k", 32) + "\n", "whitespace"},
		{"development placeholder", DevJWTSecret, "development placeholder"},
		{"placeholder marker", "please-change-this-secret-value-for-me", "placeholder"},
		{"generated value", strings.Repeat("k", 32), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := secretStrengthError("auth.jwt_secret", tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
			if !strings.Contains(err.Error(), "auth.jwt_secret") {
				t.Fatalf("error %q must name the field", err.Error())
			}
			if value := strings.TrimSpace(tc.value); value != "" && strings.Contains(err.Error(), value) {
				t.Fatalf("error %q must not echo the secret value", err.Error())
			}
		})
	}
}

func TestPasswordStrengthRules(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"empty", "", "is required"},
		{"too short", "secret12", "at least 12"},
		{"well known weak value", "staffdisplay", "weak"},
		{"padded", " super-secret ", "whitespace"},
		{"acceptable", "super-secret", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := passwordStrengthError("database.password", tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
			if !strings.Contains(err.Error(), "database.password") {
				t.Fatalf("error %q must name the field", err.Error())
			}
		})
	}
}

// TestLoadReadsSecretsFromFiles pins the preferred production secret source: the
// value never appears in an environment variable visible to `systemctl show` or
// `ps e`.
func TestLoadReadsSecretsFromFiles(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvProduction)
	t.Chdir(t.TempDir())
	productionEnvironment(t)

	jwt := strings.Repeat("j", 40)
	password := "database-password-1"
	t.Setenv("AUTH_JWT_SECRET_FILE", writeSecret(t, "jwt.secret", jwt+"\n"))
	t.Setenv("DATABASE_PASSWORD_FILE", writeSecret(t, "database.secret", password))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.JWTSecret != jwt {
		t.Errorf("auth.jwt_secret = %q, want the trimmed file content", cfg.Auth.JWTSecret)
	}
	if cfg.Database.Password != password {
		t.Errorf("database.password = %q, want the file content", cfg.Database.Password)
	}

	rendered := strings.Join(cfg.Summary(), "\n")
	for _, secret := range []string{jwt, password} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("the configuration summary leaked a secret:\n%s", rendered)
		}
	}
	if !strings.Contains(rendered, SecretMask) {
		t.Fatalf("the configuration summary must mask secrets:\n%s", rendered)
	}
}

func TestLoadRejectsAmbiguousSecretSources(t *testing.T) {
	cases := []struct{ name, direct, file string }{
		{"jwt secret", "AUTH_JWT_SECRET", "AUTH_JWT_SECRET_FILE"},
		{"database password", "DATABASE_PASSWORD", "DATABASE_PASSWORD_FILE"},
		{"previous secrets", "AUTH_JWT_PREVIOUS_SECRETS", "AUTH_JWT_PREVIOUS_SECRETS_FILE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnvironment(t)
			t.Setenv("APP_ENV", EnvDevelopment)
			t.Chdir(t.TempDir())
			t.Setenv(tc.direct, "value-from-the-environment")
			t.Setenv(tc.file, writeSecret(t, "secret", "value-from-a-file"))

			_, err := Load()
			if err == nil {
				t.Fatal("expected an error when both sources are set")
			}
			if !strings.Contains(err.Error(), "not both") {
				t.Fatalf("error %q must explain that the two sources are exclusive", err.Error())
			}
		})
	}
}

func TestLoadRejectsUnusableSecretFiles(t *testing.T) {
	cases := []struct {
		name    string
		path    func(*testing.T) string
		wantErr string
	}{
		{"missing file", func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "absent.secret")
		}, "AUTH_JWT_SECRET_FILE"},
		{"empty file", func(t *testing.T) string {
			return writeSecret(t, "empty.secret", "  \n")
		}, "is empty"},
		{"directory", func(t *testing.T) string {
			return t.TempDir()
		}, "is a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnvironment(t)
			t.Setenv("APP_ENV", EnvDevelopment)
			t.Chdir(t.TempDir())
			t.Setenv("AUTH_JWT_SECRET_FILE", tc.path(t))

			_, err := Load()
			if err == nil {
				t.Fatal("expected an error for an unusable secret file")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadReadsPreviousSecretsFromAFile(t *testing.T) {
	isolateEnvironment(t)
	t.Setenv("APP_ENV", EnvProduction)
	t.Chdir(t.TempDir())
	productionEnvironment(t)

	older := strings.Repeat("o", 40)
	oldest := strings.Repeat("t", 40)
	t.Setenv("AUTH_JWT_SECRET", strings.Repeat("c", 40))
	t.Setenv("DATABASE_PASSWORD", "database-password-1")
	t.Setenv("AUTH_JWT_PREVIOUS_SECRETS_FILE", writeSecret(t, "rotated.secrets", older+"\n,\n"+oldest+"\n"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Auth.PreviousSecrets) != 2 {
		t.Fatalf("previous secrets = %v, want the two rotated values", cfg.Auth.PreviousSecrets)
	}
	if cfg.Auth.PreviousSecrets[0] != older || cfg.Auth.PreviousSecrets[1] != oldest {
		t.Fatalf("previous secrets = %v, want one value per line", cfg.Auth.PreviousSecrets)
	}
}

func TestSplitSecretListTrimsAndDropsEmptyItems(t *testing.T) {
	got := splitSecretList(" aaa , , bbb ;\nccc\r\n ")
	want := []string{"aaa", "bbb", "ccc"}
	if len(got) != len(want) {
		t.Fatalf("splitSecretList() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("splitSecretList()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestSecretsListsEveryCredential(t *testing.T) {
	cfg := validProduction()
	cfg.Auth.PreviousSecrets = []string{strings.Repeat("p", 40)}

	secrets := cfg.Secrets()
	if len(secrets) != 3 {
		t.Fatalf("Secrets() returned %d fields, want 3", len(secrets))
	}
	wantPaths := []string{"auth.jwt_secret", "database.password", "auth.previous_secrets[0]"}
	for index, want := range wantPaths {
		if secrets[index].Path != want {
			t.Errorf("Secrets()[%d].Path = %q, want %q", index, secrets[index].Path, want)
		}
		if strings.TrimSpace(secrets[index].Value) == "" {
			t.Errorf("Secrets()[%d] (%s) must carry the value", index, want)
		}
	}
	if (*Config)(nil).Secrets() != nil {
		t.Error("Secrets() on a nil config must return nil")
	}
}

// TestRedactedConfigurationNeverCarriesSecretValues proves that the diagnostic
// copy used by `--check` and by the logger cannot leak a credential.
func TestRedactedConfigurationNeverCarriesSecretValues(t *testing.T) {
	cfg := validProduction()
	cfg.Auth.JWTSecret = strings.Repeat("j", 40)
	cfg.Auth.PreviousSecrets = []string{strings.Repeat("p", 40), strings.Repeat("q", 40)}
	cfg.Database.Password = strings.Repeat("d", 20)

	redacted := cfg.Redacted()
	if redacted.Auth.JWTSecret != SecretMask || redacted.Database.Password != SecretMask {
		t.Fatal("Redacted() must mask the signing key and the database password")
	}
	for index, previous := range redacted.Auth.PreviousSecrets {
		if previous != SecretMask {
			t.Fatalf("Redacted().Auth.PreviousSecrets[%d] = %q, want the mask", index, previous)
		}
	}
	// The original must stay untouched: the runtime needs the real values.
	if cfg.Auth.JWTSecret != strings.Repeat("j", 40) {
		t.Fatal("Redacted() must not modify the original configuration")
	}

	rendered := strings.Join(cfg.Summary(), "\n") + fmt.Sprintf("%+v", redacted)
	for _, secret := range cfg.Secrets() {
		if strings.Contains(rendered, secret.Value) {
			t.Fatalf("secret %s leaked into the diagnostic output:\n%s", secret.Path, rendered)
		}
	}
	if !strings.Contains(rendered, SecretMask) {
		t.Fatal("the diagnostic output must show the mask")
	}
	if (*Config)(nil).Redacted() != nil || (*Config)(nil).Summary() != nil {
		t.Error("Redacted() and Summary() must tolerate a nil config")
	}
}

func TestHardenedTiersRejectWeakSigningKeys(t *testing.T) {
	cases := map[string]string{
		"dev placeholder":    DevJWTSecret,
		"too short":          "short-secret",
		"padded":             strings.Repeat("k", MinProductionSecretLength) + " ",
		"placeholder marker": "change-me-please-1234567890abcdefg",
	}
	for _, env := range []string{EnvStaging, EnvProduction} {
		for name, secret := range cases {
			t.Run(env+"/"+name, func(t *testing.T) {
				cfg := validProduction()
				cfg.App.Environment = env
				cfg.Auth.JWTSecret = secret

				err := Validate(cfg)
				if err == nil {
					t.Fatalf("%s must reject the signing key", env)
				}
				if !strings.Contains(err.Error(), "auth.jwt_secret") {
					t.Fatalf("error %q does not mention auth.jwt_secret", err.Error())
				}
			})
		}
	}
}

func TestDevelopmentAcceptsThePlaceholderSecret(t *testing.T) {
	cfg := Default()
	if cfg.Auth.JWTSecret != DevJWTSecret {
		t.Fatalf("the development default must be the placeholder, got %q", cfg.Auth.JWTSecret)
	}
	if err := Validate(&cfg); err != nil {
		t.Fatalf("the relaxed tier must accept the placeholder secret: %v", err)
	}
}

func TestRotationWindowRules(t *testing.T) {
	older := strings.Repeat("o", 40)
	newer := strings.Repeat("n", 40)
	current := strings.Repeat("c", 40)

	cases := []struct {
		name     string
		previous []string
		wantErr  string
	}{
		{"acceptable overlap", []string{older}, ""},
		{"two generations", []string{older, newer}, ""},
		{"three generations", []string{older, newer, strings.Repeat("x", 40)}, "at most"},
		{"identical to the current secret", []string{current}, "must differ"},
		{"weak rotated value", []string{"short"}, "at least"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validProduction()
			cfg.Auth.JWTSecret = current
			cfg.Auth.PreviousSecrets = tc.previous

			err := Validate(cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
			if !strings.Contains(err.Error(), "previous_secrets") {
				t.Fatalf("error %q must name the rotated field", err.Error())
			}
		})
	}
}
