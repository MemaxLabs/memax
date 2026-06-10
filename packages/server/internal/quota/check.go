package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
)

// DocsURL is the public docs page that 402 responses link to. Kept
// as a package-level var so tests and ops can override without
// passing it through every Decision.
var DocsURL = "https://docs.memax.app/dreams/quotas"

// UpgradeURL is the upgrade target for personal-scope denials.
// Hub-scope denials don't use it (operators upgrade hubs through a
// different path).
var UpgradeURL = "https://memax.app/settings/billing"

// CheckDreamQuota returns the current quota state for a scope.
//
// The Decision's Allowed field reflects whether a new run can
// proceed in HARD mode (counter < limit, or unlimited). In SOFT
// mode, Allowed is always true regardless of counter state — the
// caller still consults Used/Limit/Remaining to populate the
// telemetry banner.
//
// Tier disambiguation:
//
//   - User-scoped: if LucidLimit > 0 OR LucidLimit == -1, the user
//     is on a paid tier; we report Tier=lucid and Limit/Used from
//     the lucid counter. Otherwise Tier=basic, Limit/Used from
//     basic counter. Free users in a free hub stay at Tier=basic.
//   - Hub-scoped: always Tier=lucid (hub_team and hub_enterprise
//     are the only hub plans with dream quotas; both are Lucid).
//     For hub_free_team (lucid=0), Allowed=false, Tier=lucid.
//
// # Scope selection (load-bearing for the handler-wiring commit)
//
// The caller chooses ONE scope per run; the two scopes are NOT
// max-merged at check time. This matches docs/plans/23-dream-tiers.md
// "Strict scoping":
//
//   - Personal-hub trigger or no-hub-context (API key without hub):
//     check ScopeUser. The user's max(personal, best_hub) is already
//     baked into the resolver's ResolveDreamLimits.
//   - Team-hub trigger or hub-scoped API key (key bound to a single
//     hub): check ScopeHub only. The hub's fixed-per-hub quota is
//     the only one that applies; user's personal quota is irrelevant
//     for runs that scan hub content.
//
// Do NOT check both scopes for a single run — that would either
// double-charge or apply the wrong cap. `triggered_by` on dream_runs
// is for actor audit, not quota selection.
func CheckDreamQuota(
	ctx context.Context,
	resolver DreamResolver,
	store DreamUsageStore,
	scope Scope,
	mode Mode,
) (Decision, error) {
	if !mode.Valid() {
		return Decision{}, fmt.Errorf("quota: invalid mode %q", mode)
	}
	if scope.Type != ScopeUser && scope.Type != ScopeHub {
		return Decision{}, fmt.Errorf("quota: invalid scope type %q", scope.Type)
	}
	if scope.ID() == "" {
		return Decision{}, fmt.Errorf("quota: scope %s missing ID", scope.Type)
	}

	periodStart, periodEnd := currentPeriod(time.Now())

	switch scope.Type {
	case ScopeUser:
		limits := resolver.ResolveDreamLimits(ctx, scope.UserID)
		basicUsed, lucidUsed, err := store.GetUserDreamUsage(ctx, scope.UserID, periodStart)
		if err != nil {
			return Decision{}, fmt.Errorf("quota: get user dream usage: %w", err)
		}
		return buildUserDecision(scope, limits, basicUsed, lucidUsed, mode, periodStart, periodEnd), nil

	case ScopeHub:
		limits := resolver.GetHubDreamLimits(ctx, scope.HubID)
		lucidUsed, err := store.GetHubDreamUsage(ctx, scope.HubID, periodStart)
		if err != nil {
			return Decision{}, fmt.Errorf("quota: get hub dream usage: %w", err)
		}
		return buildHubDecision(scope, limits, lucidUsed, mode, periodStart, periodEnd), nil
	}

	// Unreachable due to validation above; satisfies the compiler.
	return Decision{}, fmt.Errorf("quota: unreachable scope %q", scope.Type)
}

