-- Align an existing pre-launch staging database with 001_baseline_v1.
-- Run only while old API/worker instances are stopped, then deploy the baseline build.

BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '4min';

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

ALTER TABLE public.memories ADD COLUMN IF NOT EXISTS kind text;
ALTER TABLE public.memories ADD COLUMN IF NOT EXISTS stability text;
ALTER TABLE public.memories ADD COLUMN IF NOT EXISTS retrieval_weight double precision;
ALTER TABLE public.memories ADD COLUMN IF NOT EXISTS access_intents jsonb;

ALTER TABLE public.chunks ADD COLUMN IF NOT EXISTS kind text;
ALTER TABLE public.chunks ADD COLUMN IF NOT EXISTS stability text;
ALTER TABLE public.chunks ADD COLUMN IF NOT EXISTS retrieval_weight double precision;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'memories' AND column_name = 'category'
  ) THEN
    EXECUTE $sql$
      UPDATE public.memories
      SET kind = CASE split_part(category, '/', 1)
        WHEN 'daily' THEN 'episodic'
        WHEN 'chat' THEN 'episodic'
        WHEN 'meetings' THEN 'episodic'
        WHEN 'process' THEN 'procedural'
        WHEN 'decisions' THEN 'rationale'
        ELSE 'semantic'
      END
      WHERE kind IS NULL OR kind = '' OR kind NOT IN ('episodic', 'semantic', 'procedural', 'rationale')
    $sql$;

    EXECUTE $sql$
      UPDATE public.memories
      SET stability = CASE split_part(category, '/', 1)
        WHEN 'daily' THEN 'volatile'
        WHEN 'chat' THEN 'volatile'
        WHEN 'meetings' THEN 'volatile'
        WHEN 'decisions' THEN 'stable'
        WHEN 'personal' THEN 'stable'
        ELSE 'evolving'
      END
      WHERE stability IS NULL OR stability = '' OR stability NOT IN ('volatile', 'evolving', 'stable')
    $sql$;
  END IF;
END $$;

UPDATE public.memories
SET kind = 'semantic'
WHERE kind IS NULL OR kind = '' OR kind NOT IN ('episodic', 'semantic', 'procedural', 'rationale');

UPDATE public.memories
SET stability = 'evolving'
WHERE stability IS NULL OR stability = '' OR stability NOT IN ('volatile', 'evolving', 'stable');

UPDATE public.memories
SET retrieval_weight = 1.0
WHERE retrieval_weight IS NULL OR retrieval_weight <= 0;

UPDATE public.memories
SET access_intents = '{}'::jsonb
WHERE access_intents IS NULL;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'chunks' AND column_name = 'category'
  ) THEN
    EXECUTE $sql$
      UPDATE public.chunks c
      SET kind = CASE COALESCE(NULLIF(split_part(c.category, '/', 1), ''), m.kind)
        WHEN 'daily' THEN 'episodic'
        WHEN 'chat' THEN 'episodic'
        WHEN 'meetings' THEN 'episodic'
        WHEN 'process' THEN 'procedural'
        WHEN 'decisions' THEN 'rationale'
        WHEN 'episodic' THEN 'episodic'
        WHEN 'procedural' THEN 'procedural'
        WHEN 'rationale' THEN 'rationale'
        ELSE 'semantic'
      END
      FROM public.memories m
      WHERE c.memory_id = m.id
        AND (c.kind IS NULL OR c.kind = '' OR c.kind NOT IN ('episodic', 'semantic', 'procedural', 'rationale'))
    $sql$;

    EXECUTE $sql$
      UPDATE public.chunks c
      SET stability = CASE COALESCE(NULLIF(split_part(c.category, '/', 1), ''), m.stability)
        WHEN 'daily' THEN 'volatile'
        WHEN 'chat' THEN 'volatile'
        WHEN 'meetings' THEN 'volatile'
        WHEN 'decisions' THEN 'stable'
        WHEN 'personal' THEN 'stable'
        WHEN 'volatile' THEN 'volatile'
        WHEN 'stable' THEN 'stable'
        ELSE 'evolving'
      END
      FROM public.memories m
      WHERE c.memory_id = m.id
        AND (c.stability IS NULL OR c.stability = '' OR c.stability NOT IN ('volatile', 'evolving', 'stable'))
    $sql$;
  END IF;
