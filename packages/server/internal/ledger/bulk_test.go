package ledger_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// A bulk Keep or Reject (ApplyBulk) answers, writes and records what the
// same commands applied one by one through Apply would: each case runs on
// two databases built the same way, in bulk on one and one by one on the
// other, then again with the same keys (the retry), and the answers and
// the records must match.

// bulkWorld is a space built for a case, with the people and the
// memories the case's items name.
type bulkWorld struct {
	f     *fixture
	zz    uuid.UUID
	space uuid.UUID
	mems  map[string]*ledger.Memory
}

// bulkSpec is one item: a memory the world named (or a raw ref), and the
// version the person saw.
type bulkSpec struct {
	name    string // a key of bulkWorld.mems, or "ref:" + a ref as sent
	byID    bool   // name the memory by its id
	version int
}

func (w *bulkWorld) ref(s bulkSpec) string {
	if r, ok := strings.CutPrefix(s.name, "ref:"); ok {
		return r
	}
	m := w.mems[s.name]
	if m == nil {
		w.f.t.Fatalf("no memory %q", s.name)
	}
	if s.byID {
		return m.ID.String()
	}
	return m.Ref
}

func (w *bulkWorld) propose(name, statement string) {
	w.f.t.Helper()
	w.mems[name] = w.f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), w.f.scope(w.zz), policy.ViaMCP),
		NewMemory: fact(w.space, statement)}).Memory
}

func (w *bulkWorld) proposeDecision(name, statement, area string) {
	w.f.t.Helper()
	w.mems[name] = w.f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), w.f.scope(w.zz), policy.ViaMCP),
		NewMemory: decisionIn(w.space, statement, area)}).Memory
}

func (w *bulkWorld) decide(name, statement, area string) {
	w.f.t.Helper()
	w.mems[name] = w.f.apply(&ledger.Remember{Meta: meta(person(w.zz), w.f.scope(w.zz), policy.ViaWeb),
		NewMemory: decisionIn(w.space, statement, area)}).Memory
}

// judged records the judge's verdict on a proposal (none, or linked as an
// update of target), so Keep doesn't wait for it.
func (w *bulkWorld) judged(name string, outcome ledger.VerdictOutcome, target string) {
	w.f.t.Helper()
	var t *ledger.Memory
	if target != "" {
		t = w.mems[target]
	}
	w.f.judgeAs(w.space, w.mems[name], outcome, t, ledger.JudgeProposal)
}

type bulkCase struct {
	name   string
	reject bool
	actor  func(w *bulkWorld) ledger.Actor
	via    policy.Via
	kind   policy.SpaceKind
	setup  func(w *bulkWorld)
	items  []bulkSpec
}

