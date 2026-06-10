-- Align an existing DB's schema_migrations after re-squashing
-- 001..002 into a single 001_baseline_v1.
--
-- The schema itself is already correct (every DB that ran 002 has
-- admin_audit + idx_river_job_* + idx_memories_*), so this script
-- only rewrites the golang-migrate version pointer.
--
-- Accepts starting state at version=1 (re-squash from the prior
-- baseline only) or version=2 (the most recent production state).
-- Idempotent — re-running after alignment is a no-op.
--
-- Usage (against staging or local):
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
--     -f staging_resquash_to_v1_round2.sql

BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '30s';

DO $$
DECLARE
  cur_version bigint;
  cur_dirty boolean;
BEGIN
  SELECT version, dirty INTO cur_version, cur_dirty FROM public.schema_migrations;
  IF cur_version IS NULL THEN
    RAISE EXCEPTION 'schema_migrations is empty — refusing to run against an uninitialized DB';
  END IF;
  IF cur_dirty THEN
    RAISE EXCEPTION 'schema_migrations.dirty=true at version % — investigate before realigning', cur_version;
  END IF;
  IF cur_version = 1 THEN
    RAISE NOTICE 'schema_migrations already at version 1 — no change';
  ELSIF cur_version NOT IN (1, 2) THEN
    RAISE EXCEPTION 'schema_migrations at version %, expected 1 or 2 — this alignment script targets the 001..002 → 001 squash only', cur_version;
  END IF;
END $$;

DELETE FROM public.schema_migrations;
INSERT INTO public.schema_migrations (version, dirty) VALUES (1, false);

COMMIT;

SELECT version, dirty FROM public.schema_migrations;
