package model

import (
	"testing"
	"time"
)

// ─── Memory / retrieval normalization ────────────────────────────

func TestNormalizeMemoryKind(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		MemoryKindEpisodic:   MemoryKindEpisodic,
		MemoryKindSemantic:   MemoryKindSemantic,
		MemoryKindProcedural: MemoryKindProcedural,
		MemoryKindRationale:  MemoryKindRationale,
		"":                   MemoryKindSemantic, // default
		"garbage":            MemoryKindSemantic, // unknown → default
		"EPISODIC":           MemoryKindSemantic, // case-sensitive (not normalized upward)
	}
	for in, want := range cases {
		if got := NormalizeMemoryKind(in); got != want {
			t.Errorf("NormalizeMemoryKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeAgentSlug(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":               "",
		"Claude Code":    "claude-code",
		"  cursor   ":    "cursor",
		"CLAUDE-CODE":    "claude-code",
		"already-lower":  "already-lower",
		"Mixed Case Hub": "mixed-case-hub",
	}
	for in, want := range cases {
		if got := NormalizeAgentSlug(in); got != want {
			t.Errorf("NormalizeAgentSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeMemoryStability(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		MemoryStabilityVolatile: MemoryStabilityVolatile,
		MemoryStabilityEvolving: MemoryStabilityEvolving,
		MemoryStabilityStable:   MemoryStabilityStable,
		"":                      MemoryStabilityEvolving, // default
		"unknown":               MemoryStabilityEvolving,
	}
	for in, want := range cases {
		if got := NormalizeMemoryStability(in); got != want {
			t.Errorf("NormalizeMemoryStability(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultRetrievalWeight(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want float64
	}{
		{0, 1.0},
		{-1, 1.0},
		{0.5, 0.5},
		{1.0, 1.0},
		{2.5, 2.5},
	}
	for _, tc := range cases {
		if got := DefaultRetrievalWeight(tc.in); got != tc.want {
			t.Errorf("DefaultRetrievalWeight(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ─── Hub helpers ────────────────────────────────────────────────

func TestDefaultHubAccent(t *testing.T) {
	t.Parallel()
	if got := DefaultHubAccent("team"); got != HubAccentAmber {
		t.Errorf("team → %q, want %q", got, HubAccentAmber)
	}
	if got := DefaultHubAccent("personal"); got != HubAccentViolet {
		t.Errorf("personal → %q, want %q", got, HubAccentViolet)
	}
	if got := DefaultHubAccent(""); got != HubAccentViolet {
		t.Errorf("empty → %q, want %q (falls through to personal default)", got, HubAccentViolet)
	}
}

func TestIsValidHubAccent(t *testing.T) {
	t.Parallel()
	valid := []string{
		HubAccentViolet, HubAccentBlue, HubAccentGreen,
		HubAccentAmber, HubAccentRose, HubAccentSlate,
	}
	for _, a := range valid {
		if !IsValidHubAccent(a) {
			t.Errorf("accent %q should be valid", a)
		}
	}
	invalid := []string{"", "purple", "red", "neon-pink", "VIOLET"}
	for _, a := range invalid {
		if IsValidHubAccent(a) {
			t.Errorf("accent %q should be invalid", a)
		}
	}
}

func TestIsValidHubHeaderAuroraMode(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"", "none", "signature", "time"} {
		if !IsValidHubHeaderAuroraMode(m) {
			t.Errorf("aurora mode %q should be valid", m)
		}
	}
	for _, m := range []string{"auto", "random", "NONE"} {
		if IsValidHubHeaderAuroraMode(m) {
			t.Errorf("aurora mode %q should be invalid", m)
		}
	}
}

func TestIsHubSettingsKey(t *testing.T) {
	t.Parallel()
	// The allow-list IS what this function tests against — keep
	// the expected set in sync with HubSettingsAllowedKeys.
	for _, k := range []string{
		"dreams_enabled",
		"dreams_merge_enabled",
		"dreams_archive_enabled",
		"dreams_organize_enabled",
		"dreams_restructure_enabled",
	} {
		if !IsHubSettingsKey(k) {
			t.Errorf("%q should be an accepted hub-settings key", k)
		}
	}
	// Tuning keys are intentionally NOT accepted — they're
	// account-scoped per the allow-list rationale.
	for _, k := range []string{
		"dreams_similarity_threshold",
		"dreams_excluded_kinds",
		"dreams_staleness_days",
		"theme",
		"notifications_enabled",
	} {
		if IsHubSettingsKey(k) {
			t.Errorf("%q should NOT be an accepted hub-settings key", k)
		}
	}
}

// ─── Plan helpers ───────────────────────────────────────────────

func TestLegacyToScopedPlanID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		LegacyFreePlanID:        PersonalFreePlanID,
		LegacyEarlyAccessPlanID: PersonalEarlyAccessPlanID,
		LegacyProPlanID:         PersonalProPlanID,
		LegacyProPlusPlanID:     PersonalProPlusPlanID,
		LegacyTeamPlanID:        HubTeamPlanID,
		LegacyEnterprisePlanID:  HubEnterprisePlanID,
		// Already-scoped IDs pass through unchanged.
		PersonalFreePlanID: PersonalFreePlanID,
		HubTeamPlanID:      HubTeamPlanID,
		// Unknown IDs pass through unchanged — callers still see
		// them and can treat as "not a plan" via the registry
		// lookup.
		"made-up-plan": "made-up-plan",
		"":             "",
	}
	for in, want := range cases {
		if got := LegacyToScopedPlanID(in); got != want {
			t.Errorf("LegacyToScopedPlanID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlanIsPersonalAndIsHub(t *testing.T) {
	t.Parallel()
	personal := &Plan{Scope: PlanScopePersonal}
	if !personal.IsPersonal() {
		t.Error("personal plan should report IsPersonal=true")
	}
	if personal.IsHub() {
		t.Error("personal plan should NOT report IsHub=true")
	}
	hub := &Plan{Scope: PlanScopeHub}
	if hub.IsPersonal() {
		t.Error("hub plan should NOT report IsPersonal=true")
	}
	if !hub.IsHub() {
		t.Error("hub plan should report IsHub=true")
	}
}

func TestIsUnlimited(t *testing.T) {
	t.Parallel()
	// Negative → unlimited (the sentinel convention). Zero and
	// positive are NOT unlimited.
	if !IsUnlimited(-1) {
		t.Error("-1 should be unlimited")
	}
	if !IsUnlimited(-999) {
		t.Error("any negative should be unlimited")
	}
	if IsUnlimited(0) {
		t.Error("0 is NOT unlimited — it's a hard zero quota")
	}
	if IsUnlimited(1) {
		t.Error("positive is NOT unlimited")
	}
}

func TestIsHubFrozenAllBranches(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Nil subscription → free tier, never frozen.
	if IsHubFrozen(nil, now) {
		t.Error("nil subscription should never report frozen")
	}
	// Cancelled subscription → frozen regardless of over-limit state.
	if !IsHubFrozen(&HubSubscription{Status: "cancelled"}, now) {
		t.Error("cancelled subscription should be frozen")
	}
	if !IsHubFrozen(&HubSubscription{Status: "past_due"}, now) {
		t.Error("past_due subscription should be frozen")
	}
	// Active + no over-limit → not frozen.
	if IsHubFrozen(&HubSubscription{Status: "active"}, now) {
		t.Error("active + no over-limit should not be frozen")
	}
	// Active + over-limit WITHIN grace period → not frozen yet.
	recent := now.Add(-HubGracePeriod / 2)
	sub := &HubSubscription{Status: "active", OverLimitSince: &recent}
	if IsHubFrozen(sub, now) {
		t.Error("active + over-limit < grace period should not be frozen")
	}
	// Active + over-limit BEYOND grace period → frozen.
	stale := now.Add(-HubGracePeriod - time.Hour)
	sub = &HubSubscription{Status: "active", OverLimitSince: &stale}
	if !IsHubFrozen(sub, now) {
		t.Error("active + over-limit > grace period should be frozen")
	}
	// Boundary case: exactly at grace period → NOT frozen
	// (strict > comparison — the comment specifies grace period
	// is the threshold beyond which freeze kicks in).
	exactly := now.Add(-HubGracePeriod)
	sub = &HubSubscription{Status: "active", OverLimitSince: &exactly}
	if IsHubFrozen(sub, now) {
		t.Error("exactly at grace period should not yet be frozen")
	}
}

// ─── Campaign helpers ───────────────────────────────────────────

func TestValidCampaignChannel(t *testing.T) {
	t.Parallel()
	for _, ch := range []string{CampaignChannelInapp, CampaignChannelEmail} {
		if !ValidCampaignChannel(ch) {
			t.Errorf("%q should be a valid channel", ch)
		}
	}
	for _, ch := range []string{"", "sms", "push", "INAPP"} {
		if ValidCampaignChannel(ch) {
			t.Errorf("%q should NOT be a valid channel", ch)
		}
	}
}

func TestValidCampaignStatus(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		CampaignStatusDraft, CampaignStatusScheduled,
		CampaignStatusSending, CampaignStatusSent,
		CampaignStatusCancelled, CampaignStatusFailed,
	} {
		if !ValidCampaignStatus(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "unknown", "DRAFT", "paused"} {
		if ValidCampaignStatus(s) {
			t.Errorf("%q should NOT be valid", s)
		}
	}
}

func TestValidAudienceRuleType(t *testing.T) {
	t.Parallel()
	for _, r := range []string{AudienceRuleAll, AudienceRuleUsers, AudienceRuleHub} {
		if !ValidAudienceRuleType(r) {
			t.Errorf("%q should be valid", r)
		}
	}
	for _, r := range []string{"", "custom", "ALL"} {
		if ValidAudienceRuleType(r) {
			t.Errorf("%q should NOT be valid", r)
		}
	}
}

func TestCampaignIsTerminal(t *testing.T) {
	t.Parallel()
	// Three terminal states — no further transitions.
	for _, s := range []string{CampaignStatusSent, CampaignStatusCancelled, CampaignStatusFailed} {
		if !CampaignIsTerminal(s) {
			t.Errorf("%q should be terminal", s)
		}
	}
	// Non-terminal: can still move.
	for _, s := range []string{CampaignStatusDraft, CampaignStatusScheduled, CampaignStatusSending} {
		if CampaignIsTerminal(s) {
			t.Errorf("%q should NOT be terminal", s)
		}
	}
}

// ─── UserPreferences.MergedSettings ─────────────────────────────

func TestUserPreferencesMergedSettings(t *testing.T) {
	t.Parallel()
	prefs := &UserPreferences{
		Settings: map[string]any{
			"theme":                 "light",         // override default "auto"
			"notifications_enabled": false,           // override default true
			"custom_field":          "user-specific", // new key, not in defaults
		},
	}
	merged := prefs.MergedSettings()
	if merged["theme"] != "light" {
		t.Errorf("stored override missing: theme=%v", merged["theme"])
	}
	if merged["notifications_enabled"] != false {
		t.Errorf("bool override not respected: %v", merged["notifications_enabled"])
	}
	if merged["custom_field"] != "user-specific" {
		t.Errorf("custom key not propagated: %v", merged["custom_field"])
	}
	// Defaults that weren't overridden must still be present.
	if _, ok := merged["dreams_enabled"]; !ok {
		t.Error("default dreams_enabled missing from merged settings")
	}
	if _, ok := merged["hub_header_aurora_mode"]; !ok {
		t.Error("default hub_header_aurora_mode missing from merged settings")
	}
}

func TestUserPreferencesMergedSettingsEmptyStored(t *testing.T) {
	t.Parallel()
	// Empty stored settings → merged equals DefaultSettings().
	// Structural property: key count must match defaults exactly.
	prefs := &UserPreferences{Settings: map[string]any{}}
	merged := prefs.MergedSettings()
	defaults := DefaultSettings()
	if len(merged) != len(defaults) {
		t.Errorf("empty stored should produce defaults exactly; got %d keys, defaults has %d",
			len(merged), len(defaults))
	}
}

// ─── DreamRunCounts.Nonzero ─────────────────────────────────────

func TestDreamRunCountsNonzero(t *testing.T) {
	t.Parallel()
	if (DreamRunCounts{}).Nonzero() {
		t.Error("zero-value DreamRunCounts should report Nonzero()=false")
	}
	// Each non-zero field independently flips the result to true.
	// Catches regression where the bitwise-style or in the
	// implementation is replaced with an incomplete check that
	// forgets a field.
	for _, tc := range []struct {
		name string
		c    DreamRunCounts
	}{
		{"merged", DreamRunCounts{Merged: 1}},
		{"archived", DreamRunCounts{Archived: 1}},
		{"organized", DreamRunCounts{Organized: 1}},
		{"contradictions", DreamRunCounts{Contradictions: 1}},
		{"restructures", DreamRunCounts{Restructures: 1}},
	} {
		if !tc.c.Nonzero() {
			t.Errorf("%s=1 should produce Nonzero()=true (got false)", tc.name)
		}
	}
}

// ─── EmailTemplateOverride.HasUnpublishedDraft ──────────────────

func TestEmailTemplateOverrideHasUnpublishedDraft(t *testing.T) {
	t.Parallel()

	// Nil receiver is a valid no-op — handlers call this on
	// potentially-nil overrides without prechecking.
	var nilOverride *EmailTemplateOverride
	if nilOverride.HasUnpublishedDraft() {
		t.Error("nil receiver should report no draft")
	}

	// All draft/published fields match → no draft.
	same := &EmailTemplateOverride{
		Subject:      "Hi",
		HTML:         "<p>hi</p>",
		Text:         "hi",
		DraftSubject: "Hi",
		DraftHTML:    "<p>hi</p>",
		DraftText:    "hi",
	}
	if same.HasUnpublishedDraft() {
		t.Error("matching fields should report no draft")
	}

	// Any single field diverges → draft present.
	for _, tc := range []struct {
		name string
		ov   *EmailTemplateOverride
	}{
		{"subject differs", &EmailTemplateOverride{Subject: "A", DraftSubject: "B"}},
		{"html differs", &EmailTemplateOverride{HTML: "x", DraftHTML: "y"}},
		{"text differs", &EmailTemplateOverride{Text: "p", DraftText: "q"}},
	} {
		if !tc.ov.HasUnpublishedDraft() {
			t.Errorf("%s: should report draft", tc.name)
		}
	}
}
