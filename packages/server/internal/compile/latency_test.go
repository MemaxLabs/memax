package compile_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// One compile run's database work over a simulated network
// (internal/testdb/netsim): reading the target and its input, recording
// the run, with the in-process fake compiler and an in-memory object
// store. TestCompileRoundTrips guards its round trips in CI;
// TestCompileLatency (MEMAX_LATENCY=1) times it at 0 and 24 ms.

type compileRig struct {
	f      *fixture
	db     *testdb.DB
	s      *seeded
	agents *ledger.Target
	n      int
}

func newCompileRig(t *testing.T, o testdb.Options) *compileRig {
	t.Helper()
	db := testdb.Open(t, o)
	f := newFixture(t, fixtureOpts{db: db})
	s := f.seed()
	for i := range 60 {
		f.remember(s.owner, s.space, fmt.Sprintf("Convention %d: name queues after what they carry.", i), ledger.SectionConventions)
	}
	return &compileRig{f: f, db: db, s: s, agents: s.targets[ledger.TargetAgentsMD]}
}

// measure keeps a new memory (the target is dirty again) and compiles it.
func (r *compileRig) measure(t *testing.T) (time.Duration, *netsim.Counter) {
	t.Helper()
	r.n++
	r.f.remember(r.s.owner, r.s.space, fmt.Sprintf("Releases are tagged on day %d.", r.n), ledger.SectionConventions)
	ctx, c := netsim.Track(context.Background())
	start := time.Now()
	out, err := r.f.svc.Run(ctx, ledger.CompileTargetArgs{TargetID: r.agents.ID, SpaceID: r.s.space}, compile.RunOptions{NoWait: true})
	took := time.Since(start)
	if err != nil || out != compile.Done {
		t.Fatalf("compile: %v %v", out, err)
	}
	if !netsim.Settle(r.db.Pool, 2*time.Second) {
		t.Fatal("connections still checked out after 2 s")
	}
	return took, c
}

// TestCompileLatency times one compile run's database work at 0 and 24 ms
// of round-trip time (MEMAX_LATENCY=1): its share of Keep → target's
// 10 s (N1).
func TestCompileLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newCompileRig(t, testdb.Options{Proxy: true})
	var rows []string
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		r.db.Proxy.SetOneWay(rtt / 2)
		for range 2 {
			r.measure(t)
		}
		s := &netsim.Sample{Name: "compile run (database work)", RTT: rtt}
		for range 15 {
			took, c := r.measure(t)
			s.Add(took, c.RoundTrips())
		}
		rows = append(rows, s.Row())
	}
	t.Logf("\n%s\n%s\n%s", netsim.Header, rows[0], rows[1])
}

// TestCompileRoundTrips guards the round trips of one compile run's
// database work.
func TestCompileRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	r := newCompileRig(t, testdb.Options{Watch: audit.Observe})
	audit.Arm()
	defer audit.Require(t)
	for range 2 {
		r.measure(t)
	}
	_, c := r.measure(t)
	t.Logf("compile run: %d round trips\n%s", c.RoundTrips(), c)
	// 30 before the pipelined ledger (Oct 7, 2026).
	if got, budget := c.RoundTrips(), 24; got > budget {
		t.Errorf("compile run: %d round trips, budget %d\n%s", got, budget, c)
	}
}
