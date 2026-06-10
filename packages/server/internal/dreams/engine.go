// Package dreams implements Memory Dreams — background consolidation of user memories.
// Inspired by biological memory consolidation during sleep, the dream engine:
// 1. Detects semantically duplicate memories and merges them
// 2. Identifies contradictions between memories for user review
// 3. Archives stale, low-value memories that have fully decayed
// 4. Generates a "dream report" summarizing what was consolidated
package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	// SimilarityThreshold is the minimum cosine similarity to consider two memories as duplicates.
	SimilarityThreshold = 0.85

	// ContradictionThreshold is the similarity range where contradictions are likely.
	// Memories that are topically related (>0.7) but not exact duplicates (<0.85)
	// are candidates for contradiction detection.
	ContradictionThreshold = 0.70

	// StalenessDecayFloor — memories with a decay multiplier at the floor (0.3) and
	// zero access count are candidates for archival.
	StalenessMaxDecay = 0.35

	// MaxPairsPerRun limits the number of similar pairs to process per dream cycle.
	MaxPairsPerRun = 30

	// dreamLLMTimeout bounds any single LLM call made during a dream cycle.
	dreamLLMTimeout = 12 * time.Second

	// staleRunThreshold bounds how long a `running` dream_run row can
	// go without a heartbeat before the engine reclaims it on the
	// next cycle. The engine heartbeats at every phase boundary AND
	// inside every LLM-backed inner loop (per merge pair, per
	// contradiction pair, per organize batch, per restructure
	// suggestion). The largest gap between successive heartbeats is
	// therefore a single pair's worst-case runtime — roughly
	// 30 seconds of LLM latency plus up to ~90 seconds of embedder
	// retry — not the whole cycle.
	//
	// 30 min is ~8× the largest plausible intra-loop quiet window,
	// so a slow-but-alive cycle can never be reclaimed. Reclaim flips
	// the row to `status='failed'` and surfaces it in audit history;
	// the replacement cycle then proceeds normally.
	staleRunThreshold = 30 * time.Minute

	// organizeBatchMaxItems bounds organize batches by memory count.
	organizeBatchMaxItems = 10

	// organizeBatchMaxPreviewChars bounds organize batches by the aggregate
	// preview characters included in the prompt.
	organizeBatchMaxPreviewChars = 1200

	// organizeTopicContextLimit bounds how many existing topics the organize
	// prompt can see for any one batch.
	organizeTopicContextLimit = 30

	// organizeTopicCoverageFloor preserves some high-signal existing topics in
	// the prompt even when embedding relevance is sparse.
	organizeTopicCoverageFloor = 8

	// bootstrapOrganizeThreshold switches the run into a more generous
	// background-processing mode for large inbox backlogs.
	bootstrapOrganizeThreshold = 40
)

const (
	dreamModeMaintenance = "maintenance"
	dreamModeBootstrap   = "bootstrap"
)

// planResolver resolves a user's effective plan limits.
type planResolver interface {
	GetUserLimits(ctx context.Context, userID, planID string) model.UserLimits
}

// meterLogger is the subset of meter.Meter the dream engine needs to
// emit usage_events rows. A function-typed callback would work, but
// mirroring the planResolver interface pattern keeps the dependency
// surface consistent and avoids a callback typedef for a single
// method.
type meterLogger interface {
	LogEvent(userID, op, hubID, source, agentName string, metadata map[string]any)
}

// llmClient is the subset of *anthropic.Client the engine uses.
// Extracted into an interface so integration tests can inject a
// deterministic fake without real API calls. The concrete
// *anthropic.Client satisfies this interface with no changes.
type llmClient interface {
	Complete(ctx context.Context, req anthropic.CompleteRequest) (*anthropic.CompleteResponse, error)
}

// dreamQuotaStore is the union of both quota interfaces the engine
// needs: usage reads at start-time + atomic-consume at completion.
// Production *store.Store satisfies both; this private union lets
// SetQuota accept a single value and avoids the runtime type-assert
// hazard Codex flagged.
type dreamQuotaStore interface {
	quota.DreamUsageStore
	quota.DreamConsumeStore
}

// Engine runs the Memory Dreams consolidation pipeline.
type Engine struct {
	store         store.Store
	txRunner      store.TxRunner
	embedder      embed.Embedder
	client        llmClient
	events        events.Publisher
	model         string // Haiku — merge, contradiction
	organizeModel string // Sonnet — organize, restructure (needs stronger reasoning)
	plans         planResolver
	meter         meterLogger
	// Dream-tier quota wiring. quotaResolver+quotaStore are nil-safe;
	// when unset, finalizeDreamRun skips ConsumeDreamRun (legacy/dev
	// paths and tests that don't drive the quota path).
	quotaResolver quota.DreamResolver
	quotaStore    dreamQuotaStore
	quotaMode     quota.Mode
	// agentRuntime is the optional Phase 2b agentic-dream wiring.
	// Nil means agent path disabled regardless of any per-hub
	// `dreams_use_agent_runtime` setting — when the runtime isn't
	// configured at app startup (no Anthropic key, no recall
	// service), per-hub opt-in CANNOT silently drag the cycle
	// onto a path that wasn't built. Ships nil today; serverapp /
	// workerapp wires a real runtime in Phase 2b commit 3.
	agentRuntime *AgentRuntime
}

// AgentRuntime bundles the dependencies the dream engine's
// agentic-phase router needs. Constructed once at app startup and
// passed via SetAgentRuntime so the Engine can stay instantiable
// in tests that don't exercise the agent path.
//
// Nil semantics: when AgentRuntime is nil OR any of its fields
// are nil, every dream phase falls through to the existing
// single-call LLM path regardless of per-hub flag state. This
// fail-closed default keeps the agent path opt-in at TWO layers
// (app config + per-hub flag) so a misconfiguration in either
// direction can't accidentally route real users onto a path that
// isn't fully wired.
type AgentRuntime struct {
	// Model is the SDK provider client (Anthropic via
	// providers.NewAnthropicFromEnv). Required.
	Model agentModelClient

	// RecallService is the production recall pipeline implementation
	// the dream agent's recall_memories tool calls into. Required.
	RecallService agentRecallService

	// Resolver computes per-call SessionScope from the membership
	// store. Required.
	Resolver *agent.ScopeResolver
}

// IsConfigured reports whether the runtime has every dependency
// the agent path requires. Engines without it short-circuit every
// per-memory routing decision to "single-call".
func (r *AgentRuntime) IsConfigured() bool {
	return r != nil && r.Model != nil && r.RecallService != nil && r.Resolver != nil
}

// agentModelClient is the narrow shape we need from the SDK
// provider client. Aliased to a local interface (rather than a
// direct sdkmodel.Client dependency at this layer) so the engine
// stays decoupled from SDK specifics — internal/agent owns the
// SDK contract; the dream engine just hands the client off to
// agent.Run.
//
// We don't redeclare the methods on this side because they're
// private to the SDK; the alias is purely a type alias for clarity.
type agentModelClient = sdkmodel.Client

// agentRecallService is the recall-tool dependency. Aliased to
// the tools-package interface so the engine doesn't accidentally
// depend on the postgres impl.
type agentRecallService = tools.RecallService

// SetPlanResolver wires the plan registry for dreams-enabled enforcement.
func (e *Engine) SetPlanResolver(p planResolver) {
	if e != nil {
		e.plans = p
	}
}

// SetMeter wires the meter so dream phases can write usage_events
// rows. Nil is allowed — tests and standalone runs skip metering
// silently. Wired in serverapp.App / workerapp.App at startup.
func (e *Engine) SetMeter(m meterLogger) {
	if e != nil {
		e.meter = m
	}
}

