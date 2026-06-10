package agent

import (
	"time"

	"github.com/MemaxLabs/memax-go-agent-sdk/budget"
)

// Profile selects the budget envelope (and, downstream, the model
// id) for an agent run. Both dream and chat callers pass a profile
// at Run-time so the runtime can apply uniform per-class limits.
//
// The four documented profiles map 1:1 to the table in
// docs/plans/24-agent-runtime-lucid-and-chat.md "Provider, model
// selection, budgets". Adding a profile is a deliberate act —
// budget limits are economic-margin primitives, not knobs to
// twiddle per-callsite. New profiles must land with a corresponding
// row in that table and a code review explaining the cost shape.
type Profile string

const (
	// ProfileDreamPassive is the budget for the cheap path of the
	// dream agent: contradict/organize phases that did NOT fire a
	// trigger. Plan-23's `lucid_passive` mode. Tightly bounded —
	// at most 4 turns, 4 model calls, 2 tool calls, 60 seconds.
	// At ≥80% of dream runs landing here per plan 23's margin model,
	// this envelope is the load-bearing cost-control primitive.
	ProfileDreamPassive Profile = "dream_passive"

	// ProfileDreamActive is the budget for an agentic dream run:
	// a trigger fired (contradiction/URL-drift/topic-overload/
	// user-followup) and the agent is allowed to iterate with tools.
	// MaxModelCalls=8 matches plan 23's `tool_loop_iterations ≤ 8`
	// hard cap exactly; MaxToolCalls=16 allows up to 2 parallel
	// tool calls per round-trip on average. 300s ceiling protects
	// against runaway recall fan-out.
	ProfileDreamActive Profile = "dream_active"

	// ProfileChat is the per-turn budget for the user-facing chat
	// agent. More generous than dream because:
	//   - Chat is interactive (the user is waiting); ≤120s feels
	//     like a long-but-not-broken response, not a stuck job.
	//   - Chat fans out across the user's hubs by default, which
	//     legitimately needs more tool calls per turn.
	// MaxToolCalls=16 + MaxToolConcurrency from the runner gives
	// the SDK headroom to dispatch parallel reads on a single turn
	// without inflating the model budget.
	ProfileChat Profile = "chat"

	// profileTest is an internal-only ultra-tight envelope for the
	// runner's own unit tests. NOT exported — callers must pick
	// a real profile. Centralizing the test budget here means the
	// test suite stays insulated from changes to the production
	// profiles (a tightening of the chat budget shouldn't make
	// runner_test.go flaky).
	profileTest Profile = "test"
)

// Valid reports whether p is a known production profile. The test
// profile intentionally does NOT count — Run rejects it with a
// programmer-error message if it leaks out of test code into a real
// invocation.
func (p Profile) Valid() bool {
	switch p {
	case ProfileDreamPassive, ProfileDreamActive, ProfileChat:
		return true
	}
	return false
}

// budgetEnvelope is the per-profile resource envelope. Two separate
// SDK knobs read from these numbers and each serves a different
// purpose:
//
//   - **SDK lifecycle Governor** (Options.Budget = budget.Policy) —
//     checked at turn/model-call/tool-call boundaries; produces a
//     "budget exceeded: max ..." stop reason that surfaces to the
//     chat user as "this turn hit its budget."
//   - **SDK hard caps** (Options.MaxTurns, Options.MaxRunDuration,
//     Options.MaxToolConcurrency) — absolute ceilings the loop
//     itself enforces; the wall-clock cap wraps the run context, so
//     a hit there cancels with `context deadline exceeded`. The
//     runner treats these as the safety net, NOT the canonical
//     exhaust mechanism.
//
// To make the budget governor the canonical winner, Policy values
// are set BELOW the corresponding hard caps:
//
//   - `Policy.MaxTurns = maxTurns - 1` so the budget check sees
//     turn=maxTurns first and finalizes with the budget reason
//     before the loop's `for turn := 1; turn <= MaxTurns` bound
//     fires. **The effective number of completed work turns is
//     therefore `maxTurns - 1`** — the last turn-slot exists for
//     budget-check headroom, not productive work. The profile
//     numbers in the table are the hard cap, not the work cap.
//   - `Policy.MaxDuration = maxDuration - durationGrace` so the
//     governor's wall-clock check fires marginally before the
//     run-context deadline cancels the stream. Without this grace,
//     the timeout manifests as `context deadline exceeded` and
//     classifies as RunStatusError; with the grace, it classifies
//     as RunStatusBudgetExceeded — the difference matters because
//     the chat surface renders the two stop reasons differently.
type budgetEnvelope struct {
	maxTurns           int
	maxModelCalls      int
	maxToolCalls       int
	maxDuration        time.Duration
	maxToolConcurrency int
}

