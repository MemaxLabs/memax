package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// newUsageRequest builds a GET /v1/usage/dreams request with the
// auth context shape a normal web session has — user-scope, with
// PermDreamRead granted on each of `accessHubs`. The optional
// hubIDQuery is appended as ?hub_id=…; pass "" to omit.
func newUsageRequest(userID, hubIDQuery string, accessHubs ...string) *http.Request {
	url := "/v1/usage/dreams"
	if hubIDQuery != "" {
		url += "?hub_id=" + hubIDQuery
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	permsByHub := make(map[string]PermissionSet, len(accessHubs))
	for _, h := range accessHubs {
		permsByHub[h] = NewPermissionSet(PermDreamRead)
	}
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:           userID,
		PrincipalType:    "session",
		HubScopeMode:     HubScopeAllAccessible,
		PermissionsByHub: permsByHub,
	})
	return req.WithContext(ctx)
}

// newUsageHandler wires the dreams handler with the standard
// trigger-test store + a stub resolver. Mirrors the trigger tests'
// boilerplate, only without the enqueue function (Usage never
// enqueues).
func newUsageHandler(s *dreamsTriggerStore, resolver quota.DreamResolver, mode quota.Mode) *DreamsHandler {
	h := NewDreamsHandler(s, nil)
	if resolver != nil {
		h.SetQuotaResolver(resolver)
		h.SetQuotaMode(mode)
	}
	return h
}

// Personal Basic — Free user, no hub_id, basic counter at 5/40.
// Allowed=true, tier=basic, scope=user, no hub_id field.
func TestDreamsGetUsage_PersonalBasic(t *testing.T) {
	userID := "u-basic"
	hubID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	s := newDreamsTriggerStore(t, hubID, userID)
	s.usageOverridden = true
	s.userBasic = 5

	h := newUsageHandler(s, stubDreamResolver{
		user: planresolver.DreamLimits{BasicLimit: 40, BasicQuotaSource: "personal_free"},
	}, quota.ModeHard)

	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.Scope != "personal" {
		t.Errorf("scope=%q, want personal", got.Scope)
	}
	if got.HubID != "" {
		t.Errorf("hub_id=%q, want empty for personal scope", got.HubID)
	}
	if got.Tier != "basic" {
		t.Errorf("tier=%q, want basic", got.Tier)
	}
	if got.Limit != 40 || got.Used != 5 || derefInt(got.Remaining) != 35 {
		t.Errorf("limits: got limit=%d used=%d remaining=%v, want 40/5/35", got.Limit, got.Used, got.Remaining)
	}
	if !got.Allowed {
		t.Errorf("allowed=false, want true at 5/40")
	}
	if got.QuotaSource != "personal_free" {
		t.Errorf("quota_source=%q, want personal_free", got.QuotaSource)
	}
	if got.Mode != "hard" {
		t.Errorf("mode=%q, want hard", got.Mode)
	}
}

// Personal Lucid — Pro user, no hub_id, lucid counter at 10/40.
// Tier disambiguation prefers Lucid when LucidLimit > 0.
func TestDreamsGetUsage_PersonalLucid(t *testing.T) {
	userID := "u-lucid"
	hubID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	s := newDreamsTriggerStore(t, hubID, userID)
	s.usageOverridden = true
	s.userLucid = 10
	s.userBasic = 99 // sentinel: must NOT be reported when Lucid is active

	h := newUsageHandler(s, stubDreamResolver{
		user: planresolver.DreamLimits{
			LucidLimit:       40,
			LucidQuotaSource: "personal_pro",
			BasicLimit:       40,
			BasicQuotaSource: "personal_free",
		},
	}, quota.ModeHard)

	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.Tier != "lucid" {
		t.Errorf("tier=%q, want lucid (Lucid wins disambiguation)", got.Tier)
	}
	if got.Used != 10 {
		t.Errorf("used=%d, want 10 (lucid counter, NOT basic 99)", got.Used)
	}
	if got.Limit != 40 || derefInt(got.Remaining) != 30 {
		t.Errorf("got limit=%d remaining=%v, want 40/30", got.Limit, got.Remaining)
	}
	if got.QuotaSource != "personal_pro" {
		t.Errorf("quota_source=%q, want personal_pro", got.QuotaSource)
	}
}