// SetQuota wires the dream-tier quota plumbing. Both arguments are
// required for the gate to fire: resolver picks the limits, store
// performs both the start-time usage read AND the atomic-consume
// transaction at completion. Pass nil/nil to disable quota
// accounting (legacy and test paths). The store argument is
// production-typically the same *store.PostgresStore the engine was
// constructed with — it satisfies both quota.DreamUsageStore and
// quota.DreamConsumeStore.
//
// Mode defaults to soft if not set; SetQuotaMode flips it.
func (e *Engine) SetQuota(resolver quota.DreamResolver, qs dreamQuotaStore) {
	if e == nil {
		return
	}
	e.quotaResolver = resolver
	e.quotaStore = qs
	if e.quotaMode == "" {
		e.quotaMode = quota.ModeSoft
	}
}

// SetQuotaMode overrides the engine's enforcement mode. Defaults to
// soft (Phase 1 rollout). Set to ModeHard for Phase 3.
func (e *Engine) SetQuotaMode(m quota.Mode) {
	if e != nil && m.Valid() {
		e.quotaMode = m
	}
}

// SetAgentRuntime wires the Phase 2b agentic-dream router. Setting
// nil (the default) keeps every dream cycle on the existing
// single-call path regardless of per-hub flags. Setting a runtime
// with any nil dependency (Model, RecallService, Resolver) is
// equivalent to nil — the per-memory router calls IsConfigured
// before routing onto the agent path.
//
// Wired by serverapp / workerapp at startup once Anthropic
// credentials AND the recall service are available; tests can
// drop in a fake runtime by constructing AgentRuntime directly.
func (e *Engine) SetAgentRuntime(r *AgentRuntime) {
	if e != nil {
		e.agentRuntime = r
	}
}

type dreamExecutionConfig struct {
	mode                   string
	organizeCandidateLimit int
	organizeMaxLLMCalls    int
	organizeBatchSize      int
	organizePreviewChars   int
	organizeTopicLimit     int
	organizeTimeout        time.Duration
	restructureTimeout     time.Duration
}

func maintenanceDreamConfig() dreamExecutionConfig {
	return dreamExecutionConfig{
		mode:                   dreamModeMaintenance,
		organizeCandidateLimit: 200,
		organizeMaxLLMCalls:    10,
		organizeBatchSize:      organizeBatchMaxItems,
		organizePreviewChars:   organizeBatchMaxPreviewChars,
		organizeTopicLimit:     organizeTopicContextLimit,
		organizeTimeout:        20 * time.Second,
		restructureTimeout:     20 * time.Second,
	}
}

func bootstrapDreamConfig() dreamExecutionConfig {
	return dreamExecutionConfig{
		mode:                   dreamModeBootstrap,
		organizeCandidateLimit: 400,
		organizeMaxLLMCalls:    20,
		organizeBatchSize:      organizeBatchMaxItems,
		organizePreviewChars:   organizeBatchMaxPreviewChars,
		organizeTopicLimit:     organizeTopicContextLimit,
		organizeTimeout:        24 * time.Second,
		restructureTimeout:     24 * time.Second,
	}
}

func defaultDreamPhaseBudgets(cfg dreamExecutionConfig) map[string]model.DreamPhaseBudget {
	return map[string]model.DreamPhaseBudget{
		"merge": {
			CandidateLimit: MaxPairsPerRun,
			MaxLLMCalls:    MaxPairsPerRun,
			TimeoutMs:      dreamLLMTimeout.Milliseconds(),
		},
		"contradiction": {
			CandidateLimit: MaxPairsPerRun * 2,
			MaxLLMCalls:    MaxPairsPerRun * 2,
			TimeoutMs:      dreamLLMTimeout.Milliseconds(),
		},
		"archive": {
			CandidateLimit: 500,
		},
		"organize": {
			CandidateLimit:    cfg.organizeCandidateLimit,
			BatchSize:         cfg.organizeBatchSize,
			PreviewCharBudget: cfg.organizePreviewChars,
			TopicContextLimit: cfg.organizeTopicLimit,
			MaxLLMCalls:       cfg.organizeMaxLLMCalls,
			TimeoutMs:         cfg.organizeTimeout.Milliseconds(),
		},
		"restructure": {
			MaxLLMCalls: 1,
			TimeoutMs:   cfg.restructureTimeout.Milliseconds(),
		},
	}
}

// New creates a Dream Engine. Returns nil if the LLM client is nil.
//
// Requires s to satisfy store.TxRunner so producer fan-outs
// (review + notification + dream action) can run inside one
// transaction. Every Store implementation in this repo already
// satisfies TxRunner; the assertion exists only to fail loudly at
// startup if a new implementation forgets to include the tx-aware
// siblings.
func New(s store.Store, embedder embed.Embedder, client *anthropic.Client, publisher events.Publisher) *Engine {
	if client == nil {
		return nil
	}
	txRunner, ok := s.(store.TxRunner)
	if !ok {
		panic("dreams: store does not implement store.TxRunner")
	}
	return &Engine{
		store:         s,
		txRunner:      txRunner,
		embedder:      embedder,
		client:        client,
		events:        publisher,
		model:         anthropic.ModelFromEnv("DREAMS_MODEL"),
		organizeModel: anthropic.ModelFromEnvWithDefault("DREAMS_ORGANIZE_MODEL", anthropic.SonnetModel),
	}
}

type hubDreamContext struct {
	hub                *model.Hub
	settings           map[string]any
	organizeEnabled    bool
	restructureEnabled bool
}

// resolveHubContext gathers the plan + settings context for a hub and
// decides whether a dream cycle should run for it.
//
// Plan resolution matches `ListDreamableHubs`:
//
//   - Team hubs read the active `hub_subscriptions` row. If none exists,
//     the hub runs on `hub_free_team`. Owner's personal plan is NOT the
//     source of truth for team-hub billing; using it would attribute
//     free owners' paid hubs as free, and vice-versa.
//   - Personal hubs still read the owner's scoped `personal_plan_id`
//     (falling back to the legacy `users.plan` column when unset).
//
// Freeze gating: if the hub has an active subscription that's over-limit
// past the grace window, or whose status is no longer "active", the
// cycle is skipped with a `status='skipped'` receipt. This mirrors what
// push/recall already do for frozen hubs — dreams shouldn't be the one
// path that keeps burning budget while billing is paused.
//
// Personal-hub user preferences (`dreams_enabled`) are still honored.
// Per-hub phase settings for team hubs are a separate workstream
// (docs/engineering/dreams-engine-remediation-plan.md — B4).
func (e *Engine) resolveHubContext(hubID string) (*hubDreamContext, *model.DreamRun, error) {
	ctx := context.Background()
	hub, err := e.store.GetHub(hubID)
	if err != nil {
		return nil, nil, fmt.Errorf("get hub: %w", err)
	}

	var (
		planID   string
		sub      *model.HubSubscription
		settings map[string]any
	)

	switch hub.HubType {
	case "team":
		// Two lookups, two concerns:
		//   - GetActiveHubSubscription (status='active') drives plan
		//     entitlement resolution. A cancelled/past_due row gives
		//     no entitlement; the hub falls back to hub_free_team.
		//   - GetHubSubscriptionAnyStatus surfaces cancelled/past_due
		//     rows so the freeze gate below can see them. The
		//     active-only query would mask them as nil and let
		//     dreams keep running on a cancelled hub.
		activeSub, _ := e.store.GetActiveHubSubscription(ctx, hubID)
		if activeSub != nil {
			planID = activeSub.PlanID
		} else {
			planID = model.HubFreeTeamPlanID
		}
		sub, _ = e.store.GetHubSubscriptionAnyStatus(ctx, hubID)
	default:
		user, userErr := e.store.GetUser(hub.OwnerID)
		if userErr != nil {
			return nil, nil, fmt.Errorf("get hub owner: %w", userErr)
		}
		planID = user.PersonalPlanID
		if planID == "" {
			planID = user.Plan
		}
	}

	// Both hub types resolve dream settings from hub.Settings alone
	// (migration 018 backfilled personal hubs' phase keys from user
	// preferences, collapsing the old two-layer personal model). The
	// UI now writes every phase toggle — personal OR team — through
	// PATCH /v1/hubs/{id}/settings, so the engine has one source of
	// truth per hub.
	settings = model.MergedHubDreamSettings(hub)

	var limits model.UserLimits
	if e.plans != nil {
		limits = e.plans.GetUserLimits(ctx, hub.OwnerID, planID)
	} else {
		// Pre-resolver era: assume dreams are allowed. Used only by
		// tests that haven't wired a plan registry.
		limits.PlanID = planID
		limits.DreamsEnabled = true
	}

	if skipReport, skipped := evaluateDreamEligibility(sub, limits, settings, time.Now()); skipped {
		return &hubDreamContext{
				hub:                hub,
				settings:           settings,
				organizeEnabled:    true,
				restructureEnabled: true,
			}, &model.DreamRun{
				ID:      generateID(),
				OwnerID: hub.OwnerID,
				HubID:   hub.ID,
				Status:  model.DreamRunStatusSkipped,
				Report:  skipReport,
			}, nil
	}

	return &hubDreamContext{
		hub:                hub,
		settings:           settings,
		organizeEnabled:    true,
		restructureEnabled: true,
	}, nil, nil
}

