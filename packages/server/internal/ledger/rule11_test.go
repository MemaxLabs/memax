package ledger_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 11's two holes, closed (plan 25 §5.6 downgrade (b), the Phase 1
// gate's decision): the judge puts a Write agent's write that contradicts
// a decision in force back in Review, and "keep both" narrowing waits for
// the judge when it touches another decision in force.

// lastReceipt is a memory's newest receipt.
func (f *fixture) lastReceipt(user, id uuid.UUID) ledger.Receipt {
	f.t.Helper()
	h, err := f.l.GetMemoryHistory(context.Background(), f.scope(user), id.String())
	if err != nil || len(h.Receipts.Receipts) == 0 {
		f.t.Fatalf("history: %v", err)
	}
	rs := h.Receipts.Receipts
	newest := rs[0]
	for _, r := range rs {
		if r.Seq > newest.Seq {
			newest = r
		}
	}
	return newest
}

// noWordsIn fails when a receipt's reason quotes any of the statements.
func noWordsIn(t *testing.T, rc ledger.Receipt, statements ...string) {
	t.Helper()
	for _, s := range statements {
		if s != "" && strings.Contains(rc.Reason, s) {
			t.Errorf("receipt %s quotes %q: %q", rc.Action, s, rc.Reason)
		}
	}
}

