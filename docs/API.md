# API reference

Base URL: `https://display.example.com/api/v1` (same domain, path based — no per-store host).

Every store-scoped resource is resolved from the caller's identity (store admin token
or device token) plus the store slug/UUID. A client-supplied `store_id` is never
trusted on its own.

---

## 1. Conventions

### Response envelope

Success:

```json
{ "data": { } }
```

Failure (`Content-Type: application/json`):

```json
{
  "error": {
    "code": "validation_failed",
    "message": "slug must match ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$",
    "details": { "field": "slug" }
  }
}
```

### Error codes

| HTTP | `code` | Meaning |
|---|---|---|
| 400 | `bad_request` | Malformed body/query |
| 401 | `unauthorized` | Missing/invalid credentials |
| 403 | `forbidden` | Authenticated but outside the tenant scope |
| 404 | `not_found` | Unknown route or resource (never leaks cross-tenant existence) |
| 405 | `method_not_allowed` | Wrong HTTP method |
| 409 | `conflict` | Unique constraint (e.g. duplicate store slug) |
| 422 | `validation_failed` | Field level validation errors |
| 429 | `rate_limited` | Too many attempts (auth/pairing) |
| 500 | `internal_error` | Unhandled server error |
| 503 | `service_unavailable` | Dependency (DB) unavailable |

### Headers

| Header | Direction | Notes |
|---|---|---|
| `X-Request-ID` | in/out | Echoed when supplied, otherwise generated (UUID v4) and returned |
| `Authorization: Bearer <jwt>` | in | Store admin / super admin (FG4) |
| `X-Device-Token: <token>` | in | Display device credential (FG19) |

---

## 2. Implemented in FG1 ✅

### `GET /healthz` — liveness

Never touches the database. `200` → `{"status":"ok","service":"staffdisplay-api","time":"…"}`.

### `GET /readyz` — readiness

Checks PostgreSQL. `200` when ready, `503` with `{"status":"unavailable","checks":{"database":{…}}}` when not.

### `GET /api/v1/health`

```json
{
  "data": {
    "status": "ok",
    "service": "staffdisplay-api",
    "environment": "development",
    "version": "0.1.0",
    "uptime_seconds": 42,
    "server_time": "2026-09-24T10:00:00Z",
    "checks": {
      "database": { "status": "ok", "latency_ms": 1 }
    }
  }
}
```

`status` is `ok` (all checks pass), `degraded` (a non-critical check failed) or
`unavailable`. The endpoint itself always answers `200` so monitoring can read the
detail; use `/readyz` as the load-balancer gate.

### `GET /api/v1/version`

```json
{
  "data": {
    "version": "0.1.0",
    "git_commit": "unknown",
    "build_time": "unknown",
    "go_version": "go1.26.4",
    "environment": "development"
  }
}
```

---

## 3. Planned (see roadmap in `docs/BLUEPRINT.md` §25)

| FG | Method + path | Notes |
|---|---|---|
| FG4 | `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me` | bcrypt hash, JWT access token, refresh token rotation |
| FG5 | `GET/POST /api/v1/stores`, `GET/PUT/DELETE /api/v1/stores/{id}` | creating a store publishes `GET /s/{slug}` immediately; super admin + store admin scoped |
| FG6–FG9 | `GET/POST /api/v1/stores/{storeId}/employees`, `PUT/DELETE /api/v1/employees/{id}`, `PATCH /api/v1/employees/{id}/status`, `PATCH /api/v1/employees/{id}/order` | tenant scoped, `display_order` for staff slides |
| FG7 | `POST /api/v1/stores/{storeId}/media`, `GET /media/{assetId}` | MIME + size validation, `MediaStorage` abstraction |
| FG10–FG15 | `GET /api/v1/display/bootstrap`, `GET /api/v1/display/config`, `GET /api/v1/display/content` | single-call bootstrap for tablets (BLUEPRINT §18) |
| FG16–FG21 | `POST /api/v1/stores/{storeId}/devices/pairing`, `POST /api/v1/devices/pair`, `PATCH /api/v1/devices/{id}`, `POST /api/v1/devices/{id}/revoke` | pairing code/QR TTL, hashed device tokens, revoke + re-issue |
| FG22 | `GET /ws?token=…` | WebSocket, `employee.*`, `display.*`, `content.*`, `device.revoked` events |

### Display bootstrap contract (target, FG10)

```json
{
  "data": {
    "store":      { "id": "…", "slug": "abc", "name": "…", "timezone": "Asia/Bangkok" },
    "device":     { "id": "…", "name": "Tablet-01", "display_mode": "stand",
                    "items_per_page": 4, "auto_slide": true,
                    "slide_interval_seconds": 5, "loop": true,
                    "show_employee_status": true, "orientation": "landscape" },
    "employees":  [],
    "playlist":   [],
    "content":    [],
    "settings":   {},
    "server_time": "2026-09-24T10:00:00Z"
  }
}
```

---

## 4. CORS

`cors.allowed_origins` (env `CORS_ALLOWED_ORIGINS`) is an allow-list. In production a
wildcard is rejected during config validation and the browser is sent an explicit
`Access-Control-Allow-Origin`. In development, if the list is empty, the API reflects
the requesting origin to keep the Vite dev server usable.

## 5. Rate limiting (FG37)

Authentication and pairing endpoints are rate limited per IP and per identifier
(`auth.login`, `devices.pair`). Until then, nginx `limit_req` is the outer guard
(see `deploy/nginx.conf`).
