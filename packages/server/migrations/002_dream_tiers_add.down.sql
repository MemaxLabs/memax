-- Revert 002: dream_tiers_add
--
-- Drop in reverse order of creation to satisfy FK + dependent-index
-- constraints. The DROP TABLE statements cascade indexes; we don't
-- need explicit DROP INDEX.

-- Drop the table-level CHECK constraints first. Postgres also
-- removes them implicitly on column drop (DROP COLUMN cascades),
-- but being explicit keeps the down migration intent clear and lets
-- partial reverts work if the column drops are commented out for
-- staged rollback.
ALTER TABLE dream_runs DROP CONSTRAINT IF EXISTS dream_runs_tool_loop_iterations_nonneg;
ALTER TABLE dream_runs DROP CONSTRAINT IF EXISTS dream_runs_terminal_state_check;
ALTER TABLE dream_runs DROP CONSTRAINT IF EXISTS dream_runs_tier_check;

ALTER TABLE dream_runs DROP COLUMN IF EXISTS terminal_state;
ALTER TABLE dream_runs DROP COLUMN IF EXISTS tool_loop_iterations;
ALTER TABLE dream_runs DROP COLUMN IF EXISTS tier;
ALTER TABLE dream_runs DROP COLUMN IF EXISTS triggered_by;

DROP TABLE IF EXISTS dream_quota_signals;
DROP TABLE IF EXISTS dream_run_completions;
DROP TABLE IF EXISTS hub_usage;

ALTER TABLE usage DROP COLUMN IF EXISTS lucid_dream_count;
ALTER TABLE usage DROP COLUMN IF EXISTS basic_dream_count;

ALTER TABLE plans DROP COLUMN IF EXISTS lucid_dream_limit;
ALTER TABLE plans DROP COLUMN IF EXISTS basic_dream_limit;