// The judge's return, and every way it is held back: only an agent's own
// write, inside the window, with nothing changed or built on it since.
func TestJudgeReturnsAWriteAgentsWrite(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	const words = "Previews build on Fly.io machines."
	type setup struct {
		sp     uuid.UUID
		d, k   *ledger.Memory
		writer ledger.Actor
		target *ledger.Target
	}
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	start := func(t *testing.T) setup {
		t.Helper()
		s := setup{sp: f.space(zz, policy.SpaceProject, t.Name()), writer: agentFor(policy.AutonomyWrite)}
		f.brief(zz, s.sp, 0, nil)
		s.target = f.target(zz, s.sp, ledger.TargetAgentsMD)
		s.d = f.apply(&ledger.Remember{Meta: me(), NewMemory: decisionIn(s.sp, "Deploy the v2 API to Railway.", "deploy target")}).Memory
		s.k = f.apply(&ledger.Propose{Meta: meta(s.writer, f.scope(zz), policy.ViaMCP), NewMemory: fact(s.sp, words)}).Memory
		if s.k.Lifecycle != lifecycle.Kept {
			t.Fatalf("the agent's write = %s, want kept at once", s.k.Lifecycle)
		}
		return s
	}
	for _, c := range []struct {
		name string
		// prep changes the record after the write and returns the memory
		// version the judge rules on.
		prep    func(t *testing.T, s setup) *ledger.Memory
		late    bool
		returns bool
	}{
		{name: "an agent's write, judged in the window", returns: true},
		{name: "an agent's edit of its own write", returns: true, prep: func(t *testing.T, s setup) *ledger.Memory {
			e := f.apply(&ledger.Edit{Meta: meta(s.writer, f.scope(zz), policy.ViaMCP), Memory: s.k.Ref, ExpectedVersion: 1,
				Statement: "Previews build on Fly.io machines in iad."})
			if e.Outcome != ledger.OutcomeApplied || e.Memory.Version != 2 {
				t.Fatalf("agent edit: %s v%d", e.Outcome, e.Memory.Version)
			}
			return e.Memory
		}},
		{name: "after the return window", late: true},
		{name: "a person edited it since", prep: func(t *testing.T, s setup) *ledger.Memory {
			return f.apply(&ledger.Edit{Meta: me(), Memory: s.k.Ref, ExpectedVersion: 1, Statement: "Previews build on Fly.io machines, for now."}).Memory
		}},
		{name: "a person kept it", prep: func(t *testing.T, s setup) *ledger.Memory {
			return f.apply(&ledger.Remember{Meta: me(), NewMemory: fact(s.sp, "Preview builds run on Fly.io machines.")}).Memory
		}},
		{name: "a proposal updates it", prep: func(t *testing.T, s setup) *ledger.Memory {
			q := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
				NewMemory: fact(s.sp, "Previews build on Fly.io machines with 2 GB.")}).Memory
			f.judgeAs(s.sp, q, ledger.OutcomeLinked, s.k, ledger.JudgeProposal)
			return f.mem(zz, s.k.ID)
		}},
		{name: "the Brief places it", prep: func(t *testing.T, s setup) *ledger.Memory {
			f.brief(zz, s.sp, 1, demoSections(s.k.Ref))
			return f.mem(zz, s.k.ID)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := start(t)
			m := s.k
			if c.prep != nil {
				m = c.prep(t, s)
			}
			gen := f.get(zz, s.target.ID).DirtyGen
			judge := f.judgeAs
			if c.late {
				judge = f.judgeLate
			}
			res := judge(s.sp, m, ledger.OutcomeFlagged, s.d, ledger.JudgeKept)
			if res.Unchanged || len(res.Receipts) != 1 {
				t.Fatalf("verdict: unchanged=%v receipts=%v", res.Unchanged, actions(res.Receipts))
			}
			rc := res.Receipts[0]
			got := f.mem(zz, m.ID)
			if !got.Flags.Has(lifecycle.Conflict) || got.State != lifecycle.MarkConflict || !hasLink(got, ledger.LinkConflictsWith, ledger.LinkOut, s.d.ID) {
				t.Errorf("after: %s %v links %+v", got.State, got.Flags, got.Links)
			}
			if rc.ActorKind != policy.ActorMemax || rc.Via != policy.ViaSystem || rc.Source == nil || rc.Source.Ref != s.d.Ref {
				t.Errorf("receipt = %+v", rc)
			}
			noWordsIn(t, rc, words, m.Statement, s.d.Statement)
			if got.Version != m.Version {
				t.Errorf("version %d → %d; a return never writes words", m.Version, got.Version)
			}
			if c.returns {
				if got.Lifecycle != lifecycle.Proposed || rc.Action != ledger.ActionReturned {
					t.Errorf("want it back in Review: %s, receipt %s", got.Lifecycle, rc.Action)
				}
			} else if got.Lifecycle != lifecycle.Kept || rc.Action != ledger.ActionFlagged {
				t.Errorf("want it flagged where it stands: %s, receipt %s", got.Lifecycle, rc.Action)
			}
			// Either way it changes what compiles: out of the files, or
			// marked in conflict.
			if g := f.get(zz, s.target.ID).DirtyGen; g != gen+1 || f.jobs(s.target.ID) == 0 {
				t.Errorf("dirty_gen %d → %d, %d compile jobs", gen, g, f.jobs(s.target.ID))
			}
		})
	}

	// A verdict on a version that's no longer current changes nothing.
	t.Run("a later version exists", func(t *testing.T) {
		s := start(t)
		f.apply(&ledger.Edit{Meta: meta(s.writer, f.scope(zz), policy.ViaMCP), Memory: s.k.Ref, ExpectedVersion: 1,
			Statement: "Previews build on Fly.io machines in ams."})
		if res := f.judgeAs(s.sp, s.k, ledger.OutcomeFlagged, s.d, ledger.JudgeKept); !res.Unchanged {
			t.Errorf("verdict on v1 applied: %v", actions(res.Receipts))
		}
		if got := f.mem(zz, s.k.ID); got.Lifecycle != lifecycle.Kept || len(got.Flags) != 0 {
			t.Errorf("after: %s %v", got.Lifecycle, got.Flags)
		}
	})
	// A verdict that finds nothing leaves the write kept.
	t.Run("nothing found", func(t *testing.T) {
		s := start(t)
		res := f.judgeAs(s.sp, s.k, ledger.OutcomeNone, nil, ledger.JudgeKept)
		if res.Receipts[0].Action != ledger.ActionJudged || f.mem(zz, s.k.ID).Lifecycle != lifecycle.Kept {
			t.Errorf("verdict none: %v", actions(res.Receipts))
		}
	})
}

