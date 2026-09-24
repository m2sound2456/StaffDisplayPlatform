# Deployment (configuration hardened in FG3 · runbooks completed in FG31–FG37)

> Status: **FG1 provides the reverse-proxy and configuration skeleton; FG3 adds
> the environment profiles, secret handling and the stricter production
> validation.** FG31–FG37 complete HTTPS automation, backup, logging, monitoring
> and the security review.

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

## 1. Environments, profiles and precedence

One binary, three profiles. `APP_ENV` selects the profile, so the same build is
promoted from staging to production.

| Precedence | Source | Notes |
|---|---|---|
| 1 (lowest) | built-in defaults (`config.Default()`) | what a contributor gets with no file at all |
| 2 | `backend/configs/config.yaml` | shared base: timeouts, pool sizes, store defaults |
| 3 | `backend/configs/config.<APP_ENV>.yaml` | environment profile (host, log format, origins) |
| 4 | environment variables / systemd `EnvironmentFile` | `.env` in development only |
| 5 (highest) | `*_FILE` secret files | `AUTH_JWT_SECRET_FILE`, `DATABASE_PASSWORD_FILE`, … |

| `APP_ENV` | Tier | Profile file | Purpose |
|---|---|---|---|
| `development` (default) | `relaxed` | `config.development.yaml` | local machine, Vite dev server, local PostgreSQL |
| `staging` | `hardened` | `config.staging.yaml` | pre-production acceptance on a staging host |
| `production` | `strict` | `config.production.yaml` | live single-domain deployment behind nginx |

An unknown `APP_ENV` (`prod`, `Production2`, …) is a **startup error** — never a
silent fallback to development defaults. `CONFIG_PATH` replaces the base *and*
the profile with one explicit file (a missing file stays a startup error), which
is how a single promoted file can be shipped.

### Validation matrix

Rows are the checks in `internal/config/validate.go` and `validate_tiers.go`; `✓`
means the check applies. Every tier additionally enforces the generic rules:
required values, port/duration ranges, IANA timezone names, the `sslmode` enum,
absolute (bare) CORS origins, access token TTL < refresh token TTL, valid store
status and a store slug policy that stays inside the database rules.

| Rule | development | staging | production |
|---|---|---|---|
| required values present (`app.name`, `app.timezone`, `database.user`, `database.name`, …) | ✓ | ✓ | ✓ |
| `AUTH_JWT_SECRET` set | ✓ | ✓ | ✓ |
| `AUTH_JWT_SECRET` not the dev placeholder, not weak, ≥ 32 chars | – | ✓ | ✓ |
| `DATABASE_PASSWORD` set, ≥ 12 chars, not weak/placeholder | – | ✓ | ✓ |
| secrets come from the environment or a `*_FILE` file (never from YAML) | ✓ | ✓ | ✓ |
| rotated keys (`AUTH_JWT_PREVIOUS_SECRETS`) strong, distinct, at most 2 | – | ✓ | ✓ |
| `CORS_ALLOWED_ORIGINS` non-empty and without `*` | – | ✓ | ✓ |
| CORS origins must use `https://` (localhost exempt) | – | – | ✓ |
| `database.sslmode` ≠ `disable` | – | – | ✓ |
| a non-loopback `database.host` needs `verify-ca`/`verify-full` | – | – | ✓ |
| `logging.development = false` | – | – | ✓ |
| `logging.encoding = json` (journald/vector collection) | – | – | ✓ |
| TLS cert + key present when `server.tls.enabled` | ✓ | ✓ | ✓ |

Covered by `internal/config/*_test.go`; the shipped profiles themselves are loaded
and validated for every environment by
`TestShippedProfilesValidateForEveryEnvironment`.

### Store default overrides

`store.*` is the only per-deployment policy block (consumed by the store service
from FG5). It can only **tighten** the database rules of `docs/DATABASE.md` §2:
the slug pattern, the reserved platform paths and the 63 character ceiling come
from the migration set and cannot be relaxed from configuration.

