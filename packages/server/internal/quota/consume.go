package quota

import (
	"context"
	"fmt"
)

// ConsumeDreamRun performs the atomic consumption of a dream run
// against the quota counter. Idempotent on params.DreamRunID.
//
// Behavior summary (full SQL spec in
// docs/plans/23-dream-tiers.md):
//
//   - Inserts a row in dream_run_completions for the run; if the
//     row already exists, returns the prior counted state without
//     touching the counter.
//   - If the new run is "countable" (tier in basic/lucid_passive/
//     lucid_active AND terminal_state in completed/partial_failed/
//     loop_cap_hit AND limit != 0), runs a guarded UPDATE on the
//     usage / hub_usage counter:
//   - Hard mode: WHERE count < limit OR limit = -1 (unlimited).
//     If no row returned (cap consumed by parallel run),
//     transitions terminal_state to quota_race_lost and reports
//     QuotaRaceLost=true with Counted=false.
//   - Soft mode: unguarded UPDATE; always commits. Records the
//     observed used_after into dream_quota_signals with the
//     would_have_blocked flag for telemetry.
//
// Caller MUST NOT pass StateQuotaRaceLost in params.TerminalState —
// that's an internal transition.
//
// The mode and resolvedLimit fields should come from the start-time
// Decision (CheckDreamQuota's return value) carried through the run
// context. Re-resolving at completion would race with mid-run plan
// changes and corrupt the recorded snapshot.
func ConsumeDreamRun(
	ctx context.Context,
	store DreamConsumeStore,
	params ConsumeParams,
) (ConsumeResult, error) {
	if err := validateConsumeParams(params); err != nil {
		return ConsumeResult{}, err
	}
	mp := params.toModel()
	mr, err := store.ConsumeDreamRun(ctx, mp)
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("quota: consume dream run: %w", err)
	}
	return resultFromModel(mr), nil
}

func validateConsumeParams(p ConsumeParams) error {
	if p.DreamRunID == "" {
		return fmt.Errorf("quota: ConsumeDreamRun: DreamRunID is required")
	}
	if !p.Mode.Valid() {
		return fmt.Errorf("quota: ConsumeDreamRun: invalid mode %q", p.Mode)
	}
	if p.Scope.Type != ScopeUser && p.Scope.Type != ScopeHub {
		return fmt.Errorf("quota: ConsumeDreamRun: invalid scope type %q", p.Scope.Type)
	}
	if p.Scope.ID() == "" {
		return fmt.Errorf("quota: ConsumeDreamRun: scope %s missing ID", p.Scope.Type)
	}
	if p.Tier != TierBasic && p.Tier != TierLucidPassive && p.Tier != TierLucidActive {
		// Note: TierLucid (umbrella) is a check-time-only convenience
		// and must NOT be passed to ConsumeDreamRun — by completion
		// the engine knows whether the loop fired and reports
		// passive vs active.
		return fmt.Errorf("quota: ConsumeDreamRun: invalid tier %q (must be basic/lucid_passive/lucid_active)", p.Tier)
	}
	if p.Scope.IsHub() && p.Tier == TierBasic {
		// Hubs only have a Lucid bucket; Basic is a personal-scope
		// concept. The store's hub consume path increments
		// hub_usage.lucid_dream_count regardless of tier, so
		// charging a basic-tier run against a hub would silently
		// drain the hub's Lucid pool. Reject at the boundary.
		return fmt.Errorf("quota: ConsumeDreamRun: hub scope cannot consume tier=basic (hubs only have Lucid)")
	}
	if p.TerminalState == StateQuotaRaceLost {
		return fmt.Errorf("quota: ConsumeDreamRun: caller must not pass quota_race_lost (internal-only transition)")
	}
	if p.PeriodStart.IsZero() || p.PeriodEnd.IsZero() {
		return fmt.Errorf("quota: ConsumeDreamRun: PeriodStart and PeriodEnd are required")
	}
	if p.PeriodEnd.Before(p.PeriodStart) {
		return fmt.Errorf("quota: ConsumeDreamRun: PeriodEnd must not precede PeriodStart")
	}
	return nil
}
