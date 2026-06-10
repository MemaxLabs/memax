// Package planresolver provides per-request plan resolution with per-hub
// elevation and personal-boost caching.
//
// Resolution rules:
//   - In a team hub with an active subscription: limits come from the hub's plan
//     (except MemoryLimit, which stays personal)
//   - In a personal hub or no hub context: limits come from max(personal, best_hub)
//   - Per-user admin overrides apply on top in both cases
//
// Caching: three layers — Redis (10min TTL) → Postgres effective_plan_cache
// (30min freshness) → recompute from users.plan + hub_members + hub_subscriptions.
package planresolver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

const (
	effectivePlanKeyPrefix = "memax:effective_plan:"
	hubPlanKeyPrefix       = "memax:hub_plan:"
	effectivePlanTTL       = 10 * time.Minute
	hubPlanTTL             = 5 * time.Minute
	pgFreshnessLimit       = 30 * time.Minute
)

// Resolver provides per-request plan resolution with per-hub elevation.
type Resolver struct {
	redis    *redis.Client
	store    store.Store
	registry *plans.Registry
	logger   *slog.Logger
}

// New creates a Resolver. If redis is nil, caching is skipped (every request recomputes).
func New(redisClient *redis.Client, s store.Store, registry *plans.Registry) *Resolver {
	return &Resolver{
		redis:    redisClient,
		store:    s,
		registry: registry,
		logger:   slog.Default(),
	}
}

// ResolveForRequest returns a HYBRID UserLimits for a request.
//
// When billingHubID is a team hub with an active subscription:
//   - PushLimit, RecallLimit, AskLimit, AskModel, DreamsEnabled, ReviewInbox,
//     RateLimitRPM come from the hub's subscription plan
//   - MemoryLimit comes from the user's effective personal plan (memory count
//     is per-owner, not per-hub)
//
// When billingHubID is empty or a personal hub:
//   - ALL fields come from the effective personal plan
//
// Per-user admin overrides apply in both cases.
// ResolveForRequest is the legacy resolver method. It cannot distinguish team
// hubs from personal hubs when no subscription exists, so it falls through to
// personal plan resolution. New callers should use the scoped methods
// (ResolveReadEntitlements, ResolveHubWriteEntitlements, etc.) which handle
// the team-hub-no-subscription case correctly by defaulting to hub_free_team.
func (r *Resolver) ResolveForRequest(ctx context.Context, userID, billingHubID string) model.UserLimits {
	// Try hub-level resolution first
	if billingHubID != "" {
		hubPlanID := r.getHubPlanID(ctx, billingHubID)
		if hubPlanID != "" {
			hubPlan := r.registry.GetPlan(hubPlanID)
			if hubPlan != nil && hubPlan.Active {
				effectivePlanID := r.GetEffectivePlanID(ctx, userID)
				personalLimits := r.registry.GetUserLimits(ctx, userID, effectivePlanID)

				limits := limitsFromPlan(hubPlan)
				limits.MemoryLimit = personalLimits.MemoryLimit
				r.applyOverrides(ctx, userID, &limits)
				return limits
			}
		}
	}

	// Personal hub, no hub context, or hub with no subscription → personal plan
	effectivePlanID := r.GetEffectivePlanID(ctx, userID)
	return r.registry.GetUserLimits(ctx, userID, effectivePlanID)
}

// GetEffectivePlanID returns the user's effective personal plan ID.
// Three-layer cache: Redis → Postgres effective_plan_cache → recompute.
func (r *Resolver) GetEffectivePlanID(ctx context.Context, userID string) string {
	// Layer 1: Redis
	if r.redis != nil {
		key := effectivePlanKeyPrefix + userID
		if planID, err := r.redis.Get(ctx, key).Result(); err == nil && planID != "" {
			return planID
		}
	}

	// Layer 2: Postgres effective_plan_cache (with freshness check)
	if ep, err := r.store.GetEffectivePlan(ctx, userID); err == nil && ep != nil {
		if time.Since(ep.ComputedAt) < pgFreshnessLimit {
			// Cache hit — backfill Redis (best-effort) and return
			if r.redis != nil {
				r.redis.Set(ctx, effectivePlanKeyPrefix+userID, ep.PlanID, effectivePlanTTL)
			}
			return ep.PlanID
		}
		// Stale — fall through to recompute
	}

	// Layer 3: Recompute from source data
	planID, source := r.computeEffectivePlan(ctx, userID)

	// Write back to both caches (best-effort for the read path — errors are
	// logged but don't block the request)
	if r.redis != nil {
		r.redis.Set(ctx, effectivePlanKeyPrefix+userID, planID, effectivePlanTTL)
	}
	if err := r.store.SetEffectivePlan(ctx, userID, planID, source); err != nil {
		r.logger.Warn("planresolver: failed to write effective plan cache",
			"error", err, "user_id", userID)
	}

	return planID
}

