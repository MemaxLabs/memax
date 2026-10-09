package ledger_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func decisionIn(space uuid.UUID, statement, area string) ledger.NewMemory {
	return ledger.NewMemory{SpaceID: space, Statement: statement, Section: ledger.SectionDecisions, Kind: ledger.KindDecision,
		Decision: &ledger.DecisionFields{Area: area}}
}

// judgeAs records a verdict as Memax (the judge).
func (f *fixture) judgeAs(space uuid.UUID, m *ledger.Memory, outcome ledger.VerdictOutcome, target *ledger.Memory, mode ledger.JudgeMode) ledger.Result {
	f.t.Helper()
	return f.judgeOn(f.l, space, m, outcome, target, mode)
}

// judgeLate records a verdict after the return window has passed: a
// Write agent's write the judge flags then stays kept, in conflict.
func (f *fixture) judgeLate(space uuid.UUID, m *ledger.Memory, outcome ledger.VerdictOutcome, target *ledger.Memory, mode ledger.JudgeMode) ledger.Result {
	f.t.Helper()
	late := ledger.New(f.pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), ledger.WithReturnWindow(0))
	return f.judgeOn(late, space, m, outcome, target, mode)
}

func (f *fixture) judgeOn(l *ledger.Ledger, space uuid.UUID, m *ledger.Memory, outcome ledger.VerdictOutcome, target *ledger.Memory, mode ledger.JudgeMode) ledger.Result {
	f.t.Helper()
	scope, err := l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	rel := map[ledger.VerdictOutcome]ledger.Relation{ledger.OutcomeFlagged: ledger.RelationContradicts,
		ledger.OutcomeFolded: ledger.RelationDuplicate, ledger.OutcomeSuppressed: ledger.RelationDuplicate,
		ledger.OutcomeLinked: ledger.RelationUpdates, ledger.OutcomeSuperseding: ledger.RelationUpdates,
		ledger.OutcomeNone: ledger.RelationNone}[outcome]
	stage := ledger.StageLLM
	if outcome == ledger.OutcomeFolded {
		stage = ledger.StageExact
	}
	cmd := &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: m.ID, Version: m.Version, Mode: mode, Outcome: outcome,
		Verdict: ledger.Verdict{Stage: stage, Relation: rel},
	}
	if target != nil {
		cmd.Target, cmd.Verdict.Related = target.ID, target.ID
	}
	res, err := l.Apply(context.Background(), cmd)
	if err != nil {
		f.t.Fatalf("record verdict: %v", err)
	}
	return res
}

// conflictPair is a kept decision and an agent's proposal flagged against it.
func (f *fixture) conflictPair(owner, space uuid.UUID) (proposal, decision *ledger.Memory) {
	f.t.Helper()
	d := f.apply(&ledger.Remember{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb),
		NewMemory: decisionIn(space, "Deploy the v2 API to Railway.", "deploy target")}).Memory
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(owner), policy.ViaMCP),
		NewMemory: decisionIn(space, "Deploy the v2 API to Fly.io in iad and ams.", "deploy target")}).Memory
	if res := f.judgeAs(space, p, ledger.OutcomeFlagged, d, ledger.JudgeProposal); res.Memory.State != lifecycle.MarkConflict {
		f.t.Fatalf("flag: %s", res.Memory.State)
	}
	return p, d
}

