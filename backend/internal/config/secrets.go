package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	// SecretFileEnvSuffix turns a secret variable into a file reference:
	// AUTH_JWT_SECRET_FILE points at a file that holds the value. This is the
	// preferred production source (systemd LoadCredential, Docker/Kubernetes
	// secrets, a chmod 600 file) because the value never appears in
	// `systemctl show`, `ps e` or a journald command line.
	SecretFileEnvSuffix = "_FILE"

	// MinProductionPasswordLength is the minimum database password length in a
	// hardened tier. It is lower than the JWT minimum on purpose: the database
	// role is additionally protected by pg_hba.conf and never leaves the host.
	MinProductionPasswordLength = 12

	// MaxPreviousSecrets bounds the rotation window. Keeping at most two
	// previous values lets an operator rotate with zero downtime and still
	// forces the rotation to be finished within one more generation.
	MaxPreviousSecrets = 2

	// SecretMask replaces every credential in Redacted() and Summary() output.
	SecretMask = "***redacted***"
)

// placeholderMarkers are fragments that only survive in a secret that was never
// replaced by a real one. Matching is case-insensitive and deliberately narrow
// so a legitimate random value is never rejected.
var placeholderMarkers = []string{
	"change_me", "change-me", "changeme", "changethis",
	"replace_me", "replace-me", "replaceme", "replace-this",
	"placeholder", "your-secret", "yoursecret", "dev-secret",
	"example", "dummy", "secretkey", "secret-key", "please-change",
}

// weakSecrets are exact values that are never acceptable in a hardened tier,
// even when padded to the minimum length.
var weakSecrets = []string{
	"secret", "password", "passw0rd", "test", "test1234", "dev", "develop",
	"admin", "jwt", "jwtsecret", "jwt-secret", "staffdisplay", "qwerty",
	"letmein", "1234", "12345", "123456", "1234567890", "12345678",
	"0123456789", "abcdefgh", "aaaa", "xxxx",
}

// secretFileBindings lists the secret variables that may be sourced from a file
// (`<DIRECT>_FILE`) instead of the environment. The direct variable and its file
// variant are mutually exclusive: silently preferring one would hide a half
// finished rotation.
var secretFileBindings = []struct {
	// Direct is the variable holding the literal value; its file variant is
	// Direct + SecretFileEnvSuffix.
	Direct string
	// Apply stores the resolved value on the configuration.
	Apply func(*Config, string)
}{
	{
		Direct: "AUTH_JWT_SECRET",
		Apply:  func(cfg *Config, value string) { cfg.Auth.JWTSecret = value },
	},
	{
		Direct: "DATABASE_PASSWORD",
		Apply:  func(cfg *Config, value string) { cfg.Database.Password = value },
	},
	{
		Direct: "AUTH_JWT_PREVIOUS_SECRETS",
		Apply:  func(cfg *Config, value string) { cfg.Auth.PreviousSecrets = splitSecretList(value) },
	},
}

// secretFileVar returns the file variable of a secret variable.
func secretFileVar(direct string) string { return direct + SecretFileEnvSuffix }

// applySecretFiles resolves every *_FILE secret reference. It runs after the
// environment overlay so it can see whether the direct variable was set too.
func applySecretFiles(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	for _, binding := range secretFileBindings {
		fileVar := secretFileVar(binding.Direct)
		path, ok := lookupEnv(fileVar)
		if !ok || path == "" {
			continue
		}
		if direct, directSet := lookupEnv(binding.Direct); directSet && direct != "" {
			return fmt.Errorf("set either %s or %s, not both", binding.Direct, fileVar)
		}

		value, err := readSecretFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", fileVar, err)
		}
		binding.Apply(cfg, value)
	}
	return nil
}

// readSecretFile reads a credential from a file and trims surrounding
// whitespace: both `openssl rand -base64 48 > file` and most editors add a
// trailing newline that must not become part of the secret.
func readSecretFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read secret file %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("secret file %q is a directory", path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read secret file %q: %w", path, err)
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("secret file %q is empty", path)
	}
	return value, nil
}

// splitSecretList parses a rotated secret list from a comma, semicolon or
// newline separated value (a secret file may therefore hold one value per line).
func splitSecretList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// secretStrengthError returns nil when a signing key is strong enough for a
// hardened tier (staging, production) and a field level message otherwise.
func secretStrengthError(field, value string) error {
	return secretRuleError(field, value, MinProductionSecretLength)
}

// passwordStrengthError returns nil when a database password is strong enough
// for a hardened tier and a field level message otherwise.
func passwordStrengthError(field, value string) error {
	return secretRuleError(field, value, MinProductionPasswordLength)
}

