-- Align staging's schema_migrations after re-squashing 001..019 into a
-- single 001_baseline_v1. The schema itself is already correct (staging
-- has every object from 001..019 already applied), so this only rewrites
-- the golang-migrate version pointer.
--
-- Before running:
--   SELECT version, dirty FROM public.schema_migrations;  -- expect (19, false)
-- After running:
--   SELECT version, dirty FROM public.schema_migrations;  -- expect (1, false)
--
-- Run ONLY while the new baseline build is about to deploy (or immediately
-- before it). Safe to run against an old build too — the old build has
-- migrations 002..019 on disk and will see version=1, dirty=false, and
-- re-apply 002..019 which is a no-op because everything they add is
-- already present.
--
-- Usage (from a machine with network access to staging Postgres):
--   psql "$STAGING_DATABASE_URL" -v ON_ERROR_STOP=1 -f staging_resquash_to_v1.sql

BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '30s';

-- Defensive: guard against running on a DB that's already at v1.
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
  ELSIF cur_version < 19 THEN
    RAISE EXCEPTION 'schema_migrations at version %, expected 19 — this alignment script targets the 001..019 → 001 squash only', cur_version;
  END IF;
END $$;

DELETE FROM public.schema_migrations;
INSERT INTO public.schema_migrations (version, dirty) VALUES (1, false);

COMMIT;

SELECT version, dirty FROM public.schema_migrations;