// A return isn't undone: a person settles the conflict, and keeping the
// write is then their own Keep, with its assurance.
func TestAReturnIsSettledNotUndone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	d := f.apply(&ledger.Remember{Meta: me(), NewMemory: decisionIn(sp, "Deploy the v2 API to Railway.", "deploy target")}).Memory
	k := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP),
		NewMemory: fact(sp, "Previews build on Fly.io machines.")}).Memory
	ret := f.judgeAs(sp, k, ledger.OutcomeFlagged, d, ledger.JudgeKept)
	if ret.Receipts[0].Action != ledger.ActionReturned {
		t.Fatalf("verdict = %v", actions(ret.Receipts))
	}
	if n := f.count(`SELECT count(*) FROM v2.undo_entries WHERE $1 = ANY (receipt_ids)`, ret.Receipts[0].ID); n != 0 {
		t.Errorf("%d undo entries for the return", n)
	}
	_, err := undo(f, person(zz), f.scope(zz), ret.Receipts[0].ID)
	var ue *ledger.UndoError
	if !errors.As(err, &ue) || ue.Reason != ledger.UndoNotUndoable || !strings.Contains(ue.Message, "Settle the conflict") {
		t.Fatalf("undo the return = %v", err)
	}
	// Keep is refused while it's in conflict, naming the decision.
	_, err = f.l.Apply(context.Background(), &ledger.Keep{Meta: me(), Memory: k.Ref})
	var ic *ledger.InConflictError
	if !errors.As(err, &ic) || ic.With != d.Ref {
		t.Errorf("keep = %v", err)
	}
	// The compare view has the four answers, and "keep both" keeps it.
	v, err := f.l.GetConflict(context.Background(), f.scope(zz), person(zz), policy.ViaWeb, k.Ref, "")
	if err != nil || v.FlaggedRef != k.Ref || v.DecisionRef != d.Ref {
		t.Fatalf("conflict: %v %+v", err, v)
	}
	res := f.apply(&ledger.ResolveConflict{Meta: me(), Memory: k.Ref, Choice: ledger.ChooseBoth})
	if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionKept}) {
		t.Errorf("receipts = %v", got)
	}
	if res.Receipts[1].ActorKind != policy.ActorPerson || res.Receipts[1].Assurance != policy.AssuranceHumanWeb {
		t.Errorf("keep receipt = %+v", res.Receipts[1])
	}
	if got := f.mem(zz, k.ID); got.Lifecycle != lifecycle.Kept || len(got.Flags) != 0 {
		t.Errorf("after: %s %v", got.Lifecycle, got.Flags)
	}
}

// receiptAs writes a receipt about m by an actor kind, in the transaction.
func receiptAs(tx pgx.Tx, m *ledger.Memory, action, actorKind string, version int) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	var actorID any
	if actorKind == "agent" || actorKind == "person" {
		actorID = uuid.New()
	}
	_, err := tx.Exec(context.Background(), `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
		VALUES ($1, $2, $3, 'memory', $4, $5, $6, $7, $8, 'system', now(), $4, $9)`,
		id, m.TenantID, m.SpaceID, m.ID, m.Ref, action, actorKind, actorID, version)
	return id, err
}

