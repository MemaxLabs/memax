package ledger_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// What carries a forgotten memory's words goes with it, and only when the
// person names it: a kept Ask answer citing it (and one citing that), a
// proposal the judge folded into it, and an agent's proposal that would
// change it.
func TestForgetCarriesWhatHoldsItsWords(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	m := f.remember(zz, space, "Background jobs run on River, Postgres-backed. We do not use Temporal.")

	cite := func(statement string, refs ...string) *ledger.Memory {
		nm := fact(space, statement)
		for _, r := range refs {
			nm.Sources = append(nm.Sources, ledger.SourceInput{Kind: ledger.SourceMemory, Ref: r})
		}
		return f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}).Memory
	}
	answer := cite("Jobs run on River, not Temporal.", m.Ref)
	answer2 := cite("River is the queue, so retries are River's.", answer.Ref)
	dup := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
		NewMemory: fact(space, "Background jobs run on River, Postgres-backed. We do not use Temporal")}).Memory
	if res := f.judgeAs(space, dup, ledger.OutcomeFolded, m, ledger.JudgeProposal); res.Memory.Lifecycle != lifecycle.Merged {
		t.Fatalf("fold: %s", res.Memory.Lifecycle)
	}
	upd := f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), scope, policy.ViaMCP), Memory: m.Ref, ExpectedVersion: 1,
		Statement: "Background jobs run on River. We do not use Temporal or Sidekiq."})
	if upd.Outcome != ledger.OutcomeProposed || upd.Memory.ID == m.ID {
		t.Fatalf("agent edit: %s %s", upd.Outcome, upd.Memory.Ref)
	}
	keep := f.remember(zz, space, "Unrelated: pnpm is the package manager.")

	_, err := f.l.Apply(ctx, forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1))
	var ce *ledger.ForgetCarriesError
	if !errors.As(err, &ce) || !errors.Is(err, ledger.ErrForgetCarries) {
		t.Fatalf("forget without carries: %v, want ForgetCarriesError", err)
	}
	got := map[string]string{}
	for _, c := range ce.Carries {
		got[c.Ref] = c.Reason + "<" + c.With
	}
	want := map[string]string{
		answer.Ref:     ledger.CarryCites + "<" + m.Ref,
		answer2.Ref:    ledger.CarryCites + "<" + answer.Ref,
		dup.Ref:        ledger.CarryFolded + "<" + m.Ref,
		upd.Memory.Ref: ledger.CarryUpdates + "<" + m.Ref,
	}
	if len(got) != len(want) {
		t.Fatalf("carries = %v, want %v", got, want)
	}
	for r, w := range want {
		if got[r] != w {
			t.Errorf("carries[%s] = %q, want %q", r, got[r], w)
		}
	}
	// Naming some but not all is refused too.
	if _, err := f.l.Apply(ctx, forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1, answer.Ref)); !errors.Is(err, ledger.ErrForgetCarries) {
		t.Errorf("forget naming one: %v", err)
	}
	res := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1, answer.Ref, answer2.Ref, dup.Ref, upd.Memory.Ref))
	if len(res.Memories) != 4 {
		t.Fatalf("forgotten with it: %d", len(res.Memories))
	}
	for _, id := range []uuid.UUID{m.ID, answer.ID, answer2.ID, dup.ID, upd.Memory.ID} {
		if n := f.count(`SELECT count(*) FROM v2.memories WHERE id = $1 AND lifecycle = 'forgotten'`, id); n != 1 {
			t.Errorf("%s isn't forgotten", id)
		}
		if n := f.count(`SELECT count(*) FROM v2.tombstones WHERE object_id = $1 AND op_id = $2`, id, res.Tombstone.ID); n != 1 {
			t.Errorf("%s has no tombstone in the op", id)
		}
	}
	if k := f.mem(zz, keep.ID); k.Lifecycle != lifecycle.Kept || k.Statement == "" {
		t.Errorf("an unrelated memory changed: %s", k.Lifecycle)
	}
	slices.Sort(res.Tombstone.With)
	if len(res.Tombstone.With) != 4 {
		t.Errorf("with = %v", res.Tombstone.With)
	}
	ats, err := f.l.GetTombstone(ctx, scope, answer.Ref)
	if err != nil || ats.Carried != ledger.CarryCites || ats.Primary != m.Ref {
		t.Errorf("the answer's tombstone: %+v, %v", ats, err)
	}
	// The receipts of the carried ones name what they went with.
	rc := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND action = 'forgot' AND source->>'ref' = $2`, answer2.ID, answer.Ref)
	if rc != 1 {
		t.Errorf("answer2's forgot receipt doesn't name %s", answer.Ref)
	}
}

// Only a person who may keep and forget in the space forgets; nobody
// forgets another space's memory; and in a team space, forgetting a
// decision needs a person on the web (D15).
func TestForgetPolicyAndIsolation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy, vi, other := f.user("zz"), f.user("jy"), f.user("vi"), f.user("other")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "contributor")
	f.join(space, vi, "viewer")
	m := f.remember(zz, space, "Pin Node 24 in CI.")
	try := func(actor ledger.Actor, scope ledger.Scope, via policy.Via) ledger.Result {
		t.Helper()
		res, err := f.l.Apply(ctx, forgetCmd(actor, scope, via, m.Ref, 1))
		if err != nil {
			t.Fatalf("forget: %v", err)
		}
		return res
	}
	refusedWith(t, try(person(vi), f.scope(vi), policy.ViaWeb), policy.CodeForgetNotAllowed)
	refusedWith(t, try(person(jy), f.scope(jy), policy.ViaWeb), policy.CodeForgetNotAllowed)
	refusedWith(t, try(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), policy.CodePersonMustForget)
	key := person(zz)
	key.Credential = policy.CredentialAPIKey
	refusedWith(t, try(key, f.scope(zz), policy.ViaAPI), policy.CodeKeyCannotForget)
	// Another person's scope doesn't reach it: not found, by ref or by id.
	osp := f.space(other, policy.SpaceProject, "theirs")
	_ = osp
	for _, ref := range []string{m.Ref, m.ID.String()} {
		if _, err := f.l.Apply(ctx, forgetCmd(person(other), f.scope(other), policy.ViaWeb, ref, 1)); !errors.Is(err, ledger.ErrNotFound) {
			t.Errorf("forget %s from another space: %v, want not found", ref, err)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.memories WHERE id = $1 AND lifecycle = 'kept'`, m.ID); n != 1 {
		t.Fatal("a refused forget changed the memory")
	}

	// D15 in a team space.
	team := f.space(zz, policy.SpaceTeam, "memax-team")
	d := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		NewMemory: decisionIn(team, "Deploy the v2 API to Fly.io.", "deploy")}).Memory
	tscope := f.scope(zz).Narrow(team)
	res, err := f.l.Apply(ctx, forgetCmd(person(zz), tscope, policy.ViaCLI, d.Ref, 1))
	if err != nil {
		t.Fatal(err)
	}
	refusedWith(t, res, policy.CodeDecisionNeedsWeb)
	if res := f.apply(forgetCmd(person(zz), tscope, policy.ViaWeb, d.Ref, 1)); res.Outcome != ledger.OutcomeApplied {
		t.Errorf("forget a team decision on the web: %s", res.Outcome)
	}
}

