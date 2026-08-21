-- 023: board_slot_history
--
-- Slots are replaced in place (ON CONFLICT ... DO UPDATE), which means
-- every producer run silently destroys the previous card. For stateful
-- kinds (nightly dreamlog, nextup predictions, who-knows routing) the
-- old versions ARE the knowledge timeline — 同类的知识按照时间有新的
-- version 旧的 version. This table receives the outgoing content
-- whenever an upsert actually changes it; UpsertBoardSlot archives in
-- the same transaction and prunes to a per-slot cap so growth is
-- bounded by (slots × cap), not by nights elapsed.
CREATE TABLE board_slot_history (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    board_id uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    slot_key text NOT NULL,
    kind text NOT NULL,
    title text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    cite_memory_ids uuid[] DEFAULT '{}' NOT NULL,
    dream_run_id uuid,
    -- When the archived content was originally produced (the slot's
    -- content_updated_at at archive time) — the timeline axis.
    content_produced_at timestamp with time zone NOT NULL,
    archived_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX board_slot_history_slot_idx
    ON board_slot_history (board_id, slot_key, content_produced_at DESC);
