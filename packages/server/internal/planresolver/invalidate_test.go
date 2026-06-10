package planresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
)

// ─── InvalidateUser ─────────────────────────────────────────────

func TestInvalidateUserWritesPostgresFirstThenDeletesRedis(t *testing.T) {
	t.Parallel()

	// Invalidate's documented ordering is authoritative: Postgres
	// SET must complete before Redis DEL. If the order reverses, a
	// concurrent request between the two steps can re-backfill
	// Redis from stale Postgres, leaving the cache permanently
	// wrong. This test pins the behavior so a later refactor that
	// "cleans up" the order immediately surfaces.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ms := &mockResolverStore{
		user:     &model.User{ID: "u1", PersonalPlanID: "personal_free"},
		hubPlans: nil,
	}
	registry := plans.NewRegistry(ms)
	_ = registry.Load(context.Background())
	r := New(client, ms, registry)

	// Seed Redis with an old value — Invalidate must wipe it.
	ctx := context.Background()
	client.Set(ctx, effectivePlanKeyPrefix+"u1", "old-plan", 0)

	if err := r.InvalidateUser(ctx, "u1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	// Postgres side-effect: SetEffectivePlan was called.
	if !ms.effectivePlanSet {
		t.Error("SetEffectivePlan should have been called")
	}

	// Redis side-effect: key was deleted.
	if exists, _ := client.Exists(ctx, effectivePlanKeyPrefix+"u1").Result(); exists != 0 {
		t.Error("effective plan Redis key should be deleted")
	}
}

func TestInvalidateUserSurfacesPostgresError(t *testing.T) {
	t.Parallel()

	// Postgres write is the authoritative step — an error there
	// MUST surface so the caller knows the cache might be stale.
	// This was the fix for Codex finding #2 on the billing code.
	ms := &errStore{setErr: errors.New("pg down")}
	registry := plans.NewRegistry(ms)
	r := New(nil, ms, registry)

	err := r.InvalidateUser(context.Background(), "u1")
	if err == nil {
		t.Fatal("expected Postgres error to surface")
	}
}

func TestInvalidateUserSurfacesRedisError(t *testing.T) {
	t.Parallel()

	// Redis DEL failure means the stale cache entry stays —
	// callers that observe this error can retry or page-flush.
	// Swallowing it would leave the cache permanently stale if
	// a subsequent request resets the Redis cache from the same
	// old value before TTL expiry.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ms := &mockResolverStore{user: &model.User{ID: "u1", PersonalPlanID: "personal_free"}}
	registry := plans.NewRegistry(ms)
	r := New(client, ms, registry)

	// Close the miniredis BEFORE the Invalidate call to induce a
	// real Redis error on the DEL.
	mr.Close()

	err := r.InvalidateUser(context.Background(), "u1")
	if err == nil {
		t.Fatal("expected Redis DEL error to surface")
	}
}

// ─── InvalidateHubMembers ───────────────────────────────────────

func TestInvalidateHubMembersVisitsEveryMember(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ms := &mockResolverStore{
		user: &model.User{PersonalPlanID: "personal_free"},
		hubMembers: []model.HubMember{
			{UserID: "u1"},
			{UserID: "u2"},
			{UserID: "u3"},
		},
	}
	registry := plans.NewRegistry(ms)
	_ = registry.Load(context.Background())
	r := New(client, ms, registry)

	ctx := context.Background()
	// Seed caches so we can verify they got wiped.
	for _, u := range []string{"u1", "u2", "u3"} {
		client.Set(ctx, effectivePlanKeyPrefix+u, "stale", 0)
	}
	// Seed hub plan cache too — InvalidateHubMembers also wipes it.
	client.Set(ctx, hubPlanKeyPrefix+"hub-1", "stale-plan", 0)

	if err := r.InvalidateHubMembers(ctx, "hub-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	for _, u := range []string{"u1", "u2", "u3"} {
		if exists, _ := client.Exists(ctx, effectivePlanKeyPrefix+u).Result(); exists != 0 {
			t.Errorf("effective plan key for %s should be deleted", u)
		}
	}
	if exists, _ := client.Exists(ctx, hubPlanKeyPrefix+"hub-1").Result(); exists != 0 {
		t.Error("hub plan Redis key should be deleted")
	}
}