END $$;

UPDATE public.chunks c
SET kind = m.kind
FROM public.memories m
WHERE c.memory_id = m.id
  AND (c.kind IS NULL OR c.kind = '' OR c.kind NOT IN ('episodic', 'semantic', 'procedural', 'rationale'));

UPDATE public.chunks c
SET stability = m.stability
FROM public.memories m
WHERE c.memory_id = m.id
  AND (c.stability IS NULL OR c.stability = '' OR c.stability NOT IN ('volatile', 'evolving', 'stable'));

UPDATE public.chunks
SET retrieval_weight = 1.0
WHERE retrieval_weight IS NULL OR retrieval_weight <= 0;

ALTER TABLE public.memories ALTER COLUMN kind SET DEFAULT 'semantic';
ALTER TABLE public.memories ALTER COLUMN kind SET NOT NULL;
ALTER TABLE public.memories ALTER COLUMN stability SET DEFAULT 'evolving';
ALTER TABLE public.memories ALTER COLUMN stability SET NOT NULL;
ALTER TABLE public.memories ALTER COLUMN retrieval_weight SET DEFAULT 1.0;
ALTER TABLE public.memories ALTER COLUMN retrieval_weight SET NOT NULL;
ALTER TABLE public.memories ALTER COLUMN access_intents SET DEFAULT '{}'::jsonb;
ALTER TABLE public.memories ALTER COLUMN access_intents SET NOT NULL;

ALTER TABLE public.chunks ALTER COLUMN kind SET DEFAULT 'semantic';
ALTER TABLE public.chunks ALTER COLUMN kind SET NOT NULL;
ALTER TABLE public.chunks ALTER COLUMN stability SET DEFAULT 'evolving';
ALTER TABLE public.chunks ALTER COLUMN stability SET NOT NULL;
ALTER TABLE public.chunks ALTER COLUMN retrieval_weight SET DEFAULT 1.0;
ALTER TABLE public.chunks ALTER COLUMN retrieval_weight SET NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'memories_kind_check') THEN
    ALTER TABLE public.memories ADD CONSTRAINT memories_kind_check CHECK (kind IN ('episodic', 'semantic', 'procedural', 'rationale'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'memories_stability_check') THEN
    ALTER TABLE public.memories ADD CONSTRAINT memories_stability_check CHECK (stability IN ('volatile', 'evolving', 'stable'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chunks_kind_check') THEN
    ALTER TABLE public.chunks ADD CONSTRAINT chunks_kind_check CHECK (kind IN ('episodic', 'semantic', 'procedural', 'rationale'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chunks_stability_check') THEN
    ALTER TABLE public.chunks ADD CONSTRAINT chunks_stability_check CHECK (stability IN ('volatile', 'evolving', 'stable'));
  END IF;
END $$;

DROP INDEX IF EXISTS public.idx_memories_category;
CREATE INDEX IF NOT EXISTS idx_memories_kind ON public.memories USING btree (kind);
CREATE INDEX IF NOT EXISTS idx_memories_stability ON public.memories USING btree (stability);
CREATE INDEX IF NOT EXISTS idx_chunks_kind ON public.chunks USING btree (kind);

ALTER TABLE public.chunks DROP COLUMN IF EXISTS category;
ALTER TABLE public.memories DROP COLUMN IF EXISTS category;

CREATE TABLE IF NOT EXISTS public.schema_migrations (
  version bigint NOT NULL PRIMARY KEY,
  dirty boolean NOT NULL
);
DELETE FROM public.schema_migrations;
INSERT INTO public.schema_migrations (version, dirty) VALUES (1, false);

COMMIT;

SELECT version, dirty FROM public.schema_migrations;
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name IN ('memories', 'chunks')
  AND column_name IN ('category', 'kind', 'stability', 'retrieval_weight', 'access_intents')
ORDER BY table_name, column_name;
