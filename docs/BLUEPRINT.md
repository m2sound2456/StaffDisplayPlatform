# Staff Display Platform — BLUEPRINT v1.3 (Source of Truth)

> **Status:** v1.3 supersedes the v1.2 planning input archived at `/BLUEPRINT.md`.
> This file is the single source of truth for architecture, scope and the
> feature group roadmap. Every change of behaviour must be reflected here.
>
> Goal: a multi-tenant Web/PWA system that displays staff profiles, availability
> and store content on tablets, served by one application, one database and one
> domain.

Related documents:

| Document | Content |
|---|---|
| `docs/API.md` | REST contract, envelopes, error codes, endpoint roadmap |
| `docs/DATABASE.md` | schema plan, migration system, tenant keys, isolation tests |
| `docs/DEPLOYMENT.md` | nginx, TLS, systemd, backups, verification |
| `docs/AI_RULES.md` | binding working rules for AI agents |
| `README.md` | quick start, environment variables, FG status |

---

# 1. Vision

**Staff Display Platform** is a SaaS for stores that place a tablet at the
storefront so customers can see:

- photos of the staff who are working
- name / nickname
- availability (`available` / `busy` / `break` / `offline`)
- promotions
- QR codes
- store information
- any other content the owner defines

### Principles

1. **No ESP32**, no sensors, no external hardware integration.
2. Tablets run only the Web/PWA client.
3. Backend and database are shared by all tenants.
4. **Multi-tenant from day one.**
5. One store can own several tablets.
6. Every tablet pairs with a store and has its own device identity.
7. Admins manage everything from phone or PC.
8. The display works full screen / kiosk.
9. Admin changes should reach the display in realtime.
10. Scaling to hundreds of stores must never require a server or database per store.

---

# 2. Architecture

```text
                         Internet
                            │
                            ▼
                  ┌───────────────────┐
                  │ Reverse proxy     │  nginx + HTTPS
                  │ display.example.com
                  └─────────┬─────────┘
                            │  (one domain, path based routing)
        ┌───────────────────┼─────────────────────────────┐
        ▼                   ▼                             ▼
  static SPA          Go API /api/v1                 /ws realtime (FG22)
  (/, /app,           Gin + GORM + zap
   /s/{slug},               │
   /setup)                  ├────────────► PostgreSQL 16 (single database)
                            └────────────► local media storage (FG7)
```

Client (one PWA build, three screens):

```text
React / Vite / TypeScript / Tailwind / PWA
├── Admin        /app
├── Display      /s/{store-slug}
└── Device setup /setup   (QR pairing)
```

Backend layers:

```text
cmd/server         HTTP entrypoint + graceful shutdown
cmd/migrate        versioned SQL migration CLI (embedded + --dir)
internal/config    YAML + env + .env, validated per environment
internal/logger    zap structured logging
internal/database  GORM/pgx connection + migration runner + schema lock
internal/server    gin router, middleware, handlers, envelopes
internal/store     store domain model, slug rules, tenant scoped repository, scope helpers
internal/audit     append-only audit trail recorder (§11.10)
internal/testsupport  test-only PostgreSQL schema setup (integration tests)
internal/version   build metadata injected via ldflags
```

---

# 3. Multi-tenant strategy

One application, one database. Every business row is scoped:

```text
Tenant (account boundary; MVP: 1 tenant = 1 store)
  └── Store (slug = public display URL)
        ├── Employees
        ├── Devices            (each with its own display configuration)
        ├── Display slides / playlist
        ├── Display settings
        ├── Media assets
        └── Audit log
```

Rules:

1. `tenant_id` and/or `store_id` on every business table, indexed for the
   store-scoped hot queries.
2. Store scope always comes from the **authenticated identity** (store admin
   token or device token), never from a client supplied id alone.
3. Cross-tenant access returns `404 not_found`, so existence never leaks.
4. Isolation is covered by executable tests (see §12 and `docs/DATABASE.md` §3).

---

# 4. Domain & URL strategy — single domain + path

**The MVP uses one domain with path based routing.** Subdomains are *not* the
core architecture and the application must never depend on them for tenant
isolation.

