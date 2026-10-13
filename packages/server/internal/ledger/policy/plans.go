package policy

import (
	"fmt"
	"time"
)

// Plans (D9, plan 25 §12 3.4): what each V2 plan allows, as the plans
// registry's rows say (migration 069; internal/v2plans makes a Plan of a
// row). Decide refuses on a plan's limits only when the plan is Enforced,
// which V2_PLAN_LIMITS=enforce turns on: during the alpha none is, and the
// zero Plan enforces nothing, so a caller that knows no plan decides as
// before.
//
// Which plan decides what:
//
//   - The person's plan (Actor.Plan, their effective plan: the best of
//     their own and their teams'): the project spaces they own, the kinds
//     of agent they connect, their asks a month.
//   - The space's plan (Actor.SpacePlan, the better of its team's
//     subscription and its owner's plan): who holds a seat there, how
//     often Dream runs it, how far back its Activity goes.
//
// A seat is the right to keep (D9: members are billed, viewers are free).
// On a plan that bills seats (Team, Enterprise) every member holds one; on
// one that doesn't (Free, Pro) only the owner keeps, and a member acts as
// a viewer does, proposing. Reads, compiles, export and Forget never stop
// on a plan.

// PlanTier is the V2 plan a plan row is.
type PlanTier string

// The V2 plans.
const (
	TierFree       PlanTier = "free"
	TierPro        PlanTier = "pro"
	TierTeam       PlanTier = "team"
	TierEnterprise PlanTier = "enterprise"
)

// PlanTiers lists the plans, from Free up.
var PlanTiers = []PlanTier{TierFree, TierPro, TierTeam, TierEnterprise}

// Valid reports whether t is a known plan.
func (t PlanTier) Valid() bool {
	for _, x := range PlanTiers {
		if x == t {
			return true
		}
	}
	return false
}

// Unlimited is a limit with no limit.
const Unlimited = -1

// The Dream cadences a plan runs its spaces at.
const (
	CadenceWeekly  = "weekly"
	CadenceNightly = "nightly"
)

// Plan is a V2 plan as policy decides under it.
type Plan struct {
	// ID is the plan row ("personal_free"), Tier the V2 plan it is, and
	// Name its display name ("Free"), for messages.
	ID   string
	Tier PlanTier
	Name string
	// Enforced says Decide refuses on the limits below
	// (V2_PLAN_LIMITS=enforce). Unset, they are reported (Settings › Plan)
	// and nothing is refused on them.
	Enforced bool
	// ProjectSpaces, AgentKinds and Asks are how many project spaces a
	// person owns, how many kinds of agent they connect, and how many
	// questions a month they ask. Unlimited is no limit.
	ProjectSpaces int
	AgentKinds    int
	Asks          int
	// DreamCadence is how often Dream runs the plan's spaces, and on a
	// nightly plan NightlySpaces is how many of an owner's busiest spaces
	// run nightly (Unlimited: all of them); the rest run weekly.
	DreamCadence  string
	NightlySpaces int
	// ActivityDays is how far back Activity's reads and compiles go
	// (Unlimited: as far as they are kept).
	ActivityDays int
	// SeatBilled says members are billed seats (Team, Enterprise); viewers
	// are free either way.
	SeatBilled bool
}

// PlanLimit is the limit of a plan a decision met (D9), for the plan-limit
// dialog: the plan, how much it allows and how much is used, and the plan
// that lifts the limit. Seats have no counts.
type PlanLimit struct {
	Plan    PlanTier `json:"plan"`
	Allowed int      `json:"allowed,omitempty"`
	Used    int      `json:"used,omitempty"`
	Upgrade PlanTier `json:"upgrade,omitempty"`
}

// The plan decision codes; refusals, and seat_needed also a downgrade to
// a proposal.
const (
	CodeAgentKindLimit = "agent_kind_limit" // the plan's kinds of agent are connected
	CodeSeatNeeded     = "seat_needed"      // only the owner keeps on a plan that doesn't bill seats
	CodeDreamCadence   = "dream_cadence"    // Dream has run now as often as the plan's cadence allows
)

// name is the plan's name for messages.
func (p Plan) name() string {
	if p.Name != "" {
		return p.Name
	}
	return "your plan"
}

// within reports whether used more stays within limit.
func within(used, limit int) bool { return limit < 0 || used < limit }

// upgrade is the plan that lifts one of p's per-person limits: Pro after
// Free, none above.
func (p Plan) upgrade() PlanTier {
	if p.Tier == TierFree || p.Tier == "" {
		return TierPro
	}
	return ""
}

// limit is the PlanLimit a refusal on p met.
func (p Plan) limit(allowed, used int, upgrade PlanTier) *PlanLimit {
	tier := p.Tier
	if tier == "" {
		tier = TierFree
	}
	return &PlanLimit{Plan: tier, Allowed: allowed, Used: used, Upgrade: upgrade}
}

// Seatless reports whether a person with role r holds no seat in a space
// on p: a member, where p's limits are enforced and p doesn't bill seats.
// Owners always hold one, and viewers never need one.
func (p Plan) Seatless(r Role) bool {
	return p.Enforced && !p.SeatBilled && r == RoleMember
}

// SeatRole is the role r keeps with in a space on p: a member without a
// seat keeps as a viewer does (never). For the levels an agent is
// connected at (DefaultAutonomy, AgentCeiling).
func SeatRole(r Role, p Plan) Role {
	if p.Seatless(r) {
		return RoleViewer
	}
	return r
}

