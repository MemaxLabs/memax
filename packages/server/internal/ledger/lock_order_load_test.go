package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// TestLockOrderUnderLoad runs Forget beside Keep (one and in bulk), the
// judge's verdicts, compile records, Dream's editions, Verify, Remember and
// Propose, edits, Brief revisions and conflict settlements, on one space's
// few memories and
// targets, from eight goroutines at once for a fixed number of commands
// each. A command may refuse or clash, as the record has moved under it
// (the answers an API gives as 4xx); no command may fail any other way (a
// 500), and none may be aborted for a deadlock or a serialization failure:
// Apply's retry would hide those, so the test reads its log for them.
// Commands that found the memories they lock moved (locks_moved) and ran
// again are expected, and counted.
func TestLockOrderUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("a load test")
	}
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	// The run's own ledger: a pool for every goroutine (the fixture's has
	// four connections), and a log the test reads.
	cfg := f.pool.Config().Copy()
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	logs := &syncBuffer{}
	l := ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(logs, nil))), ledger.WithJobs(f.inserter))

	// A small record, so the commands keep meeting: decisions and facts,
	// a Brief placing some, three targets, proposals that cite, update and
	// contradict them.
	var decisions, facts []*ledger.Memory
	for i := range 4 {
		decisions = append(decisions, f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb),
			NewMemory: decisionIn(space, fmt.Sprintf("Deploy service %d to Fly.io.", i), fmt.Sprintf("deploy %d", i))}).Memory)
	}
	for i := range 6 {
		facts = append(facts, f.remember(zz, space, fmt.Sprintf("Convention %d: name branches after the ticket.", i)))
	}
	f.brief(zz, space, 0, demoSections(facts[0].Ref, facts[1].Ref, decisions[0].Ref))
	targets := []*ledger.Target{f.target(zz, space, ledger.TargetAgentsMD), f.target(zz, space, ledger.TargetClaudeMD),
		f.target(zz, space, ledger.TargetChatGPT)}

	type row struct {
		id      uuid.UUID
		ref     string
		version int
	}
	pick := func(where string) *row {
		var r row
		var seq int64
		err := pool.QueryRow(ctx, `SELECT id, seq, current_version FROM v2.memories WHERE space_id = $1 AND `+where+
			` ORDER BY random() LIMIT 1`, space).Scan(&r.id, &seq, &r.version)
		if err != nil {
			return nil
		}
		r.ref = ledger.FormatRef(ledger.PrefixMemory, seq)
		return &r
	}
	const (
		kept     = `lifecycle = 'kept'`
		proposed = `lifecycle = 'proposed'`
		flagged  = `lifecycle = 'proposed' AND 'conflict' = ANY (flags)`
		inForce  = `lifecycle = 'kept' AND kind = 'decision' AND COALESCE(decision ->> 'status', '') IN ('', 'in_force')`
	)
	person := func() ledger.Meta { return meta(person(zz), scope, policy.ViaWeb) }
	agent := func(level policy.Autonomy) ledger.Meta { return meta(agentFor(level), scope, policy.ViaMCP) }
	memax := func() ledger.Meta {
		s, err := l.SpaceScope(ctx, space)
		if err != nil {
			t.Error(err)
		}
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: s, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()}
	}
	dream := func() ledger.Meta {
		m := memax()
		m.Actor = ledger.Actor{Kind: policy.ActorDream}
		return m
	}

	// Each operation is a command (or two) on what the record holds now.
	type op struct {
		name string
		run  func(rng *rand.Rand, n int) error
	}
	ops := []op{
		{"remember", func(rng *rand.Rand, n int) error {
			_, err := l.Apply(ctx, &ledger.Remember{Meta: person(), NewMemory: fact(space, fmt.Sprintf("Fact %d: retries back off.", n))})
			return err
		}},
		{"propose", func(rng *rand.Rand, n int) error {
			k := pick(kept)
			if k == nil {
				return nil
			}
			switch rng.IntN(3) {
			case 0: // a proposal citing it: a Forget of it carries the proposal
				nm := fact(space, fmt.Sprintf("Proposal %d builds on %s.", n, k.ref))
				nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceMemory, Ref: k.ref}}
				_, err := l.Apply(ctx, &ledger.Propose{Meta: agent(policy.AutonomyPropose), NewMemory: nm})
				return err
			case 1: // an update of it, which a Keep supersedes it with
				_, err := l.Apply(ctx, &ledger.Edit{Meta: agent(policy.AutonomyWrite), Memory: k.ref, ExpectedVersion: k.version,
					Statement: fmt.Sprintf("Update %d of %s.", n, k.ref)})
				return err
			default: // a decision that may contradict one in force
				_, err := l.Apply(ctx, &ledger.Propose{Meta: agent(policy.AutonomyPropose),
					NewMemory: decisionIn(space, fmt.Sprintf("Deploy service %d to Railway instead.", rng.IntN(4)), fmt.Sprintf("deploy %d", rng.IntN(4)))})
				return err
			}
		}},
		{"keep", func(rng *rand.Rand, n int) error {
			p := pick(proposed + ` AND NOT 'conflict' = ANY (flags)`)
			if p == nil {
				return nil
			}
			_, err := l.Apply(ctx, &ledger.Keep{Meta: person(), Memory: p.ref})
			return err
		}},
		{"verdict", func(rng *rand.Rand, n int) error {
			p := pick(proposed)
			if p == nil {
				return nil
			}
			outcome, rel, target := ledger.OutcomeLinked, ledger.RelationUpdates, pick(kept)
			if rng.IntN(2) == 0 {
				outcome, rel, target = ledger.OutcomeFlagged, ledger.RelationContradicts, pick(inForce)
			}
			if target == nil {
				return nil
			}
			// As the judge records it: one verdict per version (a later one
			// for the same version changes nothing).
			_, err := l.Apply(ctx, &ledger.RecordVerdict{Meta: memax(), Memory: p.id, Version: p.version, Mode: ledger.JudgeProposal,
				Outcome: outcome, Target: target.id, Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: rel, Related: target.id}})
			return err
		}},
		{"forget", func(rng *rand.Rand, n int) error {
			k := pick(kept)
			if k == nil {
				return nil
			}
			cmd := &ledger.Forget{Meta: person(), Memory: k.ref, ExpectedVersion: k.version}
			_, err := l.Apply(ctx, cmd)
			var ce *ledger.ForgetCarriesError
			if errors.As(err, &ce) {
				again := &ledger.Forget{Meta: person(), Memory: k.ref, ExpectedVersion: k.version}
				for _, c := range ce.Carries {
					again.Carries = append(again.Carries, c.Ref)
				}
				_, err = l.Apply(ctx, again)
			}
			return err
		}},
		{"compile", func(rng *rand.Rand, n int) error {
			tg := targets[rng.IntN(len(targets))]
			var refs []string
			if k := pick(kept); k != nil {
				refs = append(refs, k.ref)
			}
			cmd := f.recordCompileCmd(tg, fmt.Sprintf("compiled %d", n), refs...)
			_, err := l.Apply(ctx, cmd)
			return err
		}},
		{"dream", func(rng *rand.Rand, n int) error {
			x, y, s, p1, p2 := pick(kept), pick(kept), pick(kept), pick(proposed), pick(proposed)
			var actions []ledger.PlannedAction
			if x != nil && y != nil && x.id != y.id {
				actions = append(actions, ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: x.id, Version: x.version,
					Related: y.id, RelatedVersion: y.version})
			}
			if s != nil {
				past := time.Now().Add(-time.Hour)
				actions = append(actions, ledger.PlannedAction{Kind: ledger.DreamStale, Memory: s.id, Version: s.version, StaleAfter: &past})
				old := time.Now().Add(-90 * 24 * time.Hour)
				actions = append(actions, ledger.PlannedAction{Kind: ledger.DreamFade, Memory: s.id, Version: s.version, Unread: &old})
			}
			if p1 != nil && p2 != nil && p1.id != p2.id {
				actions = append(actions, ledger.PlannedAction{Kind: ledger.DreamDedupe, Memory: p2.id, Version: p2.version,
					Related: p1.id, RelatedVersion: p1.version})
			}
			// Dream's actions in an order of its own, not the memories'.
			rng.Shuffle(len(actions), func(i, j int) { actions[i], actions[j] = actions[j], actions[i] })
			now := time.Now()
			_, err := l.Apply(ctx, &ledger.PublishEdition{Meta: dream(), SpaceID: space, Slot: now.Add(time.Duration(n) * time.Microsecond),
				Trigger: ledger.DreamScheduled, Until: now, StartedAt: now.Add(-time.Second), Actions: actions})
			return err
		}},
		{"settle", func(rng *rand.Rand, n int) error {
			p := pick(flagged)
			if p == nil {
				return nil
			}
			choice := []ledger.ConflictChoice{ledger.ChooseThis, ledger.ChooseOther}[rng.IntN(2)]
			_, err := l.Apply(ctx, &ledger.ResolveConflict{Meta: person(), Memory: p.ref, Choice: choice})
			return err
		}},
		{"brief", func(rng *rand.Rand, n int) error {
			b, err := l.GetBrief(ctx, scope, space)
			if err != nil {
				return err
			}
			a, c := pick(kept), pick(kept)
			if a == nil || c == nil || a.id == c.id {
				return nil
			}
			_, err = l.Apply(ctx, &ledger.ReviseBrief{Meta: person(), SpaceID: space, ExpectedVersion: b.Version, Title: "Memax V2",
				Sections: demoSections(a.ref, c.ref)})
			return err
		}},
		{"verify", func(rng *rand.Rand, n int) error {
			// Dream's stale flags (above) give it something to answer.
			k := pick(kept + ` AND 'stale' = ANY (flags)`)
			if k == nil {
				return nil
			}
			v := &ledger.Verify{Meta: person(), Memory: k.ref, ExpectedVersion: k.version, Answer: ledger.VerifyStillTrue}
			if rng.IntN(2) == 0 {
				v.Answer, v.Statement = ledger.VerifyUpdate, fmt.Sprintf("Checked %d: name branches after the ticket.", n)
			}
			_, err := l.Apply(ctx, v)
			return err
		}},
		{"bulk keep", func(rng *rand.Rand, n int) error {
			// One transaction for several Keeps, which never waits for a
			// memory (SKIP LOCKED) but does for the targets.
			var cmds []ledger.Command
			for range 3 {
				if p := pick(proposed + ` AND NOT 'conflict' = ANY (flags)`); p != nil {
					cmds = append(cmds, &ledger.Keep{Meta: person(), Memory: p.ref})
				}
			}
			for _, r := range l.ApplyBulk(ctx, cmds) {
				if r.Err != nil {
					return r.Err
				}
			}
			return nil
		}},
		{"edit", func(rng *rand.Rand, n int) error {
			k := pick(kept)
			if k == nil {
				return nil
			}
			_, err := l.Apply(ctx, &ledger.Edit{Meta: person(), Memory: k.ref, ExpectedVersion: k.version,
				Statement: fmt.Sprintf("Edited %d: name branches after the ticket.", n)})
			return err
		}},
	}

	// The refusals the record gives a command it moved under: none is a
	// 500. A Keep before the judge's verdict is busy by design.
	allowed := func(err error) bool {
		var pending *ledger.JudgePendingError
		return err == nil || errors.As(err, &pending) ||
			errors.Is(err, ledger.ErrEditClash) || errors.Is(err, ledger.ErrInvalidTransition) || errors.Is(err, ledger.ErrNotFound) ||
			errors.Is(err, ledger.ErrForgetCarries) || errors.Is(err, ledger.ErrBehind) || errors.Is(err, ledger.ErrInvalid)
	}

	const workers, perWorker = 8, 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	ran := map[string]int{}
	for g := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(g), 42))
			for i := range perWorker {
				o := ops[(g+i)%len(ops)]
				n := g*perWorker + i + 1
				err := o.run(rng, n)
				mu.Lock()
				ran[o.name]++
				mu.Unlock()
				if !allowed(err) {
					t.Errorf("%s (worker %d, #%d): %v", o.name, g, i, err)
				}
			}
		}()
	}
	// Every lock wait ends within the ledger's lock timeout (5 s), and the
	// run is a few seconds of commands on four CPUs: two minutes is a guard
	// against a hang, not a budget.
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Minute):
		t.Fatalf("the load didn't finish within two minutes: something waits forever")
	}

	out := logs.String()
	retried := map[string]int{}
	for _, m := range regexp.MustCompile(`msg="ledger: retrying a command" command=(\S+) code=(\S+)`).FindAllStringSubmatch(out, -1) {
		retried[m[1]+" "+m[2]]++
		if m[2] != "locks_moved" {
			t.Errorf("%s was aborted (%s) and ran again: the lock order has a cycle", m[1], m[2])
		}
	}
	if strings.Contains(out, "failed on every attempt") {
		t.Errorf("a command ran out of attempts:\n%s", out)
	}
	t.Logf("ran %v; retried %v", ran, retried)
}
