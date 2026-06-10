package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresPlansByScope(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	personal, err := s.ListPlansByScope(ctx, model.PlanScopePersonal)
	if err != nil {
		t.Fatalf("ListPlansByScope personal: %v", err)
	}
	hub, err := s.ListPlansByScope(ctx, model.PlanScopeHub)
	if err != nil {
		t.Fatalf("ListPlansByScope hub: %v", err)
	}

	if len(personal) == 0 {
		t.Error("expected at least one personal plan from migration 001 seeds")
	}
	if len(hub) == 0 {
		t.Error("expected at least one hub plan")
	}

	// Every returned plan must carry the requested scope — the
	// WHERE scope = ... filter is load-bearing for the admin
	// plan picker.
	for _, p := range personal {
		if p.Scope != model.PlanScopePersonal {
			t.Errorf("personal filter leaked hub plan %q", p.ID)
		}
	}
	for _, p := range hub {
		if p.Scope != model.PlanScopeHub {
			t.Errorf("hub filter leaked personal plan %q", p.ID)
		}
	}
}

func TestPostgresGetPlanByID(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	p, err := s.GetPlan(ctx, "personal_free")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p == nil || p.ID != "personal_free" {
		t.Errorf("GetPlan(personal_free) returned %+v", p)
	}

	// Unknown ID returns (nil, nil) or (nil, err) depending on
	// the store's contract — both are acceptable here; we just
	// confirm no panic and no wrong row.
	unknown, _ := s.GetPlan(ctx, "not-a-plan")
	if unknown != nil {
		t.Errorf("unknown plan should return nil, got %+v", unknown)
	}
}

func TestPostgresPlanOverrideCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'pov@t', 'O', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// --- Before upsert: no override ---
	// Contract: absent row returns (nil, err) — both nil-with-err and
	// nil-without-err are acceptable; just confirm no stale row.
	got, _ := s.GetPlanOverride(ctx, userID)
	if got != nil {
		t.Errorf("expected nil override, got %+v", got)
	}

	// --- Upsert: set override ---
	override := &model.PlanOverride{
		UserID:    userID,
		Overrides: map[string]any{"ask_limit": float64(9999)},
		Reason:    "VIP customer",
	}
	if err := s.UpsertPlanOverride(ctx, override); err != nil {
		t.Fatalf("UpsertPlanOverride: %v", err)
	}

	// Read back.
	var err error
	got, err = s.GetPlanOverride(ctx, userID)
	if err != nil {
		t.Fatalf("GetPlanOverride (after upsert): %v", err)
	}
	if got == nil {
		t.Fatal("expected override after upsert, got nil")
	}
	if got.Reason != "VIP customer" {
		t.Errorf("Reason = %q", got.Reason)
	}
	if got.Overrides["ask_limit"] != float64(9999) {
		t.Errorf("ask_limit override = %v", got.Overrides["ask_limit"])
	}

	// --- Upsert again: should update rather than insert (unique
	// on user_id).
	override.Reason = "Updated reason"
	if err := s.UpsertPlanOverride(ctx, override); err != nil {
		t.Fatalf("UpsertPlanOverride (update): %v", err)
	}
	got, _ = s.GetPlanOverride(ctx, userID)
	if got.Reason != "Updated reason" {
		t.Errorf("update path didn't persist reason: %q", got.Reason)
	}

	// --- Delete ---
	if err := s.DeletePlanOverride(ctx, userID); err != nil {
		t.Fatalf("DeletePlanOverride: %v", err)
	}
	got, _ = s.GetPlanOverride(ctx, userID)
	if got != nil {
		t.Errorf("override persisted after delete: %+v", got)
	}
}

func TestPostgresUsageEventInsert(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'ue@t', 'U', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ev := &model.UsageEvent{
		UserID:    userID,
		Operation: "push",
		Source:    "api",
		AgentName: "claude-code",
		Metadata:  map[string]any{"foo": "bar"},
	}
	if err := s.InsertUsageEvent(ctx, ev); err != nil {
		t.Fatalf("InsertUsageEvent: %v", err)
	}

	// Verify row exists in DB — usage_events is append-only so we
	// can count.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM usage_events WHERE user_id = $1::uuid`,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf("select count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 usage event row, got %d", count)
	}
}

func TestPostgresUsageCountersSetAndReset(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'uc@t', 'U', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Use the current calendar month so SetUsageCounters writes the same
	// period GetCurrentUsage reads. Hardcoded dates would silently break
	// the moment the calendar rolls past them.
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)

	// Set counters to non-zero.
	if err := s.SetUsageCounters(ctx, userID, start, end, 10, 20, 30); err != nil {
		t.Fatalf("SetUsageCounters: %v", err)
	}
	u, err := s.GetCurrentUsage(userID)
	if err != nil {
		t.Fatalf("GetCurrentUsage: %v", err)
	}
	// SetUsageCounters uses GREATEST semantics — the baseline
	// should reflect the values we set.
	if u.PushCount != 10 || u.RecallCount != 20 || u.AskCount != 30 {
		t.Errorf("after set = %+v", u)
	}

	// Reset zeroes the row (no GREATEST).
	if err := s.ResetUsageCounters(ctx, userID, start, end); err != nil {
		t.Fatalf("ResetUsageCounters: %v", err)
	}
	u, _ = s.GetCurrentUsage(userID)
	if u.PushCount != 0 || u.RecallCount != 0 || u.AskCount != 0 {
		t.Errorf("after reset = %+v (expected all zero)", u)
	}
}

func TestPostgresHubOverLimitMarker(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'olim@t', 'O', 'personal_early_access', now(), now())`,
		ownerID,
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	hub := &model.Hub{
		ID: uuid.NewString(), Name: "Cap",
		Slug: "cap-" + uuid.NewString()[:6], HubType: "team",
		Plan: model.HubFreeTeamPlanID, OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	// SetHubOverLimit → flips over_limit_since to now; returns
	// (transitioned=true) on the first set, (false) on re-set.
	now := time.Now().UTC().Truncate(time.Microsecond)
	transitioned, err := s.SetHubOverLimit(ctx, hub.ID, now)
	if err != nil {
		t.Fatalf("SetHubOverLimit: %v", err)
	}
	if !transitioned {
		t.Error("first SetHubOverLimit should report transitioned=true")
	}

	// Re-set with same value: no transition.
	transitioned, _ = s.SetHubOverLimit(ctx, hub.ID, now)
	if transitioned {
		t.Error("second SetHubOverLimit should not report transition")
	}

	// ClearHubOverLimit flips back to NULL; returns (cleared=true)
	// on the first clear, (false) on re-clear.
	cleared, err := s.ClearHubOverLimit(ctx, hub.ID)
	if err != nil {
		t.Fatalf("ClearHubOverLimit: %v", err)
	}
	if !cleared {
		t.Error("first clear should report cleared=true")
	}
	cleared, _ = s.ClearHubOverLimit(ctx, hub.ID)
	if cleared {
		t.Error("second clear should report cleared=false")
	}
}
