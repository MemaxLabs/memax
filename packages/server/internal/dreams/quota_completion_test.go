package dreams_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/dreams"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// stubResolver bypasses the full planresolver stack so we can drive
// the engine's quota path end-to-end without a Redis cache or
// active hub subscription. The engine's contract for the resolver
// is just "give me dream limits"; this test stub answers directly.
type stubResolver struct {
	user planresolver.DreamLimits
	hub  planresolver.DreamLimits
}

func (s stubResolver) ResolveDreamLimits(_ context.Context, _ string) planresolver.DreamLimits {
	return s.user
}
func (s stubResolver) GetHubDreamLimits(_ context.Context, _ string) planresolver.DreamLimits {
	return s.hub
}

// TestEngineCompletion_WritesQuotaCompletion is the smallest
// end-to-end test for the engine→ConsumeDreamRun wiring. We bypass
// RunForActor (which runs the full pipeline + LLM stubs) and
// directly construct a finalize-shaped scenario: insert a
// dream_runs row, manually invoke the engine's quota path the way
// finalizeDreamRun would, then assert the ledger row + counter
// landed.
//
// This is a contract test for "engine wiring threads the snapshot
// correctly." Full-cycle tests for the engine's pipeline already
// live in integration_test.go.
func TestEngineCompletion_WritesQuotaCompletion(t *testing.T) {
	s, pool := testdb.Acquire(t)

	ctx := context.Background()
	ownerID := uuid.NewString()
	hubID := uuid.NewString()

	// Seed user + team hub. Team hubs charge ScopeHub; the engine's
	// captureQuotaSnapshot path reads hub.HubType to pick the scope.
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, 'engine-quota@test.local', 'engine quota test', 'personal_pro', 'personal_pro', true, now(), now())`,
		ownerID,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'Engine Quota Hub', $2, 'team', $3::uuid, now(), now())`,
		hubID, "engine-quota-"+hubID[:8], ownerID,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	// Seed a completed dream_runs row that ConsumeDreamRun will
	// reference via its FK. status='completed' avoids the partial
	// unique index on running rows.
	runID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at, triggered_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', now(), now(), 'manual:test')`,
		runID, ownerID, hubID,
	)
	if err != nil {
		t.Fatalf("seed dream_runs: %v", err)
	}

	// Construct the engine. We pass nil for the LLM client + embedder
	// since this test never invokes phases — only the quota path.
	// The resolver returns a Team-tier hub limit; the consume path
	// must charge ScopeHub at that limit.
	registry := plans.NewRegistry(s)
	_ = registry.Load(ctx)
	engine := dreams.NewForTesting(s, registry)
	if engine == nil {
		t.Fatal("dreams.NewForTesting returned nil; check testing helper")
	}
	engine.SetQuota(stubResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	}, s)
	engine.SetQuotaMode(quota.ModeSoft)

	// Drive the engine's recordQuotaConsumption path via the public
	// FinalizeForTesting hook (added in this same commit so tests
	// can verify the wiring without running the full RunForActor).
	hub := &model.Hub{ID: hubID, OwnerID: ownerID, HubType: "team"}
	run := &model.DreamRun{
		ID:      runID,
		OwnerID: ownerID,
		HubID:   hubID,
		Status:  model.DreamRunStatusCompleted,
	}
	periodStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)
	if err := engine.FinalizeForTesting(ctx, hub, run, periodStart, periodEnd); err != nil {
		t.Fatalf("FinalizeForTesting: %v", err)
	}

	// Verify: ledger row exists, hub_usage incremented to 1, and
	// soft-mode signal row is present with the right snapshot.
	var ledgerCounted bool
	var ledgerTerminal string
	if err := pool.QueryRow(ctx,
		`SELECT counted, terminal_state FROM dream_run_completions WHERE dream_run_id = $1::uuid`,
		runID,
	).Scan(&ledgerCounted, &ledgerTerminal); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !ledgerCounted {
		t.Errorf("ledger row counted=false, want true")
	}
	if ledgerTerminal != "completed" {
		t.Errorf("ledger terminal_state=%q, want completed", ledgerTerminal)
	}

	var hubLucid int
	_ = pool.QueryRow(ctx,
		`SELECT lucid_dream_count FROM hub_usage WHERE hub_id = $1::uuid AND period_start = $2`,
		hubID, periodStart,
	).Scan(&hubLucid)
	if hubLucid != 1 {
		t.Errorf("hub_usage.lucid_dream_count = %d, want 1", hubLucid)
	}

	var signalLimit, signalUsedAfter int
	var signalQuotaSource, signalMode string
	var wouldHaveBlocked bool
	if err := pool.QueryRow(ctx,
		`SELECT resolved_limit, used_after, quota_source, mode, would_have_blocked
		 FROM dream_quota_signals WHERE dream_run_id = $1::uuid`,
		runID,
	).Scan(&signalLimit, &signalUsedAfter, &signalQuotaSource, &signalMode, &wouldHaveBlocked); err != nil {
		t.Fatalf("read signal: %v", err)
	}
	if signalLimit != 100 || signalUsedAfter != 1 || signalQuotaSource != "hub_team" || signalMode != "soft" || wouldHaveBlocked {
		t.Errorf("signal row mismatch: limit=%d used_after=%d source=%q mode=%q wouldBlock=%v",
			signalLimit, signalUsedAfter, signalQuotaSource, signalMode, wouldHaveBlocked)
	}
}