func (f *fixture) mem(user uuid.UUID, id uuid.UUID) *ledger.Memory {
	f.t.Helper()
	m, err := f.l.GetMemory(context.Background(), f.scope(user), id.String())
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func hasLink(m *ledger.Memory, kind ledger.LinkKind, dir string, other uuid.UUID) bool {
	return slices.ContainsFunc(m.Links, func(l ledger.Link) bool { return l.Kind == kind && l.Direction == dir && l.MemoryID == other })
}

func actions(rs []ledger.Receipt) []ledger.Action {
	out := make([]ledger.Action, len(rs))
	for i, r := range rs {
		out[i] = r.Action
	}
	return out
}

func status(m *ledger.Memory) string {
	if m.Decision == nil {
		return ""
	}
	return m.Decision.Status
}

func TestResolveConflictChoices(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	resolve := func(ref string, choice ledger.ConflictChoice, statement, other string) ledger.Result {
		return f.apply(&ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: ref, Choice: choice,
			Statement: statement, OtherStatement: other})
	}

	t.Run("keep this: the proposal replaces the decision", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "a")
		p, d := f.conflictPair(zz, sp)
		res := resolve(p.Ref, ledger.ChooseThis, "", "")
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionKept, ledger.ActionSuperseded}) {
			t.Errorf("receipts = %v", got)
		}
		if len(res.Memories) != 2 || res.Memories[0].ID != p.ID || res.Memories[1].ID != d.ID {
			t.Fatalf("memories = %+v", res.Memories)
		}
		np, nd := f.mem(zz, p.ID), f.mem(zz, d.ID)
		if np.State != lifecycle.MarkKept || len(np.Flags) != 0 || nd.Lifecycle != lifecycle.Kept || status(nd) != ledger.DecisionSuperseded {
			t.Errorf("after: %s %v / %s %s", np.State, np.Flags, nd.Lifecycle, status(nd))
		}
		if hasLink(np, ledger.LinkConflictsWith, ledger.LinkOut, d.ID) || !hasLink(np, ledger.LinkSupersedes, ledger.LinkOut, d.ID) {
			t.Errorf("links = %+v", np.Links)
		}
		if res.Receipts[1].Assurance != policy.AssuranceHumanWeb {
			t.Errorf("keep assurance = %q", res.Receipts[1].Assurance)
		}
	})
	t.Run("keep the other: the proposal is rejected", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "b")
		p, d := f.conflictPair(zz, sp)
		res := resolve(p.Ref, ledger.ChooseOther, "", "")
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionRejected}) {
			t.Errorf("receipts = %v", got)
		}
		if np, nd := f.mem(zz, p.ID), f.mem(zz, d.ID); np.Lifecycle != lifecycle.Rejected || status(nd) != "" || nd.Lifecycle != lifecycle.Kept {
			t.Errorf("after: %s / %s %s", np.Lifecycle, nd.Lifecycle, status(nd))
		}
	})
	t.Run("keep this, from the decision's side", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "c")
		p, d := f.conflictPair(zz, sp)
		resolve(d.Ref+"", ledger.ChooseThis, "", "")
		if np := f.mem(zz, p.ID); np.Lifecycle != lifecycle.Rejected {
			t.Errorf("proposal = %s", np.Lifecycle)
		}
	})
	t.Run("keep both, narrowed", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "d")
		p, d := f.conflictPair(zz, sp)
		res := resolve(p.Ref, ledger.ChooseBoth, "Production runs on Fly.io in iad and ams.", "Preview environments run on Railway.")
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionEdited, ledger.ActionEdited, ledger.ActionKept}) {
			t.Errorf("receipts = %v", got)
		}
		np, nd := f.mem(zz, p.ID), f.mem(zz, d.ID)
		if np.Lifecycle != lifecycle.Kept || np.Version != 2 || np.Statement != "Production runs on Fly.io in iad and ams." ||
			nd.Version != 2 || nd.Statement != "Preview environments run on Railway." || status(nd) != "" {
			t.Errorf("after: %s v%d %q / v%d %q %s", np.Lifecycle, np.Version, np.Statement, nd.Version, nd.Statement, status(nd))
		}
	})
	t.Run("leave it open", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "e")
		p, d := f.conflictPair(zz, sp)
		resolve(p.Ref, ledger.ChooseOpen, "", "")
		np, nd := f.mem(zz, p.ID), f.mem(zz, d.ID)
		if np.Lifecycle != lifecycle.Kept || np.Section != ledger.SectionOpenQuestion || status(np) != ledger.DecisionOpen ||
			nd.Section != ledger.SectionOpenQuestion || status(nd) != ledger.DecisionOpen {
			t.Errorf("after: %s %s %s / %s %s", np.Lifecycle, np.Section, status(np), nd.Section, status(nd))
		}
	})
	t.Run("a kept fact that loses fades", func(t *testing.T) {
		sp := f.space(zz, policy.SpaceProject, "f")
		d := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
			NewMemory: decisionIn(sp, "Deploy the v2 API to Railway.", "deploy target")}).Memory
		k := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP),
			NewMemory: fact(sp, "Previews build on Fly.io machines.")}).Memory
		if k.Lifecycle != lifecycle.Kept {
			t.Fatalf("write = %s", k.Lifecycle)
		}
		// Flagged after the return window: it stays kept, in conflict.
		f.judgeLate(sp, k, ledger.OutcomeFlagged, d, ledger.JudgeKept)
		res := resolve(d.Ref, ledger.ChooseThis, "", "")
		if got := actions(res.Receipts); !slices.Equal(got, []ledger.Action{ledger.ActionResolved, ledger.ActionFaded}) {
			t.Errorf("receipts = %v", got)
		}
		if nk := f.mem(zz, k.ID); nk.Lifecycle != lifecycle.Faded || !hasLink(nk, ledger.LinkSupersedes, ledger.LinkIn, d.ID) {
			t.Errorf("fact = %s %+v", nk.Lifecycle, nk.Links)
		}
	})
}

func TestResolveConflictRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(sp, jy, "viewer")
	p, d := f.conflictPair(zz, sp)
	for _, c := range []struct {
		name  string
		actor ledger.Actor
		scope ledger.Scope
		code  string
	}{
		{"an agent", agentFor(policy.AutonomyWrite), f.scope(zz), policy.CodePersonMustReview},
		{"a viewer", person(jy), f.scope(jy), policy.CodeViewer},
	} {
		res, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(c.actor, c.scope, policy.ViaWeb), Memory: p.Ref, Choice: ledger.ChooseThis})
		if err != nil || res.Outcome != ledger.OutcomeRefused || res.Policy.Code != c.code {
			t.Errorf("%s: %v %s %s", c.name, err, res.Outcome, res.Policy.Code)
		}
	}
	other := f.remember(zz, sp, "Not in conflict.")
	_, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: other.Ref, Choice: ledger.ChooseThis})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("no conflict: %v", err)
	}
	// Two conflicts on one decision: say which.
	q := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: decisionIn(sp, "Deploy the v2 API to Render.", "deploy target")}).Memory
	f.judgeAs(sp, q, ledger.OutcomeFlagged, d, ledger.JudgeProposal)
	_, err = f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: d.Ref, Choice: ledger.ChooseThis})
	var ve *ledger.ValidationError
	if !errors.As(err, &ve) || ve.Field != "other" {
		t.Errorf("ambiguous: %v", err)
	}
	// With other named, it settles that pair only.
	f.apply(&ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: d.Ref, Other: q.Ref, Choice: ledger.ChooseThis})
	if nq, np := f.mem(zz, q.ID), f.mem(zz, p.ID); nq.Lifecycle != lifecycle.Rejected || np.State != lifecycle.MarkConflict {
		t.Errorf("after: %s / %s", nq.Lifecycle, np.State)
	}
	// Team space: decisions need a person on the web; the CLI is refused.
	team := f.space(zz, policy.SpaceTeam, "team")
	tp, _ := f.conflictPair(zz, team)
	res, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Memory: tp.ID.String(), Choice: ledger.ChooseThis})
	if err != nil || res.Policy.Code != policy.CodeDecisionNeedsWeb {
		t.Errorf("team via cli: %v %s", err, res.Policy.Code)
	}
}

