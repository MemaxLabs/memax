-- 037 down: v2_memory_embeddings

DROP TRIGGER IF EXISTS memory_versions_purge_embeddings ON v2.memory_versions;
DROP TRIGGER IF EXISTS memories_purge_embeddings ON v2.memories;
DROP TABLE IF EXISTS v2.memory_embeddings;
DROP FUNCTION IF EXISTS v2.purge_memory_embeddings();
DROP FUNCTION IF EXISTS v2.memory_embeddings_guard();

COMMENT ON COLUMN v2.memories.embedding IS NULL;