// evaluateDreamEligibility encodes the skip policy. Pure — no I/O — so
// it's table-testable. Returns a short, user-surfaceable sentence
// explaining why the cycle was skipped, and a bool indicating whether
// the cycle should proceed.
//
// Precedence matches what the user perceives as the "loudest" reason:
//  1. Hub frozen (billing action required) — overrides everything.
//  2. Plan doesn't permit dreams — product/entitlement.
//  3. User explicitly disabled dreams — user preference.
func evaluateDreamEligibility(sub *model.HubSubscription, limits model.UserLimits, settings map[string]any, now time.Time) (string, bool) {
	if model.IsHubFrozen(sub, now) {
		return "Dreams paused — hub is frozen. The owner needs to resolve the over-limit state or subscription status.", true
	}
	if !limits.DreamsEnabled {
		display := limits.PlanDisplayName
		if display == "" {
			display = limits.PlanID
		}
		return fmt.Sprintf("Dreams require a plan with dreams enabled (current: %s).", display), true
	}
	if enabled, ok := settings["dreams_enabled"].(bool); ok && !enabled {
		return "Dreams disabled by user settings.", true
	}
	return "", false
}

func (e *Engine) shouldUseBootstrapDreamMode(hubID string) bool {
	unassigned, err := e.store.ListUnassignedMemoriesByHub(hubID, bootstrapOrganizeThreshold)
	if err != nil {
		slog.Warn("dream: failed to evaluate bootstrap mode", "hub_id", hubID, "error", err) // no ctx in scope here
		return false
	}
	return len(unassigned) >= bootstrapOrganizeThreshold
}

func (e *Engine) resolveExecutionConfig(hubID string) dreamExecutionConfig {
	if e.shouldUseBootstrapDreamMode(hubID) {
		return bootstrapDreamConfig()
	}
	return maintenanceDreamConfig()
}

// Run executes a full dream cycle for a hub. Personal hubs use the owner's
// personal plan and preferences. Team hubs use the hub's own plan and shared
// defaults. Team organize/restructure stays disabled until topics are fully
// shared by hub rather than partially owner-scoped.
func (e *Engine) Run(ctx context.Context, hubID string) (*model.DreamRun, error) {
	return e.RunForActor(ctx, hubID, "")
}

