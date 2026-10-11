package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// staleWorld is the Dream world with its date flag set: w.s ("Ask memax
// answers with the Haiku tier.") is past its stale_after, and Dream's
// edition flagged it. Its fixture has River, and AGENTS.md compiles.
type staleWorld struct {
	*dreamWorld
	target  *ledger.Target
	flagged ledger.Receipt
}

func newStaleWorld(t *testing.T) *staleWorld {
	t.Helper()
	w := &staleWorld{dreamWorld: newDreamWorldOn(t, newCompileFixture(t))}
	w.target = w.f.target(w.owner, w.space, ledger.TargetAgentsMD)
	res := w.publish(time.Now().Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamStale, Memory: w.s.ID, Version: 1})
	for _, rc := range res.Receipts {
		if rc.Action == ledger.ActionFlagged && rc.ObjectID == w.s.ID {
			w.flagged = rc
		}
	}
	if w.flagged.ID == uuid.Nil {
		t.Fatalf("Dream didn't flag %s: %+v", w.s.Ref, res.Receipts)
	}
	if m := w.memory(w.s.Ref); m.State != lifecycle.MarkStale {
		t.Fatalf("%s is %s, want stale", m.Ref, m.State)
	}
	return w
}

func (w *staleWorld) memory(ref string) *ledger.Memory {
	w.f.t.Helper()
	m, err := w.f.l.GetMemory(context.Background(), w.f.scope(w.owner), ref)
	if err != nil {
		w.f.t.Fatal(err)
	}
	return m
}

// verifyCmd is a Verify of w.s by actor, from version 1.
func (w *staleWorld) verifyCmd(actor ledger.Actor, via policy.Via, answer ledger.VerifyAnswer) *ledger.Verify {
	return &ledger.Verify{Meta: meta(actor, w.f.scope(w.owner), via), Memory: w.s.Ref, ExpectedVersion: 1, Answer: answer}
}

// Rule 10, its Verify half: "still true" clears the flag with a person's
// verified receipt that names the date that passed, the date goes, the
// targets recompile, and nothing else about the memory changes.
func TestVerifyStillTrue(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	gen := w.f.get(w.owner, w.target.ID).DirtyGen
	cmd := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue)
	res := w.f.apply(cmd)
	if res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 1 {
		t.Fatalf("verify: %+v", res)
	}
	rc := res.Receipts[0]
	date := w.s.StaleAfter.UTC().Format(time.DateOnly)
	if rc.Action != ledger.ActionVerified || rc.ActorKind != policy.ActorPerson || rc.ActorID == nil || *rc.ActorID != w.owner ||
		rc.Assurance != policy.AssuranceHumanWeb || rc.Source == nil || rc.Source.Kind != "date" || rc.Source.Ref != date {
		t.Errorf("receipt %+v (source %+v), want verified by the person, citing the date %s", rc, rc.Source, date)
	}
	m := res.Memory
	if m.State != lifecycle.MarkKept || m.Flags.Has(lifecycle.Stale) || m.StaleAfter != nil || m.Version != 1 ||
		m.Statement != "Ask memax answers with the Haiku tier." {
		t.Errorf("after verify: %s flags %v stale_after %v v%d %q", m.State, m.Flags, m.StaleAfter, m.Version, m.Statement)
	}
	if got := w.f.get(w.owner, w.target.ID).DirtyGen; got <= gen {
		t.Errorf("AGENTS.md wasn't marked to recompile: generation %d → %d", gen, got)
	}
	if w.f.jobs(w.target.ID) == 0 {
		t.Error("no compile queued")
	}
	// A retry with the same key answers the same and writes nothing.
	receipts := w.f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1`, w.s.ID)
	again := w.f.apply(cmd)
	if !again.Replayed || len(again.Receipts) != 1 || again.Receipts[0].ID != rc.ID {
		t.Errorf("replay %+v", again)
	}
	if n := w.f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1`, w.s.ID); n != receipts {
		t.Errorf("the replay wrote %d receipts", n-receipts)
	}
	// Verifying again: it isn't stale any more.
	_, err := w.f.l.Apply(context.Background(), w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue))
	var te *ledger.TransitionError
	if !errors.As(err, &te) {
		t.Errorf("verifying a memory that isn't stale: %v, want a transition error", err)
	}
	w.f.everyRowReceipted(t)
}

// A date answer moves the date to check again; "still true" from the CLI
// is client-attested.
func TestVerifyWithANewDate(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	next := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	cmd := w.verifyCmd(person(w.owner), policy.ViaCLI, ledger.VerifyStillTrue)
	cmd.StaleAfter = &next
	res := w.f.apply(cmd)
	if res.Memory.StaleAfter == nil || !res.Memory.StaleAfter.Equal(next) || res.Memory.State != lifecycle.MarkKept {
		t.Errorf("stale_after %v state %s, want %v kept", res.Memory.StaleAfter, res.Memory.State, next)
	}
	if res.Receipts[0].Assurance != policy.AssuranceClientAttested {
		t.Errorf("assurance %s from the CLI", res.Receipts[0].Assurance)
	}
	past := time.Now().Add(-time.Hour)
	bad := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue)
	bad.StaleAfter = &past
	if _, err := w.f.l.Apply(context.Background(), bad); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a date in the past: %v", err)
	}
}

