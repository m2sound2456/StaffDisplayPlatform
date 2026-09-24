# Staff Display Platform

Multi-tenant SaaS for displaying staff profiles, promotions and store content on
tablets placed at the storefront — **one application, one database, one domain**.

```text
React / Vite / TypeScript / PWA   +   Go (Gin) API   +   PostgreSQL   +   Realtime   +   Tablet
```

> **Source of Truth:** [`docs/BLUEPRINT.md`](docs/BLUEPRINT.md)
> The original planning input (v1.2) is archived at [`BLUEPRINT.md`](BLUEPRINT.md).

---

## 1. Architecture at a glance

```text
Single Domain + Path  (see BLUEPRINT §32)
─────────────────────────────────────────────────────────
/                     landing / platform overview
/app                  Admin application (store admin & super admin)
/s/{store-slug}       Store display (tablet, e.g. /s/abc)
/setup                Device setup + QR pairing
/healthz, /readyz     liveness / readiness probes
/api/v1/...           Go REST API (same host, path based)
/ws                   Realtime WebSocket (FG22)
```

* **Multi-tenant from day one** — every business row is scoped by `tenant_id` / `store_id`.
* **No subdomains** as core architecture (kept as optional future aliases only).
* **Devices** have their own identity + credential (never a store-admin password).
* **No ESP32 / sensors / external hardware.**

---

## 2. Repository layout

```text
.
├── backend/                 Go API (Gin + GORM + zap + viper)
│   ├── cmd/server/          HTTP server entrypoint (`--check` validates configuration)
│   ├── cmd/migrate/         SQL migration CLI (embedded + --dir)
│   ├── configs/             config.yaml (shared base) + config.<env>.yaml profiles
│   ├── internal/            config, logger, database, server, version,
│   │                        store (domain + repository + scope helpers),
│   │                        auth (users, roles, bcrypt, JWT + rotation, sessions),
│   │                        audit (audit trail recorder), testsupport (test setup)
│   └── migrations/          versioned *.sql migrations (embedded)
├── frontend/                React + Vite + TypeScript PWA (Admin / Display / Setup)
├── deploy/                  nginx reverse proxy + staging/production env templates
├── docs/                    BLUEPRINT, API, DATABASE, DEPLOYMENT, AI_RULES
├── scripts/                 local helper scripts (db create, icon generation)
└── BLUEPRINT.md             archived original v1.2 planning input
```

---

## 3. Requirements

| Tool | Version used | Notes |
|---|---|---|
| Go | 1.25+ (validated on 1.26.4) | `backend/go.mod` |
| Node.js | 22+ (validated on 24.16.0) | npm 11 |
| PostgreSQL | 16 (validated on 16, Windows service `postgresql-x64-16`) | DB `staffdisplay` |

---

## 4. Quick start

### 4.1 Database (once)

```powershell
pwsh ./scripts/create-database.ps1   # creates staffdisplay + staffdisplay_test and migrates
```

### 4.2 Backend

```powershell
cd backend
Copy-Item .env.example .env          # then edit DATABASE_PASSWORD / AUTH_JWT_SECRET
go run ./cmd/migrate up              # apply migrations
go run ./cmd/server                  # http://127.0.0.1:8080
```

Verify:

```powershell
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/api/v1/health
```

### 4.3 Frontend

```powershell
cd frontend
npm install
npm run dev                          # http://127.0.0.1:5173 (proxies /api -> :8080)
```

Open `/` (landing), `/app` (admin shell), `/s/demo` (display shell), `/setup` (pairing shell).

### 4.4 Tests and builds

```powershell
cd backend;  go build ./...; go vet ./...; go test ./...
cd frontend; npm run typecheck; npm run lint; npm run test; npm run build
```

Database integration tests (tenant isolation, slug uniqueness, authentication,
migration and schema checks) run against `staffdisplay_test` and are skipped
unless enabled:

```powershell
cd backend
$env:TEST_DATABASE_INTEGRATION = '1'
$env:DATABASE_PASSWORD         = '<local postgres password>'
go test ./...                  # every suite resets the schema under an advisory lock
```

---

## 5. Configuration