// The database admits kept → proposed only as the judge's return: beside
// Memax's `returned` receipt, in conflict, at the same version, right
// after the agent's own keep. Everything else is refused (MXL01).
func TestDatabaseAdmitsOnlyTheJudgesReturn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	agentWrite := func(s string) *ledger.Memory {
		return f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), NewMemory: fact(sp, s)}).Memory
	}
	move := func(m *ledger.Memory, action, actor, set string, version int) error {
		return f.asV2([]uuid.UUID{sp}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
			rid, err := receiptAs(tx, m, action, actor, version)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'proposed', last_receipt_id = $2, stream_version = $3, `+set+` WHERE id = $1`,
				m.ID, rid, version)
			return err
		})
	}
	const flagged = `flags = '{conflict}'`
	for _, c := range []struct {
		name    string
		m       *ledger.Memory
		action  string
		actor   string
		set     string
		version int
	}{
		{"an edited receipt", agentWrite("One."), "edited", "memax", flagged, 2},
		{"returned by an agent", agentWrite("Three."), "returned", "agent", flagged, 2},
		{"returned by a person", agentWrite("Four."), "returned", "person", flagged, 2},
		{"without the conflict flag", agentWrite("Five."), "returned", "memax", `flags = '{}'`, 2},
		{"a person's keep", f.remember(zz, sp, "Six."), "returned", "memax", flagged, 2},
		{"with a new version", agentWrite("Seven."), "returned", "memax", flagged + `, current_version = 2`, 2},
		{"skipping a receipt", agentWrite("Eight."), "returned", "memax", flagged, 3},
	} {
		if err := move(c.m, c.action, c.actor, c.set, c.version); sqlstate(err) != "MXL01" {
			t.Errorf("%s: %v, want MXL01", c.name, err)
		}
	}
	// A `returned` receipt from an earlier transaction admits nothing.
	early := agentWrite("Two.")
	var rid uuid.UUID
	if err := f.asV2([]uuid.UUID{sp}, []uuid.UUID{early.TenantID}, func(tx pgx.Tx) error {
		var err error
		rid, err = receiptAs(tx, early, "returned", "memax", 2)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.asV2([]uuid.UUID{sp}, []uuid.UUID{early.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'proposed', flags = '{conflict}', last_receipt_id = $2, stream_version = 2
		                         WHERE id = $1`, early.ID, rid)
		return err
	}); sqlstate(err) != "MXL01" {
		t.Errorf("a receipt from another transaction: %v, want MXL01", err)
	}
	// The return as RecordVerdict writes it commits.
	ok := agentWrite("Nine.")
	if err := move(ok, "returned", "memax", flagged, 2); err != nil {
		t.Errorf("the judge's return: %v", err)
	}
	// And only once: a returned memory is a proposal, so a second return
	// (or anything else that makes a proposal of it) has nothing to admit.
	agentEdit := agentWrite("Ten.")
	f.apply(&ledger.Edit{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: agentEdit.Ref, ExpectedVersion: 1, Statement: "Ten, edited."})
	agentEdit = f.mem(zz, agentEdit.ID)
	if err := move(agentEdit, "returned", "memax", flagged, agentEdit.Version+1); sqlstate(err) != "MXL01" {
		// The row's receipt is a person's edit now.
		t.Errorf("after a person's edit: %v, want MXL01", err)
	}
}

// v2.lifecycle_return_allowed is lifecycle.ReturnAllowed.
func TestReturnMatchesSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, a := range append([]lifecycle.Lifecycle{lifecycle.None}, lifecycle.Lifecycles...) {
		for _, b := range lifecycle.Lifecycles {
			var from any = string(a)
			if a == lifecycle.None {
				from = nil
			}
			var got bool
			if err := f.pool.QueryRow(context.Background(), `SELECT v2.lifecycle_return_allowed($1::text, $2)`, from, string(b)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := lifecycle.ReturnAllowed(a, b); got != want {
				t.Errorf("return %q → %q: SQL %v, Go %v", a, b, got, want)
			}
		}
	}
}

// A conflict with a third decision in force in the way: d is the decision
// the proposal p is flagged against, and x a decision about previews that
// narrowed words can touch.
type thirdDecision struct {
	sp      uuid.UUID
	p, d, x *ledger.Memory
}

func (f *fixture) withThirdDecision(owner uuid.UUID, name string) thirdDecision {
	f.t.Helper()
	sp := f.space(owner, policy.SpaceProject, name)
	p, d := f.conflictPair(owner, sp)
	x := f.apply(&ledger.Remember{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb),
		NewMemory: decisionIn(sp, "Preview environments run on Render.", "previews")}).Memory
	return thirdDecision{sp: sp, p: p, d: d, x: x}
}