// "Update it" writes the new words (edited) and then verified; the flag
// and the passed date go; Undo, by the same person, puts back the words,
// the flag and the date, as one command.
func TestVerifyUpdateAndUndo(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	before := w.fingerprint()
	cmd := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyUpdate)
	cmd.Statement = "Ask memax answers with the Sonnet tier."
	cmd.Reason = "PR #198 moved Ask to Sonnet."
	res := w.f.apply(cmd)
	if res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 2 ||
		res.Receipts[0].Action != ledger.ActionEdited || res.Receipts[1].Action != ledger.ActionVerified {
		t.Fatalf("update: %+v", res.Receipts)
	}
	if res.Receipts[1].Reason != cmd.Reason || res.Receipts[0].StreamVersion+1 != res.Receipts[1].StreamVersion {
		t.Errorf("receipts %+v", res.Receipts)
	}
	m := res.Memory
	if m.Statement != cmd.Statement || m.Version != 2 || m.State != lifecycle.MarkKept || m.StaleAfter != nil {
		t.Errorf("after update: %q v%d %s stale_after %v", m.Statement, m.Version, m.State, m.StaleAfter)
	}
	for _, rc := range res.Receipts {
		if strings.Contains(rc.Reason, "Sonnet tier") && rc.Action == ledger.ActionEdited {
			t.Errorf("a receipt holds the words: %+v", rc)
		}
	}
	// Dream's own undo of its flag can't take back what the person did since.
	var action uuid.UUID
	if err := w.f.pool.QueryRow(context.Background(), `SELECT id FROM v2.dream_actions WHERE memory_id = $1 AND kind = 'stale'`,
		w.s.ID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if _, err := w.undo(action); err == nil {
		t.Error("undoing Dream's flag after a person verified it went through")
	}
	// Undo the verify: words, flag and date come back.
	undo := w.f.apply(&ledger.Undo{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Receipt: res.Receipts[1].ID})
	if undo.Outcome != ledger.OutcomeApplied {
		t.Fatalf("undo: %+v", undo)
	}
	after := w.memory(w.s.Ref)
	if after.State != lifecycle.MarkStale || after.Version != 1 || after.Statement != "Ask memax answers with the Haiku tier." ||
		after.StaleAfter == nil || !after.StaleAfter.Equal(*w.s.StaleAfter) {
		t.Errorf("after undo: %s v%d %q stale_after %v", after.State, after.Version, after.Statement, after.StaleAfter)
	}
	if d := diffPrints(before, w.fingerprint()); len(d) != 0 {
		t.Errorf("undo left changes: %v", d)
	}
	// Someone else can't undo it.
	jy := w.f.user("jy")
	w.f.join(w.space, jy, "contributor")
	res2 := w.f.apply(w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue))
	other := w.f.apply(&ledger.Undo{Meta: meta(person(jy), w.f.scope(jy), policy.ViaWeb), Receipt: res2.Receipts[0].ID})
	if other.Outcome != ledger.OutcomeRefused || other.Policy.Code != policy.CodeUndoByDecider {
		t.Errorf("another person's undo: %+v", other)
	}
	w.f.everyRowReceipted(t)
}

// Who may verify, and what's refused before anything is written.
func TestVerifyRefusals(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	ctx := context.Background()
	jy := w.f.user("jy")
	w.f.join(w.space, jy, "viewer")
	writer := agentFor(policy.AutonomyWrite)
	receipts := w.f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1`, w.space)

	for _, c := range []struct {
		name string
		cmd  *ledger.Verify
		code string
	}{
		{"an agent at Write", w.verifyCmd(writer, policy.ViaMCP, ledger.VerifyStillTrue), policy.CodeVerifyByPerson},
		{"a viewer", &ledger.Verify{Meta: meta(person(jy), w.f.scope(jy), policy.ViaWeb), Memory: w.s.Ref, ExpectedVersion: 1,
			Answer: ledger.VerifyStillTrue}, policy.CodeViewer},
		{"new words with a secret", func() *ledger.Verify {
			c := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyUpdate)
			c.Statement = "Ask uses the key AKIAIOSFODNN7EXAMPLE."
			return c
		}(), policy.CodeSecret},
	} {
		res, err := w.f.l.Apply(ctx, c.cmd)
		if err != nil || res.Outcome != ledger.OutcomeRefused || res.Policy.Code != c.code {
			t.Errorf("%s: %v %+v, want refused %s", c.name, err, res.Policy, c.code)
		}
	}
	clash := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue)
	clash.ExpectedVersion = 2
	var ce *ledger.EditClashError
	if _, err := w.f.l.Apply(ctx, clash); !errors.As(err, &ce) {
		t.Errorf("a stale version: %v", err)
	}
	same := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyUpdate)
	same.Statement = "Ask memax answers with the Haiku tier."
	if _, err := w.f.l.Apply(ctx, same); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("an update with the same words: %v", err)
	}
	words := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue)
	words.Statement = "New words."
	if _, err := w.f.l.Apply(ctx, words); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("still true with words: %v", err)
	}
	if _, err := w.f.l.Apply(ctx, w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyForget)); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("forget through the ledger's Verify: %v", err)
	}
	notStale := &ledger.Verify{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Memory: w.k.Ref, ExpectedVersion: 1,
		Answer: ledger.VerifyStillTrue}
	var te *ledger.TransitionError
	if _, err := w.f.l.Apply(ctx, notStale); !errors.As(err, &te) {
		t.Errorf("a memory that isn't stale: %v", err)
	}
	if n := w.f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1`, w.space); n != receipts {
		t.Errorf("refusals wrote %d receipts", n-receipts)
	}
	if m := w.memory(w.s.Ref); m.State != lifecycle.MarkStale {
		t.Errorf("after the refusals %s is %s", m.Ref, m.State)
	}
}