// InvalidateUser recomputes and caches a user's effective personal plan.
// Called when: personal plan changes, user joins/leaves a hub.
//
// Strategy: recompute → write Postgres → checked DEL Redis.
//
// Why this order: a concurrent request between steps can read the old
// Postgres row and backfill Redis with stale data. By writing Postgres
// FIRST (making it authoritative), then DELeting Redis (removing any
// stale backfill), we guarantee that after this function returns:
//   - Postgres has the correct value
//   - Redis either has no value (forcing recompute → fresh Postgres)
//     or was SET by a concurrent request that read the NEW Postgres row
//
// We do NOT write Redis after DEL to avoid re-introducing a race window
// where a concurrent backfill could overwrite our SET.
func (r *Resolver) InvalidateUser(ctx context.Context, userID string) error {
	// Step 1: Recompute from source data.
	planID, source := r.computeEffectivePlan(ctx, userID)

	// Step 2: Write Postgres cache (authoritative source for Layer 2).
	if err := r.store.SetEffectivePlan(ctx, userID, planID, source); err != nil {
		return fmt.Errorf("set effective plan in postgres: %w", err)
	}

	// Step 3: DELETE Redis key. Any concurrent backfill that read old
	// Postgres data is now wiped. The next request will either:
	//   - Miss Redis → read fresh Postgres → correct
	//   - Hit Redis from a concurrent request that read NEW Postgres → correct
	if r.redis != nil {
		key := effectivePlanKeyPrefix + userID
		if err := r.redis.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("delete redis effective plan key: %w", err)
		}
	}

	return nil
}

// InvalidateHubMembers invalidates all members of a hub.
// Called when: hub plan changes, hub subscription status changes.
func (r *Resolver) InvalidateHubMembers(ctx context.Context, hubID string) error {
	members, err := r.store.ListHubMembers(hubID)
	if err != nil {
		return fmt.Errorf("list hub members: %w", err)
	}

	// Also invalidate the hub plan Redis cache
	if r.redis != nil {
		r.redis.Del(ctx, hubPlanKeyPrefix+hubID)
	}

	var lastErr error
	for _, member := range members {
		if err := r.InvalidateUser(ctx, member.UserID); err != nil {
			r.logger.Warn("planresolver: failed to invalidate hub member",
				"error", err, "user_id", member.UserID, "hub_id", hubID)
			lastErr = err
		}
	}
	return lastErr
}

// ─── Scoped Entitlement Resolvers ───────────────────────────────────────────
//
// These replace the generic ResolveForRequest with explicit, operation-typed
// methods. Each method encodes exactly which plan sources are consulted and
// returns full provenance in the EntitlementSource.
//
// Operation matrix (from design doc):
//   recall, ask          → max(personal, best_hub)    [ResolveReadEntitlements]
//   push to personal hub → max(personal, best_hub)    [ResolvePersonalWriteEntitlements]
//   push to team hub     → target hub's plan ONLY     [ResolveHubWriteEntitlements]
//   hub settings/caps    → target hub's plan          [ResolveHubManagementEntitlements]
//   hub creation/transfer→ personal plan + counts     [ResolveOwnershipEntitlements]

// ResolveReadEntitlements returns limits for read operations (recall, ask).
// Uses max(personal, best_hub) by entitlement_rank — read benefits travel
// with the person.
func (r *Resolver) ResolveReadEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements {
	effectivePlanID := r.GetEffectivePlanID(ctx, userID)
	limits := r.registry.GetUserLimits(ctx, userID, effectivePlanID)

	source := r.buildSource(ctx, userID, effectivePlanID)
	r.applyOverrides(ctx, userID, &limits)

	return model.ResolvedEntitlements{
		Context: model.EntitlementRead,
		Limits:  limits,
		Source:  source,
	}
}

