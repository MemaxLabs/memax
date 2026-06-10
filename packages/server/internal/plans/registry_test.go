package plans

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// fakeStore stubs the narrow slice of store.Store that the plans
// package needs. Embedding InMemoryStore gives us every other
// method for free; we only override ListPlans and
// GetPlanOverride, the two the registry actually calls.
//
// Concurrency-safe via atomic pointers so the background reload
// loop doesn't race the test goroutine.
type fakeStore struct {
	*store.InMemoryStore

	plansList   atomic.Pointer[[]model.Plan]
	listErr     atomic.Pointer[error]
	override    atomic.Pointer[model.PlanOverride]
	overrideErr atomic.Pointer[error]

	listCalls atomic.Int64
}

func newFakeStore(plans []model.Plan) *fakeStore {
	f := &fakeStore{InMemoryStore: store.NewInMemoryStore()}
	f.plansList.Store(&plans)
	return f
}

func (f *fakeStore) ListPlans(_ context.Context) ([]model.Plan, error) {
	f.listCalls.Add(1)
	if errPtr := f.listErr.Load(); errPtr != nil && *errPtr != nil {
		return nil, *errPtr
	}
	ptr := f.plansList.Load()
	if ptr == nil {
		return nil, nil
	}
	return *ptr, nil
}

func (f *fakeStore) GetPlanOverride(_ context.Context, _ string) (*model.PlanOverride, error) {
	if errPtr := f.overrideErr.Load(); errPtr != nil && *errPtr != nil {
		return nil, *errPtr
	}
	return f.override.Load(), nil
}

// Seed plans used by most tests. Kept minimal — specific tests
// override individual fields by swapping in their own slice.
func testPlans() []model.Plan {
	return []model.Plan{
		{
			ID:              "personal_free",
			Scope:           model.PlanScopePersonal,
			DisplayName:     "Free",
			TierOrder:       0,
			EntitlementRank: 0,
			MemoryLimit:     300,
			PushLimit:       200,
			RecallLimit:     500,
			AskLimit:        10,
			AskModel:        "haiku",
			RateLimitRPM:    60,
			Active:          true,
			Features:        map[string]any{"betafeature": true},
		},
		{
			ID:              "personal_early_access",
			Scope:           model.PlanScopePersonal,
			DisplayName:     "Early Access",
			TierOrder:       15,
			EntitlementRank: 15,
			MemoryLimit:     10000,
			PushLimit:       2000,
			RecallLimit:     -1,
			AskLimit:        150,
			AskModel:        "sonnet",
			RateLimitRPM:    120,
			Active:          true,
		},
		{
			ID:              "hub_team",
			Scope:           model.PlanScopeHub,
			DisplayName:     "Team",
			TierOrder:       25,
			EntitlementRank: 25,
			AskLimit:        500,
			AskModel:        "sonnet",
			Active:          true,
		},
		{
			ID:              "inactive_plan",
			Scope:           model.PlanScopePersonal,
			DisplayName:     "Inactive",
			TierOrder:       10,
			EntitlementRank: 10,
			Active:          false,
		},
	}
}

func TestLoadPopulatesRegistry(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	if err := r.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	p := r.GetPlan("personal_free")
	if p == nil {
		t.Fatal("expected personal_free plan to be loaded")
	}
	if p.DisplayName != "Free" {
		t.Errorf("DisplayName = %q, want Free", p.DisplayName)
	}
}

func TestLoadReturnsStoreError(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(nil)
	sentinel := errors.New("db down")
	fs.listErr.Store(&sentinel)
	r := NewRegistry(fs)
	if err := r.Load(context.Background()); err == nil {
		t.Fatal("expected Load to surface the store error")
	}
}

func TestGetPlanReturnsNilForUnknown(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())
	if p := r.GetPlan("nope"); p != nil {
		t.Errorf("unknown plan ID should return nil, got %+v", p)
	}
}

func TestGetPlanReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	p1 := r.GetPlan("personal_free")
	if p1 == nil {
		t.Fatal("expected plan")
	}
	// Mutating the returned copy must not affect subsequent reads.
	// If GetPlan returned the live pointer, this write would
	// leak into every other caller's view.
	p1.DisplayName = "HACKED"
	if p1.Features != nil {
		p1.Features["tampered"] = true
	}

	p2 := r.GetPlan("personal_free")
	if p2.DisplayName == "HACKED" {
		t.Errorf("GetPlan leaked mutable state: DisplayName = %q", p2.DisplayName)
	}
	if p2.Features != nil {
		if _, ok := p2.Features["tampered"]; ok {
			t.Error("GetPlan leaked mutable Features map")
		}
	}
}