// TestEngineCompletion_NoMemoriesDoesNotCount locks in the fix for
// codex finding High #1: a hub with zero memories goes through
// finalizeDreamRun with run.Status=completed, but the QUOTA
// terminal must be no_memories so the run does NOT charge against
// quota. Without this, an empty hub on a nightly sweep would
// drain its Lucid pool every day for doing nothing.
func TestEngineCompletion_NoMemoriesDoesNotCount(t *testing.T) {
	s, pool := testdb.Acquire(t)

	ctx := context.Background()
	ownerID := uuid.NewString()
	hubID := uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, 'no-mem@test.local', 'no mem test', 'personal_pro', 'personal_pro', true, now(), now())`,
		ownerID,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'No Mem Hub', $2, 'team', $3::uuid, now(), now())`,
		hubID, "no-mem-"+hubID[:8], ownerID,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	runID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at, triggered_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', now(), now(), 'manual:test')`,
		runID, ownerID, hubID,
	)
	if err != nil {
		t.Fatalf("seed dream_runs: %v", err)
	}

	registry := plans.NewRegistry(s)
	_ = registry.Load(ctx)
	engine := dreams.NewForTesting(s, registry)
	engine.SetQuota(stubResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	}, s)
	engine.SetQuotaMode(quota.ModeSoft)

	hub := &model.Hub{ID: hubID, OwnerID: ownerID, HubType: "team"}
	run := &model.DreamRun{
		ID:      runID,
		OwnerID: ownerID,
		HubID:   hubID,
		Status:  model.DreamRunStatusCompleted, // engine status: completed
	}
	periodStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	// Drive with explicit no_memories override (what the no-memories
	// path inside RunForActor does).
	if err := engine.FinalizeWithQuotaTerminalForTesting(ctx, hub, run, periodStart, periodEnd, quota.StateNoMemories); err != nil {
		t.Fatalf("FinalizeWithQuotaTerminalForTesting: %v", err)
	}

	// Counter must NOT have moved.
	var hubLucid int
	_ = pool.QueryRow(ctx,
		`SELECT lucid_dream_count FROM hub_usage WHERE hub_id = $1::uuid AND period_start = $2`,
		hubID, periodStart,
	).Scan(&hubLucid)
	if hubLucid != 0 {
		t.Errorf("hub_usage.lucid_dream_count = %d after no_memories run, want 0", hubLucid)
	}

	// Ledger row exists with the right state + counted=false.
	var counted bool
	var terminal string
	if err := pool.QueryRow(ctx,
		`SELECT counted, terminal_state FROM dream_run_completions WHERE dream_run_id = $1::uuid`,
		runID,
	).Scan(&counted, &terminal); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if counted {
		t.Errorf("ledger counted=true, want false for no_memories")
	}
	if terminal != "no_memories" {
		t.Errorf("ledger terminal_state=%q, want no_memories", terminal)
	}
}

