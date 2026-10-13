package policy

import (
	"strings"
	"testing"
	"time"
)

// The plans as migration 067's rows make them (internal/v2plans), enforced.
var (
	freePlan = Plan{ID: "personal_free", Tier: TierFree, Name: "Free", Enforced: true,
		ProjectSpaces: 1, AgentKinds: 3, Asks: FreeAskLimit, DreamCadence: CadenceWeekly, ActivityDays: 30}
	proPlan = Plan{ID: "personal_pro", Tier: TierPro, Name: "Pro", Enforced: true,
		ProjectSpaces: Unlimited, AgentKinds: Unlimited, Asks: 1000, DreamCadence: CadenceNightly, NightlySpaces: 5, ActivityDays: 90}
	teamPlan = Plan{ID: "hub_team", Tier: TierTeam, Name: "Team", Enforced: true,
		ProjectSpaces: Unlimited, AgentKinds: Unlimited, Asks: 1000, DreamCadence: CadenceNightly, NightlySpaces: Unlimited,
		ActivityDays: 365, SeatBilled: true}
)

// alpha is a plan as it is during the alpha: the same limits, reported and
// not enforced.
func alpha(p Plan) Plan { p.Enforced = false; return p }

// TestPlanLimitsBothWays: every refusal on a plan's limit happens with the
// limits enforced and never without them (V2_PLAN_LIMITS), so turning the
// switch on is the only way a plan refuses anything new.
func TestPlanLimitsBothWays(t *testing.T) {
	t.Parallel()
	session := func(role Role, p Plan) Actor {
		return with(person(role, ViaWeb), func(a *Actor) { a.Credential = CredentialSession; a.Plan = p })
	}
	cases := []struct {
		name   string
		decide func(p Plan) Decision
		plan   Plan
		code   string
		limit  *PlanLimit
		effect Effect // when not a refusal (seat_needed sends a write to Review)
	}{
		{"a second project space on Free", func(p Plan) Decision {
			return DecideCreateSpace(session(RoleOwner, p), SpaceProject, 1)
		}, freePlan, CodeSpaceLimit, &PlanLimit{Plan: TierFree, Allowed: 1, Used: 1, Upgrade: TierPro}, EffectRefuse},
		{"a fourth kind of agent on Free", func(p Plan) Decision {
			return DecideConnection(session(RoleOwner, p), ConnectionConnect,
				Connection{Name: "Cursor", Mine: true, Credential: CredentialOAuth, To: AutonomyPropose, NewKind: true, Kinds: 3}, project)
		}, freePlan, CodeAgentKindLimit, &PlanLimit{Plan: TierFree, Allowed: 3, Used: 3, Upgrade: TierPro}, EffectRefuse},
		{"the 51st ask on Free", func(p Plan) Decision {
			return Decide(session(RoleOwner, p), ActionAsk, Object{AsksBefore: 50}, project)
		}, freePlan, CodeAskLimit, &PlanLimit{Plan: TierFree, Allowed: 50, Used: 50, Upgrade: TierPro}, EffectRefuse},
		{"the 1,001st ask on Pro", func(p Plan) Decision {
			return Decide(session(RoleOwner, p), ActionAsk, Object{AsksBefore: 1000}, project)
		}, proPlan, CodeAskLimit, &PlanLimit{Plan: TierPro, Allowed: 1000, Used: 1000}, EffectRefuse},
		{"a member keeps in a Pro owner's space", func(p Plan) Decision {
			return Decide(with(session(RoleMember, proPlan), func(a *Actor) { a.SpacePlan = p }), ActionKeep, proposal, project)
		}, proPlan, CodeSeatNeeded, &PlanLimit{Plan: TierPro, Upgrade: TierTeam}, EffectRefuse},
		{"a member keeps a decision from the CLI: the seat first, then the web", func(p Plan) Decision {
			return Decide(with(person(RoleMember, ViaCLI), func(a *Actor) { a.SpacePlan = p }), ActionKeep, propDec, team)
		}, proPlan, CodeSeatNeeded, &PlanLimit{Plan: TierPro, Upgrade: TierTeam}, EffectRefuse},
		{"a member remembers in a Free owner's space", func(p Plan) Decision {
			return Decide(with(session(RoleMember, freePlan), func(a *Actor) { a.SpacePlan = p }), ActionRemember, newFact, project)
		}, freePlan, CodeSeatNeeded, &PlanLimit{Plan: TierFree, Upgrade: TierTeam}, EffectPropose},
		{"a member's agent at Write in a Pro owner's space", func(p Plan) Decision {
			return Decide(with(agent(RoleMember, AutonomyWrite), func(a *Actor) { a.AgentStatus = AgentConnected; a.SpacePlan = p }),
				ActionRemember, newFact, project)
		}, proPlan, CodeSeatNeeded, &PlanLimit{Plan: TierPro, Upgrade: TierTeam}, EffectPropose},
		{"a member answers a gate in a Pro owner's space", func(p Plan) Decision {
			return Decide(with(session(RoleMember, proPlan), func(a *Actor) { a.SpacePlan = p }), ActionAnswerGate, gate, project)
		}, proPlan, CodeSeatNeeded, &PlanLimit{Plan: TierPro, Upgrade: TierTeam}, EffectRefuse},
		{"a member lets their agent write in a Pro owner's space", func(p Plan) Decision {
			return DecideConnection(with(session(RoleMember, proPlan), func(a *Actor) { a.SpacePlan = p }), ConnectionSetAutonomy,
				Connection{Name: "Codex", Mine: true, Credential: CredentialOAuth, From: AutonomyPropose, To: AutonomyWrite}, project)
		}, proPlan, CodeSeatNeeded, &PlanLimit{Plan: TierPro, Upgrade: TierTeam}, EffectRefuse},
		{"run now twice in a week on Free", func(p Plan) Decision {
			return DecideRunNow(p, 1, 1)
		}, freePlan, CodeDreamCadence, &PlanLimit{Plan: TierFree, Allowed: 1, Used: 1, Upgrade: TierPro}, EffectRefuse},
		{"run now past the day's cap on Pro", func(p Plan) Decision {
			return DecideRunNow(p, 3, 3)
		}, proPlan, CodeDreamCadence, &PlanLimit{Plan: TierPro, Allowed: 3, Used: 3}, EffectRefuse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := c.decide(c.plan)
			if d.Effect != c.effect || d.Code != c.code || d.Message == "" {
				t.Fatalf("enforced: got %+v, want %s %s", d, c.effect, c.code)
			}
			if d.Limit == nil || *d.Limit != *c.limit {
				t.Fatalf("enforced: limit %+v, want %+v", d.Limit, c.limit)
			}
			for _, banned := range []string{"!", "AI", "magic", "smart", "delete"} {
				if strings.Contains(d.Message, banned) {
					t.Errorf("message %q says %q", d.Message, banned)
				}
			}
			// The same decision during the alpha refuses nothing on the plan.
			if off := c.decide(alpha(c.plan)); off.Code == c.code || off.Limit != nil {
				t.Fatalf("not enforced: got %+v, want no %s", off, c.code)
			}
		})
	}
}

