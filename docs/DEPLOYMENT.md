# Deployment (FG31 — skeleton created in FG1)

> Status: **FG1 provides the reverse-proxy and configuration skeleton.**
> FG31–FG37 complete HTTPS automation, backup, logging, monitoring and the
> security review.

Target topology (BLUEPRINT §2 and §32):

```text
Internet → nginx (443, TLS) → ┬─ static SPA   frontend/dist  ( /, /app, /s/{slug}, /setup )
                              └─ proxy        127.0.0.1:8080 (/api, /ws, /healthz, /readyz)
                                                    ↓
                                               PostgreSQL 16
```

**Single domain, path-based tenants.** No per-store virtual host, folder, service
or database is ever created for a store.

---

## 1. Artefacts

| Path | Purpose |
|---|---|
| `deploy/nginx.conf` | Reverse proxy + SPA fallback + long-cache static assets |
| `deploy/env.production.example` | Production `.env` template for the Go API |
| `backend/configs/config.production.yaml` | Production defaults (strict validation is enforced by `APP_ENV=production`) |
| `backend/Makefile` | `build`, `build-server`, `build-migrate` with version ldflags |
| `backend/cmd/migrate` | `up` / `status` / `version` migration CLI |

---

## 2. Build the release

```bash
# backend
cd backend
VERSION=1.0.0 make build            # -> build/staffdisplay-server, build/staffdisplay-migrate

# frontend
cd ../frontend
npm ci
VITE_API_BASE_URL=/api/v1 npm run build   # -> frontend/dist
```

Deploy layout on the VPS:

```text
/opt/staffdisplay/
├── server/staffdisplay-server       # + .env (chmod 600)
├── server/staffdisplay-migrate
├── server/migrations/               # only needed with --dir; embedded FS is the default
└── web/                             # contents of frontend/dist
```

---

## 3. First-time provisioning

```bash
# 1. database (dedicated role, least privilege)
sudo -u postgres psql <<'SQL'
CREATE ROLE staffdisplay LOGIN PASSWORD 'replace-me';
CREATE DATABASE staffdisplay OWNER staffdisplay;
SQL

# 2. configuration
install -d -m 750 /opt/staffdisplay/server
install -m 600 deploy/env.production.example /opt/staffdisplay/server/.env
$EDITOR /opt/staffdisplay/server/.env      # DATABASE_*, AUTH_JWT_SECRET, CORS_ALLOWED_ORIGINS, APP_ENV=production

# 3. schema
cd /opt/staffdisplay/server && ./staffdisplay-migrate up && ./staffdisplay-migrate status

# 4. service (systemd unit sketch)
cat >/etc/systemd/system/staffdisplay.service <<'UNIT'
[Unit]
Description=Staff Display Platform API
After=network-online.target postgresql.service

[Service]
User=staffdisplay
WorkingDirectory=/opt/staffdisplay/server
EnvironmentFile=/opt/staffdisplay/server/.env
ExecStart=/opt/staffdisplay/server/staffdisplay-server
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=full
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
systemctl enable --now staffdisplay
```

---

## 4. HTTPS (FG32)

```bash
apt install certbot python3-certbot-nginx
certbot --nginx -d display.example.com          # single certificate covers every store path
```

Renewal is automatic; `nginx -t && systemctl reload nginx` runs from the
certbot hook. Store URLs (`https://display.example.com/s/{slug}`) share this
certificate — do **not** issue per-store certificates (that would break the
single-domain tenancy model).

DNS: one `A`/`AAAA` record for the platform domain. Wildcard DNS/certificates
(FG33) are only interesting for *optional* future subdomain aliases; the
application must keep working without them.

---

## 5. Hardening checklist

1. `APP_ENV=production` → rejects wildcard CORS, `sslmode=disable`, weak/absent secrets.
2. PostgreSQL reachable only from the app host (`pg_hba.conf`, `listen_addresses`).
3. Rate limiting enabled in nginx (`/api/v1/auth`, pairing endpoints) — FG37.
4. Security headers are sent by the Go API (`internal/server/middleware.go`); nginx
   repeats `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`.
5. Uploads (FG7) land in `backend/storage/` → move to object storage later via the
   `MediaStorage` abstraction; never store binaries in PostgreSQL.
6. Logs: JSON to stdout, collected by journald/vector; audit log table for
   privileged actions (FG5+).
7. Backups (FG34):
   ```bash
   pg_dump --format=custom --file=/var/backups/staffdisplay-$(date +%F).dump staffdisplay
   # retain 14 daily + 8 weekly, verify with pg_restore --list
   ```

---

## 6. Verification after deploy

```bash
curl -fsS https://display.example.com/healthz
curl -fsS https://display.example.com/readyz          # 503 until DB reachable
curl -fsS https://display.example.com/api/v1/version
curl -fsSI https://display.example.com/s/demo         # 200 text/html (SPA fallback)
```

Tablet acceptance (FG10–FG21): pair a tablet through `/setup`, confirm it shows
only its own store, then verify revocation and offline cache behaviour.
