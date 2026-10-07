package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func forgetCmd(actor ledger.Actor, scope ledger.Scope, via policy.Via, ref string, version int, carries ...string) *ledger.Forget {
	return &ledger.Forget{Meta: meta(actor, scope, via), Memory: ref, ExpectedVersion: version, Carries: carries}
}

// Forget takes the words out of every row that held them, redacts the
// receipts' reasons, writes the tombstone, and leaves the memory a shell
// that says forgotten.
func TestForgetPurgesTheWords(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	nm := fact(space, "Jiahao is away from Oct 12 to Oct 26, so route reviews to Ziyang.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceSession, Ref: "Session 3e1a", Quote: "Jiahao said he is away Oct 12-26."}}
	rm := meta(person(zz), scope, policy.ViaWeb)
	rm.Reason = "Jiahao told me in standup"
	res := f.apply(&ledger.Remember{Meta: rm, NewMemory: nm})
	m := res.Memory
	ed := meta(person(zz), scope, policy.ViaWeb)
	ed.Reason = "the dates moved: Oct 13 to Oct 27"
	f.apply(&ledger.Edit{Meta: ed, Memory: m.Ref, ExpectedVersion: 1, Statement: "Jiahao is away from Oct 13 to Oct 27; route reviews to Ziyang."})

	// A stale version is an edit clash.
	if _, err := f.l.Apply(ctx, forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1)); !errors.Is(err, ledger.ErrEditClash) {
		t.Fatalf("forget at version 1: %v, want an edit clash", err)
	}
	cmd := forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 2)
	cmd.Note = "personal, and not something agents need"
	out := f.apply(cmd)
	if out.Outcome != ledger.OutcomeApplied || out.Memory.Lifecycle != lifecycle.Forgotten || out.Memory.Statement != "" {
		t.Fatalf("forget: %s %s %q", out.Outcome, out.Memory.Lifecycle, out.Memory.Statement)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL`, m.ID); n != 0 {
		t.Errorf("%d versions still hold words", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.sources s JOIN v2.memory_sources ms ON ms.source_id = s.id
		WHERE ms.memory_id = $1 AND (s.quote IS NOT NULL OR s.uri IS NOT NULL OR s.ref <> s.kind)`, m.ID); n != 0 {
		t.Errorf("%d sources still hold words", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND (reason IS NOT NULL OR reason_salt IS NOT NULL)`, m.ID); n != 0 {
		t.Errorf("%d receipts still hold a reason", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND reason_sha256 IS NOT NULL`, m.ID); n != 2 {
		t.Errorf("%d receipts keep their commitment, want 2", n)
	}
	ts := out.Tombstone
	if ts == nil || ts.Ref != m.Ref || ts.Note != cmd.Note || ts.Status != ledger.TombstonePropagating || ts.Gone.Versions != 2 || ts.Gone.Sources != 1 {
		t.Fatalf("tombstone: %+v", ts)
	}
	got, err := f.l.GetTombstone(ctx, scope, m.Ref)
	if err != nil || got.ID != ts.ID {
		t.Fatalf("GetTombstone: %+v, %v", got, err)
	}
	// Forgetting again is a refused transition; replaying the same key is
	// the same answer.
	if _, err := f.l.Apply(ctx, forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 2)); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("forget twice: %v, want invalid transition", err)
	}
	again, err := f.l.Apply(ctx, cmd)
	if err != nil || !again.Replayed || again.Tombstone == nil || again.Tombstone.ID != ts.ID {
		t.Errorf("replay: %+v, %v", again, err)
	}
	// Undo never brings it back.
	_, err = f.l.Apply(ctx, &ledger.Undo{Meta: meta(person(zz), scope, policy.ViaWeb), Receipt: out.Receipts[0].ID})
	var ue *ledger.UndoError
	if !errors.As(err, &ue) || ue.Reason != ledger.UndoNotUndoable {
		t.Errorf("undo the forget: %v, want not_undoable", err)
	}
	_ = uuid.Nil
}
