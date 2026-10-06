package judge_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fixture is a migrated database with a ledger that enqueues its jobs
// through River (insert-only), the way the API server runs.
type fixture struct {
	t    *testing.T
	pool *pgxpool.Pool
	l    *ledger.Ledger
	ctx  context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, pool: pool, ctx: context.Background(),
		l: ledger.New(pool, ledger.WithLogger(quiet), ledger.WithJobs(client))}
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n
}

func (f *fixture) user(name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

func (f *fixture) space(owner uuid.UUID, name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
		id, name, id.String(), owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (f *fixture) scope(user uuid.UUID) ledger.Scope {
	s, err := f.l.UserScope(f.ctx, user)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func person(id uuid.UUID) ledger.Actor {
	return ledger.Actor{Kind: policy.ActorPerson, ID: id, Name: "Ziyang"}
}

func agent(level policy.Autonomy) ledger.Actor {
	return ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: level}
}

func (f *fixture) apply(cmd ledger.Command) ledger.Result {
	f.t.Helper()
	res, err := f.l.Apply(f.ctx, cmd)
	if err != nil {
		f.t.Fatalf("Apply %s: %v", cmd.Name(), err)
	}
	return res
}

func meta(a ledger.Actor, s ledger.Scope, via policy.Via) ledger.Meta {
	return ledger.Meta{Actor: a, Scope: s, Via: via, IdempotencyKey: uuid.NewString()}
}

// kept remembers a statement as the owner, on the web.
func (f *fixture) kept(owner, space uuid.UUID, nm ledger.NewMemory) *ledger.Memory {
	f.t.Helper()
	nm.SpaceID = space
	res := f.apply(&ledger.Remember{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb), NewMemory: nm})
	if res.Memory.Lifecycle != lifecycle.Kept {
		f.t.Fatalf("remember: %s", res.Policy.Message)
	}
	return res.Memory
}

// propose writes a proposal as an agent at Propose.
func (f *fixture) propose(owner, space uuid.UUID, nm ledger.NewMemory) *ledger.Memory {
	f.t.Helper()
	nm.SpaceID = space
	res := f.apply(&ledger.Propose{Meta: meta(agent(policy.AutonomyPropose), f.scope(owner), policy.ViaMCP), NewMemory: nm})
	if res.Memory == nil || res.Memory.Lifecycle != lifecycle.Proposed {
		f.t.Fatalf("propose: %s %s", res.Outcome, res.Policy.Message)
	}
	return res.Memory
}

func fact(statement string) ledger.NewMemory {
	return ledger.NewMemory{Statement: statement, Section: ledger.SectionConventions}
}

func decision(statement, area string) ledger.NewMemory {
	return ledger.NewMemory{Statement: statement, Section: ledger.SectionDecisions, Kind: ledger.KindDecision,
		Decision: &ledger.DecisionFields{Area: area, Status: ledger.DecisionInForce}}
}

// judgeJob is the job the ledger enqueued for a memory version.
func (f *fixture) judgeJob(m *ledger.Memory, version int) ledger.JudgeArgs {
	f.t.Helper()
	var raw []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT args FROM river_job WHERE kind = 'judge_proposal'
	        AND args->>'memory_id' = $1 AND (args->>'version')::int = $2 ORDER BY id DESC LIMIT 1`,
		m.ID.String(), version).Scan(&raw); err != nil {
		f.t.Fatalf("no judge job for %s v%d: %v", m.Ref, version, err)
	}
	var a ledger.JudgeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		f.t.Fatal(err)
	}
	return a
}

// run judges a memory's current version the way the worker would.
func (f *fixture) run(j *judge.Judge, m *ledger.Memory) *judge.Run {
	f.t.Helper()
	r, err := j.Run(f.ctx, f.judgeJob(m, m.Version), judge.RunOptions{EnqueuedAt: time.Now()})
	if err != nil {
		f.t.Fatalf("judge %s: %v", m.Ref, err)
	}
	return r
}

func (f *fixture) get(user uuid.UUID, ref uuid.UUID) *ledger.Memory {
	f.t.Helper()
	m, err := f.l.GetMemory(f.ctx, f.scope(user), ref.String())
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func link(m *ledger.Memory, kind ledger.LinkKind, dir string) *ledger.Link {
	for i, l := range m.Links {
		if l.Kind == kind && l.Direction == dir {
			return &m.Links[i]
		}
	}
	return nil
}

func stage0Only(f *fixture, opts ...judge.Option) *judge.Judge {
	return judge.New(f.l, nil, judge.Config{Log: quiet}, opts...)
}

func withModel(f *fixture, m judge.Model, cfg judge.Config) *judge.Judge {
	cfg.Log = quiet
	return judge.New(f.l, m, cfg)
}

// The ledger enqueues judge_proposal in the propose transaction, unique
// per memory version, and not for a person's own keep.
func TestJudgeIsEnqueuedWithTheProposal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	p := f.propose(zz, sp, fact("Workers must be idempotent."))
	a := f.judgeJob(p, 1)
	if a.Mode != ledger.JudgeProposal || a.SpaceID != sp || a.Round != 0 {
		t.Errorf("job args = %+v", a)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal' AND queue = 'judge'`); n != 1 {
		t.Errorf("%d judge jobs", n)
	}
	f.kept(zz, sp, fact("A person's own words."))
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal'`); n != 1 {
		t.Errorf("a person's keep enqueued the judge (%d jobs)", n)
	}
	// A new version of the proposal is judged again.
	f.apply(&ledger.Edit{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref, ExpectedVersion: 1,
		Statement: "Workers must be idempotent and retry safely."})
	if a := f.judgeJob(p, 2); a.Version != 2 {
		t.Errorf("edit job = %+v", a)
	}
	// Until the judge runs, Review shows the neutral working mark.
	if m := f.get(zz, p.ID); m.Judge == nil || m.Judge.State != ledger.JudgeWorking || m.Judge.Version != 2 {
		t.Errorf("judge = %+v", m.Judge)
	}
}

