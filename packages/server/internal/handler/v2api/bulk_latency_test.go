package v2api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// The bulk paths over a simulated network: an import (`memax init`'s
// upload, and the V1 switch's) and a bulk Keep, at three sizes each.
// TestBulkRoundTrips holds each to a budget that doesn't grow with the
// number of items, in CI; TestBulkLatency (MEMAX_LATENCY=1) times them at
// 0 and 24 ms.

// bulkRig is a person's session on a space with an AGENTS.md target (so a
// Keep queues its compile), index jobs on, and kept memories for an
// import's repeat check to look through. Proposals to keep come from
// imports, as `memax init` makes them.
type bulkRig struct {
	e         *env
	db        *testdb.DB
	sp        space
	tok       string
	seq       int
	proposals []string
}

func newBulkRig(t *testing.T, o testdb.Options) *bulkRig {
	t.Helper()
	db := testdb.Open(t, o)
	if db.Proxy != nil {
		db.Proxy.SetOneWay(0) // set up at full speed, whatever TEST_DB_RTT says
	}
	e := newEnvOn(t, db, func(*env) []v2api.Option { return nil }, ledger.WithIndexJobs())
	zz := e.user("zz")
	e.space(zz, policy.SpacePersonal, "zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	r := &bulkRig{e: e, db: db, sp: sp, tok: e.session(zz)}

	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	meta := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: zz}, Scope: scope, Via: policy.ViaWeb,
			IdempotencyKey: uuid.NewString()}
	}
	for i := range 100 {
		if _, err := e.ledger.Apply(ctx, &ledger.Remember{Meta: meta(), NewMemory: ledger.NewMemory{SpaceID: sp.id,
			Statement: bulkStatement("kept", i), Section: ledger.SectionConventions}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.ledger.Apply(ctx, &ledger.ReviseBrief{Meta: meta(), SpaceID: sp.id, Title: "memax-v2",
		Sections: []ledger.BriefSection{{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: "M-0001"}}}}}); err != nil {
		t.Fatal(err)
	}
	if res, err := e.ledger.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(), SpaceID: sp.id, Kind: ledger.TargetAgentsMD}); err != nil || res.Target == nil {
		t.Fatalf("configure target: %v %+v", err, res.Policy)
	}
	return r
}

// bulkWords make statements that share few five-letter shingles, as the
// lines of real agent files do, so the repeat check finds no near repeats.
var bulkWords = strings.Fields("deploy target postgres migrations review queue rate limits lighthouse fly machines " +
	"session compile budget staging database token audience agent proposal receipt brief section convention " +
	"river worker index embed vector search recall cursor codex gemini claude hook daemon socket proxy cookie " +
	"passkey export forget tombstone seal chain merkle signature rollout canary flag cohort metric gate funnel " +
	"lint format prettier vitest playwright turbo pnpm workspace docker compose redis neon cloudflare wrangler")

// bulkStatement is a distinct statement: a number differs between any two,
// which the judge's stage 0 never folds.
func bulkStatement(run string, i int) string {
	h := uint64(len(run)*1_000_003 + i*7919 + 17)
	var w []string
	for range 7 {
		h = h*6364136223846793005 + 1442695040888963407
		w = append(w, bulkWords[(h>>33)%uint64(len(bulkWords))])
	}
	return fmt.Sprintf("%s %s %s %s with %s %s and %s (%s rule %d).", w[0], w[1], w[2], w[3], w[4], w[5], w[6], run, i)
}

// importItems is an upload of n new statements.
func (r *bulkRig) importItems(n int) map[string]any {
	r.seq++
	run := "r" + strconv.Itoa(r.seq)
	items := make([]map[string]any, n)
	for i := range n {
		ref := fmt.Sprintf("CLAUDE.md:%d", i+1)
		items[i] = importLine(fmt.Sprintf("%s-%d", run, i), ref, bulkStatement(run, i))
	}
	return importBody(items...)
}

// stock imports proposals for the bulk Keeps to keep.
func (r *bulkRig) stock(t *testing.T, n int) {
	t.Helper()
	for n > 0 {
		k := min(n, ledger.MaxImportItems)
		var out importOut
		r.e.do(call{method: "POST", path: "/v2/spaces/" + r.sp.slug + "/imports", token: r.tok, body: r.importItems(k),
			header: map[string]string{"X-Memax-Via": "cli"}}).ok(http.StatusCreated, &out)
		for _, it := range out.Items {
			if it.Outcome != "proposed" {
				t.Fatalf("stock: %s %s", it.Key, it.Outcome)
			}
			r.proposals = append(r.proposals, it.Memory.Ref)
		}
		n -= k
	}
}

func (r *bulkRig) take(t *testing.T, n int) []map[string]any {
	t.Helper()
	if len(r.proposals) < n {
		t.Fatalf("out of proposals: %d left, want %d", len(r.proposals), n)
	}
	items := make([]map[string]any, n)
	for i, ref := range r.proposals[:n] {
		items[i] = map[string]any{"memory": ref, "version": 1}
	}
	r.proposals = r.proposals[n:]
	return items
}

type bulkOp struct {
	name string
	// keeps is how many proposals one run keeps (0 for an import).
	keeps int
	run   func(t *testing.T, ctx context.Context) time.Duration
}