// RunForActor executes a dream cycle. ctx is honored for cancellation
// AND propagates the slog-ctx attrs set by the River worker middleware
// (job_id / job_kind / job_attempt), so every phase log under a dream
// cycle in Loki can be filtered by that job id.
func (e *Engine) RunForActor(ctx context.Context, hubID string, actorID string) (*model.DreamRun, error) {
	hubCtx, skippedRun, err := e.resolveHubContext(hubID)
	if err != nil {
		return nil, err
	}
	if skippedRun != nil {
		return skippedRun, nil
	}

	settings := hubCtx.settings
	execCfg := e.resolveExecutionConfig(hubCtx.hub.ID)
	run := &model.DreamRun{
		ID:           generateID(),
		OwnerID:      hubCtx.hub.OwnerID,
		HubID:        hubCtx.hub.ID,
		Mode:         execCfg.mode,
		Status:       model.DreamRunStatusRunning,
		StartedAt:    time.Now(),
		PhaseMetrics: map[string]model.DreamPhaseMetrics{},
		PhaseBudgets: defaultDreamPhaseBudgets(execCfg),
	}
	if err := e.store.CreateDreamRun(run); err != nil {
		// Another cycle is already running for this hub. Before
		// giving up, try to reclaim a stale row: if a prior cycle
		// committed phase mutations but failed its terminal
		// UpdateDreamRun (pgbouncer blip, worker killed, etc.), the
		// `running` row is stuck and migration 012's unique index
		// would otherwise block all future cycles for this hub
		// forever.
		//
		// staleRunThreshold is wider than the 10-minute River worker
		// timeout so we cannot race a live run. If ClaimStaleDreamRun
		// reclaims anything, the second CreateDreamRun attempt
		// proceeds normally. If it doesn't, a genuine concurrent run
		// is in flight — return a `skipped` receipt (not an error),
		// because returning an error would make River retry and
		// re-collide.
		if errors.Is(err, store.ErrDreamRunConcurrent) {
			reclaimed, reclaimErr := e.store.ClaimStaleDreamRun(ctx, hubCtx.hub.ID, staleRunThreshold)
			if reclaimErr != nil {
				slog.WarnContext(ctx, "dream: ClaimStaleDreamRun failed", "hub_id", hubCtx.hub.ID, "error", reclaimErr)
			}
			if reclaimed {
				slog.InfoContext(ctx, "dream: reclaimed stale running row, retrying", "hub_id", hubCtx.hub.ID)
				if retryErr := e.store.CreateDreamRun(run); retryErr != nil {
					// If the retry also collides, a new cycle raced us
					// in between reclaim and retry. Skip cleanly.
					slog.InfoContext(ctx, "dream cycle skipped — concurrent run after reclaim", "hub_id", hubCtx.hub.ID)
					return &model.DreamRun{
						ID:      generateID(),
						OwnerID: hubCtx.hub.OwnerID,
						HubID:   hubCtx.hub.ID,
						Status:  model.DreamRunStatusSkipped,
						Report:  "Another dream cycle started for this hub before we could reclaim the stale row. Skipped.",
					}, nil
				}
			} else {
				slog.InfoContext(ctx, "dream cycle skipped — concurrent run in flight", "hub_id", hubCtx.hub.ID)
				return &model.DreamRun{
					ID:      generateID(),
					OwnerID: hubCtx.hub.OwnerID,
					HubID:   hubCtx.hub.ID,
					Status:  model.DreamRunStatusSkipped,
					Report:  "Another dream cycle is already running for this hub. Skipped.",
				}, nil
			}
		} else {
			return nil, fmt.Errorf("create dream run: %w", err)
		}
	}
	events.PublishDreamsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)

	// Capture the dream-tier quota snapshot at run start so completion
	// records the same Mode/Limit/QuotaSource the gate saw, even if a
	// rollout-phase config change or plan upgrade fires mid-run. Both
	// sides nil-safe — when quotaResolver isn't wired (legacy/test),
	// the snapshot is empty and finalizeDreamRun skips the consume.
	quotaSnap := e.captureQuotaSnapshot(ctx, hubCtx.hub)

	// Worker-side defense in depth: if hard mode is on AND the gate
	// just denied, short-circuit the run. This catches jobs that
	// were queued under cap but became over-cap by the time the
	// worker dequeued. Mark the run skipped, log
	// dream_skipped_quota, and finalize with terminal_state=
	// skipped_concurrent (which doesn't charge quota per the truth
	// table). The trigger handler already gates on enqueue;
	// without this check, hard-mode races would still execute all
	// phases and only race-lose at completion, defeating the
	// defense in depth.
	if quotaSnap.captured && quotaSnap.mode == quota.ModeHard && !quotaSnap.allowed {
		slog.InfoContext(ctx, "dream skipped: quota exhausted at worker start",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"scope", quotaSnap.scope.Type, "limit", quotaSnap.limit,
			"quota_source", quotaSnap.quotaSource)
		// Pin engine status to Skipped BEFORE finalize so
		// finalizeDreamRunWithQuota doesn't overwrite it with
		// computeFinalStatus(empty metrics)="completed". The quota
		// terminal override sets the ledger + dream_runs.terminal_state
		// to skipped_concurrent (non-counting per truth table).
		// writesReceiptOnFinalize(Skipped) is false, so no
		// "dream_run_completed" notification fires for this run —
		// users wouldn't expect a notification for a quota-skipped
		// dream.
		run.Status = model.DreamRunStatusSkipped
		run.Report = "Quota exhausted before phase execution; this run did not consume quota."
		if err := e.finalizeDreamRunWithQuota(ctx, hubCtx.hub, run, model.DreamRunCounts{},
			quotaSnap, quota.StateSkippedConcurrent); err != nil {
			// Propagate the error so River retries (the run row is
			// in 'running' state and migration 012's partial unique
			// index would block a re-run anyway, but River
			// propagation makes the failure visible to ops).
			return run, err
		}
		events.PublishDreamsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)
		return run, nil
	}

	cycleStart := time.Now()
	slog.InfoContext(ctx, "dream cycle started",
		"run_id", run.ID,
		"hub_id", hubCtx.hub.ID,
		"hub_owner_id", hubCtx.hub.OwnerID,
		"hub_type", hubCtx.hub.HubType,
		"mode", run.Mode,
	)

	// Count total memories
	_, _, totalCount, err := e.store.ListMemoriesPaginated(store.ListOptions{
		Scope: store.VisibilityScope{OwnerID: hubCtx.hub.OwnerID, HubIDs: []string{hubCtx.hub.ID}},
		HubID: hubCtx.hub.ID,
		Limit: 1,
	})
	if err != nil {
		run.Status = model.DreamRunStatusFailed
		run.FinishedAt = time.Now()
		run.Report = fmt.Sprintf("Dream failed before scan: %v", err)
		if updateErr := e.store.UpdateDreamRun(run); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist dream run failure", "run_id", run.ID, "error", updateErr)
		}
		slog.ErrorContext(ctx, "dream cycle failed — scan error",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID, "error", err,
			"elapsed_ms", time.Since(cycleStart).Milliseconds(),
		)
		events.PublishDreamsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)
		return run, err
	}
	if totalCount == 0 {
		run.Report = "No memories to consolidate."
		// No work was done — record terminal_state=no_memories per
		// the truth table so this doesn't charge against quota.
		// run.Status will still resolve to "completed" via
		// computeFinalStatus (empty metrics), which is the right
		// engine-side answer (the run finished cleanly); the
		// override pins the QUOTA-side state to no_memories.
		if err := e.finalizeDreamRunWithQuota(ctx, hubCtx.hub, run, model.DreamRunCounts{},
			quotaSnap, quota.StateNoMemories); err != nil {
			return run, err
		}
		events.PublishDreamsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)
		return run, nil
	}
	run.MemoriesScanned = totalCount
	e.store.UpdateDreamRun(run) // persist count so frontend can show "Scanning N memories"
	e.heartbeat(run.ID)         // first heartbeat — proof of life to the reclaim sweeper.

	// Fetch similar pairs once (both merge and contradiction phases use this data).
	// Using the lower threshold so we get both ranges in one query.
	//
	// A failure here suppresses merge + contradiction candidate input
	// but does not stop the cycle — archive / organize / restructure
	// can still do useful work. Record the failure under its own
	// "similarity_scan" phase key so the merge phase's later
	// PhaseMetrics assignment can't overwrite it; without this
	// signal, a DB hiccup during similarity scan would silently
	// report "completed, 0 pairs" which is indistinguishable from a
	// hub with no duplicates.
	simScanStart := time.Now()
	allPairs, err := e.store.FindSimilarMemoriesByHub(hubCtx.hub.ID, ContradictionThreshold, MaxPairsPerRun*2)
	if err != nil {
		slog.ErrorContext(ctx, "dream: similarity scan failed",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID, "error", err,
			"elapsed_ms", time.Since(simScanStart).Milliseconds(),
		)
		run.PhaseMetrics["similarity_scan"] = model.DreamPhaseMetrics{Errors: 1}
	}
	var mergePairs, contradictionPairs []model.SimilarMemoryPair
	for _, p := range allPairs {
		if p.Similarity >= SimilarityThreshold {
			mergePairs = append(mergePairs, p)
		} else {
			contradictionPairs = append(contradictionPairs, p)
		}
	}
	slog.InfoContext(ctx, "dream: similarity scan complete",
		"run_id", run.ID,
		"hub_id", hubCtx.hub.ID,
		"total_pairs", len(allPairs),
		"merge_candidates", len(mergePairs),
		"contradiction_candidates", len(contradictionPairs),
		"elapsed_ms", time.Since(simScanStart).Milliseconds(),
	)

	// Trigger evaluation. Per plan 24, every memory the engine
	// considers gets a dream_trigger_decisions row recording which
	// trigger fired (if any) and the raw signal values. Always
	// runs (the log is calibration evidence regardless of which
	// path the phases take); the returned per-memory decisions
	// drive Phase 2b's agentic routing when
	// `settings["dreams_use_agent_runtime"]` is true and the
	// engine is wired with an AgentRuntime. Best-effort: a
	// failure logs and continues with an empty decisions map, so
	// the existing single-call phases still run unimpaired.
	triggerDecisions := e.evaluateTriggers(ctx, run.ID, hubCtx.hub.ID)

	// Phase 2b dream-run shared governor (plan 24). One per cycle;
	// every routed agent.Run consumes from this budget. Soft cap
	// fires a warning, hard cap silently degrades remaining pairs
	// to single-call.
	runBudget := newDreamRunBudget()

	// Phase 1: Find and merge semantic duplicates
	var mergeCount int
	var mergeActions []model.DreamAction
	mergedMemoryIDs := map[string]bool{}
	if mergeEnabled, ok := settings["dreams_merge_enabled"].(bool); !ok || mergeEnabled {
		phaseStart := time.Now()
		slog.InfoContext(ctx, "dream: phase starting",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"phase", "merge", "candidates", len(mergePairs),
		)
		var mergeMetrics model.DreamPhaseMetrics
		mergeCount, mergeActions, mergedMemoryIDs, mergeMetrics = e.phaseMergeDuplicates(ctx, hubCtx.hub.ID, run.ID, mergePairs, actorID)
		run.PhaseMetrics["merge"] = mergeMetrics
		e.meterPhase(hubCtx.hub.OwnerID, hubCtx.hub.ID, "merge", mergeMetrics)
		slog.InfoContext(ctx, "dream: phase complete",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"phase", "merge",
			"merged", mergeCount, "errors", mergeMetrics.Errors,
			"elapsed_ms", time.Since(phaseStart).Milliseconds(),
		)
	}
	run.DuplicatesMerged = mergeCount
	e.heartbeat(run.ID)

	// Phase 2: Detect contradictions (always runs — informational, doesn't modify memories)
	var contradictionCount int
	var contradictionActions []model.DreamAction
	{
		phaseStart := time.Now()
		slog.InfoContext(ctx, "dream: phase starting",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"phase", "contradiction", "candidates", len(contradictionPairs),
		)
		var contradictionMetrics model.DreamPhaseMetrics
		contradictionCount, contradictionActions, contradictionMetrics = e.phaseDetectContradictions(ctx, hubCtx.hub, run.ID, contradictionPairs, mergedMemoryIDs, actorID, settings, triggerDecisions, runBudget)
		run.PhaseMetrics["contradiction"] = contradictionMetrics
		run.ContradictionsFound = contradictionCount
		e.meterPhase(hubCtx.hub.OwnerID, hubCtx.hub.ID, "contradiction", contradictionMetrics)
		slog.InfoContext(ctx, "dream: phase complete",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"phase", "contradiction",
			"found", contradictionCount, "errors", contradictionMetrics.Errors,
			"elapsed_ms", time.Since(phaseStart).Milliseconds(),
		)
	}
	e.heartbeat(run.ID)

	// Phase 3: Archive stale memories
	var archiveCount int
	var archiveActions []model.DreamAction
	if archiveEnabled, ok := settings["dreams_archive_enabled"].(bool); !ok || archiveEnabled {
		phaseStart := time.Now()
		slog.InfoContext(ctx, "dream: phase starting",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID, "phase", "archive",
		)
		var archiveMetrics model.DreamPhaseMetrics
		archiveCount, archiveActions, archiveMetrics = e.phaseArchiveStale(ctx, hubCtx.hub.ID, run.ID)
		run.PhaseMetrics["archive"] = archiveMetrics
		e.meterPhase(hubCtx.hub.OwnerID, hubCtx.hub.ID, "archive", archiveMetrics)
		slog.InfoContext(ctx, "dream: phase complete",
			"run_id", run.ID, "hub_id", hubCtx.hub.ID,
			"phase", "archive",
			"archived", archiveCount, "errors", archiveMetrics.Errors,
			"elapsed_ms", time.Since(phaseStart).Milliseconds(),
		)
	}
	run.MemoriesArchived = archiveCount
	e.heartbeat(run.ID)

	// Phase 4: Organize unassigned memories into topics
	var organizeCount int
	var organizeActions []model.DreamAction
	if hubCtx.organizeEnabled {
		if organizeEnabled, ok := settings["dreams_organize_enabled"].(bool); !ok || organizeEnabled {
			phaseStart := time.Now()
			slog.InfoContext(ctx, "dream: phase starting",
				"run_id", run.ID, "hub_id", hubCtx.hub.ID, "phase", "organize",
			)
			var organizeMetrics model.DreamPhaseMetrics
			organizeCount, organizeActions, organizeMetrics = e.phaseOrganize(ctx, hubCtx.hub, run.ID, execCfg, actorID, settings, triggerDecisions, runBudget)
			run.PhaseMetrics["organize"] = organizeMetrics
			e.meterPhase(hubCtx.hub.OwnerID, hubCtx.hub.ID, "organize", organizeMetrics)
			slog.InfoContext(ctx, "dream: phase complete",
				"run_id", run.ID, "hub_id", hubCtx.hub.ID,
				"phase", "organize",
				"organized", organizeCount, "errors", organizeMetrics.Errors,
				"elapsed_ms", time.Since(phaseStart).Milliseconds(),
			)
		}
	}
	run.MemoriesOrganized = organizeCount
	e.heartbeat(run.ID)

	// Phase 5: Restructure topic tree (periodic — groups flat root topics into hierarchy)
	var restructureCount int
	var restructureActions []model.DreamAction
	if hubCtx.restructureEnabled {
		if restructureEnabled, ok := settings["dreams_restructure_enabled"].(bool); !ok || restructureEnabled {
			phaseStart := time.Now()
			slog.InfoContext(ctx, "dream: phase starting",
				"run_id", run.ID, "hub_id", hubCtx.hub.ID, "phase", "restructure",
			)
			var restructureMetrics model.DreamPhaseMetrics
			restructureCount, restructureActions, restructureMetrics = e.phaseRestructure(ctx, hubCtx.hub, run.ID, execCfg, actorID)
			run.PhaseMetrics["restructure"] = restructureMetrics
			e.meterPhase(hubCtx.hub.OwnerID, hubCtx.hub.ID, "restructure", restructureMetrics)
			slog.InfoContext(ctx, "dream: phase complete",
				"run_id", run.ID, "hub_id", hubCtx.hub.ID,
				"phase", "restructure",
				"restructured", restructureCount, "errors", restructureMetrics.Errors,
				"elapsed_ms", time.Since(phaseStart).Milliseconds(),
			)
		}
	}
	run.TopicsRestructured = restructureCount
	e.heartbeat(run.ID)

	// Generate dream report
	allActions := append(mergeActions, contradictionActions...)
	allActions = append(allActions, archiveActions...)
	allActions = append(allActions, organizeActions...)
	allActions = append(allActions, restructureActions...)
	run.Report = e.generateReport(run, allActions)

	if err := e.finalizeDreamRun(ctx, hubCtx.hub, run, model.DreamRunCounts{
		Merged:         mergeCount,
		Archived:       archiveCount,
		Organized:      organizeCount,
		Contradictions: contradictionCount,
		Restructures:   restructureCount,
	}, quotaSnap); err != nil {
		return run, err
	}
	events.PublishDreamsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)
	events.PublishHubMemoriesChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID)
	events.PublishTopicsChanged(ctx, e.events, hubCtx.hub.ID, hubCtx.hub.OwnerID, "")

	// Phase 3b — dream_run_completed receipt.
	//
	// The three states of dream work are handled distinctly by design:
	//
	//   - Live "dreaming now"  → dream_runs.status, consumed by a
	//                             dedicated Phase 4 run-status hook.
	//   - Completed outcome    → this notification (bar push + durable
	//                             inbox receipt, Phase 4 wires the UI).
	//   - Pending reviews      → notification summary / badge only,
	//                             NOT bar logic.
	//
	// Every successful run surfaces as a receipt, including clean runs.
	// The row is
	// addressed to the hub owner directly (audience=user) so the
	// receipt shows up even when the owner is browsing a different
	// hub. Idempotent via notifications_source_unique on
	// (source_kind='dream_run', source_id=run.id): if somehow this
	// write runs twice for the same run, the second call UPSERTs
	// the payload without creating a duplicate row.
	slog.InfoContext(ctx, "dream cycle completed",
		"run_id", run.ID,
		"hub_id", hubCtx.hub.ID,
		"hub_owner_id", hubCtx.hub.OwnerID,
		"mode", run.Mode,
		"status", string(run.Status),
		"memories_scanned", run.MemoriesScanned,
		"merged", mergeCount,
		"contradictions", contradictionCount,
		"archived", archiveCount,
		"organized", organizeCount,
		"restructured", restructureCount,
		"elapsed_ms", time.Since(cycleStart).Milliseconds(),
	)

	return run, nil
}

