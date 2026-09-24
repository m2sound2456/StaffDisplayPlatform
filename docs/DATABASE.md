# Database

Engine: **PostgreSQL 16**. One database serves every tenant (BLUEPRINT §3).

* Local name: `staffdisplay`
* Local test name: `staffdisplay_test`
* Multi-tenant rule: business rows carry `tenant_id` and/or `store_id`.
  *A store must never be able to read another store's data.*

---

## 1. Migration system (FG1 ✅, first schema migrations in FG2 ✅)

Versioned, forward-only SQL migrations embedded into the server binary.

```text
backend/migrations/0001_init_extensions_and_helpers.sql   ← FG1  pgcrypto + citext + set_updated_at()
                   0002_create_tenants.sql                ← FG2  tenants
                   0003_create_stores.sql                 ← FG2  stores
                   0004_create_audit_logs.sql             ← FG2  audit_logs (skeleton)
                   embed.go        ← //go:embed *.sql
backend/internal/database/migrate.go  ← loader + runner + checksums
backend/internal/database/lock.go     ← advisory lock that serialises schema writers
backend/cmd/migrate/main.go           ← CLI: up | status | version
```

Rules:

1. File name: `NNNN_snake_case_description.sql` (`NNNN` = 4-digit, strictly increasing).
2. Each file is applied inside its own transaction; a failure rolls back that file only.
3. Applied versions are recorded in `schema_migrations(version, name, checksum, applied_at)`.
4. A SHA-256 checksum guards against editing an already-applied file (drift → hard error).
5. Migrations are **never deleted or renumbered** once committed. Fix forward.
6. Schema writers are serialised by the PostgreSQL advisory lock
   `database.SchemaLockKey` (`pg_advisory_lock`): `migrate up` and the integration
   suites take it, so two applies can never interleave DDL.

CLI:

```bash
go run ./cmd/migrate up        # apply pending migrations
go run ./cmd/migrate status    # show applied/pending + checksums
go run ./cmd/migrate version   # print the schema version
go run ./cmd/migrate up --dir /opt/staffdisplay/server/migrations   # filesystem mode
```

---

## 2. Implemented schema (FG2 ✅)

Column types are the ones PostgreSQL reports (`citext` / `jsonb` / `inet` / `timestamptz`).

### 2.1 `tenants` — platform account boundary (migration 0002)

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` | PK, `gen_random_uuid()` |
| `name` | `text` | 1-120 characters, trimmed non-blank (CHECK) |
| `slug` | `citext` | `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`, globally unique |
| `status` | `text` | `active` \| `suspended` \| `archived` |
| `created_at`, `updated_at` | `timestamptz` | `updated_at` refreshed by `set_updated_at()` |

### 2.2 `stores` — the record behind `/s/{slug}` (migration 0003)

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` | PK, `gen_random_uuid()` |
| `tenant_id` | `uuid` | **isolation root**, `REFERENCES tenants(id) ON DELETE CASCADE` |
| `name` | `text` | 1-120 characters, trimmed non-blank |
| `slug` | `citext` | public display key, see §2.4 |
| `logo_url` | `text` | optional, ≤ 512 characters, never blank |
| `status` | `text` | `active` \| `inactive` \| `archived` |
| `timezone` | `text` | IANA name (default `UTC`), 2-64 characters |
| `opening_hours` | `jsonb` | JSON **object** (default `{}`) |
| `created_at`, `updated_at` | `timestamptz` | `updated_at` refreshed by trigger |
| `deleted_at` | `timestamptz` | soft delete; NULL = live |

Indexes: `stores_slug_active_key` (partial unique), `stores_tenant_id_idx`,
`stores_tenant_status_idx` (both partial on live rows).

---

