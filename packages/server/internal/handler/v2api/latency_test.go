package v2api_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// The /v2 hot paths over a simulated network (internal/testdb/netsim), as
// the web app calls them (by slug, and a memory by display ID): the memory
// list and one memory, the review queue, the Brief, Keep and Remember
// (with their compile and index jobs enqueued), and Ask up to its sources
// event. TestAPIRoundTrips guards the round trips each makes, in CI;
// TestAPILatency (MEMAX_LATENCY=1) times them at 0 and 24 ms.

// apiRig is a person's session on a space of seeded, embedded memories
// with a Brief, an AGENTS.md target (so a Keep queues its compile), a
// stock of agent proposals to keep, and Ask on the hybrid searcher with an
// instant fake model. Index jobs are on, as in production with Voyage.
type apiRig struct {
	e         *env
	db        *testdb.DB
	sp        space
	tok       string
	mu        sync.Mutex
	proposals []string
	seq       int
}

func newAPIRig(t *testing.T, o testdb.Options) *apiRig {
	t.Helper()
	db := testdb.Open(t, o)
	words := mockembed.NewWords()
	model := instantModel{}
	e := newEnvOn(t, db, func(e *env) []v2api.Option {
		vec := v2recall.NewVectors(e.ledger, words, words, v2recall.VectorConfig{Model: embedModel, Log: quiet})
		search := v2recall.New(e.ledger).WithVectors(vec)
		return []v2api.Option{v2api.WithAsk(ask.New(e.ledger, search, model, ask.Config{Model: "test/answer-tier", Log: quiet}))}
	}, ledger.WithIndexJobs())
	zz := e.user("zz")
	e.space(zz, policy.SpacePersonal, "zz") // as every account has
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	r := &apiRig{e: e, db: db, sp: sp, tok: e.session(zz)}

	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	meta := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: zz}, Scope: scope, Via: policy.ViaWeb,
			IdempotencyKey: uuid.NewString()}
	}
	words2 := strings.Fields("deploy target postgres migrations review queue rate limits lighthouse fly machines session " +
		"compile budget staging database token audience agent proposal receipt brief section convention decision")
	sections := []ledger.Section{ledger.SectionDecisions, ledger.SectionConventions, ledger.SectionPreferences}
	for i := range 300 {
		statement := fmt.Sprintf("%s %s %s %s (%d)", words2[i%len(words2)], words2[(i*7)%len(words2)],
			words2[(i*13)%len(words2)], words2[(i*17)%len(words2)], i)
		section := sections[i%len(sections)]
		kind := ledger.KindFact
		if section == ledger.SectionDecisions {
			kind = ledger.KindDecision
		}
		if _, err := e.ledger.Apply(ctx, &ledger.Remember{Meta: meta(),
			NewMemory: ledger.NewMemory{SpaceID: sp.id, Statement: statement, Section: section, Kind: kind}}); err != nil {
			t.Fatal(err)
		}
	}
	e.indexAll(words, sp)
	if _, err := e.ledger.Apply(ctx, &ledger.ReviseBrief{Meta: meta(), SpaceID: sp.id, Title: "memax-v2",
		Sections: []ledger.BriefSection{{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: "M-0001"}, {Ref: "M-0004"}}}}}); err != nil {
		t.Fatal(err)
	}
	if res, err := e.ledger.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(), SpaceID: sp.id, Kind: ledger.TargetAgentsMD}); err != nil || res.Target == nil {
		t.Fatalf("configure target: %v %+v", err, res.Policy)
	}
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	for i := range 150 {
		res := e.remember(key, sp, fmt.Sprintf("Proposed by Codex: rotate the staging token every %d days.", i+1))
		r.proposals = append(r.proposals, res.Memory.Ref)
	}
	return r
}

// instantModel answers at once, citing the first memory it was given.
type instantModel struct{}

func (instantModel) Stream(_ context.Context, c ask.Call, onText func(string)) (ask.Usage, error) {
	ref := "M-0001"
	if i := strings.Index(c.Prompt, `<memory id="`); i >= 0 {
		ref = strings.SplitN(c.Prompt[i+len(`<memory id="`):], `"`, 2)[0]
	}
	onText("Deploys go through Fly.[" + ref + "]")
	return ask.Usage{InputTokens: 100, OutputTokens: 10}, nil
}

// apiOp is one request; run reports how long its answer took (Ask: until
// the sources event).
type apiOp struct {
	name string
	run  func(t *testing.T, ctx context.Context) time.Duration
}

func (r *apiRig) get(path string) func(t *testing.T, ctx context.Context) time.Duration {
	return func(t *testing.T, ctx context.Context) time.Duration {
		start := time.Now()
		r.e.do(call{method: "GET", path: path, token: r.tok, ctx: ctx}).ok(http.StatusOK, nil)
		return time.Since(start)
	}
}

func (r *apiRig) nextProposal(t *testing.T) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.proposals) == 0 {
		t.Fatal("out of proposals to keep")
	}
	ref := r.proposals[0]
	r.proposals = r.proposals[1:]
	return ref
}