// settleJob reads the settling judge job queued for a memory version: its
// mode and the side it leaves out.
func (f *fixture) settleJob(m uuid.UUID, version int) (mode, beside string) {
	f.t.Helper()
	if err := f.pool.QueryRow(context.Background(), `
		SELECT args->>'mode', COALESCE(args->>'beside', '') FROM river_job
		 WHERE kind = 'judge_proposal' AND args->>'memory_id' = $1 AND (args->>'version')::int = $2 AND args->>'mode' = 'settling'`,
		m.String(), version).Scan(&mode, &beside); err != nil {
		f.t.Fatalf("settling job for %s v%d: %v", m, version, err)
	}
	return mode, beside
}

// settled records a settling verdict on a version: nothing found, or a
// contradiction of the given decision.
func (f *fixture) settled(space uuid.UUID, m uuid.UUID, version int, contradicts *ledger.Memory) ledger.Result {
	f.t.Helper()
	scope, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: m, Version: version, Mode: ledger.JudgeSettling, Outcome: ledger.OutcomeNone,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationUnrelated},
	}
	if contradicts != nil {
		cmd.Outcome, cmd.Target = ledger.OutcomeFlagged, contradicts.ID
		cmd.Verdict = ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationContradicts, Related: contradicts.ID}
	}
	return f.apply(cmd)
}