func TestInvalidateHubMembersSurfacesListError(t *testing.T) {
	t.Parallel()

	// Inability to list members is a hard failure — can't
	// invalidate what we don't know about. Callers relying on
	// fresh data after this call would silently see old values.
	ms := &errStore{listMembersErr: errors.New("pg conn")}
	registry := plans.NewRegistry(ms)
	r := New(nil, ms, registry)

	if err := r.InvalidateHubMembers(context.Background(), "hub-1"); err == nil {
		t.Fatal("expected listMembers error to surface")
	}
}

// ─── GetHubMemoryLimit ──────────────────────────────────────────

func TestGetHubMemoryLimitEmptyHubUnlimited(t *testing.T) {
	t.Parallel()

	// Empty hub ID = no hub cap applies → return -1 (unlimited
	// sentinel). Matches the "personal hub has no hub cap"
	// docstring expectation.
	r := New(nil, &mockResolverStore{}, plans.NewRegistry(&mockResolverStore{}))
	if got := r.GetHubMemoryLimit(context.Background(), ""); got != -1 {
		t.Errorf("empty hub: got %d, want -1", got)
	}
}

func TestGetHubMemoryLimitPlanFallback(t *testing.T) {
	t.Parallel()

	// A store that reports a hub plan ID the registry doesn't know
	// about, plus a hub_free_team plan in its plan list. Resolver
	// should fall through to hub_free_team's MemoryLimit.
	ms := &hubLimitFallbackStore{
		mockResolverStore: mockResolverStore{hubPlanID: ""},
		plansList: []model.Plan{
			{
				ID:          model.HubFreeTeamPlanID,
				Scope:       model.PlanScopeHub,
				DisplayName: "Free Team",
				MemoryLimit: 500,
				Active:      true,
			},
		},
	}
	registry := plans.NewRegistry(ms)
	_ = registry.Load(context.Background())

	r := New(nil, ms, registry)
	got := r.GetHubMemoryLimit(context.Background(), "hub-1")
	if got != 500 {
		t.Errorf("expected fallback to hub_free_team MemoryLimit=500, got %d", got)
	}
}

// hubLimitFallbackStore injects the hub_free_team plan into the
// registry via ListPlans for the fallback test above — the real
// InMemoryStore's ListPlans returns nil.
type hubLimitFallbackStore struct {
	mockResolverStore
	plansList []model.Plan
}

func (s *hubLimitFallbackStore) ListPlans(_ context.Context) ([]model.Plan, error) {
	return s.plansList, nil
}

func TestGetHubMemoryLimitFailOpenWhenPlanMissing(t *testing.T) {
	t.Parallel()

	// Registry has NO plans — the "absolute fallback" branch
	// returns -1 so pushes don't block while ops diagnose.
	ms := &mockResolverStore{hubPlanID: ""}
	r := New(nil, ms, plans.NewRegistry(ms))
	if got := r.GetHubMemoryLimit(context.Background(), "hub-1"); got != -1 {
		t.Errorf("fail-open expected, got %d", got)
	}
}

// ─── Test doubles ───────────────────────────────────────────────

// errStore is a store that returns errors for the narrow methods
// the invalidate paths touch. Uses the existing mockResolverStore
// as a base so unspecified methods have sane no-op behavior.
type errStore struct {
	mockResolverStore

	setErr         error
	listMembersErr error
}

func (e *errStore) SetEffectivePlan(_ context.Context, _, _, _ string) error {
	if e.setErr != nil {
		return e.setErr
	}
	return nil
}

func (e *errStore) ListHubMembers(_ string) ([]model.HubMember, error) {
	if e.listMembersErr != nil {
		return nil, e.listMembersErr
	}
	return nil, nil
}
