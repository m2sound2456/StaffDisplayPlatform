package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// applyEnv overlays environment variables on top of cfg. Every supported
// variable is documented in README.md §5, backend/.env.example and
// docs/DEPLOYMENT.md §2.
func applyEnv(cfg *Config) error {
	var err error

	// --- app ---
	envString("APP_NAME", &cfg.App.Name)
	envString("APP_TIMEZONE", &cfg.App.Timezone)
	if err = envDuration("APP_SHUTDOWN_TIMEOUT", &cfg.App.ShutdownGrace); err != nil {
		return err
	}

	// --- server ---
	envString("SERVER_HOST", &cfg.Server.Host)
	if err = envInt("SERVER_PORT", &cfg.Server.Port); err != nil {
		return err
	}
	if err = envDuration("SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout); err != nil {
		return err
	}
	if err = envDuration("SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout); err != nil {
		return err
	}
	if err = envDuration("SERVER_IDLE_TIMEOUT", &cfg.Server.IdleTimeout); err != nil {
		return err
	}
	if err = envBool("SERVER_TLS_ENABLED", &cfg.Server.TLS.Enabled); err != nil {
		return err
	}
	envString("SERVER_TLS_CERT_FILE", &cfg.Server.TLS.CertFile)
	envString("SERVER_TLS_KEY_FILE", &cfg.Server.TLS.KeyFile)

	// --- logging ---
	if err = envBool("LOGGING_DEVELOPMENT", &cfg.Logging.Development); err != nil {
		return err
	}
	envString("LOGGING_LEVEL", &cfg.Logging.Level)
	envString("LOGGING_ENCODING", &cfg.Logging.Encoding)

	// --- database ---
	envString("DATABASE_HOST", &cfg.Database.Host)
	if err = envInt("DATABASE_PORT", &cfg.Database.Port); err != nil {
		return err
	}
	envString("DATABASE_USER", &cfg.Database.User)
	envString("DATABASE_PASSWORD", &cfg.Database.Password)
	envString("DATABASE_NAME", &cfg.Database.Name)
	envString("DATABASE_SSLMODE", &cfg.Database.SSLMode)
	envString("DATABASE_TIMEZONE", &cfg.Database.Timezone)
	if err = envInt("DATABASE_MAX_OPEN_CONNECTIONS", &cfg.Database.MaxOpenConnections); err != nil {
		return err
	}
	if err = envInt("DATABASE_MAX_IDLE_CONNECTIONS", &cfg.Database.MaxIdleConnections); err != nil {
		return err
	}
	if err = envDuration("DATABASE_CONNECTION_MAX_LIFETIME", &cfg.Database.ConnectionMaxLifetime); err != nil {
		return err
	}

	// --- auth (used from FG4 onwards) ---
	// AUTH_JWT_SECRET is also accepted as AUTH_JWT_SECRET_FILE (see secrets.go).
	envString("AUTH_JWT_SECRET", &cfg.Auth.JWTSecret)
	if raw, ok := lookupEnv("AUTH_JWT_PREVIOUS_SECRETS"); ok && raw != "" {
		cfg.Auth.PreviousSecrets = splitSecretList(raw)
	}
	if err = envDuration("AUTH_ACCESS_TOKEN_TTL", &cfg.Auth.AccessTokenTTL); err != nil {
		return err
	}
	if err = envDuration("AUTH_REFRESH_TOKEN_TTL", &cfg.Auth.RefreshTokenTTL); err != nil {
		return err
	}

	// --- cors ---
	// An empty variable keeps the profile value: an unset (or blank) variable in
	// a systemd EnvironmentFile must not silently wipe the allow-list.
	if raw, ok := lookupEnv("CORS_ALLOWED_ORIGINS"); ok && raw != "" {
		cfg.CORS.AllowedOrigins = splitList(raw)
	}

	// --- store defaults (consumed by the store service from FG5) ---
	envString("STORE_DEFAULT_TIMEZONE", &cfg.Store.DefaultTimezone)
	if raw, ok := lookupEnv("STORE_DEFAULT_STATUS"); ok && raw != "" {
		cfg.Store.DefaultStatus = store.Status(strings.ToLower(raw))
	}
	if err = envInt("STORE_SLUG_MIN_LENGTH", &cfg.Store.Slug.MinLength); err != nil {
		return err
	}
	if err = envInt("STORE_SLUG_MAX_LENGTH", &cfg.Store.Slug.MaxLength); err != nil {
		return err
	}
	if err = envBool("STORE_SLUG_AUTO_GENERATE", &cfg.Store.Slug.AutoGenerate); err != nil {
		return err
	}
	if raw, ok := lookupEnv("STORE_SLUG_EXTRA_RESERVED_SLUGS"); ok && raw != "" {
		cfg.Store.Slug.ExtraReservedSlugs = splitList(raw)
	}

	return nil
}

func lookupEnv(key string) (string, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(raw), true
}

func envString(key string, target *string) {
	if value, ok := lookupEnv(key); ok && value != "" {
		*target = value
	}
}

func envInt(key string, target *int) error {
	value, ok := lookupEnv(key)
	if !ok || value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("environment %s must be an integer: %w", key, err)
	}
	*target = parsed
	return nil
}

func envBool(key string, target *bool) error {
	value, ok := lookupEnv(key)
	if !ok || value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("environment %s must be a boolean: %w", key, err)
	}
	*target = parsed
	return nil
}

func envDuration(key string, target *time.Duration) error {
	value, ok := lookupEnv(key)
	if !ok || value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("environment %s must be a duration (e.g. 15s, 30m): %w", key, err)
	}
	*target = parsed
	return nil
}

// splitList parses a comma separated environment value, dropping empty items.
func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
