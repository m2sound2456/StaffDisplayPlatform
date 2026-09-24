-- =============================================================================
-- 0003_create_stores
-- =============================================================================
-- stores — one physical shop / branch of a tenant (BLUEPRINT §3, §4).
--
-- A store is a plain database record. Its *public* identity is the slug, which
-- is the only thing the display URL needs: /s/{store-slug} on the single
-- platform domain. Device identity is deliberately independent of this: a
-- device has its own id + hashed token (FG16–FG21) and never derives its
-- identity from the store URL.
--
-- Isolation root: tenant_id FKs tenants(id) ON DELETE CASCADE, so removing a
-- tenant can never orphan store rows, and every store query is additionally
-- scoped by tenant_id in the repository layer (tenant writes use
-- "WHERE tenant_id = $scope", never a client supplied id alone).
-- =============================================================================

CREATE TABLE IF NOT EXISTS stores (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          text        NOT NULL,
    slug          citext      NOT NULL,
    logo_url      text,
    status        text        NOT NULL DEFAULT 'active',
    timezone      text        NOT NULL DEFAULT 'UTC',
    opening_hours jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    CONSTRAINT stores_name_not_blank
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    -- citext makes the regexp case-insensitive, so the pattern is applied to the
    -- text form: casting is what keeps a slug lowercase as well as well formed.
    CONSTRAINT stores_slug_format
        CHECK (slug::text ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    CONSTRAINT stores_slug_not_reserved
        CHECK (slug::text NOT IN ('app', 'setup', 'api', 'ws', 'healthz', 'readyz', 'assets', 'icons', 'static')),
    CONSTRAINT stores_status_valid
        CHECK (status IN ('active', 'inactive', 'archived')),
    -- IANA zone name; the domain layer mirrors this rule (backend/internal/store).
    CONSTRAINT stores_timezone_format
        CHECK (timezone ~ '^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$' AND char_length(timezone) BETWEEN 2 AND 64),
    CONSTRAINT stores_logo_url_not_blank
        CHECK (logo_url IS NULL OR (btrim(logo_url) <> '' AND char_length(logo_url) <= 512)),
    CONSTRAINT stores_opening_hours_is_object
        CHECK (jsonb_typeof(opening_hours) = 'object')
);

-- The public display route /s/{slug} resolves a store from the slug alone, so
-- the slug must be globally unique (BLUEPRINT §4) — never per store only.
-- The index is partial on live rows so a retired store releases its slug
-- without touching audit history; two deleted stores may share one slug.
CREATE UNIQUE INDEX IF NOT EXISTS stores_slug_active_key
    ON stores (slug)
    WHERE deleted_at IS NULL;

-- Tenant scoped listings (admin store list, §15) and status filters.
CREATE INDEX IF NOT EXISTS stores_tenant_id_idx
    ON stores (tenant_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS stores_tenant_status_idx
    ON stores (tenant_id, status)
    WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS trg_stores_updated_at ON stores;
CREATE TRIGGER trg_stores_updated_at
    BEFORE UPDATE ON stores
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE stores IS
    'Store (shop/branch) of exactly one tenant; addressed publicly as /s/{slug} on the single platform domain.';
COMMENT ON COLUMN stores.tenant_id IS
    'Isolation root. Every store query must be scoped by tenant_id in the repository layer.';
COMMENT ON COLUMN stores.slug IS
    'Public display key /s/{slug}: ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$, lowercase, globally unique among live stores, never a reserved platform path.';
COMMENT ON COLUMN stores.status IS
    'active | inactive | archived — only active stores are meant to serve displays (enforced from FG5 onwards).';
COMMENT ON COLUMN stores.opening_hours IS
    'JSON object keyed by weekday; shape is defined by the admin UI (FG5/FG15).';
COMMENT ON COLUMN stores.deleted_at IS
    'Soft delete: retired stores keep their rows (and audit trail) but disappear from every repository query.';
