-- =============================================================================
-- 0004_create_audit_logs
-- =============================================================================
-- audit_logs — append-only skeleton for privileged actions (BLUEPRINT §11.10).
--
-- Skeleton scope of FG2: the table, its constraints and its indexes exist, and
-- backend/internal/audit can write rows. Nothing writes to it yet: every
-- privileged action (auth, store, device, revoke) starts recording from the
-- feature group that introduces the action.
--
-- Design notes:
--   * tenant_id / store_id are nullable and use ON DELETE SET NULL: an audit
--     row must survive the deletion of the tenant or store it refers to.
--   * No updated_at and no trigger: audit rows are immutable by convention.
--     Retention jobs (FG34) delete, they never rewrite.
--   * actor_id is the user id, device id or NULL for system actions; it is the
--     *actor* identity, never a store identity.
-- =============================================================================

CREATE TABLE IF NOT EXISTS audit_logs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid        REFERENCES tenants(id) ON DELETE SET NULL,
    store_id    uuid        REFERENCES stores(id) ON DELETE SET NULL,
    actor_type  text        NOT NULL,
    actor_id    uuid,
    action      text        NOT NULL,
    entity_type text,
    entity_id   uuid,
    metadata    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip          inet,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT audit_logs_actor_type_valid
        CHECK (actor_type IN ('user', 'device', 'system')),
    -- Dotted snake_case machine actions, e.g. store.created, device.revoked.
    CONSTRAINT audit_logs_action_format
        CHECK (action ~ '^[a-z0-9_]+(\.[a-z0-9_]+)*$'),
    CONSTRAINT audit_logs_action_length
        CHECK (char_length(action) BETWEEN 3 AND 120),
    CONSTRAINT audit_logs_entity_type_not_blank
        CHECK (entity_type IS NULL OR btrim(entity_type) <> ''),
    CONSTRAINT audit_logs_metadata_is_object
        CHECK (jsonb_typeof(metadata) = 'object')
);

-- Store / tenant audit views (admin screens) read newest first.
CREATE INDEX IF NOT EXISTS audit_logs_store_created_idx
    ON audit_logs (store_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_tenant_created_idx
    ON audit_logs (tenant_id, created_at DESC);
-- Investigating one action type across the platform (security review, FG37).
CREATE INDEX IF NOT EXISTS audit_logs_action_created_idx
    ON audit_logs (action, created_at DESC);

COMMENT ON TABLE audit_logs IS
    'Append-only audit trail for privileged actions; rows are never updated (retention deletes go through FG34 jobs).';
COMMENT ON COLUMN audit_logs.actor_type IS
    'user | device | system — a device is a first class actor with its own id, never a store identity.';
COMMENT ON COLUMN audit_logs.action IS
    'Dotted snake_case action, e.g. store.created, employee.status_changed, device.revoked.';
COMMENT ON COLUMN audit_logs.metadata IS
    'Free-form JSON object with action specific detail; never contains secrets, tokens or passwords.';
