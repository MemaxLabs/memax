-- 015: topic_archive
--
-- Topic archive: soft lifecycle state for topics, mirroring memory archival.
-- Archived topics are hidden from the active tree, excluded from AI
-- organization (dreams, inline classification) and recall boosts, but keep
-- their memory assignments so restore is lossless.
ALTER TABLE public.topics
    ADD COLUMN archived_at timestamp with time zone;

-- Fast lookup of a hub's archived topics (the archived browse surface).
CREATE INDEX idx_topics_archived ON public.topics (hub_id)
    WHERE archived_at IS NOT NULL;
