-- 018: chat_session_persona
--
-- Persona binding for the memax agent (Agent Chat). Semantics:
--   NULL   -> inherit the user's chat_default_persona_id setting
--   'none' -> explicitly no persona (overrides the default)
--   <uuid> -> that persona row
-- TEXT (not uuid) so the 'none' sentinel lives in one column; the handler
-- validates uuid values against the personas table on write.
ALTER TABLE chat_sessions ADD COLUMN persona_id text;