// ResolvePersonalWriteEntitlements returns limits for writes to the user's
// personal hub (or no explicit hub context). Same resolution as reads:
// max(personal, best_hub).
func (r *Resolver) ResolvePersonalWriteEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements {
	effectivePlanID := r.GetEffectivePlanID(ctx, userID)
	limits := r.registry.GetUserLimits(ctx, userID, effectivePlanID)

	source := r.buildSource(ctx, userID, effectivePlanID)
	r.applyOverrides(ctx, userID, &limits)

	return model.ResolvedEntitlements{
		Context: model.EntitlementPersonalWrite,
		Limits:  limits,
		Source:  source,
	}
}

// GetHubMemoryLimit returns the memory_limit for a specific hub's
// subscription plan — independent of any user's entitlements. Used by
// the meter to enforce the hub-level memory cap on push (separate from
// the owner cap). Returns -1 (unlimited) if the hub's plan is unlimited
// or the plan cannot be resolved (fail-open: never block a push because
// we couldn't read a plan).
//
// The resolution mirrors ResolveHubWriteEntitlements: hub subscription
// plan first, hub_free_team as default when inactive/missing, -1 as the
// absolute fallback.
func (r *Resolver) GetHubMemoryLimit(ctx context.Context, hubID string) int {
	if hubID == "" {
		return -1
	}
	hubPlanID := r.getHubPlanID(ctx, hubID)
	hubPlan := r.registry.GetPlan(hubPlanID)
	if hubPlan == nil || !hubPlan.Active {
		hubPlan = r.registry.GetPlan(model.HubFreeTeamPlanID)
	}
	if hubPlan == nil {
		return -1
	}
	return hubPlan.MemoryLimit
}

// ResolveHubWriteEntitlements returns limits for writes to a specific team hub.
// Uses the target hub's subscription plan ONLY for operation limits (push, recall,
// ask, rate). MemoryLimit comes from the user's effective personal plan since
// memory count is per-owner inventory.
//
// This is the key product rule: you cannot use Hub A's paid plan to write into
// Hub B. Each hub owner pays for their own hub's experience.
func (r *Resolver) ResolveHubWriteEntitlements(ctx context.Context, userID, hubID string) model.ResolvedEntitlements {
	hubPlanID := r.getHubPlanID(ctx, hubID)
	hubPlan := r.registry.GetPlan(hubPlanID)

	// If the hub has no subscription or plan is inactive, use hub_free_team
	// as the default team hub plan. We do NOT fall back to personal entitlements
	// because that would let a Pro+ user bypass the free team hub's lower limits
	// (violating the "writes use target hub plan" rule).
	if hubPlan == nil || !hubPlan.Active {
		hubPlan = r.registry.GetPlan(model.HubFreeTeamPlanID)
		if hubPlan == nil {
			// Absolute fallback — hub_free_team plan row missing from registry
			ent := r.ResolvePersonalWriteEntitlements(ctx, userID)
			ent.Context = model.EntitlementHubWrite
			return ent
		}
	}

	// Hub plan for operation limits
	limits := limitsFromPlan(hubPlan)

	// MemoryLimit stays personal (per-owner inventory, not per-hub)
	effectivePlanID := r.GetEffectivePlanID(ctx, userID)
	personalLimits := r.registry.GetUserLimits(ctx, userID, effectivePlanID)
	limits.MemoryLimit = personalLimits.MemoryLimit

	r.applyOverrides(ctx, userID, &limits)

	return model.ResolvedEntitlements{
		Context: model.EntitlementHubWrite,
		Limits:  limits,
		Source: model.EntitlementSource{
			Scope:  model.PlanScopeHub,
			PlanID: hubPlan.ID,
			HubID:  hubID,
			Reason: "target_hub",
		},
	}
}

// ResolveHubManagementEntitlements returns the hub's plan limits for settings,
// member cap, and feature gates. This is NOT per-user — it reflects what the
// hub subscription provides.
func (r *Resolver) ResolveHubManagementEntitlements(ctx context.Context, hubID string) model.ResolvedEntitlements {
	hubPlanID := r.getHubPlanID(ctx, hubID)
	hubPlan := r.registry.GetPlan(hubPlanID)

	if hubPlan == nil || !hubPlan.Active {
		// No subscription — return free team defaults
		hubPlan = r.registry.GetPlan(model.HubFreeTeamPlanID)
		if hubPlan == nil {
			return model.ResolvedEntitlements{
				Context: model.EntitlementHubManage,
				Limits:  model.UserLimits{PlanID: model.HubFreeTeamPlanID, PlanDisplayName: "Free Team"},
				Source:  model.EntitlementSource{Scope: model.PlanScopeHub, PlanID: model.HubFreeTeamPlanID, HubID: hubID, Reason: "target_hub"},
			}
		}
	}

	return model.ResolvedEntitlements{
		Context: model.EntitlementHubManage,
		Limits:  limitsFromPlan(hubPlan),
		Source: model.EntitlementSource{
			Scope:  model.PlanScopeHub,
			PlanID: hubPlan.ID,
			HubID:  hubID,
			Reason: "target_hub",
		},
	}
}