| Key | Default (shared base) | production profile |
|---|---|---|
| `store.default_timezone` | `Asia/Bangkok` | inherited |
| `store.default_status` | `active` | inherited |
| `store.slug.min_length` | `1` | `3` |
| `store.slug.max_length` | `63` | inherited |
| `store.slug.auto_generate` | `true` | inherited |
| `store.slug.extra_reserved_slugs` | `[]` | `admin`, `portal` |

Environment overrides: `STORE_DEFAULT_TIMEZONE`, `STORE_DEFAULT_STATUS`,
`STORE_SLUG_MIN_LENGTH`, `STORE_SLUG_MAX_LENGTH`, `STORE_SLUG_AUTO_GENERATE`,
`STORE_SLUG_EXTRA_RESERVED_SLUGS`.

---

## 2. Secrets: storage, generation and rotation

**Rules (enforced in code, not only by convention)**

1. Secrets never live in a YAML file: a config file containing
   `auth.jwt_secret`, `auth.previous_secrets` or `database.password` is rejected
   at startup (`mergeYAML` → `rejectSecretsInConfig`).
2. Secrets never live in the repository: `.gitignore` ignores `.env` and every
   `.env.*` except the tracked frontend defaults — and no secret may be placed
   there, because only `VITE_*` values reach the browser bundle.
3. Secrets are never logged: `config.Config.Redacted()`/`Summary()` mask the
   signing key(s) and the database password, and
   `database.Options.RedactedDSN()` masks the password inside the connection
   string.

**Sources, best first**

| Source | Usage | Notes |
|---|---|---|
| `AUTH_JWT_SECRET_FILE`, `DATABASE_PASSWORD_FILE`, `AUTH_JWT_PREVIOUS_SECRETS_FILE` | staging, production | systemd `LoadCredential=`, a Docker/Kubernetes secret or a `chmod 600` file. The value never appears in `systemctl show`, `ps e` or `/proc/<pid>/environ`. |
| environment / `EnvironmentFile` | all environments | `install -m 600`, owned by the service user, `NoNewPrivileges=true` |
| `backend/.env` | development only | git-ignored, loaded by godotenv; never deploy it |

Setting a variable *and* its `_FILE` variant is a startup error: a half finished
rotation must not be resolved silently.

**Required values**

| Variable | development | staging / production | Generate with |
|---|---|---|---|
| `AUTH_JWT_SECRET` | placeholder `dev-secret-change-me` | required, ≥ 32 chars, no weak/placeholder value | `openssl rand -base64 48` |
| `DATABASE_PASSWORD` | local PostgreSQL password | required, ≥ 12 chars, no weak/placeholder value | `openssl rand -base64 24` |

The JWT secret is the root credential of the deployment: FG4 signs admin access
tokens with it and verifies them against it (plus the rotation window). Secret
files live in `/etc/staffdisplay/` with `chmod 600` and
`chown staffdisplay:staffdisplay`.

**First administrator (FG4, until FG5 adds user management)**

FG4 has no provisioning endpoint, so the first super admin is created with SQL.
`pgcrypto` (migration 0001) provides `crypt()`/`gen_salt()`, which produce the
bcrypt digest the `users.password_hash` CHECK requires:

```bash
psql -U staffdisplay -h 127.0.0.1 -d staffdisplay <<'SQL'
INSERT INTO users (email, display_name, password_hash, role)
VALUES ('admin@example.com', 'Platform admin',
        crypt('<strong password>', gen_salt('bf', 12)), 'super_admin');
SQL
```

A store admin carries a tenant (and optionally a store) instead: pass
`tenant_id`/`store_id` and the role `store_admin` — the schema rejects an
inconsistent role/scope combination. Then verify the deployment:

```bash
curl -fsS -X POST https://display.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"<strong password>"}'   # tokens (never logged)
curl -fsS https://display.example.com/api/v1/auth/me -H "Authorization: Bearer <access_token>"
```

**Rotation**

*Signing key — zero downtime, overlapping validity*

1. Generate the new key:
   `openssl rand -base64 48 > /etc/staffdisplay/jwt.secret.next`.
2. Keep the current key as a rotation value while the new one takes over:
   `AUTH_JWT_PREVIOUS_SECRETS_FILE` (one value per line) or
   `AUTH_JWT_PREVIOUS_SECRETS=<previous>` (comma separated). At most **two**
   previous values are accepted; each must be as strong as the current key and
   must differ from it.
