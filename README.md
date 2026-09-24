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
│   ├── cmd/server/          HTTP server entrypoint
│   ├── cmd/migrate/         SQL migration CLI (embedded + --dir)
│   ├── configs/             config.yaml / config.production.yaml
│   ├── internal/            config, logger, database, server, version
│   └── migrations/          versioned *.sql migrations (embedded)
├── frontend/                React + Vite + TypeScript PWA (Admin / Display / Setup)
├── deploy/                  nginx reverse proxy + production env template
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

---

## 5. Environment variables

Backend (`backend/.env`, see `backend/.env.example`):

| Variable | Default | Purpose |
|---|---|---|
| `APP_ENV` | `development` | `development` \| `production` (config file selection + strict validation) |
| `CONFIG_PATH` | — | Explicit YAML config path (overrides `APP_ENV` selection) |
| `SERVER_HOST` / `SERVER_PORT` | `0.0.0.0` / `8080` | HTTP listener |
| `SERVER_TLS_ENABLED` + cert/key | `false` | App-level TLS (only when TLS is not terminated at nginx) |
| `DATABASE_HOST` / `_PORT` / `_USER` / `_PASSWORD` / `_NAME` / `_SSLMODE` / `_TIMEZONE` | `127.0.0.1` / `5432` / `postgres` / — / `staffdisplay` / `disable` / `Asia/Bangkok` | PostgreSQL |
| `DATABASE_MAX_OPEN_CONNECTIONS` / `_MAX_IDLE_CONNECTIONS` / `_CONNECTION_MAX_LIFETIME` | `25` / `5` / `30m` | Connection pool |
| `LOGGING_DEVELOPMENT` / `LOGGING_LEVEL` / `LOGGING_ENCODING` | `true` / `info` / `console` | zap logger |
| `AUTH_JWT_SECRET` | dev placeholder | **FG4** signing key (strong + required in production) |
| `CORS_ALLOWED_ORIGINS` | dev localhost origins | comma separated; `*` rejected in production |
| `APP_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown budget |

Frontend (`frontend/.env.development`, `.env.production`):

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `/api/v1` | REST base URL (same-origin in production) |
| `VITE_DEFAULT_STORE_SLUG` | `demo` | Slug used by `/s/{slug}` helpers |

---

## 6. Frontend routes

| Route | Screen | Status |
|---|---|---|
| `/` | Platform landing + API health | FG1 ✅ |
| `/app` | Admin shell (employees/devices live under `/app/*`) | FG1 shell ✅, features FG4+ |
| `/s/{store-slug}` | Store display (tablet, kiosk) | FG1 shell ✅, display FG10+ |
| `/setup` | Device setup + QR pairing | FG1 shell ✅, pairing FG16+ |
| `/ws` | Realtime WebSocket | FG22 |

---

## 7. Feature Group roadmap (BLUEPRINT §25)

| Phase | Feature Groups | Status |
|---|---|---|
| 1 — Foundation | **FG1 project setup ✅**, FG2 database, FG3 configuration, FG4 authentication, FG5 tenant/store model | 🟡 in progress |
| 2 — Employee | FG6 CRUD, FG7 image upload, FG8 status, FG9 ordering | ⬜ |
| 3 — Display | FG10 display page, FG11 responsive tablet UI, FG12 staff slide, FG13 promotion slide, FG14 QR slide, FG15 playlist | ⬜ |
| 4 — Device | FG16 device model, FG17 pairing code, FG18 QR pairing, FG19 device auth, FG20 management, FG21 revoke | ⬜ |
| 5 — Realtime | FG22 WebSocket, FG23–FG26 events + reconnect | ⬜ |
| 6 — Offline/PWA | FG27 service worker, FG28 IndexedDB cache, FG29 offline display, FG30 automatic sync | ⬜ |
| 7 — Production | FG31 nginx, FG32 HTTPS, FG33 DNS, FG34 backup, FG35 logging, FG36 monitoring, FG37 security review | ⬜ |

FG1 decisions, deviations from the v1.2 input and open items are recorded in
[`docs/BLUEPRINT.md`](docs/BLUEPRINT.md) §34 (change log).
