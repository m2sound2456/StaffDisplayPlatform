package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsIndividualViolations(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"missing app name", func(c *Config) { c.App.Name = "" }, "app.name"},
		{"missing timezone", func(c *Config) { c.App.Timezone = "" }, "app.timezone"},
		{"port out of range", func(c *Config) { c.Server.Port = 0 }, "server.port"},
		{"zero read timeout", func(c *Config) { c.Server.ReadTimeout = 0 }, "server.read_timeout"},
		{"bad logging level", func(c *Config) { c.Logging.Level = "verbose" }, "logging.level"},
		{"bad logging encoding", func(c *Config) { c.Logging.Encoding = "xml" }, "logging.encoding"},
		{"bad sslmode", func(c *Config) { c.Database.SSLMode = "sometimes" }, "database.sslmode"},
		{"pool sanity", func(c *Config) { c.Database.MaxIdleConnections = 99 }, "max_idle_connections"},
		{"missing jwt secret", func(c *Config) { c.Auth.JWTSecret = " " }, "auth.jwt_secret"},
		{"zero refresh ttl", func(c *Config) { c.Auth.RefreshTokenTTL = 0 }, "auth.refresh_token_ttl"},
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
	cfg.Database.Password = "super-secret"
	cfg.Database.SSLMode = "require"
	cfg.Auth.JWTSecret = strings.Repeat("s", MinProductionSecretLength)
	cfg.CORS.AllowedOrigins = []string{"https://display.example.com"}
	return &cfg
}

func TestValidateAcceptsValidProductionConfiguration(t *testing.T) {
	if err := Validate(validProduction()); err != nil {
		t.Fatalf("baseline production configuration must validate, got: %v", err)
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
		{"missing db password", func(c *Config) { c.Database.Password = "" }, "database.password"},
		{"sslmode disable", func(c *Config) { c.Database.SSLMode = "disable" }, "database.sslmode"},
		{"dev logging enabled", func(c *Config) { c.Logging.Development = true }, "logging.development"},
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
