package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 11 for "edit, then keep": a person's new words that touch a
// decision in force are saved as the proposal's new version, judged like
// any proposal's, and not kept until the judge has looked (Keep waits for
// it). The saved edit is undoable as an edit; words that touch nothing are
// kept at once, as before.
func TestEditThenKeepHoldsForTheJudge(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	f.apply(&ledger.Remember{Meta: me(), NewMemory: decisionIn(sp, "Deploy the v2 API to Railway.", "deploy target")})
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: decisionIn(sp, "Deploy the v2 API to Fly.io.", "deploy target")}).Memory

	held := f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "Deploy the v2 API to Fly.io in iad.", Keep: true})
	if held.Outcome != ledger.OutcomeProposed || held.Policy.Code != policy.CodeJudgePending || held.Policy.Effect != policy.EffectPropose {
		t.Fatalf("held = %s %s %s", held.Outcome, held.Policy.Effect, held.Policy.Code)
	}
	if m := held.Memory; m.Lifecycle != lifecycle.Proposed || m.Version != 2 || m.Statement != "Deploy the v2 API to Fly.io in iad." {
		t.Fatalf("held memory = %s v%d %q", m.Lifecycle, m.Version, m.Statement)
	}
	if len(held.Receipts) != 1 || held.Receipts[0].Action != ledger.ActionEdited {
		t.Errorf("held receipts = %v", actions(held.Receipts))
	}
	// The new words are queued for the judge, in the edit's transaction.
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal' AND args->>'memory_id' = $1 AND (args->>'version')::int = 2`,
		p.ID.String()); n != 1 {
		t.Errorf("%d judge jobs for version 2", n)
	}
	// Keep on that version waits for the judge.
	_, err := f.l.Apply(ctx, &ledger.Keep{Meta: me(), Memory: p.Ref, ExpectedVersion: 2})
	var pending *ledger.JudgePendingError
	if !errors.As(err, &pending) || pending.Ref != p.Ref {
		t.Fatalf("keep before the judge = %v", err)
	}
	// Judged, nothing found: the same Keep goes through.
	f.judgeAs(sp, held.Memory, ledger.OutcomeNone, nil, ledger.JudgeProposal)
	kept := f.apply(&ledger.Keep{Meta: me(), Memory: p.Ref, ExpectedVersion: 2})
	if kept.Memory.Lifecycle != lifecycle.Kept {
		t.Fatalf("keep after the judge = %s", kept.Memory.Lifecycle)
	}
	// Undo the Keep, then the saved edit: the agent's words, in Review.
	if _, err := undo(f, person(zz), f.scope(zz), kept.Receipts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := undo(f, person(zz), f.scope(zz), held.Receipts[0].ID); err != nil {
		t.Fatalf("undo the saved edit: %v", err)
	}
	if m := f.mem(zz, p.ID); m.Lifecycle != lifecycle.Proposed || m.Version != 1 || m.Statement != "Deploy the v2 API to Fly.io." {
		t.Errorf("after undo: %s v%d %q", m.Lifecycle, m.Version, m.Statement)
	}

	// Moving the same words to another section waits only while they do.
	again := f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "Deploy the v2 API to Fly.io.",
		Section: ledger.SectionOpenQuestion, Keep: true})
	if again.Policy.Code != policy.CodeJudgePending {
		t.Errorf("same words, unjudged version: %s", again.Policy.Code)
	}

	// Words that touch no decision in force are kept at once.
	plain := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: fact(sp, "Workers must be idempotent.")}).Memory
	res := f.apply(&ledger.Edit{Meta: me(), Memory: plain.Ref, ExpectedVersion: 1, Statement: "Workers must be idempotent and retry safely.", Keep: true})
	if res.Outcome != ledger.OutcomeApplied || res.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("plain edit then keep = %s %s", res.Outcome, res.Memory.Lifecycle)
	}
}

// "Undo the later change first", as the refusal says, then works; and the
// judge's `judged` receipt (a verdict that changed nothing) is no later
// change, so a proposal's edit stays undoable once it's judged.
func TestUndoOnceTheLaterChangeIsUndone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	propose := func(s string) *ledger.Memory {
		return f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP), NewMemory: fact(sp, s)}).Memory
	}
	refusal := func(err error) string {
		var ue *ledger.UndoError
		if errors.As(err, &ue) {
			return ue.Reason
		}
		return ""
	}

	p := propose("One.")
	k := f.apply(&ledger.Keep{Meta: me(), Memory: p.Ref})
	e := f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "One, edited."})
	if _, err := undo(f, person(zz), f.scope(zz), k.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
		t.Fatalf("keep under a later edit: %v", err)
	}
	if _, err := undo(f, person(zz), f.scope(zz), e.Receipts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := undo(f, person(zz), f.scope(zz), k.Receipts[0].ID); err != nil {
		t.Fatalf("keep, once the edit is undone: %v", err)
	}
	if m := f.mem(zz, p.ID); m.Lifecycle != lifecycle.Proposed || m.Statement != "One." {
		t.Errorf("after both: %s %q", m.Lifecycle, m.Statement)
	}

	// A proposal's edit, then the judge's verdict that changed nothing.
	q := propose("Two.")
	qe := f.apply(&ledger.Edit{Meta: me(), Memory: q.Ref, ExpectedVersion: 1, Statement: "Two, edited."})
	f.judgeAs(sp, qe.Memory, ledger.OutcomeNone, nil, ledger.JudgeProposal)
	if _, err := undo(f, person(zz), f.scope(zz), qe.Receipts[0].ID); err != nil {
		t.Fatalf("edit after a verdict that changed nothing: %v", err)
	}
	// A verdict that did change something still counts.
	r := propose("Three.")
	d := f.apply(&ledger.Remember{Meta: me(), NewMemory: decisionIn(sp, "Use npm.", "package manager")}).Memory
	re := f.apply(&ledger.Edit{Meta: me(), Memory: r.Ref, ExpectedVersion: 1, Statement: "Three, with npm."})
	f.judgeAs(sp, re.Memory, ledger.OutcomeFlagged, d, ledger.JudgeProposal)
	if _, err := undo(f, person(zz), f.scope(zz), re.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
		t.Errorf("edit under a flag: %v", err)
	}
}

// Keep, or edit then keep, on a proposal the judge flagged is refused as
// in conflict, naming the decision in force in the way.
func TestKeepInConflictNamesTheDecision(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	p, d := f.conflictPair(zz, sp)
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	for name, cmd := range map[string]ledger.Command{
		"keep":           &ledger.Keep{Meta: me(), Memory: p.Ref},
		"edit then keep": &ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: p.Version, Statement: "Deploy the v2 API to Fly.io.", Keep: true},
	} {
		_, err := f.l.Apply(ctx, cmd)
		var ic *ledger.InConflictError
		if !errors.As(err, &ic) || ic.Ref != p.Ref || ic.With != d.Ref {
			t.Errorf("%s = %v", name, err)
		}
		// Still the lifecycle's refusal, for callers that check for it.
		if !errors.Is(err, ledger.ErrInvalidTransition) {
			t.Errorf("%s isn't an invalid transition: %v", name, err)
		}
	}
	// Nothing changed: still a flagged proposal at its version.
	if m := f.mem(zz, p.ID); m.State != lifecycle.MarkConflict || m.Version != p.Version {
		t.Errorf("after: %s v%d", m.State, m.Version)
	}
}
