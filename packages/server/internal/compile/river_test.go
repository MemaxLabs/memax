package compile_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, within time.Duration, what string, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: not within %v", what, within)
	return 0
}

// dumpJobs logs the compile jobs, for a failure.
func (f *fixture) dumpJobs(t *testing.T) {
	rows, err := f.pool.Query(context.Background(), `SELECT id, state, attempt, args::text, coalesce(errors::text, ''), metadata::text FROM river_job WHERE queue = 'compile' ORDER BY id`)
	if err != nil {
		t.Log(err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var state, args, errs, meta string
		var attempt int
		_ = rows.Scan(&id, &state, &attempt, &args, &errs, &meta)
		t.Logf("job %d %s attempt %d %s errors=%s meta=%s", id, state, attempt, args, errs, meta)
	}
}

// settled reports whether every target compiled its latest generation.
func (f *fixture) settled(s *seeded) bool {
	for _, tg := range s.targets {
		g := f.get(s.owner, tg.ID)
		if g.CompiledGen < g.DirtyGen {
			return false
		}
	}
	return true
}

// The compile path through River with production settings (the 1.5 s
// quiet window, River's default polling): a Keep's InsertTx-ed jobs
// recompile every target of the space, well inside N1's 10 s.
func TestKeepRecompilesEveryTargetThroughRiver(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{workers: true})
	s := f.seed()
	waitFor(t, 15*time.Second, "the first compile of every target", func() bool { return f.settled(s) })

	codex := ledger.Actor{Kind: policy.ActorAgent, ID: s.owner, Agent: "codex", Autonomy: policy.AutonomyPropose}
	p := f.apply(&ledger.Propose{Meta: ledger.Meta{Actor: codex, Scope: f.scope(s.owner), Via: policy.ViaMCP, IdempotencyKey: "p1"},
		NewMemory: ledger.NewMemory{SpaceID: s.space, Statement: "MCP write tools ask with input_required.", Section: ledger.SectionConventions}}).Memory
	keep := f.apply(&ledger.Keep{Meta: f.meta(s.owner, policy.ViaReview), Memory: p.Ref})
	kept := keep.Receipts[0].RecordedAt
	t.Cleanup(func() {
		if t.Failed() {
			f.dumpJobs(t)
		}
	})
	took := waitFor(t, 10*time.Second, "every target after the keep", func() bool { return f.settled(s) })
	agents := f.get(s.owner, s.targets[ledger.TargetAgentsMD].ID)
	if !contains(agents.LastCompile.Refs, p.Ref) {
		t.Errorf("AGENTS.md doesn't carry %s: %v", p.Ref, agents.LastCompile.Refs)
	}
	sinceReceipt := agents.LastCompile.CompiledAt.Sub(kept)
	t.Logf("keep → every target compiled: %v (receipt → AGENTS.md compiled: %v)", took.Round(time.Millisecond), sinceReceipt.Round(time.Millisecond))
	if took > 10*time.Second {
		t.Errorf("N1: %v", took)
	}
}

// A job that finds its target dirtied while compiling snoozes and compiles
// again; nothing behind is recorded.
func TestCompileJobSnoozesThroughRiver(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{workers: true, cfg: compile.Config{Quiet: 50 * time.Millisecond, QuietCap: 200 * time.Millisecond}})
	s := f.seed()
	waitFor(t, 15*time.Second, "the first compile", func() bool { return f.settled(s) })
	tg := s.targets[ledger.TargetAgentsMD]
	var once atomic.Bool
	f.fake.OnCompile = func(in *compile.Input) {
		if in.Targets[0].Kind == "agents_md" && once.CompareAndSwap(false, true) {
			f.apply(&ledger.RequestCompile{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID})
		}
	}
	runs := len(f.runs(tg))
	f.apply(&ledger.ReviseBrief{Meta: f.meta(s.owner, policy.ViaWeb), SpaceID: s.space, ExpectedVersion: 1,
		Title: "Memax V2 engineering brief, revised", Sections: []ledger.BriefSection{
			{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: s.river.Ref}}}}})
	waitFor(t, 10*time.Second, "the revised Brief compiled", func() bool { return f.settled(s) })
	if got := len(f.runs(tg)); got != runs+1 {
		t.Errorf("runs %d → %d, want exactly one new run (the snoozed attempt records nothing)", runs, got)
	}
	var snoozes int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(max((metadata->>'snoozes')::int), 0) FROM river_job
		 WHERE kind = 'compile_target' AND args->>'target_id' = $1`, tg.ID.String()).Scan(&snoozes); err != nil {
		t.Fatal(err)
	}
	if snoozes < 1 {
		t.Errorf("the job never snoozed")
	}
}

// §5.7 step 4: the sweeper re-enqueues a target left dirty without a job.
func TestSweeperReenqueuesDirtyTargets(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{workers: true, cfg: compile.Config{Quiet: 50 * time.Millisecond, QuietCap: 200 * time.Millisecond}})
	s := f.seed()
	waitFor(t, 15*time.Second, "the first compile", func() bool { return f.settled(s) })
	tg := s.targets[ledger.TargetAgentsMD]
	// Dirty it with no job, as a crash between commit and enqueue would
	// if River weren't the outbox (or a job exhausted its attempts).
	f.exec(`UPDATE v2.targets SET dirty_gen = dirty_gen + 1, dirty_at = now(),
	               sync_state = CASE WHEN sync_state IN ('in_sync', 'pending_delivery') THEN 'compiling' ELSE sync_state END
	         WHERE id = $1`, tg.ID)
	time.Sleep(300 * time.Millisecond)
	if f.settled(s) {
		t.Fatal("fixture: the target should be behind")
	}
	if _, err := f.client.Insert(context.Background(), compile.SweepArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "the swept target compiled", func() bool { return f.settled(s) })
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
