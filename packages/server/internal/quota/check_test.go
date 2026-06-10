package quota

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
)

// stubResolver is an in-memory DreamResolver for unit tests.
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

// stubUsage is an in-memory DreamUsageStore for unit tests.
type stubUsage struct {
	userBasic, userLucid int
	hubLucid             int
}

func (u stubUsage) GetUserDreamUsage(_ context.Context, _ string, _ time.Time) (basic, lucid int, err error) {
	return u.userBasic, u.userLucid, nil
}
func (u stubUsage) GetHubDreamUsage(_ context.Context, _ string, _ time.Time) (lucid int, err error) {
	return u.hubLucid, nil
}

// ─────────────────────────────────────────────────────────────────
// Tier selection: Free user (basic only) vs Pro user (lucid).
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_FreeUserPicksBasicTier(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 40, LucidLimit: 0,
		BasicQuotaSource: "personal_free", LucidQuotaSource: "personal_free",
	}}
	store := stubUsage{userBasic: 5, userLucid: 0}

	d, err := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeUser, UserID: "user-1"}, ModeHard)
	if err != nil {
		t.Fatalf("CheckDreamQuota: %v", err)
	}
	if d.Tier != TierBasic {
		t.Errorf("Tier=%q, want basic", d.Tier)
	}
	if d.Limit != 40 || d.Used != 5 || d.Remaining != 35 {
		t.Errorf("limit=%d used=%d remaining=%d, want 40/5/35", d.Limit, d.Used, d.Remaining)
	}
	if !d.Allowed {
		t.Errorf("expected Allowed=true (5/40)")
	}
}

func TestCheckDreamQuota_ProUserPicksLucidTier(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: 40,
		BasicQuotaSource: "personal_pro", LucidQuotaSource: "personal_pro",
	}}
	store := stubUsage{userBasic: 0, userLucid: 17}

	d, err := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeUser, UserID: "user-2"}, ModeHard)
	if err != nil {
		t.Fatalf("CheckDreamQuota: %v", err)
	}
	if d.Tier != TierLucid {
		t.Errorf("Tier=%q, want lucid", d.Tier)
	}
	if d.Limit != 40 || d.Used != 17 || d.Remaining != 23 {
		t.Errorf("limit=%d used=%d remaining=%d, want 40/17/23", d.Limit, d.Used, d.Remaining)
	}
}

// ─────────────────────────────────────────────────────────────────
// Hard mode at cap → !Allowed; soft mode at cap → Allowed but
// Details populated for the telemetry banner.
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_HardAtCapDenies(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: 40,
		LucidQuotaSource: "personal_pro",
	}}
	store := stubUsage{userLucid: 40}

	d, _ := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeUser, UserID: "u"}, ModeHard)
	if d.Allowed {
		t.Errorf("expected !Allowed at cap")
	}
	if d.Details == nil {
		t.Fatalf("expected Details populated on denial")
	}
	if d.Details.Code != "lucid_quota_exhausted" {
		t.Errorf("Details.Code=%q, want lucid_quota_exhausted", d.Details.Code)
	}
	if d.Details.Scope != "personal" {
		t.Errorf("Details.Scope=%q, want personal", d.Details.Scope)
	}
	if d.Details.UpgradeURL == "" {
		t.Errorf("Details.UpgradeURL empty for personal scope")
	}
}

func TestCheckDreamQuota_SoftAtCapAllowsWithDetails(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: 40,
		LucidQuotaSource: "personal_pro",
	}}
	store := stubUsage{userLucid: 40}

	d, _ := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeUser, UserID: "u"}, ModeSoft)
	if !d.Allowed {
		t.Errorf("expected Allowed=true in soft mode at cap")
	}
	if d.Details == nil {
		t.Errorf("expected Details populated even in soft mode (drives the banner)")
	}
}