// durationGrace is the gap between the budget governor's
// MaxDuration and the run-context's hard timeout. The governor
// must fire MARGINALLY first so duration exhaust classifies as a
// budget stop (visible to surfaces) rather than a context-deadline
// error (opaque). With pure 0, context.DeadlineExceeded races the
// budget check and frequently wins because the SDK's
// budget.Policy.Check returns ctx.Err() before evaluating
// MaxDuration.
//
// 1 second is large enough to cover the SDK's per-event scheduling
// jitter without meaningfully extending the user-visible budget.
const durationGrace = 1 * time.Second

// envelopeFor maps a Profile to its budget shape. The numbers come
// from docs/plans/24-agent-runtime-lucid-and-chat.md "Provider,
// model selection, budgets" — keep this table in sync if the doc
// changes. CI doesn't enforce that link; reviewer responsibility.
func envelopeFor(p Profile) budgetEnvelope {
	switch p {
	case ProfileDreamPassive:
		return budgetEnvelope{
			maxTurns:           4,
			maxModelCalls:      4,
			maxToolCalls:       2,
			maxDuration:        60 * time.Second,
			maxToolConcurrency: 1,
		}
	case ProfileDreamActive:
		return budgetEnvelope{
			maxTurns:           12,
			maxModelCalls:      8,
			maxToolCalls:       16,
			maxDuration:        300 * time.Second,
			maxToolConcurrency: 4,
		}
	case ProfileChat:
		return budgetEnvelope{
			maxTurns:           20,
			maxModelCalls:      12,
			maxToolCalls:       16,
			maxDuration:        120 * time.Second,
			maxToolConcurrency: 4,
		}
	case profileTest:
		// Just enough to let a test run a turn or two. Bounded so a
		// runaway test stops fast.
		return budgetEnvelope{
			maxTurns:           4,
			maxModelCalls:      4,
			maxToolCalls:       4,
			maxDuration:        5 * time.Second,
			maxToolConcurrency: 1,
		}
	}
	// Unreachable for valid profiles. Returning a zero envelope
	// would mis-bound the run; panic so we catch it at boot.
	panic("agent: envelopeFor called with unknown profile " + string(p))
}

// policyFor builds the SDK Governor for p. Used as Options.Budget.
// Policy.MaxTurns sits 1 below the envelope's MaxTurns AND
// Policy.MaxDuration sits durationGrace below the envelope's
// MaxDuration so a turn- or duration-budget exhaust finalizes via
// the policy's "budget exceeded" reason rather than the SDK's
// hard-loop-bound "max turns" or context-deadline error — this
// keeps the surface's stop-reason taxonomy aligned (budget is
// "this turn is done," hard-cap is "I gave up").
func policyFor(p Profile) budget.Policy {
	env := envelopeFor(p)
	turns := env.maxTurns - 1
	if turns < 1 {
		turns = 1
	}
	duration := env.maxDuration - durationGrace
	if duration <= 0 {
		// Profiles with a duration <= durationGrace are programmer
		// errors (they'd produce a non-positive policy duration,
		// disabling the budget check entirely). Fall back to half
		// the envelope's duration so we still enforce SOMETHING.
		duration = env.maxDuration / 2
	}
	return budget.Policy{
		MaxTurns:      turns,
		MaxModelCalls: env.maxModelCalls,
		MaxToolCalls:  env.maxToolCalls,
		MaxDuration:   duration,
	}
}
