package database

import (
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
)

func TestOptionsFromConfigTrimsAndNormalizes(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "  127.0.0.1 ",
		Port:     5432,
		User:     " postgres ",
		Name:     " staffdisplay ",
		SSLMode:  " DISABLE ",
		Timezone: " Asia/Bangkok ",
	}

	opts := OptionsFromConfig(cfg)
	if opts.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want trimmed", opts.Host)
	}
	if opts.User != "postgres" {
		t.Errorf("User = %q, want trimmed", opts.User)
	}
	if opts.Name != "staffdisplay" {
		t.Errorf("Name = %q, want trimmed", opts.Name)
	}
	if opts.SSLMode != "disable" {
		t.Errorf("SSLMode = %q, want lowercase", opts.SSLMode)
	}
	if opts.Timezone != "Asia/Bangkok" {
		t.Errorf("Timezone = %q, want trimmed", opts.Timezone)
	}
}

func TestOptionsDSNDefaults(t *testing.T) {
	opts := Options{Host: "db.internal", Port: 5432, User: "app", Password: "p", Name: "staffdisplay"}
	dsn := opts.DSN()

	for _, want := range []string{
		"host=db.internal", "port=5432", "user=app", "dbname=staffdisplay",
		"sslmode=disable", "TimeZone=UTC", "connect_timeout=10",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN() = %q, missing %q", dsn, want)
		}
	}
}

func TestRedactedDSNHidesPassword(t *testing.T) {
	opts := Options{Host: "127.0.0.1", Port: 5432, User: "postgres", Password: "super-secret", Name: "staffdisplay", SSLMode: "disable"}

	redacted := opts.RedactedDSN()
	if strings.Contains(redacted, "super-secret") {
		t.Fatalf("RedactedDSN() leaks the password: %q", redacted)
	}
	if !strings.Contains(redacted, "password=***") {
		t.Fatalf("RedactedDSN() = %q, want a masked password", redacted)
	}
	if got := opts.String(); got != redacted {
		t.Fatalf("String() = %q, want the redacted DSN", got)
	}
}

func TestRedactedDSNWithoutPassword(t *testing.T) {
	opts := Options{Host: "127.0.0.1", Port: 5432, User: "postgres", Name: "staffdisplay"}
	if opts.RedactedDSN() != opts.DSN() {
		t.Fatal("RedactedDSN() must equal DSN() when no password is set")
	}
}

func TestWithDebugReturnsCopy(t *testing.T) {
	original := Options{Host: "127.0.0.1"}
	debugged := original.WithDebug(true)

	if debugged.Debug != true {
		t.Error("WithDebug(true) did not enable debug")
	}
	if original.Debug {
		t.Error("WithDebug mutated the original options")
	}
}

func TestNilDatabaseIsSafe(t *testing.T) {
	var db *Database
	if db.Gorm() != nil {
		t.Error("nil Database.Gorm() must return nil")
	}
	if db.SQL() != nil {
		t.Error("nil Database.SQL() must return nil")
	}
	if err := db.Close(); err != nil {
		t.Errorf("nil Database.Close() = %v, want nil", err)
	}
	if err := db.EnsureMigrationTable(nil); err == nil { //nolint:staticcheck // nil ctx is intentional here
		t.Error("EnsureMigrationTable on a nil database must fail")
	}
}
