package dreams

import (
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
)

func TestDreamRunBudget_NilSafe(t *testing.T) {
	t.Parallel()
	var b *dreamRunBudget
	if !b.shouldRoute("run-1") {
		t.Errorf("nil budget should route (no cap configured)")
	}
	b.consume(&agent.RunResult{ModelCalls: 5}) // must not panic
	mc, tc, sf, hf := b.summary()
	if mc != 0 || tc != 0 || sf || hf {
		t.Errorf("nil budget summary should be zero, got %d/%d/%v/%v", mc, tc, sf, hf)
	}
}

func TestDreamRunBudget_NoCapsWhileBelowSoft(t *testing.T) {
	t.Parallel()
	b := newDreamRunBudget()
	b.consume(&agent.RunResult{ModelCalls: 30})
	if !b.shouldRoute("run-1") {
		t.Errorf("below soft cap (30 < 60) should route")
	}
	if _, _, sf, hf := b.summary(); sf || hf {
		t.Errorf("below soft cap should not have fired any warning, got soft=%v hard=%v", sf, hf)
	}
}

func TestDreamRunBudget_SoftCapFiresOnceLetsRouteContinue(t *testing.T) {
	t.Parallel()
	b := newDreamRunBudget()
	// Bring count to soft cap exactly.
	b.consume(&agent.RunResult{ModelCalls: dreamRunBudgetSoft})

	if !b.shouldRoute("run-1") {
		t.Errorf("at soft cap routing should still proceed")
	}
	_, _, sf, _ := b.summary()
	if !sf {
		t.Errorf("soft cap should fire on first shouldRoute past threshold")
	}

	// Same accumulator after another should-route call: softFired stays true,
	// no second log (pinned by the latch behavior in the function).
	b.shouldRoute("run-1")
	_, _, sf2, hf2 := b.summary()
	if !sf2 || hf2 {
		t.Errorf("soft cap should latch (still true), hard cap should NOT fire yet, got soft=%v hard=%v", sf2, hf2)
	}
}

func TestDreamRunBudget_HardCapReroutesToSingleCall(t *testing.T) {
	t.Parallel()
	b := newDreamRunBudget()
	b.consume(&agent.RunResult{ModelCalls: dreamRunBudgetHard})

	if b.shouldRoute("run-1") {
		t.Errorf("at hard cap routing should be denied — caller falls back to single-call")
	}
	_, _, _, hf := b.summary()
	if !hf {
		t.Errorf("hard cap should fire on first shouldRoute past threshold")
	}
}

// Once over hard cap, every subsequent shouldRoute returns false
// and the single warning latches — no log spam per pair.
func TestDreamRunBudget_HardCapLatchesNoLogSpam(t *testing.T) {
	t.Parallel()
	b := newDreamRunBudget()
	b.consume(&agent.RunResult{ModelCalls: dreamRunBudgetHard + 50})

	for i := 0; i < 10; i++ {
		if b.shouldRoute("run-1") {
			t.Fatalf("over hard cap should NEVER route, iteration %d", i)
		}
	}
	_, _, _, hf := b.summary()
	if !hf {
		t.Errorf("hard fired flag should be set")
	}
}

func TestDreamRunBudget_TrackToolCalls(t *testing.T) {
	t.Parallel()
	b := newDreamRunBudget()
	b.consume(&agent.RunResult{
		ModelCalls: 3,
		ToolCalls: []agent.ToolCallRecord{
			{ID: "t1"}, {ID: "t2"}, {ID: "t3"},
		},
	})
	mc, tc, _, _ := b.summary()
	if mc != 3 {
		t.Errorf("ModelCalls = %d, want 3", mc)
	}
	if tc != 3 {
		t.Errorf("ToolCalls = %d, want 3", tc)
	}
}
