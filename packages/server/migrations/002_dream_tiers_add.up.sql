-- 002: dream_tiers_add
--
-- Adds the data model for the Basic/Lucid dream tier split per
-- docs/plans/23-dream-tiers.md. This is Migration A: ADD-only. It is
-- safe to ship while old code still reads `plans.dreams_enabled` —
-- old code continues to work, new code starts populating the new
-- columns.
--
-- A SEPARATE later migration will drop `plans.dreams_enabled` once
-- all consumers (server, web, admin) have moved to the new fields.
-- See the plan's "Migration B" section + the targeted grep gate.
--
-- New surface introduced:
--   1. plans.basic_dream_limit, plans.lucid_dream_limit (int, -1=unlimited, 0=disabled)
--   2. usage.basic_dream_count, usage.lucid_dream_count (per-period counters)
--   3. hub_usage table — monthly counters keyed by hub for shared-pool quotas
--   4. dream_run_completions table — idempotency ledger keyed by dream_run_id
--   5. dream_quota_signals table — Phase-1 soft-mode telemetry sink
--   6. dream_runs.triggered_by, .tier, .tool_loop_iterations, .terminal_state
--      (audit fields per the plan's "Audit" decision)

-- ─────────────────────────────────────────────────────────────────────
-- 1. Plan-level dream limits.
-- ─────────────────────────────────────────────────────────────────────

ALTER TABLE plans ADD COLUMN basic_dream_limit int NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN lucid_dream_limit int NOT NULL DEFAULT 0;

COMMENT ON COLUMN plans.basic_dream_limit IS
    'Monthly Basic Dream run quota. -1 = unlimited, 0 = disabled. See docs/plans/23-dream-tiers.md.';
COMMENT ON COLUMN plans.lucid_dream_limit IS
    'Monthly Lucid Dream run quota. -1 = unlimited, 0 = disabled. See docs/plans/23-dream-tiers.md.';

-- Backfill the per-tier numbers locked in the plan. Active rows get
-- the real values; inactive shadows mirror them defensively in case
-- they are ever activated.
UPDATE plans SET basic_dream_limit = 40 WHERE id IN ('personal_free', 'free');

UPDATE plans SET lucid_dream_limit = 40 WHERE id IN ('personal_pro', 'pro');
UPDATE plans SET lucid_dream_limit = 80 WHERE id IN ('personal_pro_plus', 'pro_plus');

-- Early access is grandfathered to Pro+ tier (80 Lucid/mo) since it
-- predates the tier split and was the previous "premium" personal plan.
UPDATE plans SET lucid_dream_limit = 80 WHERE id IN ('personal_early_access', 'early_access');

-- Team hub plan: fixed-per-hub 100/mo regardless of seat count.
UPDATE plans SET lucid_dream_limit = 100 WHERE id IN ('hub_team', 'team');

-- Enterprise: unlimited on both for paid hub deployments. Personal-side
-- Enterprise (if ever created) would inherit the same.
UPDATE plans SET basic_dream_limit = -1, lucid_dream_limit = -1
    WHERE id IN ('hub_enterprise', 'enterprise');

-- hub_free_team gets no Lucid bucket: free team hubs participate in
-- the Basic-only personal experience for their members. Members'
-- personal hubs use their own personal_free Basic 40/mo, unaffected
-- by the field-wise resolver rule because hub_free_team's dream
-- limits are both 0 (it does not contribute a stronger signal).
-- No UPDATE needed; defaults of 0 are correct.

-- ─────────────────────────────────────────────────────────────────────
-- 2. User-side dream counters (mirrors hub side defined below).
-- ─────────────────────────────────────────────────────────────────────

ALTER TABLE usage ADD COLUMN basic_dream_count int NOT NULL DEFAULT 0;
ALTER TABLE usage ADD COLUMN lucid_dream_count int NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage.basic_dream_count IS
    'Basic Dream runs consumed in this billing period (calendar month). Incremented atomically at run completion.';
COMMENT ON COLUMN usage.lucid_dream_count IS
    'Lucid Dream runs consumed in this billing period (calendar month). Incremented atomically at run completion. lucid_passive and lucid_active both count here; the cost-tier distinction is for telemetry only.';

-- ─────────────────────────────────────────────────────────────────────
-- 3. hub_usage — monthly counters keyed by hub for shared-pool quotas.
-- ─────────────────────────────────────────────────────────────────────
--
-- Mirrors the user-scoped `usage` table shape but keyed by hub_id so
-- a Team hub's 100/mo Lucid pool can be shared across all members.
-- One row per (hub, billing period). Created lazily on first run of
-- a new period via INSERT ... ON CONFLICT in the consume primitive.
--
-- Future-proofing: room to grow with hub-scoped push/recall/ask
-- counters as billing surface evolves; not used today, intentionally
-- left out of v1 to keep the migration scoped.

CREATE TABLE hub_usage (
    id                uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    hub_id            uuid        NOT NULL REFERENCES hubs(id) ON DELETE CASCADE,
    period_start      date        NOT NULL,
    period_end        date        NOT NULL,
    lucid_dream_count int         NOT NULL DEFAULT 0,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (hub_id, period_start)
);

CREATE INDEX idx_hub_usage_period ON hub_usage (period_start, period_end);

COMMENT ON TABLE hub_usage IS
    'Monthly counters keyed by hub for shared-pool quotas (e.g., Team hub Lucid runs). Mirror of `usage` for user-scoped counters. See docs/plans/23-dream-tiers.md.';

-- ─────────────────────────────────────────────────────────────────────
-- 4. dream_run_completions — idempotency ledger keyed by dream_run_id.
-- ─────────────────────────────────────────────────────────────────────
--
-- The single source of truth for whether a run was counted toward
-- quota. The dream_run_id PRIMARY KEY enforces "counted exactly once"
-- under retries; the `counted` flag distinguishes "we recorded this
-- run happened" from "we charged it to quota" (some terminal states
-- record without charging — see the plan's truth table).

CREATE TABLE dream_run_completions (
    dream_run_id   uuid        NOT NULL PRIMARY KEY REFERENCES dream_runs(id) ON DELETE CASCADE,
    scope_type     text        NOT NULL CHECK (scope_type IN ('user', 'hub')),
    scope_id       uuid        NOT NULL,
    period_start   date        NOT NULL,
    tier           text        NOT NULL CHECK (tier IN ('basic', 'lucid_passive', 'lucid_active')),
    counted        bool        NOT NULL DEFAULT false,
    terminal_state text        NOT NULL CHECK (terminal_state IN (
                                  'completed',
                                  'partial_failed',
                                  'failed',
                                  'skipped_concurrent',
                                  'no_memories',
                                  'loop_cap_hit',
                                  'quota_race_lost'
                              )),
    completed_at   timestamptz NOT NULL DEFAULT now(),

    -- Defense in depth for the plan's truth table: a row may only
    -- have counted=true if its terminal_state is one of the three
    -- "did meaningful work, charge it" states. Application code
    -- already enforces this in the consume primitive; the CHECK
    -- catches a future bug or a hand-edited row before it corrupts
    -- per-period totals.
    CONSTRAINT drc_counted_only_for_chargeable_states
        CHECK (NOT counted OR terminal_state IN ('completed', 'partial_failed', 'loop_cap_hit'))
);

CREATE INDEX idx_drc_scope_period ON dream_run_completions (scope_type, scope_id, period_start);

COMMENT ON TABLE dream_run_completions IS
    'Idempotency ledger for dream-run quota accounting. One row per terminal run; PRIMARY KEY enforces exactly-once counting under retries. See docs/plans/23-dream-tiers.md.';
COMMENT ON COLUMN dream_run_completions.scope_id IS
    'user_id when scope_type=user, hub_id when scope_type=hub. Not a foreign key because the referent table varies; integrity is enforced by the consume primitive.';
COMMENT ON COLUMN dream_run_completions.counted IS
    'TRUE if this run was added to the usage/hub_usage counter. FALSE for terminal states that record-but-do-not-charge (failed, skipped_concurrent, no_memories, quota_race_lost).';

-- ─────────────────────────────────────────────────────────────────────
-- 5. dream_quota_signals — soft-mode telemetry sink for Phase 1 only.
-- ─────────────────────────────────────────────────────────────────────
--
-- Captures what would have happened under hard enforcement so we can
-- size the caps with real data before flipping enforcement on. Every
-- column is a snapshot at completion time so dashboards remain valid
-- even if the user later changes plan or a hub's cap is adjusted
-- mid-period — reconstruct-from-mutable-counters is forbidden.
--
-- Append-only. Dropped after Phase 3 enforcement is stable (a future
-- migration; not on a deadline).

CREATE TABLE dream_quota_signals (
    dream_run_id        uuid        NOT NULL PRIMARY KEY REFERENCES dream_runs(id) ON DELETE CASCADE,
    scope_type          text        NOT NULL CHECK (scope_type IN ('user', 'hub')),
    scope_id            uuid        NOT NULL,
    period_start        date        NOT NULL,
    tier                text        NOT NULL CHECK (tier IN ('basic', 'lucid_passive', 'lucid_active')),
    mode                text        NOT NULL CHECK (mode IN ('soft', 'hard')),
    resolved_limit      int         NOT NULL,
    used_after          int         NOT NULL,
    quota_source        text        NOT NULL,
    would_have_blocked  bool        NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_dqs_period_blocked ON dream_quota_signals (period_start, would_have_blocked);

COMMENT ON TABLE dream_quota_signals IS
    'Phase-1 soft-mode telemetry: per-run snapshot capturing what would have happened under hard enforcement. Drives capacity-planning dashboards. Dropped after Phase 3 stabilizes. See docs/plans/23-dream-tiers.md.';
COMMENT ON COLUMN dream_quota_signals.resolved_limit IS
    'Limit value at run completion time. Frozen — must not be reread later.';
COMMENT ON COLUMN dream_quota_signals.quota_source IS
    'Plan ID that contributed the limit (e.g., personal_pro, hub_team). Frozen at insert; survives later plan changes.';

-- ─────────────────────────────────────────────────────────────────────
-- 6. dream_runs audit fields.
-- ─────────────────────────────────────────────────────────────────────

ALTER TABLE dream_runs ADD COLUMN triggered_by         text NOT NULL DEFAULT 'nightly';
ALTER TABLE dream_runs ADD COLUMN tier                 text;
ALTER TABLE dream_runs ADD COLUMN tool_loop_iterations int  NOT NULL DEFAULT 0;
ALTER TABLE dream_runs ADD COLUMN terminal_state       text;

-- Mirror the ledger's enum CHECKs on dream_runs so the
-- query-convenience copy can never drift from the canonical values
-- in dream_run_completions. NULL is allowed on tier and
-- terminal_state because rows are populated at run completion;
-- in-flight runs leave both NULL.
ALTER TABLE dream_runs ADD CONSTRAINT dream_runs_tier_check
    CHECK (tier IS NULL OR tier IN ('basic', 'lucid_passive', 'lucid_active'));

ALTER TABLE dream_runs ADD CONSTRAINT dream_runs_terminal_state_check
    CHECK (terminal_state IS NULL OR terminal_state IN (
        'completed',
        'partial_failed',
        'failed',
        'skipped_concurrent',
        'no_memories',
        'loop_cap_hit',
        'quota_race_lost'
    ));

-- Iteration counters cannot be negative. Defends against a buggy
-- decrement path in the engine's LucidLoopBudget.
ALTER TABLE dream_runs ADD CONSTRAINT dream_runs_tool_loop_iterations_nonneg
    CHECK (tool_loop_iterations >= 0);

COMMENT ON COLUMN dream_runs.triggered_by IS
    '"nightly" (engine sweep) or "manual:<user_id>" (POST /v1/dreams/trigger). Drives audit and the team-hub usage tab.';
COMMENT ON COLUMN dream_runs.tier IS
    '"basic" | "lucid_passive" | "lucid_active". Lucid runs that did not fire the tool-use loop are lucid_passive (single-call cost). Set at completion; NULL while the run is still in flight.';
COMMENT ON COLUMN dream_runs.tool_loop_iterations IS
    'Number of LLM-call+tool-call cycles consumed by this run. Hard cap of 8 enforced by LucidLoopBudget in the engine. Drives the lucid_active vs lucid_passive ratio in margin telemetry.';
COMMENT ON COLUMN dream_runs.terminal_state IS
    'Mirror of dream_run_completions.terminal_state for query convenience (avoids a join when reading the dream_runs surface). Set at completion alongside the ledger insert.';