func TestStage0FoldsRepeatsOfKeptMemories(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	j := stage0Only(f)
	pnpm := f.kept(zz, sp, fact("Use pnpm workspaces only; never npm or yarn in this repository."))

	exact := f.propose(zz, sp, fact("use pnpm workspaces only; never npm or yarn in this repository"))
	r := f.run(j, exact)
	got := f.get(zz, exact.ID)
	if r.Outcome != ledger.OutcomeFolded || r.Stage != ledger.StageExact || got.Lifecycle != lifecycle.Merged {
		t.Fatalf("exact repeat: %+v, %s", r, got.Lifecycle)
	}
	if l := link(got, ledger.LinkMergedInto, ledger.LinkOut); l == nil || l.MemoryID != pnpm.ID {
		t.Errorf("links = %+v", got.Links)
	}
	if got.Judge == nil || got.Judge.Verdict != ledger.RelationDuplicate || got.Judge.Related.Ref != pnpm.Ref {
		t.Errorf("judge = %+v", got.Judge)
	}
	// The fold is a receipt by Memax, naming the memory it repeats, with no words.
	if r.Receipt.Action != ledger.ActionMerged || r.Receipt.ActorKind != policy.ActorMemax || r.Receipt.Source.Ref != pnpm.Ref ||
		strings.Contains(r.Receipt.Reason, "pnpm") {
		t.Errorf("receipt = %+v", r.Receipt)
	}

	near := f.propose(zz, sp, fact("Use pnpm workspaces only, never npm or yarn in this repository."))
	if r := f.run(j, near); r.Outcome != ledger.OutcomeFolded || r.Stage != ledger.StageNear {
		t.Errorf("near repeat: %+v", r)
	}
	// Numbers differ: never a stage-0 duplicate. No model, so nothing happens.
	f.kept(zz, sp, fact("Use pnpm 9 for every package in the monorepo, with workspaces."))
	trap := f.propose(zz, sp, fact("Use pnpm 10 for every package in the monorepo, with workspaces."))
	if r := f.run(j, trap); r.Outcome != ledger.OutcomeNone || r.Stage != ledger.StageNone {
		t.Errorf("pnpm 9 vs 10: %+v", r)
	}
	if m := f.get(zz, trap.ID); m.Lifecycle != lifecycle.Proposed || m.Judge.State != ledger.JudgeJudged {
		t.Errorf("trap = %s %+v", m.Lifecycle, m.Judge)
	}
	// A second run of the same job changes nothing.
	if r := f.run(j, trap); !r.Skipped {
		t.Errorf("rerun = %+v", r)
	}
}

