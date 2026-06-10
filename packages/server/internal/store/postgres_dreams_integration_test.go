package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupDreamFixture seeds user + personal hub combo, returning the
// pieces each dream test needs to scope its writes. Dreams are
// hub-scoped; the personal hub satisfies the migration-012 partial
// unique index.
func setupDreamFixture(t *testing.T) (store.Store, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@dream",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hubID := uuid.NewString()
	hub := &model.Hub{
		ID: hubID, Name: "P", Slug: "p-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	return s, hubID, userID
}

func TestPostgresDreamRunLifecycle(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupDreamFixture(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	run := &model.DreamRun{
		ID:        uuid.NewString(),
		OwnerID:   userID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    model.DreamRunStatusRunning,
		StartedAt: now,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"scan": {Candidates: 10, Processed: 5},
		},
		PhaseBudgets: map[string]model.DreamPhaseBudget{},
		Report:       "started",
	}

	// --- CreateDreamRun ---
	if err := s.CreateDreamRun(run); err != nil {
		t.Fatalf("CreateDreamRun: %v", err)
	}

	// --- Partial-unique-index collision: second running run for the
	// same hub must return ErrDreamRunConcurrent. This is the migration
	// 012 guard that keeps dream cycles single-flight per hub.
	dup := *run
	dup.ID = uuid.NewString()
	if err := s.CreateDreamRun(&dup); !errors.Is(err, store.ErrDreamRunConcurrent) {
		t.Errorf("second active run for hub: got %v, want ErrDreamRunConcurrent", err)
	}

	// --- Heartbeat ---
	if err := s.HeartbeatDreamRun(ctx, run.ID); err != nil {
		t.Fatalf("HeartbeatDreamRun: %v", err)
	}

	// --- GetLatestDreamRunByHub (also covers GetLatestDreamRun via
	// personal-hub resolver below).
	latest, err := s.GetLatestDreamRunByHub(hubID)
	if err != nil {
		t.Fatalf("GetLatestDreamRunByHub: %v", err)
	}
	if latest.ID != run.ID {
		t.Errorf("latest.ID = %q, want %q", latest.ID, run.ID)
	}
	if latest.Status != model.DreamRunStatusRunning {
		t.Errorf("latest.Status = %q", latest.Status)
	}
	if latest.LastHeartbeatAt == nil {
		t.Error("HeartbeatDreamRun didn't set last_heartbeat_at")
	}

	// Owner -> personal hub lookup.
	latestByOwner, err := s.GetLatestDreamRun(userID)
	if err != nil {
		t.Fatalf("GetLatestDreamRun: %v", err)
	}
	if latestByOwner.ID != run.ID {
		t.Errorf("owner lookup returned different run: %q", latestByOwner.ID)
	}

	// --- UpdateDreamRun — terminate the row so subsequent CreateDreamRun
	// for the same hub is unblocked (partial unique index keys on status).
	finished := now.Add(time.Minute)
	run.Status = model.DreamRunStatusCompleted
	run.FinishedAt = finished
	run.MemoriesScanned = 42
	run.DuplicatesMerged = 3
	run.Report = "done"
	if err := s.UpdateDreamRun(run); err != nil {
		t.Fatalf("UpdateDreamRun: %v", err)
	}

	// Re-fetch and verify persistence.
	latest, _ = s.GetLatestDreamRunByHub(hubID)
	if latest.Status != model.DreamRunStatusCompleted || latest.MemoriesScanned != 42 || latest.DuplicatesMerged != 3 {
		t.Errorf("UpdateDreamRun didn't persist: %+v", latest)
	}

	// After completion, another run can start — previously-blocking
	// partial index only keys 'running' rows.
	next := *run
	next.ID = uuid.NewString()
	next.Status = model.DreamRunStatusRunning
	next.FinishedAt = time.Time{}
	next.Report = "next cycle"
	if err := s.CreateDreamRun(&next); err != nil {
		t.Errorf("new run after completion should succeed, got %v", err)
	}

	// --- ListDreamRunsByHub returns them in started-at desc order.
	runs, err := s.ListDreamRunsByHub(hubID, 10)
	if err != nil {
		t.Fatalf("ListDreamRunsByHub: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("ListDreamRunsByHub returned %d runs, want 2", len(runs))
	}
}

func TestPostgresClaimStaleDreamRun(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupDreamFixture(t)
	ctx := context.Background()

	// Seed a running run whose started_at is deep in the past.
	run := &model.DreamRun{
		ID:           uuid.NewString(),
		OwnerID:      userID,
		HubID:        hubID,
		Mode:         "maintenance",
		Status:       model.DreamRunStatusRunning,
		StartedAt:    time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Microsecond),
		PhaseMetrics: map[string]model.DreamPhaseMetrics{},
		PhaseBudgets: map[string]model.DreamPhaseBudget{},
		Report:       "legacy",
	}
	if err := s.CreateDreamRun(run); err != nil {
		t.Fatalf("CreateDreamRun: %v", err)
	}

	// Claim with a 1h threshold — row is 2h old so it should flip to
	// failed and report transitioned=true.
	claimed, err := s.ClaimStaleDreamRun(ctx, hubID, time.Hour)
	if err != nil {
		t.Fatalf("ClaimStaleDreamRun: %v", err)
	}
	if !claimed {
		t.Fatal("stale row should be claimed (transitioned=true)")
	}

	// Re-claim — no rows match the stale predicate now.
	claimed, _ = s.ClaimStaleDreamRun(ctx, hubID, time.Hour)
	if claimed {
		t.Error("re-claim should report transitioned=false")
	}

	// Latest run should now be status=failed.
	latest, _ := s.GetLatestDreamRunByHub(hubID)
	if latest.Status != model.DreamRunStatusFailed {
		t.Errorf("after stale claim: status = %q, want failed", latest.Status)
	}
}

func TestPostgresDreamActions(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupDreamFixture(t)

	run := &model.DreamRun{
		ID:           uuid.NewString(),
		OwnerID:      userID,
		HubID:        hubID,
		Mode:         "maintenance",
		Status:       model.DreamRunStatusRunning,
		StartedAt:    time.Now().UTC().Truncate(time.Microsecond),
		PhaseMetrics: map[string]model.DreamPhaseMetrics{},
		PhaseBudgets: map[string]model.DreamPhaseBudget{},
	}
	if err := s.CreateDreamRun(run); err != nil {
		t.Fatalf("CreateDreamRun: %v", err)
	}

	// Create a couple of actions spanning different action_types — the
	// handler's report renderer branches on ActionType so we need
	// variety to cover the GetDreamActions scan/unmarshal path.
	actions := []model.DreamAction{
		{
			ID: uuid.NewString(), RunID: run.ID,
			ActionType:      "merge",
			SourceMemoryIDs: []string{uuid.NewString(), uuid.NewString()},
			ResultMemoryID:  uuid.NewString(),
			Reason:          "duplicate",
			Similarity:      0.92,
			CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
		},
		{
			ID: uuid.NewString(), RunID: run.ID,
			ActionType:      "archive",
			SourceMemoryIDs: []string{uuid.NewString()},
			Reason:          "stale",
			CreatedAt:       time.Now().UTC().Add(time.Millisecond).Truncate(time.Microsecond),
		},
	}
	for i := range actions {
		if err := s.CreateDreamAction(&actions[i]); err != nil {
			t.Fatalf("CreateDreamAction[%d]: %v", i, err)
		}
	}

	got, err := s.GetDreamActions(run.ID)
	if err != nil {
		t.Fatalf("GetDreamActions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetDreamActions returned %d, want 2", len(got))
	}
	// Ordered by created_at — merge (earlier) should precede archive.
	if got[0].ActionType != "merge" || got[1].ActionType != "archive" {
		t.Errorf("action order wrong: %q, %q", got[0].ActionType, got[1].ActionType)
	}
	if got[0].Similarity < 0.9 {
		t.Errorf("similarity lost in round-trip: %v", got[0].Similarity)
	}
	if len(got[0].SourceMemoryIDs) != 2 {
		t.Errorf("source_memory_ids round-trip: got %d, want 2", len(got[0].SourceMemoryIDs))
	}
}
