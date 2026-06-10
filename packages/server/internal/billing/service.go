package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Service is the single mutation path for all plan changes. Every caller —
// admin actions, waitlist invite consumption, payment webhooks, self-service
// upgrade — must go through ChangePlan to keep subscription state, audit
// events, and cache invalidation consistent.
type Service struct {
	provider Provider
	store    store.Store
	registry *plans.Registry
	logger   *slog.Logger

	// resetCounters is an optional callback for resetting Redis usage counters.
	// Injected by the meter package after initialization (avoids circular deps).
	resetCounters func(ctx context.Context, userID string) error

	// invalidateUserPlan is an optional callback that recomputes a user's
	// effective plan cache. Injected by the planresolver after initialization.
	invalidateUserPlan func(ctx context.Context, userID string) error

	// invalidateHubMembers recomputes effective plan for all members of a hub.
	// Injected by the planresolver after initialization.
	invalidateHubMembers func(ctx context.Context, hubID string) error

	// notifyHubRestored is an optional hook fired when a hub's
	// over_limit_since transitions non-null → null as a side effect of
	// ChangeHubPlan (plan upgrade pulled the hub back under the cap).
	// Injected from serverapp so the service stays out of the notif
	// package; a nil hook just skips the notification.
	notifyHubRestored func(ctx context.Context, hubID string)
}

// NewService creates a billing Service.
//
// If provider is nil, an AdminProvider (no-op) is used automatically.
// This means the Service always works — no nil checks needed at call sites.
func NewService(s store.Store, registry *plans.Registry) *Service {
	return &Service{
		provider: &AdminProvider{},
		store:    s,
		registry: registry,
		logger:   slog.Default(),
	}
}

// SetProvider replaces the payment provider. Use this when configuring
// an external billing system (Stripe, Polar) after service creation.
//
// NOTE: External provider methods (CreateCustomer, CreateSubscription, etc.)
// are not yet invoked by ChangePlan. The current implementation handles plan
// changes locally only (update users.plan + internal subscription record).
// When Stripe/Polar integration is built, ChangePlan will branch:
// AdminProvider → local-only; external provider → coordinate with provider
// before updating local state. The interface is defined now so the provider
// can be wired and tested before the coordination logic is added.
func (s *Service) SetProvider(p Provider) {
	if p != nil {
		s.provider = p
	}
}

// SetResetCounters injects the meter's counter reset function.
func (s *Service) SetResetCounters(fn func(ctx context.Context, userID string) error) {
	s.resetCounters = fn
}

// SetInvalidateUserPlan injects the plan resolver's cache invalidation function.
func (s *Service) SetInvalidateUserPlan(fn func(ctx context.Context, userID string) error) {
	s.invalidateUserPlan = fn
}

// SetInvalidateHubMembers injects the plan resolver's hub-wide cache invalidation.
func (s *Service) SetInvalidateHubMembers(fn func(ctx context.Context, hubID string) error) {
	s.invalidateHubMembers = fn
}

// SetNotifyHubRestored injects the hook ChangeHubPlan calls when a
// plan upgrade clears over_limit_since. The callback dispatches the
// hub_restored notification to owner + admins; lives in serverapp
// rather than here so billing doesn't depend on handler / events.
func (s *Service) SetNotifyHubRestored(fn func(ctx context.Context, hubID string)) {
	s.notifyHubRestored = fn
}