func TestReproposalsFoldIntoTheirRejection(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	j := stage0Only(f)
	first := f.propose(zz, sp, fact("Squash every commit before merging."))
	f.run(j, first)
	f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: first.Ref})

	again := f.propose(zz, sp, fact("Squash every commit before merging"))
	r := f.run(j, again)
	got := f.get(zz, again.ID)
	if r.Outcome != ledger.OutcomeSuppressed || r.Stage != ledger.StageReproposal || got.Lifecycle != lifecycle.Merged {
		t.Fatalf("re-proposal: %+v %s", r, got.Lifecycle)
	}
	if !strings.Contains(r.Receipt.Reason, "previously rejected") || !strings.Contains(r.Receipt.Reason, first.Ref) {
		t.Errorf("reason = %q", r.Receipt.Reason)
	}
	// It never reaches Review.
	page, err := f.l.ReviewQueue(f.ctx, f.scope(zz), ledger.ReviewQuery{SpaceID: sp})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range page.Memories {
		if m.ID == again.ID {
			t.Error("the re-proposal is in Review")
		}
	}
	// Outside the window, a repeat of a rejection is judged afresh.
	late := judge.New(f.l, nil, judge.Config{Log: quiet, RejectedWithin: time.Millisecond})
	third := f.propose(zz, sp, fact("Squash every commit before merging!"))
	if r := f.run(late, third); r.Outcome != ledger.OutcomeNone {
		t.Errorf("outside the window: %+v", r)
	}
}

func TestModelVerdicts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	ask := f.kept(zz, sp, fact("Ask uses the Haiku tier for answer synthesis."))
	errorsRFC := f.kept(zz, sp, fact("API errors are RFC 9457 problem+json documents."))
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	queue := f.kept(zz, sp, fact("Background jobs run on River with Postgres."))

	cases := []struct {
		name    string
		nm      ledger.NewMemory
		table   map[string]verdict
		outcome ledger.VerdictOutcome
		target  *ledger.Memory
		state   lifecycle.Mark
	}{
		{"a paraphrase is folded", fact("Answer synthesis in Ask runs on the Haiku tier."),
			map[string]verdict{ask.Ref: {ledger.RelationDuplicate, 0.95, false, ""}}, ledger.OutcomeFolded, ask, lifecycle.MarkMerged},
		{"an update is linked", fact("Ask uses the Sonnet tier for answer synthesis."),
			map[string]verdict{ask.Ref: {ledger.RelationUpdates, 0.9, false, ""}}, ledger.OutcomeLinked, ask, lifecycle.MarkProposed},
		{"an extension is left alone", fact("API errors carry a stable code alongside the RFC 9457 type."),
			map[string]verdict{errorsRFC.Ref: {ledger.RelationExtends, 0.9, false, "API errors are RFC 9457 problem+json with a code."}},
			ledger.OutcomeNone, errorsRFC, lifecycle.MarkProposed},
		{"a contradiction of a decision is flagged", decision("Deploy the v2 API to Fly.io in iad and ams.", "deploy target"),
			map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.92, false, ""}}, ledger.OutcomeFlagged, railway, lifecycle.MarkConflict},
		{"a contradiction of a plain fact is only recorded", fact("Background jobs run on Temporal."),
			map[string]verdict{queue.Ref: {ledger.RelationContradicts, 0.95, false, ""}}, ledger.OutcomeNone, queue, lifecycle.MarkProposed},
		{"unrelated", fact("The docs site is built with Fumadocs."), map[string]verdict{}, ledger.OutcomeNone, nil, lifecycle.MarkProposed},
	}
	for _, c := range cases {
		m := &fakeModel{answer: oracle(c.table)}
		j := withModel(f, m, tiers(true, true))
		p := f.propose(zz, sp, c.nm)
		r := f.run(j, p)
		got := f.get(zz, p.ID)
		if r.Outcome != c.outcome || got.State != c.state {
			t.Errorf("%s: outcome %s state %s, want %s %s (calls %v)", c.name, r.Outcome, got.State, c.outcome, c.state, m.tiers())
			continue
		}
		if c.target != nil && (got.Judge.Related == nil || got.Judge.Related.ID != c.target.ID) {
			t.Errorf("%s: judge = %+v", c.name, got.Judge)
		}
		switch c.outcome {
		case ledger.OutcomeLinked:
			if got.Updates == nil || got.Updates.Ref != c.target.Ref || got.Updates.Statement != c.target.Statement {
				t.Errorf("%s: updates = %+v", c.name, got.Updates)
			}
		case ledger.OutcomeFlagged:
			if l := link(got, ledger.LinkConflictsWith, ledger.LinkOut); l == nil || l.MemoryID != c.target.ID {
				t.Errorf("%s: links = %+v", c.name, got.Links)
			}
			// Verdicts on a decision in force are the strong tier's.
			if got.Judge.Tier != ledger.TierStrong {
				t.Errorf("%s: tier %s", c.name, got.Judge.Tier)
			}
		case ledger.OutcomeNone:
			if c.name == "an extension is left alone" && got.Judge.MergedStatement == "" {
				t.Errorf("%s: no merged statement offered", c.name)
			}
		}
	}
}

