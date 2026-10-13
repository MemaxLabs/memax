// Package v2plans makes the V2 plans policy decides under (policy.Plan,
// D9) of the plans registry's rows (internal/plans; the V2 columns are
// migration 069's). It is the ledger's Plans: the scope a request reads
// names each grant's plan rows (the person's effective plan, the space's
// team subscription and its owner's plan), and Decide asks this package
// what they allow.
//
// Whether policy refuses on the limits is one switch, V2_PLAN_LIMITS:
// "enforce" turns them on; unset or "off" leaves every plan's limits
// reported (Settings › Plan) and nothing refused on them, as during the
// free alpha. The registry's values change with the rows (it reloads every
// minute); the switch is read once at startup.
package v2plans

import (
	"fmt"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
)

// Service is the ledger's Plans over the plans registry.
type Service struct {
	registry *plans.Registry
	enforce  bool
}

// New returns the plans of registry's rows, enforced or not, or nil
// without a registry (no database: nothing is known, and nothing is
// enforced).
func New(registry *plans.Registry, enforce bool) *Service {
	if registry == nil {
		return nil
	}
	return &Service{registry: registry, enforce: enforce}
}

// EnforceFromEnv reads V2_PLAN_LIMITS: "enforce" is true; unset, empty or
// "off" is false; anything else is an error, so a typo never turns limits
// on or off by surprise.
func EnforceFromEnv(lookup func(string) (string, bool)) (bool, error) {
	v, _ := lookup("V2_PLAN_LIMITS")
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "off":
		return false, nil
	case "enforce":
		return true, nil
	}
	return false, fmt.Errorf(`V2_PLAN_LIMITS is %q: use "enforce" or "off"`, v)
}

// Enforced says whether policy refuses on the plans' limits.
func (s *Service) Enforced() bool { return s != nil && s.enforce }

// Person is the plan of a person on the plan row id: that row, its scoped
// twin for a legacy id, else Free's.
func (s *Service) Person(id string) policy.Plan {
	if s == nil {
		return policy.Plan{}
	}
	p := s.registry.GetPlan(id)
	if p == nil {
		p = s.registry.GetPlan(model.LegacyToScopedPlanID(id))
	}
	if p == nil {
		p = s.registry.GetPlan(model.PersonalFreePlanID)
	}
	return s.of(p)
}

// Space is the plan of a space: the better of its team's subscription
// (hubID's plan row, "" without one) and its owner's plan, by the rows'
// entitlement rank, as V1's resolver compares a personal and a hub plan.
func (s *Service) Space(hubPlanID, ownerPlanID string) policy.Plan {
	if s == nil {
		return policy.Plan{}
	}
	if p := s.registry.GetPlanByEntitlementRank(hubPlanID, ownerPlanID); p != nil {
		return s.of(p)
	}
	return s.Person(ownerPlanID)
}

// Plan is the plan row id as policy sees it, enforced or not; the zero
// Plan for a row the registry doesn't have.
func (s *Service) Plan(id string) policy.Plan {
	if s == nil {
		return policy.Plan{}
	}
	return s.of(s.registry.GetPlan(id))
}

// of makes a policy.Plan of a row; the zero Plan (nothing enforced) when
// there is none, so a registry that failed to load never refuses.
func (s *Service) of(p *model.Plan) policy.Plan {
	if p == nil {
		return policy.Plan{}
	}
	tier := policy.PlanTier(p.V2Tier)
	if !tier.Valid() {
		tier = policy.TierFree
	}
	return policy.Plan{
		ID: p.ID, Tier: tier, Name: p.DisplayName, Enforced: s.enforce,
		ProjectSpaces: p.ProjectSpaceLimit, AgentKinds: p.AgentKindLimit, Asks: p.V2AskLimit,
		DreamCadence: p.DreamCadence, NightlySpaces: p.DreamNightlySpaces,
		ActivityDays: p.ActivityRetentionDays, SeatBilled: p.SeatBilled,
	}
}