// buildUserDecision picks the right tier (Basic vs Lucid) for a
// personal-scope user based on which limit is set, then assembles
// the Decision. Tier preference: Lucid wins when the user has any
// Lucid quota (positive or unlimited); falls back to Basic.
func buildUserDecision(
	scope Scope,
	limits planresolver.DreamLimits,
	basicUsed, lucidUsed int,
	mode Mode,
	periodStart, periodEnd time.Time,
) Decision {
	tier := TierBasic
	limit := limits.BasicLimit
	used := basicUsed
	source := limits.BasicQuotaSource

	if limits.LucidLimit > 0 || limits.LucidLimit == -1 {
		tier = TierLucid
		limit = limits.LucidLimit
		used = lucidUsed
		source = limits.LucidQuotaSource
	}

	return assembleDecision(scope, tier, limit, used, source, mode, periodStart, periodEnd)
}

// buildHubDecision is the hub-scope equivalent. Hubs only ever use
// the Lucid bucket — Basic is a personal-scope concept.
func buildHubDecision(
	scope Scope,
	limits planresolver.DreamLimits,
	lucidUsed int,
	mode Mode,
	periodStart, periodEnd time.Time,
) Decision {
	return assembleDecision(scope, TierLucid, limits.LucidLimit, lucidUsed, limits.LucidQuotaSource, mode, periodStart, periodEnd)
}

// assembleDecision is the shared tail used by user and hub paths.
func assembleDecision(
	scope Scope,
	tier Tier,
	limit, used int,
	quotaSource string,
	mode Mode,
	periodStart, periodEnd time.Time,
) Decision {
	d := Decision{
		Mode:        mode,
		Tier:        tier,
		Limit:       limit,
		Used:        used,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		QuotaSource: quotaSource,
	}

	switch {
	case limit == -1:
		// Unlimited: always allowed. Remaining intentionally left at
		// zero — callers should check Limit==-1 first to decide
		// whether to display Remaining.
		d.Allowed = true

	case limit == 0:
		// Tier disabled. Hard mode rejects; soft mode also rejects
		// because soft is "allow over cap" not "ignore disable".
		// A disabled tier means the user has no entitlement at all
		// for this run type, period.
		d.Allowed = false
		d.Details = denialDetails(scope, tier, limit, used, quotaSource, periodEnd)

	default:
		// Finite cap. Hard mode rejects when used >= limit; soft
		// mode allows through but still populates Details so the
		// caller can surface the would-have-blocked banner.
		d.Remaining = limit - used
		if d.Remaining < 0 {
			d.Remaining = 0
		}

		if used >= limit {
			d.Allowed = mode == ModeSoft
			d.Details = denialDetails(scope, tier, limit, used, quotaSource, periodEnd)
		} else {
			d.Allowed = true
		}
	}

	return d
}

// denialDetails builds the structured payload that lands in the
// 402 response body's `details` field. The shape mirrors the spec
// in docs/plans/23-dream-tiers.md (API contract section).
func denialDetails(
	scope Scope,
	tier Tier,
	limit, used int,
	quotaSource string,
	periodEnd time.Time,
) *DenialDetails {
	code := "basic_quota_exhausted"
	if tier.IsLucid() {
		code = "lucid_quota_exhausted"
	}
	scopeStr := "personal"
	if scope.IsHub() {
		scopeStr = "hub"
	}
	d := &DenialDetails{
		Code:        code,
		Scope:       scopeStr,
		Limit:       limit,
		Used:        used,
		ResetsAt:    periodEnd.AddDate(0, 0, 1), // first day after period_end
		DocsURL:     DocsURL,
		QuotaSource: quotaSource,
	}
	if scope.IsHub() {
		d.HubID = scope.HubID
	} else {
		d.UpgradeURL = UpgradeURL
	}
	return d
}

// currentPeriod returns the calendar-month boundaries for `now`.
// period_start = first of the month, period_end = last day of the
// month. Both at UTC midnight to match the migration's `date` column
// semantics. This is the v1 cadence; Stripe billing-anchor support
// is explicitly out of scope per the plan.
func currentPeriod(now time.Time) (time.Time, time.Time) {
	t := now.UTC()
	periodStart := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)
	return periodStart, periodEnd
}