func sanitizeDreamReceiptReport(report string) string {
	report = strings.ToValidUTF8(strings.TrimSpace(report), "")
	if report == "" {
		return ""
	}
	const maxLen = 280
	if len(report) <= maxLen {
		return report
	}
	return strings.TrimSpace(report[:maxLen-1]) + "…"
}

// meterPhase emits a usage_events row for a completed dream phase.
// The engine calls it right after each phase so billing dashboards
// can see what dreams cost, when they ran, and which phases did
// work. Skipped phases (no LLM calls, no actions) still write a row
// so operators can see "the cycle touched this hub but the phase
// was a no-op" — zero-count rows are the happy path for a cheap
// hub.
//
// Nil meter is a valid state (tests, standalone runs); the call
// silently drops. The go-routine path inside meter.LogEvent makes
// this a fire-and-forget — no blocking and no error can surface
// back into the cycle.
// evaluateTriggers runs the per-memory trigger scan + soft-mode
// log AND returns the per-memory decisions for in-process reuse
// by Phase 2b's agentic phases.
//
// **The decision map is the routing primitive for Phase 2b.** When
// `dreams_use_agent_runtime` is enabled on a hub, the contradict
// and organize phases look up the focal memory's decision in the
// returned map — if Fired is true and the kind matches the phase's
// concern, the phase routes to agent.Run; otherwise it falls
// through to the existing single-call LLM path. Memories absent
// from the map (e.g. created mid-cycle, or the load-CTE failed)
// take the single-call path by default — fail-open is the correct
// behavior here because the agent path is opt-in and incremental.
//
// **Soft-mode log is always-on.** Even hubs that never opt into
// agentic dreams get a dream_trigger_decisions row per memory
// considered, so calibration accumulates evidence cross-hub
// independent of the rollout schedule.
//
// **Failures are non-fatal.** A load error returns an empty map
// (every phase takes the single-call path); a write error logs
// and proceeds (the in-memory map is still populated for routing).
// The calibration log is best-effort by design; missing rows in a
// noisy hub don't break the system, they just leave a small gap
// in the calibration signal.
//
// **Cost shape:** one batched read (LoadDreamTriggerInputs, single
// CTE) + one batched write (LogTriggerDecisions, single multi-row
// INSERT). For a 1000-memory hub that's 2 round-trips, not 2000.
func (e *Engine) evaluateTriggers(ctx context.Context, runID, hubID string) map[string]triggers.Decision {
	scanStart := time.Now()
	inputs, err := e.store.LoadDreamTriggerInputs(ctx, hubID, 0)
	if err != nil {
		slog.WarnContext(ctx, "dream: trigger load failed (calibration data and Phase 2b routing both missing for this run)",
			"run_id", runID, "hub_id", hubID, "error", err,
			"elapsed_ms", time.Since(scanStart).Milliseconds(),
		)
		return nil
	}
	if len(inputs) == 0 {
		return nil
	}

	cfg := triggers.DefaultConfig()
	rows := make([]store.DreamTriggerDecisionRow, 0, len(inputs))
	decisions := make(map[string]triggers.Decision, len(inputs))
	var fired int
	for _, in := range inputs {
		mem := &model.Memory{
			ID:                 in.MemoryID,
			ContentHash:        in.ContentHash,
			SourceFetchHash:    in.SourceFetchHash,
			UserFollowupMarker: in.UserFollowupMarker,
		}
		decision := cfg.Evaluate(triggers.Inputs{
			Memory:              mem,
			UserFollowupScanned: in.UserFollowupScanned,
			ContradictionCount:  in.ContradictionCount,
			TopicSiblingCount:   in.TopicSiblingCount,
		})
		decisions[in.MemoryID] = decision
		signalsJSON, jerr := json.Marshal(decision.Signals)
		if jerr != nil {
			// Marshal of a fixed shape with primitive values can
			// only fail in pathological scenarios (out of memory).
			// Skip the LOG row but keep the in-memory decision so
			// Phase 2b routing for this memory still works.
			slog.WarnContext(ctx, "dream: trigger signals marshal failed",
				"run_id", runID, "hub_id", hubID, "memory_id", in.MemoryID, "error", jerr,
			)
			continue
		}
		rows = append(rows, store.DreamTriggerDecisionRow{
			MemoryID:    in.MemoryID,
			Fired:       decision.Fired,
			Kind:        string(decision.Kind),
			SignalsJSON: signalsJSON,
		})
		if decision.Fired {
			fired++
		}
	}

	if err := e.store.LogTriggerDecisions(ctx, runID, hubID, rows); err != nil {
		slog.WarnContext(ctx, "dream: trigger decisions log failed (in-memory routing still works)",
			"run_id", runID, "hub_id", hubID, "error", err,
			"considered", len(rows),
			"elapsed_ms", time.Since(scanStart).Milliseconds(),
		)
		// Fall through: return the in-memory map so Phase 2b
		// routing still works even though calibration data
		// missed this run.
		return decisions
	}
	slog.InfoContext(ctx, "dream: trigger scan complete",
		"run_id", runID, "hub_id", hubID,
		"considered", len(rows),
		"fired", fired,
		"elapsed_ms", time.Since(scanStart).Milliseconds(),
	)
	return decisions
}