// ─────────────────────────────────────────────────────────────────
// Edge: limit=0 (tier disabled) — both modes deny.
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_LimitZeroDeniesBothModes(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: 0,
		BasicQuotaSource: "free", LucidQuotaSource: "free",
	}}
	store := stubUsage{}

	for _, mode := range []Mode{ModeHard, ModeSoft} {
		t.Run(string(mode), func(t *testing.T) {
			d, _ := CheckDreamQuota(context.Background(), resolver, store,
				Scope{Type: ScopeUser, UserID: "u"}, mode)
			if d.Allowed {
				t.Errorf("limit=0 must deny in %q mode", mode)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// Edge: limit=-1 (unlimited) always allows.
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_UnlimitedAllows(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{user: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: -1,
		LucidQuotaSource: "enterprise",
	}}
	// Pretend a million runs already happened; unlimited still allows.
	store := stubUsage{userLucid: 1_000_000}

	d, _ := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeUser, UserID: "u"}, ModeHard)
	if !d.Allowed {
		t.Errorf("unlimited must allow regardless of used count; got %+v", d)
	}
	if d.Limit != -1 {
		t.Errorf("Limit=%d, want -1", d.Limit)
	}
}

// ─────────────────────────────────────────────────────────────────
// Hub scope: always Tier=lucid, no UpgradeURL.
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_HubScopeAtCap(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{hub: planresolver.DreamLimits{
		BasicLimit: 0, LucidLimit: 100,
		LucidQuotaSource: "hub_team",
	}}
	store := stubUsage{hubLucid: 100}

	d, _ := CheckDreamQuota(context.Background(), resolver, store,
		Scope{Type: ScopeHub, HubID: "hub-1"}, ModeHard)
	if d.Tier != TierLucid {
		t.Errorf("hub scope Tier=%q, want lucid", d.Tier)
	}
	if d.Allowed {
		t.Errorf("expected !Allowed at hub cap")
	}
	if d.Details == nil {
		t.Fatalf("expected Details on denial")
	}
	if d.Details.Scope != "hub" || d.Details.HubID != "hub-1" {
		t.Errorf("Details.Scope=%q HubID=%q, want hub/hub-1", d.Details.Scope, d.Details.HubID)
	}
	if d.Details.UpgradeURL != "" {
		t.Errorf("hub scope must not surface UpgradeURL (hub upgrades go through a different flow)")
	}
}

// ─────────────────────────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────────────────────────

func TestCheckDreamQuota_ValidationErrors(t *testing.T) {
	t.Parallel()
	resolver := stubResolver{}
	store := stubUsage{}

	cases := []struct {
		name  string
		scope Scope
		mode  Mode
	}{
		{"invalid mode", Scope{Type: ScopeUser, UserID: "u"}, Mode("ridiculous")},
		{"empty scope type", Scope{UserID: "u"}, ModeHard},
		{"missing user ID", Scope{Type: ScopeUser}, ModeHard},
		{"missing hub ID", Scope{Type: ScopeHub}, ModeHard},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := CheckDreamQuota(context.Background(), resolver, store, c.scope, c.mode); err == nil {
				t.Errorf("expected validation error for %q", c.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────
// Tier helpers
// ─────────────────────────────────────────────────────────────────

func TestTier_IsLucid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tier Tier
		want bool
	}{
		{TierBasic, false},
		{TierLucid, true},
		{TierLucidPassive, true},
		{TierLucidActive, true},
	}
	for _, c := range cases {
		if c.tier.IsLucid() != c.want {
			t.Errorf("%q.IsLucid()=%v, want %v", c.tier, c.tier.IsLucid(), c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// ConsumeParams validation
// ─────────────────────────────────────────────────────────────────

func TestValidateConsumeParams_HubRejectsBasicTier(t *testing.T) {
	t.Parallel()
	p := ConsumeParams{
		DreamRunID:    "00000000-0000-0000-0000-000000000001",
		Scope:         Scope{Type: ScopeHub, HubID: "h"},
		Tier:          TierBasic, // illegal for hub scope
		TerminalState: StateCompleted,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().Add(time.Hour),
		Mode:          ModeHard,
		ResolvedLimit: 100,
		QuotaSource:   "hub_team",
	}
	if err := validateConsumeParams(p); err == nil {
		t.Fatal("expected validation error for hub+basic; got nil")
	}
}

func TestValidateConsumeParams_RejectsRaceLostFromCaller(t *testing.T) {
	t.Parallel()
	p := ConsumeParams{
		DreamRunID:    "00000000-0000-0000-0000-000000000002",
		Scope:         Scope{Type: ScopeUser, UserID: "u"},
		Tier:          TierLucidPassive,
		TerminalState: StateQuotaRaceLost, // internal-only
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().Add(time.Hour),
		Mode:          ModeHard,
		ResolvedLimit: 40,
		QuotaSource:   "personal_pro",
	}
	if err := validateConsumeParams(p); err == nil {
		t.Fatal("expected validation error for caller-supplied quota_race_lost; got nil")
	}
}

func TestTerminalState_Counted(t *testing.T) {
	t.Parallel()
	want := map[TerminalState]bool{
		StateCompleted:         true,
		StatePartialFailed:     true,
		StateLoopCapHit:        true,
		StateFailed:            false,
		StateSkippedConcurrent: false,
		StateNoMemories:        false,
		StateQuotaRaceLost:     false,
	}
	for state, w := range want {
		if got := state.Counted(); got != w {
			t.Errorf("%q.Counted()=%v, want %v", state, got, w)
		}
	}
}
