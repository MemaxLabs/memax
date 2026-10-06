-- 026: v2_schema_and_role
--
-- V2 lives in its own Postgres schema, `v2`, next to V1's `public`
-- (plan 25 §5.4). V1 keeps running on `public` untouched until
-- cutover.
--
-- # The memax_v2 role
--
-- Every V2 transaction runs `SET LOCAL ROLE memax_v2` (internal/ledger
-- does this) before it touches a `v2` table. The reason: managed
-- Postgres login roles (Neon's project owner, for example) can carry
-- BYPASSRLS, and a superuser always bypasses row-level security.
-- memax_v2 never does, so the RLS policies on `v2` hold whichever role
-- the app logged in as.
--
-- memax_v2 is NOLOGIN: nothing connects as it directly. The migrating
-- role is granted membership so it can switch to it. If the app ever
-- connects as a different role than the one running migrations, grant
-- that role membership too: `GRANT memax_v2 TO <app_role>;`.
--
-- Roles are cluster-wide, so this block is idempotent and tolerates a
-- concurrent migration on another database in the same cluster (the
-- test harness migrates several template databases at once). Creating
-- the role needs CREATEROLE, which Neon's owner role has.
--
-- # Scope settings
--
-- RLS policies read the transaction's scope from two custom settings
-- that the ledger sets with `set_config(..., true)` (transaction-local):
--   app.space_ids  — uuid[] literal of the spaces the actor may touch
--   app.tenant_ids — uuid[] literal of those spaces' tenants
-- v2.current_space_ids() / v2.current_tenant_ids() turn a missing or
-- empty setting into an empty array, so "no scope" means "no rows",
-- never an error and never everything.

CREATE SCHEMA v2;
COMMENT ON SCHEMA v2 IS 'Memax V2 record: receipts, memories and their projections. Written only through internal/ledger.';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2') THEN
        BEGIN
            CREATE ROLE memax_v2 NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2 must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM pg_auth_members m
          JOIN pg_roles r ON r.oid = m.roleid
          JOIN pg_roles u ON u.oid = m.member
         WHERE r.rolname = 'memax_v2' AND u.rolname = current_user
    ) THEN
        BEGIN
            EXECUTE format('GRANT memax_v2 TO %I', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2;

CREATE FUNCTION v2.current_space_ids() RETURNS uuid[]
    LANGUAGE sql STABLE
    AS $$ SELECT COALESCE(NULLIF(current_setting('app.space_ids', true), ''), '{}')::uuid[] $$;

CREATE FUNCTION v2.current_tenant_ids() RETURNS uuid[]
    LANGUAGE sql STABLE
    AS $$ SELECT COALESCE(NULLIF(current_setting('app.tenant_ids', true), ''), '{}')::uuid[] $$;

COMMENT ON FUNCTION v2.current_space_ids() IS 'Spaces in the current transaction scope (app.space_ids); empty when unset.';
COMMENT ON FUNCTION v2.current_tenant_ids() IS 'Tenants in the current transaction scope (app.tenant_ids); empty when unset.';
