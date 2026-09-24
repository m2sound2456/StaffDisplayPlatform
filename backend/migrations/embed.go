// Package migrations embeds the versioned SQL migrations so the server and the
// migration CLI never depend on the working directory (BLUEPRINT §16, §22).
//
// File naming: NNNN_snake_case_description.sql — strictly increasing, never
// renumbered or edited after being applied. See backend/migrations/README.md.
package migrations

import "embed"

// FS holds every migration file. Consumers use
// database.LoadMigrations(migrations.FS, ".").
//
//go:embed *.sql
var FS embed.FS
