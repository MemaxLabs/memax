-- Revert 021: board_slot_content_updated
ALTER TABLE board_slots DROP COLUMN IF EXISTS content_updated_at;