### 2.3 `audit_logs` — append-only skeleton (migration 0004)

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` | PK, `gen_random_uuid()` |
| `tenant_id`, `store_id` | `uuid` | nullable, `REFERENCES … ON DELETE SET NULL` so history survives a deletion |
| `actor_type` | `text` | `user` \| `device` \| `system` |
| `actor_id` | `uuid` | user id / device id / NULL for system actions |
| `action` | `text` | dotted snake_case action, e.g. `store.created`, 3-120 characters |
| `entity_type`, `entity_id` | `text`, `uuid` | affected resource |
| `metadata` | `jsonb` | JSON **object**; never contains secrets, tokens or passwords |
| `ip` | `inet` | caller address, optional |
| `created_at` | `timestamptz` | no `updated_at`: audit rows are immutable |

Indexes: `audit_logs_store_created_idx`, `audit_logs_tenant_created_idx`,
`audit_logs_action_created_idx`.

FG2 ships the table **and** the write path (`internal/audit`). Nothing records
yet: every privileged action starts auditing in the feature group that
introduces the action (auth FG4, devices FG16–FG21, …).

### 2.4 Store slug uniqueness (decision)

A store is addressed as `/s/{slug}` on the single platform domain, so the slug is
the **only** routing key: it must be globally unique, not unique per tenant. That
is enforced with a partial unique index over live rows (BLUEPRINT §4):

```sql
CREATE UNIQUE INDEX stores_slug_active_key ON stores (slug) WHERE deleted_at IS NULL;
```

* `citext` makes lookups case insensitive without `lower(slug)`;
* the CHECK constraint applies the slug pattern to `slug::text` — a bare `citext`
  match would be case insensitive and accept uppercase;
* soft deleting a store releases its slug for reuse while keeping the retired row
  and its audit trail.

Devices are deliberately **not** part of this model: a device has its own
identity (`id` + hashed token) and its own display configuration — stand or
handheld, 1/4/8/12 items per page, auto slide, slide interval, loop, employee
status — and is bound to a store only through `store_id` (FG16–FG21).

### 2.5 Planned tables (next feature groups)

```text
users, sessions / refresh_tokens   # FG4  authentication (hashed passwords, rotated refresh tokens)
employees                          # FG6  availability status, display_order
media_assets                       # FG7  metadata only; files live on disk (MediaStorage)
devices                            # FG16 id, store_id, hashed device token, per-device display config
pairing_codes                      # FG17 short TTL, single use, stored hashed
display_slides, display_settings   # FG15 playlist and store/device level defaults
```

Indexing / constraint rules:

1. Every tenant-scoped table carries `tenant_id` **and** `store_id` where a store
   exists, with a composite index starting at `store_id` for the display/employee
   hot paths.
2. Foreign keys use `on delete cascade` from `tenants → stores → children` so a store
   deletion can never orphan rows across tenants.
3. Status columns are `text` + a named CHECK constraint (documented values) instead of
   PostgreSQL enums: adding a value stays a plain forward migration.
4. Device tokens and pairing codes are stored **hashed only**.
5. Soft delete (`deleted_at`) is used for stores, employees and devices; hard delete
   only via explicit retention jobs.

---

## 3. Tenant isolation (executable from FG2 ✅, extended per feature group)

```text
store admin (store A) ──token──▶ API ──▶ query WHERE tenant_id = A / store_id = A   (never A + B)
device (store B)      ──token──▶ API ──▶ query WHERE store_id = B
```

Implemented today (`internal/store`, `internal/database`):

| Test | Expectation | Where |
|---|---|---|
| Tenant A reads a tenant B store id | `ErrNotFound` → `404 not_found` (existence not disclosed) | `internal/store/repository_integration_test.go` |
| Tenant A lists stores | only its own rows | same |
| Tenant A updates a tenant B store | `ErrNotFound`, row unchanged (also when the payload claims tenant A) | same |
| Tenant A deletes a tenant B store | `ErrNotFound`, row untouched | same |
| Repository call without tenant scope | refused before any query (`ErrMissingTenantScope`) | `internal/store/repository_test.go` |
| Slug lookup `/s/{slug}` | resolves exactly one store, case insensitive, deleted stores invisible | `internal/store/repository_integration_test.go`, `internal/database/schema_integration_test.go` |
| Slug uniqueness | duplicate slug (any tenant, any case) → `ErrSlugTaken` → `409 conflict` | `internal/store/repository_integration_test.go` |
| Database constraints | reserved/uppercase/malformed slugs, blank names, unknown status/timezone, non-object JSON and unknown tenants are rejected even from raw SQL | `internal/database/schema_integration_test.go` |
| Cross-tenant cascade | deleting a tenant cascades to its stores; audit rows survive with NULL references | `internal/database/schema_integration_test.go`, `internal/audit/audit_integration_test.go` |

Still scheduled for the feature groups that introduce the actor (BLUEPRINT §22):
device bootstrap isolation (FG19), revocation (FG21), super admin auditing (FG4+).

Integration tests run against `staffdisplay_test` when
`TEST_DATABASE_INTEGRATION=1` is set (plus `DATABASE_PASSWORD`); every suite
resets the schema through `internal/testsupport`, which serialises resets with
`database.SchemaLockKey` so `go test ./...` can run the packages in parallel.

```powershell
cd backend
$env:TEST_DATABASE_INTEGRATION = '1'
$env:DATABASE_PASSWORD = '<local postgres password>'
go test ./...
```

---

## 4. Local helpers

```powershell
pwsh ./scripts/create-database.ps1      # create DBs (idempotent) + run migrations
psql -U postgres -h 127.0.0.1 -d staffdisplay -P pager=off -c "\dt"
psql -U postgres -h 127.0.0.1 -d staffdisplay -P pager=off -c "select * from schema_migrations order by version;"
```

Connection defaults live in `backend/configs/config.yaml`; secrets come from
`backend/.env` (`DATABASE_PASSWORD`) and are never committed.