Precedence, lowest to highest: built-in defaults → `backend/configs/config.yaml`
(shared base) → `backend/configs/config.<APP_ENV>.yaml` (environment profile) →
environment variables → `*_FILE` secret files. `CONFIG_PATH` replaces the base and
the profile with one explicit file. Details and the full validation matrix:
[`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) §1.

| `APP_ENV` | Tier | Profile | Validated for |
|---|---|---|---|
| `development` (default) | relaxed | `config.development.yaml` | local machine, console logs, localhost origins |
| `staging` | hardened | `config.staging.yaml` | staging host — strong secrets required |
| `production` | strict | `config.production.yaml` | live deployment — every hardening rule |

An unknown `APP_ENV` is a startup error (no silent fallback). Verify a
configuration without starting the service:

```powershell
cd backend
$env:APP_ENV = 'production'
go run ./cmd/server --check      # prints the effective config with secrets masked
```

Backend variables (`backend/.env`, see `backend/.env.example`):

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `development` | `development` \| `staging` \| `production` (profile + validation tier) |
| `CONFIG_PATH` | — | Explicit YAML config path (replaces base + profile) |
| `SERVER_HOST` / `SERVER_PORT` | `0.0.0.0` / `8080` | HTTP listener |
| `SERVER_TLS_ENABLED` + cert/key | `false` | App-level TLS (only when TLS is not terminated at nginx) |
| `DATABASE_HOST` / `_PORT` / `_USER` / `_PASSWORD` / `_NAME` / `_SSLMODE` / `_TIMEZONE` | `127.0.0.1` / `5432` / `postgres` / — / `staffdisplay` / `disable` / `Asia/Bangkok` | PostgreSQL |
| `DATABASE_MAX_OPEN_CONNECTIONS` / `_MAX_IDLE_CONNECTIONS` / `_CONNECTION_MAX_LIFETIME` | `25` / `5` / `30m` | Connection pool |
| `LOGGING_DEVELOPMENT` / `LOGGING_LEVEL` / `LOGGING_ENCODING` | `true` / `info` / `console` | zap logger (production requires `json`) |
| `AUTH_JWT_SECRET` | dev placeholder | FG4 signing key (≥ 32 chars, no placeholder, in staging/production) |
| `AUTH_JWT_SECRET_FILE` | — | Secret file alternative to `AUTH_JWT_SECRET` (preferred in production) |
| `AUTH_JWT_PREVIOUS_SECRETS` (+ `_FILE`) | — | Rotation window: previous keys still **verify** (max 2); new tokens always use the current key |
| `AUTH_ACCESS_TOKEN_TTL` / `AUTH_REFRESH_TOKEN_TTL` | `15m` / `720h` | JWT lifetime / session (refresh) lifetime |
| `DATABASE_PASSWORD_FILE` | — | Secret file alternative to `DATABASE_PASSWORD` |
| `CORS_ALLOWED_ORIGINS` | dev localhost origins | comma separated bare origins; `*` rejected |
| `STORE_DEFAULT_TIMEZONE` / `STORE_DEFAULT_STATUS` | `Asia/Bangkok` / `active` | Store defaults (FG5) |
| `STORE_SLUG_MIN_LENGTH` / `_MAX_LENGTH` / `_AUTO_GENERATE` / `_EXTRA_RESERVED_SLUGS` | `1` / `63` / `true` / — | Slug policy (can only tighten the DB rules) |
| `APP_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown budget |

**Secrets** never belong in a YAML config file (startup rejects them), in the
repository or in a log line — `Config.Summary()`/`Redacted()` mask every value.
Prefer `*_FILE` variables (`chmod 600`, systemd `LoadCredential=`) in
staging/production; rotation guidance is in
[`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) §2.

Frontend profiles (Vite modes, `.env.development` / `.env.staging` /
`.env.production`):

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `/api/v1` | REST base; must stay same-origin outside development (build fails otherwise) |
| `VITE_DEFAULT_STORE_SLUG` | `demo` | Slug used by `/s/{slug}` helpers |

---

## 6. Frontend routes

| Route | Screen | Status |
|---|---|---|
| `/` | Platform landing + API health | FG1 ✅ |
| `/app` | Admin shell (employees/devices live under `/app/*`) | FG1 shell ✅, login UI FG5+ |
| `/s/{store-slug}` | Store display (tablet, kiosk) | FG1 shell ✅, display FG10+ |
| `/setup` | Device setup + QR pairing | FG1 shell ✅, pairing FG16+ |
| `/ws` | Realtime WebSocket | FG22 |

The API is authenticated (FG4 ✅): `POST /api/v1/auth/login` returns a JWT access
token (15 min) plus a rotating refresh token (30 days), `POST /api/v1/auth/logout`
revokes the session and `GET /api/v1/auth/me` describes the caller — see
[`docs/API.md`](docs/API.md) §3. The tenant scope of every request comes from the
account, never from a request parameter. Store records exist in the database
(`tenants`, `stores`, `audit_logs`, `users`, `user_sessions` — FG2 ✅ / FG4 ✅), but
no route serves stores yet: the store CRUD API is FG5.

---

## 7. Feature Group roadmap (BLUEPRINT §21)

| Phase | Feature Groups | Status |
|---|---|---|
| 1 — Foundation | **FG1 project setup ✅**, **FG2 database schema ✅**, **FG3 configuration hardening ✅**, **FG4 authentication ✅**, FG5 tenant/store model | 🟡 in progress |
| 2 — Employee | FG6 CRUD, FG7 image upload, FG8 status, FG9 ordering | ⬜ |
| 3 — Display | FG10 display page, FG11 responsive tablet UI, FG12 staff slide, FG13 promotion slide, FG14 QR slide, FG15 playlist | ⬜ |
| 4 — Device | FG16 device model, FG17 pairing code, FG18 QR pairing, FG19 device auth, FG20 management, FG21 revoke | ⬜ |
| 5 — Realtime | FG22 WebSocket, FG23–FG26 events + reconnect | ⬜ |
| 6 — Offline/PWA | FG27 service worker, FG28 IndexedDB cache, FG29 offline display, FG30 automatic sync | ⬜ |
| 7 — Production | FG31 nginx, FG32 HTTPS, FG33 DNS, FG34 backup, FG35 logging, FG36 monitoring, FG37 security review | ⬜ |

FG1–FG4 decisions, deviations from the v1.2 input and open items are recorded
in [`docs/BLUEPRINT.md`](docs/BLUEPRINT.md) §26 (change log).
