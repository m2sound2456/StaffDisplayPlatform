-- =============================================================================
-- 0006_create_user_sessions
-- =============================================================================
-- user_sessions — the session behind a JWT access token (FG4, BLUEPRINT §6/§11).
--
-- Why a table when the access token is self contained:
--   * logout has to take effect immediately — a stateless token could not be
--     revoked before it expires;
--   * the refresh token is stored **hashed only** (sha256) and is rotated on
--     every refresh, so a leaked refresh token stops working after one use;
--   * the tenant/store scope is snapshotted from the user row at login and
--     re-read by the middleware, so a token claim can never widen it.
--
-- Design notes:
--   * no soft delete and no updated_at: sessions are hard deleted by cascade or
--     retention (FG34); revocation stamps `revoked_at` + `revoked_reason` and
--     keeps the row for the audit trail.
--   * `refresh_token_hash` is `^[0-9a-f]{64}$` (sha256 hex), so a plaintext
--     refresh token cannot be stored by accident.
--   * tenant_id / store_id are denormalised copies: a session row must be
--     filterable by tenant without joining users (admin session views, FG5+).
-- =============================================================================

CREATE TABLE IF NOT EXISTS user_sessions (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id          uuid        REFERENCES tenants(id) ON DELETE CASCADE,
    store_id           uuid        REFERENCES stores(id) ON DELETE SET NULL,
    refresh_token_hash text        NOT NULL,
    user_agent         text,
    ip                 inet,
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_used_at       timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    revoked_reason     text,

    CONSTRAINT user_sessions_refresh_token_hash_is_sha256
        CHECK (refresh_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT user_sessions_user_agent_length
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 400),
    CONSTRAINT user_sessions_expires_after_creation
        CHECK (expires_at > created_at),
    CONSTRAINT user_sessions_revoked_after_creation
        CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT user_sessions_revoked_reason_format
        CHECK (revoked_reason IS NULL OR revoked_reason IN ('logout', 'revoked')),
    -- A revocation is always explained: neither half may exist alone.
    CONSTRAINT user_sessions_revocation_is_atomic
        CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);

-- One row per issued refresh token: the lookup path of POST /auth/refresh.
CREATE UNIQUE INDEX IF NOT EXISTS user_sessions_refresh_token_hash_key
    ON user_sessions (refresh_token_hash);
CREATE INDEX IF NOT EXISTS user_sessions_user_created_idx
    ON user_sessions (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS user_sessions_tenant_created_idx
    ON user_sessions (tenant_id, created_at DESC);
-- Live sessions of a user (revoked rows stay for the audit trail).
CREATE INDEX IF NOT EXISTS user_sessions_live_user_idx
    ON user_sessions (user_id) WHERE revoked_at IS NULL;

COMMENT ON TABLE user_sessions IS
    'Login sessions: one row per issued refresh token, hashed; revocation (logout) takes effect on the next request.';
COMMENT ON COLUMN user_sessions.refresh_token_hash IS
    'sha256 hex of the opaque refresh token — the token itself is never stored (docs/AI_RULES.md §5.2).';
COMMENT ON COLUMN user_sessions.revoked_reason IS
    'logout | revoked — set together with revoked_at, never alone.';
