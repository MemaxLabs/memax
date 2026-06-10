package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// These tests pin the ops queries against a real Postgres fixture.
// They seed river_job / memories rows directly (testdb gives us a
// schema with everything) and verify the projection logic.

func TestOpsPulse_EmptyDB_DoesNotErrorOrPanic(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	pulse, err := s.(store.AdminOpsStore).GetOpsPulse(ctx, 60)
	if err != nil {
		t.Fatalf("GetOpsPulse on empty DB: %v", err)
	}
	if pulse == nil {
		t.Fatal("pulse should not be nil")
	}
	if pulse.Throughput.WindowMinutes != 60 {
		t.Errorf("window echo lost: %d", pulse.Throughput.WindowMinutes)
	}
	// A fresh DB has no workers heartbeating and no jobs. Each
	// slice field MUST be a non-nil empty slice, not nil, because
	// Go marshals nil slices as JSON null which crashes TS
	// clients calling `.length` / `.map`. Regression guard: a
	// crash on an empty DB hit the UI as "can't access property
	// length, pulse.workers.clients is null".
	if pulse.Workers.Clients == nil {
		t.Error("pulse.Workers.Clients should be []OpsWorkerClient{}, got nil")
	}
	if pulse.Queues == nil {
		t.Error("pulse.Queues should be []OpsQueueDepth{}, got nil")
	}
	if pulse.RedFlags == nil {
		t.Error("pulse.RedFlags should be []OpsRedFlag{}, got nil")
	}
	if pulse.Workers.Healthy != 0 || pulse.Workers.Stale != 0 {
		t.Errorf("unexpected worker counts on empty DB: %+v", pulse.Workers)
	}

	// JSON round-trip test: the wire form MUST use [] not null for
	// every slice, since TS clients don't coerce null→[].
	// clients + queues are actually empty on a fresh DB; red_flags
	// fires a worker_offline entry (no workers heartbeating), so we
	// can't assert []. The contract check: no slice-shaped field
	// serializes as `:null` anywhere.
	buf, err := json.Marshal(pulse)
	if err != nil {
		t.Fatalf("marshal pulse: %v", err)
	}
	wire := string(buf)
	for _, field := range []string{`"clients":[]`, `"queues":[]`} {
		if !strings.Contains(wire, field) {
			t.Errorf("wire form missing %q — client will crash on null:\n%s", field, wire)
		}
	}
	if strings.Contains(wire, `":null`) {
		t.Errorf("wire form contains null where a slice is expected:\n%s", wire)
	}
}

func TestOpsPulse_CountsMemoriesAndThroughput(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	seedOwnerAndHub(t, pool)

	// Three jobs finalized in the last hour: 2 succeeded, 1 failed.
	seedRiverJob(t, pool, "memory_process", "default", "completed", time.Now().Add(-5*time.Minute))
	seedRiverJob(t, pool, "memory_process", "default", "completed", time.Now().Add(-10*time.Minute))
	seedRiverJob(t, pool, "memory_process", "default", "discarded", time.Now().Add(-15*time.Minute))
	// One dream succeeded.
	seedRiverJob(t, pool, "dream_cycle", "dreams", "completed", time.Now().Add(-20*time.Minute))
	// One outside the window (must NOT count).
	seedRiverJob(t, pool, "memory_process", "default", "completed", time.Now().Add(-70*time.Minute))

	pulse, err := s.(store.AdminOpsStore).GetOpsPulse(ctx, 60)
	if err != nil {
		t.Fatalf("GetOpsPulse: %v", err)
	}
	if pulse.Throughput.MemoriesProcessed != 2 {
		t.Errorf("MemoriesProcessed = %d, want 2", pulse.Throughput.MemoriesProcessed)
	}
	if pulse.Throughput.MemoriesFailed != 1 {
		t.Errorf("MemoriesFailed = %d, want 1", pulse.Throughput.MemoriesFailed)
	}
	if pulse.Throughput.DreamsCompleted != 1 {
		t.Errorf("DreamsCompleted = %d, want 1", pulse.Throughput.DreamsCompleted)
	}
}

func TestOpsPulse_RedFlag_StuckMemories(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID, hubID := seedOwnerAndHub(t, pool)

	// Memory created 10 min ago, still processing — should flag.
	seedMemory(t, pool, ownerID, hubID, "processing", time.Now().Add(-10*time.Minute), "")

	pulse, err := s.(store.AdminOpsStore).GetOpsPulse(ctx, 60)
	if err != nil {
		t.Fatalf("GetOpsPulse: %v", err)
	}
	if pulse.Ingestion.StuckOverFiveMin != 1 {
		t.Errorf("StuckOverFiveMin = %d, want 1", pulse.Ingestion.StuckOverFiveMin)
	}

	// A red flag of kind "stuck_memories" must exist.
	found := false
	for _, f := range pulse.RedFlags {
		if f.Kind == "stuck_memories" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected stuck_memories red flag; got %+v", pulse.RedFlags)
	}
}

