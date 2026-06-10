package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Registry is an in-memory cache of plan definitions loaded from Postgres.
// It provides fast, thread-safe access to plan data for metering and handlers.
type Registry struct {
	store store.Store

	mu    sync.RWMutex
	plans map[string]*model.Plan // plan ID → plan
}

// NewRegistry creates a registry. Call Load() then Start() to initialize.
func NewRegistry(s store.Store) *Registry {
	return &Registry{
		store: s,
		plans: make(map[string]*model.Plan),
	}
}

// Load synchronously loads all plans from Postgres. This is called during
// server startup; if it fails, the server should fail fast (running with
// no plan definitions is nonsensical).
func (r *Registry) Load(ctx context.Context) error {
	return r.Reload(ctx)
}

// Start begins a background goroutine that reloads plans every interval.
// Returns a cancel function that should be registered with app.addCleanup.
func (r *Registry) Start(ctx context.Context, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	go r.reloadLoop(ctx, interval)
	return cancel
}

func (r *Registry) reloadLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Reload(ctx); err != nil {
				slog.Warn("plan registry reload failed", "error", err)
			}
		}
	}
}

// Reload fetches all active plans from Postgres and atomically swaps
// the in-memory map.
func (r *Registry) Reload(ctx context.Context) error {
	plans, err := r.store.ListPlans(ctx)
	if err != nil {
		return fmt.Errorf("load plans: %w", err)
	}
	m := make(map[string]*model.Plan, len(plans))
	for i := range plans {
		m[plans[i].ID] = &plans[i]
	}
	r.mu.Lock()
	r.plans = m
	r.mu.Unlock()
	slog.Debug("plan registry reloaded", "count", len(m))
	return nil
}

// GetPlan returns a copy of the plan by ID, or nil if not found.
// Returns a value copy so callers cannot mutate cached plans.
func (r *Registry) GetPlan(planID string) *model.Plan {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p := r.plans[planID]
	if p == nil {
		return nil
	}
	return copyPlan(p)
}

// AllPlans returns copies of all active plans sorted by scope then tier_order.
func (r *Registry) AllPlans() []model.Plan {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Plan, 0, len(r.plans))
	for _, p := range r.plans {
		out = append(out, *copyPlan(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope // "hub" < "personal"
		}
		return out[i].TierOrder < out[j].TierOrder
	})
	return out
}

// PlansByScope returns copies of active plans filtered by scope, sorted by tier_order.
func (r *Registry) PlansByScope(scope model.PlanScope) []model.Plan {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []model.Plan
	for _, p := range r.plans {
		if p.Scope == scope {
			out = append(out, *copyPlan(p))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TierOrder < out[j].TierOrder
	})
	return out
}

// GetPlanByEntitlementRank returns the plan with the highest entitlement_rank
// among the given plan IDs. Used for max() resolution across personal/hub plans.
func (r *Registry) GetPlanByEntitlementRank(planIDs ...string) *model.Plan {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best *model.Plan
	for _, id := range planIDs {
		p := r.plans[id]
		if p == nil || !p.Active {
			continue
		}
		if best == nil || p.EntitlementRank > best.EntitlementRank {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	return copyPlan(best)
}

// copyPlan returns a deep copy of a Plan, including the Features map.
func copyPlan(p *model.Plan) *model.Plan {
	cp := *p
	if p.Features != nil {
		cp.Features = make(map[string]any, len(p.Features))
		for k, v := range p.Features {
			cp.Features[k] = v
		}
	}
	return &cp
}

// GetUserLimits resolves a user's effective limits by combining their plan
// with any per-user overrides. If the plan is not found, returns free-tier
// defaults. Accepts both legacy and scoped plan IDs.
func (r *Registry) GetUserLimits(ctx context.Context, userID, planID string) model.UserLimits {
	plan := r.GetPlan(planID)
	if plan == nil {
		// Try scoped equivalent of legacy ID
		plan = r.GetPlan(model.LegacyToScopedPlanID(planID))
	}
	if plan == nil {
		plan = r.GetPlan(model.PersonalFreePlanID)
	}
	if plan == nil {
		plan = r.GetPlan(model.LegacyFreePlanID)
	}
	if plan == nil {
		// Absolute fallback if no plan rows exist
		return model.UserLimits{
			PlanID:             model.PersonalFreePlanID,
			PlanDisplayName:    "Free",
			MemoryLimit:        300,
			PushLimit:          200,
			RecallLimit:        500,
			AskLimit:           10,
			MaxAttachmentBytes: 5 * 1024 * 1024,
			StorageBytesLimit:  500 * 1024 * 1024,
			AskModel:           "haiku",
			RateLimitRPM:       60,
			RateLimitHeavyRPM:  10,
			RateLimitLightRPM:  60,
		}
	}

	// Dream limits are not threaded through UserLimits — see the
	// note on model.UserLimits and use planresolver.ResolveDreamLimits.
	limits := model.UserLimits{
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

	// Apply per-user overrides
	override, err := r.store.GetPlanOverride(ctx, userID)
	if err != nil || override == nil {
		return limits
	}
	ApplyOverrides(&limits, override.Overrides)
	return limits
}

// ApplyOverrides merges the override map onto the limits struct.
func ApplyOverrides(limits *model.UserLimits, overrides map[string]any) {
	if len(overrides) == 0 {
		return
	}
	// Marshal overrides to JSON and unmarshal onto a copy for type-safe merging.
	raw, err := json.Marshal(overrides)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, limits)
}