// "Keep both" whose narrowed words touch a third decision in force waits
// for the judge, like edit-then-keep: the words are saved, the conflict
// stays open, the same resolution waits (503) and then applies, or the
// words turn out to contradict the third decision (409).
func TestKeepBothWaitsForTheJudge(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	resolve := func(c thirdDecision, version int, statement, other string) (ledger.Result, error) {
		return f.l.Apply(ctx, &ledger.ResolveConflict{Meta: me(), Memory: c.p.Ref, Other: c.d.Ref, Choice: ledger.ChooseBoth,
			ExpectedVersion: version, Statement: statement, OtherStatement: other})
	}
	pending := func(t *testing.T, err error, ref string) {
		t.Helper()
		var jp *ledger.JudgePendingError
		if !errors.As(err, &jp) || jp.Ref != ref {
			t.Fatalf("retry before the verdict = %v, want judge pending on %s", err, ref)
		}
	}
	inConflict := func(t *testing.T, err error, ref, with string) {
		t.Helper()
		var ic *ledger.InConflictError
		if !errors.As(err, &ic) || ic.Ref != ref || ic.With != with {
			t.Fatalf("retry after the verdict = %v, want %s in conflict with %s", err, ref, with)
		}
	}
	held := func(t *testing.T, res ledger.Result, err error, want ...ledger.Action) {
		t.Helper()
		if err != nil || res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeJudgePending || res.Policy.Effect != policy.EffectPropose {
			t.Fatalf("held = %v %s %s %s", err, res.Outcome, res.Policy.Effect, res.Policy.Code)
		}
		if got := actions(res.Receipts); !slices.Equal(got, want) {
			t.Errorf("held receipts = %v, want %v", got, want)
		}
		if res.Memory == nil || len(res.Memories) == 0 || res.Memories[0].ID != res.Memory.ID {
			t.Errorf("held memories = %+v", res.Memories)
		}
	}
	stillOpen := func(t *testing.T, c thirdDecision) {
		t.Helper()
		p := f.mem(zz, c.p.ID)
		if p.Lifecycle != lifecycle.Proposed || !p.Flags.Has(lifecycle.Conflict) || !hasLink(p, ledger.LinkConflictsWith, ledger.LinkOut, c.d.ID) {
			t.Errorf("the conflict didn't stay open: %s %v %+v", p.Lifecycle, p.Flags, p.Links)
		}
	}

	const narrowP = "Previews deploy to Fly.io; production stays on Railway."
	const narrowD = "Production deploys to Railway; previews run elsewhere."

	t.Run("the proposal's words, then nothing found", func(t *testing.T) {
		c := f.withThirdDecision(zz, "a")
		res, err := resolve(c, 1, narrowP, "")
		held(t, res, err, ledger.ActionEdited)
		if res.Memory.Version != 2 || res.Memory.Statement != narrowP {
			t.Errorf("saved = v%d %q", res.Memory.Version, res.Memory.Statement)
		}
		stillOpen(t, c)
		if mode, beside := f.settleJob(c.p.ID, 2); mode != "settling" || beside != c.d.ID.String() {
			t.Errorf("job = %s beside %s", mode, beside)
		}
		// The same resolution (now at version 2) waits for the verdict.
		_, err = resolve(c, 2, narrowP, "")
		pending(t, err, c.p.Ref)
		f.settled(c.sp, c.p.ID, 2, nil)
		res, err = resolve(c, 2, narrowP, "")
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("after the verdict = %v %s", err, res.Outcome)
		}
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionKept}) {
			t.Errorf("receipts = %v", got)
		}
		if p, d := f.mem(zz, c.p.ID), f.mem(zz, c.d.ID); p.Lifecycle != lifecycle.Kept || p.Version != 2 || d.Version != 1 || status(d) != "" {
			t.Errorf("after: %s v%d / v%d %s", p.Lifecycle, p.Version, d.Version, status(d))
		}
	})
	t.Run("the proposal's words contradict the third decision", func(t *testing.T) {
		c := f.withThirdDecision(zz, "b")
		res, err := resolve(c, 1, narrowP, "")
		held(t, res, err, ledger.ActionEdited)
		v := f.settled(c.sp, c.p.ID, 2, c.x)
		if v.Receipts[0].Action != ledger.ActionFlagged || v.Receipts[0].Source.Ref != c.x.Ref {
			t.Errorf("verdict receipt = %+v", v.Receipts[0])
		}
		_, err = resolve(c, 2, narrowP, "")
		inConflict(t, err, c.p.Ref, c.x.Ref)
		p := f.mem(zz, c.p.ID)
		if !hasLink(p, ledger.LinkConflictsWith, ledger.LinkOut, c.x.ID) || !hasLink(p, ledger.LinkConflictsWith, ledger.LinkOut, c.d.ID) {
			t.Errorf("links = %+v", p.Links)
		}
		stillOpen(t, c)
	})
	t.Run("the kept side's words wait as a draft", func(t *testing.T) {
		c := f.withThirdDecision(zz, "c")
		res, err := resolve(c, 1, "", narrowD)
		held(t, res, err, ledger.ActionDrafted)
		noWordsIn(t, res.Receipts[0], narrowD, c.d.Statement)
		// The words in force stay: the draft is a version above them.
		d := f.mem(zz, c.d.ID)
		if d.Version != 1 || d.Statement != c.d.Statement || d.Lifecycle != lifecycle.Kept || status(d) != "" {
			t.Errorf("decision = v%d %q %s", d.Version, d.Statement, d.Lifecycle)
		}
		h, err := f.l.GetMemoryHistory(ctx, f.scope(zz), c.d.ID.String())
		if err != nil || len(h.Versions) != 2 || h.Versions[0].Version != 2 || h.Versions[0].Statement != narrowD {
			t.Fatalf("versions: %v %+v", err, h.Versions)
		}
		if mode, beside := f.settleJob(c.d.ID, 2); mode != "settling" || beside != c.p.ID.String() {
			t.Errorf("job = %s beside %s", mode, beside)
		}
		// The judge reads the draft's words, and leaves the other side out.
		scope, _ := f.l.SpaceScope(ctx, c.sp)
		snap, err := f.l.JudgeSnapshot(ctx, scope, ledger.JudgeArgs{MemoryID: c.d.ID, SpaceID: c.sp, Version: 2, Mode: ledger.JudgeSettling, Beside: c.p.ID}, 0)
		if err != nil || !snap.Eligible || snap.Memory.Statement != narrowD || snap.Memory.Version != 2 {
			t.Fatalf("snapshot: %v %+v", err, snap)
		}
		if !slices.ContainsFunc(snap.Decisions, func(j ledger.JudgeCandidate) bool { return j.ID == c.x.ID }) ||
			slices.ContainsFunc(snap.Decisions, func(j ledger.JudgeCandidate) bool { return j.ID == c.p.ID || j.ID == c.d.ID }) {
			t.Errorf("decisions = %+v", snap.Decisions)
		}
		_, err = resolve(c, 1, "", narrowD)
		pending(t, err, c.d.Ref)
		f.settled(c.sp, c.d.ID, 2, nil)
		res, err = resolve(c, 1, "", narrowD)
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("after the verdict = %v %s", err, res.Outcome)
		}
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionEdited, ledger.ActionKept}) {
			t.Errorf("receipts = %v", got)
		}
		// The judged draft is adopted: no third version.
		if d := f.mem(zz, c.d.ID); d.Version != 2 || d.Statement != narrowD {
			t.Errorf("decision after = v%d %q", d.Version, d.Statement)
		}
		// The whole resolution is one undo: the words in force come back.
		if _, err := undo(f, person(zz), f.scope(zz), res.Receipts[0].ID); err != nil {
			t.Fatal(err)
		}
		if d := f.mem(zz, c.d.ID); d.Version != 1 || d.Statement != c.d.Statement {
			t.Errorf("decision after undo = v%d %q", d.Version, d.Statement)
		}
		stillOpen(t, c)
	})
	t.Run("the kept side's draft contradicts the third decision", func(t *testing.T) {
		c := f.withThirdDecision(zz, "d")
		res, err := resolve(c, 1, "", narrowD)
		held(t, res, err, ledger.ActionDrafted)
		v := f.settled(c.sp, c.d.ID, 2, c.x)
		// A draft isn't in force: the verdict flags nothing on the decision.
		if v.Receipts[0].Action != ledger.ActionJudged || v.Receipts[0].Source.Ref != c.x.Ref {
			t.Errorf("verdict receipt = %+v", v.Receipts[0])
		}
		if d := f.mem(zz, c.d.ID); len(d.Flags) != 0 || d.Version != 1 || hasLink(d, ledger.LinkConflictsWith, ledger.LinkOut, c.x.ID) {
			t.Errorf("decision = v%d %v %+v", d.Version, d.Flags, d.Links)
		}
		_, err = resolve(c, 1, "", narrowD)
		inConflict(t, err, c.d.Ref, c.x.Ref)
		stillOpen(t, c)
		// Other words that touch nothing else apply at once.
		res, err = resolve(c, 1, "", "Production deploys to Railway.")
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("plain words = %v %s", err, res.Outcome)
		}
		if d := f.mem(zz, c.d.ID); d.Version != 3 || d.Statement != "Production deploys to Railway." {
			t.Errorf("decision = v%d %q", d.Version, d.Statement)
		}
	})
	t.Run("both sides: the hold saves both, the plain one applies with it", func(t *testing.T) {
		c := f.withThirdDecision(zz, "e")
		res, err := resolve(c, 1, "Production runs on Fly.io in iad and ams.", narrowD)
		held(t, res, err, ledger.ActionEdited, ledger.ActionDrafted)
		_, err = resolve(c, 2, "Production runs on Fly.io in iad and ams.", narrowD)
		pending(t, err, c.d.Ref)
		f.settled(c.sp, c.d.ID, 2, nil)
		res, err = resolve(c, 2, "Production runs on Fly.io in iad and ams.", narrowD)
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("after the verdict = %v %s", err, res.Outcome)
		}
		if p, d := f.mem(zz, c.p.ID), f.mem(zz, c.d.ID); p.Lifecycle != lifecycle.Kept || p.Version != 2 || d.Version != 2 {
			t.Errorf("after: %s v%d / v%d", p.Lifecycle, p.Version, d.Version)
		}
	})
	t.Run("words that touch nothing else apply at once", func(t *testing.T) {
		c := f.withThirdDecision(zz, "f")
		res, err := resolve(c, 1, "Production runs on Fly.io in iad and ams.", "Staging runs on Railway.")
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("resolve = %v %s", err, res.Outcome)
		}
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionEdited, ledger.ActionEdited, ledger.ActionKept}) {
			t.Errorf("receipts = %v", got)
		}
		if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal' AND args->>'mode' = 'settling'
		                   AND args->>'memory_id' = ANY ($1)`, []string{c.p.ID.String(), c.d.ID.String()}); n != 0 {
			t.Errorf("%d settling jobs", n)
		}
	})
	t.Run("a judge that's down never blocks it", func(t *testing.T) {
		c := f.withThirdDecision(zz, "g")
		res, err := resolve(c, 1, narrowP, narrowD)
		held(t, res, err, ledger.ActionEdited, ledger.ActionDrafted)
		// JudgeGrace passes with no verdict on either (backdated by hand,
		// past the receipt trigger: the test's clock, not the record).
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE v2.memory_versions SET created_at = created_at - interval '1 minute' WHERE memory_id = ANY ($1)`,
			[]uuid.UUID{c.p.ID, c.d.ID}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		res, err = resolve(c, 2, narrowP, narrowD)
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("after the grace = %v %s", err, res.Outcome)
		}
	})

	// Every write above has its receipt in its space.
	for name, sql := range map[string]string{
		"memories": `SELECT count(*) FROM v2.memories m LEFT JOIN v2.receipts r ON r.id = m.last_receipt_id AND r.space_id = m.space_id
		             AND r.object_id = m.id WHERE r.id IS NULL`,
		"versions": `SELECT count(*) FROM v2.memory_versions v LEFT JOIN v2.receipts r ON r.id = v.receipt_id AND r.space_id = v.space_id
		             AND r.object_id = v.memory_id WHERE r.id IS NULL`,
		"verdicts": `SELECT count(*) FROM v2.judge_verdicts v LEFT JOIN v2.receipts r ON r.id = v.receipt_id AND r.space_id = v.space_id
		             AND r.object_id = v.memory_id WHERE r.id IS NULL`,
		"links": `SELECT count(*) FROM v2.memory_links l LEFT JOIN v2.receipts r ON r.id = l.receipt_id AND r.space_id = l.space_id
		          WHERE r.id IS NULL`,
	} {
		if n := f.count(sql); n != 0 {
			t.Errorf("%s: %d rows without their receipt", name, n)
		}
	}
}

