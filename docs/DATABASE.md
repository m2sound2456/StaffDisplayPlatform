# Database

Engine: **PostgreSQL 16**. One database serves every tenant (BLUEPRINT §3).

* Local name: `staffdisplay`
* Local test name: `staffdisplay_test`
* Multi-tenant rule: business rows carry `tenant_id` and/or `store_id`.
  *A store must never be able to read another store's data.*

---

## 1. Migration system (implemented in FG1 ✅)

Versioned, forward-only SQL migrations embedded into the server binary.

```text
backend/migrations/0001_create_tenants_and_stores.sql
                   0002_create_users.sql
                   …
                   embed.go        ← //go:embed *.sql
backend/internal/database/migrate.go  ← loader + runner + checksums
backend/cmd/migrate/main.go           ← CLI: up | status | version
```

Rules:

1. File name: `NNNN_snake_case_description.sql` (`NNNN` = 4-digit, strictly increasing).
2. Each file is applied inside its own transaction; a failure rolls back that file only.
3. Applied versions are recorded in `schema_migrations(version, name, checksum, applied_at)`.
4. A SHA-256 checksum guards against editing an already-applied file (drift → hard error).
5. Migrations are **never deleted or renumbered** once committed. Fix forward.

CLI:

```bash
go run ./cmd/migrate up        # apply pending migrations
go run ./cmd/migrate status    # show applied/pending + checksums
go run ./cmd/migrate version   # print the schema version
go run ./cmd/migrate up --dir /opt/staffdisplay/server/migrations   # filesystem mode
```

FG1 ships the runner and the (empty) migration set: `up` creates
`schema_migrations` and reports `no pending migrations`. FG2 adds the first
schema migrations below.

---

## 2. Planned schema (FG2+ — designed now, created in FG2)

```text
tenants                     # platform/account boundary (MVP: 1 tenant = 1 store)
  id uuid pk
  name, slug, status, created_at, updated_at

stores                      # BLUEPRINT §6 FG02
  id uuid pk
  tenant_id uuid fk → tenants(id) on delete cascade      # ← isolation root
  name, slug (unique, validated), logo_url, status,
  timezone, opening_hours jsonb, created_at, updated_at
  unique (tenant_id, slug), index on slug

users                       # store admins + super admins (FG4)
  id uuid pk
  tenant_id uuid fk (null for super admin)
  store_id  uuid fk (null for super admin / tenant-wide users)
  email (unique), password_hash, role, status,
  last_login_at, created_at, updated_at

sessions / refresh_tokens   # FG4
  id uuid pk, user_id fk, token_hash, expires_at, revoked_at, created_at

employees                   # FG6
  id uuid pk, store_id uuid fk, tenant_id uuid fk
  name, nickname, image_url / image_asset_id,
  status ∈ {available, busy, break, offline},
  display_order int, active bool, created_at, updated_at
  index (store_id, active, display_order)

devices                     # FG16 — BLUEPRINT §11
  id uuid pk, store_id uuid fk, tenant_id uuid fk
  name, device_token_hash, platform, app_version,
  orientation, status, last_seen_at,
  display_mode ∈ {stand, handheld},
  items_per_page ∈ {1, 4, 8, 12},
  auto_slide bool, slide_interval_seconds int,
  loop bool, show_employee_status bool,
  created_at, updated_at                                  # per-device display config

pairing_codes               # FG17
  id uuid pk, store_id uuid fk, device_name,
  code_hash, expires_at, used_at, created_by, created_at   # short TTL, single use

display_slides              # FG15 playlist
  id uuid pk, store_id uuid fk, tenant_id uuid fk
  type ∈ {staff, promotion, qr, store_info}, title,
  duration_seconds int, enabled bool, display_order int,
  payload jsonb, created_at, updated_at

display_settings            # FG15 store/device level defaults
  id uuid pk, store_id uuid fk, device_id uuid null fk,
  settings jsonb, created_at, updated_at

media_assets                # FG7 — BLUEPRINT §19
  id uuid pk, store_id uuid fk, tenant_id uuid fk
  file_name, storage_key, mime_type, width, height, size_bytes, created_at

audit_logs                  # BLUEPRINT §15.12
  id uuid pk, tenant_id, store_id, actor_type ∈ {user, device, system},
  actor_id, action, entity_type, entity_id, metadata jsonb, ip, created_at
```

Indexing / constraint rules:

1. Every tenant-scoped table carries `tenant_id` **and** `store_id` where a store
   exists, with a composite index starting at `store_id` for the display/employee
   hot paths.
2. Foreign keys use `on delete cascade` from `tenants → stores → children` so a store
   deletion can never orphan rows across tenants.
3. `slug` uniqueness is enforced in the database (globally unique for MVP; revisit to
   `(tenant_id, slug)` when a tenant owns several stores).
4. Device tokens and pairing codes are stored **hashed only**.
5. Soft delete (`deleted_at`) is used for employees/devices; hard delete only via
   explicit retention jobs.

---

## 3. Tenant isolation (verified by tests from FG5 onwards)

```text
store admin (store A) ──token──▶ API ──▶ query WHERE store_id = A   (never A + B)
device (store B)      ──token──▶ API ──▶ query WHERE store_id = B
```

Required executable tests (BLUEPRINT §26, to be added with the feature):

| Test | Expectation |
|---|---|
| Store A reads `/employees` | only store A rows |
| Store A requests store B employee id | `404 not_found` (existence not disclosed) |
| Store A updates store B employee | `404`/`403`, row unchanged |
| Device A bootstrap | contains only store A data |
| Revoked device | `401` on bootstrap and cannot reconnect |
| Super admin | may act across tenants, every action audited |

Integration tests run against `staffdisplay_test` when
`TEST_DATABASE_INTEGRATION=1` is set (see `internal/database/migrate_integration_test.go`).

---

## 4. Local helpers

```powershell
pwsh ./scripts/create-database.ps1      # create DBs (idempotent) + run migrations
psql -U postgres -h 127.0.0.1 -d staffdisplay -c "\dt"
psql -U postgres -h 127.0.0.1 -d staffdisplay -c "select * from schema_migrations order by version;"
```

Connection defaults live in `backend/configs/config.yaml`; secrets come from
`backend/.env` (`DATABASE_PASSWORD`) and are never committed.