// Team hub — hub_id targets a team hub, scope=hub, hub_id populated.
func TestDreamsGetUsage_TeamHubScope(t *testing.T) {
	userID := "u-team"
	hubID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	s := newDreamsTriggerStore(t, hubID, userID) // creates a 'team' hub
	s.usageOverridden = true
	s.hubLucid = 25

	h := newUsageHandler(s, stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	}, quota.ModeHard)

	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, hubID, hubID))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.Scope != "hub" {
		t.Errorf("scope=%q, want hub", got.Scope)
	}
	if got.HubID != hubID {
		t.Errorf("hub_id=%q, want %q", got.HubID, hubID)
	}
	if got.Tier != "lucid" || got.Limit != 100 || got.Used != 25 || derefInt(got.Remaining) != 75 {
		t.Errorf("got tier=%q limit=%d used=%d remaining=%v, want lucid/100/25/75",
			got.Tier, got.Limit, got.Used, got.Remaining)
	}
	if got.QuotaSource != "hub_team" {
		t.Errorf("quota_source=%q, want hub_team", got.QuotaSource)
	}
}

// Unlimited (Limit=-1) — Remaining is omitted from the JSON, Allowed
// is true regardless of Used.
func TestDreamsGetUsage_Unlimited(t *testing.T) {
	userID := "u-unlim"
	hubID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	s := newDreamsTriggerStore(t, hubID, userID)
	s.usageOverridden = true
	s.userLucid = 1000 // wildly above any finite cap

	h := newUsageHandler(s, stubDreamResolver{
		user: planresolver.DreamLimits{LucidLimit: -1, LucidQuotaSource: "personal_unlimited"},
	}, quota.ModeHard)

	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.Limit != -1 {
		t.Errorf("limit=%d, want -1 (unlimited)", got.Limit)
	}
	if !got.Allowed {
		t.Errorf("allowed=false, want true for unlimited")
	}
	if got.Remaining != nil {
		t.Errorf("Remaining=%v, want nil for unlimited", *got.Remaining)
	}
	// Belt-and-braces: the JSON must omit the field for unlimited
	// (otherwise clients see remaining: null which is a different
	// shape from an absent key).
	var raw struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, present := raw.Data["remaining"]; present {
		t.Errorf("JSON includes remaining for unlimited; should be omitted")
	}
}

// Disabled (Limit=0) — Allowed=false in BOTH soft and hard modes.
// "Disabled" means no entitlement, distinct from "exhausted finite".
func TestDreamsGetUsage_DisabledTier(t *testing.T) {
	userID := "u-disabled"
	hubID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"

	for _, mode := range []quota.Mode{quota.ModeHard, quota.ModeSoft} {
		t.Run(string(mode), func(t *testing.T) {
			s := newDreamsTriggerStore(t, hubID, userID)
			h := newUsageHandler(s, stubDreamResolver{
				hub: planresolver.DreamLimits{LucidLimit: 0, LucidQuotaSource: "hub_free_team"},
			}, mode)

			rec := httptest.NewRecorder()
			h.GetUsage(rec, newUsageRequest(userID, hubID, hubID))

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			got := decodeUsage(t, rec.Body.Bytes())
			if got.Limit != 0 {
				t.Errorf("limit=%d, want 0", got.Limit)
			}
			if got.Allowed {
				t.Errorf("allowed=true in mode=%s, want false (disabled tier never allows)", mode)
			}
		})
	}
}

