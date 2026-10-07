-- Revert 026: v2_schema_and_role
--
-- 027 and 028 have already removed everything else in schema v2, so
-- the plain DROP SCHEMA (no CASCADE) also proves the schema is empty.
--
-- memax_v2 is cluster-wide: other databases in the same cluster (test
-- clones, another environment) may still hold grants to it, in which
-- case DROP ROLE fails. The role then stays, with no privileges in
-- this database, and the migration still succeeds.
DROP FUNCTION IF EXISTS v2.current_tenant_ids();
DROP FUNCTION IF EXISTS v2.current_space_ids();
REVOKE USAGE ON SCHEMA v2 FROM memax_v2;
REVOKE USAGE ON SCHEMA public FROM memax_v2;
DROP SCHEMA v2;

DO $$
BEGIN
    DROP ROLE IF EXISTS memax_v2;
EXCEPTION WHEN dependent_objects_still_exist OR insufficient_privilege OR object_in_use THEN
    RAISE NOTICE 'role memax_v2 kept: %', SQLERRM;
END $$;
