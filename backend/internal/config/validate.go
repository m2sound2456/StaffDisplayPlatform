package config

import (
	"fmt"
	"strings"
)

// Validate checks a resolved configuration and reports every problem it finds,
// so an operator can fix them in one pass instead of one restart at a time.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// --- app ---
	if strings.TrimSpace(cfg.App.Name) == "" {
		report("app.name is required")
	}
	if strings.TrimSpace(cfg.App.Timezone) == "" {
		report("app.timezone is required")
	}
	if cfg.App.ShutdownGrace <= 0 {
		report("app.shutdown_grace must be > 0")
	}

	// --- server ---
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		report("server.port must be between 1 and 65535")
	}
	if cfg.Server.ReadTimeout <= 0 {
		report("server.read_timeout must be > 0")
	}
	if cfg.Server.WriteTimeout <= 0 {
		report("server.write_timeout must be > 0")
	}
	if cfg.Server.IdleTimeout <= 0 {
		report("server.idle_timeout must be > 0")
	}

	// --- logging ---
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case "debug", "info", "warn", "warning", "error", "fatal":
	default:
		report("logging.level must be one of: debug, info, warn, error, fatal")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Encoding)) {
	case "json", "console":
	default:
		report("logging.encoding must be one of: json, console")
	}

	// --- database ---
	if strings.TrimSpace(cfg.Database.Host) == "" {
		report("database.host is required")
	}
	if cfg.Database.Port <= 0 || cfg.Database.Port > 65535 {
		report("database.port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Database.User) == "" {
		report("database.user is required")
	}
	if strings.TrimSpace(cfg.Database.Name) == "" {
		report("database.name is required")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Database.SSLMode)) {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		report("database.sslmode must be one of: disable, allow, prefer, require, verify-ca, verify-full")
	}
	if strings.TrimSpace(cfg.Database.Timezone) == "" {
		report("database.timezone is required")
	}
	if cfg.Database.MaxOpenConnections <= 0 {
		report("database.max_open_connections must be > 0")
	}
	if cfg.Database.MaxIdleConnections < 0 {
		report("database.max_idle_connections must be >= 0")
	}
	if cfg.Database.MaxIdleConnections > cfg.Database.MaxOpenConnections {
		report("database.max_idle_connections must not exceed database.max_open_connections")
	}
	if cfg.Database.ConnectionMaxLifetime < 0 {
		report("database.connection_max_lifetime must be >= 0")
	}

	// --- auth ---
	if strings.TrimSpace(cfg.Auth.JWTSecret) == "" {
		report("auth.jwt_secret is required")
	}
	if cfg.Auth.AccessTokenTTL <= 0 {
		report("auth.access_token_ttl must be > 0")
	}
	if cfg.Auth.RefreshTokenTTL <= 0 {
		report("auth.refresh_token_ttl must be > 0")
	}

	if IsProduction(cfg) {
		validateProduction(cfg, report)
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// validateProduction enforces the rules that must never reach a live tenant.
func validateProduction(cfg *Config, report func(string, ...any)) {
	if cfg.Auth.JWTSecret == DevJWTSecret {
		report("auth.jwt_secret must not be the development placeholder in production")
	} else if len(cfg.Auth.JWTSecret) < MinProductionSecretLength {
		report("auth.jwt_secret must be at least %d characters in production", MinProductionSecretLength)
	}
	if strings.TrimSpace(cfg.Database.Password) == "" {
		report("database.password is required in production")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Database.SSLMode), "disable") {
		report("database.sslmode must not be 'disable' in production; use require, verify-ca or verify-full")
	}
	if cfg.Logging.Development {
		report("logging.development must be false in production")
	}
	if len(cfg.CORS.AllowedOrigins) == 0 {
		report("cors.allowed_origins must list the platform origin in production")
	}
	for _, origin := range cfg.CORS.AllowedOrigins {
		if origin == "*" {
			report("cors.allowed_origins must not contain the wildcard '*' in production")
			continue
		}
		if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://localhost") {
			report("cors.allowed_origins entry %q must use https:// (http:// is only allowed for localhost)", origin)
		}
	}
	if cfg.Server.TLS.Enabled {
		if strings.TrimSpace(cfg.Server.TLS.CertFile) == "" {
			report("server.tls.cert_file is required when server.tls.enabled is true")
		}
		if strings.TrimSpace(cfg.Server.TLS.KeyFile) == "" {
			report("server.tls.key_file is required when server.tls.enabled is true")
		}
	}
}