```text
https://display.example.com/                 landing / platform overview
https://display.example.com/app              admin application
https://display.example.com/s/abc            store display (store slug "abc")
https://display.example.com/s/coffee         store display (store slug "coffee")
https://display.example.com/setup            device setup + pairing
https://display.example.com/api/v1/...       REST API
wss://display.example.com/ws                 realtime (FG22)
https://display.example.com/healthz          liveness probe
https://display.example.com/readyz           readiness probe
```

* A store is a **database record** (`store_id`, `slug`, `name`, …). Creating a
  store through the admin UI immediately publishes its `/s/{slug}` route.
* **Never** create a folder, virtual host, service, certificate or database per
  store.
* Slug rules: `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`, lowercase, unique, and
  must not collide with the reserved paths (`app`, `setup`, `api`, `ws`,
  `healthz`, `readyz`, `assets`, `icons`, `static`).
* Subdomains may later be added as **optional aliases**; they must resolve to the
  same store record, and the app keeps working without them.

# 5. Roles

| Role | Scope | Notes |
|---|---|---|
| **Super admin** | platform | tenants, stores, devices, system settings, platform status |
| **Store admin** | one store | employees, images, availability, display settings, content, devices, QR pairing |
| **Display device** | one store | not a human user: `device_id`, `store_id`, hashed device token, name, status, `last_seen_at`, display configuration |

Authentication for humans arrives in **FG4** (JWT access + refresh token, bcrypt
password hashes, role claims). Device authentication arrives in **FG19**
(`X-Device-Token`, revocable).

---

# 6. Device identity, pairing and configuration

```text
Admin → Create store → Add device → Generate QR / pairing code
      → Open /setup on the tablet → Scan or type the code → Pair
      → Device receives its own credentials → Display starts
```

1. A device has: `id`, `store_id`, `name`, `device_token_hash`, `platform`,
   `app_version`, `orientation`, `status`, `last_seen_at`, plus display config.
2. The **store admin password is never stored on the tablet.**
3. Device tokens and pairing codes are stored **hashed only**, are revocable and
   re-issuable (FG21), and are scoped to exactly one store.
4. Every device belongs to exactly one store; revoking a device must immediately
   stop its access to display data.

**Per-device display configuration** (each tablet independently):

```text
device
  display_mode         stand | handheld
  items_per_page       1 | 4 | 8 | 12
  auto_slide           true | false
  slide_interval_secs  e.g. 5, 10
  loop                 true | false
  show_employee_status true | false
  orientation          landscape | portrait | auto
```

Example fleet:

```text
Tablet-01  stand     4 items  auto slide ON   5s
Tablet-02  handheld  1 item   auto slide OFF  manual swipe
Tablet-03  stand     8 items  auto slide ON   10s
```

---

# 7. Display requirements

The display (`/s/{store-slug}`) must support:

| Requirement | Detail |
|---|---|
| Stand display mode | storefront tablet; pages rotate automatically |
| Handheld display mode | staff show profiles to customers instead of printed paper; manual browse |
| Items per page | **1 / 4 / 8 / 12** |
| Auto slide | ON/OFF |
| Slide interval | configurable per device (seconds) |
| Manual navigation | swipe / next / previous |
| Loop | ON/OFF at the end of the playlist |
| Employee status | show/hide availability |
| Responsive tablet UI | landscape **and** portrait, 16:9 and 4:3, safe areas respected |
| Kiosk | full screen, no admin navigation, minimal chrome |

Display states (retained from v1.2 §7):

```text
LOADING → CONNECTING → DISPLAYING
                           │ connection lost
                           ▼
                        OFFLINE  (cached data keeps rendering — FG29)
                           │ network returns
                           ▼
                       reconnecting → sync (FG30) → DISPLAYING
```

Screen rules:

* big photos, large readable type, few buttons, no unnecessary controls
* smooth page transitions
* explicit connection indicator (implemented in FG1 as `ConnectionIndicator`)
* screen-burn mitigation: subtle drift animation, playlist rotation

---

# 8. Display content and playlist

| Slide | MVP content | FG |
|---|---|---|
| Staff | photo grid with name/nickname and availability | FG12 |
| Promotion | image, text, price/promotion label | FG13 |
| QR | QR code + text + URL | FG14 |
| Store information | store name, logo, opening hours, contact | FG15 |