func (e *Engine) meterPhase(ownerID, hubID, phase string, metrics model.DreamPhaseMetrics) {
	if e.meter == nil {
		return
	}
	// agent_name="" matches the HTTP meter's convention for ops
	// without a real agent identity — dreams run as the system, not
	// as a specific agent. source="dreams" is the discriminator for
	// admin dashboards and rollups.
	e.meter.LogEvent(ownerID, "dream."+phase, hubID, "dreams", "", map[string]any{
		"candidates":        metrics.Candidates,
		"attempted":         metrics.Attempted,
		"processed":         metrics.Processed,
		"actions":           metrics.Actions,
		"skipped":           metrics.Skipped,
		"batches":           metrics.Batches,
		"completed_batches": metrics.CompletedBatches,
		"timed_out_batches": metrics.TimedOutBatches,
		"llm_calls":         metrics.LLMCalls,
		"llm_errors":        metrics.LLMErrors,
		"llm_timeouts":      metrics.LLMTimeouts,
		"errors":            metrics.Errors,
		"tokens_in":         metrics.TokensIn,
		"tokens_out":        metrics.TokensOut,
		"duration_ms":       metrics.DurationMs,
	})
}

// heartbeat bumps last_heartbeat_at on the running dream_runs row.
// Called at every phase boundary AND inside each LLM-backed inner
// loop so ClaimStaleDreamRun can distinguish a slow-but-alive cycle
// from one that has actually stopped calling home. Best-effort: a
// failure is logged but does not abort the cycle — missing a single
// heartbeat just widens the window before a stale reclaim would fire.
//
// Called with just a runID — the SQL UPDATE is guarded by
// `WHERE status='running'`, so a runID that never made it into
// dream_runs (the skip-receipt path on ErrDreamRunConcurrent builds
// a client-side shell) is safely a no-op.
func (e *Engine) heartbeat(runID string) {
	if runID == "" {
		return
	}
	if err := e.store.HeartbeatDreamRun(context.Background(), runID); err != nil {
		slog.Warn("dream: heartbeat failed", "run_id", runID, "error", err)
	}
}

