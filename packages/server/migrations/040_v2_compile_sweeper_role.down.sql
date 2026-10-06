-- Revert 040: v2_compile_sweeper_role
--
-- Restores 031 exactly: the policy for every role, EXECUTE on
-- v2.dirty_targets for PUBLIC (the default) and memax_v2, and 031's
-- comment.

DROP POLICY targets_dirty_sweep ON v2.targets;
CREATE POLICY targets_dirty_sweep ON v2.targets FOR SELECT
    USING (current_setting('app.sweep', true) = 'dirty_targets'
           AND dirty_gen > compiled_gen AND sync_state <> 'off');

REVOKE ALL ON FUNCTION v2.dirty_targets(integer) FROM memax_v2_compile_sweeper;
GRANT EXECUTE ON FUNCTION v2.dirty_targets(integer) TO PUBLIC;
GRANT EXECUTE ON FUNCTION v2.dirty_targets(integer) TO memax_v2;

COMMENT ON FUNCTION v2.dirty_targets(integer) IS
    'The compile sweeper''s cross-space read: ids of targets whose dirty_gen is ahead of compiled_gen.';

REVOKE ALL ON v2.targets FROM memax_v2_compile_sweeper;
REVOKE USAGE ON SCHEMA v2 FROM memax_v2_compile_sweeper;

-- The role is cluster-wide: other databases (test clones, another
-- environment) may still hold grants to it, and then it stays.
DO $$
BEGIN
    DROP ROLE IF EXISTS memax_v2_compile_sweeper;
EXCEPTION WHEN dependent_objects_still_exist OR insufficient_privilege OR object_in_use THEN
    RAISE NOTICE 'role memax_v2_compile_sweeper kept: %', SQLERRM;
END $$;
