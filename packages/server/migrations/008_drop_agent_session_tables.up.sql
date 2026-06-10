-- 008: drop_agent_session_tables
--
-- Removes the four agent-session-sync tables. Coding-agent CLIs (Claude Code,
-- Codex, Gemini) change session formats and on-disk locations frequently, so
-- the session-sync feature kept breaking. We're trading the feature for CLI
-- stability — `memax agents sync` now syncs configs only.
--
-- Drop order respects FKs: snapshots reference sessions, so it goes first.
-- The R2 object-storage blobs referenced by these rows become orphaned;
-- a separate one-shot cleanup script will purge them outside the schema.

DROP TABLE IF EXISTS public.agent_session_snapshots;
DROP TABLE IF EXISTS public.agent_session_tombstones;
DROP TABLE IF EXISTS public.agent_session_sync_states;
DROP TABLE IF EXISTS public.agent_sessions;