// AskLimit is how many questions a month the actor asks: their plan's,
// when plan limits are enforced (0: no limit), else the server's own
// (Object.AskLimit, ASK_MONTHLY_LIMIT; 0 during the alpha).
func AskLimit(a Actor, o Object) int {
	if a.Plan.Enforced {
		return max(a.Plan.Asks, 0)
	}
	return o.AskLimit
}

// DreamCadence is how often Dream runs a space on p that is busyRank among
// its owner's spaces (1 is the busiest): nightly on a nightly plan for its
// NightlySpaces busiest, weekly otherwise.
func DreamCadence(p Plan, busyRank int) string {
	if p.DreamCadence != CadenceNightly {
		return CadenceWeekly
	}
	if p.NightlySpaces < 0 || (busyRank >= 1 && busyRank <= p.NightlySpaces) {
		return CadenceNightly
	}
	return CadenceWeekly
}

// RunNowWindow is the window Dream's run-now cap counts in for a space on
// p: a week on a weekly plan, a day on a nightly one.
func RunNowWindow(p Plan) time.Duration {
	if p.DreamCadence == CadenceNightly {
		return 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// DecideRunNow decides a run of Dream now, beside a space's own schedule,
// on a plan whose limits are enforced: at most `limit` in RunNowWindow,
// `runs` asked for in it already. Without plan limits it applies, and the
// caller's cap (DREAM_MANUAL_PER_DAY and _PER_WEEK) stands as a rate limit.
func DecideRunNow(p Plan, runs, limit int) Decision {
	if !p.Enforced || within(runs, limit) {
		return apply()
	}
	period, own, up := "week", "each week, and Pro runs it nightly", TierPro
	if p.DreamCadence == CadenceNightly {
		period, own, up = "day", "each night", ""
	}
	d := refuse(CodeDreamCadence, fmt.Sprintf("Dream has run now %s this %s, as often as %s allows. It still runs on its own %s.",
		times(runs), period, p.name(), own))
	d.Limit = p.limit(limit, runs, up)
	return d
}

// ActivitySince is the oldest Activity a space on p shows at now: reads
// and compiles from before it are kept (fading reads them) but not listed.
// The zero time is no window: limits not enforced, or kept as long as they
// are.
func ActivitySince(p Plan, now time.Time) time.Time {
	if !p.Enforced || p.ActivityDays < 0 {
		return time.Time{}
	}
	return now.AddDate(0, 0, -p.ActivityDays)
}

// ActivityDays is how far back a space on p lists its Activity, in days
// (0: no window).
func ActivityDays(p Plan) int {
	if !p.Enforced || p.ActivityDays < 0 {
		return 0
	}
	return p.ActivityDays
}

// spaceLimit refuses a new project space past the person's plan.
func spaceLimit(p Plan, owned int) *Decision {
	if !p.Enforced || within(owned, p.ProjectSpaces) {
		return nil
	}
	d := refuse(CodeSpaceLimit, fmt.Sprintf(
		"You own %s, all %s has. Pro has as many as you like; until then, use the one you have with --space.",
		count(owned, "project space", "project spaces"), p.name()))
	d.Limit = p.limit(p.ProjectSpaces, owned, p.upgrade())
	return &d
}

// agentKindLimit refuses connecting a new kind of agent past the person's
// plan; kinds counts the kinds connected already (the same agent on
// another machine, or with another key, isn't a new kind).
func agentKindLimit(p Plan, name string, kinds int) *Decision {
	if !p.Enforced || within(kinds, p.AgentKinds) {
		return nil
	}
	d := refuse(CodeAgentKindLimit, fmt.Sprintf(
		"%s connects %s, and you have connected that many, so %s can't connect. Disconnect one in Agents, or move to Pro for as many as you like.",
		p.name(), count(p.AgentKinds, "kind of agent", "kinds of agent"), name))
	d.Limit = p.limit(p.AgentKinds, kinds, p.upgrade())
	return &d
}

// askLimit refuses an ask past the person's monthly limit.
func askLimit(a Actor, o Object) *Decision {
	limit := AskLimit(a, o)
	if limit <= 0 || o.AsksBefore < limit {
		return nil
	}
	if !a.Plan.Enforced {
		d := refuse(CodeAskLimit, fmt.Sprintf(
			"You've asked %d questions this month, all your plan answers. Asks start again on the 1st.", limit))
		return &d
	}
	more := " Asks start again on the 1st."
	if up := a.Plan.upgrade(); up == TierPro {
		more = " Asks start again on the 1st, and Pro answers 1,000 a month."
	}
	d := refuse(CodeAskLimit, fmt.Sprintf("You've asked %d questions this month, all %s answers.%s", limit, a.Plan.name(), more))
	d.Limit = a.Plan.limit(limit, o.AsksBefore, a.Plan.upgrade())
	return &d
}

// seatNeeded words what a member without a seat may not do, or does as a
// proposal: d is a viewer's decision, with a viewer's code.
func seatNeeded(d Decision, p Plan, s Space) Decision {
	d.Code = CodeSeatNeeded
	if d.Effect == EffectRefuse {
		d.Message = fmt.Sprintf(
			"Only the owner of %s keeps on %s: members need a Team seat to keep, answer or settle anything there. Ask the owner, or move the space to Team.",
			spaceName(s), p.name())
	} else {
		d.Message = fmt.Sprintf("Sent to Review: only the owner keeps in %s until members have Team seats.", spaceName(s))
	}
	d.Limit = p.limit(0, 0, TierTeam)
	return d
}

// count is "1 project space", "3 project spaces".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// times is "once", "twice", "3 times".
func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	}
	return fmt.Sprintf("%d times", n)
}
