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
| `Authorization: Bearer <jwt>` | in | Super admin / store admin access token (FG4 ✅) |
| `WWW-Authenticate: Bearer realm="staffdisplay"` | out | Sent with every `401` of an authenticated route |
| `X-Device-Token: <token>` | in | Display device credential (FG19) |

---

## 2. Platform endpoints (FG1 ✅)

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

## 3. Authentication (FG4 ✅)

Human accounts only. A display device is a **different actor** with its own
credential (FG19) and is never a user account; a store-admin password is never
stored on a tablet.

| Role | Scope |
|---|---|
| `super_admin` | platform: no tenant, no store |
| `store_admin` | exactly one tenant (optionally bound to one store) |

* **Access token** — HS256 JWT, 15 min (`AUTH_ACCESS_TOKEN_TTL`), signed with the
  current `AUTH_JWT_SECRET`. Claims: `sub` (user id), `sid` (session id), `role`,
  `tenant_id`, `store_id`, `iss` (`staffdisplay`), `iat`, `nbf`, `exp`.
* **Refresh token** — opaque 256 bit value, 30 days (`AUTH_REFRESH_TOKEN_TTL`),
  stored as a sha256 digest and **rotated on every use**. A replayed refresh
  token is rejected (`401`).
* **Rotation window** — a key in `AUTH_JWT_PREVIOUS_SECRETS` still **verifies**
  tokens during a rotation, but new tokens are always signed with the current
  key. Dropping the previous key finishes the rotation.
* **Authorization** — the role, status, tenant and store of the caller are
  re-read from the account row on **every** request, so disabling an account,
  changing its role, moving it to another tenant or revoking its session takes
  effect immediately. A JWT claim is a mirror for diagnostics, never the source
  of an authorization decision.
* **Logout** — revokes the session (`user_sessions.revoked_at`, reason
  `logout`). The credential is dead on the next request.

### `POST /api/v1/auth/login`

```json
{ "email": "admin@example.com", "password": "…" }
```

`200`:

```json
{
  "data": {
    "user": {
      "id": "…", "email": "admin@example.com", "display_name": "Store Admin",
      "role": "store_admin", "status": "active",
      "tenant_id": "…", "store_id": "…",
      "last_login_at": "2026-09-24T10:00:00Z", "created_at": "2026-09-01T08:00:00Z"
    },
    "tokens": {
      "access_token": "…", "token_type": "Bearer", "expires_in": 900,
      "refresh_token": "…", "refresh_expires_in": 2592000
    }
  }
}
```

Errors: `401 unauthorized` (wrong password **or** unknown address — identical
response, so accounts cannot be enumerated), `403 forbidden` (correct password on
a disabled account), `422 validation_failed` (field details), `400 bad_request`
(body is not a JSON object).

### `POST /api/v1/auth/refresh`

```json
{ "refresh_token": "…" }
```

`200` → `{"data": {"tokens": {…}}}` with a **new** refresh token. `401` when the
token is unknown, revoked, expired or already rotated away.

### `POST /api/v1/auth/logout`

Requires `Authorization`. `200` → `{"data": {"logged_out": true}}`. The same
credential answers `401` afterwards (including a repeated logout).

### `GET /api/v1/auth/me`

Requires `Authorization`. `200` → `{"data": {…user…}}` (the account as the
database describes it now). Any `tenant_id` / `store_id` / `slug` / `role` query
parameter is **ignored**: the scope of a caller is server controlled
(BLUEPRINT §3.2).

### `GET /api/v1/auth/users`

Requires `Authorization`. Lists the accounts the caller may see: its own tenant
for a store admin, every tenant for a super admin. `?limit=` (≤ 200) and
`?offset=` page the result. `200` → `{"data": {"users": […], "count": n}}`.
Password hashes are never returned.

### Not in FG4

Rate limiting of `/auth/login` and `/auth/refresh` (FG37, nginx `limit_req` until
then), password change/reset, multi-factor authentication and user management
(creating/editing accounts, FG5+).

---

## 4. Planned (see roadmap in `docs/BLUEPRINT.md` §21)

| FG | Method + path | Notes |
|---|---|---|
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

## 5. CORS

`cors.allowed_origins` (env `CORS_ALLOWED_ORIGINS`) is an allow-list. In production a
wildcard is rejected during config validation and the browser is sent an explicit
`Access-Control-Allow-Origin`. In development, if the list is empty, the API reflects
the requesting origin to keep the Vite dev server usable.

## 6. Rate limiting (FG37)

Authentication and pairing endpoints are rate limited per IP and per identifier
(`auth.login`, `devices.pair`). Until then, nginx `limit_req` is the outer guard
(see `deploy/nginx.conf`). FG4 audits every failed login (`auth.login_failed`) and
every refused refresh (`auth.refresh_rejected`) so brute force is visible in
`audit_logs` until the limiter exists.