// finalizeDreamRun promotes the run to its terminal status and then
// writes a best-effort completion receipt.
//
// The status update and the receipt notification used to share a
// single transaction. That coupling was a correctness bug: phase
// mutations (merges, archives, organizes) commit in their own
// transactions earlier in the cycle, so if the combined final tx
// failed only because of the notification insert, River would retry
// the whole cycle and double-execute those phases.
//
// Split into two commits:
//
//   - UpdateDreamRun (strict). If this fails, we return the error
//     to River. The dream_runs row stays in 'running' until
//     recovered — migration 012's partial unique index then blocks
//     a re-run on retry, surfacing the problem instead of
//     double-executing. Stale 'running' rows are an operator
//     concern (run a sweeper / bump worker timeout); they are not
//     a data-correctness concern.
//   - writeDreamReceipt (best-effort). A failure is logged but does
//     not undo the terminal status. The user misses this cycle's
//     notification; they do not get double-merged memories.
func (e *Engine) finalizeDreamRun(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	counts model.DreamRunCounts,
	quotaSnap dreamQuotaSnapshot,
) error {
	return e.finalizeDreamRunWithQuota(ctx, hub, run, counts, quotaSnap, "")
}

// finalizeDreamRunWithQuota is the shared implementation; the
// quotaTerminalOverride parameter lets the caller pin a specific
// quota.TerminalState that wouldn't be derivable from run.Status
// alone — e.g., the no-memories path goes through DreamRunStatusCompleted
// (engine-side) but should land as no_memories (quota-side, doesn't
// charge) per the truth table.
//
// Pass "" to use the default mapping (run.Status → quota state).
//
// run.Status is computed from phase metrics by default. If the
// caller already set run.Status to a terminal value before calling
// finalize (e.g. the worker hard-deny path sets Skipped), that
// pre-set value is preserved — never overwritten. Tests that
// don't set run.Status get the computed status as before.
func (e *Engine) finalizeDreamRunWithQuota(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	counts model.DreamRunCounts,
	quotaSnap dreamQuotaSnapshot,
	quotaTerminalOverride quota.TerminalState,
) error {
	if hub == nil || run == nil {
		return nil
	}
	// Only compute status from phase metrics if the caller didn't
	// already pin a terminal state. Worker-skip paths set
	// run.Status before calling finalize and must not be
	// overwritten — a quota-skipped run is NOT "completed" engine-
	// side, even though it has empty metrics.
	if !isTerminalStatus(run.Status) {
		run.Status = computeFinalStatus(run.PhaseMetrics)
	}
	run.FinishedAt = time.Now()

	if err := e.store.UpdateDreamRun(run); err != nil {
		return fmt.Errorf("persist dream run completion: %w", err)
	}

	// Quota accounting (best-effort). A failure here logs but does
	// NOT undo the terminal status — the user's run completed and
	// double-charging on retry would be worse than missing one
	// accounting row. The next nightly sweep will not re-execute
	// this run because dream_run_completions has the run_id (or
	// doesn't, in the failure case — but the engine never retries
	// terminal_state).
	e.recordQuotaConsumption(ctx, hub, run, quotaSnap, quotaTerminalOverride)

	// dream_run_completed receipt: write only for runs that did
	// real work. Skipped + failed runs don't produce a user-visible
	// "memax dreamed last night" notification — that would be
	// misleading for a quota-skipped or freeze-skipped run.
	if writesReceiptOnFinalize(run.Status) {
		if err := e.writeDreamReceipt(hub, run, counts); err != nil {
			slog.Warn("dream: failed to write completion receipt",
				"run_id", run.ID, "hub_id", hub.ID, "error", err)
		}
	}
	return nil
}

// isTerminalStatus returns true for any DreamRunStatus value the
// engine treats as "the run is done." Used by finalize to detect
// caller-set status so it isn't overwritten.
func isTerminalStatus(s string) bool {
	switch s {
	case model.DreamRunStatusCompleted,
		model.DreamRunStatusPartialFailed,
		model.DreamRunStatusFailed,
		model.DreamRunStatusSkipped:
		return true
	}
	return false
}

// writesReceiptOnFinalize returns true for runs that did meaningful
// work and deserve a user-visible "memax dreamed last night"
// notification. Skipped/failed runs are recorded for audit but
// don't produce a receipt — the notification would be misleading.
func writesReceiptOnFinalize(s string) bool {
	switch s {
	case model.DreamRunStatusCompleted, model.DreamRunStatusPartialFailed:
		return true
	}
	return false
}

// dreamQuotaSnapshot is the gate-time decision frozen at run start
// and threaded through to finalizeDreamRun. Empty (zero value) when
// the engine has no resolver wired.
type dreamQuotaSnapshot struct {
	scope       quota.Scope
	mode        quota.Mode
	tier        quota.Tier // gate-time tier (basic or lucid umbrella)
	limit       int
	quotaSource string
	periodStart time.Time
	periodEnd   time.Time
	// allowed records the gate-time decision so the worker can
	// short-circuit hard-mode denial before running phases. The
	// trigger handler already ran an Allowed check, but the worker
	// re-check is defense in depth: a job queued under cap may
	// become over-cap by the time the worker dequeues it.
	allowed bool
	// captured tracks whether captureQuotaSnapshot ran successfully.
	// We use this to distinguish "no resolver wired" from "resolver
	// wired but failed" so the consume side can log the right thing.
	captured bool
}

// captureQuotaSnapshot resolves the scope + decision at run start and
// returns a frozen snapshot for completion. Nil-safe: when the engine
// has no resolver, returns the zero snapshot and the consume side
// silently skips.
//
// Errors are logged but not propagated — failure to capture the
// snapshot should not block the dream cycle from running. The
// downside is the run won't be charged against quota; the upside is
// users don't see a 402-equivalent during a transient resolver issue.
func (e *Engine) captureQuotaSnapshot(ctx context.Context, hub *model.Hub) dreamQuotaSnapshot {
	if e.quotaResolver == nil || e.quotaStore == nil {
		return dreamQuotaSnapshot{}
	}
	if hub == nil {
		return dreamQuotaSnapshot{}
	}
	// Use the same SelectScope rule the trigger handler uses.
	// Nightly sweeps and manual triggers both land here; for a
	// nightly run on a personal hub, the actor is the owner.
	scope, err := quota.SelectScope(e.store, hub.ID, hub.OwnerID)
	if err != nil {
		slog.WarnContext(ctx, "dream quota: scope selection failed at run start",
			"hub_id", hub.ID, "error", err)
		return dreamQuotaSnapshot{}
	}

	// Gate-time decision. e.quotaStore implements DreamUsageStore
	// directly via the dreamQuotaStore union — no runtime cast.
	decision, err := quota.CheckDreamQuota(ctx, e.quotaResolver, e.quotaStore, scope, e.quotaMode)
	if err != nil {
		slog.WarnContext(ctx, "dream quota: check failed at run start",
			"hub_id", hub.ID, "scope", scope.Type, "error", err)
		return dreamQuotaSnapshot{}
	}

	return dreamQuotaSnapshot{
		scope:       scope,
		mode:        decision.Mode,
		tier:        decision.Tier,
		limit:       decision.Limit,
		quotaSource: decision.QuotaSource,
		periodStart: decision.PeriodStart,
		periodEnd:   decision.PeriodEnd,
		allowed:     decision.Allowed,
		captured:    true,
	}
}

