package config

import (
	"strings"
	"testing"
	"time"
)

// accessTokenTTLNotShorter is as long as the default refresh token TTL, which
// the access token must never reach.
const accessTokenTTLNotShorter = 30 * 24 * time.Hour

func TestValidateRejectsIndividualViolations(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"missing app name", func(c *Config) { c.App.Name = "" }, "app.name"},
		{"missing timezone", func(c *Config) { c.App.Timezone = "" }, "app.timezone"},
		{"invalid timezone", func(c *Config) { c.App.Timezone = "Asia Bangkok" }, "IANA timezone"},
		{"unknown environment", func(c *Config) { c.App.Environment = "prod" }, "app.environment"},
		{"zero shutdown grace", func(c *Config) { c.App.ShutdownGrace = 0 }, "app.shutdown_grace"},
		{"port out of range", func(c *Config) { c.Server.Port = 0 }, "server.port"},
		{"zero read timeout", func(c *Config) { c.Server.ReadTimeout = 0 }, "server.read_timeout"},
		{"bad logging level", func(c *Config) { c.Logging.Level = "verbose" }, "logging.level"},
		{"bad logging encoding", func(c *Config) { c.Logging.Encoding = "xml" }, "logging.encoding"},
		{"bad sslmode", func(c *Config) { c.Database.SSLMode = "sometimes" }, "database.sslmode"},
		{"invalid database timezone", func(c *Config) { c.Database.Timezone = "Asia Bangkok" }, "IANA timezone"},
		{"pool sanity", func(c *Config) { c.Database.MaxIdleConnections = 99 }, "max_idle_connections"},
		{"missing jwt secret", func(c *Config) { c.Auth.JWTSecret = " " }, "auth.jwt_secret"},
		{"zero refresh ttl", func(c *Config) { c.Auth.RefreshTokenTTL = 0 }, "auth.refresh_token_ttl"},
		{"access ttl not shorter than refresh ttl", func(c *Config) { c.Auth.AccessTokenTTL = accessTokenTTLNotShorter }, "shorter than"},
		{"wildcard origin", func(c *Config) { c.CORS.AllowedOrigins = []string{"*"} }, "wildcard"},
		{"origin with a path", func(c *Config) { c.CORS.AllowedOrigins = []string{"https://display.example.com/app"} }, "absolute origin"},
		{"origin with a trailing slash", func(c *Config) { c.CORS.AllowedOrigins = []string{"https://display.example.com/"} }, "absolute origin"},
		{"origin without a scheme", func(c *Config) { c.CORS.AllowedOrigins = []string{"display.example.com"} }, "absolute origin"},
		{"missing store timezone", func(c *Config) { c.Store.DefaultTimezone = "" }, "store.default_timezone"},
		{"invalid store status", func(c *Config) { c.Store.DefaultStatus = "open" }, "store.default_status"},
		{"slug min length below the database rule", func(c *Config) { c.Store.Slug.MinLength = 0 }, "store.slug.min_length"},
		{"slug max length above the database rule", func(c *Config) { c.Store.Slug.MaxLength = 64 }, "store.slug.max_length"},
		{"slug bounds inverted", func(c *Config) {
			c.Store.Slug.MinLength = 10
			c.Store.Slug.MaxLength = 5
		}, "must not exceed"},
		{"extra reserved slug is a platform path", func(c *Config) { c.Store.Slug.ExtraReservedSlugs = []string{"setup"} }, "extra_reserved_slugs"},
		{"extra reserved slug is not a slug", func(c *Config) { c.Store.Slug.ExtraReservedSlugs = []string{"Ops Room"} }, "extra_reserved_slugs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := Validate(&cfg)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// validProduction returns a production configuration that passes validation.
func validProduction() *Config {
	cfg := Default()
	cfg.App.Environment = EnvProduction
	cfg.Logging.Development = false
	cfg.Logging.Encoding = "json"
	cfg.Database.Password = "super-secret"
	cfg.Database.SSLMode = "require"
	cfg.Auth.JWTSecret = strings.Repeat("s", MinProductionSecretLength)
	cfg.CORS.AllowedOrigins = []string{"https://display.example.com"}
	return &cfg
}

// validStaging returns a staging configuration that passes validation: the
// hardened tier requires strong credentials but tolerates a looser transport and
// a console log format.
func validStaging() *Config {
	cfg := Default()
	cfg.App.Environment = EnvStaging
	cfg.Database.Password = "super-secret"
	cfg.Auth.JWTSecret = strings.Repeat("s", MinProductionSecretLength)
	cfg.CORS.AllowedOrigins = []string{"https://staging.display.example.com"}
	return &cfg
}

func TestValidateAcceptsValidProductionConfiguration(t *testing.T) {
	if err := Validate(validProduction()); err != nil {
		t.Fatalf("baseline production configuration must validate, got: %v", err)
	}
}

func TestValidateAcceptsValidStagingConfiguration(t *testing.T) {
	if err := Validate(validStaging()); err != nil {
		t.Fatalf("baseline staging configuration must validate, got: %v", err)
	}
}

func TestValidateStagingKeepsLooserTransportSettings(t *testing.T) {
	cfg := validStaging()
	cfg.Database.SSLMode = "disable"
	cfg.Logging.Development = true
	cfg.Logging.Encoding = "console"
	cfg.CORS.AllowedOrigins = []string{"http://staging.internal"}
	if err := Validate(cfg); err != nil {
		t.Fatalf("staging may relax transport settings, got: %v", err)
	}
}

func TestValidateProductionRules(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"placeholder jwt secret", func(c *Config) { c.Auth.JWTSecret = DevJWTSecret }, "development placeholder"},
		{"short jwt secret", func(c *Config) { c.Auth.JWTSecret = "too-short" }, "at least"},
		{"padded jwt secret", func(c *Config) {
			c.Auth.JWTSecret = strings.Repeat("s", MinProductionSecretLength) + " "
		}, "whitespace"},
		{"missing db password", func(c *Config) { c.Database.Password = "" }, "database.password"},
		{"short db password", func(c *Config) { c.Database.Password = "short" }, "at least"},
		{"weak db password", func(c *Config) { c.Database.Password = "staffdisplay" }, "weak"},
		{"sslmode disable", func(c *Config) { c.Database.SSLMode = "disable" }, "database.sslmode"},
		{"remote database without verification", func(c *Config) {
			c.Database.Host = "db.example.com"
			c.Database.SSLMode = "require"
		}, "verify-ca"},
		{"dev logging enabled", func(c *Config) { c.Logging.Development = true }, "logging.development"},
		{"console logging", func(c *Config) { c.Logging.Encoding = "console" }, "logging.encoding"},
		{"no cors origins", func(c *Config) { c.CORS.AllowedOrigins = nil }, "cors.allowed_origins"},
		{"wildcard cors", func(c *Config) { c.CORS.AllowedOrigins = []string{"*"} }, "wildcard"},
		{"insecure origin", func(c *Config) { c.CORS.AllowedOrigins = []string{"http://display.example.com"} }, "https://"},
		{"tls without cert", func(c *Config) { c.Server.TLS.Enabled = true }, "server.tls.cert_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validProduction()
			tc.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateReportsNilConfig(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) must return an error")
	}
}

func TestValidateHardenedTiersApplyToStagingAsWell(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"placeholder jwt secret", func(c *Config) { c.Auth.JWTSecret = DevJWTSecret }, "development placeholder"},
		{"short jwt secret", func(c *Config) { c.Auth.JWTSecret = "too-short" }, "at least"},
		{"missing db password", func(c *Config) { c.Database.Password = "" }, "database.password"},
		{"wildcard cors", func(c *Config) { c.CORS.AllowedOrigins = []string{"*"} }, "wildcard"},
		{"no cors origins", func(c *Config) { c.CORS.AllowedOrigins = nil }, "cors.allowed_origins"},
		{"tls without cert", func(c *Config) { c.Server.TLS.Enabled = true }, "server.tls.cert_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validStaging()
			tc.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	cfg := validProduction()
	cfg.Database.Password = ""
	cfg.CORS.AllowedOrigins = nil
	cfg.Logging.Development = true

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	for _, want := range []string{"database.password", "cors.allowed_origins", "logging.development"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q (every problem must be reported in one pass)", err.Error(), want)
		}
	}
}
