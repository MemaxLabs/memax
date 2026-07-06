-- Revert 015: topic_archive

DROP INDEX IF EXISTS idx_topics_archived;

ALTER TABLE public.topics
    DROP COLUMN IF EXISTS archived_at;
