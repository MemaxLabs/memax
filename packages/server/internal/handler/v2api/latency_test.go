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
	// stale are kept memories past their date to check again, flagged as
	// Dream flags them, for Verify to answer.
	stale []string
	// armed are kept memories whose path condition held on the default
	// branch, for a check run to find gone.
	armed []armedCondition
	seq   int
}

type armedCondition struct{ ref, hash string }

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
	past := time.Now().Add(-24 * time.Hour)
	for i := range 60 {
		res, err := e.ledger.Apply(ctx, &ledger.Remember{Meta: meta(), NewMemory: ledger.NewMemory{SpaceID: sp.id,
			Statement: fmt.Sprintf("Staging tokens rotate every %d days.", i+1), Section: ledger.SectionConventions, StaleAfter: &past}})
		if err != nil {
			t.Fatal(err)
		}
		e.flag(memory{ID: res.Memory.ID, Ref: res.Memory.Ref, SpaceID: sp.id}, "stale")
		r.stale = append(r.stale, res.Memory.Ref)
	}
	// The space compiles for a repository from here on, so memories that
	// name a path get its condition, and one check run arms them all.
	e.exec(`UPDATE hubs SET repository = 'MemaxLabs/memax' WHERE id = $1`, sp.id)
	for i := range 50 {
		if _, err := e.ledger.Apply(ctx, &ledger.Remember{Meta: meta(), NewMemory: ledger.NewMemory{SpaceID: sp.id,
			Statement: fmt.Sprintf("Handler %d lives in `packages/server/internal/h%d/handler.go`.", i, i), Section: ledger.SectionConventions}}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := e.ledger.ListConditions(ctx, scope, sp.id)
	if err != nil {
		t.Fatal(err)
	}
	arm := ledger.CheckReport{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: zz}, Scope: scope, Via: policy.ViaCLI, SpaceID: sp.id,
		IdempotencyKey: uuid.NewString(), Repository: "MemaxLabs/memax", Branch: "main", Commit: strings.Repeat("a", 40),
		CommitAt: time.Now().Add(-3 * time.Hour), Trigger: ledger.TriggerConditions, Device: "dev-1", Complete: true}
	for _, it := range list.Items {
		c := it.Conditions[0]
		arm.Results = append(arm.Results, ledger.CheckResult{Memory: it.Memory, Condition: c.Condition.ID, Hash: c.Hash, Outcome: ledger.CheckHolds})
		r.armed = append(r.armed, armedCondition{ref: it.Memory, hash: c.Hash})
	}
	if res, err := e.ledger.ReportChecks(ctx, arm); err != nil || res.Run == nil || res.Run.Counts.Holds != 50 {
		t.Fatalf("arm: %+v %v", res, err)
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

func (r *apiRig) nextStale(t *testing.T) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stale) < 2 {
		t.Fatal("out of stale memories to verify")
	}
	ref := r.stale[len(r.stale)-1]
	r.stale = r.stale[:len(r.stale)-1]
	return ref
}

func (r *apiRig) nextArmed(t *testing.T) armedCondition {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.armed) < 2 {
		t.Fatal("out of armed conditions to find gone")
	}
	a := r.armed[len(r.armed)-1]
	r.armed = r.armed[:len(r.armed)-1]
	return a
}