func TestGetConflict(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(sp, jy, "viewer")
	p, d := f.conflictPair(zz, sp)
	v, err := f.l.GetConflict(ctx, f.scope(zz), person(zz), policy.ViaWeb, p.Ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Memory.ID != p.ID || v.Other.ID != d.ID || v.FlaggedRef != p.Ref || v.DecisionRef != d.Ref || len(v.Options) != 4 || len(v.Receipts) == 0 {
		t.Fatalf("view = %+v", v)
	}
	want := map[ledger.ConflictChoice][]ledger.ConflictEffect{
		ledger.ChooseThis:  {{Ref: p.Ref, Change: ledger.EffectKept}, {Ref: d.Ref, Change: ledger.EffectSuperseded}},
		ledger.ChooseOther: {{Ref: p.Ref, Change: ledger.EffectRejected}, {Ref: d.Ref, Change: ledger.EffectStays}},
		ledger.ChooseBoth:  {{Ref: p.Ref, Change: ledger.EffectKept}, {Ref: d.Ref, Change: ledger.EffectStays}},
		ledger.ChooseOpen:  {{Ref: p.Ref, Change: ledger.EffectOpen}, {Ref: d.Ref, Change: ledger.EffectOpen}},
	}
	for _, o := range v.Options {
		if !slices.Equal(o.Effects, want[o.Choice]) || !o.Allowed {
			t.Errorf("%s: %+v allowed %v", o.Choice, o.Effects, o.Allowed)
		}
	}
	// A viewer sees the options, not allowed, with the reason.
	vv, err := f.l.GetConflict(ctx, f.scope(jy), person(jy), policy.ViaWeb, d.Ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if vv.Options[0].Allowed || vv.Options[0].Policy == nil || vv.Options[0].Policy.Code != policy.CodeViewer {
		t.Errorf("viewer option = %+v", vv.Options[0])
	}
	// Other spaces don't see it.
	stranger := f.user("x")
	f.space(stranger, policy.SpaceProject, "x")
	if _, err := f.l.GetConflict(ctx, f.scope(stranger), person(stranger), policy.ViaWeb, p.ID.String(), ""); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
}

func undo(f *fixture, actor ledger.Actor, scope ledger.Scope, receipt uuid.UUID) (ledger.Result, error) {
	return f.l.Apply(context.Background(), &ledger.Undo{Meta: meta(actor, scope, policy.ViaWeb), Receipt: receipt})
}

func TestUndoRestoresEachDecision(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	f.brief(zz, sp, 0, nil)
	tg := f.target(zz, sp, ledger.TargetAgentsMD)
	propose := func(s string) *ledger.Memory {
		return f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP), NewMemory: fact(sp, s)}).Memory
	}
	me := func() ledger.Meta { return meta(person(zz), f.scope(zz), policy.ViaWeb) }
	undone := func(rc uuid.UUID) ledger.Result {
		t.Helper()
		res, err := undo(f, person(zz), f.scope(zz), rc)
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			t.Fatalf("undo: %v %s %s", err, res.Outcome, res.Policy.Message)
		}
		for _, r := range res.Receipts {
			if r.Action != ledger.ActionUndid || r.Source == nil || r.Source.Kind != "receipt" {
				t.Errorf("undo receipt = %+v", r)
			}
		}
		return res
	}

	t.Run("keep", func(t *testing.T) {
		p := propose("Workers must be idempotent.")
		gen := f.get(zz, tg.ID).DirtyGen
		k := f.apply(&ledger.Keep{Meta: me(), Memory: p.Ref})
		res := undone(k.Receipts[0].ID)
		if res.Receipts[0].Source.Ref != k.Receipts[0].ID.String() {
			t.Errorf("undid source = %+v", res.Receipts[0].Source)
		}
		if m := f.mem(zz, p.ID); m.Lifecycle != lifecycle.Proposed || m.Version != 1 {
			t.Errorf("after: %s v%d", m.Lifecycle, m.Version)
		}
		// The kept set changed twice: Keep and its undo both recompile.
		if g := f.get(zz, tg.ID).DirtyGen; g != gen+2 {
			t.Errorf("dirty_gen %d → %d", gen, g)
		}
		if f.jobs(tg.ID) == 0 {
			t.Error("no compile job after the undo")
		}
		// Kept before the judge got to it: it is judged once back in Review.
		if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal' AND args->>'memory_id' = $1`, p.ID.String()); n != 2 {
			t.Errorf("%d judge jobs, want the original and one after the undo", n)
		}
	})
	t.Run("reject", func(t *testing.T) {
		p := propose("Use tabs.")
		r := f.apply(&ledger.Reject{Meta: me(), Memory: p.Ref})
		undone(r.Receipts[0].ID)
		if m := f.mem(zz, p.ID); m.Lifecycle != lifecycle.Proposed {
			t.Errorf("after: %s", m.Lifecycle)
		}
	})
	t.Run("edit then keep", func(t *testing.T) {
		p := propose("Pin pnpm with corepack.")
		e := f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "Pin pnpm 10 with corepack.", Keep: true})
		if len(e.Receipts) != 2 {
			t.Fatalf("edit receipts = %v", actions(e.Receipts))
		}
		undone(e.Receipts[1].ID) // any of the command's receipts addresses it
		m := f.mem(zz, p.ID)
		if m.Lifecycle != lifecycle.Proposed || m.Version != 1 || m.Statement != "Pin pnpm with corepack." {
			t.Errorf("after: %s v%d %q", m.Lifecycle, m.Version, m.Statement)
		}
		// The undone version stays in the history; the next edit is v3.
		h, err := f.l.GetMemoryHistory(context.Background(), f.scope(zz), p.ID.String())
		if err != nil || len(h.Versions) != 2 {
			t.Fatalf("history: %v %d", err, len(h.Versions))
		}
		e2 := f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "Pin pnpm 10.1 with corepack."})
		if e2.Memory.Version != 3 {
			t.Errorf("next version = %d", e2.Memory.Version)
		}
	})
	t.Run("edit of a kept memory", func(t *testing.T) {
		k := f.remember(zz, sp, "API errors are problem+json.")
		e := f.apply(&ledger.Edit{Meta: me(), Memory: k.Ref, ExpectedVersion: 1, Statement: "API errors are RFC 9457 problem+json."})
		undone(e.Receipts[0].ID)
		if m := f.mem(zz, k.ID); m.Statement != "API errors are problem+json." || m.Lifecycle != lifecycle.Kept {
			t.Errorf("after: %q %s", m.Statement, m.Lifecycle)
		}
	})
	t.Run("a judge fold", func(t *testing.T) {
		k := f.remember(zz, sp, "River, not Temporal.")
		p := propose("river, not temporal")
		fold := f.judgeAs(sp, p, ledger.OutcomeFolded, k, ledger.JudgeProposal)
		if fold.Memory.Lifecycle != lifecycle.Merged {
			t.Fatalf("fold = %s", fold.Memory.Lifecycle)
		}
		res := undone(fold.Receipts[0].ID)
		m := f.mem(zz, p.ID)
		if m.Lifecycle != lifecycle.Proposed || hasLink(m, ledger.LinkMergedInto, ledger.LinkOut, k.ID) || len(res.Memories) != 1 {
			t.Errorf("after: %s %+v", m.Lifecycle, m.Links)
		}
		// Judged again, forced, without stage 0's folds.
		var force, skip bool
		if err := f.pool.QueryRow(context.Background(), `SELECT (args->>'force')::bool, (args->>'skip_fold')::bool FROM river_job
		        WHERE kind = 'judge_proposal' AND args->>'memory_id' = $1 AND (args->>'round')::int = 1`, p.ID.String()).Scan(&force, &skip); err != nil || !force || !skip {
			t.Errorf("re-judge job: force %v skip %v %v", force, skip, err)
		}
	})
	t.Run("a conflict resolution", func(t *testing.T) {
		p, d := f.conflictPair(zz, sp)
		r := f.apply(&ledger.ResolveConflict{Meta: me(), Memory: p.Ref, Choice: ledger.ChooseThis})
		res := undone(r.Receipts[0].ID)
		if len(res.Memories) != 2 {
			t.Errorf("memories = %d", len(res.Memories))
		}
		np, nd := f.mem(zz, p.ID), f.mem(zz, d.ID)
		if np.State != lifecycle.MarkConflict || np.Lifecycle != lifecycle.Proposed || status(nd) != "" || nd.Lifecycle != lifecycle.Kept {
			t.Errorf("after: %s / %s %s", np.State, nd.Lifecycle, status(nd))
		}
		if !hasLink(np, ledger.LinkConflictsWith, ledger.LinkOut, d.ID) || hasLink(np, ledger.LinkSupersedes, ledger.LinkOut, d.ID) {
			t.Errorf("links = %+v", np.Links)
		}
		// And it can be settled again.
		f.apply(&ledger.ResolveConflict{Meta: me(), Memory: p.Ref, Choice: ledger.ChooseOther})
		if np := f.mem(zz, p.ID); np.Lifecycle != lifecycle.Rejected {
			t.Errorf("settled again: %s", np.Lifecycle)
		}
	})
	t.Run("a keep that superseded a decision", func(t *testing.T) {
		d := f.apply(&ledger.Remember{Meta: me(), NewMemory: decisionIn(sp, "Use npm.", "package manager")}).Memory
		p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
			NewMemory: decisionIn(sp, "We switched from npm to pnpm.", "package manager")}).Memory
		f.judgeAs(sp, p, ledger.OutcomeSuperseding, d, ledger.JudgeProposal)
		k := f.apply(&ledger.Keep{Meta: me(), Memory: p.Ref})
		if status(f.mem(zz, d.ID)) != ledger.DecisionSuperseded {
			t.Fatal("not superseded")
		}
		undone(k.Receipts[0].ID)
		if nd := f.mem(zz, d.ID); status(nd) != "" || nd.Lifecycle != lifecycle.Kept {
			t.Errorf("decision after undo: %s %s", nd.Lifecycle, status(nd))
		}
	})
}

func TestUndoRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(sp, jy, "member")
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

	// Someone else's decision.
	p := propose("One.")
	k := f.apply(&ledger.Keep{Meta: me(), Memory: p.Ref})
	if res, err := undo(f, person(jy), f.scope(jy), k.Receipts[0].ID); err != nil || res.Policy.Code != policy.CodeUndoByDecider {
		t.Errorf("not yours: %v %s", err, res.Policy.Code)
	}
	if res, err := undo(f, agentFor(policy.AutonomyWrite), f.scope(zz), k.Receipts[0].ID); err != nil || res.Policy.Code != policy.CodePersonMustReview {
		t.Errorf("an agent: %v %s", err, res.Policy.Code)
	}
	// A later change depends on it.
	f.apply(&ledger.Edit{Meta: me(), Memory: p.Ref, ExpectedVersion: 1, Statement: "One, edited."})
	if _, err := undo(f, person(zz), f.scope(zz), k.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
		t.Errorf("later change: %v", err)
	}
	// Already undone.
	q := propose("Two.")
	rj := f.apply(&ledger.Reject{Meta: me(), Memory: q.Ref})
	if _, err := undo(f, person(zz), f.scope(zz), rj.Receipts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := undo(f, person(zz), f.scope(zz), rj.Receipts[0].ID); refusal(err) != ledger.UndoAlreadyUndone {
		t.Errorf("twice: %v", err)
	}
	// Not undoable: a proposal's receipt, an undo's receipt.
	if _, err := undo(f, person(zz), f.scope(zz), q.CreatedReceiptID); refusal(err) != ledger.UndoNotUndoable {
		t.Errorf("a proposal: %v", err)
	}
	// The Brief cites what the undo would take out of the kept set.
	r := propose("Three.")
	k3 := f.apply(&ledger.Keep{Meta: me(), Memory: r.Ref})
	f.brief(zz, sp, 0, demoSections(r.Ref))
	_, err := undo(f, person(zz), f.scope(zz), k3.Receipts[0].ID)
	var ue *ledger.UndoError
	if !errors.As(err, &ue) || ue.Reason != ledger.UndoLaterChanges || ue.Ref[:2] != "B-" {
		t.Errorf("brief cites it: %v", err)
	}
	// Too late.
	short := ledger.New(f.pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), ledger.WithUndoWindows(time.Millisecond, time.Millisecond))
	s := propose("Four.")
	k4, err := short.Apply(ctx, &ledger.Keep{Meta: me(), Memory: s.Ref})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := undo(f, person(zz), f.scope(zz), k4.Receipts[0].ID); refusal(err) != ledger.UndoWindowPassed {
		t.Errorf("too late: %v", err)
	}
	// Another space's receipt is not found.
	stranger := f.user("x")
	f.space(stranger, policy.SpaceProject, "x")
	if _, err := undo(f, person(stranger), f.scope(stranger), k4.Receipts[0].ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
	// Any member may undo a judge fold.
	kept := f.remember(zz, sp, "Five.")
	dup := propose("five")
	fold := f.judgeAs(sp, dup, ledger.OutcomeFolded, kept, ledger.JudgeProposal)
	if res, err := undo(f, person(jy), f.scope(jy), fold.Receipts[0].ID); err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Errorf("member unfolds: %v %s %s", err, res.Outcome, res.Policy.Code)
	}
}

