package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Registry is an in-memory cache of plan definitions loaded from Postgres.
// It provides fast, thread-safe access to plan data for metering and handlers.
type Registry struct {
	store store.Store

	mu    sync.RWMutex
	plans map[string]*model.Plan // plan ID → plan

	// overrides caches each person's limit overrides (UserOverrides).
	omu       sync.Mutex
	overrides map[string]cachedOverrides
}

// OverridesTTL is how long a person's limit overrides are kept in memory.
// They change only when an operator sets them, yet every rate-limited
// request read them from Postgres (twice: the registry's limits and the
// resolver's), a round trip of about 24 ms each in production. An
// operator's change applies on its own process at once (ForgetOverrides)
// and on every other within the TTL.
const OverridesTTL = time.Minute

// maxCachedOverrides bounds the cache: past it, expired entries go, and if
// that isn't enough, all of them.
const maxCachedOverrides = 50_000

type cachedOverrides struct {
	values map[string]any
	until  time.Time
}

// NewRegistry creates a registry. Call Load() then Start() to initialize.
func NewRegistry(s store.Store) *Registry {
	return &Registry{
		store:     s,
		plans:     make(map[string]*model.Plan),
		overrides: make(map[string]cachedOverrides),
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
	ApplyOverrides(&limits, r.UserOverrides(ctx, userID))
	return limits
}

// UserOverrides returns a person's limit overrides (none is nil), from
// memory when read within OverridesTTL, else from the store. A failed read
// isn't kept: the next request asks again.
func (r *Registry) UserOverrides(ctx context.Context, userID string) map[string]any {
	if r == nil {
		return nil
	}
	now := time.Now()
	r.omu.Lock()
	if e, ok := r.overrides[userID]; ok && now.Before(e.until) {
		r.omu.Unlock()
		return e.values
	}
	r.omu.Unlock()
	if r.store == nil {
		return nil
	}
	o, err := r.store.GetPlanOverride(ctx, userID)
	var values map[string]any
	switch {
	case err == nil && o != nil:
		values = o.Overrides
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil
	}
	r.omu.Lock()
	if r.overrides == nil {
		r.overrides = make(map[string]cachedOverrides)
	}
	if len(r.overrides) >= maxCachedOverrides {
		for id, e := range r.overrides {
			if !now.Before(e.until) {
				delete(r.overrides, id)
			}
		}
		if len(r.overrides) >= maxCachedOverrides {
			clear(r.overrides)
		}
	}
	r.overrides[userID] = cachedOverrides{values: values, until: now.Add(OverridesTTL)}
	r.omu.Unlock()
	return values
}

// ForgetOverrides drops a person's cached overrides, after an operator
// set or removed them, so this process reads them again at once.
func (r *Registry) ForgetOverrides(userID string) {
	if r == nil {
		return
	}
	r.omu.Lock()
	delete(r.overrides, userID)
	r.omu.Unlock()
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