func (r *bulkRig) ops(importSizes, keepSizes []int) []bulkOp {
	var ops []bulkOp
	for _, n := range importSizes {
		ops = append(ops, bulkOp{name: fmt.Sprintf("/v2 import of %d", n), run: func(t *testing.T, ctx context.Context) time.Duration {
			body := r.importItems(n)
			start := time.Now()
			var out importOut
			r.e.do(call{method: "POST", path: "/v2/spaces/" + r.sp.slug + "/imports", token: r.tok, ctx: ctx, body: body,
				header: map[string]string{"X-Memax-Via": "cli"}}).ok(http.StatusCreated, &out)
			took := time.Since(start)
			if out.Import.Counts.Proposed != n {
				t.Fatalf("import of %d: counts %+v", n, out.Import.Counts)
			}
			return took
		}})
	}
	for _, n := range keepSizes {
		ops = append(ops, bulkOp{name: fmt.Sprintf("/v2 bulk keep of %d (+ jobs)", n), keeps: n,
			run: func(t *testing.T, ctx context.Context) time.Duration {
				items := r.take(t, n)
				start := time.Now()
				var out struct{ Applied, Refused, Failed int }
				r.e.do(call{method: "POST", path: "/v2/spaces/" + r.sp.slug + "/memories:keep", token: r.tok, ctx: ctx,
					body: map[string]any{"items": items}, header: map[string]string{"X-Memax-Via": "cli"}}).ok(http.StatusOK, &out)
				took := time.Since(start)
				if out.Applied != n {
					t.Fatalf("bulk keep of %d: %+v", n, out)
				}
				return took
			}})
	}
	return ops
}

// measure runs op once with a counter, then waits for what it left running.
func (r *bulkRig) measure(t *testing.T, op bulkOp) (time.Duration, *netsim.Counter) {
	t.Helper()
	ctx, c := netsim.Track(context.Background())
	r.db.Trips.SetFallback(c)
	defer r.db.Trips.SetFallback(nil)
	took := op.run(t, ctx)
	r.e.h.Wait()
	if !netsim.Settle(r.db.Pool, 5*time.Second) {
		t.Fatalf("%s: connections still checked out after 5 s", op.name)
	}
	return took, c
}

// TestBulkRoundTrips guards the round trips of an import and a bulk Keep:
// the budget is the same at every size, so a change that makes either
// grow with its items fails here. Counts are steady state (warm statement
// caches) and include the auth middleware's and River's. The wire audit
// checks every statement they send: none touches a v2 table before its
// transaction's scope, or as another role.
func TestBulkRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	r := newBulkRig(t, testdb.Options{Watch: audit.Observe})
	importSizes, keepSizes := []int{1, 50, 500}, []int{1, 50, 200}
	r.stock(t, 3*(1+50+200))
	audit.Arm()
	defer audit.Require(t)
	// One statement at a time (until Oct 11, 2026), an import cost 26, 668
	// and 6,579 round trips at these sizes, and a bulk Keep 12, 506 and
	// 2,017. Now each is one transaction: the auth middleware's two reads,
	// one round trip of reads, one of writes, River's insert (and its
	// notification, at most once per queue per fetch cooldown) and COMMIT.
	budgets := map[string]int{}
	for _, n := range importSizes {
		budgets[fmt.Sprintf("/v2 import of %d", n)] = 7
	}
	for _, n := range keepSizes {
		budgets[fmt.Sprintf("/v2 bulk keep of %d (+ jobs)", n)] = 7
	}
	for _, op := range r.ops(importSizes, keepSizes) {
		for range 2 {
			r.measure(t, op)
		}
		_, c := r.measure(t, op)
		t.Logf("%s: %d round trips\n%s", op.name, c.RoundTrips(), c)
		if want, ok := budgets[op.name]; ok && c.RoundTrips() > want {
			t.Errorf("%s: %d round trips, budget %d\n%s", op.name, c.RoundTrips(), want, c)
		}
	}
}

// TestBulkLatency times an import and a bulk Keep at three sizes each, at
// 0 and 24 ms of round-trip time (MEMAX_LATENCY=1), and prints a table.
// MEMAX_BULK_RUNS sets the runs per row (default 3, after one warm-up).
func TestBulkLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("MEMAX_BULK_RUNS")); err == nil && v > 0 {
		runs = v
	}
	r := newBulkRig(t, testdb.Options{Proxy: true})
	importSizes, keepSizes := []int{1, 50, 500}, []int{1, 50, 200}
	keeps := 0
	for _, n := range keepSizes {
		keeps += n
	}
	r.stock(t, 2*(1+runs)*keeps)
	var rows []string
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		r.db.Proxy.SetOneWay(rtt / 2)
		for _, op := range r.ops(importSizes, keepSizes) {
			r.measure(t, op) // warm the statement caches
			s := &netsim.Sample{Name: op.name, RTT: rtt}
			for range runs {
				took, c := r.measure(t, op)
				s.Add(took, c.RoundTrips())
			}
			rows = append(rows, s.Row())
			t.Logf("%s", s.Row())
		}
	}
	r.db.Proxy.SetOneWay(0)
	t.Logf("\n%s\n%s", netsim.Header, strings.Join(rows, "\n"))
}
