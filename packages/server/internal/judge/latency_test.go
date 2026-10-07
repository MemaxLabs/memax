package judge_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// One judge job's database work over a simulated network
// (internal/testdb/netsim): the space's scope, the snapshot, and the
// verdict, with a model that finds every candidate unrelated (so the
// proposal stays in Review). TestJudgeRoundTrips guards its round trips in
// CI; TestJudgeLatency (MEMAX_LATENCY=1) times it at 0 and 24 ms.

type judgeRig struct {
	f  *fixture
	db *testdb.DB
	j  *judge.Judge
	zz uuid.UUID
	sp uuid.UUID
	n  int
}

func newJudgeRig(t *testing.T, o testdb.Options) *judgeRig {
	t.Helper()
	db := testdb.Open(t, o)
	f := newFixtureOn(t, db)
	r := &judgeRig{f: f, db: db, zz: f.user("zz")}
	r.sp = f.space(r.zz, "memax-v2")
	for i := range 40 {
		f.kept(r.zz, r.sp, fact(fmt.Sprintf("Workers retry failed jobs %d times with backoff in queue %d.", i+2, i)))
	}
	r.j = withModel(f, &fakeModel{answer: oracle(nil)}, judge.Config{Primary: judge.Tier{Model: "fake-primary"}})
	return r
}

// next proposes a new fact and returns its judge job.
func (r *judgeRig) next() ledger.JudgeArgs {
	r.n++
	m := r.f.propose(r.zz, r.sp, fact(fmt.Sprintf("Workers retry failed jobs %d times before paging (%d).", r.n+100, r.n)))
	return r.f.judgeJob(m, 1)
}

func (r *judgeRig) measure(t *testing.T) (time.Duration, *netsim.Counter) {
	t.Helper()
	args := r.next()
	ctx, c := netsim.Track(context.Background())
	start := time.Now()
	run, err := r.j.Run(ctx, args, judge.RunOptions{EnqueuedAt: time.Now()})
	took := time.Since(start)
	if err != nil || run.Skipped || run.Candidates == 0 {
		t.Fatalf("judge: %v %+v", err, run)
	}
	if !netsim.Settle(r.db.Pool, 2*time.Second) {
		t.Fatal("connections still checked out after 2 s")
	}
	return took, c
}

// TestJudgeLatency times one judge job's database work at 0 and 24 ms of
// round-trip time (MEMAX_LATENCY=1). The verdict's budget is 5 s; the
// database's share of it is what this shows.
func TestJudgeLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newJudgeRig(t, testdb.Options{Proxy: true})
	var rows []string
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		r.db.Proxy.SetOneWay(rtt / 2)
		for range 2 {
			r.measure(t)
		}
		s := &netsim.Sample{Name: "judge job (database work)", RTT: rtt}
		for range 15 {
			took, c := r.measure(t)
			s.Add(took, c.RoundTrips())
		}
		rows = append(rows, s.Row())
	}
	t.Logf("\n%s\n%s\n%s", netsim.Header, rows[0], rows[1])
}

// TestJudgeRoundTrips guards the round trips of one judge job's database
// work: the scope, the snapshot and the verdict.
func TestJudgeRoundTrips(t *testing.T) {
	r := newJudgeRig(t, testdb.Options{})
	for range 2 {
		r.measure(t)
	}
	_, c := r.measure(t)
	t.Logf("judge job: %d round trips\n%s", c.RoundTrips(), c)
	if got, budget := c.RoundTrips(), 99; got > budget {
		t.Errorf("judge job: %d round trips, budget %d\n%s", got, budget, c)
	}
}