// TestPlansAllowWhatTheyHave: within a plan's limits, and on the plans
// without them, nothing is refused.
func TestPlansAllowWhatTheyHave(t *testing.T) {
	t.Parallel()
	session := func(role Role, p, sp Plan) Actor {
		return with(person(role, ViaWeb), func(a *Actor) { a.Credential = CredentialSession; a.Plan, a.SpacePlan = p, sp })
	}
	for _, c := range []struct {
		name string
		d    Decision
		want Effect
	}{
		{"Free's first project space", DecideCreateSpace(session(RoleOwner, freePlan, freePlan), SpaceProject, 0), EffectApply},
		{"Pro's tenth", DecideCreateSpace(session(RoleOwner, proPlan, proPlan), SpaceProject, 9), EffectApply},
		{"Free's third kind of agent", DecideConnection(session(RoleOwner, freePlan, freePlan), ConnectionConnect,
			Connection{Name: "Codex", Mine: true, Credential: CredentialOAuth, To: AutonomyPropose, NewKind: true, Kinds: 2}, project), EffectApply},
		{"another Claude Code on Free, with three kinds", DecideConnection(session(RoleOwner, freePlan, freePlan), ConnectionConnect,
			Connection{Name: "Claude Code", Mine: true, Credential: CredentialOAuth, To: AutonomyPropose, Kinds: 3}, project), EffectApply},
		{"Free's 50th ask", Decide(session(RoleOwner, freePlan, freePlan), ActionAsk, Object{AsksBefore: 49}, project), EffectApply},
		{"the owner keeps on Pro", Decide(session(RoleOwner, proPlan, proPlan), ActionKeep, proposal, project), EffectApply},
		{"a member keeps on Team", Decide(session(RoleMember, proPlan, teamPlan), ActionKeep, proposal, project), EffectApply},
		{"a member without a seat forgets where members forget", Decide(session(RoleMember, proPlan, proPlan), ActionForget, kept, membersFgt), EffectApply},
		{"and asks", Decide(session(RoleMember, proPlan, proPlan), ActionAsk, Object{}, project), EffectApply},
		{"and exports", Decide(session(RoleMember, proPlan, proPlan), ActionExport, Object{}, project), EffectApply},
		{"and proposes", Decide(session(RoleMember, proPlan, proPlan), ActionPropose, newFact, project), EffectPropose},
		{"and connects an agent at Propose", DecideConnection(session(RoleMember, proPlan, proPlan), ConnectionConnect,
			Connection{Name: "Codex", Mine: true, Credential: CredentialOAuth, To: AutonomyPropose}, project), EffectApply},
		{"Memax connects V1's credentials whatever the plan", DecideConnection(Actor{Kind: ActorMemax, Role: RoleOwner, Plan: freePlan},
			ConnectionConnect, Connection{Name: "Cursor", To: AutonomyPropose, NewKind: true, Kinds: 9}, project), EffectApply},
		{"run now once a week on Free", DecideRunNow(freePlan, 0, 1), EffectApply},
	} {
		if c.d.Effect != c.want || c.d.Limit != nil {
			t.Errorf("%s: got %+v, want %s", c.name, c.d, c.want)
		}
	}
	// A viewer stays a viewer, said as one.
	if d := Decide(session(RoleViewer, proPlan, proPlan), ActionKeep, proposal, project); d.Code != CodeViewer {
		t.Errorf("a viewer keeps on Pro: got %+v, want %s", d, CodeViewer)
	}
	// The alpha's limits stand as they were: the ask limit the server sets,
	// and the fair-use cap on project spaces.
	if d := Decide(session(RoleOwner, alpha(freePlan), alpha(freePlan)), ActionAsk, Object{AsksBefore: 10, AskLimit: 10}, project); d.Code != CodeAskLimit || d.Limit != nil {
		t.Errorf("ASK_MONTHLY_LIMIT during the alpha: got %+v", d)
	}
	if d := DecideCreateSpace(session(RoleOwner, proPlan, proPlan), SpaceProject, MaxOwnedProjectSpaces); d.Code != CodeSpaceLimit || d.Limit != nil {
		t.Errorf("the fair-use cap on Pro: got %+v", d)
	}
}

