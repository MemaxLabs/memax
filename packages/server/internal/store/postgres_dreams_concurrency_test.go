package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestCreateDreamRunRejectsOverlappingRunningRuns verifies the partial
// unique index added in migration 012 keeps two cycles from racing for
// the same hub. The store must map the pg unique_violation into the
// typed ErrDreamRunConcurrent so callers don't have to sniff SQLSTATE
// strings themselves.
//
// Gated on DATABASE_URL so unit runs stay hermetic; the CI integration
// job sets DATABASE_URL to a disposable Postgres instance with
// migrations applied.
func TestCreateDreamRunRejectsOverlappingRunningRuns(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres dream concurrency test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Seed a disposable hub + owner so FK constraints hold. UUIDs are
	// stable so re-running the test cleans up after itself in the
	// DELETE at the end.
	ownerID := "00000000-0000-0000-0000-dd00aaaa0001"
	hubID := "00000000-0000-0000-0000-dd00bbbb0001"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)
		 ON CONFLICT (id) DO NOTHING`,
		ownerID, "dream-concurrency@test.local", "dream-concurrency@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5)
		 ON CONFLICT (id) DO NOTHING`,
		hubID, "dream-concurrency-hub", "dream-concurrency-hub", ownerID, now,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}

	// Cleanup first — prior aborted runs of this test may have left
	// rows behind, and the unique index would surface that as a
	// false positive.
	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup dream_runs: %v", err)
	}

	first := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0001",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now,
	}
	if err := s.CreateDreamRun(first); err != nil {
		t.Fatalf("first CreateDreamRun: %v", err)
	}

	// A second `running` insert for the same hub must collide.
	second := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0002",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now.Add(time.Second),
	}
	if err := s.CreateDreamRun(second); !errors.Is(err, ErrDreamRunConcurrent) {
		t.Fatalf("second CreateDreamRun should return ErrDreamRunConcurrent, got: %v", err)
	}

	// Once the first run completes, the partial index no longer covers
	// it — the next cycle should be accepted. This verifies the
	// predicate is `WHERE status = 'running'` and not a blanket
	// per-hub constraint.
	first.Status = "completed"
	first.FinishedAt = now.Add(time.Minute)
	if err := s.UpdateDreamRun(first); err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	third := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0003",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now.Add(2 * time.Minute),
	}
	if err := s.CreateDreamRun(third); err != nil {
		t.Fatalf("third CreateDreamRun after first completed: %v", err)
	}

	// Clean up for re-runs.
	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Logf("cleanup dream_runs: %v", err)
	}
}

// TestClaimStaleDreamRunReclaimsOldRunning covers the self-healing
// path the engine relies on when a prior cycle's terminal
// UpdateDreamRun failed. A `running` row older than the threshold
// is flipped to `failed`; a younger row is left alone.
func TestClaimStaleDreamRunReclaimsOldRunning(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping ClaimStaleDreamRun test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-dd00aaaa0002"
	hubID := "00000000-0000-0000-0000-dd00bbbb0002"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)
		 ON CONFLICT (id) DO NOTHING`,
		ownerID, "stale-reclaim@test.local", "stale-reclaim@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5)
		 ON CONFLICT (id) DO NOTHING`,
		hubID, "stale-reclaim-hub", "stale-reclaim-hub", ownerID, now,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup dream_runs: %v", err)
	}

	// A fresh `running` row younger than the threshold must NOT be
	// reclaimed — could be a live cycle. Started 1 minute ago; we
	// claim with threshold=15min below.
	young := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0010",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now.Add(-1 * time.Minute),
	}
	if err := s.CreateDreamRun(young); err != nil {
		t.Fatalf("seed young running row: %v", err)
	}
	reclaimed, err := s.ClaimStaleDreamRun(ctx, hubID, 15*time.Minute)
	if err != nil {
		t.Fatalf("ClaimStaleDreamRun (young): %v", err)
	}
	if reclaimed {
		t.Fatalf("must not reclaim a young running row")
	}

	// Now back-date the row past the threshold. Reclaim should fire.
	if _, err := pool.Exec(ctx,
		`UPDATE dream_runs SET started_at = $1 WHERE id = $2::uuid`,
		now.Add(-30*time.Minute), young.ID,
	); err != nil {
		t.Fatalf("back-date running row: %v", err)
	}
	reclaimed, err = s.ClaimStaleDreamRun(ctx, hubID, 15*time.Minute)
	if err != nil {
		t.Fatalf("ClaimStaleDreamRun (stale): %v", err)
	}
	if !reclaimed {
		t.Fatalf("expected reclaim to flip stale running row")
	}

	// After reclaim the row is no longer `running`, so a fresh
	// CreateDreamRun must succeed.
	next := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0011",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now,
	}
	if err := s.CreateDreamRun(next); err != nil {
		t.Fatalf("CreateDreamRun after reclaim: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Logf("cleanup dream_runs: %v", err)
	}
}

// TestHeartbeatKeepsRunFromReclaim verifies that a live cycle bumping
// last_heartbeat_at is not reclaimed even when started_at is old.
// This is the whole point of the heartbeat column (migration 014):
// ClaimStaleDreamRun checks COALESCE(last_heartbeat_at, started_at),
// so a chatty slow run is safe regardless of total cycle length.
func TestHeartbeatKeepsRunFromReclaim(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping heartbeat reclaim test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-dd00aaaa0003"
	hubID := "00000000-0000-0000-0000-dd00bbbb0003"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)
		 ON CONFLICT (id) DO NOTHING`,
		ownerID, "heartbeat-reclaim@test.local", "heartbeat-reclaim@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5)
		 ON CONFLICT (id) DO NOTHING`,
		hubID, "heartbeat-reclaim-hub", "heartbeat-reclaim-hub", ownerID, now,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup dream_runs: %v", err)
	}

	// started_at is old (would otherwise be reclaimed), but the
	// heartbeat is fresh — a slow-but-alive cycle.
	run := &model.DreamRun{
		ID:        "00000000-0000-0000-0000-dd00cccc0100",
		OwnerID:   ownerID,
		HubID:     hubID,
		Mode:      "maintenance",
		Status:    "running",
		StartedAt: now.Add(-45 * time.Minute),
	}
	if err := s.CreateDreamRun(run); err != nil {
		t.Fatalf("seed running row: %v", err)
	}
	if err := s.HeartbeatDreamRun(ctx, run.ID); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	// Reclaim threshold of 30 min must NOT fire — heartbeat is now.
	reclaimed, err := s.ClaimStaleDreamRun(ctx, hubID, 30*time.Minute)
	if err != nil {
		t.Fatalf("ClaimStaleDreamRun (fresh heartbeat): %v", err)
	}
	if reclaimed {
		t.Fatalf("a row with a fresh heartbeat must not be reclaimed")
	}

	// Now back-date the heartbeat past the threshold. Reclaim should fire.
	if _, err := pool.Exec(ctx,
		`UPDATE dream_runs SET last_heartbeat_at = $1 WHERE id = $2::uuid`,
		now.Add(-45*time.Minute), run.ID,
	); err != nil {
		t.Fatalf("back-date heartbeat: %v", err)
	}
	reclaimed, err = s.ClaimStaleDreamRun(ctx, hubID, 30*time.Minute)
	if err != nil {
		t.Fatalf("ClaimStaleDreamRun (stale heartbeat): %v", err)
	}
	if !reclaimed {
		t.Fatalf("stale heartbeat must be reclaimed")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Logf("cleanup dream_runs: %v", err)
	}
}