// Drafts, settling verdicts and returns stay in their space (rule 13).
func TestRule11RowsStayInTheirSpace(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	mine := f.withThirdDecision(zz, "mine")
	theirs := f.space(jy, policy.SpaceProject, "theirs")
	res := f.apply(&ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: mine.p.Ref,
		Choice: ledger.ChooseBoth, OtherStatement: "Production deploys to Railway; previews run elsewhere."})
	if res.Policy.Code != policy.CodeJudgePending {
		t.Fatalf("hold = %s", res.Policy.Code)
	}
	f.settled(mine.sp, mine.d.ID, 2, mine.x)
	tenant := f.mem(zz, mine.d.ID).TenantID
	for _, c := range []struct {
		name string
		sql  string
	}{
		{"the draft", `SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND version = 2`},
		{"its verdict", `SELECT count(*) FROM v2.judge_verdicts WHERE memory_id = $1 AND mode = 'settling'`},
		{"its receipts", `SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND action IN ('drafted', 'judged')`},
	} {
		for _, s := range []struct {
			spaces []uuid.UUID
			want   int
		}{{[]uuid.UUID{mine.sp}, 1}, {[]uuid.UUID{theirs}, 0}, {nil, 0}} {
			if c.name == "its receipts" && s.want == 1 {
				s.want = 2
			}
			var n int
			if err := f.asV2(s.spaces, []uuid.UUID{tenant}, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, c.sql, mine.d.ID).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if n != s.want {
				t.Errorf("%s in scope %v: %d rows, want %d", c.name, s.spaces, n, s.want)
			}
		}
	}
	// Another space's scope can't snapshot the draft or settle the conflict.
	scope, _ := f.l.SpaceScope(ctx, theirs)
	if _, err := f.l.JudgeSnapshot(ctx, scope, ledger.JudgeArgs{MemoryID: mine.d.ID, SpaceID: mine.sp, Version: 2, Mode: ledger.JudgeSettling}, 0); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("snapshot across spaces: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), Memory: mine.p.ID.String(),
		Choice: ledger.ChooseBoth}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("resolve across spaces: %v", err)
	}
}
