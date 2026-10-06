package ledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// A pulled hand edit holds its file until each proposal it wrote is kept
// or rejected (DriftResolve: "The file stays as it is until you keep or
// reject them."); then the latest compile is due over it. Undo of the
// decision that lifted a hold holds the file again, until something was
// delivered over the edit.
func TestPullHoldsTheFileUntilItsProposalsAreDecided(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	pnpm := f.remember(zz, space, "pnpm workspaces only.")
	f.brief(zz, space, 0, demoSections(pnpm.Ref))
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	run := f.compiled(tg, "compiled v1")
	dm := meta(person(zz), f.scope(zz), policy.ViaCLI)
	f.apply(&ledger.RecordDelivery{Meta: dm, Target: tg.ID, Compile: run.Ref, SHA256: run.DriftSHA256})

	pullEdit := func(content string) (ledger.Result, ledger.Observation) {
		t.Helper()
		decisions := "Decisions"
		obs := f.apply(&ledger.RecordObservation{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: tg.ID, Path: "AGENTS.md",
			ObservedSHA256: sha(content), ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop", ArtifactKey: "obs/" + sha(content),
			Bytes: len(content), Changes: ledger.ChangeSet{Changes: []ledger.DriftChange{
				{Kind: ledger.ChangeEdit, Ref: pnpm.Ref, Refs: []string{pnpm.Ref}, OldText: pnpm.Statement, NewText: "pnpm only; never npm.", OldLine: 4, NewLine: 4},
				{Kind: ledger.ChangeNew, Text: "Prefer named exports. " + content, Line: 6, Section: &decisions},
			}}})
		pull := f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftPull})
		return pull, obs.Observations[0]
	}
	shown := func() *ledger.Target {
		t.Helper()
		return f.get(zz, tg.ID)
	}
	deliver := func(c *ledger.CompileRun) error {
		_, err := f.l.Apply(ctx, &ledger.RecordDelivery{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: tg.ID, Compile: c.Ref, SHA256: c.DriftSHA256})
		return err
	}

	pull, obs := pullEdit("edit one")
	keepP, newP := pull.Proposals[0], pull.Proposals[1]
	g := shown()
	if g.ShownState() != ledger.SyncHeld || len(g.Holds) != 1 || g.Holds[0].Path != "AGENTS.md" || g.Holds[0].Observation != obs.ID ||
		!slices.Equal(g.Holds[0].Proposals, []string{keepP.Ref, newP.Ref}) {
		t.Fatalf("after the pull: %s %+v", g.ShownState(), g.Holds)
	}
	if f := g.Delivered.Files[0]; f.Held == nil || !*f.Held || *f.Observation != obs.ID {
		t.Errorf("the baseline file isn't marked held: %+v", f)
	}
	// What a reader is shown is a projection: the row stores no new state.
	if g.SyncState == ledger.SyncHeld || pull.Target.ShownState() != ledger.SyncHeld {
		t.Errorf("stored %s, pull result shows %s", g.SyncState, pull.Target.ShownState())
	}

	// A Keep elsewhere compiles, but nothing is delivered over the held file.
	f.remember(zz, space, "Every write tool returns a receipt ID.")
	run2 := f.compiled(shown(), "compiled v2")
	if err := deliver(run2); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Fatalf("delivered over a held file: %v", err)
	}
	if g := shown(); g.ShownState() != ledger.SyncHeld {
		t.Errorf("after a compile while held: %s", g.ShownState())
	}

	// Keeping one: still held for the other.
	kept := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: keepP.ID.String()})
	if g := shown(); g.ShownState() != ledger.SyncHeld || !slices.Equal(g.Holds[0].Proposals, []string{newP.Ref}) {
		t.Errorf("after one keep: %s %+v", g.ShownState(), g.Holds)
	}
	// Rejecting the other lifts the hold: the latest compile is due over
	// the edited file, which Memax may now replace.
	rejected := f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: newP.ID.String()})
	g = shown()
	if len(g.Holds) != 0 || (g.ShownState() != ledger.SyncCompiling && g.ShownState() != ledger.SyncPendingDelivery) {
		t.Fatalf("after both are decided: %s %+v", g.ShownState(), g.Holds)
	}
	if f := g.Delivered.Files[0]; f.Held == nil || *f.Held {
		t.Errorf("the baseline file is still marked held: %+v", f)
	}

	// Undo of the decision that lifted it, before anything was delivered
	// over the edit: the file is held again.
	if _, err := undo(f, person(zz), f.scope(zz), rejected.Receipts[0].ID); err != nil {
		t.Fatalf("undo the reject: %v", err)
	}
	if g := shown(); g.ShownState() != ledger.SyncHeld || !slices.Equal(g.Holds[0].Proposals, []string{newP.Ref}) {
		t.Errorf("after undoing the reject: %s %+v", g.ShownState(), g.Holds)
	}
	again := f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: newP.ID.String()})
	run3 := f.compiled(shown(), "compiled v3, with the kept line")
	if g := shown(); g.ShownState() != ledger.SyncPendingDelivery {
		t.Errorf("before the delivery: %s", g.ShownState())
	}
	if err := deliver(run3); err != nil {
		t.Fatalf("deliver after the hold lifted: %v", err)
	}
	if g := shown(); g.ShownState() != ledger.SyncInSync || g.Delivered.Files[0].Observation != nil || g.Delivered.Compile != run3.Ref {
		t.Errorf("after the delivery: %s %+v", g.ShownState(), g.Delivered)
	}
	// Once delivered over, the edit isn't on disk to hold: undoing the
	// reject makes it a proposal again, and holds nothing.
	if _, err := undo(f, person(zz), f.scope(zz), again.Receipts[0].ID); err != nil {
		t.Fatalf("undo the second reject: %v", err)
	}
	if g := shown(); g.ShownState() != ledger.SyncInSync || len(g.Holds) != 0 {
		t.Errorf("an undo after the delivery held the file: %s %+v", g.ShownState(), g.Holds)
	}
	_ = kept

	// A hold lifted by rejects alone, with nothing recompiled: the
	// delivered run is due over the edit again, so the rejected lines go.
	pull2, _ := pullEdit("edit two")
	for _, p := range pull2.Proposals {
		f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.ID.String()})
	}
	g = shown()
	if g.SyncState != ledger.SyncInSync || g.ShownState() != ledger.SyncPendingDelivery {
		t.Fatalf("rejected without a recompile: stored %s, shown %s", g.SyncState, g.ShownState())
	}
	if err := deliver(run3); err != nil {
		t.Fatalf("redeliver %s over the rejected edit: %v", run3.Ref, err)
	}
	if g := shown(); g.ShownState() != ledger.SyncInSync || g.Delivered.Files[0].Observation != nil {
		t.Errorf("after redelivering: %s %+v", g.ShownState(), g.Delivered)
	}
}

// A pull that writes no proposal (a removal only) holds nothing.
func TestPullWithoutProposalsHoldsNothing(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	pnpm := f.remember(zz, space, "pnpm workspaces only.")
	f.brief(zz, space, 0, demoSections(pnpm.Ref))
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	run := f.compiled(tg, "compiled v1")
	f.apply(&ledger.RecordDelivery{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: tg.ID, Compile: run.Ref, SHA256: run.DriftSHA256})
	f.apply(&ledger.RecordObservation{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: tg.ID, Path: "AGENTS.md",
		ObservedSHA256: sha("removed"), ObserverKind: ledger.ObserverDevice, ObserverID: "d", ArtifactKey: "obs/r", Bytes: 7,
		Changes: ledger.ChangeSet{Changes: []ledger.DriftChange{{Kind: ledger.ChangeRemove, Ref: pnpm.Ref, Refs: []string{pnpm.Ref}, OldText: pnpm.Statement, OldLine: 4}}}})
	pull := f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftPull})
	if len(pull.Proposals) != 0 || pull.Target.ShownState() != ledger.SyncInSync || len(pull.Target.Holds) != 0 {
		t.Errorf("a pull without proposals: %s %+v", pull.Target.ShownState(), pull.Target.Holds)
	}
}