func TestBulkReviewMatchesOneByOne(t *testing.T) {
	t.Parallel()
	cases := []bulkCase{
		{name: "keeps proposals, with and without the version seen",
			setup: func(w *bulkWorld) {
				w.propose("a", "Run tests with pnpm test.")
				w.propose("b", "Commit messages are short and imperative.")
				w.propose("c", "Background jobs use River.")
			},
			items: []bulkSpec{{name: "a", version: 1}, {name: "b"}, {name: "c", byID: true, version: 1}}},
		{name: "reports what can't be kept beside what is",
			setup: func(w *bulkWorld) {
				w.propose("a", "Run tests with pnpm test.")
				w.propose("b", "Commit messages are short and imperative.")
				w.mems["kept"] = w.f.remember(w.zz, w.space, "Deploys go through Fly.")
				w.propose("c", "Background jobs use River.")
			},
			items: []bulkSpec{{name: "a", version: 2}, {name: "kept"}, {name: "ref:M-9999"}, {name: "ref:X-12"},
				{name: "ref:" + uuid.NewString()}, {name: "b"}, {name: "c", version: 1}}},
		{name: "the same memory twice, by display ID and by id",
			setup: func(w *bulkWorld) { w.propose("a", "Run tests with pnpm test.") },
			items: []bulkSpec{{name: "a"}, {name: "a", byID: true}}},
		{name: "a flagged proposal is settled, not kept",
			setup: func(w *bulkWorld) {
				w.mems["p"], w.mems["d"] = w.f.conflictPair(w.zz, w.space)
				w.propose("b", "Commit messages are short and imperative.")
			},
			items: []bulkSpec{{name: "p"}, {name: "b"}}},
		{name: "a proposal the judge hasn't seen waits when it touches a decision in force",
			setup: func(w *bulkWorld) {
				w.decide("d", "Deploy the v2 API to Railway.", "deploy target")
				w.proposeDecision("p", "Deploy the v2 API to Fly.io in iad and ams.", "deploy target")
				w.propose("b", "Commit messages are short and imperative.")
			},
			items: []bulkSpec{{name: "p"}, {name: "b"}}},
		{name: "a decision an earlier item keeps is in force for the next",
			setup: func(w *bulkWorld) {
				w.proposeDecision("q", "Background jobs run on River.", "job queue")
				w.proposeDecision("r", "Background jobs run on Sidekiq instead.", "job queue")
				w.proposeDecision("s", "Background jobs run on Temporal instead.", "job queue")
			},
			items: []bulkSpec{{name: "q"}, {name: "r"}, {name: "s"}}},
		{name: "before the decision it touches is kept, a proposal keeps",
			setup: func(w *bulkWorld) {
				w.proposeDecision("q", "Background jobs run on River.", "job queue")
				w.proposeDecision("r", "Background jobs run on Sidekiq instead.", "job queue")
			},
			items: []bulkSpec{{name: "r"}, {name: "q"}}},
		{name: "keeping an update supersedes the decision, once",
			setup: func(w *bulkWorld) {
				w.decide("d", "Deploy the v2 API to Railway.", "deploy target")
				w.proposeDecision("p", "Deploy the v2 API to Fly.io in iad and ams.", "deploy target")
				w.judged("p", ledger.OutcomeLinked, "d")
				w.proposeDecision("p2", "Deploy the v2 API to Fly.io in sjc.", "deploy target")
				w.judged("p2", ledger.OutcomeLinked, "d")
			},
			items: []bulkSpec{{name: "p", version: 1}, {name: "p2"}, {name: "d"}}},
		{name: "a decision an earlier item supersedes no longer holds the next one back",
			setup: func(w *bulkWorld) {
				w.decide("d", "Deploy the v2 API to Railway.", "deploy target")
				w.propose("p", "The v2 API moves off Railway this quarter.")
				w.judged("p", ledger.OutcomeLinked, "d")
				w.proposeDecision("x", "Deploy previews to Fly.io machines.", "deploy target")
			},
			items: []bulkSpec{{name: "p"}, {name: "x"}}},
		{name: "until it is superseded, the decision holds a proposal back",
			setup: func(w *bulkWorld) {
				w.decide("d", "Deploy the v2 API to Railway.", "deploy target")
				w.propose("p", "The v2 API moves off Railway this quarter.")
				w.judged("p", ledger.OutcomeLinked, "d")
				w.proposeDecision("x", "Deploy previews to Fly.io machines.", "deploy target")
			},
			items: []bulkSpec{{name: "x"}, {name: "p"}}},
		{name: "two updates of two decisions",
			setup: func(w *bulkWorld) {
				w.decide("d1", "Deploy the v2 API to Railway.", "deploy target")
				w.decide("d2", "Background jobs run on Sidekiq.", "job queue")
				w.propose("p1", "The v2 API moves off Railway this quarter.")
				w.judged("p1", ledger.OutcomeLinked, "d1")
				w.propose("p2", "Background jobs move to River.")
				w.judged("p2", ledger.OutcomeLinked, "d2")
			},
			items: []bulkSpec{{name: "p2"}, {name: "p1"}}},
		{name: "an agent never keeps",
			actor: func(*bulkWorld) ledger.Actor { return agentFor(policy.AutonomyWrite) }, via: policy.ViaMCP,
			setup: func(w *bulkWorld) { w.propose("a", "Run tests with pnpm test.") },
			items: []bulkSpec{{name: "a"}}},
		{name: "decisions in a team space need a person on the web",
			via: policy.ViaCLI, kind: policy.SpaceTeam,
			setup: func(w *bulkWorld) {
				w.proposeDecision("q", "Background jobs run on River.", "job queue")
				w.propose("a", "Run tests with pnpm test.")
			},
			items: []bulkSpec{{name: "q"}, {name: "a"}}},
		{name: "rejects proposals, and not what isn't one", reject: true,
			setup: func(w *bulkWorld) {
				w.propose("a", "Run tests with pnpm test.")
				w.mems["kept"] = w.f.remember(w.zz, w.space, "Deploys go through Fly.")
				w.propose("b", "Commit messages are short and imperative.")
			},
			items: []bulkSpec{{name: "a"}, {name: "kept"}, {name: "b", version: 3}, {name: "b", byID: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			bulk, alone := newBulkWorld(t, c), newBulkWorld(t, c)
			for round := range 2 {
				got := bulk.f.l.ApplyBulk(context.Background(), bulk.commands(c))
				want := make([]ledger.BulkResult, len(c.items))
				for i, cmd := range alone.commands(c) {
					res, err := alone.f.l.Apply(context.Background(), cmd)
					want[i] = ledger.BulkResult{Result: res, Err: err}
				}
				for i := range c.items {
					if g, w := describeBulk(got[i]), describeBulk(want[i]); g != w {
						t.Errorf("round %d, item %d:\n bulk     %s\n one by one %s", round, i, g, w)
					}
				}
				if g, w := recordOf(bulk), recordOf(alone); g != w {
					t.Errorf("round %d: the records differ\n%s", round, lineDiff(g, w))
				}
			}
		})
	}
}

func newBulkWorld(t *testing.T, c bulkCase) *bulkWorld {
	t.Helper()
	f := newCompileFixture(t)
	zz := f.user("zz")
	kind := c.kind
	if kind == "" {
		kind = policy.SpaceProject
	}
	w := &bulkWorld{f: f, zz: zz, space: f.space(zz, kind, "memax-v2"), mems: map[string]*ledger.Memory{}}
	m := f.remember(zz, w.space, "The Brief cites this.")
	f.brief(zz, w.space, 0, demoSections(m.Ref))
	f.target(zz, w.space, ledger.TargetAgentsMD)
	c.setup(w)
	return w
}

// commands are the case's items as the handler builds them: one command
// each, sharing the envelope, with a key per item.
func (w *bulkWorld) commands(c bulkCase) []ledger.Command {
	actor := person(w.zz)
	if c.actor != nil {
		actor = c.actor(w)
	}
	via := c.via
	if via == "" {
		via = policy.ViaWeb
	}
	scope := w.f.scope(w.zz).Narrow(w.space)
	occurred := bulkOccurred
	cmds := make([]ledger.Command, len(c.items))
	for i, it := range c.items {
		m := ledger.Meta{Actor: actor, Scope: scope, Via: via, IdempotencyKey: fmt.Sprintf("bulk-%d", i),
			Reason: "Agreed in review.", OccurredAt: occurred}
		if c.reject {
			cmds[i] = &ledger.Reject{Meta: m, Memory: w.ref(it), ExpectedVersion: it.version}
		} else {
			cmds[i] = &ledger.Keep{Meta: m, Memory: w.ref(it), ExpectedVersion: it.version}
		}
	}
	return cmds
}

// bulkOccurred is when the cases' bulk items happened on the client.
var bulkOccurred = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// describeBulk is what an answer says, as a client would compare it.
func describeBulk(r ledger.BulkResult) string {
	if r.Err != nil {
		return "error: " + r.Err.Error()
	}
	res := r.Result
	s := fmt.Sprintf("%s %s/%s %q replayed=%v", res.Outcome, res.Policy.Effect, res.Policy.Code, res.Policy.Message, res.Replayed)
	if m := res.Memory; m != nil {
		s += fmt.Sprintf(" memory %s %s %s v%d flags=%v status=%q", m.Ref, m.Lifecycle, m.State, m.Version, m.Flags.Strings(), status(m))
	}
	for _, rc := range res.Receipts {
		s += fmt.Sprintf(" [%s %s v%d %s]", rc.Action, rc.ObjectRef, rc.StreamVersion, rc.Assurance)
	}
	return s
}

// recordOf is the space's record as rows a test can compare: memories,
// receipts, undo entries, idempotency records, whether the targets moved
// and their jobs.
func recordOf(w *bulkWorld) string {
	w.f.t.Helper()
	ctx := context.Background()
	var b strings.Builder
	q := func(sql string, args ...any) {
		rows, err := w.f.pool.Query(ctx, sql, args...)
		if err != nil {
			w.f.t.Fatalf("%s: %v", firstLine(sql), err)
		}
		defer rows.Close()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				w.f.t.Fatal(err)
			}
			fmt.Fprintln(&b, vals...)
		}
		if err := rows.Err(); err != nil {
			w.f.t.Fatal(err)
		}
	}
	q(`SELECT seq, lifecycle, flags, stream_version, current_version, section, COALESCE(decision ->> 'status', '')
	     FROM v2.memories WHERE space_id = $1 ORDER BY seq`, w.space)
	q(`SELECT object_ref, action, stream_version, COALESCE(assurance, ''), COALESCE(source::text, ''), COALESCE(reason, ''),
	          actor_kind, via, occurred_at = $2
	     FROM v2.receipts WHERE space_id = $1 ORDER BY seq`, w.space, bulkOccurred)
	q(`SELECT u.command, u.actor_kind, (SELECT array_agg(m.seq ORDER BY m.seq) FROM v2.memories m WHERE m.id = ANY (u.memory_ids)),
	          (SELECT array_agg(r.object_ref || ':' || r.action ORDER BY r.seq) FROM v2.receipts r WHERE r.id = ANY (u.receipt_ids)),
	          u.inverse - 'memories', jsonb_path_query_array(u.inverse, '$.memories[*].after_stream_version'),
	          jsonb_path_query_array(u.inverse, '$.memories[*].before')
	     FROM v2.undo_entries u WHERE u.space_id = $1 ORDER BY u.id`, w.space)
	q(`SELECT k.idempotency_key, k.command, k.outcome, k.policy ->> 'code', m.seq, cardinality(k.receipt_ids)
	     FROM v2.command_keys k LEFT JOIN v2.memories m ON m.id = k.object_id
	    WHERE k.space_id = $1 AND k.idempotency_key LIKE 'bulk-%' ORDER BY k.idempotency_key`, w.space)
	q(`SELECT kind, dirty_gen > 1 FROM v2.targets WHERE space_id = $1 ORDER BY kind`, w.space)
	q(`SELECT kind, count(*) FROM river_job WHERE kind = 'compile_target' GROUP BY kind`)
	return b.String()
}