func TestDreamCadence(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		plan Plan
		rank int
		want string
	}{
		{freePlan, 1, CadenceWeekly},
		{proPlan, 1, CadenceNightly},
		{proPlan, 5, CadenceNightly},
		{proPlan, 6, CadenceWeekly},
		{proPlan, 0, CadenceWeekly}, // unranked: no changes this week
		{teamPlan, 40, CadenceNightly},
		{Plan{}, 1, CadenceWeekly},
	} {
		if got := DreamCadence(c.plan, c.rank); got != c.want {
			t.Errorf("%s rank %d: %s, want %s", c.plan.ID, c.rank, got, c.want)
		}
	}
	if RunNowWindow(freePlan) != 7*24*time.Hour || RunNowWindow(proPlan) != 24*time.Hour {
		t.Error("run-now windows: a week on a weekly plan, a day on a nightly one")
	}
}

func TestActivityWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if got := ActivitySince(freePlan, now); !got.Equal(now.AddDate(0, 0, -30)) || ActivityDays(freePlan) != 30 {
		t.Errorf("Free: %v, %d days", got, ActivityDays(freePlan))
	}
	if got := ActivitySince(alpha(freePlan), now); !got.IsZero() || ActivityDays(alpha(freePlan)) != 0 {
		t.Errorf("the alpha keeps no window: %v", got)
	}
	enterprise := Plan{Enforced: true, ActivityDays: Unlimited}
	if !ActivitySince(enterprise, now).IsZero() || ActivityDays(enterprise) != 0 {
		t.Error("no window on a plan without one")
	}
}

func TestSeatRole(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		role Role
		plan Plan
		want Role
	}{
		{RoleMember, proPlan, RoleViewer},
		{RoleMember, alpha(proPlan), RoleMember},
		{RoleMember, teamPlan, RoleMember},
		{RoleOwner, freePlan, RoleOwner},
		{RoleViewer, teamPlan, RoleViewer},
	} {
		if got := SeatRole(c.role, c.plan); got != c.want {
			t.Errorf("%s on %s: %s, want %s", c.role, c.plan.ID, got, c.want)
		}
	}
	// A member without a seat connects an agent at most at Propose, even
	// where the space lets new agents write.
	writes := Space{Name: "acme", Kind: SpaceProject, Rules: Rules{NewAgentAutonomy: AutonomyWrite}, OnV2: true}
	if got := DefaultAutonomy(SeatRole(RoleMember, proPlan), writes); got != AutonomyPropose {
		t.Errorf("default autonomy without a seat: %s", got)
	}
}