// ResolveOwnershipEntitlements returns the user's personal plan ownership caps.
// Used for hub creation and ownership transfer validation.
func (r *Resolver) ResolveOwnershipEntitlements(ctx context.Context, userID string) (model.OwnershipEntitlements, error) {
	// Get user's personal plan
	user, err := r.store.GetUser(userID)
	if err != nil {
		return model.OwnershipEntitlements{}, fmt.Errorf("get user: %w", err)
	}

	planID := user.PersonalPlanID
	if planID == "" {
		planID = model.LegacyToScopedPlanID(user.Plan)
	}

	plan := r.registry.GetPlan(planID)
	if plan == nil {
		plan = r.registry.GetPlan(model.PersonalFreePlanID)
	}

	maxHubs := 0
	if plan != nil {
		maxHubs = plan.MaxOwnedFreeTeamHubs
	}

	// Count current owned free team hubs
	currentCount, err := r.store.CountOwnedFreeTeamHubs(ctx, userID)
	if err != nil {
		return model.OwnershipEntitlements{}, fmt.Errorf("count owned free team hubs: %w", err)
	}

	return model.OwnershipEntitlements{
		PersonalPlanID:       planID,
		MaxOwnedFreeTeamHubs: maxHubs,
		CurrentOwnedCount:    currentCount,
		CanCreateFreeTeamHub: currentCount < maxHubs,
	}, nil
}

// buildSource constructs an EntitlementSource for the given effective plan.
func (r *Resolver) buildSource(ctx context.Context, userID, effectivePlanID string) model.EntitlementSource {
	plan := r.registry.GetPlan(effectivePlanID)
	if plan == nil {
		return model.EntitlementSource{Scope: model.PlanScopePersonal, PlanID: effectivePlanID, Reason: "personal"}
	}

	// If the effective plan is a hub plan, find which hub it came from
	if plan.IsHub() {
		hubPlans, err := r.store.GetUserHubPlans(ctx, userID)
		if err == nil {
			for _, hp := range hubPlans {
				if hp.PlanID == effectivePlanID {
					return model.EntitlementSource{
						Scope:  model.PlanScopeHub,
						PlanID: effectivePlanID,
						HubID:  hp.HubID,
						Reason: "best_hub",
					}
				}
			}
		}
	}

	return model.EntitlementSource{Scope: model.PlanScopePersonal, PlanID: effectivePlanID, Reason: "personal"}
}

// --- Internal helpers ---

// getHubPlanID returns the plan ID for a hub's active subscription.
// Cached in Redis for 5 minutes.
func (r *Resolver) getHubPlanID(ctx context.Context, hubID string) string {
	// Redis cache
	if r.redis != nil {
		key := hubPlanKeyPrefix + hubID
		if planID, err := r.redis.Get(ctx, key).Result(); err == nil {
			return planID // may be "" if hub has no subscription (cached negative)
		}
	}

	// Postgres lookup
	planID, err := r.store.GetHubPlanID(ctx, hubID)
	if err != nil {
		planID = "" // no subscription
	}

	// Cache in Redis (including empty = no subscription)
	if r.redis != nil {
		key := hubPlanKeyPrefix + hubID
		r.redis.Set(ctx, key, planID, hubPlanTTL)
	}

	return planID
}

// DreamLimits is the resolved dream quota for a request, with the
// limits already merged across the user's personal plan and any hub
// plans they belong to. The merge is FIELD-WISE for dream fields
// specifically (max across candidates, -1 wins as unlimited) — see
// docs/plans/23-dream-tiers.md.
type DreamLimits struct {
	BasicLimit       int    // -1 unlimited, 0 disabled, >0 finite cap
	LucidLimit       int    // same convention
	BasicQuotaSource string // plan ID that contributed BasicLimit
	LucidQuotaSource string // plan ID that contributed LucidLimit
}