// A bulk Keep of two hundred proposals writes one receipt and one undo
// entry per memory, bumps the targets once and queues their compile once.
func TestBulkKeepWritesOneReceiptPerMemory(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "The Brief cites this.")
	f.brief(zz, space, 0, demoSections(m.Ref))
	target := f.target(zz, space, ledger.TargetAgentsMD)
	before := f.get(zz, target.ID).DirtyGen
	scope := f.scope(zz).Narrow(space)
	var cmds []ledger.Command
	for i := range 200 {
		p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
			NewMemory: fact(space, fmt.Sprintf("Rule %d: keep the build green.", i))}).Memory
		cmds = append(cmds, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: p.Ref, ExpectedVersion: 1})
	}
	for i, r := range f.l.ApplyBulk(context.Background(), cmds) {
		if r.Err != nil || r.Result.Outcome != ledger.OutcomeApplied || r.Result.Memory.Lifecycle != lifecycle.Kept ||
			len(r.Result.Receipts) != 1 || r.Result.Receipts[0].Seq == 0 {
			t.Fatalf("item %d: %s", i, describeBulk(r))
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'kept' AND actor_kind = 'person'`, space); n != 201 {
		t.Errorf("%d keeps, want 201 (the Brief's memory and 200 in bulk)", n)
	}
	if n := f.count(`SELECT count(DISTINCT stream_id) FROM v2.receipts WHERE space_id = $1 AND action = 'kept' AND via = 'web'`, space); n != 201 {
		t.Errorf("keeps on %d memories, want 201", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.undo_entries WHERE space_id = $1 AND command = 'keep'`, space); n != 200 {
		t.Errorf("%d undo entries, want one per Keep", n)
	}
	if got := f.get(zz, target.ID).DirtyGen; got != before+1 {
		t.Errorf("dirty_gen %d → %d, want one bump for the bulk", before, got)
	}
	if n := f.jobs(target.ID); n != 1 {
		t.Errorf("%d compile jobs, want 1", n)
	}
	// Each Keep undoes on its own, by its receipt.
	res := f.l.ApplyBulk(context.Background(), cmds[:1])[0]
	if !res.Result.Replayed {
		t.Fatalf("the retry: %s", describeBulk(res))
	}
	undo := f.apply(&ledger.Undo{Meta: meta(person(zz), scope, policy.ViaWeb), Receipt: res.Result.Receipts[0].ID})
	if len(undo.Memories) != 1 || undo.Memories[0].Lifecycle != lifecycle.Proposed {
		t.Errorf("undo: %+v", undo.Memories)
	}
}

