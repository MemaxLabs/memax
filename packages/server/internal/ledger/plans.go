package ledger

import (
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Plans makes the V2 plans policy decides under (policy.Plan, D9) of the
// plan rows a grant names (SpaceGrant.PlanID, HubPlanID, OwnerPlanID: read
// with the scope, so no round trip). internal/v2plans implements it over
// the plans registry, enforcing the limits only with V2_PLAN_LIMITS. A
// ledger without one (WithPlans) decides under the zero Plan: nothing is
// enforced, as during the alpha.
type Plans interface {
	// Person is the plan of a person on the plan row planID (their
	// effective plan).
	Person(planID string) policy.Plan
	// Space is the plan of a space: the better of its team's subscription
	// (hubPlanID, "" without one) and its owner's plan.
	Space(hubPlanID, ownerPlanID string) policy.Plan
}

// WithPlans sets the plans decisions are made under.
func WithPlans(p Plans) Option { return func(l *Ledger) { l.plans = p } }

// PersonPlan is the plan of the person a grant is read for (the zero Plan
// without Plans).
func (l *Ledger) PersonPlan(g SpaceGrant) policy.Plan {
	if l == nil {
		return policy.Plan{}
	}
	return personPlan(l.plans, g)
}

// SpacePlan is the plan of the space a grant is for (the zero Plan without
// Plans).
func (l *Ledger) SpacePlan(g SpaceGrant) policy.Plan {
	if l == nil {
		return policy.Plan{}
	}
	return spacePlan(l.plans, g)
}

func personPlan(p Plans, g SpaceGrant) policy.Plan {
	if p == nil || g.PlanID == "" {
		return policy.Plan{}
	}
	return p.Person(g.PlanID)
}

func spacePlan(p Plans, g SpaceGrant) policy.Plan {
	if p == nil || g.OwnerPlanID == "" {
		return policy.Plan{}
	}
	return p.Space(g.HubPlanID, g.OwnerPlanID)
}

// activitySince is the oldest Activity a space on p lists at now, as a
// query parameter: nil for no window.
func activitySince(p policy.Plan, now time.Time) *time.Time {
	since := policy.ActivitySince(p, now)
	if since.IsZero() {
		return nil
	}
	since = since.UTC()
	return &since
}