func (r *apiRig) ops() []apiOp {
	slug := r.sp.slug
	return []apiOp{
		{"/v2 memory list", r.get("/v2/spaces/" + slug + "/memories")},
		{"/v2 memory get", r.get("/v2/memories/M-0002?space=" + slug)},
		{"/v2 review queue", r.get("/v2/spaces/" + slug + "/review")},
		{"/v2 Brief", r.get("/v2/spaces/" + slug + "/brief")},
		{"/v2 Keep (+ jobs)", func(t *testing.T, ctx context.Context) time.Duration {
			ref := r.nextProposal(t)
			start := time.Now()
			r.e.do(call{method: "POST", path: "/v2/memories/" + ref + ":keep?space=" + slug, token: r.tok, ctx: ctx,
				header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
			return time.Since(start)
		}},
		{"/v2 Remember (+ jobs)", func(t *testing.T, ctx context.Context) time.Duration {
			r.seq++
			start := time.Now()
			r.e.do(call{method: "POST", path: "/v2/spaces/" + slug + "/memories", token: r.tok, ctx: ctx,
				body: remember(fmt.Sprintf("Releases are cut on day %d of the sprint.", r.seq), "conventions")}).ok(http.StatusCreated, nil)
			return time.Since(start)
		}},
		{"/v2 Ask, to the sources event", func(t *testing.T, ctx context.Context) time.Duration {
			r.seq++
			req := r.e.request(call{method: "POST", path: "/v2/spaces/" + slug + "/ask", token: r.tok, ctx: ctx,
				body: map[string]any{"question": fmt.Sprintf("How do we deploy to fly machines? (%d)", r.seq)}})
			w := &firstEvent{ResponseRecorder: httptest.NewRecorder(), event: "event: sources", start: time.Now()}
			r.e.srv.ServeHTTP(w, req)
			if w.Code != http.StatusOK || w.at.IsZero() {
				t.Fatalf("ask: %d, sources at %v: %s", w.Code, w.at, w.Body.String())
			}
			return w.at.Sub(w.start)
		}},
	}
}

// firstEvent notes when an event stream first carries an event.
type firstEvent struct {
	*httptest.ResponseRecorder
	event string
	start time.Time
	at    time.Time
}

func (f *firstEvent) Write(b []byte) (int, error) {
	if f.at.IsZero() && bytes.Contains(b, []byte(f.event)) {
		f.at = time.Now()
	}
	return f.ResponseRecorder.Write(b)
}

// measure runs op once with a counter, then waits for what it left running.
func (r *apiRig) measure(t *testing.T, op apiOp) (time.Duration, *netsim.Counter) {
	t.Helper()
	ctx, c := netsim.Track(context.Background())
	// The auth middleware's store queries with context.Background(): those
	// trips count here too.
	r.db.Trips.SetFallback(c)
	defer r.db.Trips.SetFallback(nil)
	took := op.run(t, ctx)
	r.e.h.Wait()
	if !netsim.Settle(r.db.Pool, 2*time.Second) {
		t.Fatalf("%s: connections still checked out after 2 s", op.name)
	}
	return took, c
}

// TestAPILatency times the /v2 hot paths at 0 and 24 ms of round-trip time
// (MEMAX_LATENCY=1) and prints a table. At 24 ms, Ask's sources must come
// within 300 ms and a Keep's own request within about 200 ms.
func TestAPILatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newAPIRig(t, testdb.Options{Proxy: true})
	const runs = 20
	bars := map[string]time.Duration{
		"/v2 Ask, to the sources event": 300 * time.Millisecond,
		"/v2 Keep (+ jobs)":             200 * time.Millisecond,
	}
	var rows []string
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		r.db.Proxy.SetOneWay(rtt / 2)
		for _, op := range r.ops() {
			for range 2 {
				r.measure(t, op) // warm the statement caches
			}
			s := &netsim.Sample{Name: op.name, RTT: rtt}
			for range runs {
				took, c := r.measure(t, op)
				s.Add(took, c.RoundTrips())
			}
			rows = append(rows, s.Row())
			if bar, ok := bars[op.name]; ok && rtt == netsim.ProductionRTT && s.P(0.95) > bar {
				t.Errorf("%s at %v: p95 %v, want under %v", op.name, rtt, s.P(0.95), bar)
			}
		}
	}
	t.Logf("\n%s\n%s", netsim.Header, strings.Join(rows, "\n"))
}

// TestAPIRoundTrips guards the round trips of each /v2 hot path: a change
// that adds one fails here, whatever the machine's speed. The counts are
// steady-state (warm statement caches) and include work a request leaves
// running after it answers, such as a read's COMMIT. The wire audit checks
// every statement the requests send, River's inserts as the login role
// included: none touches a v2 table before its transaction's scope.
func TestAPIRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	r := newAPIRig(t, testdb.Options{Watch: audit.Observe})
	audit.Arm()
	defer audit.Require(t)
	// Before the pipelined ledger (Oct 7, 2026) these were 15, 25, 16, 14,
	// 32, 27 and 30.
	budgets := map[string]int{
		"/v2 memory list":               5,
		"/v2 memory get":                11,
		"/v2 review queue":              6,
		"/v2 Brief":                     5,
		"/v2 Keep (+ jobs)":             15,
		"/v2 Remember (+ jobs)":         12,
		"/v2 Ask, to the sources event": 9,
	}
	for _, op := range r.ops() {
		for range 2 {
			r.measure(t, op)
		}
		_, c := r.measure(t, op)
		t.Logf("%s: %d round trips\n%s", op.name, c.RoundTrips(), c)
		if got, want := c.RoundTrips(), budgets[op.name]; got > want {
			t.Errorf("%s: %d round trips, budget %d\n%s", op.name, got, want, c)
		}
	}
}