// Forget purges the words of the judge's verdicts on both sides of the
// pair, ends the conflict (the other side loses its flag), and purges a
// gate's words when the forgotten memory is the decision it became.
func TestForgetReachesVerdictsConflictsAndGates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	p, d := f.conflictPair(zz, space)
	sscope, err := f.l.SpaceScope(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	// A verdict with the model's words about the pair (round 1).
	f.apply(&ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: sscope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: p.ID, Version: p.Version, Mode: ledger.JudgeProposal, Round: 1, Force: true, Outcome: ledger.OutcomeNone,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationContradicts, Related: d.ID,
			Rationale: "It contradicts 'Deploy the v2 API to Railway.'", MergedStatement: "Deploy the v2 API to Railway or Fly.io.",
			Model:      "deepseek-v4.1-flash",
			Candidates: []ledger.VerdictCandidate{{MemoryID: d.ID, Ref: d.Ref, Sets: []string{"keyed"}, Relation: ledger.RelationContradicts}}},
	})
	if n := f.count(`SELECT count(*) FROM v2.judge_verdicts WHERE memory_id = $1 AND rationale IS NOT NULL`, p.ID); n != 1 {
		t.Fatalf("seeded %d verdicts with words", n)
	}
	res := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, d.Ref, 1))
	if n := f.count(`SELECT count(*) FROM v2.judge_verdicts WHERE (memory_id = $1 OR related_memory_id = $1) AND (rationale IS NOT NULL OR merged_statement IS NOT NULL)`, d.ID); n != 0 {
		t.Errorf("%d verdicts still quote it", n)
	}
	np := f.mem(zz, p.ID)
	if np.Lifecycle != lifecycle.Proposed || np.Flags.Has(lifecycle.Conflict) || hasLink(np, ledger.LinkConflictsWith, ledger.LinkOut, d.ID) {
		t.Errorf("the other side: %s %v links %+v", np.Lifecycle, np.Flags, np.Links)
	}
	if !slices.Contains(actions(res.Receipts), ledger.ActionResolved) {
		t.Errorf("no resolved receipt for the other side: %v", actions(res.Receipts))
	}
	if res.Tombstone.Gone.Verdicts < 1 || res.Tombstone.Gone.ModelVerdicts < 1 {
		t.Errorf("gone = %+v", res.Tombstone.Gone)
	}

	// A gate's words go with the decision it became.
	conn := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "codex", nil), ledger.AgentCodex, at(space, policy.AutonomyPropose))
	actor, ascope := f.agentActor(zz, conn, policy.AutonomyPropose)
	g := f.asked(actor, ascope, space)
	ans := answer(person(zz), scope, policy.ViaWeb, g.Ref, 1)
	ans.Reason = "Fly.io, because the workers already run there."
	decision := f.apply(ans).Memory
	f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, decision.Ref, 1))
	if n := f.count(`SELECT count(*) FROM v2.decision_gates WHERE id = $1 AND (question IS NOT NULL OR context IS NOT NULL OR options IS NOT NULL)`, g.ID); n != 0 {
		t.Error("the gate keeps its words")
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND action = 'forgot'`, g.ID); n != 1 {
		t.Error("the gate has no forgot receipt")
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND reason IS NOT NULL`, g.ID); n != 0 {
		t.Error("the gate's receipts keep a reason")
	}
	gate, err := f.l.GetGate(ctx, scope, g.Ref)
	if err != nil || gate.Question != "" || len(gate.Options) != 0 {
		t.Errorf("gate after: %+v %v", gate, err)
	}
}

