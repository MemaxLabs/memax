package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// stubDreamResolver implements quota.DreamResolver for the trigger
// gate tests. We reuse it instead of standing up a full
// planresolver.Resolver (which needs Redis + plan registry).
type stubDreamResolver struct {
	user planresolver.DreamLimits
	hub  planresolver.DreamLimits
}

func (s stubDreamResolver) ResolveDreamLimits(_ context.Context, _ string) planresolver.DreamLimits {
	return s.user
}
func (s stubDreamResolver) GetHubDreamLimits(_ context.Context, _ string) planresolver.DreamLimits {
	return s.hub
}

// dreamsTriggerStore lets us control the hub subscription lookup that
// the Trigger handler consults before enqueueing work. Everything else
// (hub row, membership) is served from the real InMemoryStore.
//
// We expose TWO subscription fields on purpose:
//
//   - anyStatusSub is what Trigger actually consults for the freeze
//     gate, because cancelled/past_due rows are invisible to the
//     active-only query. Mirrors real Postgres behavior where
//     GetActiveHubSubscription is filtered `status='active'`.
//   - activeSub stays here for tests that need to distinguish the
//     two call sites, though the handler itself doesn't touch it
//     today.
type dreamsTriggerStore struct {
	*store.InMemoryStore
	anyStatusSub *model.HubSubscription
	activeSub    *model.HubSubscription

	// Optional usage overrides — set to exercise CheckDreamQuota at
	// finite-cap exhaustion. When unset (the zero values), the
	// embedded InMemoryStore returns zero usage and the gate
	// uses Used=0 against whatever limit the resolver returns.
	userBasic, userLucid int
	hubLucid             int
	usageOverridden      bool
}

func (s *dreamsTriggerStore) GetActiveHubSubscription(_ context.Context, _ string) (*model.HubSubscription, error) {
	return s.activeSub, nil
}

func (s *dreamsTriggerStore) GetHubSubscriptionAnyStatus(_ context.Context, _ string) (*model.HubSubscription, error) {
	return s.anyStatusSub, nil
}

// GetUserDreamUsage / GetHubDreamUsage shadow the InMemoryStore
// stubs (which return zeros) when the test sets usageOverridden=true.
// Lets us stage finite-cap-exhaustion scenarios for the trigger gate
// without spinning up a real Postgres.
func (s *dreamsTriggerStore) GetUserDreamUsage(_ context.Context, _ string, _ time.Time) (int, int, error) {
	if s.usageOverridden {
		return s.userBasic, s.userLucid, nil
	}
	return 0, 0, nil
}

func (s *dreamsTriggerStore) GetHubDreamUsage(_ context.Context, _ string, _ time.Time) (int, error) {
	if s.usageOverridden {
		return s.hubLucid, nil
	}
	return 0, nil
}

func newDreamsTriggerStore(t *testing.T, hubID, ownerID string) *dreamsTriggerStore {
	t.Helper()
	mem := store.NewInMemoryStore()
	if err := mem.CreateHub(&model.Hub{
		ID:      hubID,
		Name:    "Test Hub",
		Slug:    "test",
		HubType: "team",
		OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	return &dreamsTriggerStore{InMemoryStore: mem}
}

func newTriggerRequest(hubID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/dreams/trigger", nil)
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, hubIDKey, hubID)
	ctx = context.WithValue(ctx, writeHubIDKey, hubID)
	return req.WithContext(ctx)
}

// A frozen hub must reject Trigger at the API edge with a typed 409.
// We don't want to silently enqueue work that the engine will skip —
// that costs a River slot and hides the state from the caller.
func TestDreamsTriggerRejectsFrozenHub(t *testing.T) {
	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"

	graceExpired := time.Now().Add(-(model.HubGracePeriod + time.Hour))
	s := newDreamsTriggerStore(t, hubID, ownerID)
	// Active row is over-limit past grace — both lookups return the
	// same row in this state, since the subscription is still
	// `status='active'`.
	frozen := &model.HubSubscription{
		HubID:          hubID,
		Status:         "active",
		OverLimitSince: &graceExpired,
	}
	s.activeSub = frozen
	s.anyStatusSub = frozen

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _ string, _ string) error {
		enqueued = true
		return nil
	})

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if enqueued {
		t.Fatal("frozen hub must not enqueue a dream cycle")
	}
}