// A memory another transaction holds is left out of the bulk's
// transaction, which never waits for it, and kept once it is free. A
// single command that holds one of the bulk's memories and then needs
// another of them doesn't deadlock with it.
func TestBulkKeepNeverWaitsForAMemory(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz).Narrow(space)
	var refs []*ledger.Memory
	for i := range 5 {
		refs = append(refs, f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
			NewMemory: fact(space, fmt.Sprintf("Rule %d: keep the build green.", i))}).Memory)
	}
	held, other := refs[3], refs[1]
	sp, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		t.Fatal(err)
	}
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- f.asV2([]uuid.UUID{space}, sp.TenantIDs(), func(tx pgx.Tx) error {
			ctx := context.Background()
			if _, err := tx.Exec(ctx, `SELECT 1 FROM v2.memories WHERE id = $1 FOR UPDATE`, held.ID); err != nil {
				return err
			}
			close(locked)
			<-release
			// Now it needs another memory of the bulk, as a Keep that
			// supersedes one, or a Forget of what carries one, would.
			_, err := tx.Exec(ctx, `SELECT 1 FROM v2.memories WHERE id = $1 FOR UPDATE`, other.ID)
			return err
		})
	}()
	<-locked
	var cmds []ledger.Command
	for _, m := range refs {
		cmds = append(cmds, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1})
	}
	var results []ledger.BulkResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = f.l.ApplyBulk(context.Background(), cmds)
	}()
	// The bulk keeps the four it could lock while the fifth is held …
	deadline := time.Now().Add(10 * time.Second)
	for f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'kept'`, space) < 4 {
		if time.Now().After(deadline) {
			t.Fatal("the bulk didn't keep the memories it could lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// … then the holder takes one of those, and lets go.
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the other transaction: %v", err)
	}
	wg.Wait()
	for i, r := range results {
		if r.Err != nil || r.Result.Outcome != ledger.OutcomeApplied {
			t.Errorf("item %d: %s", i, describeBulk(r))
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'kept'`, space); n != 5 {
		t.Errorf("%d kept, want 5", n)
	}
	if !slices.ContainsFunc(results, func(r ledger.BulkResult) bool { return r.Result.Memory != nil && r.Result.Memory.ID == held.ID }) {
		t.Error("the held memory wasn't kept")
	}
}

// lineDiff lists the lines of got and want that differ, by position.
func lineDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	var b strings.Builder
	for i := range max(len(g), len(w)) {
		var x, y string
		if i < len(g) {
			x = g[i]
		}
		if i < len(w) {
			y = w[i]
		}
		if x != y {
			fmt.Fprintf(&b, "line %d\n  got  %s\n  want %s\n", i, x, y)
		}
	}
	return b.String()
}