// Exhausted finite cap — limit=100, used=100. Hard mode reports
// Allowed=false; soft mode reports Allowed=true (Phase 1 rollout
// contract: never block on cap exhaustion in soft).
func TestDreamsGetUsage_ExhaustedCap(t *testing.T) {
	userID := "u-exh"
	hubID := "ffffffff-ffff-ffff-ffff-ffffffffffff"

	cases := []struct {
		mode        quota.Mode
		wantAllowed bool
	}{
		{quota.ModeHard, false},
		{quota.ModeSoft, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			s := newDreamsTriggerStore(t, hubID, userID)
			s.usageOverridden = true
			s.hubLucid = 100

			h := newUsageHandler(s, stubDreamResolver{
				hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
			}, tc.mode)

			rec := httptest.NewRecorder()
			h.GetUsage(rec, newUsageRequest(userID, hubID, hubID))

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			got := decodeUsage(t, rec.Body.Bytes())
			if got.Used != 100 || got.Limit != 100 || derefInt(got.Remaining) != 0 {
				t.Errorf("got limit=%d used=%d remaining=%v, want 100/100/0",
					got.Limit, got.Used, got.Remaining)
			}
			// Defense against the omitempty regression: the JSON must
			// CONTAIN remaining: 0 for an exhausted finite cap, not
			// drop it. A missing field would conflate "exhausted" with
			// "unlimited" client-side.
			var raw struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatalf("decode raw: %v", err)
			}
			if v, present := raw.Data["remaining"]; !present {
				t.Errorf("JSON missing remaining for exhausted finite cap; clients can't distinguish from unlimited")
			} else if v != float64(0) {
				t.Errorf("remaining=%v, want 0", v)
			}
			if got.Allowed != tc.wantAllowed {
				t.Errorf("mode=%s allowed=%v, want %v", tc.mode, got.Allowed, tc.wantAllowed)
			}
		})
	}
}

// Unauthorized hub — caller asks about a hub they don't have
// PermDreamRead on. Returns 403, never leaks the hub's quota state.
func TestDreamsGetUsage_UnauthorizedHub(t *testing.T) {
	userID := "u-noaccess"
	myHubID := "11111111-2222-3333-4444-555555555555"
	otherHubID := "99999999-8888-7777-6666-555555555555"
	s := newDreamsTriggerStore(t, myHubID, "owner-of-other")
	// Ensure the other hub also exists in the store so SelectScope
	// doesn't error out for a missing hub before we even hit authz.
	if err := s.InMemoryStore.CreateHub(&model.Hub{
		ID: otherHubID, Name: "Other", Slug: "other",
		HubType: "team", OwnerID: "owner-of-other",
	}); err != nil {
		t.Fatalf("seed other hub: %v", err)
	}

	h := newUsageHandler(s, stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 100, LucidQuotaSource: "hub_team"},
	}, quota.ModeHard)

	// Request asks about otherHubID; auth context only grants access
	// to myHubID. Authz must reject before quota lookup runs.
	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, otherHubID, myHubID))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// No resolver wired — Usage returns 503, same shape as Trigger when