3. Verify and restart:
   `staffdisplay-server --check && systemctl restart staffdisplay`. FG4 keeps
   *verifying* tokens with a previous key but never *signs* with one, so access
   tokens issued before the rotation stay valid until they expire
   (`AUTH_ACCESS_TOKEN_TTL`, default 15 minutes — the refresh token is opaque, so
   it is unaffected by the signing key).
4. Once every client has re-authenticated (15 minutes plus clock skew is enough),
   drop the previous value and restart — the rotation is complete.

*Database password*

1. `ALTER ROLE staffdisplay PASSWORD '<new>';` (run as `postgres`).
2. Write the new value into the secret file / `EnvironmentFile` (`chmod 600`) —
   `staffdisplay-migrate` uses the same credentials.
3. `staffdisplay-server --check && systemctl restart staffdisplay`.
4. Verify `/readyz` plus one authenticated API call.

*Compromise or leaked value*

Rotate **without** a rotation window: do not put the leaked key into
`AUTH_JWT_PREVIOUS_SECRETS`, because that would keep accepting tokens signed with
it. Rotate the database role password, revoke the affected device tokens (FG21)
and review `audit_logs` (written from FG4 onwards).

---

## 3. Artefacts

| Path | Purpose |
|---|---|
| `deploy/nginx.conf` | Reverse proxy + SPA fallback + long-cache static assets |
| `deploy/env.production.example` | Production `.env` template for the Go API |
| `deploy/env.staging.example` | Staging `.env` template (same secret rules) |
| `backend/configs/config.yaml` | Shared base profile |
| `backend/configs/config.development.yaml` · `config.staging.yaml` · `config.production.yaml` | Environment profiles |
| `backend/Makefile` | `build`, `build-server`, `build-migrate` with version ldflags |
| `backend/cmd/migrate` | `up` / `status` / `version` migration CLI |
| `backend/cmd/server --check` | validate the effective configuration, print a redacted summary, exit |

---

## 4. Build the release

```bash
# backend
cd backend
VERSION=1.0.0 make build            # -> build/staffdisplay-server, build/staffdisplay-migrate

# frontend — one profile per environment (FG3)
cd ../frontend
npm ci
npm run build                       # .env.production -> frontend/dist
npm run build:staging               # .env.staging  -> frontend/dist (staging host)
```

Both frontend builds refuse an absolute `VITE_API_BASE_URL`
(`src/lib/envProfile.ts`): the SPA and the API share the platform origin.

Deploy layout on the VPS:

```text
/opt/staffdisplay/
├── server/staffdisplay-server       # + .env (chmod 600) or secret files (chmod 600)
├── server/staffdisplay-migrate
├── server/configs/                  # config.yaml + config.production.yaml (no secrets!)
├── server/migrations/               # only needed with --dir; embedded FS is the default
└── web/                             # contents of frontend/dist
```

Run the binaries from `/opt/staffdisplay/server` so `configs/` is found (or point
`CONFIG_PATH` at an absolute path).

---

## 5. First-time provisioning