// Rule 13 for the new tables: verdicts and undo entries are visible only
// in their own space's scope.
func TestJudgeAndUndoRowsStayInTheirSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	a := f.space(zz, policy.SpaceProject, "a")
	b := f.space(jy, policy.SpaceProject, "b")
	for _, s := range []struct{ owner, space uuid.UUID }{{zz, a}, {jy, b}} {
		p, _ := f.conflictPair(s.owner, s.space) // a verdict
		f.apply(&ledger.ResolveConflict{Meta: meta(person(s.owner), f.scope(s.owner), policy.ViaWeb), Memory: p.ID.String(),
			Choice: ledger.ChooseOther}) // an undo entry
	}
	ta := f.mem(zz, f.remember(zz, a, "x").ID).TenantID
	tb := f.mem(jy, f.remember(jy, b, "y").ID).TenantID
	for _, tbl := range []string{"v2.judge_verdicts", "v2.undo_entries"} {
		own := func(space uuid.UUID) int { return f.count(`SELECT count(*) FROM `+tbl+` WHERE space_id = $1`, space) }
		if own(a) == 0 || own(b) == 0 {
			t.Fatalf("%s: the fixture gives a space no rows", tbl)
		}
		for _, c := range []struct {
			spaces, tenants []uuid.UUID
			want            int
		}{{nil, nil, 0}, {[]uuid.UUID{a}, []uuid.UUID{ta}, own(a)}, {[]uuid.UUID{b}, []uuid.UUID{tb}, own(b)}} {
			var n int
			if err := f.asV2(c.spaces, c.tenants, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if n != c.want {
				t.Errorf("%s in scope %v: %d rows, want %d", tbl, c.spaces, n, c.want)
			}
		}
	}
	// Undo through another space's scope finds nothing.
	var rc uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT receipt_id FROM v2.undo_entries WHERE space_id = $1`, b).Scan(&rc); err != nil {
		t.Fatal(err)
	}
	if _, err := undo(f, person(zz), f.scope(zz), rc); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("undo across spaces: %v", err)
	}
}

// An undo replays idempotently, and every write it made has its receipt.
func TestUndoIsIdempotentAndReceipted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	p, _ := f.conflictPair(zz, sp)
	r := f.apply(&ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref, Choice: ledger.ChooseThis})
	m := meta(person(zz), f.scope(zz), policy.ViaWeb)
	first, err := f.l.Apply(ctx, &ledger.Undo{Meta: m, Receipt: r.Receipts[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.l.Apply(ctx, &ledger.Undo{Meta: m, Receipt: r.Receipts[2].ID})
	if err != nil || !again.Replayed || len(again.Memories) != 2 || len(again.Receipts) != len(first.Receipts) {
		t.Errorf("replay = %v %v %d", err, again.Replayed, len(again.Memories))
	}
	for name, sql := range map[string]string{
		"undo entries": `SELECT count(*) FROM v2.undo_entries u LEFT JOIN v2.receipts r ON r.id = u.last_receipt_id AND r.space_id = u.space_id
		                 WHERE r.id IS NULL`,
		"ended links": `SELECT count(*) FROM v2.memory_links l LEFT JOIN v2.receipts r ON r.id = l.ended_receipt_id AND r.space_id = l.space_id
		                WHERE l.ended_receipt_id IS NOT NULL AND r.id IS NULL`,
		"undid receipts": `SELECT count(*) FROM v2.receipts u WHERE u.action = 'undid'
		                   AND NOT EXISTS (SELECT 1 FROM v2.receipts o WHERE o.id::text = u.source->>'ref')`,
	} {
		if n := f.count(sql); n != 0 {
			t.Errorf("%s: %d rows without their receipt", name, n)
		}
	}
	// The database refuses kept → proposed without an undo receipt.
	k := f.remember(zz, sp, "Kept for good.")
	err = f.asV2([]uuid.UUID{sp}, []uuid.UUID{k.TenantID}, func(tx pgx.Tx) error {
		rid, err := insertReceiptSQL(tx, k, k.ID, 2)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'proposed', last_receipt_id = $2, stream_version = 2 WHERE id = $1`, k.ID, rid)
		return err
	})
	if sqlstate(err) != "MXL01" {
		t.Errorf("kept → proposed without an undo: %v", err)
	}
}
