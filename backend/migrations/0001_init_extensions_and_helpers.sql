-- =============================================================================
-- 0001_init_extensions_and_helpers
-- =============================================================================
-- Foundation only (FG1): PostgreSQL extensions and shared trigger helpers used
-- by every tenant-scoped table created from FG2 onwards.
--
-- Idempotent by design so it can be re-run safely in development.
-- =============================================================================

-- gen_random_uuid() / digest() helpers. pgcrypto and citext are "trusted"
-- extensions on PostgreSQL 13+, so the application role can install them.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Case-insensitive text: used for user emails and store slugs.
CREATE EXTENSION IF NOT EXISTS "citext";

-- Keeps updated_at in sync on every UPDATE. Attach with:
--   CREATE TRIGGER trg_stores_updated_at BEFORE UPDATE ON stores
--     FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at() IS
    'Shared BEFORE UPDATE trigger function: refreshes updated_at on every tenant-scoped table.';
