package database

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

func TestLoadMigrationsOrdersByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0010_tenth.sql":  {Data: []byte("select 10;")},
		"0002_second.sql": {Data: []byte("select 2;")},
		"0001_first.sql":  {Data: []byte("select 1;")},
	}

	loaded, err := LoadMigrations(fsys, ".")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("len(migrations) = %d, want 3", len(loaded))
	}
	wantOrder := []int{1, 2, 10}
	for i, want := range wantOrder {
		if loaded[i].Version != want {
			t.Errorf("migrations[%d].Version = %d, want %d", i, loaded[i].Version, want)
		}
		if len(loaded[i].Checksum) != 64 {
			t.Errorf("migration %s checksum %q is not a sha256 hex digest", loaded[i].Filename, loaded[i].Checksum)
		}
	}
	if loaded[0].Name != "first" {
		t.Errorf("migrations[0].Name = %q, want first", loaded[0].Name)
	}
}

func TestLoadMigrationsRejectsInvalidFileNames(t *testing.T) {
	invalid := []string{
		"abc.sql",
		"1_too_short.sql",
		"0002_Bad-Case.sql",
		"migration.sql",
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{name: {Data: []byte("select 1;")}}
			if _, err := LoadMigrations(fsys, "."); err == nil {
				t.Fatalf("expected an error for migration file %q", name)
			}
		})
	}
}

func TestLoadMigrationsRejectsDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_first.sql":     {Data: []byte("select 1;")},
		"0001_duplicate.sql": {Data: []byte("select 2;")},
	}
	_, err := LoadMigrations(fsys, ".")
	if err == nil {
		t.Fatal("expected an error for duplicate migration versions")
	}
	if !strings.Contains(err.Error(), "duplicate migration version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadMigrationsRejectsEmptyFile(t *testing.T) {
	fsys := fstest.MapFS{"0001_empty.sql": {Data: []byte("   \n\t\n")}}
	if _, err := LoadMigrations(fsys, "."); err == nil {
		t.Fatal("expected an error for an empty migration")
	}
}

func TestLoadMigrationsSupportsSubdirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"db/0001_first.sql": {Data: []byte("select 1;")},
		"other/ignored.sql": {Data: []byte("select 9;")},
	}
	loaded, err := LoadMigrations(fsys, "db")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Filename != "0001_first.sql" {
		t.Fatalf("unexpected migrations: %#v", loaded)
	}
}

func TestLoadMigrationsEmptySetIsNotAnError(t *testing.T) {
	loaded, err := LoadMigrations(fstest.MapFS{}, ".")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("len(migrations) = %d, want 0", len(loaded))
	}
}

func TestLoadMigrationsRejectsNilFilesystem(t *testing.T) {
	if _, err := LoadMigrations(nil, "."); err == nil {
		t.Fatal("expected an error for a nil filesystem")
	}
}

// TestEmbeddedMigrationsAreValid guarantees the shipped migration set is
// loadable and starts at version 1 (FG1 foundation migration).
func TestEmbeddedMigrationsAreValid(t *testing.T) {
	loaded, err := LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("embedded migrations must load: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("expected at least one embedded migration")
	}
	if loaded[0].Version != 1 {
		t.Errorf("first embedded migration version = %d, want 1", loaded[0].Version)
	}
	if loaded[0].Checksum == "" {
		t.Error("embedded migration has no checksum")
	}
}

func TestMigrationsAreOrderedStrictlyIncreasing(t *testing.T) {
	loaded, err := LoadMigrations(migrations.FS, ".")
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	for i := 1; i < len(loaded); i++ {
		if loaded[i].Version <= loaded[i-1].Version {
			t.Fatalf("migration order broken at index %d: %d after %d", i, loaded[i].Version, loaded[i-1].Version)
		}
	}
}
