-- 040: v2_compile_sweeper_role
--
-- The compile sweeper's cross-space read gets its own role, as the
-- sealer's does (039).
--
-- 031's targets_dirty_sweep policy admitted dirty targets of every space to
-- any role while app.sweep = 'dirty_targets', and any session can set a
-- custom setting, so memax_v2 (every request) could read other spaces'
-- dirty targets by setting it itself, and anyone could call
-- v2.dirty_targets (EXECUTE was PUBLIC's by default). Now:
--
--   * memax_v2_compile_sweeper (NOLOGIN, NOSUPERUSER, NOBYPASSRLS) runs
--     the sweep: Ledger.DirtyTargets switches to it for the one read, the
--     way the sealer switches to memax_v2_sealer;
--   * the policy applies to that role only, so memax_v2 setting app.sweep
--     sees nothing more than its scope;
--   * only that role may execute v2.dirty_targets, and it may read only the
--     targets columns the sweep needs (ids, the generation counters, the
--     sync state and the quiet-window clock), never settings or delivery.
--
-- A dedicated role rather than reusing memax_v2_sealer: the sealer reads
-- every receipt in a space and writes seals; the sweeper reads a few
-- columns of dirty targets. Sharing a role would give each the other's
-- reach, for no gain.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2_compile_sweeper') THEN
        BEGIN
            CREATE ROLE memax_v2_compile_sweeper NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2_compile_sweeper' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2_compile_sweeper must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    -- The migrating role must be able to SET ROLE to it. A role made by a
    -- non-superuser with CREATEROLE (Neon's owner) comes with ADMIN but not
    -- SET (Postgres 16), so a membership alone isn't enough: grant SET.
    IF NOT pg_has_role(current_user, 'memax_v2_compile_sweeper', 'SET') THEN
        BEGIN
            EXECUTE format('GRANT memax_v2_compile_sweeper TO %I WITH SET TRUE', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2_compile_sweeper;
-- The extensions (vector, pg_trgm, unaccent, pgcrypto) live in schema
-- public. Postgres lets PUBLIC use it by default, but a public schema owned
-- by another role (Neon's owner) lets nobody, so the role needs its own
-- USAGE: it resolves names (halfvec, similarity, unaccent), not tables.
GRANT USAGE ON SCHEMA public TO memax_v2_compile_sweeper;
GRANT SELECT (id, space_id, dirty_gen, compiled_gen, sync_state, dirty_at) ON v2.targets TO memax_v2_compile_sweeper;

REVOKE ALL ON FUNCTION v2.dirty_targets(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.dirty_targets(integer) FROM memax_v2;
GRANT EXECUTE ON FUNCTION v2.dirty_targets(integer) TO memax_v2_compile_sweeper;

DROP POLICY targets_dirty_sweep ON v2.targets;
-- Read-only, only inside v2.dirty_targets, and only for the sweeper.
CREATE POLICY targets_dirty_sweep ON v2.targets FOR SELECT TO memax_v2_compile_sweeper
    USING (current_setting('app.sweep', true) = 'dirty_targets'
           AND dirty_gen > compiled_gen AND sync_state <> 'off');

COMMENT ON FUNCTION v2.dirty_targets(integer) IS
    'The compile sweeper''s cross-space read: ids of targets whose dirty_gen is ahead of compiled_gen. memax_v2_compile_sweeper only.';