Playlist: admin ordered list of slides, each with `type`, `duration`, `enabled`,
`display_order`. Staff slides paginate by the device's `items_per_page`; the other
slide types occupy the full screen.

# 9. Realtime (FG22–FG26)

WebSocket at `/ws`, authenticated per store admin or device.

```text
employee.created  employee.updated  employee.deleted  employee.status_changed
display.settings_changed  playlist.updated  content.updated  device.revoked
```

Flow: admin writes → PostgreSQL commit → event fan-out to that store's devices →
display refreshes only the affected data. Tablets must not poll aggressively: the
FG1 `ConnectionIndicator` uses a slow 15s interval purely as a health check and is
replaced by push + reconnect logic in FG22–FG26.

---

# 10. Offline / PWA (FG27–FG30)

The tablet must cache at least: store information, employee list, employee images,
display playlist, display settings, content and device configuration. Offline the
cached snapshot keeps rendering (never a blank screen); on reconnect the client
syncs and returns to live data.

FG1 ships: PWA manifest (`fullscreen`, installable), Workbox service worker with
app-shell precache, offline shell, and API routes **excluded** from service worker
caching so stale API data can never be served.

---

# 11. Security

1. Every endpoint authorises the caller and enforces tenant scope.
2. A store admin can never read or write another store's data.
3. Device credentials can be revoked; revocation takes effect immediately.
4. Store admin passwords are never sent to tablets.
5. Passwords are hashed (bcrypt/argon2); tokens are stored hashed.
6. Uploads validate MIME type, size and decodability; file names are not user
   controlled (no path traversal).
7. Slugs are validated (§4) and unique.
8. Authentication and pairing endpoints are rate limited.
9. HTTPS everywhere in production; `sslmode` must not be `disable` in production.
10. Audit log for privileged actions (`audit_logs`).
11. Security headers are sent by the API; nginx repeats them.

Implemented in FG1: request id propagation, panic recovery with JSON envelope,
access logging, CORS allow-list with production wildcard rejection, security
headers, DSN/password redaction in logs, aggregated configuration validation.

---

# 12. Data model

```text
tenants ─┬─ stores ─┬─ users
         │          ├─ employees ─── media_assets
         │          ├─ devices  (per-device display configuration)
         │          ├─ pairing_codes
         │          ├─ display_slides / display_settings
         │          └─ audit_logs
```

Column level details, indexes, constraints and the migration workflow are in
`docs/DATABASE.md`. Migration tooling (embedded SQL, checksums, per-file
transactions) exists since FG1; FG2 created `tenants`, `stores` and the
`audit_logs` skeleton, and the remaining tables are created by FG4 (users),
FG6 (employees), FG7 (media), FG15 (playlist/settings) and FG16 (devices,
pairing codes).

---

# 13. API surface

Base path `/api/v1` (details in `docs/API.md`):

```text
FG1  ✅ GET /healthz · GET /readyz · GET /api/v1/health · GET /api/v1/version
FG4     POST /auth/login · POST /auth/logout · GET /auth/me
FG5     GET|POST /stores · GET|PUT|DELETE /stores/{id}
FG6–9   GET|POST /stores/{storeId}/employees · PUT|DELETE /employees/{id}
        PATCH /employees/{id}/status · PATCH /employees/{id}/order
FG7     POST /stores/{storeId}/media · GET /media/{assetId}
FG10–15 GET /display/bootstrap · GET /display/config · GET /display/content
FG16–21 POST /stores/{storeId}/devices/pairing · POST /devices/pair
        PATCH /devices/{id} · POST /devices/{id}/revoke
FG22    GET /ws
```

Envelope: success `{"data": …}`, failure `{"error": {"code","message","details"}}`.
`GET /display/bootstrap` returns store, device (with display configuration),
employees, playlist, settings, content and `server_time` in a single call so a
tablet does not fan out requests at start-up.

---

# 14. Media strategy