// checkRun posts a run of one result at a commit an hour ago.
func (r *apiRig) checkRun(t *testing.T, ctx context.Context, a armedCondition, outcome string) time.Duration {
	result := map[string]any{"memory": a.ref, "condition": "c1", "hash": a.hash, "outcome": outcome}
	if outcome == "fails" {
		result["evidence"] = map[string]any{"change": "removed"}
	}
	start := time.Now()
	r.e.do(call{method: "POST", path: "/v2/spaces/" + r.sp.slug + "/check-runs", token: r.tok, ctx: ctx,
		header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"repository": "MemaxLabs/memax", "branch": "main", "commit": strings.Repeat("b", 40),
			"commit_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "trigger": "head_moved", "device": "dev-1",
			"complete": false, "results": []any{result}}}).ok(http.StatusCreated, nil)
	return time.Since(start)
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
		// The stale report reads the first stale memory, which Verify never
		// answers here.
		{"/v2 stale report", r.get("/v2/memories/" + r.stale[0] + "/stale?space=" + slug)},
		{"/v2 Verify (+ jobs)", func(t *testing.T, ctx context.Context) time.Duration {
			ref := r.nextStale(t)
			start := time.Now()
			r.e.do(call{method: "POST", path: "/v2/memories/" + ref + ":verify?space=" + slug, token: r.tok, ctx: ctx,
				header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"answer": "still_true"}}).ok(http.StatusOK, nil)
			return time.Since(start)
		}},
		{"/v2 conditions", r.get("/v2/spaces/" + slug + "/conditions")},
		// A run whose result changes nothing that flags: the first armed
		// condition still holds.
		{"/v2 check run", func(t *testing.T, ctx context.Context) time.Duration {
			return r.checkRun(t, ctx, r.armed[0], "holds")
		}},
		{"/v2 check run that flags (+ jobs)", func(t *testing.T, ctx context.Context) time.Duration {
			return r.checkRun(t, ctx, r.nextArmed(t), "fails")
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
// within 300 ms, the reads within 300 ms, and a Keep or a Remember within
// 500 ms. A Keep's aim of about 200 ms is reported, not enforced: its
// command makes about a dozen dependent statements (AGENTS.md, "Round
// trips").
func TestAPILatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newAPIRig(t, testdb.Options{Proxy: true})
	const runs = 20
	bars := map[string]time.Duration{
		"/v2 memory list":                   300 * time.Millisecond,
		"/v2 memory get":                    300 * time.Millisecond,
		"/v2 review queue":                  300 * time.Millisecond,
		"/v2 Brief":                         300 * time.Millisecond,
		"/v2 Keep (+ jobs)":                 500 * time.Millisecond,
		"/v2 Remember (+ jobs)":             500 * time.Millisecond,
		"/v2 stale report":                  300 * time.Millisecond,
		"/v2 Verify (+ jobs)":               500 * time.Millisecond,
		"/v2 conditions":                    300 * time.Millisecond,
		"/v2 check run":                     300 * time.Millisecond,
		"/v2 check run that flags (+ jobs)": 500 * time.Millisecond,
		"/v2 Ask, to the sources event":     300 * time.Millisecond,
	}
	aims := map[string]time.Duration{"/v2 Keep (+ jobs)": 200 * time.Millisecond}
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
			if aim, ok := aims[op.name]; ok && rtt == netsim.ProductionRTT && s.P(0.95) > aim {
				t.Logf("%s at %v: p95 %v misses the %v aim", op.name, rtt, s.P(0.95), aim)
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
	// 32, 27 and 30. Keep and Remember count River's insert notification
	// (PGNotifyMany), which River sends at most once per queue per fetch
	// cooldown (100 ms): a call right after another skips it, but at alpha
	// traffic nearly every one sends it. Keep was 16 and Remember 13 until
	// the ref resolved in the locking statement, the space's read went with
	// the idempotency claim, and Keep's target bump with its undo read.
	// The stale report is the auth's two reads and one ReadBatch; Verify
	// (still true) is Keep's path with the stale flag's read for the
	// receipt's source, and River's notification like Keep's. The
	// conditions are the auth's two and one ReadBatch; a check run is the
	// auth's two, then the space with the key's claim, the memories locked
	// with their check state, the run and its results, and COMMIT (the
	// stale-detection design's 4), however many results it carries; one
	// that flags adds the receipts, flags and targets to the run's round
	// trip, then River's insert and its notification, however many it
	// flags.
	budgets := map[string]int{
		"/v2 memory list":                   5,
		"/v2 memory get":                    11,
		"/v2 review queue":                  6,
		"/v2 Brief":                         5,
		"/v2 Keep (+ jobs)":                 13,
		"/v2 Remember (+ jobs)":             12,
		"/v2 stale report":                  3,
		"/v2 Verify (+ jobs)":               11,
		"/v2 conditions":                    3,
		"/v2 check run":                     6,
		"/v2 check run that flags (+ jobs)": 8,
		"/v2 Ask, to the sources event":     9,
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