// Rule 11: a proposal that contradicts a decision in force is flagged
// before anyone keeps it, Review returns it with the conflict, and it
// can't be kept until the conflict is settled.
func TestContradictionIsFlaggedBeforeKeep(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	fly := f.propose(zz, sp, decision("Deploy the v2 API to Fly.io in iad and ams.", "deploy target"))
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.9, false, ""}})},
		tiers(false, false))
	r := f.run(j, fly)
	if r.Outcome != ledger.OutcomeFlagged || r.Receipt.Action != ledger.ActionFlagged {
		t.Fatalf("run = %+v", r)
	}
	page, err := f.l.ReviewQueue(f.ctx, f.scope(zz), ledger.ReviewQuery{SpaceID: sp})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Memories) != 1 || page.Memories[0].State != lifecycle.MarkConflict ||
		link(&page.Memories[0], ledger.LinkConflictsWith, ledger.LinkOut) == nil || page.Memories[0].Judge.Verdict != ledger.RelationContradicts {
		t.Fatalf("review = %+v", page.Memories)
	}
	_, err = f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: fly.Ref})
	var te *ledger.TransitionError
	if err == nil || !errors.As(err, &te) {
		t.Errorf("keep of a conflict = %v, want a transition error", err)
	}
	// The decision shows the incoming conflict.
	if d := f.get(zz, railway.ID); link(d, ledger.LinkConflictsWith, ledger.LinkIn) == nil {
		t.Errorf("decision links = %+v", d.Links)
	}
}

func TestStage1FailureNeverBlocksReview(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway.", "deploy target"))
	m := &fakeModel{answer: func(judge.Call, int) (string, error) { return "", nil }}
	j := withModel(f, m, tiers(true, true))
	p := f.propose(zz, sp, decision("Deploy the v2 API to Fly.io.", "deploy target"))
	r := f.run(j, p)
	got := f.get(zz, p.ID)
	if r.Outcome != ledger.OutcomeFailed || got.State != lifecycle.MarkProposed || got.Judge.State != ledger.JudgeFailed {
		t.Errorf("run %+v, state %s, judge %+v", r, got.State, got.Judge)
	}
	if j.Metrics.LLMFailed.Load() != 1 {
		t.Errorf("metric = %d", j.Metrics.LLMFailed.Load())
	}
	if len(m.tiers()) != 4 {
		t.Errorf("calls = %v; want two primary and two fallback", m.tiers())
	}
	// The person can still keep it: nothing was flagged.
	res := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref})
	if res.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("keep = %s", res.Memory.Lifecycle)
	}
	_ = railway
}