// The Brief loses it: a new B- without its line or the prose that cited
// it, and older versions keep that prose's citations, not its words.
func TestForgetTakesItOutOfTheBrief(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	m := f.remember(zz, space, "Jiahao is away from Oct 12 to Oct 26.")
	o := f.remember(zz, space, "Route reviews to Ziyang.")
	sections := []ledger.BriefSection{{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{
		{Ref: m.Ref}, {Ref: o.Ref},
		{Text: "While Jiahao is away, Ziyang reviews.", Cites: []string{m.Ref, o.Ref}},
		{Text: "Ziyang owns the review queue.", Cites: []string{o.Ref}},
	}}}
	b1 := f.brief(zz, space, 0, sections)
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	f.compiled(tg, "- Jiahao is away [M]\n", m.Ref, o.Ref)
	before := f.get(zz, tg.ID).DirtyGen

	res := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1))
	b, err := f.l.GetBrief(ctx, scope, space)
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != b1.Version+1 {
		t.Fatalf("Brief version %d, want %d", b.Version, b1.Version+1)
	}
	items := b.Sections[0].Items
	if len(items) != 2 || items[0].Ref != o.Ref || items[1].Text != "Ziyang owns the review queue." {
		t.Errorf("new version = %+v", items)
	}
	page, err := f.l.ListBriefVersions(ctx, scope, ledger.BriefVersionQuery{SpaceID: space})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range page.Versions {
		for _, s := range v.Sections {
			for _, it := range s.Items {
				if strings.Contains(it.Text, "Jiahao") {
					t.Errorf("B version %d keeps the words: %q", v.Version, it.Text)
				}
				if it.Forgotten && (it.Text != "" || len(it.Cites) == 0) {
					t.Errorf("a forgotten line: %+v", it)
				}
			}
		}
	}
	if f.get(zz, tg.ID).DirtyGen <= before {
		t.Error("the targets weren't dirtied")
	}
	steps := 0
	for _, s := range res.Tombstone.Steps {
		if s.Kind == ledger.StepTarget {
			steps++
			if s.Status != ledger.StepWaiting || s.Target.Label != "AGENTS.md" {
				t.Errorf("target step = %+v", s)
			}
		}
	}
	if steps != 1 || res.Tombstone.Gone.Files != 1 {
		t.Errorf("%d target steps, %d files", steps, res.Tombstone.Gone.Files)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'forget_propagate' AND args->>'op_id' = $1`, res.Tombstone.ID.String()); n != 1 {
		t.Errorf("%d propagation jobs queued", n)
	}
}

// Every agent that read it is told, once, on its next response; so is
// every agent connected to the space.
func TestForgetTellsEveryAgentOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	m := f.remember(zz, space, "The staging database is memax-staging-2.")
	cc := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "claude-code", nil), ledger.AgentClaudeCode, at(space, policy.AutonomyPropose))
	cx := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "codex", nil), ledger.AgentCodex, at(space, policy.AutonomyRead))
	f.record(readOf(m, cc.ID, zz, "claude-code", ledger.ReadRecall, time.Now(), m.ID))
	res := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1))
	if n := f.count(`SELECT count(*) FROM v2.agent_notices WHERE op_id = $1`, res.Tombstone.ID); n != 2 {
		t.Fatalf("%d notices, want 2 (the reader and the connected one)", n)
	}
	readIt := f.count(`SELECT count(*) FROM v2.agent_notices WHERE op_id = $1 AND connection_id = $2 AND read_it`, res.Tombstone.ID, cc.ID)
	if readIt != 1 {
		t.Error("the reader's notice doesn't say it read it")
	}
	actor, ascope := f.agentActor(zz, cc, policy.AutonomyPropose)
	_ = actor
	got, err := f.l.TakeNotices(ctx, ascope, cc.ID, "memax_recall", 10)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Refs, []string{m.Ref}) || got[0].Kind != ledger.NoticeForgotten {
		t.Fatalf("take: %+v, %v", got, err)
	}
	if again, _ := f.l.TakeNotices(ctx, ascope, cc.ID, "memax_recall", 10); len(again) != 0 {
		t.Errorf("told twice: %+v", again)
	}
	ts, err := f.l.GetTombstone(ctx, scope, m.Ref)
	if err != nil {
		t.Fatal(err)
	}
	told, waiting := 0, 0
	for _, s := range ts.Steps {
		if s.Kind != ledger.StepAgent {
			continue
		}
		switch s.Status {
		case ledger.StepDone:
			told++
		case ledger.StepWaiting:
			waiting++
		}
	}
	if told != 1 || waiting != 1 {
		t.Errorf("agent steps: told %d, waiting %d", told, waiting)
	}
	u := map[string]ledger.UnreachableCopy{}
	for _, c := range ts.Unreachable {
		u[c.Kind] = c
	}
	if u[ledger.UnreachableAgentMemory].Agents == nil || u[ledger.UnreachableBackups].Days != ledger.DefaultBackupDays {
		t.Errorf("unreachable = %+v", ts.Unreachable)
	}
	_ = cx
}

// An agent's memax_forget is a request: a person forgets the memory (the
// request is answered) or keeps it (declined).
func TestForgetRequests(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	m := f.remember(zz, space, "The demo uses the memax-v2 space.")
	n := f.remember(zz, space, "Another fact.")
	cx := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "codex", nil), ledger.AgentCodex, at(space, policy.AutonomyPropose))
	actor, ascope := f.agentActor(zz, cx, policy.AutonomyPropose)
	req := &ledger.RequestForget{Meta: meta(actor, ascope, policy.ViaMCP), Memory: m.Ref}
	req.Reason = "it was a test value"
	res := f.apply(req)
	if res.Outcome != ledger.OutcomeApplied || res.ForgetRequest == nil || res.ForgetRequest.Status != ledger.ForgetRequestWaiting {
		t.Fatalf("request: %+v", res)
	}
	if again := f.apply(&ledger.RequestForget{Meta: meta(actor, ascope, policy.ViaMCP), Memory: m.Ref}); !again.Unchanged {
		t.Errorf("asking again wrote: %+v", again)
	}
	waiting, err := f.l.ForgetRequests(ctx, scope, m.ID)
	if err != nil || len(waiting) != 1 || waiting[0].Reason != "it was a test value" || waiting[0].Agent.Agent != "codex" {
		t.Fatalf("waiting: %+v, %v", waiting, err)
	}
	// The person forgets it: the request is answered, its reason goes.
	ts := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, m.Ref, 1)).Tombstone
	if ts.RequestedBy == nil || ts.RequestedBy.ConnectionID != cx.ID {
		t.Errorf("tombstone requested_by = %+v", ts.RequestedBy)
	}
	if c := f.count(`SELECT count(*) FROM v2.forget_requests WHERE memory_id = $1 AND status = 'forgotten'`, m.ID); c != 1 {
		t.Error("the request isn't answered")
	}
	// Keeping one instead.
	f.apply(&ledger.RequestForget{Meta: meta(actor, ascope, policy.ViaMCP), Memory: n.Ref})
	refused := f.apply(&ledger.DeclineForget{Meta: meta(actor, ascope, policy.ViaMCP), Memory: n.Ref})
	refusedWith(t, refused, policy.CodeKeyCannotForget)
	kept := f.apply(&ledger.DeclineForget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: n.Ref})
	if kept.Outcome != ledger.OutcomeApplied || kept.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("keep it: %+v", kept)
	}
	if left, _ := f.l.ForgetRequests(ctx, scope, n.ID); len(left) != 0 {
		t.Errorf("still waiting: %+v", left)
	}
	if _, err := f.l.Apply(ctx, &ledger.DeclineForget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: n.Ref}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("keep it with nothing waiting: %v", err)
	}
	// A person doesn't request; a read-only agent can't.
	refusedWith(t, f.apply(&ledger.RequestForget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: n.Ref}), policy.CodeForgetByPerson)
	ro := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "cursor", nil), ledger.AgentCursor, at(space, policy.AutonomyRead))
	roActor, roScope := f.agentActor(zz, ro, policy.AutonomyRead)
	refusedWith(t, f.apply(&ledger.RequestForget{Meta: meta(roActor, roScope, policy.ViaMCP), Memory: n.Ref}), policy.CodeKeyReadOnly)
}

// Rule 11's drafts: a kept side's narrowed words wait above its current
// version, and Forget purges them with every other version, along with
// the settling verdict on them.
func TestForgetPurgesDrafts(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	c := f.withThirdDecision(zz, "drafts")
	const narrowD = "Production deploys to Railway; previews run elsewhere."
	res, err := f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: c.p.Ref, Other: c.d.Ref,
		Choice: ledger.ChooseBoth, ExpectedVersion: 1, OtherStatement: narrowD})
	if err != nil || res.Outcome != ledger.OutcomeProposed {
		t.Fatalf("keep both: %v %s", err, res.Outcome)
	}
	f.settled(c.sp, c.d.ID, 2, nil)
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND version = 2 AND statement IS NOT NULL`, c.d.ID); n != 1 {
		t.Fatalf("no draft to purge (%d)", n)
	}
	scope := f.scope(zz).Narrow(c.sp)
	out := f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, c.d.Ref, 1))
	if out.Tombstone.Gone.Versions != 2 {
		t.Errorf("gone = %+v, want both versions", out.Tombstone.Gone)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL`, c.d.ID); n != 0 {
		t.Errorf("%d versions (the draft included) keep words", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.judge_verdicts WHERE memory_id = $1 AND (rationale IS NOT NULL OR merged_statement IS NOT NULL)`, c.d.ID); n != 0 {
		t.Errorf("%d verdicts on the draft keep words", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND reason IS NOT NULL`, c.d.ID); n != 0 {
		t.Errorf("%d receipts (the drafted one included) keep a reason", n)
	}
	// The proposal left in conflict with it is free.
	if p := f.mem(zz, c.p.ID); p.Flags.Has(lifecycle.Conflict) {
		t.Errorf("the other side still in conflict: %v", p.Flags)
	}
}

// An import's disagreement (memax init) holds the model's words about its
// members: forgetting any member takes them, before the group is settled
// and after, and the group keeps its members and its settlement.
func TestForgetPurgesImportConflictWords(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	words := `(subject IS NOT NULL OR rationale IS NOT NULL OR suggestion IS NOT NULL)`

	res := f.importAs(person(zz), f.scope(zz), sp, "init",
		fileItem("a", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		fileItem("b", "AGENTS.md:8", "Run `npm run test` before committing."),
		fileItem("c", "CLAUDE.md:20", "Lint with `pnpm lint` before pushing."),
		fileItem("d", "AGENTS.md:30", "Lint with `npm run lint` only in CI."))
	a, c, d := res.Items[0].Memory, res.Items[2].Memory, res.Items[3].Memory
	b := res.Items[1].Memory
	f.checkImport(sp, res.Import.ID, "Test command", "Run tests with `pnpm test` everywhere.", a.ID, b.ID)
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND `+words, res.Import.ID); n != 1 {
		t.Fatalf("%d groups with words", n)
	}
	scope := f.scope(zz).Narrow(sp)
	f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, a.Ref, 1))
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND `+words, res.Import.ID); n != 0 {
		t.Errorf("an open group keeps the model's words about a forgotten member")
	}
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND cardinality(members) = 2 AND state = 'open'`, res.Import.ID); n != 1 {
		t.Error("the group lost its members or its state")
	}

	// A second import, its group settled, then a member forgotten.
	res2 := f.importAs(person(zz), f.scope(zz), sp, "init-2",
		fileItem("e", "CLAUDE.md:40", "Deploy with `fly deploy`."),
		fileItem("g", "AGENTS.md:41", "Deploy through the Railway dashboard."))
	e, g := res2.Items[0].Memory, res2.Items[1].Memory
	f.checkImport(sp, res2.Import.ID, "Deploy target", "", e.ID, g.ID)
	f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res2.Import.ID, N: 1, Choice: ledger.ChooseKeepAll})
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND state = 'settled' AND `+words, res2.Import.ID); n != 1 {
		t.Fatalf("the settled group: %d with words", n)
	}
	kept, err := f.l.GetMemory(context.Background(), scope, e.Ref)
	if err != nil {
		t.Fatal(err)
	}
	f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, e.Ref, kept.Version))
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND `+words, res2.Import.ID); n != 0 {
		t.Error("a settled group keeps the model's words about a forgotten member")
	}
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND state = 'settled' AND choice = 'keep_all'`, res2.Import.ID); n != 1 {
		t.Error("forgetting a member changed the settlement")
	}
	// The settlement itself is still final.
	if _, err := f.pool.Exec(context.Background(), `UPDATE v2.import_conflicts SET state = 'open', settled_at = NULL, choice = NULL WHERE import_id = $1`, res2.Import.ID); err == nil {
		t.Error("a settled group reopened")
	}

	// The check comes back after a member was forgotten: the model's words
	// may repeat it, so the group is recorded without them.
	res3 := f.importAs(person(zz), f.scope(zz), sp, "init-3",
		fileItem("h", "CLAUDE.md:50", "Format with Prettier on save."),
		fileItem("i", "AGENTS.md:51", "Format with Biome in CI."),
		fileItem("j", "AGENTS.md:52", "Never run a formatter on generated files."))
	h, i, j := res3.Items[0].Memory, res3.Items[1].Memory, res3.Items[2].Memory
	f.apply(forgetCmd(person(zz), scope, policy.ViaWeb, j.Ref, 1))
	f.checkImport(sp, res3.Import.ID, "Formatter", "Format with Prettier.", h.ID, i.ID, j.ID)
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND cardinality(members) = 2`, res3.Import.ID); n != 1 {
		t.Fatalf("the group of the two left: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.import_conflicts WHERE import_id = $1 AND `+words, res3.Import.ID); n != 0 {
		t.Error("a group recorded after a member was forgotten keeps the model's words")
	}
	_, _, _ = c, d, g
}
