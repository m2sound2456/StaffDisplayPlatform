# Migrations

Forward-only, versioned SQL migrations for the single shared PostgreSQL database
(BLUEPRINT §3 — one database for every tenant).

## Naming

```text
NNNN_snake_case_description.sql

0001_init_extensions_and_helpers.sql   ← FG1  pgcrypto + citext + set_updated_at()
0002_create_tenants.sql                ← FG2  tenants
0003_create_stores.sql                 ← FG2  stores (slug rules, soft delete, tenant FK cascade)
0004_create_audit_logs.sql             ← FG2  audit_logs skeleton (append-only)
0005_create_users.sql                  ← FG4  users (roles, bcrypt hashes, tenant scope)
0006_create_user_sessions.sql          ← FG4  user_sessions (hashed refresh tokens, revocation)
0007_…                                 ← next feature group
```

`NNNN` is a 4 digit version, strictly increasing. A version is **never** reused,
renumbered or edited after it has been applied — fix forward with a new file.
The runner stores a SHA-256 checksum per version and refuses to continue when an
applied file changed (drift detection).

## Rules

1. One logical change per file.
2. Every business table carries `tenant_id` and/or `store_id` (see `docs/DATABASE.md`).
3. Attach `set_updated_at()` (created in 0001) to tables with an `updated_at` column.
4. Prefer `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` so local
   re-runs stay painless; migrations are still applied exactly once.
5. Migrations run inside a transaction per file (no `CONCURRENTLY` DDL here).

## Usage

```bash
go run ./cmd/migrate up        # apply all pending migrations
go run ./cmd/migrate status    # applied / pending overview
go run ./cmd/migrate version   # current schema version

# deploy from a plain directory instead of the embedded FS
go run ./cmd/migrate up --dir /opt/staffdisplay/server/migrations
```

`up` takes the PostgreSQL advisory lock `database.SchemaLockKey` before applying
anything, so two concurrent applies (or an integration test suite resetting the
schema) queue instead of interleaving DDL.

The embedded copy is provided by `embed.go` (`//go:embed *.sql`), so the built
binary is self contained. `--dir` is only needed when operators ship raw SQL.

## Bookkeeping

```sql
select * from schema_migrations order by version;
-- version | name                                | checksum | applied_at
```
