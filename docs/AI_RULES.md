# AI_RULES.md — working rules for AI agents in this repository

These rules are binding for every automated change in this repository.
They combine BLUEPRINT §24 with the concrete conventions adopted in FG1.

## 1. Source of truth

1. Read `docs/BLUEPRINT.md` before any work. It wins over assumptions, tutorials
   and "best practice" opinions.
2. If code and blueprint disagree, fix one of them **in the same commit** and
   record the decision in the blueprint change log (§26).
3. `BLUEPRINT.md` at repository root is the archived original v1.2 input — do not
   edit it (except to fix a factual pointer).

## 2. Architecture invariants (never break these)

1. **Single domain + path routing**: `/`, `/app`, `/s/{store-slug}`, `/setup`, `/api`, `/ws`.
   No per-store folders, sites, servers or databases. No per-store physical assets.
2. **Multi-tenant from day one**: every business table and query is scoped by
   `tenant_id` and/or `store_id`. A store must never be able to read or write
   another store's data.
3. **Devices** are first-class actors: `device_id` + hashed device token, always
   bound to exactly one store. Store-admin credentials are never stored on a tablet.
4. **No ESP32, sensors, cameras-as-hardware, or other external hardware integration.**
5. Realtime is push-based (WebSocket). Tablets must not poll aggressively.
6. Display must keep working from cache when the network drops (FG27–FG30).

## 3. Stack conventions (match existing code, do not swap frameworks)

| Layer | Convention |
|---|---|
| Backend | Go + Gin, GORM (PostgreSQL), zap logger, viper+godotenv config, testify tests |
| Backend layout | `cmd/<binary>` + `internal/<concern>`; one concern per package |
| Configuration | `internal/config`: shared base + `APP_ENV` profile, then env, then `*_FILE` secrets; tiers `relaxed`/`hardened`/`strict` (`docs/DEPLOYMENT.md` §1) |
| Responses | JSON envelope: success `{"data": …}`, failure `{"error": {"code","message","details"}}` |
| Errors | `snake_case` machine codes (e.g. `not_found`, `validation_failed`) + human message |
| IDs | UUID (`github.com/google/uuid`) |
| Timestamps | `timestamptz` in DB, RFC3339/ISO-8601 in JSON |
| Frontend | React + Vite + TypeScript (strict), Tailwind v4, React Router, vitest |
| Frontend layout | `src/app`, `src/components`, `src/features`, `src/lib`, `src/pages`, `src/services`, `src/db`, `src/pwa` |
| Frontend calls | Only through `src/services/*` (never `fetch` inline in components) |
| Styling | Tailwind utility classes; shared primitives in `src/components/ui` |
| Language | Code, comments, commits and docs in English; Thai only in user-facing store content |

## 4. Working protocol

1. One Feature Group at a time, in roadmap order (BLUEPRINT §25).
2. Before coding: inspect, then write/extend tests, then implement.
3. After coding: `go build ./... && go vet ./... && go test ./...` for the backend,
   `npm run typecheck && npm run lint && npm run test && npm run build` for the frontend.
4. Run migrations against the local database and verify with `psql` / `migrate status`.
5. Review `git diff`, stage only files in scope, commit with
   `<type>(<scope>): <summary>` (`feat|fix|test|docs|chore|refactor`).
6. Stop at the documented review point and report: changed files, test results,
   remaining issues, next FG proposal.

## 5. Security rules

1. Every API route validates authorization; tenant scope comes from the token,
   never from a client-supplied `tenant_id`/`store_id` alone.
2. Passwords are hashed (bcrypt/argon2); tokens are stored hashed.
3. Device credentials are revocable and re-issuable.
4. Uploads validate MIME type, size and (for images) decodability; file names are
   never user controlled (no path traversal).
5. `slug` is validated: `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`, lowercase, unique.
6. Authentication/pairing endpoints are rate limited.
7. Never log secrets, tokens, passwords or full request bodies of credentials.
8. No production domain is hard-coded — always configuration driven.
9. Secrets come from the environment or a `*_FILE` secret file — never from a YAML
   config file or the repository. Diagnostics use
   `config.Config.Redacted()`/`Summary()`; `staffdisplay-server --check` proves a
   configuration (and the tier rules) without starting the service.

## 6. Testing rules

1. Tenant isolation, device isolation and revocation must have executable tests
   (BLUEPRINT §26) as soon as the relevant feature exists.
2. Display code needs an offline/reconnect test.
3. Every new API endpoint needs at least one happy-path and one authorization/
   validation test.
4. Tests must be deterministic: no sleeps, no reliance on wall-clock ordering,
   no external network. Integration tests that need PostgreSQL are skipped unless
   `TEST_DATABASE_INTEGRATION=1`.

## 7. Documentation rules

Update in the same commit when behaviour changes:

* `docs/BLUEPRINT.md` — architecture / roadmap / change log
* `docs/API.md` — endpoints, request/response, errors
* `docs/DATABASE.md` — schema, indexes, constraints, tenant keys
* `docs/DEPLOYMENT.md` — env vars, nginx, TLS, backup, runbooks
* `README.md` — quick start and FG status table
