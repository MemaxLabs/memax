// Phase 2b dream-run shared governor (plan 24 §"Provider, model
// selection, budgets" → "Dream-run-level shared governor"). The
// per-Run budget envelope from agent.Run caps a SINGLE invocation,
// but a dream cycle on an opted-in hub can route many memories
// through agent.Run. Without a cross-Run cap, a hub with 100
// fired triggers could consume 100 × 8 = 800 model calls in a
// single cycle — entirely outside the per-Run envelope.
//
// dreamRunBudget is the per-cycle accumulator. It tracks total
// model calls + tool calls across every agent.Run in this cycle
// and gates further routing when the hard cap is reached. Soft
// cap surfaces as a warning log; hard cap silently routes
// remaining pairs back to the single-call path so the cycle still
// completes its other phases.
//
// The budget is intentionally NOT goroutine-safe — the dream
// engine's phases run sequentially within one cycle. If a future
// refactor parallelizes routed phases, wrap the counters in a
// mutex.
package dreams

import (
	"log/slog"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
)

// dreamRunBudgetSoft is plan 24's soft cap on cumulative model
// calls per cycle. Crossing it logs a warning ("dream run
// approaching budget") but does NOT change routing — the cycle
// keeps going. Calibration data captures the soft-fired event
// for retuning before the hard cap shifts.
const dreamRunBudgetSoft = 60

// dreamRunBudgetHard is the cycle's hard ceiling. Once reached,
// further pairs route to the single-call path instead of agent.
// We do NOT abort the dream cycle (plan 24's "partial_failed"
// language is reserved for true SDK budget errors); we degrade
// gracefully so the rest of the cycle still produces useful
// work for the user.
const dreamRunBudgetHard = 100

// dreamRunBudget is the cycle-level cumulative accumulator. One
// instance per dream cycle, reset on cycle start.
type dreamRunBudget struct {
	// modelCalls is the running total across all agent.Run
	// invocations this cycle. Compared against dreamRunBudgetHard
	// to gate further routing; soft cap is logged once.
	modelCalls int

	// toolCalls is the running total across all agent.Run
	// invocations this cycle. Tracked for visibility (logged in
	// the cycle summary) but doesn't gate routing — the per-Run
	// envelope's MaxToolCalls already bounds tool fan-out per
	// run, and the cross-Run sum is informational.
	toolCalls int

	// softFired latches at true once the soft cap is logged so
	// we don't spam the same warning every routed pair after the
	// threshold is crossed.
	softFired bool

	// hardFired latches at true once the hard cap kicks in.
	// Logged once, then `shouldRoute` returns false for the
	// rest of the cycle.
	hardFired bool
}

// newDreamRunBudget returns a fresh per-cycle accumulator. Tests
// can override the constants by constructing a custom shape, but
// production callers should use this helper so the soft/hard
// thresholds stay in lockstep with plan 24.
func newDreamRunBudget() *dreamRunBudget {
	return &dreamRunBudget{}
}

// shouldRoute reports whether the engine should route the next
// pair to agent.Run. Returns false once the hard cap is reached;
// the caller falls back to the single-call path. The soft cap
// does NOT change the answer here — it just logs a warning the
// first time it's crossed.
//
// runID is logged with the warning so an operator querying Loki
// for a budget alert can correlate to the specific cycle.
func (b *dreamRunBudget) shouldRoute(runID string) bool {
	if b == nil {
		return true
	}
	if b.modelCalls >= dreamRunBudgetHard {
		if !b.hardFired {
			b.hardFired = true
			slog.Warn("dream: run budget HARD cap reached — remaining pairs route to single-call",
				"run_id", runID,
				"model_calls", b.modelCalls,
				"hard_cap", dreamRunBudgetHard,
			)
		}
		return false
	}
	if !b.softFired && b.modelCalls >= dreamRunBudgetSoft {
		b.softFired = true
		slog.Warn("dream: run budget SOFT cap reached — routing continues",
			"run_id", runID,
			"model_calls", b.modelCalls,
			"soft_cap", dreamRunBudgetSoft,
			"hard_cap", dreamRunBudgetHard,
		)
	}
	return true
}

// consume accumulates one agent.Run's model + tool call counts
// onto the cycle total. Call AFTER agent.Run returns so even a
// budget-exhausted run is fully counted (its consumption is real
// even when its verdict is "no vote").
func (b *dreamRunBudget) consume(res *agent.RunResult) {
	if b == nil || res == nil {
		return
	}
	b.modelCalls += res.ModelCalls
	b.toolCalls += len(res.ToolCalls)
}

// summary returns the running totals for cycle-end logging. Kept
// as a method (not bare struct field reads) so a future change
// can add metric exports / tracing without touching call sites.
func (b *dreamRunBudget) summary() (modelCalls, toolCalls int, softFired, hardFired bool) {
	if b == nil {
		return 0, 0, false, false
	}
	return b.modelCalls, b.toolCalls, b.softFired, b.hardFired
}