```bash
# 1. database (dedicated role, least privilege)
sudo -u postgres psql <<'SQL'
CREATE ROLE staffdisplay LOGIN PASSWORD 'replace-me';
CREATE DATABASE staffdisplay OWNER staffdisplay;
SQL

# 2. non-secret configuration
install -d -m 750 /opt/staffdisplay/server
install -m 600 deploy/env.production.example /opt/staffdisplay/server/.env
$EDITOR /opt/staffdisplay/server/.env      # host/port/sslmode/CORS/APP_ENV=production

# 3. secrets (loaded by systemd, never committed)
install -d -m 700 /etc/staffdisplay
openssl rand -base64 48 | install -m 600 /dev/stdin /etc/staffdisplay/jwt.secret
openssl rand -base64 24 | install -m 600 /dev/stdin /etc/staffdisplay/database.secret
chown -R staffdisplay:staffdisplay /etc/staffdisplay

# 4. schema (uses the same credentials)
cd /opt/staffdisplay/server && ./staffdisplay-migrate up && ./staffdisplay-migrate status

# 5. verify the configuration before touching systemd
APP_ENV=production \
  AUTH_JWT_SECRET_FILE=/etc/staffdisplay/jwt.secret \
  DATABASE_PASSWORD_FILE=/etc/staffdisplay/database.secret \
  ./staffdisplay-server --check        # exit 0 + redacted summary; non-zero lists every problem

# 6. service (systemd unit sketch)
cat >/etc/systemd/system/staffdisplay.service <<'UNIT'
[Unit]
Description=Staff Display Platform API
After=network-online.target postgresql.service

[Service]
User=staffdisplay
WorkingDirectory=/opt/staffdisplay/server
EnvironmentFile=/opt/staffdisplay/server/.env
# Secrets are passed as files, so they never appear in `systemctl show`.
LoadCredential=auth-jwt-secret:/etc/staffdisplay/jwt.secret
LoadCredential=database-password:/etc/staffdisplay/database.secret
Environment=AUTH_JWT_SECRET_FILE=%d/auth-jwt-secret
Environment=DATABASE_PASSWORD_FILE=%d/database-password
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

`%d` is systemd's `$CREDENTIALS_DIRECTORY`; the `_FILE` variables make the binary
read the credential from that directory instead of from the environment.

---

## 6. HTTPS (FG32)

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

## 7. Hardening checklist

1. `APP_ENV=production` → strict tier: rejects wildcard CORS, `http://` origins,
   `sslmode=disable`, an unverified remote database, console/dev logging, missing
   or weak secrets, and secrets stored in YAML (see the §1 matrix).
2. Secrets come from `*_FILE` files (`chmod 600`, owned by the service user) via
   systemd `LoadCredential=`; `--check` proves the effective configuration before
   a restart (§2).
3. PostgreSQL reachable only from the app host (`pg_hba.conf`, `listen_addresses`)
   and only over TLS (`sslmode=require` or stricter).
4. Rotate the signing key and the database password on schedule and on staff or
   credential changes; keep at most one rotation generation
   (`AUTH_JWT_PREVIOUS_SECRETS`, §2).
5. Rate limiting enabled in nginx (`/api/v1/auth`, pairing endpoints) — FG37.
6. Security headers are sent by the Go API (`internal/server/middleware.go`); nginx
   repeats `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`.
7. Uploads (FG7) land in `backend/storage/` → move to object storage later via the
   `MediaStorage` abstraction; never store binaries in PostgreSQL.
8. Logs: JSON to stdout, collected by journald/vector; the `audit_logs` table
   records privileged actions from FG4 onwards (`auth.login_succeeded`,
   `auth.login_failed`, `auth.logout`, `auth.token_refreshed`,
   `auth.refresh_rejected`) and never contains a password, token or signing key.
9. Backups (FG34):
   ```bash
   pg_dump --format=custom --file=/var/backups/staffdisplay-$(date +%F).dump staffdisplay
   # retain 14 daily + 8 weekly, verify with pg_restore --list
   ```

---

## 8. Verification after deploy

```bash
# configuration gate (no listener, no database connection involved)
cd /opt/staffdisplay/server
APP_ENV=production \
  AUTH_JWT_SECRET_FILE=/etc/staffdisplay/jwt.secret \
  DATABASE_PASSWORD_FILE=/etc/staffdisplay/database.secret \
  ./staffdisplay-server --check

# runtime
curl -fsS https://display.example.com/healthz
curl -fsS https://display.example.com/readyz          # 503 until DB reachable
curl -fsS https://display.example.com/api/v1/version
curl -fsSI https://display.example.com/s/demo         # 200 text/html (SPA fallback)
curl -sS -o /dev/null -w '%{http_code}\n' https://display.example.com/api/v1/auth/me   # 401 without credentials
```

`--check` prints the environment, tier, merged config files, CORS origins and
store defaults with every secret masked (`***redacted***`). Add it to the deploy
script or the CI pipeline: a configuration that would refuse to start now fails
before the service is restarted.

Tablet acceptance (FG10–FG21): pair a tablet through `/setup`, confirm it shows
only its own store, then verify revocation and offline cache behaviour.