Binary images are **not** stored in PostgreSQL. `media_assets` stores metadata and
a `storage_key`; a `MediaStorage` interface is implemented by `LocalStorage` for
development and the MVP, with S3-compatible object storage as a later drop-in.
Validation: MIME allow-list (jpeg/png/webp), maximum size, decodable image,
server generated file name, path traversal protection.

# 15. Admin UI (FG4–FG21)

```text
Dashboard      employees / devices / online / offline counters
Store          Profile · Settings
Employees      List · Add/Edit (image, status, display order)
Display        Playlist · Settings · Preview (per device)
Devices        Device list · Pair device (QR / code) · Revoke
Media          Assets
```

FG1 ships the shell (`/app`) with the module map and the API status panel; the
real screens land with their feature groups.

---

# 16. Display UI/UX principles (FG10–FG15)

* Large photos, high contrast text, distance-legible typography.
* Minimal chrome: no navigation menus, no admin controls, no debug output.
* Stand mode: automatic page/slide rotation with configurable interval and loop.
* Handheld mode: one employee per page, swipe / next / previous, auto slide off.
* Item counts 1 / 4 / 8 / 12 with a responsive grid that adapts to orientation.
* Smooth transitions; avoid full-screen white flashes.
* Screen-burn mitigation (drift animation, rotation) and safe-area insets.
* Explicit connection state; never render a blank screen when the network drops.

---

# 17. Repository structure

```text
.
├── backend/            Go API
│   ├── cmd/server/     HTTP entrypoint (graceful shutdown)
│   ├── cmd/migrate/    migration CLI (up | status | version, --dir)
│   ├── configs/        config.yaml · config.production.yaml
│   ├── internal/       config · logger · database · server · version
│   │                   store · audit · testsupport
│   └── migrations/     NNNN_*.sql (embedded via embed.go)
├── frontend/           React + Vite + TypeScript PWA
│   └── src/            app · components · features · lib · pages · services · db · pwa · styles
├── deploy/             nginx.conf · env.production.example
├── docs/               BLUEPRINT · API · DATABASE · DEPLOYMENT · AI_RULES
├── scripts/            create-database.ps1 · generate-icons.ps1
├── BLUEPRINT.md        archived v1.2 input (do not edit)
└── README.md           quick start + FG status
```

---

# 18. Technology stack (as implemented)

| Layer | Choice | Rationale |
|---|---|---|
| Frontend | React 19 + Vite 7 + TypeScript 5.9 + Tailwind 4 + React Router 7 | required by the blueprint; static PWA shell behind nginx |
| PWA | `vite-plugin-pwa` (Workbox) | manifest, installable app, app-shell precache (FG27–FG30 extend the data cache) |
| Frontend tests | Vitest 5 + Testing Library (jsdom) | fast, no browser dependency in CI |
| Backend | Go 1.25+ with Gin, GORM (pgx driver), zap, viper + godotenv | matches the author's established Go conventions |
| Database | PostgreSQL 16 | single database, tenant scoped, `citext`/`pgcrypto` helpers |
| Migrations | embedded, versioned SQL + checksum bookkeeping + `cmd/migrate` | deterministic, auditable, no schema drift |
| Realtime | WebSocket (`/ws`) | FG22 |
| Reverse proxy | nginx | single domain, path routing, TLS termination |

Explicitly **not** used for the MVP: ESP32, hardware sensors, face recognition,
AI recommendations, payments, POS, booking, membership, LINE integration,
subscription billing, complex analytics, per-store servers/databases/subdomains.

---

# 19. Configuration

Precedence: built-in defaults < YAML file < environment (a local `.env` is loaded
first when present).

* `APP_ENV=development` (default) → `configs/config.yaml`
* `APP_ENV=production` → `configs/config.production.yaml` + strict validation
  (rejects the dev JWT secret, short secrets, missing DB password,
  `sslmode=disable`, wildcard CORS, insecure origins, dev logging, TLS without cert)
* `CONFIG_PATH` overrides the file selection; a configured-but-missing file is a
  startup error.

Complete variable list: `README.md` §5, `backend/.env.example`,
`deploy/env.production.example`. No production domain is compiled in.

---

# 20. Development workflow (binding)

```text
Feature Group → implement → test → build → review git diff → commit → next FG
```

