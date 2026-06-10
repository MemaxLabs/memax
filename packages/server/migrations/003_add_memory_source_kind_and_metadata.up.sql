-- 003: add_memory_source_kind_and_metadata
--
-- Plan 23 §4.1 — onboarding seed memories foundation.
--
-- `source_kind` is a sub-classification under `source`. For seeded
-- onboarding memories: `source = 'system'`, `source_kind =
-- 'onboarding-seed'`. Stays nullable + indexed so:
--   - existing rows (which won't have a kind) keep working
--   - admin queries that filter for seeds (`WHERE source_kind =
--     'onboarding-seed'`) hit the index
--   - plan 18's `first_memory` / `five_memories` triggers can
--     filter seeds OUT via `WHERE source_kind != 'onboarding-seed'
--     OR source_kind IS NULL` (deferred until plan 18 ships)
--
-- `metadata` is a structured bag for per-memory facts that don't
-- fit existing columns. Initially used by seeds:
--   - metadata->>'seed_origin_id' references the source seed-template
--     UUID so a partial unique index can prevent duplicate copies of
--     the same seed into the same hub on idempotent re-enqueue (the
--     worker job is River unique-arg; this is the DB-level guard for
--     the rare case the dedup window misses).
--
-- The partial unique index uses `WHERE source_kind = 'onboarding-seed'`
-- so it only covers seed copies — non-seed metadata can repeat the
-- same key without collision.

ALTER TABLE public.memories
  ADD COLUMN source_kind text,
  ADD COLUMN metadata jsonb;

CREATE INDEX memories_source_kind_idx
  ON public.memories (source_kind)
  WHERE source_kind IS NOT NULL;

CREATE UNIQUE INDEX memories_seed_origin_per_owner_uidx
  ON public.memories (owner_id, (metadata->>'seed_origin_id'))
  WHERE source_kind = 'onboarding-seed';