func TestEscalationGuardsConflicts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway.", "deploy target"))
	primarySays := map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.95, false, ""}}

	// The strong tier overrules the primary: no conflict.
	m := &fakeModel{answer: func(c judge.Call, n int) (string, error) {
		if c.Tier.Name == ledger.TierStrong {
			return oracle(map[string]verdict{railway.Ref: {ledger.RelationExtends, 0.8, false, ""}})(c, n)
		}
		return oracle(primarySays)(c, n)
	}}
	p := f.propose(zz, sp, decision("Preview environments for the v2 API also run on Railway.", "deploy target"))
	if r := f.run(withModel(f, m, tiers(true, true)), p); r.Outcome != ledger.OutcomeNone || r.Relation != ledger.RelationExtends {
		t.Errorf("overruled: %+v", r)
	}
	if !strings.Contains(strings.Join(m.tiers(), ","), "strong") {
		t.Errorf("calls = %v", m.tiers())
	}

	// The strong tier is down: the unconfirmed conflict is not flagged.
	down := &fakeModel{answer: func(c judge.Call, n int) (string, error) {
		if c.Tier.Name == ledger.TierStrong {
			return "", context.DeadlineExceeded
		}
		return oracle(primarySays)(c, n)
	}}
	j := withModel(f, down, tiers(true, true))
	q := f.propose(zz, sp, decision("Deploy the v2 API to Fly.io.", "deploy target"))
	if r := f.run(j, q); r.Outcome != ledger.OutcomeNone {
		t.Errorf("unconfirmed: %+v", r)
	}
	if j.Metrics.EscalationFailed.Load() != 1 {
		t.Error("escalation failure not counted")
	}
	var errCode string
	if err := f.pool.QueryRow(f.ctx, `SELECT error FROM v2.judge_verdicts WHERE memory_id = $1`, q.ID).Scan(&errCode); err != nil || errCode != "escalation_failed" {
		t.Errorf("verdict error = %q %v", errCode, err)
	}
}

// TEPA: an explicit change of a decision with the same key supersedes it;
// keeping the change supersedes the decision, which stops being in force.
func TestExplicitChangeSupersedesOnKeep(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway.", "deploy target"))
	p := f.propose(zz, sp, decision("We moved the v2 API from Railway to Fly.io.", "deploy-target"))
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationUpdates, 0.9, true, ""}})}, tiers(false, true))
	if r := f.run(j, p); r.Outcome != ledger.OutcomeSuperseding {
		t.Fatalf("run = %+v", r)
	}
	got := f.get(zz, p.ID)
	if got.Updates == nil || got.Updates.ID != railway.ID || got.State != lifecycle.MarkProposed {
		t.Fatalf("proposal = %s %+v", got.State, got.Updates)
	}
	res := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref})
	if len(res.Receipts) != 2 || res.Receipts[1].Action != ledger.ActionSuperseded || res.Receipts[1].ObjectID != railway.ID {
		t.Fatalf("keep receipts = %+v", res.Receipts)
	}
	old := f.get(zz, railway.ID)
	if old.Lifecycle != lifecycle.Kept || old.Decision.Status != ledger.DecisionSuperseded {
		t.Errorf("old decision = %s %+v", old.Lifecycle, old.Decision)
	}
	if l := link(old, ledger.LinkSupersedes, ledger.LinkIn); l == nil || l.MemoryID != p.ID {
		t.Errorf("old links = %+v", old.Links)
	}
}

// A Write-level agent keeps at once unless its write touches a decision in
// force; what it kept is still checked after the fact.
func TestWriteAgentsArePrechecked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	writer := agent(policy.AutonomyWrite)
	write := func(nm ledger.NewMemory) ledger.Result {
		nm.SpaceID = sp
		return f.apply(&ledger.Propose{Meta: meta(writer, f.scope(zz), policy.ViaMCP), NewMemory: nm})
	}
	for _, c := range []struct {
		name string
		nm   ledger.NewMemory
	}{
		{"same area", decision("Use one Fly app per region.", "deploy target")},
		{"names the area", fact("Our deploy target needs a health check on /healthz.")},
		{"overlaps", fact("The v2 API deploys to Fly.io now, with preview environments on Fly too.")},
	} {
		res := write(c.nm)
		if res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeTouchesDecision {
			t.Errorf("%s: %s %s", c.name, res.Outcome, res.Policy.Code)
		}
	}
	if unrelated := write(fact("Background jobs retry five times.")); unrelated.Outcome != ledger.OutcomeApplied {
		t.Fatalf("unrelated write: %s %s", unrelated.Outcome, unrelated.Policy.Code)
	}
	// One shared word is below the pre-check's bar, so this is kept at once;
	// the judge's candidates still include the decision.
	kept := write(fact("Preview builds run on Fly.io machines."))
	if kept.Outcome != ledger.OutcomeApplied || kept.Memory.Lifecycle != lifecycle.Kept {
		t.Fatalf("write: %s %s", kept.Outcome, kept.Policy.Code)
	}
	a := f.judgeJob(kept.Memory, 1)
	if a.Mode != ledger.JudgeKept {
		t.Fatalf("kept write job = %+v", a)
	}
	// The judge finds what the pre-check couldn't: the kept memory is
	// flagged, and every target recompiles to mark it.
	f.apply(&ledger.ReviseBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: sp, Title: "Brief",
		Sections: []ledger.BriefSection{{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: railway.Ref}}}}})
	f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: sp, Kind: ledger.TargetAgentsMD})
	before := f.count(`SELECT dirty_gen FROM v2.targets WHERE space_id = $1`, sp)
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.9, false, ""}})}, tiers(false, false))
	r, err := j.Run(f.ctx, a, judge.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := f.get(zz, kept.Memory.ID)
	if r.Outcome != ledger.OutcomeFlagged || got.Lifecycle != lifecycle.Kept || !got.Flags.Has(lifecycle.Conflict) {
		t.Errorf("post-hoc: %+v, %s %v", r, got.Lifecycle, got.Flags)
	}
	if n := f.count(`SELECT dirty_gen FROM v2.targets WHERE space_id = $1`, sp); n != before+1 {
		t.Errorf("dirty_gen %d → %d; the flag must recompile", before, n)
	}
}

func TestConditionsFromSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	f.kept(zz, sp, fact("Background jobs run on River with Postgres."))
	nm := fact("Background jobs use River's unique jobs for compile_target.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "go.mod:14", URI: "go.mod",
		Quote: "github.com/riverqueue/river v0.49.0"}}
	p := f.propose(zz, sp, nm)
	m := &fakeModel{answer: oracle(nil,
		judge.Condition{Kind: "dep_present", Manifest: "go.mod", Name: "github.com/riverqueue/river"},
		judge.Condition{Kind: "file_exists", Path: "internal/queue/invented.go"},
		judge.Condition{Kind: "before", Before: "2027-03-01"})}
	cfg := tiers(false, false)
	cfg.Conditions = true
	f.run(withModel(f, m, cfg), p)
	var conds []map[string]any
	if err := json.Unmarshal(f.get(zz, p.ID).Conditions, &conds); err != nil {
		t.Fatal(err)
	}
	if len(conds) != 1 || conds[0]["kind"] != "dep_present" || conds[0]["name"] != "github.com/riverqueue/river" {
		t.Errorf("conditions = %v; only the evident one should stay", conds)
	}
	if !strings.Contains(m.calls[0].System, "stays true while") {
		t.Error("the prompt doesn't ask for conditions")
	}
}

// Isolation (rule 13): the judge never compares across spaces.
func TestJudgeStaysInItsSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	mine := f.space(zz, "memax-v2")
	theirs := f.space(jy, "side-project")
	f.kept(jy, theirs, fact("Use pnpm workspaces only."))
	f.kept(jy, theirs, decision("Deploy everything to Railway.", "deploy target"))
	p := f.propose(zz, mine, decision("Deploy everything to Railway.", "deploy target"))
	m := &fakeModel{answer: oracle(nil)}
	r := f.run(withModel(f, m, tiers(false, false)), p)
	if r.Outcome != ledger.OutcomeNone || r.Candidates != 0 || len(m.calls) != 0 {
		t.Errorf("run = %+v, calls %d", r, len(m.calls))
	}
	q := f.propose(zz, mine, fact("Use pnpm workspaces only."))
	if r := f.run(stage0Only(f), q); r.Outcome != ledger.OutcomeNone {
		t.Errorf("folded into another space: %+v", r)
	}
}