// secretRuleError implements the shared rule: present, not padded with
// whitespace, not the development placeholder, not a well-known weak value or a
// placeholder fragment, and long enough. The specific checks run before the
// length check so the operator gets the most useful message. The value itself is
// never included in the message.
func secretRuleError(field, value string, minLength int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}

	lowered := strings.ToLower(value)
	if lowered == DevJWTSecret {
		return fmt.Errorf("%s must not be the development placeholder", field)
	}
	for _, weak := range weakSecrets {
		if lowered == weak {
			return fmt.Errorf("%s is a well-known weak value; generate one with 'openssl rand -base64 48'", field)
		}
	}
	for _, marker := range placeholderMarkers {
		if strings.Contains(lowered, marker) {
			return fmt.Errorf("%s still contains the placeholder %q; replace it with a generated secret", field, marker)
		}
	}
	if len(value) < minLength {
		return fmt.Errorf("%s must be at least %d characters", field, minLength)
	}
	return nil
}

// SecretField names a credential held by the configuration. It exists so a
// diagnostic (or a test) can iterate over every secret without hard coding the
// field list.
type SecretField struct {
	// Path is the configuration path, e.g. "database.password".
	Path string
	// Value is the secret itself. It is never meant to be logged.
	Value string
}

// Secrets lists every credential of the configuration in a stable order.
func (c *Config) Secrets() []SecretField {
	if c == nil {
		return nil
	}
	secrets := []SecretField{
		{Path: "auth.jwt_secret", Value: c.Auth.JWTSecret},
		{Path: "database.password", Value: c.Database.Password},
	}
	for index, previous := range c.Auth.PreviousSecrets {
		secrets = append(secrets, SecretField{
			Path:  fmt.Sprintf("auth.previous_secrets[%d]", index),
			Value: previous,
		})
	}
	return secrets
}

// Redacted returns a copy of the configuration with every credential masked, so
// it is safe to log, print from `--check` or embed in an error message.
func (c *Config) Redacted() *Config {
	if c == nil {
		return nil
	}

	out := *c
	out.Auth.JWTSecret = SecretMask
	if len(c.Auth.PreviousSecrets) > 0 {
		previous := make([]string, len(c.Auth.PreviousSecrets))
		for index := range previous {
			previous[index] = SecretMask
		}
		out.Auth.PreviousSecrets = previous
	}
	out.Database.Password = SecretMask
	return &out
}

// Summary describes the effective configuration line by line with every secret
// masked. `staffdisplay-server --check` prints it so an operator can verify a
// production configuration without starting the service, and no credential can
// reach the terminal or a log file through it.
func (c *Config) Summary() []string {
	if c == nil {
		return nil
	}
	safe := c.Redacted()

	return []string{
		fmt.Sprintf("environment: %s (tier %s)", safe.App.Environment, ActiveTier(c)),
		fmt.Sprintf("config files: %s", joinedOrNone(safe.Files)),
		fmt.Sprintf("app: name=%s timezone=%s shutdown_grace=%s version=%s",
			safe.App.Name, safe.App.Timezone, safe.App.ShutdownGrace, safe.App.Version),
		fmt.Sprintf("server: %s:%d tls=%t", safe.Server.Host, safe.Server.Port, safe.Server.TLS.Enabled),
		fmt.Sprintf("database: user=%s name=%s host=%s port=%d sslmode=%s password=%s timezone=%s",
			safe.Database.User, safe.Database.Name, safe.Database.Host, safe.Database.Port,
			safe.Database.SSLMode, safe.Database.Password, safe.Database.Timezone),
		fmt.Sprintf("auth: jwt_secret=%s previous_secrets=%d access_token_ttl=%s refresh_token_ttl=%s",
			safe.Auth.JWTSecret, len(safe.Auth.PreviousSecrets),
			safe.Auth.AccessTokenTTL, safe.Auth.RefreshTokenTTL),
		fmt.Sprintf("logging: development=%t level=%s encoding=%s",
			safe.Logging.Development, safe.Logging.Level, safe.Logging.Encoding),
		fmt.Sprintf("cors: origins=%s", joinedOrNone(safe.CORS.AllowedOrigins)),
		fmt.Sprintf("store defaults: timezone=%s status=%s slug_min=%d slug_max=%d slug_auto_generate=%t slug_extra_reserved=%s",
			safe.Store.DefaultTimezone, safe.Store.DefaultStatus, safe.Store.Slug.MinLength,
			safe.Store.Slug.MaxLength, safe.Store.Slug.AutoGenerate,
			joinedOrNone(safe.Store.Slug.ExtraReservedSlugs)),
	}
}

// joinedOrNone renders a list for the human readable summary.
func joinedOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}