func TestAllPlansSortedAndCopied(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	all := r.AllPlans()
	if len(all) != 4 {
		t.Fatalf("got %d plans, want 4", len(all))
	}
	// Scope ordering: "hub" < "personal" alphabetically, so the
	// first entry should be our hub plan.
	if all[0].Scope != model.PlanScopeHub {
		t.Errorf("first plan scope = %q, want hub (alphabetical sort)", all[0].Scope)
	}
	// Within personal scope, tier_order ascending.
	personal := r.PlansByScope(model.PlanScopePersonal)
	if len(personal) != 3 {
		t.Fatalf("got %d personal plans, want 3", len(personal))
	}
	for i := 1; i < len(personal); i++ {
		if personal[i-1].TierOrder > personal[i].TierOrder {
			t.Errorf("PlansByScope not sorted by tier_order: %d > %d at %d",
				personal[i-1].TierOrder, personal[i].TierOrder, i)
		}
	}
}

func TestPlansByScopeFiltersCorrectly(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	hub := r.PlansByScope(model.PlanScopeHub)
	if len(hub) != 1 {
		t.Errorf("hub plans: got %d, want 1", len(hub))
	}
	if hub[0].ID != "hub_team" {
		t.Errorf("got %q, want hub_team", hub[0].ID)
	}
}

func TestGetPlanByEntitlementRankPicksHighest(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	best := r.GetPlanByEntitlementRank("personal_free", "personal_early_access", "hub_team")
	if best == nil || best.ID != "hub_team" {
		t.Errorf("got %+v, want hub_team (rank 25)", best)
	}
}

func TestGetPlanByEntitlementRankIgnoresInactive(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	best := r.GetPlanByEntitlementRank("personal_free", "inactive_plan")
	if best == nil || best.ID != "personal_free" {
		t.Errorf("got %+v, want personal_free (inactive_plan should be skipped)", best)
	}
}

func TestGetPlanByEntitlementRankReturnsNilWhenAllUnknown(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	best := r.GetPlanByEntitlementRank("nope", "also_nope")
	if best != nil {
		t.Errorf("expected nil, got %+v", best)
	}
}

func TestGetUserLimitsResolvesPlan(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	limits := r.GetUserLimits(context.Background(), "u1", "personal_early_access")
	if limits.PlanID != "personal_early_access" {
		t.Errorf("PlanID = %q, want personal_early_access", limits.PlanID)
	}
	if limits.AskLimit != 150 {
		t.Errorf("AskLimit = %d, want 150", limits.AskLimit)
	}
	if limits.AskModel != "sonnet" {
		t.Errorf("AskModel = %q, want sonnet", limits.AskModel)
	}
}

func TestGetUserLimitsFallsBackViaLegacyToScopedMapping(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	// "early_access" is the legacy ID that maps to
	// "personal_early_access". Passing it should resolve via the
	// second branch of GetUserLimits's fallback chain.
	limits := r.GetUserLimits(context.Background(), "u1", model.LegacyEarlyAccessPlanID)
	if limits.PlanID != "personal_early_access" {
		t.Errorf("legacy-to-scoped fallback failed: got %q", limits.PlanID)
	}
}

func TestGetUserLimitsFallsBackToPersonalFreeForUnknownPlan(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	limits := r.GetUserLimits(context.Background(), "u1", "does-not-exist")
	if limits.PlanID != "personal_free" {
		t.Errorf("got %q, want personal_free (unknown plan fallback)", limits.PlanID)
	}
}

func TestGetUserLimitsFallsBackToHardcodedFreeWhenRegistryEmpty(t *testing.T) {
	t.Parallel()

	// Empty plan list — every fallback (by id, by legacy, by
	// personal_free, by free) misses. GetUserLimits must still
	// return something sensible so callers don't receive zero
	// limits that would trip rate limiters to "deny everything".
	fs := newFakeStore([]model.Plan{})
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	limits := r.GetUserLimits(context.Background(), "u1", "anything")
	if limits.PlanID != model.PersonalFreePlanID {
		t.Errorf("PlanID = %q, want hardcoded personal_free fallback", limits.PlanID)
	}
	if limits.AskLimit != 10 {
		t.Errorf("AskLimit = %d, want 10 (hardcoded free tier)", limits.AskLimit)
	}
	if limits.MemoryLimit != 300 {
		t.Errorf("MemoryLimit = %d, want 300 (hardcoded free tier)", limits.MemoryLimit)
	}
}

