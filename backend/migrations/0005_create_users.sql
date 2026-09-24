-- =============================================================================
-- 0005_create_users
-- =============================================================================
-- users — the human accounts of the platform (BLUEPRINT §5, docs/DATABASE.md §2).
--
-- Roles (BLUEPRINT §5):
--   * super_admin — platform scope: no tenant, no store. It may manage tenants,
--     stores and devices across the platform.
--   * store_admin — tenant scope (and, from FG5 onwards, optionally bound to one
--     store): employees, media, display settings, devices, pairing.
--
-- Design notes:
--   * The tenant scope is a *column*, never a claim a client can choose: the
--     authentication middleware re-reads it from this row on every request
--     (see backend/internal/auth), so editing a token cannot widen access.
--   * `tenant_id` is nullable because a super admin has no tenant; the CHECK
--     constraints below tie the nullable scope to the role so an inconsistent
--     row can never be stored.
--   * `email` is citext and globally unique (live rows): the login form is served
--     from the single platform origin and carries no tenant hint, so a duplicate
--     address across tenants would make a login ambiguous.
--   * `password_hash` is bcrypt (FG4) and the format CHECK makes storing a
--     plaintext or truncated value impossible at the database level.
-- =============================================================================

CREATE TABLE IF NOT EXISTS users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid        REFERENCES tenants(id) ON DELETE CASCADE,
    store_id      uuid        REFERENCES stores(id) ON DELETE SET NULL,
    email         citext      NOT NULL,
    display_name  text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL,
    status        text        NOT NULL DEFAULT 'active',
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    CONSTRAINT users_email_format
        CHECK (email::text ~ '^[^@[:space:]]+@[^@[:space:]]+[.][^@[:space:]]+$'),
    CONSTRAINT users_email_length
        CHECK (char_length(email::text) <= 254),
    CONSTRAINT users_display_name_not_blank
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 120),
    -- bcrypt: $2a$/$2b$/$2y$ + cost + 53 character digest. A plaintext password
    -- (or a hash produced by another, unintended algorithm) cannot be stored.
    CONSTRAINT users_password_hash_is_bcrypt
        CHECK (password_hash ~ '^[$]2[aby][$][0-9]{2}[$][./A-Za-z0-9]{53}$'),
    CONSTRAINT users_role_valid
        CHECK (role IN ('super_admin', 'store_admin')),
    CONSTRAINT users_status_valid
        CHECK (status IN ('active', 'disabled')),
    -- The scope of a role is not negotiable: a super admin owns no tenant row, a
    -- store admin always has one (MVP: one tenant = one store, BLUEPRINT §3).
    CONSTRAINT users_scope_matches_role
        CHECK (
            (role = 'super_admin' AND tenant_id IS NULL AND store_id IS NULL)
            OR (role = 'store_admin' AND tenant_id IS NOT NULL)
        ),
    CONSTRAINT users_store_requires_tenant
        CHECK (store_id IS NULL OR tenant_id IS NOT NULL)
);

-- Live rows own the address; a soft deleted account releases it for reuse.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key
    ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS users_tenant_id_idx
    ON users (tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS users_store_id_idx
    ON users (store_id) WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE users IS
    'Human accounts: platform super admins (no tenant) and store admins (exactly one tenant), BLUEPRINT §5.';
COMMENT ON COLUMN users.tenant_id IS
    'Tenant scope of the account; NULL only for a super admin. Re-read from this row on every request, never taken from a token claim.';
COMMENT ON COLUMN users.password_hash IS
    'bcrypt hash only (never plaintext); the CHECK constraint rejects any value that is not a bcrypt digest.';
COMMENT ON COLUMN users.role IS
    'super_admin | store_admin — the authoritative role; JWT claims only mirror it and the database wins.';
