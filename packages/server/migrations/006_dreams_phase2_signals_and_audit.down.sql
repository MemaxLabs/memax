-- Revert 006: dreams_phase2_signals_and_audit
--
-- Reverse-order undo: Part B (agent audit, the most recent
-- additions) goes first; Part A (signals + decision log) follows.

-- ────────────────────────────────────────────────────────────
-- Part B — undo phase 2b agent audit
-- ────────────────────────────────────────────────────────────

DROP INDEX IF EXISTS public.idx_dream_actions_run_agent_path;

ALTER TABLE public.dream_actions
    DROP CONSTRAINT IF EXISTS dream_actions_agent_path_check;

ALTER TABLE public.dream_actions
    DROP COLUMN IF EXISTS agent_path,
    DROP COLUMN IF EXISTS agent_session_id,
    DROP COLUMN IF EXISTS agent_model_calls,
    DROP COLUMN IF EXISTS agent_tool_calls;

-- ────────────────────────────────────────────────────────────
-- Part A — undo phase 2a signals
-- ────────────────────────────────────────────────────────────
--
-- The decision log table goes first; the column drops follow. The
-- two memory columns are nullable + new-ingest-only, so dropping
-- them won't strand data — pre-existing memories never had values
-- written. Worker code that reads the columns is gated behind the
-- soft-mode flag, so a rollback to pre-003 won't break recall or
-- ingest paths.

DROP TABLE IF EXISTS public.dream_trigger_decisions;

ALTER TABLE public.memories DROP COLUMN IF EXISTS user_followup_marker;
ALTER TABLE public.memories DROP COLUMN IF EXISTS source_fetch_hash;
