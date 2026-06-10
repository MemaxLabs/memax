-- 006: dreams_phase2_signals_and_audit
--
-- Squashes the un-shipped pair (formerly 003 phase2a_signals + 004
-- phase2b_agent_audit) into a single migration so staging applies
-- one row instead of two. Both halves are dream-engine domain and
-- ship together with the agent-runtime rollout (docs/plans/24);
-- splitting them on disk only costs migrations-applied count
-- without buying any deploy-time benefit. Squash is safe because
-- neither original migration has been applied to staging or prod
-- yet — they only live in this feature branch.
--
-- Numbered 006 (was 003 before the merge with origin/main)
-- because origin/main's 003/004/005 are different domains
-- (memory source_kind, system-user bootstrap, onboarding seed
-- memories) that landed in parallel; keeping the agent-runtime
-- migrations after them avoids version collision.
--
-- The two halves below are independent at the SQL level (they
-- touch different tables) and could re-split in the future if
-- one needs to ship without the other. For now they're a unit.

-- ────────────────────────────────────────────────────────────
-- Part A — phase 2a signals (formerly migration 003)
-- ────────────────────────────────────────────────────────────
--
-- Soft-mode trigger evaluator scaffold. Two memory-level signal
-- columns and one decision-log table. Plan 23 specified these
-- as Phase-0 deliverables that didn't ship; plan 24 lifts them
-- to a hard prerequisite for the agent integration.
--
-- Why now: before flipping the dream engine onto agent.Run for
-- contradict/organize, we need observed signal data to:
--   1. Tune trigger thresholds against real traffic (e.g. is
--      contradiction-count >= 3 the right cutoff? URL-drift byte
--      diff >= 256? topic-overload child count >= 12?)
--   2. Confirm the ≥80% lucid_passive margin assumption (plan 23)
--      by simulating "would this memory have fired a trigger?"
--      against ingested traffic.
-- The decision log captures both. Soft-mode = log only; the engine
-- continues using the existing single-call LLM path until calibration
-- says triggers behave correctly, at which point the behavior gate
-- flips.

-- memories.source_fetch_hash: when a URL-source memory is re-fetched
-- (a Phase 2b worker, not in scope here), the body hash of the
-- re-fetched HTML/markdown lands here. Distinct from content_hash,
-- which is the hash of the body STORED in the memory at ingest time.
-- A non-null source_fetch_hash != content_hash signals URL drift —
-- the canonical content has changed since we ingested it. Nullable
-- + new-ingest-only by design: backfill is expensive (re-fetch every
-- URL memory) and not required because the URL-drift trigger only
-- evaluates rows where source_fetch_hash is non-null. Pre-existing
-- memories stay null until they're re-fetched.
ALTER TABLE public.memories ADD COLUMN source_fetch_hash text;

-- memories.user_followup_marker: text holding an @TODO / @FIXME /
-- @QUESTION marker the ingest distill stage extracted from the
-- memory body, e.g. "@TODO verify this number". Holding the matched
-- text (not just a boolean) means the dream agent has actionable
-- context — "the user flagged 'verify this number' for follow-up"
-- — without re-scanning the body. Nullable + new-ingest-only:
-- existing memories stay null until they're re-ingested. The
-- ingest path writes this column; the trigger evaluator only
-- reads it.
ALTER TABLE public.memories ADD COLUMN user_followup_marker text;

-- dream_trigger_decisions: one row per memory the dream engine
-- considers during its scan loop, recording which trigger fired
-- (if any) and the raw signal values. Used for soft-mode calibration
-- — the engine logs decisions but does NOT change behavior in
-- Phase 2a. Phase 2b reads these rows when deciding whether to
-- promote a hub from soft-mode to active triggers.
--
-- One row per (memory considered, scan), so a single dream run
-- writes ~N_memories rows. For a typical hub with 200 memories, a
-- daily dream cycle adds ~200 rows; Phase 3 will add a retention
-- policy (default 90 days) but Phase 2a doesn't need one yet —
-- the calibration window is weeks, not months.
--
-- memory_id is uuid NOT NULL but NOT a FK reference because the
-- decision needs to outlive the memory it described: an organize-
-- phase decision that triggered a forget MUST keep its log entry
-- after the memory row is gone, otherwise we can't audit "why did
-- we forget this?" Mirrors the existing dream_actions pattern
-- (source_memory_ids text[], no FK).
CREATE TABLE public.dream_trigger_decisions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dream_run_id    uuid NOT NULL REFERENCES public.dream_runs(id) ON DELETE CASCADE,
    hub_id          uuid NOT NULL REFERENCES public.hubs(id) ON DELETE CASCADE,
    memory_id       uuid NOT NULL,
    trigger_fired   boolean NOT NULL DEFAULT false,
    trigger_kind    text NOT NULL DEFAULT 'none',
    signals         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT dream_trigger_decisions_kind_check
        CHECK (trigger_kind IN ('none', 'contradiction', 'url_drift', 'topic_overload', 'user_followup'))
);

