package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2plans"
)

// planFixture is a fixture whose ledger decides under the migrated plan
// rows, enforced or not (V2_PLAN_LIMITS).
func planFixture(t *testing.T, enforce bool) *fixture {
	t.Helper()
	db := testdb.Open(t, testdb.Options{})
	r := plans.NewRegistry(db.Store)
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newFixtureOn(t, db, ledger.WithPlans(v2plans.New(r, enforce)))
}

// onPlan puts a person on a plan row, as billing does (their personal
// plan), with the plan resolver's cache in step.
func (f *fixture) onPlan(user uuid.UUID, planID string) {
	f.t.Helper()
	f.exec(`UPDATE users SET personal_plan_id = $2 WHERE id = $1`, user, planID)
	f.exec(`DELETE FROM effective_plan_cache WHERE user_id = $1`, user)
}

// TestScopeReadsPlans: the scope's one statement names each grant's plan
// rows: the person's effective plan (the resolver's cache before their
// personal plan), the space's team subscription and its owner's plan.
func TestScopeReadsPlans(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner, member := f.user("owner"), f.user("member")
	f.onPlan(owner, "personal_pro")
	mine := f.space(owner, policy.SpaceProject, "acme")
	shared := f.space(owner, policy.SpaceTeam, "acme-team")
	f.join(shared, member, "contributor")
	f.exec(`INSERT INTO hub_subscriptions (hub_id, plan_id, billing_user_id) VALUES ($1, 'hub_team', $2)`, shared, owner)
	// The resolver found the member a better plan than their own (a team's).
	f.exec(`INSERT INTO effective_plan_cache (user_id, effective_plan, source) VALUES ($1, 'hub_team', 'hub:' || $2::text)`, member, shared)

	g, _ := f.scope(owner).Grant(mine)
	if g.PlanID != "personal_pro" || g.OwnerPlanID != "personal_pro" || g.HubPlanID != "" {
		t.Errorf("the owner's own space: %+v", g)
	}
	g, _ = f.scope(member).Grant(shared)
	if g.PlanID != "hub_team" || g.OwnerPlanID != "personal_pro" || g.HubPlanID != "hub_team" {
		t.Errorf("the member's team space: %+v", g)
	}
	// A system scope reads the space's plans too (Dream's run-now cap).
	sys, err := f.l.SpaceScope(context.Background(), shared)
	if err != nil {
		t.Fatal(err)
	}
	if g := sys.Spaces[0]; g.HubPlanID != "hub_team" || g.OwnerPlanID != "personal_pro" || g.PlanID != "" {
		t.Errorf("the system scope: %+v", g)
	}
}

