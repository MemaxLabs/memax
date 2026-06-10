-- Revert 003: add_memory_source_kind_and_metadata

DROP INDEX IF EXISTS public.memories_seed_origin_per_owner_uidx;
DROP INDEX IF EXISTS public.memories_source_kind_idx;

ALTER TABLE public.memories
  DROP COLUMN IF EXISTS metadata,
  DROP COLUMN IF EXISTS source_kind;