func TestGetUserLimitsAppliesOverrides(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	override := &model.PlanOverride{
		UserID:    "u1",
		Overrides: map[string]any{"ask_limit": 9999.0},
	}
	fs.override.Store(override)

	limits := r.GetUserLimits(context.Background(), "u1", "personal_free")
	if limits.AskLimit != 9999 {
		t.Errorf("AskLimit = %d, want 9999 (override should apply)", limits.AskLimit)
	}
	if limits.PushLimit != 200 {
		t.Errorf("PushLimit = %d, want 200 (non-overridden field preserved)", limits.PushLimit)
	}
}

func TestGetUserLimitsIgnoresOverrideFetchError(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	sentinel := errors.New("override store down")
	fs.overrideErr.Store(&sentinel)

	// Plan resolution must still succeed — a transient override
	// fetch failure should not cascade into "every request fails"
	// because a user without any override is the common case.
	limits := r.GetUserLimits(context.Background(), "u1", "personal_free")
	if limits.PlanID != "personal_free" {
		t.Errorf("got %q, want personal_free — override fetch error shouldn't block resolution", limits.PlanID)
	}
}

func TestApplyOverridesSkipsEmpty(t *testing.T) {
	t.Parallel()

	base := model.UserLimits{PlanID: "x", AskLimit: 10}
	ApplyOverrides(&base, nil)
	if base.AskLimit != 10 {
		t.Errorf("empty overrides must not mutate limits; AskLimit = %d", base.AskLimit)
	}
	ApplyOverrides(&base, map[string]any{})
	if base.AskLimit != 10 {
		t.Errorf("empty-map overrides must not mutate limits; AskLimit = %d", base.AskLimit)
	}
}

func TestApplyOverridesMergesKnownFields(t *testing.T) {
	t.Parallel()

	limits := model.UserLimits{PlanID: "x", AskLimit: 10, PushLimit: 50}
	ApplyOverrides(&limits, map[string]any{
		"ask_limit":     42.0,
		"unknown_field": "ignored",
	})
	if limits.AskLimit != 42 {
		t.Errorf("AskLimit = %d, want 42", limits.AskLimit)
	}
	if limits.PushLimit != 50 {
		t.Errorf("PushLimit = %d, want 50 (unchanged)", limits.PushLimit)
	}
}

func TestReloadReplacesPlans(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	newPlans := []model.Plan{{ID: "new_plan", Active: true, Scope: model.PlanScopePersonal}}
	fs.plansList.Store(&newPlans)
	if err := r.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p := r.GetPlan("personal_free"); p != nil {
		t.Error("reload should have evicted old plans")
	}
	if p := r.GetPlan("new_plan"); p == nil {
		t.Error("reload should have loaded new plans")
	}
}

func TestStartBackgroundReload(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	// Reload should fire at roughly each interval tick. We use a
	// 5ms interval (tight enough to observe within test budget,
	// loose enough not to busy-spin) and wait for at least two
	// ticks to land. Tolerance > 2 ticks so a loaded CI slot can
	// still pass.
	initial := fs.listCalls.Load()
	cancel := r.Start(context.Background(), 5*time.Millisecond)
	t.Cleanup(cancel)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fs.listCalls.Load() >= initial+2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("background reload never fired; calls went from %d to %d", initial, fs.listCalls.Load())
}

func TestStartRespectsCancel(t *testing.T) {
	t.Parallel()

	fs := newFakeStore(testPlans())
	r := NewRegistry(fs)
	_ = r.Load(context.Background())

	cancel := r.Start(context.Background(), 5*time.Millisecond)
	// Let a couple ticks land.
	time.Sleep(20 * time.Millisecond)
	beforeCancel := fs.listCalls.Load()
	cancel()
	// Give the goroutine time to notice cancellation.
	time.Sleep(30 * time.Millisecond)
	after := fs.listCalls.Load()
	// Allow a single extra call for the tick that was mid-flight
	// when cancel fired.
	if after > beforeCancel+1 {
		t.Errorf("reloads continued after cancel: before=%d after=%d", beforeCancel, after)
	}
}