// Only a person clears a stale flag: an agent's edit of a stale memory is
// a proposal that would update it, and the flag stays. A person's own edit
// clears it, and the passed date with it; Undo puts both back.
func TestOnlyAPersonClearsAStaleFlag(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	scope := w.f.scope(w.owner)
	writer := agentFor(policy.AutonomyWrite)
	res := w.f.apply(&ledger.Edit{Meta: meta(writer, scope, policy.ViaMCP), Memory: w.s.Ref, ExpectedVersion: 1,
		Statement: "Ask memax answers with the Sonnet tier."})
	if res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeVerifyByPerson || res.Memory.Ref == w.s.Ref {
		t.Errorf("an agent's edit of a stale memory: %s %+v", res.Outcome, res.Policy)
	}
	if res.Memory.Updates == nil || res.Memory.Updates.Ref != w.s.Ref {
		t.Errorf("the proposal doesn't say it updates %s: %+v", w.s.Ref, res.Memory.Updates)
	}
	if m := w.memory(w.s.Ref); m.State != lifecycle.MarkStale {
		t.Errorf("after the agent's edit %s is %s", m.Ref, m.State)
	}
	edit := w.f.apply(&ledger.Edit{Meta: meta(person(w.owner), scope, policy.ViaWeb), Memory: w.s.Ref, ExpectedVersion: 1,
		Statement: "Ask memax answers with the Sonnet tier."})
	if m := edit.Memory; m.State != lifecycle.MarkKept || m.StaleAfter != nil {
		t.Errorf("after a person's edit: %s stale_after %v", m.State, m.StaleAfter)
	}
	w.f.apply(&ledger.Undo{Meta: meta(person(w.owner), scope, policy.ViaWeb), Receipt: edit.Receipts[0].ID})
	if m := w.memory(w.s.Ref); m.State != lifecycle.MarkStale || m.StaleAfter == nil || !m.StaleAfter.Equal(*w.s.StaleAfter) {
		t.Errorf("after undoing the edit: %s stale_after %v", m.State, m.StaleAfter)
	}
}

// The stale report says why, in one read: the flag's receipt, the date,
// the keep it followed and the reads since; once verified, nothing.
func TestStaleReport(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	ctx := context.Background()
	scope := w.f.scope(w.owner)
	w.f.record(readOf(w.s, uuid.New(), w.owner, "claude-code", ledger.ReadGet, time.Now(), w.s.ID))
	r, err := w.f.l.GetStaleReport(ctx, scope, w.s.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stale || r.Ref != w.s.Ref || r.Version != 1 || r.Statement != "Ask memax answers with the Haiku tier." ||
		r.State != lifecycle.MarkStale || r.Flagged == nil || r.Flagged.ID != w.flagged.ID || r.Kept == nil ||
		r.Kept.Action != ledger.ActionKept || r.ReadsSince != 1 {
		t.Fatalf("report %+v (flagged %+v, kept %+v)", r, r.Flagged, r.Kept)
	}
	if len(r.Causes) != 1 || r.Causes[0].Kind != ledger.StaleCauseDate || !r.Causes[0].StaleAfter.Equal(*w.s.StaleAfter) {
		t.Errorf("causes %+v", r.Causes)
	}
	// By id too.
	if byID, err := w.f.l.GetStaleReport(ctx, scope, w.s.ID.String()); err != nil || byID.Ref != w.s.Ref {
		t.Errorf("by id: %v %+v", err, byID)
	}
	w.f.apply(w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyStillTrue))
	r, err = w.f.l.GetStaleReport(ctx, scope, w.s.Ref)
	if err != nil || r.Stale || len(r.Causes) != 0 || r.Kept == nil || r.Kept.Action != ledger.ActionVerified {
		t.Errorf("after verify: %v %+v", err, r)
	}
	// Another space's memory isn't found.
	jy := w.f.user("jy")
	if _, err := w.f.l.GetStaleReport(ctx, w.f.scope(jy), w.s.ID.String()); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person's report: %v", err)
	}
	if _, err := w.f.l.GetStaleReport(ctx, scope, "M-9999"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a missing ref: %v", err)
	}
}
