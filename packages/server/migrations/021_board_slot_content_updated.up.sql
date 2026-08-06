-- 021: board_slot_content_updated
--
-- `updated_at` is bumped by BOTH producers and user actions (ack /
-- dismiss / feedback), which made it useless as a production-cadence
-- signal: the synthesis dedupe read it and concluded a card was fresh
-- because the user had just engaged with it — so the more a user used
-- the board, the longer it stayed silent. content_updated_at moves
-- only when a producer writes content.
ALTER TABLE board_slots ADD COLUMN content_updated_at timestamp with time zone DEFAULT now() NOT NULL;
