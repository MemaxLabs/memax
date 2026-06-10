package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// Tests for ConsumeDreamRun and the GetDreamUsage primitives.
// Schema spec: docs/plans/23-dream-tiers.md.
//
// Tests are gated on testdb.Acquire (real Postgres). On a machine
// without postgres reachable, the tests skip silently — `go test
// ./...` stays green. CI runs them against a real DB.

// fixtures bundles the seeded user/hub/dream-run UUIDs every test
// reuses. Created fresh per test via setupFixtures so each gets an
// isolated cloned database.
type fixtures struct {
	ctx         context.Context
	store       store.Store
	pool        *pgxpool.Pool
	ownerID     string
	hubID       string
	periodStart time.Time
	periodEnd   time.Time
}

func setupFixtures(t *testing.T) *fixtures {
	t.Helper()
	s, pool := testdb.Acquire(t)

	ctx := context.Background()
	ownerID := uuid.NewString()
	hubID := uuid.NewString()

	// Seed a user so the user-scope tests can FK-key against it.
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, 'personal_pro', 'personal_pro', true, now(), now())`,
		ownerID, "quota-test-"+ownerID+"@example.test", "Quota Test User",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Seed a hub for the hub-scope tests.
	_, err = pool.Exec(ctx, `
		INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, 'Quota Test Hub', $2, 'team', $3::uuid, now(), now())`,
		hubID, "quota-test-"+hubID, ownerID,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}

	// Calendar-month period.
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	return &fixtures{
		ctx:         ctx,
		store:       s,
		pool:        pool,
		ownerID:     ownerID,
		hubID:       hubID,
		periodStart: periodStart,
		periodEnd:   periodEnd,
	}
}

// seedDreamRun creates a row in dream_runs we can reference from
// dream_run_completions / dream_quota_signals via FK.
//
// Uses status='completed' (not 'running') because there is a
// partial unique index `idx_dream_runs_one_active_per_hub` that
// prevents multiple in-flight runs per hub. The consume primitive
// only cares that the FK row exists; status doesn't drive its logic.
func (f *fixtures) seedDreamRun(t *testing.T) string {
	t.Helper()
	runID := uuid.NewString()
	_, err := f.pool.Exec(f.ctx, `
		INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at, triggered_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', now(), now(), 'manual:'||$2::text)`,
		runID, f.ownerID, f.hubID,
	)
	if err != nil {
		t.Fatalf("seed dream_runs: %v", err)
	}
	return runID
}

// userParams returns ConsumeParams for a user-scope run with the
// given tier, terminal state, mode, and resolved limit.
func (f *fixtures) userParams(runID string, tier, terminal, mode string, limit int) model.DreamConsumeParams {
	return model.DreamConsumeParams{
		DreamRunID:    runID,
		ScopeType:     "user",
		ScopeID:       f.ownerID,
		Tier:          tier,
		TerminalState: terminal,
		PeriodStart:   f.periodStart,
		PeriodEnd:     f.periodEnd,
		Mode:          mode,
		ResolvedLimit: limit,
		QuotaSource:   "personal_pro",
	}
}

// hubParams is the hub-scope equivalent.
func (f *fixtures) hubParams(runID string, tier, terminal, mode string, limit int) model.DreamConsumeParams {
	return model.DreamConsumeParams{
		DreamRunID:    runID,
		ScopeType:     "hub",
		ScopeID:       f.hubID,
		Tier:          tier,
		TerminalState: terminal,
		PeriodStart:   f.periodStart,
		PeriodEnd:     f.periodEnd,
		Mode:          mode,
		ResolvedLimit: limit,
		QuotaSource:   "hub_team",
	}
}

// ─────────────────────────────────────────────────────────────────
// Happy path
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_UserHardCompleted(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	r, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_passive", "completed", "hard", 40))
	if err != nil {
		t.Fatalf("ConsumeDreamRun: %v", err)
	}
	if !r.Counted {
		t.Fatalf("expected Counted=true, got %+v", r)
	}
	if r.QuotaRaceLost {
		t.Fatalf("unexpected QuotaRaceLost on a non-conflicting run")
	}
	if r.UsedAfter != 1 {
		t.Errorf("UsedAfter=%d, want 1", r.UsedAfter)
	}

	// Verify the counter actually moved.
	_, lucid, err := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if err != nil {
		t.Fatalf("GetUserDreamUsage: %v", err)
	}
	if lucid != 1 {
		t.Errorf("lucid count after consume = %d, want 1", lucid)
	}
}