// enqueue is unwired. Avoids fabricating quota state in environments
// without a configured plan stack.
func TestDreamsGetUsage_ResolverNotConfigured(t *testing.T) {
	userID := "u-nores"
	hubID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	s := newDreamsTriggerStore(t, hubID, userID)

	h := NewDreamsHandler(s, nil)
	// Intentionally do NOT call SetQuotaResolver.

	rec := httptest.NewRecorder()
	h.GetUsage(rec, newUsageRequest(userID, ""))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Hub-scoped credential ignoring user-supplied ?hub_id — even when
// the caller passes hub_id=hubB, the handler MUST resolve to the
// scoped credential's allowlisted hub. Without this, a token bound
// to hub A could probe other hubs' existence (lookups would still
// 403, but only AFTER the SelectScope hub fetch ran).
//
// This is the same belt-and-braces defense List has built in;
// commit history calls it out as load-bearing for scoped tokens.
func TestDreamsGetUsage_HubScopedTokenLockedToScopedHub(t *testing.T) {
	userID := "u-scoped-locked"
	scopedHubID := "scoped00-aaaa-bbbb-cccc-dddddddddddd"
	otherHubID := "other000-aaaa-bbbb-cccc-dddddddddddd"

	mem := store.NewInMemoryStore()
	for _, h := range []string{scopedHubID, otherHubID} {
		if err := mem.CreateHub(&model.Hub{ID: h, Name: "h", Slug: h, HubType: "team", OwnerID: userID}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	s := &dreamsTriggerStore{InMemoryStore: mem}
	s.usageOverridden = true
	s.hubLucid = 3 // counter for whatever hub the handler resolves to

	h := newUsageHandler(s, stubDreamResolver{
		hub: planresolver.DreamLimits{LucidLimit: 50, LucidQuotaSource: "hub_team"},
	}, quota.ModeHard)

	// Caller passes ?hub_id=otherHubID, but the credential is bound
	// to scopedHubID. The override MUST win.
	req := httptest.NewRequest(http.MethodGet, "/v1/usage/dreams?hub_id="+otherHubID, nil)
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:        userID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  HubScopeAllowlist,
		ScopedHubIDs:  []string{scopedHubID},
		PermissionsByHub: map[string]PermissionSet{
			scopedHubID: NewPermissionSet(PermDreamRead),
		},
	})

	rec := httptest.NewRecorder()
	h.GetUsage(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.HubID != scopedHubID {
		t.Errorf("hub_id=%q, want scoped %q (handler must NOT honor user-supplied hub_id for scoped credentials)",
			got.HubID, scopedHubID)
	}
}

// Hub-scoped credential WITHOUT ?hub_id — must default to the
// scoped hub, not silently report personal-scope quota. This was a
// real leak: a token bound to hub A could call /v1/usage/dreams
// without a query and learn the user's personal quota state.
// Mirrors the same defense that List has built in.
func TestDreamsGetUsage_HubScopedTokenDefaultsToScopedHub(t *testing.T) {
	userID := "u-scoped-default"
	scopedHubID := "scoped11-aaaa-bbbb-cccc-dddddddddddd"

	mem := store.NewInMemoryStore()
	if err := mem.CreateHub(&model.Hub{
		ID: scopedHubID, Name: "Scoped", Slug: "scoped",
		HubType: "team", OwnerID: userID,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := &dreamsTriggerStore{InMemoryStore: mem}
	s.usageOverridden = true
	s.hubLucid = 7

	// The handler MUST use the scoped hub even when ?hub_id is absent.
	// If the personal-scope path fired instead, the lucid_user fields
	// (zero) would dominate and we'd report user-scope. We pin the
	// hub-scope shape to verify the defense.
	h := newUsageHandler(s, stubDreamResolver{
		hub:  planresolver.DreamLimits{LucidLimit: 50, LucidQuotaSource: "hub_team"},
		user: planresolver.DreamLimits{}, // sentinel: would yield "tier=basic limit=0" if user-scope fired
	}, quota.ModeHard)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage/dreams", nil)
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:        userID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  HubScopeAllowlist,
		ScopedHubIDs:  []string{scopedHubID},
		PermissionsByHub: map[string]PermissionSet{
			scopedHubID: NewPermissionSet(PermDreamRead),
		},
	})

	rec := httptest.NewRecorder()
	h.GetUsage(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeUsage(t, rec.Body.Bytes())
	if got.Scope != "hub" {
		t.Errorf("scope=%q, want hub (scoped credential must default to its scoped hub)", got.Scope)
	}
	if got.HubID != scopedHubID {
		t.Errorf("hub_id=%q, want %q", got.HubID, scopedHubID)
	}
	if got.Used != 7 || got.Limit != 50 {
		t.Errorf("got used=%d limit=%d, want 7/50 (hub counter, not personal sentinel)",
			got.Used, got.Limit)
	}
}

// decodeUsage extracts the DreamUsage payload from an
// {data: {...}} envelope.
func decodeUsage(t *testing.T, body []byte) model.DreamUsage {
	t.Helper()
	var resp struct {
		Data model.DreamUsage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v: body=%s", err, string(body))
	}
	return resp.Data
}

// derefInt safely reads a *int that the handler returns nil for in
// the unlimited case. Test sites use it to assert on the underlying
// value when they expect a non-nil pointer.
func derefInt(p *int) int {
	if p == nil {
		return -999 // sentinel: unmistakable in test failure output
	}
	return *p
}