func TestOpsListJobs_Pagination(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	seedOwnerAndHub(t, pool)

	// Seed 5 jobs with descending creation times so they list in
	// deterministic order.
	now := time.Now()
	for i := range 5 {
		seedRiverJob(t, pool, "memory_process", "default", "completed", now.Add(-time.Duration(i)*time.Minute))
	}

	// Page 1: limit 2.
	resp, err := s.(store.AdminOpsStore).ListOpsJobs(ctx, model.JobListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("ListOpsJobs page1: %v", err)
	}
	if len(resp.Jobs) != 2 || !resp.HasMore || resp.NextCursor == "" {
		t.Fatalf("page1 unexpected: %+v", resp)
	}

	// Page 2.
	resp2, err := s.(store.AdminOpsStore).ListOpsJobs(ctx, model.JobListOpts{Limit: 2, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatalf("ListOpsJobs page2: %v", err)
	}
	if len(resp2.Jobs) != 2 {
		t.Errorf("page2 len = %d, want 2", len(resp2.Jobs))
	}
	// IDs must strictly differ between pages — no double-counting
	// at the keyset boundary.
	seen := map[int64]bool{}
	for _, j := range resp.Jobs {
		seen[j.ID] = true
	}
	for _, j := range resp2.Jobs {
		if seen[j.ID] {
			t.Errorf("job %d appears on both pages", j.ID)
		}
	}
}

func TestOpsStuckMemories_FiltersByAge(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID, hubID := seedOwnerAndHub(t, pool)

	// One recent (within threshold), one stuck (beyond).
	seedMemory(t, pool, ownerID, hubID, "processing", time.Now().Add(-30*time.Second), "")
	seedMemory(t, pool, ownerID, hubID, "processing", time.Now().Add(-10*time.Minute), "")

	stuck, err := s.(store.AdminOpsStore).ListOpsStuckMemories(ctx, 300, 50)
	if err != nil {
		t.Fatalf("ListOpsStuckMemories: %v", err)
	}
	if len(stuck) != 1 {
		t.Fatalf("stuck = %d, want 1 (only the 10-min-old one)", len(stuck))
	}
	if stuck[0].AgeSeconds < 300 {
		t.Errorf("AgeSeconds = %d, want >= 300", stuck[0].AgeSeconds)
	}
}

func TestAppendAdminAudit_StoresBigintResourceID(t *testing.T) {
	// Regression: before migration 002, ops writes used comms_audit,
	// whose resource_id column is uuid — the SQL silently failed on
	// bigint-like job IDs. admin_audit accepts text, so a
	// bigint-string job id must round-trip cleanly and the
	// resource_type column must accept our free-text "ops_job".
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID, _ := seedOwnerAndHub(t, pool)

	entry := &model.AdminAuditEntry{
		ID:           uuid.NewString(),
		ResourceType: "ops_job",
		ResourceID:   "9223372036854775800", // near-max int64 — proves text
		Action:       "retry",
		ActorID:      &ownerID,
		Metadata:     json.RawMessage(`{"job_id": 9223372036854775800}`),
	}
	if err := s.(store.AdminOpsStore).AppendAdminAudit(ctx, entry); err != nil {
		t.Fatalf("AppendAdminAudit: %v", err)
	}

	// Read it back to prove the row landed with the values we
	// supplied (not coerced or truncated).
	var gotType, gotID, gotAction string
	var gotActor *string
	if err := pool.QueryRow(ctx, `
		SELECT resource_type, resource_id, action, actor_id::text
		FROM admin_audit WHERE id = $1::uuid
	`, entry.ID).Scan(&gotType, &gotID, &gotAction, &gotActor); err != nil {
		t.Fatalf("select admin_audit: %v", err)
	}
	if gotType != "ops_job" || gotID != "9223372036854775800" || gotAction != "retry" {
		t.Errorf("row mismatch: type=%q id=%q action=%q", gotType, gotID, gotAction)
	}
	if gotActor == nil || *gotActor != ownerID {
		t.Errorf("actor_id = %v, want %s", gotActor, ownerID)
	}
}

func TestOpsFailedIngestions_MatchesMarker(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID, hubID := seedOwnerAndHub(t, pool)

	// Two active memories: one with the failure marker, one without.
	seedMemory(t, pool, ownerID, hubID, "active", time.Now().Add(-10*time.Minute), "Processing failed after 3 attempts: voyage timeout")
	seedMemory(t, pool, ownerID, hubID, "active", time.Now().Add(-10*time.Minute), "Normal summary.")

	failed, err := s.(store.AdminOpsStore).ListOpsFailedIngestions(ctx, 60, 50)
	if err != nil {
		t.Fatalf("ListOpsFailedIngestions: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(failed))
	}
}

// --- helpers ---

// seedOwnerAndHub inserts a user + personal hub and returns their IDs.
func seedOwnerAndHub(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'Ops Test', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@ops.local",
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, plan, created_at, updated_at)
		 VALUES ($1::uuid, 'Ops Personal', $2, 'personal', $3::uuid, 'personal_early_access', now(), now())`,
		hubID, "ops-"+hubID[:8], ownerID,
	); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	return ownerID, hubID
}

// seedMemory inserts one memory at the given state + created_at.
func seedMemory(t *testing.T, pool *pgxpool.Pool, ownerID, hubID, state string, createdAt time.Time, summary string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO memories (id, owner_id, hub_id, title, summary, content, content_type, source, state, kind, stability, retrieval_weight, access_intents, created_at, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'Test', $4, 'body', 'markdown', 'cli', $5, 'semantic', 'evolving', 1.0, '{}'::jsonb, $6, $6)`,
		id, ownerID, hubID, summary, state, createdAt,
	)
	if err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	return id
}

// seedRiverJob inserts one river_job row with a chosen state + finalized_at.
// Completed/discarded/cancelled states require finalized_at per the
// CHECK constraint.
func seedRiverJob(t *testing.T, pool *pgxpool.Pool, kind, queue, state string, finalizedAt time.Time) int64 {
	t.Helper()
	args := json.RawMessage(`{}`)
	var finalized any
	if state == "completed" || state == "discarded" || state == "cancelled" {
		finalized = finalizedAt
	}
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO river_job (state, kind, queue, max_attempts, args, finalized_at, created_at, scheduled_at, attempted_at)
		 VALUES ($1::river_job_state, $2, $3, 3, $4::jsonb, $5, $6, $6, $6)
		 RETURNING id`,
		state, kind, queue, args, finalized, finalizedAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed river_job: %v", err)
	}
	return id
}
