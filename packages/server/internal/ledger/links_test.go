package ledger_test

import (
	"context"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// An agent's edit of a decision is a proposal that supersedes it. The
// judge may find the same (it updates the decision), and a person may keep
// it over the decision when they settle a conflict between the two: each
// means the link there is, and neither fails on it (it did, with a unique
// violation, until TestLockOrderUnderLoad found it).
func TestLinkingWhatIsLinkedAlready(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	d := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		NewMemory: decisionIn(space, "Deploy the v2 API to Railway.", "deploy target")}).Memory
	upd := f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), Memory: d.Ref,
		ExpectedVersion: 1, Statement: "Deploy the v2 API to Fly.io."}).Memory
	if upd.ID == d.ID || !hasLink(f.mem(zz, upd.ID), ledger.LinkSupersedes, ledger.LinkOut, d.ID) {
		t.Fatalf("the agent's edit: %s", upd.Ref)
	}
	res := f.judgeAs(space, upd, ledger.OutcomeSuperseding, d, ledger.JudgeProposal)
	if res.Unchanged || res.Receipts[0].Action != ledger.ActionLinked {
		t.Fatalf("verdict: %+v", res.Receipts)
	}
	links := 0
	for _, l := range f.mem(zz, upd.ID).Links {
		if l.Kind == ledger.LinkSupersedes && l.MemoryID == d.ID {
			links++
		}
	}
	if links != 1 {
		t.Errorf("%d supersedes links, want 1", links)
	}

	// The same pair, flagged and settled for the update.
	upd2 := f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), Memory: d.Ref,
		ExpectedVersion: 1, Statement: "Deploy the v2 API to Render."}).Memory
	if r := f.judgeAs(space, upd2, ledger.OutcomeFlagged, d, ledger.JudgeProposal); r.Memory.State != lifecycle.MarkConflict {
		t.Fatalf("flag: %s", r.Memory.State)
	}
	if _, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: upd2.Ref,
		Choice: ledger.ChooseThis}); err != nil {
		t.Fatalf("settle for the update: %v", err)
	}
	if got := f.mem(zz, upd2.ID); got.Lifecycle != lifecycle.Kept {
		t.Errorf("the update after: %s", got.Lifecycle)
	}
}
