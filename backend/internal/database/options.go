package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
)

// Options holds the resolved PostgreSQL connection parameters used to build the
// DSN. It is deliberately independent from config so tests can build a
// connection without loading a configuration file.
type Options struct {
	Host                  string
	Port                  int
	User                  string
	Password              string
	Name                  string
	SSLMode               string
	Timezone              string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
	// Debug enables GORM's verbose SQL logging (development only).
	Debug bool
}

// OptionsFromConfig converts the application configuration into Options.
func OptionsFromConfig(cfg config.DatabaseConfig) Options {
	return Options{
		Host:                  strings.TrimSpace(cfg.Host),
		Port:                  cfg.Port,
		User:                  strings.TrimSpace(cfg.User),
		Password:              cfg.Password,
		Name:                  strings.TrimSpace(cfg.Name),
		SSLMode:               strings.ToLower(strings.TrimSpace(cfg.SSLMode)),
		Timezone:              strings.TrimSpace(cfg.Timezone),
		MaxOpenConnections:    cfg.MaxOpenConnections,
		MaxIdleConnections:    cfg.MaxIdleConnections,
		ConnectionMaxLifetime: cfg.ConnectionMaxLifetime,
	}
}

// WithDebug returns a copy of the options with SQL logging toggled.
func (o Options) WithDebug(debug bool) Options {
	o.Debug = debug
	return o
}

// DSN builds the PostgreSQL connection string.
func (o Options) DSN() string {
	timezone := o.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	sslMode := o.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s connect_timeout=10",
		o.Host, o.Port, o.User, o.Password, o.Name, sslMode, timezone,
	)
}

// RedactedDSN returns the connection string with the password masked so it is
// safe to write to logs and error messages.
func (o Options) RedactedDSN() string {
	if o.Password == "" {
		return o.DSN()
	}
	return strings.Replace(o.DSN(), "password="+o.Password, "password=***", 1)
}

// String implements fmt.Stringer for safe logging.
func (o Options) String() string { return o.RedactedDSN() }