// TestEngineCompletion_BasicTierUsesBasicCounter locks in the fix
// for codex finding High #2: a Free user (basic_dream_limit=40)
// must charge basic_dream_count, not lucid_dream_count.
func TestEngineCompletion_BasicTierUsesBasicCounter(t *testing.T) {
	s, pool := testdb.Acquire(t)

	ctx := context.Background()
	ownerID := uuid.NewString()
	hubID := uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, 'free@test.local', 'free user', 'personal_free', 'personal_free', true, now(), now())`,
		ownerID,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'Personal', $2, 'personal', $3::uuid, now(), now())`,
		hubID, "personal-"+hubID[:8], ownerID,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	runID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at, triggered_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', now(), now(), 'manual:test')`,
		runID, ownerID, hubID,
	)
	if err != nil {
		t.Fatalf("seed dream_runs: %v", err)
	}

	registry := plans.NewRegistry(s)
	_ = registry.Load(ctx)
	engine := dreams.NewForTesting(s, registry)
	// Free user limits — Basic 40, no Lucid.
	engine.SetQuota(stubResolver{
		user: planresolver.DreamLimits{BasicLimit: 40, BasicQuotaSource: "personal_free"},
	}, s)
	engine.SetQuotaMode(quota.ModeSoft)

	hub := &model.Hub{ID: hubID, OwnerID: ownerID, HubType: "personal"}
	run := &model.DreamRun{
		ID:      runID,
		OwnerID: ownerID,
		HubID:   hubID,
		Status:  model.DreamRunStatusCompleted,
	}
	periodStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)
	if err := engine.FinalizeForTesting(ctx, hub, run, periodStart, periodEnd); err != nil {
		t.Fatalf("FinalizeForTesting: %v", err)
	}

	var basic, lucid int
	_ = pool.QueryRow(ctx,
		`SELECT basic_dream_count, lucid_dream_count FROM usage
		 WHERE user_id = $1::uuid AND period_start = $2`,
		ownerID, periodStart,
	).Scan(&basic, &lucid)
	if basic != 1 {
		t.Errorf("basic_dream_count=%d, want 1 (Basic tier should charge basic counter)", basic)
	}
	if lucid != 0 {
		t.Errorf("lucid_dream_count=%d, want 0 (Basic tier must NOT touch lucid counter)", lucid)
	}

	// Ledger row should record tier=basic.
	var ledgerTier string
	_ = pool.QueryRow(ctx,
		`SELECT tier FROM dream_run_completions WHERE dream_run_id = $1::uuid`, runID,
	).Scan(&ledgerTier)
	if ledgerTier != "basic" {
		t.Errorf("ledger tier=%q, want basic", ledgerTier)
	}
}

