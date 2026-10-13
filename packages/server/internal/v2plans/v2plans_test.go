package v2plans_test

import (
	"context"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2plans"
)

// registry loads the migrated plan rows.
func registry(t *testing.T) *plans.Registry {
	t.Helper()
	db := testdb.Open(t, testdb.Options{})
	r := plans.NewRegistry(db.Store)
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestRowsAreD9: migration 069 gives V1's plan rows D9's limits, V1's plans
// mapped onto Free, Pro and Team and grandfathered (plan 25 §10).
func TestRowsAreD9(t *testing.T) {
	s := v2plans.New(registry(t), true)
	free := policy.Plan{Tier: policy.TierFree, Enforced: true, ProjectSpaces: 1, AgentKinds: 3, Asks: policy.FreeAskLimit,
		DreamCadence: policy.CadenceWeekly, ActivityDays: 30}
	pro := policy.Plan{Tier: policy.TierPro, Enforced: true, ProjectSpaces: policy.Unlimited, AgentKinds: policy.Unlimited, Asks: 1000,
		DreamCadence: policy.CadenceNightly, NightlySpaces: 5, ActivityDays: 90}
	team := policy.Plan{Tier: policy.TierTeam, Enforced: true, ProjectSpaces: policy.Unlimited, AgentKinds: policy.Unlimited, Asks: 1000,
		DreamCadence: policy.CadenceNightly, NightlySpaces: policy.Unlimited, ActivityDays: 365, SeatBilled: true}
	enterprise := policy.Plan{Tier: policy.TierEnterprise, Enforced: true, ProjectSpaces: policy.Unlimited, AgentKinds: policy.Unlimited,
		Asks: policy.Unlimited, DreamCadence: policy.CadenceNightly, NightlySpaces: policy.Unlimited, ActivityDays: policy.Unlimited, SeatBilled: true}
	for id, want := range map[string]policy.Plan{
		model.PersonalFreePlanID:        free,
		model.HubFreeTeamPlanID:         free,
		model.PersonalEarlyAccessPlanID: pro,
		model.PersonalProPlanID:         pro,
		model.PersonalProPlusPlanID:     pro,
		model.HubTeamPlanID:             team,
		model.HubEnterprisePlanID:       enterprise,
	} {
		got := s.Plan(id)
		if got.ID != id || got.Name == "" {
			t.Errorf("%s: id %q name %q", id, got.ID, got.Name)
		}
		got.ID, got.Name = "", ""
		if got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", id, got, want)
		}
	}
}

// TestPersonAndSpace: a person's plan is their row (a legacy id its scoped
// twin, an unknown one Free); a space's is the better of its team's
// subscription and its owner's plan.
func TestPersonAndSpace(t *testing.T) {
	s := v2plans.New(registry(t), true)
	for _, c := range []struct {
		name string
		got  policy.Plan
		want string
	}{
		{"a person on Pro", s.Person(model.PersonalProPlanID), model.PersonalProPlanID},
		{"a legacy id", s.Person(model.LegacyProPlanID), model.PersonalProPlanID},
		{"an unknown row is Free", s.Person("nonesuch"), model.PersonalFreePlanID},
		{"a Pro owner's space", s.Space("", model.PersonalProPlanID), model.PersonalProPlanID},
		{"a Free owner's team on Team", s.Space(model.HubTeamPlanID, model.PersonalFreePlanID), model.HubTeamPlanID},
		{"a Pro owner's free team is Pro", s.Space(model.HubFreeTeamPlanID, model.PersonalProPlanID), model.PersonalProPlanID},
		{"a Free owner's free team is Free Team", s.Space(model.HubFreeTeamPlanID, model.PersonalFreePlanID), model.HubFreeTeamPlanID},
	} {
		if c.got.ID != c.want {
			t.Errorf("%s: %s, want %s", c.name, c.got.ID, c.want)
		}
	}
}

// TestNotEnforcedByDefault: the plans' limits are reported either way, and
// enforced only when V2_PLAN_LIMITS says so.
func TestNotEnforcedByDefault(t *testing.T) {
	r := registry(t)
	for _, c := range []struct {
		env  map[string]string
		want bool
		err  bool
	}{
		{map[string]string{}, false, false},
		{map[string]string{"V2_PLAN_LIMITS": "off"}, false, false},
		{map[string]string{"V2_PLAN_LIMITS": " Enforce "}, true, false},
		{map[string]string{"V2_PLAN_LIMITS": "on"}, false, true},
	} {
		got, err := v2plans.EnforceFromEnv(func(k string) (string, bool) { v, ok := c.env[k]; return v, ok })
		if got != c.want || (err != nil) != c.err {
			t.Errorf("%v: %v, %v", c.env, got, err)
		}
		p := v2plans.New(r, got).Person(model.PersonalFreePlanID)
		if p.Enforced != c.want || p.ProjectSpaces != 1 {
			t.Errorf("%v: %+v", c.env, p)
		}
	}
	var none *v2plans.Service
	if none.Enforced() || none.Person(model.PersonalFreePlanID) != (policy.Plan{}) || v2plans.New(nil, true) != nil {
		t.Error("no registry enforces nothing")
	}
}

// TestEmptyRegistryRefusesNothing: a registry that failed to load knows no
// plan, so every plan is the zero Plan and nothing is enforced.
func TestEmptyRegistryRefusesNothing(t *testing.T) {
	r := plans.NewRegistry(store.NewInMemoryStore())
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := v2plans.New(r, true)
	if p := s.Person(model.PersonalFreePlanID); p.Enforced || p.ProjectSpaces != 0 {
		t.Errorf("person: %+v", p)
	}
	if p := s.Space("", model.PersonalFreePlanID); p.Enforced {
		t.Errorf("space: %+v", p)
	}
}