func TestConsumeDreamRun_HubHardCompleted(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	r, err := f.store.ConsumeDreamRun(f.ctx,
		f.hubParams(runID, "lucid_active", "completed", "hard", 100))
	if err != nil {
		t.Fatalf("ConsumeDreamRun: %v", err)
	}
	if !r.Counted || r.UsedAfter != 1 {
		t.Errorf("hub consume result = %+v, want Counted=true UsedAfter=1", r)
	}
}

// ─────────────────────────────────────────────────────────────────
// Idempotency: same run_id called twice returns the prior state.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_Idempotent(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	first, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_passive", "completed", "hard", 40))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_passive", "completed", "hard", 40))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	// Both report the same counted state and used_after; counter
	// only moved once.
	if !first.Counted || !second.Counted {
		t.Errorf("expected both counted=true; first=%+v second=%+v", first, second)
	}
	if first.UsedAfter != 1 || second.UsedAfter != 1 {
		t.Errorf("UsedAfter mismatch; first=%d second=%d (counter should move only once)", first.UsedAfter, second.UsedAfter)
	}
	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != 1 {
		t.Errorf("counter incremented %d times, want 1 (idempotency broken)", lucid)
	}
}

// ─────────────────────────────────────────────────────────────────
// Edge case: limit=0 short-circuits (no counter touched, no signal).
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_LimitZeroShortCircuits(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	r, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "basic", "completed", "hard", 0))
	if err != nil {
		t.Fatalf("ConsumeDreamRun: %v", err)
	}
	if r.Counted {
		t.Errorf("limit=0 must not count; got %+v", r)
	}

	// Counter should be zero (no insert at all).
	basic, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if basic != 0 || lucid != 0 {
		t.Errorf("counter touched on limit=0; basic=%d lucid=%d", basic, lucid)
	}

	// Ledger row should still exist for audit.
	var ledgerCount int
	_ = f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM dream_run_completions WHERE dream_run_id = $1::uuid`, runID,
	).Scan(&ledgerCount)
	if ledgerCount != 1 {
		t.Errorf("ledger rows = %d, want 1 (record-without-charge)", ledgerCount)
	}
}

// ─────────────────────────────────────────────────────────────────
// Edge case: limit=-1 (unlimited) never refuses.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_UnlimitedNeverRefuses(t *testing.T) {
	f := setupFixtures(t)
	for i := 0; i < 5; i++ {
		runID := f.seedDreamRun(t)
		r, err := f.store.ConsumeDreamRun(f.ctx,
			f.userParams(runID, "lucid_active", "completed", "hard", -1))
		if err != nil {
			t.Fatalf("run %d ConsumeDreamRun: %v", i, err)
		}
		if !r.Counted {
			t.Fatalf("run %d: limit=-1 should always count; got %+v", i, r)
		}
		if r.QuotaRaceLost {
			t.Fatalf("run %d: unlimited should never lose race", i)
		}
	}
	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != 5 {
		t.Errorf("after 5 unlimited consumes, count=%d, want 5", lucid)
	}
}

// ─────────────────────────────────────────────────────────────────
// THE LOAD-BEARING TEST: hard-mode race-loss with concurrent
// completions at the cap. Two parallel runs both consume against a
// limit=1 budget; exactly one wins, the other is reclassified
// quota_race_lost. Counter never exceeds 1.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_HardModeConcurrentRaceLoss(t *testing.T) {
	f := setupFixtures(t)

	const concurrency = 8
	const limit = 1

	// Seed one dream_run per worker upfront so the t.Parallel below
	// doesn't race on the seeding step.
	runIDs := make([]string, concurrency)
	for i := 0; i < concurrency; i++ {
		runIDs[i] = f.seedDreamRun(t)
	}

	results := make([]model.DreamConsumeResult, concurrency)
	errs := make([]error, concurrency)

	// Use a barrier so all goroutines hit the SQL at roughly the
	// same instant — maximizes the chance of a real race against
	// the unique-key serialization point.
	var startBarrier sync.WaitGroup
	startBarrier.Add(1)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			startBarrier.Wait()
			results[i], errs[i] = f.store.ConsumeDreamRun(f.ctx,
				f.userParams(runIDs[i], "lucid_passive", "completed", "hard", limit))
		}(i)
	}
	startBarrier.Done()
	wg.Wait()

	// Tally outcomes.
	var counted, raceLost int
	for i, r := range results {
		if errs[i] != nil {
			t.Errorf("worker %d: %v", i, errs[i])
		}
		if r.Counted {
			counted++
		}
		if r.QuotaRaceLost {
			raceLost++
		}
	}

	// Exactly one must have counted; the rest must have race-lost.
	if counted != 1 {
		t.Errorf("counted=%d, want exactly 1 (cap=%d)", counted, limit)
	}
	if raceLost != concurrency-1 {
		t.Errorf("raceLost=%d, want %d", raceLost, concurrency-1)
	}

	// Counter must equal limit, not over.
	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != limit {
		t.Errorf("final lucid count=%d, want %d (counter exceeded cap)", lucid, limit)
	}

	// Each race-lost run's terminal_state must be quota_race_lost
	// in dream_run_completions (transitioned by the consume primitive).
	var raceLostInDB int
	_ = f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM dream_run_completions
		 WHERE dream_run_id = ANY($1::uuid[]) AND terminal_state = 'quota_race_lost'`,
		runIDs,
	).Scan(&raceLostInDB)
	if raceLostInDB != concurrency-1 {
		t.Errorf("dream_run_completions race-lost count=%d, want %d", raceLostInDB, concurrency-1)
	}
}