// ChangePlan is the single mutation path for all plan changes. It:
//  1. Validates the target plan exists and is active
//  2. Updates users.plan in Postgres
//  3. Upserts billing_subscriptions for audit trail
//  4. Logs a plan_change usage event
//
// It does NOT reset usage counters. On upgrade, the new higher limits
// apply immediately with existing counters. On downgrade, the user may be
// temporarily over-quota until the natural monthly reset. Use
// ResetUsageCounters for explicit admin resets.
//
// source identifies the trigger: "admin", "waitlist_invite", "webhook",
// "self_service".
func (s *Service) ChangePlan(ctx context.Context, userID, newPlanID, source string) error {
	// 1. Validate plan exists and has the correct scope.
	// ChangePlan is for personal plan changes only. Hub plan changes
	// go through ChangeHubPlan.
	plan := s.registry.GetPlan(newPlanID)
	if plan == nil {
		// Try scoped equivalent of legacy ID
		plan = s.registry.GetPlan(model.LegacyToScopedPlanID(newPlanID))
	}
	if plan == nil {
		return fmt.Errorf("plan %q not found", newPlanID)
	}
	if !plan.Active {
		return fmt.Errorf("plan %q is not active", newPlanID)
	}
	if plan.IsHub() {
		return fmt.Errorf("plan %q is a hub plan; use ChangeHubPlan instead", newPlanID)
	}

	// 2. Update users.plan in Postgres (writes both legacy + scoped columns)
	if err := s.store.UpdateUserPlan(ctx, userID, newPlanID); err != nil {
		return fmt.Errorf("update user plan: %w", err)
	}

	// 3. Upsert billing subscription for audit trail.
	// Always write the scoped plan ID so billing_subscriptions stays
	// in the personal scope and can eventually have a scoped FK.
	scopedPlanID := model.LegacyToScopedPlanID(newPlanID)
	now := time.Now()
	sub := &model.BillingSubscription{
		UserID:             userID,
		PlanID:             scopedPlanID,
		Provider:           providerName(s.provider),
		Status:             "active",
		CurrentPeriodStart: &now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.UpsertBillingSubscription(ctx, sub); err != nil {
		// Non-fatal: the plan change succeeded, subscription tracking is secondary
		s.logger.Warn("failed to upsert billing subscription",
			"error", err, "user_id", userID, "plan_id", newPlanID)
	}

	// 4. Log plan_change usage event
	event := &model.UsageEvent{
		UserID:    userID,
		Operation: "plan_change",
		Source:    source,
		Metadata: map[string]any{
			"new_plan": newPlanID,
		},
	}
	if err := s.store.InsertUsageEvent(ctx, event); err != nil {
		// Non-fatal: audit logging should not block the plan change
		s.logger.Warn("failed to log plan change event",
			"error", err, "user_id", userID, "plan_id", newPlanID)
	}

	// Invalidate the user's effective plan cache so the new personal plan
	// is reflected in the NEXT request. For ChangePlan this is part of the
	// mutation's correctness — cache invalidation failure means the user
	// keeps old limits despite the plan change (Codex finding #2).
	if s.invalidateUserPlan != nil {
		if err := s.invalidateUserPlan(ctx, userID); err != nil {
			// Return the error — the plan change succeeded in Postgres but
			// the cache may serve stale limits. The caller should know.
			return fmt.Errorf("plan changed in DB but cache invalidation failed: %w", err)
		}
	}

	s.logger.Info("plan changed",
		"user_id", userID, "new_plan", newPlanID, "source", source)
	return nil
}

// ResetUsageCounters explicitly resets a user's usage counters for the
// current billing period. This is an admin tool for goodwill gestures
// (e.g., customer had a bad experience, grant them a fresh month).
//
// It zeroes the Postgres usage row and, if the meter is wired, deletes
// the Redis counter keys.
func (s *Service) ResetUsageCounters(ctx context.Context, userID, adminUserID string) error {
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	// Zero the Postgres usage row unconditionally (not GREATEST — actual zero)
	if err := s.store.ResetUsageCounters(ctx, userID, periodStart, periodEnd); err != nil {
		return fmt.Errorf("reset postgres usage: %w", err)
	}

	// Reset Redis counters if the meter is wired
	if s.resetCounters != nil {
		if err := s.resetCounters(ctx, userID); err != nil {
			s.logger.Warn("failed to reset redis counters",
				"error", err, "user_id", userID)
			// Non-fatal: Postgres is already zeroed, Redis will catch up on sync
		}
	}

	// Audit trail
	event := &model.UsageEvent{
		UserID:    userID,
		Operation: "usage_reset",
		Source:    "admin",
		Metadata: map[string]any{
			"reset_by": adminUserID,
		},
	}
	if err := s.store.InsertUsageEvent(ctx, event); err != nil {
		s.logger.Warn("failed to log usage reset event",
			"error", err, "user_id", userID)
	}

	s.logger.Info("usage counters reset",
		"user_id", userID, "reset_by", adminUserID)
	return nil
}

// GetActivePlan returns the user's active plan definition, resolving through
// the registry. Convenience method for handlers that need plan info but don't
// want to wire the registry directly.
func (s *Service) GetActivePlan(userID string) *model.Plan {
	user, err := s.store.GetUser(userID)
	if err != nil || user == nil {
		return s.freePlanFallback()
	}
	// Prefer scoped personal_plan_id, fall back to legacy plan
	planID := user.PersonalPlanID
	if planID == "" {
		planID = user.Plan
	}
	plan := s.registry.GetPlan(planID)
	if plan == nil {
		plan = s.registry.GetPlan(model.LegacyToScopedPlanID(planID))
	}
	if plan == nil {
		return s.freePlanFallback()
	}
	return plan
}

// freePlanFallback returns the free plan, trying scoped ID first then legacy.
func (s *Service) freePlanFallback() *model.Plan {
	if p := s.registry.GetPlan(model.PersonalFreePlanID); p != nil {
		return p
	}
	return s.registry.GetPlan(model.LegacyFreePlanID)
}

// --- Hub billing methods ---

// ChangeHubPlan creates or updates a hub's subscription.
// This is the single mutation path for hub-level plan changes.
//
//  1. Validates plan exists and is active
//  2. Counts current hub members for seat count (min 3 for Team)
//  3. Upserts hub_subscriptions row
//  4. Updates hubs.plan for backward compatibility
//  5. Invalidates effective plans for ALL hub members
//  6. Logs hub_plan_change usage event
func (s *Service) ChangeHubPlan(ctx context.Context, hubID, planID, source string) error {
	// 1. Validate plan exists, is active, and has the correct scope.
	// ChangeHubPlan is for hub plan changes only. Personal plan changes
	// go through ChangePlan.
	plan := s.registry.GetPlan(planID)
	if plan == nil {
		return fmt.Errorf("plan %q not found", planID)
	}
	if !plan.Active {
		return fmt.Errorf("plan %q is not active", planID)
	}
	if plan.IsPersonal() {
		return fmt.Errorf("plan %q is a personal plan; use ChangePlan instead", planID)
	}

	// 2. Get hub and count members
	hub, err := s.store.GetHub(hubID)
	if err != nil || hub == nil {
		return fmt.Errorf("hub %q not found", hubID)
	}
	// Hub subscriptions are a team-hub concept. Personal hubs have no
	// billing surface — reject here so an admin mis-routing or a
	// direct API call can't upsert a subscription on one.
	if hub.HubType != "team" {
		return fmt.Errorf("hub %q is not a team hub", hubID)
	}
	memberCount, err := s.store.CountHubMembers(hubID)
	if err != nil {
		return fmt.Errorf("count hub members: %w", err)
	}
	seatCount := memberCount
	if seatCount < 3 {
		seatCount = 3 // minimum 3 seats for Team plan
	}

	// 3. Upsert hub subscription
	now := time.Now()
	sub := &model.HubSubscription{
		HubID:              hubID,
		PlanID:             planID,
		SeatCount:          seatCount,
		Provider:           providerName(s.provider),
		Status:             "active",
		BillingUserID:      hub.OwnerID,
		CurrentPeriodStart: &now,
	}
	// Check if subscription already exists
	existing, err := s.store.GetActiveHubSubscription(ctx, hubID)
	if err != nil {
		return fmt.Errorf("check existing hub subscription: %w", err)
	}
	if existing != nil {
		existing.PlanID = planID
		existing.SeatCount = seatCount
		if err := s.store.UpdateHubSubscription(ctx, existing); err != nil {
			return fmt.Errorf("update hub subscription: %w", err)
		}
	} else {
		if err := s.store.CreateHubSubscription(ctx, sub); err != nil {
			return fmt.Errorf("create hub subscription: %w", err)
		}
	}

	// Phase 6: hubs.plan backward-compat write removed.
	// hub_subscriptions is the sole authority for hub plan state.

	// Plan change may have resolved a prior over-limit state. Compare
	// the authoritative hub memory count to the new plan's limit and
	// clear over_limit_since when the hub fits — otherwise an upgrade
	// from a too-small plan to a bigger one would leave a stale marker
	// that the push path would read as "still frozen". Non-fatal: a
	// failed clear here just means the next delete will retry.
	//
	// If the clear actually transitioned the column non-null → null,
	// fire the hub_restored notification hook so owner + admins get
	// the receipt. Delete-driven restores go through memories.go
	// maybeClearHubOverLimit; this is the parallel billing-driven
	// path.
	restored := false
	if plan.MemoryLimit >= 0 {
		count, cntErr := s.store.CountMemoriesByHub(ctx, hubID)
		if cntErr == nil && count <= plan.MemoryLimit {
			ok, _ := s.store.ClearHubOverLimit(ctx, hubID)
			restored = ok
		}
	} else {
		// Unlimited plan — always clear.
		ok, _ := s.store.ClearHubOverLimit(ctx, hubID)
		restored = ok
	}
	if restored && s.notifyHubRestored != nil {
		s.notifyHubRestored(ctx, hubID)
	}

	// 4. Invalidate effective plans for all members
	if s.invalidateHubMembers != nil {
		if err := s.invalidateHubMembers(ctx, hubID); err != nil {
			return fmt.Errorf("hub plan changed but member cache invalidation failed: %w", err)
		}
	}

	// 6. Audit trail
	event := &model.UsageEvent{
		UserID:    hub.OwnerID,
		HubID:     hubID,
		Operation: "hub_plan_change",
		Source:    source,
		Metadata: map[string]any{
			"new_plan":   planID,
			"seat_count": seatCount,
		},
	}
	if err := s.store.InsertUsageEvent(ctx, event); err != nil {
		s.logger.Warn("failed to log hub plan change event",
			"error", err, "hub_id", hubID)
	}

	s.logger.Info("hub plan changed",
		"hub_id", hubID, "new_plan", planID, "seats", seatCount, "source", source)
	return nil
}

// UpdateHubSeatCount syncs the subscription seat count with actual membership.
// Called after member join/leave.
func (s *Service) UpdateHubSeatCount(ctx context.Context, hubID string) error {
	sub, err := s.store.GetActiveHubSubscription(ctx, hubID)
	if err != nil {
		return fmt.Errorf("get hub subscription: %w", err)
	}
	if sub == nil {
		return nil // no subscription — nothing to update
	}

	memberCount, err := s.store.CountHubMembers(hubID)
	if err != nil {
		return fmt.Errorf("count hub members: %w", err)
	}
	seatCount := memberCount
	if seatCount < 3 {
		seatCount = 3
	}

	if sub.SeatCount == seatCount {
		return nil // no change
	}

	sub.SeatCount = seatCount
	if err := s.store.UpdateHubSubscription(ctx, sub); err != nil {
		return fmt.Errorf("update seat count: %w", err)
	}

	s.logger.Info("hub seat count updated",
		"hub_id", hubID, "seats", seatCount)
	return nil
}

// TransferHubBilling updates the billing contact after ownership transfer.
func (s *Service) TransferHubBilling(ctx context.Context, hubID, newOwnerID string) error {
	sub, err := s.store.GetActiveHubSubscription(ctx, hubID)
	if err != nil {
		return fmt.Errorf("get hub subscription: %w", err)
	}
	if sub == nil {
		return nil // no subscription — nothing to transfer
	}

	sub.BillingUserID = newOwnerID
	if err := s.store.UpdateHubSubscription(ctx, sub); err != nil {
		return fmt.Errorf("transfer hub billing: %w", err)
	}

	s.logger.Info("hub billing transferred",
		"hub_id", hubID, "new_owner", newOwnerID)
	return nil
}

// GetHubSubscription returns the active subscription for a hub.
func (s *Service) GetHubSubscription(ctx context.Context, hubID string) (*model.HubSubscription, error) {
	return s.store.GetActiveHubSubscription(ctx, hubID)
}

// ListBillingHubs returns all hubs a user pays for.
func (s *Service) ListBillingHubs(ctx context.Context, userID string) ([]model.HubSubscription, error) {
	return s.store.ListHubSubscriptionsByBillingUser(ctx, userID)
}

// providerName returns a display name for the provider type.
func providerName(p Provider) string {
	switch p.(type) {
	case *AdminProvider:
		return "admin"
	default:
		return "external"
	}
}