Definition of Done for every feature group:

1. `go build ./...`, `go vet ./...`, `go test ./...` pass (backend).
2. `npm run typecheck`, `npm run lint`, `npm run test`, `npm run build` pass (frontend).
3. Migrations applied against a live database and verified (`migrate status`).
4. New/changed behaviour has tests, including tenant isolation where relevant.
5. Documentation updated (`docs/BLUEPRINT.md` change log, `docs/API.md`,
   `docs/DATABASE.md`).
6. `git diff` reviewed; only in-scope files changed; single purpose commit.

**Stop at the review point after each feature group.**

---

# 21. Feature group roadmap

## Phase 1 — Foundation

| FG | Title | Status |
|---|---|---|
| FG1 | Project setup (repo, stack, config, logging, DB tooling, HTTP foundation, PWA shell, docs) | ✅ **complete** |
| FG2 | Database schema (tenants, stores, audit_logs skeleton, slug rules, store repository + tenant isolation tests) | ✅ **complete** |
| FG3 | Configuration hardening (secrets management, per-environment profiles) | ⬜ next |
| FG4 | Authentication (login/logout/me, JWT, roles, rate limiting) | ⬜ |
| FG5 | Tenant/store model + admin store CRUD + isolation tests | ⬜ |

## Phase 2 — Employee

FG6 employee CRUD · FG7 image upload (`MediaStorage`) · FG8 availability status ·
FG9 display ordering

## Phase 3 — Display

FG10 display page · FG11 responsive tablet UI (stand/handheld, 1/4/8/12, timing) ·
FG12 staff slide · FG13 promotion slide · FG14 QR slide · FG15 playlist

## Phase 4 — Device

FG16 device model · FG17 pairing code · FG18 QR pairing · FG19 device auth ·
FG20 device management (last seen, rename, disable) · FG21 revoke / re-issue

## Phase 5 — Realtime

FG22 WebSocket · FG23 employee events · FG24 playlist events · FG25 display
settings events · FG26 reconnect

## Phase 6 — Offline/PWA

FG27 service worker hardening · FG28 IndexedDB/local cache · FG29 offline display ·
FG30 automatic sync

## Phase 7 — Production

FG31 nginx · FG32 HTTPS · FG33 DNS (optional subdomain aliases) · FG34 backup ·
FG35 logging · FG36 monitoring · FG37 security review

# 22. Critical acceptance tests

| Scenario | Expectation | Automated in |
|---|---|---|
| Multi-tenant isolation | Store A login reads/writes A only; any other store's resource returns `404` and the row is unchanged | FG5 |
| Device isolation | Device A bootstrap contains store A data only; Device B only store B | FG19 |
| Revocation | After revoke the device cannot bootstrap or reconnect; access denied | FG21 |
| Realtime | Admin edits an employee → the tablet updates without manual refresh | FG23 |
| Offline | Network off → display keeps rendering cached data (no blank screen) | FG29 |
| Recovery | Network on → automatic reconnect → sync → current data displayed | FG26/FG30 |
| Slug routing | `/s/{slug}` resolves the tenant purely from the store record | FG1 (validation) / FG5 (data) |
| Configuration | `APP_ENV=production` rejects insecure/placeholder settings at startup | ✅ FG1 (`config` tests) |
| Migration drift | Editing an applied migration aborts the run with a checksum error | ✅ FG1 (integration test) |
| API foundation | `/healthz`, `/readyz`, `/api/v1/health`, `/api/v1/version` behave per contract | ✅ FG1 (router tests) |

MVP complete (v1.2 §31 flow retained):

```text
1 admin login → 2 create store → 3 add employee → 4 upload image
→ 5 create display device → 6 pair tablet → 7 tablet opens display
→ 8 employee appears → 9 configure 1/4/8/12 + stand/handheld
→ 10 admin changes employee → 11 tablet updates automatically
→ 12 internet disconnects → 13 tablet keeps showing cached data
→ 14 internet returns → 15 tablet reconnects and syncs
```

---

# 23. Non-goals for the MVP

ESP32 · hardware sensors · face recognition · AI recommendations · payments · POS ·
booking · membership · LINE integration · subscription billing · complex analytics ·
custom domains · white-label · remote screenshot/restart · multiple playlists ·
scheduled content.