// ResolveDreamLimits computes a user's effective dream quotas using
// the field-wise rule: each dream limit is the max across all
// candidate plans (personal plan + every hub plan they belong to),
// with -1 (unlimited) winning over any finite value.
//
// This is the personal-hub view. For team-hub triggers, the hub's
// own plan is the only candidate (not max-ed with personal) — call
// GetHubDreamLimits instead.
//
// Frozen hubs are excluded from the candidate set in two layers:
//
//  1. GetUserHubPlans only returns hubs with an active subscription
//     row (status='active'), filtering cancelled/past_due.
//  2. ListFrozenHubIDs additionally filters hubs whose subscription
//     is "active" but over-limit past the grace window. Without this
//     second pass, an over-limit team hub could still elevate its
//     members' personal dream quota even though the hub itself is
//     frozen.
//
// Per-user plan overrides (PlanOverride.Overrides) are NOT applied
// to dream limits in v1. Overrides target UserLimits fields and
// dream limits are resolved on a parallel field-wise path; if
// operators need per-user dream-quota adjustments later, we add an
// explicit override channel rather than reusing the existing one.
func (r *Resolver) ResolveDreamLimits(ctx context.Context, userID string) DreamLimits {
	// Personal plan candidate.
	user, err := r.store.GetUser(userID)
	personalPlanID := model.PersonalFreePlanID
	if err == nil && user != nil {
		if user.PersonalPlanID != "" {
			personalPlanID = user.PersonalPlanID
		} else if user.Plan != "" {
			personalPlanID = model.LegacyToScopedPlanID(user.Plan)
		}
	}

	// Seed result from the personal plan.
	result := DreamLimits{
		BasicLimit:       0,
		LucidLimit:       0,
		BasicQuotaSource: personalPlanID,
		LucidQuotaSource: personalPlanID,
	}
	if p := r.registry.GetPlan(personalPlanID); p != nil {
		result.BasicLimit = p.BasicDreamLimit
		result.LucidLimit = p.LucidDreamLimit
	}

	// Hub-plan candidates. Each hub the user belongs to may contribute
	// stronger limits via the field-wise max rule.
	hubPlans, err := r.store.GetUserHubPlans(ctx, userID)
	if err != nil {
		r.logger.Warn("planresolver: failed to get user hub plans for dream resolve",
			"error", err, "user_id", userID)
		return result
	}
	if len(hubPlans) == 0 {
		return result
	}

	// Frozen-hub filter: even when the subscription row says
	// status='active', the hub may be frozen due to over-limit past
	// the HubGracePeriod. We mirror the convention from
	// stripFrozenHubsFromScope (handler/hub_scope.go) — collect the
	// candidate hub IDs, ask the store which are frozen, then drop
	// those from elevation. A store error degrades to "include all"
	// (matches the recall-path posture: an outage on the
	// subscriptions table shouldn't take down quota resolution).
	hubIDs := make([]string, 0, len(hubPlans))
	for _, hp := range hubPlans {
		hubIDs = append(hubIDs, hp.HubID)
	}
	frozenSet := make(map[string]struct{})
	cutoff := time.Now().UTC().Add(-model.HubGracePeriod)
	if frozen, err := r.store.ListFrozenHubIDs(ctx, hubIDs, cutoff); err == nil {
		for _, id := range frozen {
			frozenSet[id] = struct{}{}
		}
	} else {
		r.logger.Warn("planresolver: list frozen hubs failed; including all hubs in dream resolve",
			"error", err, "hub_count", len(hubIDs))
	}

	for _, hp := range hubPlans {
		if _, isFrozen := frozenSet[hp.HubID]; isFrozen {
			continue
		}
		p := r.registry.GetPlan(hp.PlanID)
		if p == nil || !p.Active {
			continue
		}
		if newBasic := maxLimit(result.BasicLimit, p.BasicDreamLimit); newBasic != result.BasicLimit {
			result.BasicLimit = newBasic
			result.BasicQuotaSource = p.ID
		}
		if newLucid := maxLimit(result.LucidLimit, p.LucidDreamLimit); newLucid != result.LucidLimit {
			result.LucidLimit = newLucid
			result.LucidQuotaSource = p.ID
		}
	}
	return result
}

// GetHubDreamLimits returns the dream limits for a team-hub trigger.
// Hub plan's limits only — no max-merge with the user's personal
// plan, by design (Team's 100/hub/mo is a fixed-per-hub bucket).
//
// If the hub has no active subscription row, falls back to the
// hub_free_team plan's limits, matching the convention in the rest
// of the hub-resolver paths. hub_free_team currently has 0/0 for
// dream limits — i.e., team-hub triggers on a free hub get no Lucid
// quota — which is the correct product behavior.
func (r *Resolver) GetHubDreamLimits(ctx context.Context, hubID string) DreamLimits {
	hubPlanID := r.getHubPlanID(ctx, hubID)
	if hubPlanID == "" {
		hubPlanID = model.HubFreeTeamPlanID
	}
	p := r.registry.GetPlan(hubPlanID)
	if p == nil {
		return DreamLimits{
			BasicQuotaSource: hubPlanID,
			LucidQuotaSource: hubPlanID,
		}
	}
	return DreamLimits{
		BasicLimit:       p.BasicDreamLimit,
		LucidLimit:       p.LucidDreamLimit,
		BasicQuotaSource: p.ID,
		LucidQuotaSource: p.ID,
	}
}