// ─────────────────────────────────────────────────────────────────
// Soft mode: over-cap allowed, signal rows record actual usage and
// would_have_blocked=true past the cap.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_SoftModeOverCapSignal(t *testing.T) {
	f := setupFixtures(t)
	const limit = 2

	// Three sequential runs: first two within cap, third over.
	for i := 0; i < 3; i++ {
		runID := f.seedDreamRun(t)
		r, err := f.store.ConsumeDreamRun(f.ctx,
			f.userParams(runID, "lucid_passive", "completed", "soft", limit))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if !r.Counted {
			t.Errorf("run %d: soft mode must count; got %+v", i, r)
		}
		if r.QuotaRaceLost {
			t.Errorf("run %d: soft mode never produces quota_race_lost; got %+v", i, r)
		}
		wantUsedAfter := i + 1
		if r.UsedAfter != wantUsedAfter {
			t.Errorf("run %d: UsedAfter=%d, want %d", i, r.UsedAfter, wantUsedAfter)
		}
	}

	// All three signal rows should exist; the third should have
	// would_have_blocked=true (used_after=3 > limit=2).
	type signalRow struct {
		usedAfter        int
		wouldHaveBlocked bool
	}
	rows, err := f.pool.Query(f.ctx,
		`SELECT used_after, would_have_blocked FROM dream_quota_signals
		 WHERE scope_id = $1::uuid AND mode = 'soft'
		 ORDER BY used_after`,
		f.ownerID,
	)
	if err != nil {
		t.Fatalf("query signals: %v", err)
	}
	defer rows.Close()
	var got []signalRow
	for rows.Next() {
		var r signalRow
		if err := rows.Scan(&r.usedAfter, &r.wouldHaveBlocked); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("signal rows=%d, want 3", len(got))
	}
	wantBlocked := []bool{false, false, true} // used_after 1, 2, 3 vs limit=2
	for i, sr := range got {
		if sr.wouldHaveBlocked != wantBlocked[i] {
			t.Errorf("signal %d: would_have_blocked=%v, want %v (used_after=%d, limit=%d)",
				i, sr.wouldHaveBlocked, wantBlocked[i], sr.usedAfter, limit)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// Non-counting terminal states: "failed", "skipped_concurrent",
// "no_memories" record the run without charging the counter.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_NonCountingTerminalStates(t *testing.T) {
	f := setupFixtures(t)

	for _, terminal := range []string{"failed", "skipped_concurrent", "no_memories"} {
		t.Run(terminal, func(t *testing.T) {
			runID := f.seedDreamRun(t)
			r, err := f.store.ConsumeDreamRun(f.ctx,
				f.userParams(runID, "lucid_passive", terminal, "hard", 40))
			if err != nil {
				t.Fatalf("ConsumeDreamRun: %v", err)
			}
			if r.Counted {
				t.Errorf("terminal=%q must not count; got %+v", terminal, r)
			}
			// Ledger row exists (record without charge).
			var ledgerExists int
			_ = f.pool.QueryRow(f.ctx,
				`SELECT count(*) FROM dream_run_completions WHERE dream_run_id = $1::uuid`, runID,
			).Scan(&ledgerExists)
			if ledgerExists != 1 {
				t.Errorf("ledger rows = %d, want 1", ledgerExists)
			}
		})
	}

	// Counter must still be zero after all those non-counting runs.
	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != 0 {
		t.Errorf("counter incremented on non-counting states; lucid=%d", lucid)
	}
}

// ─────────────────────────────────────────────────────────────────
// loop_cap_hit IS counted (the run did meaningful work up to the cap).
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_LoopCapHitCounts(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	r, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_active", "loop_cap_hit", "hard", 40))
	if err != nil {
		t.Fatalf("ConsumeDreamRun: %v", err)
	}
	if !r.Counted {
		t.Errorf("loop_cap_hit must count; got %+v", r)
	}
}

// ─────────────────────────────────────────────────────────────────
// Soft-mode duplicate (idempotent re-read): the second call with
// the same run_id reports Counted=true (it was counted by the first
// call), and the per-run signal row is written exactly once.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_SoftModeIdempotent(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)

	first, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_passive", "completed", "soft", 40))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !first.Counted {
		t.Fatalf("first call must count: %+v", first)
	}

	// Repeat with same run_id — the ledger gate makes this idempotent.
	second, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runID, "lucid_passive", "completed", "soft", 40))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !second.Counted {
		t.Errorf("second call should report Counted=true (the run was counted on first call): %+v", second)
	}

	// Counter only moves once.
	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != 1 {
		t.Errorf("counter incremented %d times, want 1 (idempotency broken)", lucid)
	}

	// dream_quota_signals must have exactly one row for this run_id.
	var signalCount int
	_ = f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM dream_quota_signals WHERE dream_run_id = $1::uuid`, runID,
	).Scan(&signalCount)
	if signalCount != 1 {
		t.Errorf("dream_quota_signals rows for run = %d, want 1 (PRIMARY KEY enforces)", signalCount)
	}
}

// ─────────────────────────────────────────────────────────────────
// Hub scope cannot use tier=basic. Validation rejects.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_HubScopeRejectsBasicTier(t *testing.T) {
	f := setupFixtures(t)
	runID := f.seedDreamRun(t)
	_, err := f.store.ConsumeDreamRun(f.ctx,
		f.hubParams(runID, "basic", "completed", "hard", 100))
	if err == nil {
		t.Fatal("expected validation error for hub+basic, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// partial_failed retry: a NEW run_id charges again. Per the plan's
// explicit contract — operator-free retry is a post-v1 option.
// ─────────────────────────────────────────────────────────────────

func TestConsumeDreamRun_PartialFailedRetryCharges(t *testing.T) {
	f := setupFixtures(t)

	// First run: partial_failed.
	runA := f.seedDreamRun(t)
	rA, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runA, "lucid_passive", "partial_failed", "hard", 40))
	if err != nil {
		t.Fatalf("run A: %v", err)
	}
	if !rA.Counted {
		t.Fatalf("partial_failed must count; got %+v", rA)
	}

	// Retry as a new run_id — also counts.
	runB := f.seedDreamRun(t)
	rB, err := f.store.ConsumeDreamRun(f.ctx,
		f.userParams(runB, "lucid_passive", "completed", "hard", 40))
	if err != nil {
		t.Fatalf("run B: %v", err)
	}
	if !rB.Counted {
		t.Fatalf("retry must count under v1 contract; got %+v", rB)
	}

	_, lucid, _ := f.store.GetUserDreamUsage(f.ctx, f.ownerID, f.periodStart)
	if lucid != 2 {
		t.Errorf("after partial_failed + retry, lucid=%d, want 2 (each new run_id charges)", lucid)
	}
}