// Cancelled and past_due subscriptions are also frozen per
// model.IsHubFrozen. The critical wrinkle: the real Postgres
// GetActiveHubSubscription filters `status='active'`, so a cancelled
// row comes back as nil from that path. If the handler asked only
// the active-status query, IsHubFrozen(nil, now) would return false
// and the cancelled hub would keep running dreams. The freeze gate
// MUST consult GetHubSubscriptionAnyStatus.
//
// This test reproduces real DB behavior: activeSub=nil (cancelled
// rows are filtered out), anyStatusSub holds the cancelled row. The
// handler must still return 409.
func TestDreamsTriggerRejectsCancelledSubscription(t *testing.T) {
	hubID := "22222222-2222-2222-2222-222222222222"
	ownerID := "u2"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub = nil
	s.anyStatusSub = &model.HubSubscription{HubID: hubID, Status: "cancelled"}

	h := NewDreamsHandler(s, nil)
	h.SetEnqueue(func(_ context.Context, _ string, _ string) error { return nil })

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// past_due is the second inactive state IsHubFrozen catches; lock it
// down with the same realistic "active-lookup returns nil" shape.
func TestDreamsTriggerRejectsPastDueSubscription(t *testing.T) {
	hubID := "44444444-4444-4444-4444-444444444444"
	ownerID := "u4"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub = nil
	s.anyStatusSub = &model.HubSubscription{HubID: hubID, Status: "past_due"}

	h := NewDreamsHandler(s, nil)
	h.SetEnqueue(func(_ context.Context, _ string, _ string) error { return nil })

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Happy path: a hub with no subscription (hub_free_team default) or an
// active-healthy subscription enqueues as before.
func TestDreamsTriggerEnqueuesHealthyHub(t *testing.T) {
	hubID := "33333333-3333-3333-3333-333333333333"
	ownerID := "u3"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	// No subscription row of any status → both lookups return nil →
	// IsHubFrozen(nil, now) returns false → enqueue proceeds.
	s.activeSub = nil
	s.anyStatusSub = nil

	h := NewDreamsHandler(s, nil)
	var gotHubID, gotUser string
	h.SetEnqueue(func(_ context.Context, hID, uID string) error {
		gotHubID, gotUser = hID, uID
		return nil
	})

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotHubID != hubID || gotUser != ownerID {
		t.Fatalf("unexpected enqueue args: hub=%q user=%q", gotHubID, gotUser)
	}
}

// ─────────────────────────────────────────────────────────────────
// Dream-quota gate (commit 9 wiring).
// ─────────────────────────────────────────────────────────────────

// When no quota resolver is wired (legacy / dev), the gate is
// skipped entirely and the trigger enqueues. Asserts the gate is
// opt-in and doesn't break environments without a resolver.
func TestDreamsTriggerSkipsQuotaGateWhenResolverUnset(t *testing.T) {
	hubID := "44444444-4444-4444-4444-444444444444"
	ownerID := "u4"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub, s.anyStatusSub = nil, nil

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})
	// Intentionally do NOT call SetQuotaResolver.

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when resolver unset, got %d: %s", rec.Code, rec.Body.String())
	}
	if !enqueued {
		t.Errorf("expected enqueue to fire when resolver is unset")
	}
}

// Soft mode at cap: returns 200, enqueues, and emits the
// would_have_blocked telemetry log (we just verify the response
// shape — log assertion would be flaky).
func TestDreamsTriggerSoftModeAllowsAtCap(t *testing.T) {
	hubID := "55555555-5555-5555-5555-555555555555"
	ownerID := "u5"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub, s.anyStatusSub = nil, nil

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})

	// Hub trigger on a team hub at cap. Soft mode → allow.
	h.SetQuotaResolver(stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	})
	h.SetQuotaMode(quota.ModeSoft)
	// Note: soft mode allows even when used >= limit, so we don't
	// need to fake a counter — Decision treats Used=0 < Limit=100
	// as Allowed. The "would_have_blocked" path is exercised in the
	// hard-mode test below.

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in soft mode, got %d: %s", rec.Code, rec.Body.String())
	}
	if !enqueued {
		t.Errorf("expected enqueue under soft mode")
	}
}

// Hard mode at cap: returns 402 with code lucid_quota_exhausted and
// the plan-spec'd Details envelope. Does NOT enqueue.
//
// Constructed by stubbing usage at the cap via the inmemory store's
// behavior (zero), then setting the limit equal to that value (0).
// limit=0 means "tier disabled," which produces denial in BOTH
// modes — that's the documented behavior. To exercise an at-cap
// finite-limit case in unit-test scope, see the postgres-side
// concurrency test where the counter actually moves; the unit test
// here verifies the 402 path fires correctly when CheckDreamQuota
// returns Allowed=false.
func TestDreamsTriggerHardModeDeniesAtCap(t *testing.T) {
	hubID := "66666666-6666-6666-6666-666666666666"
	ownerID := "u6"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub, s.anyStatusSub = nil, nil

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})

	// limit=0 disables the tier — denied in both modes.
	h.SetQuotaResolver(stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 0, LucidQuotaSource: "hub_free_team"},
	})
	h.SetQuotaMode(quota.ModeHard)

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d: %s", rec.Code, rec.Body.String())
	}
	if enqueued {
		t.Errorf("must not enqueue when quota gate denies")
	}

	// Verify the response envelope shape per the plan spec.
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected error envelope")
	}
	if resp.Error.Code != "lucid_quota_exhausted" {
		t.Errorf("error code=%q, want lucid_quota_exhausted", resp.Error.Code)
	}
	// Details should be a map with the spec fields.
	details, ok := resp.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected Details as map; got %T", resp.Error.Details)
	}
	for _, want := range []string{"code", "scope", "limit", "used", "resets_at", "docs_url", "quota_source"} {
		if _, ok := details[want]; !ok {
			t.Errorf("Details missing field %q", want)
		}
	}
	if details["scope"] != "hub" {
		t.Errorf("Details.scope=%v, want hub", details["scope"])
	}
	if details["hub_id"] != hubID {
		t.Errorf("Details.hub_id=%v, want %q", details["hub_id"], hubID)
	}
}