---

# 24. Future expansion

AI-generated promotions · QR campaigns · customer interaction · advertising ·
multiple playlists · scheduled content · analytics · remote device management ·
device health · subscription billing · custom domains (e.g.
`display.shopabc.com`) · white-label — all of which build on the same tenant model
without new servers or databases per store.

---

# 25. Golden rule

> **Never turn this into "a website for one store".**

```text
ONE PLATFORM ─┬─ Store A
              ├─ Store B
              ├─ Store C
              └─ Store N
```

Every feature must answer: *"does this still work with 1,000 stores?"* — and the
answer must never be "give each store its own server or database".

---

# 26. Change log

## v1.3 — FG1 (project foundation)

**Decisions taken while establishing the project**

| # | Decision | Reason |
|---|---|---|
| D1 | Single domain + path routing is the **only** MVP architecture; the v1.2 subdomain diagrams (§2, §7) are superseded | §32 final decision; per-store hostnames/certificates would break the one-platform model |
| D2 | The workspace root *is* the project root (`backend/`, `frontend/`, `docs/`, `deploy/`, `scripts/`) | the repository contained only the planning document; no nested duplicate project was created |
| D3 | `docs/BLUEPRINT.md` (this file) is the Source of Truth; root `BLUEPRINT.md` is archived as the v1.2 input | §29 step 2 asks for a corrected `docs/BLUEPRINT.md` |
| D4 | FG numbering follows §25 (FG1 = project setup); the §6 list is treated as a feature catalogue | removes the "FG01 = auth" vs "FG1 = setup" conflict |
| D5 | Display configuration columns live on `devices` (per device) and are complemented by `display_slides` / `display_settings` | satisfies v1.2 §11 and §16 without duplicating the source of truth |
| D6 | Backend reuses the author's established Go stack (Gin, GORM/pgx, zap, viper, testify) | consistency across the author's projects, warm module cache, faster turnaround |
| D7 | Migrations are versioned SQL files with SHA-256 checksums instead of pure ORM auto-migration | auditable shared database; editing applied history fails fast |
| D8 | `PreferSimpleProtocol` is enabled for the pgx driver | required to execute multi-statement, dollar-quoted migration files |
| D9 | API responses use the `{"data"…}` / `{"error"…}` envelope from day one | one predictable contract for all later feature groups |
| D10 | Frontend is a Vite SPA/PWA (not Next.js, although the author's other project uses Next.js) | the blueprint mandates React/Vite/PWA; the kiosk needs a light static shell |
| D11 | Display/API traffic never passes through the service worker cache | prevents stale employee data before FG28 introduces the real offline cache |
| D12 | No Docker dependency introduced | Docker is not installed on the target machine; deployment is nginx + systemd |

**Delivered**

* Repository: git initialised, `.gitignore`, `.editorconfig`, `README.md`.
* Docs: this blueprint, `API.md`, `DATABASE.md`, `DEPLOYMENT.md`, `AI_RULES.md`.
* Backend: Go module, `internal/{config,logger,database,server,version}`,
  `cmd/server`, `cmd/migrate`, embedded migration `0001_init_extensions_and_helpers.sql`,
  config YAMLs, `.env.example`, `Makefile`, 60+ tests (unit + live PostgreSQL integration).
* Frontend: Vite/React/TS/Tailwind PWA, routes `/`, `/app`, `/s/:storeSlug`, `/setup`,
  404, API client, status hook/panel, connection indicator, kiosk shell, generated
  icon set, 52 tests.
* Deploy: `deploy/nginx.conf` (single domain, path routing, rate-limit zones,
  WebSocket upgrade map), `deploy/env.production.example`.
* Database: `staffdisplay` and `staffdisplay_test` created; `migrate up | status | version`
  verified against the live PostgreSQL 16 server.

**Known gaps carried into the next feature groups**

1. No business tables yet — FG2 creates `tenants` and `stores` (the rest follow).
2. No authentication yet — FG4 (routes reserved, config keys already present).
3. Store slugs are validated on both sides, but store records do not exist yet (FG5).
4. `/ws` is reserved but not mounted (FG22).
5. The offline data cache is documented (`frontend/src/db/README.md`), implemented in FG28.
6. Single HTML shell with client-side routing; nginx `try_files` covers deep links
   (verified for `/s/{slug}` in FG1).
7. Rate limiting is prepared in nginx but not enforced per endpoint yet (FG37).

---

## v1.4 — FG2 (database schema)

**Decisions taken while building the schema**

| # | Decision | Reason |
|---|---|---|
| D13 | Store slug is **globally** unique (partial unique index over live rows), not unique per tenant | `/s/{slug}` is the only public key on a single domain: it can only resolve one store, so a `(tenant_id, slug)` key would be unusable for display routing |
| D14 | Slug columns are `citext` and the format CHECK is applied to `slug::text` | `citext` gives case-insensitive lookups without `lower(slug)`; the text cast is what rejects uppercase, so the extra lowercase CHECK was dropped as unreachable |
| D15 | Status columns are `text` + a named CHECK constraint instead of PostgreSQL enums | adding a value stays a plain forward migration (no `ALTER TYPE` lock) and the ORM/serialisation stays simple |
| D16 | `stores.deleted_at` soft delete + partial unique slug index | retiring a store releases its public slug for reuse while the retired row, its FKs and its audit trail survive |
| D17 | `audit_logs.tenant_id` / `store_id` are `ON DELETE SET NULL` and the table has no `updated_at` | history must outlive the rows it refers to; audit rows are append-only |
| D18 | Tenant isolation is enforced inside the repository by `store.Scope` + `ensureTenantScope` | a missing scope becomes a programming error instead of an unscoped query, and cross-tenant ids answer `ErrNotFound` (404) |
| D19 | Schema writers are serialised by the PostgreSQL advisory lock `database.SchemaLockKey` | stops interleaved DDL from concurrent `migrate up` runs, and lets the integration test packages run in parallel |
| D20 | New packages are small and single-purpose: `internal/store` (domain + repository + scope helpers), `internal/audit` (recorder skeleton), `internal/testsupport` (test-only schema setup) | one concern per package (AI_RULES §3); tenant CRUD itself stays in FG5 |

**Delivered**

* Migrations `0002_create_tenants`, `0003_create_stores`, `0004_create_audit_logs`
  (applied and verified against PostgreSQL 16; `migrate status` → 4 applied, 0 pending).
* `internal/store`: `Store` entity + validation (name, slug, status, timezone,
  opening hours, logo url), slug rules shared with the DB and the frontend,
  `Scope` store-scoped query helpers, tenant-scoped repository (create, get, get by
  slug, list + filter/paging, update, soft delete, slug availability) with
  PostgreSQL error mapping (`23505` → `ErrSlugTaken`, `23503` → `ErrTenantNotFound`).
* `internal/audit`: append-only entry model + validated recorder (no caller yet).
* `internal/testsupport`: PostgreSQL integration test setup with schema reset and
  advisory locking.
* Tests: 115 backend tests green, including tenant isolation (read/update/delete),
  slug uniqueness and validation, soft delete + slug reuse, reserved path parity
  between Go/DB/frontend, database constraint enforcement, trigger refresh,
  cascade + audit survival, and migration set integrity.
* Docs: `docs/DATABASE.md` rewritten around the implemented schema,
  `backend/migrations/README.md`, `README.md`, this change log.

**Known gaps carried into the next feature groups**

1. No HTTP surface yet: store CRUD endpoints (`GET|POST /stores`, …) and their
   request/response shapes land in FG5; FG2 ships domain + repository only.
2. `tenants` has a table but no admin service; tenant CRUD is FG5 scope.
3. `audit_logs` is a skeleton: nothing writes to it until the privileged actions
   themselves exist (FG4+).
4. Device identity and the per-device display configuration (stand/handheld,
   1/4/8/12 items, auto slide, interval, loop, employee status) remain exactly as
   planned for FG16 — devices are not modelled in the store schema.
5. Role/authorisation middleware (FG4) still has to translate the repository
   errors into HTTP status codes (`ErrNotFound` → 404, `ErrSlugTaken` → 409,
   validation → 422).






