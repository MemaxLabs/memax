-- Revert 005: chat_phase3_foundation
--
-- Drop in reverse FK-dependency order: approvals → events →
-- messages → sessions. The DEFERRABLE FK from chat_sessions to
-- chat_messages doesn't change drop order — the table-level DROP
-- CASCADE handles it.

DROP TABLE IF EXISTS public.chat_tool_approvals;
DROP TABLE IF EXISTS public.chat_message_events;
-- chat_messages must drop before chat_sessions because of the
-- DEFERRABLE FK chat_sessions.active_message_id → chat_messages.
-- Postgres drops the constraint with the table.
DROP TABLE IF EXISTS public.chat_messages CASCADE;
DROP TABLE IF EXISTS public.chat_sessions CASCADE;