// TestPlanLimitsInTheLedger: each limit the ledger decides on refuses with
// plan limits enforced, and the same command goes through without them.
func TestPlanLimitsInTheLedger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, enforce := range []bool{true, false} {
		f := planFixture(t, enforce)
		refusedIf := func(t *testing.T, res ledger.Result, code string) {
			t.Helper()
			if enforce {
				refusedWith(t, res, code)
				if res.Policy.Limit == nil {
					t.Errorf("%s without its limit", code)
				}
			} else if res.Outcome == ledger.OutcomeRefused {
				t.Errorf("refused during the alpha: %s (%s)", res.Policy.Code, res.Policy.Message)
			}
		}
		name := map[bool]string{true: "enforced", false: "not enforced"}[enforce]

		t.Run(name+": a second project space on Free", func(t *testing.T) {
			zz := f.user("zz")
			f.space(zz, policy.SpaceProject, "one")
			sp, _, err := f.l.CreateSpace(ctx, ledger.Actor{Kind: policy.ActorPerson, ID: zz, Credential: policy.CredentialSession},
				ledger.NewSpace{Name: "Two"})
			var refused *ledger.SpaceRefusedError
			switch {
			case enforce && (err == nil || !asSpaceRefused(err, &refused) || refused.Decision.Code != policy.CodeSpaceLimit):
				t.Fatalf("got %v, %v; want space_limit", sp, err)
			case enforce && (refused.Decision.Limit == nil || refused.Decision.Limit.Allowed != 1 || refused.Decision.Limit.Upgrade != policy.TierPro):
				t.Errorf("limit %+v", refused.Decision.Limit)
			case !enforce && err != nil:
				t.Fatalf("refused during the alpha: %v", err)
			}
			// Pro owns as many as they like.
			f.onPlan(zz, "personal_pro")
			if _, _, err := f.l.CreateSpace(ctx, ledger.Actor{Kind: policy.ActorPerson, ID: zz, Credential: policy.CredentialSession},
				ledger.NewSpace{Name: "Three"}); err != nil {
				t.Errorf("Pro's third: %v", err)
			}
		})

		t.Run(name+": a fourth kind of agent on Free", func(t *testing.T) {
			zz := f.user("zz")
			space := f.space(zz, policy.SpaceProject, "acme")
			for _, a := range []ledger.AgentKind{"claude-code", "codex", "cursor"} {
				f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, string(a), space), a, at(space, policy.AutonomyPropose))
			}
			// The same kind again (another machine) is no new kind.
			f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex", space), "codex", at(space, policy.AutonomyPropose))
			res := f.apply(&ledger.ConnectAgent{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
				Credential: ledger.CredentialOAuthGrant, CredentialID: f.grant(zz, "copilot", space), Agent: "copilot",
				Spaces: []ledger.SpaceAutonomy{at(space, policy.AutonomyPropose)}})
			refusedIf(t, res, policy.CodeAgentKindLimit)
			if enforce && (res.Policy.Limit.Allowed != 3 || res.Policy.Limit.Used != 3) {
				t.Errorf("limit %+v", res.Policy.Limit)
			}
		})

		t.Run(name+": a member keeps in a Pro owner's space", func(t *testing.T) {
			owner, member := f.user("owner"), f.user("member")
			f.onPlan(owner, "personal_pro")
			space := f.space(owner, policy.SpaceProject, "acme")
			f.join(space, member, "contributor")
			p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(owner), policy.ViaMCP),
				NewMemory: fact(space, "Deploys go through staging first.")}).Memory
			res := f.apply(&ledger.Keep{Meta: meta(person(member), f.scope(member), policy.ViaWeb), Memory: p.Ref, ExpectedVersion: 1})
			refusedIf(t, res, policy.CodeSeatNeeded)
			// A member's own Remember waits in Review, said as the seat.
			res = f.apply(&ledger.Remember{Meta: meta(person(member), f.scope(member), policy.ViaWeb),
				NewMemory: fact(space, "Releases are cut on Tuesdays.")})
			if enforce && (res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeSeatNeeded) {
				t.Errorf("remember: %s %s", res.Outcome, res.Policy.Code)
			}
			if !enforce && res.Outcome != ledger.OutcomeApplied {
				t.Errorf("remember during the alpha: %s %s", res.Outcome, res.Policy.Code)
			}
			// On Team every member holds a seat.
			f.exec(`INSERT INTO hub_subscriptions (hub_id, plan_id, billing_user_id) VALUES ($1, 'hub_team', $2)`, space, owner)
			q := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(owner), policy.ViaMCP),
				NewMemory: fact(space, "Every service has a runbook.")}).Memory
			if res := f.apply(&ledger.Keep{Meta: meta(person(member), f.scope(member), policy.ViaWeb), Memory: q.Ref, ExpectedVersion: 1}); res.Outcome != ledger.OutcomeApplied ||
				res.Memory.Lifecycle != lifecycle.Kept {
				t.Errorf("a member keeps on Team: %s %s", res.Outcome, res.Policy.Code)
			}
			// A bulk Keep follows the same rule.
			f.exec(`DELETE FROM hub_subscriptions WHERE hub_id = $1`, space)
			r := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(owner), policy.ViaMCP),
				NewMemory: fact(space, "Feature flags expire after a quarter.")}).Memory
			bulk := f.l.ApplyBulk(ctx, []ledger.Command{&ledger.Keep{Meta: meta(person(member), f.scope(member), policy.ViaWeb), Memory: r.Ref, ExpectedVersion: 1}})
			if bulk[0].Err != nil {
				t.Fatal(bulk[0].Err)
			}
			refusedIf(t, bulk[0].Result, policy.CodeSeatNeeded)
		})

		t.Run(name+": Activity on Free goes back 30 days", func(t *testing.T) {
			zz := f.user("zz")
			space := f.space(zz, policy.SpaceProject, "acme")
			m := f.remember(zz, space, "Every service has a runbook.")
			conn, now := uuid.New(), time.Now()
			f.record(readOf(m, conn, zz, "codex", ledger.ReadRecall, now.AddDate(0, 0, -40), m.ID),
				readOf(m, conn, zz, "codex", ledger.ReadRecall, now.AddDate(0, 0, -2), m.ID))
			page, err := f.l.ListReads(ctx, f.scope(zz), ledger.ReadQuery{SpaceID: space})
			if err != nil {
				t.Fatal(err)
			}
			want, days := 2, 0
			if enforce {
				want, days = 1, 30
			}
			if len(page.Reads) != want || page.RetentionDays != days {
				t.Errorf("%d reads over %d days, want %d over %d", len(page.Reads), page.RetentionDays, want, days)
			}
		})
	}
}

func asSpaceRefused(err error, out **ledger.SpaceRefusedError) bool {
	r, ok := err.(*ledger.SpaceRefusedError)
	if ok {
		*out = r
	}
	return ok
}

// TestDreamSweepReadsPlans: the sweep hands each space's plan to the
// schedule, so Dream's cadence follows it once plan limits are enforced.
func TestDreamSweepReadsPlans(t *testing.T) {
	t.Parallel()
	f := planFixture(t, true)
	free, pro := f.user("free"), f.user("pro")
	f.onPlan(pro, "personal_pro")
	a, b := f.space(free, policy.SpaceProject, "free-space"), f.space(pro, policy.SpaceProject, "pro-space")
	got := map[uuid.UUID]policy.Plan{}
	if _, err := f.l.DreamSweep(context.Background(), time.Now(), 100, func(s ledger.SweepSpace) ledger.SweepDecision {
		got[s.SpaceID] = s.Plan
		return ledger.SweepDecision{Cadence: "weekly", TimeZone: "UTC", DueAt: time.Now().Add(time.Hour)}
	}, nil); err != nil {
		t.Fatal(err)
	}
	if p := got[a]; !p.Enforced || p.Tier != policy.TierFree || p.DreamCadence != policy.CadenceWeekly {
		t.Errorf("the Free owner's space: %+v", p)
	}
	if p := got[b]; p.Tier != policy.TierPro || p.DreamCadence != policy.CadenceNightly || p.NightlySpaces != 5 {
		t.Errorf("the Pro owner's space: %+v", p)
	}
}