// Every judge write has its receipt in the same space: the verdicts, the
// links and the state changes.
func TestEveryJudgeWriteIsReceipted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway.", "deploy target"))
	f.kept(zz, sp, fact("Use pnpm workspaces only."))
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.9, false, ""}})}, tiers(false, false))
	for _, nm := range []ledger.NewMemory{fact("use pnpm workspaces only"), decision("Deploy the v2 API to Fly.io.", "deploy target"), fact("Unrelated.")} {
		f.run(j, f.propose(zz, sp, nm))
	}
	for name, sql := range map[string]string{
		"verdicts": `SELECT count(*) FROM v2.judge_verdicts v LEFT JOIN v2.receipts r ON r.id = v.receipt_id AND r.space_id = v.space_id
		             AND r.object_id = v.memory_id WHERE r.id IS NULL`,
		"links": `SELECT count(*) FROM v2.memory_links l LEFT JOIN v2.receipts r ON r.id = l.receipt_id AND r.space_id = l.space_id
		          WHERE r.id IS NULL`,
		"memories": `SELECT count(*) FROM v2.memories m LEFT JOIN v2.receipts r ON r.id = m.last_receipt_id AND r.space_id = m.space_id
		             AND r.object_id = m.id WHERE r.id IS NULL`,
	} {
		if n := f.count(sql); n != 0 {
			t.Errorf("%s: %d rows without their receipt", name, n)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.judge_verdicts`); n != 3 {
		t.Errorf("%d verdicts", n)
	}
	// A verdict can't be written without a receipt, even by hand.
	_, err := f.pool.Exec(f.ctx, `INSERT INTO v2.judge_verdicts (memory_id, version, round, space_id, mode, stage, verdict, outcome,
	        receipt_id, last_receipt_id) SELECT id, current_version, 9, space_id, 'proposal', 'none', 'none', 'none', last_receipt_id,
	        last_receipt_id FROM v2.memories LIMIT 1`)
	if err == nil || !strings.Contains(err.Error(), "MXR01") && !strings.Contains(err.Error(), "receipt") {
		t.Errorf("verdict without a receipt: %v", err)
	}
}

// Rule 11 holds against the two ways a Keep could beat the judge: a
// confirmation in the agent, and a person's Keep right after the proposal.
func TestKeepWaitsForTheJudge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	present := agent(policy.AutonomyPropose)
	present.PersonPresent, present.CanElicit = true, true
	ask := func(nm ledger.NewMemory) ledger.Result {
		nm.SpaceID = sp
		return f.apply(&ledger.Propose{Meta: meta(present, f.scope(zz), policy.ViaMCP), NewMemory: nm})
	}
	if res := ask(fact("Workers must be idempotent.")); res.Outcome != ledger.OutcomeNeedsConfirmation {
		t.Errorf("unrelated: %s %s", res.Outcome, res.Policy.Code)
	}
	touching := ask(decision("Deploy the v2 API to Fly.io.", "deploy target"))
	if touching.Outcome != ledger.OutcomeProposed || touching.Policy.Code != policy.CodeTouchesDecision {
		t.Fatalf("touching: %s %s; it must wait for the judge, not be confirmed in the agent", touching.Outcome, touching.Policy.Code)
	}
	// Keeping it before the judge has run is busy, not done.
	_, err := f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Memory.Ref})
	var pending *ledger.JudgePendingError
	if !errors.As(err, &pending) || !errors.Is(err, ledger.ErrBusy) {
		t.Fatalf("early keep = %v", err)
	}
	// Once judged (and flagged), it's a conflict to settle.
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.9, false, ""}})}, tiers(false, false))
	f.run(j, touching.Memory)
	_, err = f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Memory.Ref})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("keep of the flagged proposal = %v", err)
	}
	// A proposal that touches no decision is kept at once.
	plain := f.propose(zz, sp, fact("Use tabs in Go files."))
	if res := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: plain.Ref}); res.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("plain keep = %s", res.Memory.Lifecycle)
	}
	// So is a touching one the judge cleared.
	other := f.propose(zz, sp, fact("Preview environments for the v2 API need seed data."))
	f.run(withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationExtends, 0.9, false, ""}})}, tiers(false, false)), other)
	if res := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: other.Ref}); res.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("cleared keep = %s", res.Memory.Lifecycle)
	}
}

// The judge's own latency, with an instant model: a verdict is well inside
// the 5 s budget (the model's own time comes on top).
func TestJudgeIsFast(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	for i := range 40 {
		f.kept(zz, sp, fact("Convention number "+strings.Repeat("x", i%7)+" for the v2 API and its workers."))
	}
	f.kept(zz, sp, decision("Deploy the v2 API to Railway.", "deploy target"))
	p := f.propose(zz, sp, fact("The v2 API workers retry jobs with backoff."))
	r := f.run(withModel(f, &fakeModel{answer: oracle(nil)}, tiers(false, false)), p)
	if r.Timings["total_ms"] > 1000 {
		t.Errorf("total %d ms without the model", r.Timings["total_ms"])
	}
	if r.Candidates == 0 || r.Candidates > 11 {
		t.Errorf("candidates = %d", r.Candidates)
	}
}