// TestEngineCompletion_WorkerHardDenySkipsAndDoesNotCount locks in
// the fix for codex engine v2 finding (Medium #1): when the worker
// hard-deny short-circuit runs, finalizeDreamRunWithQuota must
// preserve the caller-set run.Status=Skipped (not overwrite it with
// computeFinalStatus(empty)="completed"), the ledger must record
// terminal=skipped_concurrent + counted=false, the hub_usage
// counter must stay at 0, and NO dream_run_completed notification
// should fire (a quota-skipped run did no work for the user).
//
// We can't drive RunForActor end-to-end here without the full
// pipeline (LLM + embedder), so we exercise the same finalize
// contract the worker hard-deny path does: pin run.Status=Skipped,
// override quota terminal to skipped_concurrent.
func TestEngineCompletion_WorkerHardDenySkipsAndDoesNotCount(t *testing.T) {
	s, pool := testdb.Acquire(t)

	ctx := context.Background()
	ownerID := uuid.NewString()
	hubID := uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, 'worker-skip@test.local', 'worker skip test', 'personal_pro', 'personal_pro', true, now(), now())`,
		ownerID,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'Worker Skip Hub', $2, 'team', $3::uuid, now(), now())`,
		hubID, "worker-skip-"+hubID[:8], ownerID,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	runID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at, triggered_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', now(), now(), 'manual:test')`,
		runID, ownerID, hubID,
	)
	if err != nil {
		t.Fatalf("seed dream_runs: %v", err)
	}

	registry := plans.NewRegistry(s)
	_ = registry.Load(ctx)
	engine := dreams.NewForTesting(s, registry)
	engine.SetQuota(stubResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	}, s)
	engine.SetQuotaMode(quota.ModeHard)

	hub := &model.Hub{ID: hubID, OwnerID: ownerID, HubType: "team"}
	// Caller pins Skipped before finalize — exactly what the worker
	// hard-deny short-circuit does in RunForActor.
	run := &model.DreamRun{
		ID:      runID,
		OwnerID: ownerID,
		HubID:   hubID,
		Status:  model.DreamRunStatusSkipped,
		Report:  "Quota exhausted before phase execution; this run did not consume quota.",
	}
	periodStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	if err := engine.FinalizeWithQuotaTerminalForTesting(ctx, hub, run, periodStart, periodEnd, quota.StateSkippedConcurrent); err != nil {
		t.Fatalf("FinalizeWithQuotaTerminalForTesting: %v", err)
	}

	// 1. dream_runs.status must remain 'skipped' — finalize must NOT
	//    have overwritten it with computeFinalStatus(empty)="completed".
	var dreamRunStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM dream_runs WHERE id = $1::uuid`, runID,
	).Scan(&dreamRunStatus); err != nil {
		t.Fatalf("read dream_runs.status: %v", err)
	}
	if dreamRunStatus != model.DreamRunStatusSkipped {
		t.Errorf("dream_runs.status = %q, want %q (finalize must preserve caller-set Skipped)",
			dreamRunStatus, model.DreamRunStatusSkipped)
	}

	// 2. Ledger row exists with terminal=skipped_concurrent, counted=false.
	var ledgerCounted bool
	var ledgerTerminal string
	if err := pool.QueryRow(ctx,
		`SELECT counted, terminal_state FROM dream_run_completions WHERE dream_run_id = $1::uuid`,
		runID,
	).Scan(&ledgerCounted, &ledgerTerminal); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if ledgerCounted {
		t.Errorf("ledger counted=true, want false for skipped_concurrent")
	}
	if ledgerTerminal != "skipped_concurrent" {
		t.Errorf("ledger terminal_state=%q, want skipped_concurrent", ledgerTerminal)
	}

	// 3. hub_usage counter must NOT have moved.
	var hubLucid int
	_ = pool.QueryRow(ctx,
		`SELECT lucid_dream_count FROM hub_usage WHERE hub_id = $1::uuid AND period_start = $2`,
		hubID, periodStart,
	).Scan(&hubLucid)
	if hubLucid != 0 {
		t.Errorf("hub_usage.lucid_dream_count = %d after worker-skip, want 0", hubLucid)
	}

	// 4. NO dream_run_completed notification fires for a quota-skipped
	//    run — that would mislead the user into thinking memax dreamed
	//    when it actually did nothing.
	var notifCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE hub_id = $1::uuid AND kind = 'dream_run_completed' AND source_id = $2`,
		hubID, runID,
	).Scan(&notifCount); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if notifCount != 0 {
		t.Errorf("dream_run_completed notifications = %d, want 0 for worker-skip", notifCount)
	}
}