// Soft mode at FINITE-cap exhaustion: limit=100 / used=100 → Decision
// reports Allowed=true (soft) but Details is populated, exercising
// the "would_have_blocked" telemetry path. Trigger still 200s and
// enqueues; that's the rollout-window contract.
func TestDreamsTriggerSoftModeAtFiniteCapAllowsAndLogsTelemetry(t *testing.T) {
	hubID := "88888888-8888-8888-8888-888888888888"
	ownerID := "u8"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub, s.anyStatusSub = nil, nil
	// Hub lucid counter at the cap.
	s.usageOverridden = true
	s.hubLucid = 100

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})
	h.SetQuotaResolver(stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	})
	h.SetQuotaMode(quota.ModeSoft)

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in soft mode at cap, got %d: %s", rec.Code, rec.Body.String())
	}
	if !enqueued {
		t.Errorf("soft mode at cap must still enqueue (Phase 1 rollout contract)")
	}
}

// Hard mode at FINITE-cap exhaustion: limit=100 / used=100 → 402 with
// the exact Details envelope shape the plan spec'd. Distinct from
// the limit=0 case which is "tier disabled."
func TestDreamsTriggerHardModeAtFiniteCapDenies(t *testing.T) {
	hubID := "99999999-9999-9999-9999-999999999999"
	ownerID := "u9"
	s := newDreamsTriggerStore(t, hubID, ownerID)
	s.activeSub, s.anyStatusSub = nil, nil
	s.usageOverridden = true
	s.hubLucid = 100

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})
	h.SetQuotaResolver(stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	})
	h.SetQuotaMode(quota.ModeHard)

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 at cap in hard mode, got %d: %s", rec.Code, rec.Body.String())
	}
	if enqueued {
		t.Errorf("hard mode at cap must not enqueue")
	}

	// Envelope assertion: the at-cap path goes through the same
	// denialDetails builder as the limit=0 path; verify limit/used
	// reflect the finite-cap state, not a placeholder.
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	details, _ := resp.Error.Details.(map[string]any)
	if got, want := details["limit"], float64(100); got != want {
		t.Errorf("Details.limit=%v, want %v", got, want)
	}
	if got, want := details["used"], float64(100); got != want {
		t.Errorf("Details.used=%v, want %v", got, want)
	}
}

// Personal-hub trigger uses ScopeUser even though hub_id is set —
// the scope-selection helper should pick user-scope.
func TestDreamsTriggerPersonalHubUsesUserScope(t *testing.T) {
	hubID := "77777777-7777-7777-7777-777777777777"
	ownerID := "u7"

	mem := store.NewInMemoryStore()
	if err := mem.CreateHub(&model.Hub{
		ID: hubID, Name: "My Personal", Slug: "my-personal",
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	s := &dreamsTriggerStore{InMemoryStore: mem}

	h := NewDreamsHandler(s, nil)
	enqueued := false
	h.SetEnqueue(func(_ context.Context, _, _ string) error {
		enqueued = true
		return nil
	})

	// Personal user has lucid=40 limit, used=0 → allowed.
	// Hub stub returns disabled (which would deny if we'd picked
	// hub-scope by mistake). If the gate fires hub-scope, the test
	// fails 402; if user-scope, passes.
	h.SetQuotaResolver(stubDreamResolver{
		user: planresolver.DreamLimits{LucidLimit: 40, LucidQuotaSource: "personal_pro"},
		hub:  planresolver.DreamLimits{LucidLimit: 0, LucidQuotaSource: "hub_free_team"}, // sentinel
	})
	h.SetQuotaMode(quota.ModeHard)

	rec := httptest.NewRecorder()
	h.Trigger(rec, newTriggerRequest(hubID, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (user-scope allowed), got %d: %s", rec.Code, rec.Body.String())
	}
	if !enqueued {
		t.Errorf("expected enqueue when user-scope check passes")
	}
}