-- "What did we decide for memory M, most recent first?" — the
-- investigation path for a misfire ("why did/didn't we trigger
-- on this memory?"). Memory + time order is the natural query.
CREATE INDEX idx_dream_trigger_decisions_memory
    ON public.dream_trigger_decisions (memory_id, created_at DESC);

-- "All decisions in run R" — the run-summary query. Surfaces a
-- run's full trigger fire/skip history without scanning the table.
CREATE INDEX idx_dream_trigger_decisions_run
    ON public.dream_trigger_decisions (dream_run_id);

-- "What's the trigger fire rate over the last N hours, by kind?" —
-- the calibration NUMERATOR query. Partial index on fired-only
-- rows because skip-rows are 95%+ of the table during soft-mode
-- and we don't want them in the calibration scan.
CREATE INDEX idx_dream_trigger_decisions_kind_fired
    ON public.dream_trigger_decisions (trigger_kind, created_at DESC)
    WHERE trigger_fired = true;

-- "How many decisions did we make for hub H over the last N
-- hours?" — the calibration DENOMINATOR query. Every fire-rate
-- metric is per-hub (cross-hub rates are meaningless because hubs
-- have wildly different memory volumes and trigger profiles), so
-- the (hub_id, created_at DESC) shape matches the query exactly.
-- Without this, soft-mode's skip-heavy table forces a Seq Scan for
-- every calibration window. The numerator partial index above
-- handles the "fired-by-kind" half; together they cover
-- both halves of fire-rate = numerator/denominator.
CREATE INDEX idx_dream_trigger_decisions_hub_recent
    ON public.dream_trigger_decisions (hub_id, created_at DESC);

-- ────────────────────────────────────────────────────────────
-- Part B — phase 2b agent audit (formerly migration 004)
-- ────────────────────────────────────────────────────────────
--
-- When the dream engine routes a contradict / organize decision
-- through agent.Run instead of the existing single-call LLM path,
-- the resulting dream_action row should carry enough audit
-- metadata to:
--   - distinguish lucid_active actions from single-call actions
--     in calibration / cost queries (the agent path costs ~8x the
--     single-call path because the agent can fan out across
--     multiple turns before producing a verdict);
--   - look up the SDK session id for log/trace correlation when
--     debugging a misroute or a budget exhaust;
--   - report accurate model-call + tool-call counts for cost
--     attribution (logging 1 LLM call per agent.Run when the
--     real round-trip count is up to MaxModelCalls=8 silently
--     under-counts cost pressure by ~8x).
--
-- All columns are NULL/0-defaulted so existing dream_actions rows
-- (and the single-call path that continues to write them) stay
-- valid. agent_path defaults to 'single_call' so the existing
-- query "what kind of action did the engine take" doesn't need
-- conditional branches for old vs new rows.

ALTER TABLE public.dream_actions
    ADD COLUMN agent_path        text NOT NULL DEFAULT 'single_call',
    ADD COLUMN agent_session_id  text,
    ADD COLUMN agent_model_calls integer NOT NULL DEFAULT 0,
    ADD COLUMN agent_tool_calls  integer NOT NULL DEFAULT 0;

-- Constrain agent_path to known values. Adding a row outside this
-- list is a programmer error (typo on the engine side) — the CHECK
-- catches it at write time. Mirror of the Go consts in
-- internal/dreams/agent_audit.go (kept in lockstep by tests).
ALTER TABLE public.dream_actions
    ADD CONSTRAINT dream_actions_agent_path_check
        CHECK (agent_path IN ('single_call', 'lucid_active'));

-- "What lucid_active actions did this run produce?" — the audit
-- query for an opted-in hub. Partial index on the non-default
-- value because single-call rows are >99% of the table during
-- staged rollout and we don't want them in the agent-path scan.
CREATE INDEX idx_dream_actions_run_agent_path
    ON public.dream_actions (run_id, agent_path)
    WHERE agent_path = 'lucid_active';
