-- Revert 018: chat_session_persona
ALTER TABLE chat_sessions DROP COLUMN IF EXISTS persona_id;
