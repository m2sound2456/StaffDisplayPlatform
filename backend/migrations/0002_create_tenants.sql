-- =============================================================================
-- 0002_create_tenants
-- =============================================================================
-- tenants — the platform account boundary (BLUEPRINT §3, docs/DATABASE.md §2).
-- MVP: one tenant owns exactly one store, but the column exists from day one so
-- several stores per tenant never need a schema rewrite.
--
-- Conventions introduced here and reused by every later business table:
--   * status columns are text + a named CHECK constraint (documented values, no
--     ALTER TYPE lock when a value is added later);
--   * timestamps are timestamptz, updated_at is maintained by set_updated_at()
--     created in 0001_init_extensions_and_helpers;
--   * slug columns are citext (case-insensitive lookups) and are validated in
--     the database as well as in the domain layer (backend/internal/store).
--
-- Idempotent by design so it can be re-run safely in development.
-- =============================================================================

CREATE TABLE IF NOT EXISTS tenants (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    slug       citext      NOT NULL,
    status     text        NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tenants_name_not_blank
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    -- citext makes the regexp case-insensitive, so the pattern is applied to the
    -- text form: casting is what keeps a slug lowercase as well as well formed.
    CONSTRAINT tenants_slug_format
        CHECK (slug::text ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    CONSTRAINT tenants_slug_not_reserved
        CHECK (slug::text NOT IN ('app', 'setup', 'api', 'ws', 'healthz', 'readyz', 'assets', 'icons', 'static')),
    CONSTRAINT tenants_status_valid
        CHECK (status IN ('active', 'suspended', 'archived'))
);

-- Tenant slugs are unique platform wide because they may later become optional
-- subdomain aliases (BLUEPRINT §4); citext keeps the index case-insensitive.
CREATE UNIQUE INDEX IF NOT EXISTS tenants_slug_key ON tenants (slug);

DROP TRIGGER IF EXISTS trg_tenants_updated_at ON tenants;
CREATE TRIGGER trg_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE tenants IS
    'Platform account boundary; every store belongs to exactly one tenant (BLUEPRINT §3).';
COMMENT ON COLUMN tenants.slug IS
    'Platform slug: ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$, lowercase, unique, never a reserved platform path.';
COMMENT ON COLUMN tenants.status IS
    'active | suspended | archived — suspended tenants keep their data but must not serve displays.';