// recordQuotaConsumption writes the dream_run_completions ledger row
// (idempotent), increments the counter (guarded in hard mode,
// unguarded in soft + writes a signal row), and mirrors the
// terminal_state to dream_runs.
//
// Best-effort: a failure here logs but does NOT propagate. The dream
// run is already terminal in dream_runs at this point; re-trying
// the whole cycle would double-execute phase mutations, which is
// worse than missing one accounting row.
func (e *Engine) recordQuotaConsumption(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	snap dreamQuotaSnapshot,
	override quota.TerminalState,
) {
	if !snap.captured {
		// No resolver wired (legacy/dev) or capture failed. The
		// captureQuotaSnapshot call already logged a warn; we don't
		// double-log here.
		return
	}
	if e.quotaStore == nil {
		return
	}

	// Determine terminal state. Caller-supplied override wins so
	// no-memories (engine status=completed but quota state=no_memories)
	// and worker-skip (status=skipped + state=skipped_concurrent)
	// can be expressed precisely. Otherwise translate run.Status
	// per the truth table.
	var terminal quota.TerminalState
	if override != "" {
		terminal = override
	} else {
		switch run.Status {
		case model.DreamRunStatusCompleted:
			terminal = quota.StateCompleted
		case model.DreamRunStatusPartialFailed:
			terminal = quota.StatePartialFailed
		case model.DreamRunStatusFailed:
			terminal = quota.StateFailed
		case model.DreamRunStatusSkipped:
			terminal = quota.StateSkippedConcurrent
		default:
			// Unknown terminal status. Treat as failed-non-counting so
			// we don't accidentally charge quota for an unmapped state.
			slog.WarnContext(ctx, "dream quota: unknown terminal status; treating as failed",
				"run_id", run.ID, "status", run.Status)
			terminal = quota.StateFailed
		}
	}

	// Tier comes from the gate-time decision (snap.tier), NOT
	// inferred from the limit value. CheckDreamQuota picks Basic
	// for Free users (no Lucid quota) and Lucid umbrella for paid;
	// at completion we resolve the umbrella to lucid_passive.
	// Phase 2 (LucidLoopBudget) will upgrade lucid_active reporting
	// based on actual tool-loop iterations — until then, every
	// Lucid run is recorded as passive.
	//
	// Earlier code inferred from snap.limit > 0, which broke for
	// Free users (basic_dream_limit=40 > 0 but tier should be
	// Basic, not Lucid). The snapshot's tier is the source of
	// truth.
	tier := snap.tier
	switch tier {
	case quota.TierLucid:
		tier = quota.TierLucidPassive
	case quota.TierBasic, quota.TierLucidPassive, quota.TierLucidActive:
		// Already a valid sub-tier — keep it.
	default:
		// Unknown / zero (snapshot not captured properly). Skip
		// quota — we don't want to charge an unmapped tier.
		slog.WarnContext(ctx, "dream quota: unknown snapshot tier; skipping consume",
			"run_id", run.ID, "tier", tier)
		return
	}

	params := quota.ConsumeParams{
		DreamRunID:    run.ID,
		Scope:         snap.scope,
		Tier:          tier,
		TerminalState: terminal,
		PeriodStart:   snap.periodStart,
		PeriodEnd:     snap.periodEnd,
		Mode:          snap.mode,
		ResolvedLimit: snap.limit,
		QuotaSource:   snap.quotaSource,
	}

	result, err := quota.ConsumeDreamRun(ctx, e.quotaStore, params)
	if err != nil {
		slog.WarnContext(ctx, "dream quota: consume failed",
			"run_id", run.ID, "hub_id", hub.ID,
			"scope", snap.scope.Type, "tier", tier, "terminal", terminal,
			"error", err)
		return
	}

	if result.QuotaRaceLost {
		slog.InfoContext(ctx, "dream quota: race lost at completion (hard mode)",
			"run_id", run.ID, "hub_id", hub.ID,
			"scope", snap.scope.Type, "limit", snap.limit, "quota_source", snap.quotaSource)
		return
	}
	if result.Counted {
		slog.InfoContext(ctx, "dream quota: counted",
			"run_id", run.ID, "hub_id", hub.ID,
			"scope", snap.scope.Type, "tier", tier,
			"used_after", result.UsedAfter, "limit", snap.limit, "mode", snap.mode)
	}
}

// computeFinalStatus derives the terminal run status from per-phase
// metrics. Any phase that recorded LLM errors or timeouts demotes
// the run to partial_failed so operators can distinguish a truly
// clean run from one where (for example) organize timed out on
// every batch but merge + archive still ran.
//
// Pure — no I/O. Empty or nil metrics map returns Completed (zero-
// memory cycle, skip-phase cycle).
func computeFinalStatus(metrics map[string]model.DreamPhaseMetrics) string {
	for _, m := range metrics {
		if m.LLMErrors > 0 || m.LLMTimeouts > 0 || m.Errors > 0 {
			return model.DreamRunStatusPartialFailed
		}
	}
	return model.DreamRunStatusCompleted
}

// writeDreamReceipt creates and publishes the dream_run_completed
// notification. The row is idempotent via notifications_source_unique
// on (source_kind='dream_run', source_id=run.id): a retry that
// somehow re-runs this path upserts the payload without creating a
// duplicate.
//
// Addressed to the hub owner directly (audience=user) so the
// receipt surfaces even when they're browsing a different hub.
// Team-hub member fan-out is tracked as follow-up B3 in
// docs/engineering/dreams-engine-remediation-plan.md.
//
// Respects the recipient's notifications_enabled preference — a hub
// owner who turned notifications off in settings sees no receipt.
// The run itself is still marked terminal; we just don't deliver
// the row. Historical runs for users who later re-enable the
// setting won't retroactively appear — that's the contract
// elsewhere in the notification framework too.
func (e *Engine) writeDreamReceipt(hub *model.Hub, run *model.DreamRun, counts model.DreamRunCounts) error {
	if !userNotifiable(e.store, hub.OwnerID) {
		slog.Info("dream: receipt suppressed by recipient prefs", "run_id", run.ID, "hub_id", hub.ID, "recipient", hub.OwnerID)
		return nil
	}
	expiresAt := run.FinishedAt.Add(24 * time.Hour)
	payloadModel := model.DreamRunCompletedPayload{
		RunID:           run.ID,
		Mode:            run.Mode,
		Status:          run.Status,
		Counts:          counts,
		MemoriesScanned: run.MemoriesScanned,
		FinishedAt:      run.FinishedAt,
		ReportTL:        sanitizeDreamReceiptReport(run.Report),
	}
	payload, err := json.Marshal(payloadModel)
	if err != nil && payloadModel.ReportTL != "" {
		payloadModel.ReportTL = ""
		payload, err = json.Marshal(payloadModel)
	}
	if err != nil {
		return fmt.Errorf("marshal dream_run_completed payload: %w", err)
	}
	runID := run.ID
	notif := &model.Notification{
		ID:              generateID(),
		Audience:        model.AudienceUser,
		RecipientUserID: hub.OwnerID,
		HubID:           hub.ID,
		Kind:            model.NotificationKindDreamRunCompleted,
		Status:          model.NotificationStatusPending,
		Priority:        0,
		SourceKind:      model.NotificationSourceDreamRun,
		SourceID:        run.ID,
		DreamRunID:      &runID,
		Payload:         payload,
		CreatedAt:       run.FinishedAt,
		ExpiresAt:       &expiresAt,
	}
	if err := e.txRunner.WithTx(context.Background(), func(tx pgx.Tx) error {
		return e.txRunner.CreateNotificationTx(context.Background(), tx, notif)
	}); err != nil {
		return fmt.Errorf("create dream receipt: %w", err)
	}
	events.PublishNotificationCreated(context.Background(), e.events, notif)
	return nil
}

// phaseMergeDuplicates merges semantically similar memory pairs.
