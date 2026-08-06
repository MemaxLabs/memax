-- Revert 020: chat_pinned_memories
ALTER TABLE chat_sessions DROP COLUMN IF EXISTS pinned_memory_ids;