// computeEffectivePlan computes max(personal, best_hub) by entitlement_rank.
// Returns (planID, source).
func (r *Resolver) computeEffectivePlan(ctx context.Context, userID string) (string, string) {
	// Get user's personal plan — prefer scoped PersonalPlanID, fall back to legacy Plan
	user, err := r.store.GetUser(userID)
	personalPlanID := model.PersonalFreePlanID
	if err == nil && user != nil {
		if user.PersonalPlanID != "" {
			personalPlanID = user.PersonalPlanID
		} else {
			personalPlanID = model.LegacyToScopedPlanID(user.Plan)
		}
	}

	// Get all hub plans the user has access to
	hubPlans, err := r.store.GetUserHubPlans(ctx, userID)
	if err != nil {
		r.logger.Warn("planresolver: failed to get user hub plans",
			"error", err, "user_id", userID)
		return personalPlanID, "personal"
	}

	// Find the highest-ranked plan using entitlement_rank (cross-scope comparison)
	bestRank := 0
	bestPlan := personalPlanID
	bestSource := "personal"
	if p := r.registry.GetPlan(personalPlanID); p != nil {
		bestRank = p.EntitlementRank
	}
	for _, hp := range hubPlans {
		if p := r.registry.GetPlan(hp.PlanID); p != nil && p.Active && p.EntitlementRank > bestRank {
			bestRank = p.EntitlementRank
			bestPlan = p.ID
			bestSource = "hub:" + hp.HubID
		}
	}

	return bestPlan, bestSource
}

// limitsFromPlan creates UserLimits from a Plan (without per-user overrides).
//
// Dream limits are NOT included here — they're resolved separately
// via ResolveDreamLimits / GetHubDreamLimits using a different rule
// (field-wise max across all candidate plans, vs the rank-wins
// approach the rest of UserLimits uses). Mixing the two on one
// struct would let callers read dream fields from a rank-wins-
// resolved instance and silently get the wrong value.
func limitsFromPlan(plan *model.Plan) model.UserLimits {
	return model.UserLimits{
		PlanID:             plan.ID,
		PlanDisplayName:    plan.DisplayName,
		MemoryLimit:        plan.MemoryLimit,
		PushLimit:          plan.PushLimit,
		RecallLimit:        plan.RecallLimit,
		AskLimit:           plan.AskLimit,
		MaxAttachmentBytes: plan.MaxAttachmentBytes,
		StorageBytesLimit:  plan.StorageBytesLimit,
		AskModel:           plan.AskModel,
		DreamsEnabled:      plan.DreamsEnabled,
		ReviewInbox:        plan.ReviewInbox,
		MaxTeamHubs:        plan.MaxTeamHubs,
		RateLimitRPM:       plan.RateLimitRPM,
		RateLimitHeavyRPM:  plan.RateLimitHeavyRPM,
		RateLimitLightRPM:  plan.RateLimitLightRPM,
	}
}

// maxLimit returns the larger of two integer limit fields, treating
// -1 as "unlimited" (always wins) and 0 as "disabled" (loses to any
// positive value). Used by ResolveDreamLimits for the field-wise
// dream-limit resolution.
//
// Truth table:
//
//	-1 vs anything → -1   (unlimited beats finite/disabled/anything)
//	 0 vs N>0     →  N    (disabled loses to a finite enable)
//	 N vs M>=0    → max   (standard greater-of-two)
func maxLimit(a, b int) int {
	if a == -1 || b == -1 {
		return -1
	}
	if a > b {
		return a
	}
	return b
}

// applyOverrides merges per-user admin overrides onto limits.
func (r *Resolver) applyOverrides(ctx context.Context, userID string, limits *model.UserLimits) {
	override, err := r.store.GetPlanOverride(ctx, userID)
	if err != nil || override == nil || len(override.Overrides) == 0 {
		return
	}
	plans.ApplyOverrides(limits, override.Overrides)
}
