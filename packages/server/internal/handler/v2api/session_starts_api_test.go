package v2api_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// Cloud session starts (cloud warm-start design §3.4): a sandbox names
// the compile its committed AGENTS.md holds, and hears what changed in
// the record since: new and changed cited lines (words from the latest
// compile only), memories that left the file, what a person forgot that
// the file still says, waiting decisions and the agent's notices, once.

type startLine struct {
	Refs []string `json:"refs"`
	Text string   `json:"text"`
}

type startFile struct {
	Path       string      `json:"path"`
	Here       string      `json:"here"`
	Latest     string      `json:"latest"`
	HandEdited bool        `json:"hand_edited"`
	Added      []startLine `json:"added"`
	Changed    []startLine `json:"changed"`
	Gone       []string    `json:"gone"`
	More       int         `json:"more"`
}

type startForget struct {
	Ref     string    `json:"ref"`
	With    []string  `json:"with"`
	At      time.Time `json:"at"`
	InFiles bool      `json:"in_files"`
}

type startAnswer struct {
	Space struct {
		ID   uuid.UUID `json:"id"`
		Slug string    `json:"slug"`
	} `json:"space"`
	Files     []startFile   `json:"files"`
	Forgotten []startForget `json:"forgotten"`
	Gates     []struct {
		Ref      string `json:"ref"`
		Question string `json:"question"`
		Agent    string `json:"agent"`
	} `json:"gates"`
	Notices []struct {
		Kind string   `json:"kind"`
		Refs []string `json:"refs"`
	} `json:"notices"`
	Handoffs []any `json:"handoffs"`
}

func startsPath(sp space) string { return "/v2/spaces/" + sp.slug + "/session-starts" }

func startBody(files ...map[string]any) map[string]any {
	return map[string]any{"agent": "copilot", "session_ref": "cp-1", "source": "startup", "files": files,
		"commit": "a41c9e0", "branch": "copilot/fix-412"}
}

func agentsFile(compile, drift string) map[string]any {
	f := map[string]any{"path": "AGENTS.md", "compile": compile}
	if drift != "" {
		f["drift_sha256"] = drift
	}
	return f
}

// startFixture is a project space whose AGENTS.md was compiled (C-1, the
// commit a sandbox holds), then changed: a memory kept, one edited, one
// left out of the Brief, one forgotten, and a gate asked; and compiled
// again (C-2).
type startFixture struct {
	*env
	rec                             *syncReads
	owner                           uuid.UUID
	sp                              space
	tok, key                        string
	keyID, conn                     uuid.UUID
	target                          uuid.UUID
	c1, c2, drift1                  string
	deploy, old, forgot, river, pnp memory
	gate                            gate
}

func newStartFixture(t *testing.T) *startFixture {
	t.Helper()
	return newStartFixtureOn(t, testdb.Open(t, testdb.Options{}), true)
}

// newStartFixtureOn builds the fixture on db; withReads records the
// digest reads at once (production's recorder writes them off the
// request path, so the round-trip budgets leave them out).
func newStartFixtureOn(t *testing.T, db *testdb.DB, withReads bool) *startFixture {
	t.Helper()
	rec := &syncReads{t: t}
	web, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnvOn(t, db, func(*env) []v2api.Option {
		opts := []v2api.Option{v2api.WithWebSurface(web)}
		if withReads {
			opts = append(opts, v2api.WithReads(rec))
		}
		return opts
	})
	rec.l = e.ledger
	f := &startFixture{env: e, rec: rec, owner: e.user("zz")}
	f.sp = e.space(f.owner, policy.SpaceProject, "acme-web")
	f.tok = e.session(f.owner)
	f.deploy = e.remember(f.tok, f.sp, "Deploy to staging first.").Memory
	f.forgot = e.remember(f.tok, f.sp, "Zebra tokens live in the ops vault.").Memory
	f.river = e.remember(f.tok, f.sp, "Background jobs run on River.").Memory
	var old result
	e.do(call{method: "POST", path: memoriesPath(f.sp), token: f.tok, body: remember("Prefer yarn workspaces.", "preferences")}).
		ok(http.StatusCreated, &old)
	f.old = old.Memory
	e.do(call{method: "POST", path: briefPath(f.sp), token: f.tok, body: briefBody("acme-web",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{
			{"ref": f.deploy.Ref}, {"ref": f.forgot.Ref}, {"ref": f.river.Ref}}},
		map[string]any{"key": "preferences", "heading": "Preferences", "items": []map[string]any{{"ref": f.old.Ref}}})}).
		ok(http.StatusCreated, nil)
	var tr targetResult
	e.do(call{method: "POST", path: targetsPath(f.sp), token: f.tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, &tr)
	f.target = tr.Target.ID
	e.compileAll()
	run := f.lastRun()
	f.c1, f.drift1 = run.Ref, run.Files[0].DriftSHA256

	// An agent connected to the space, which a Forget tells.
	f.key, f.keyID = e.apiKey(f.owner, keyOpts{agent: "copilot"})
	f.conn = e.connection(f.keyID)

	f.pnp = e.remember(f.tok, f.sp, "Use pnpm, not npm, in packages/web.").Memory
	e.do(call{method: "POST", path: memoryPath(f.deploy, ":edit"), token: f.tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"statement": "Deploy to staging first: fly deploy -c fly/fly.server.staging.toml."}}).ok(http.StatusOK, nil)
	e.do(call{method: "POST", path: briefPath(f.sp), token: f.tok, header: map[string]string{"If-Match": `"1"`},
		body: briefBody("acme-web", map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{
			{"ref": f.deploy.Ref}, {"ref": f.forgot.Ref}, {"ref": f.river.Ref}}})}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: memoryPath(f.forgot, ":forget"), token: f.tok, header: map[string]string{"If-Match": `"1"`}}).
		ok(http.StatusOK, nil)
	var asked gateResult
	e.do(call{method: "POST", path: gatesPath(f.sp), token: f.key, body: deployGate()}).ok(http.StatusCreated, &asked)
	f.gate = asked.Gate
	e.compileAll()
	f.c2 = f.lastRun().Ref
	if f.c2 == f.c1 {
		t.Fatalf("no second compile: %s", f.c2)
	}
	return f
}

func (f *startFixture) lastRun() compileRun {
	f.t.Helper()
	var runs page[compileRun]
	f.do(call{method: "GET", path: targetPath(f.target, "/runs"), token: f.tok}).ok(http.StatusOK, &runs)
	if len(runs.Items) == 0 || runs.Items[0].Status == "failed" {
		f.t.Fatalf("runs = %+v", runs)
	}
	return runs.Items[0]
}

func (f *startFixture) start(token string, body map[string]any, header map[string]string) startAnswer {
	f.t.Helper()
	h := map[string]string{"X-Memax-Via": "cli"}
	for k, v := range header {
		h[k] = v
	}
	var out startAnswer
	f.do(call{method: "POST", path: startsPath(f.sp), token: token, body: body, header: h}).ok(http.StatusOK, &out)
	return out
}

func (f *startFixture) loads(conn uuid.UUID) int {
	return f.count(`SELECT count(*) FROM v2.reads WHERE kind = 'compile_load' AND connection_id = $1`, conn)
}

func refsOf(ls []startLine) []string {
	var out []string
	for _, l := range ls {
		out = append(out, strings.Join(l.Refs, ","))
	}
	return out
}

func TestSessionStartDelta(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	key := uuid.NewString()
	got := f.start(f.key, startBody(agentsFile(f.c1, f.drift1)), map[string]string{"Idempotency-Key": key})

	if got.Space.ID != f.sp.id || got.Space.Slug != f.sp.slug || len(got.Files) != 1 {
		t.Fatalf("answer = %+v", got)
	}
	file := got.Files[0]
	if file.Path != "AGENTS.md" || file.Here != f.c1 || file.Latest != f.c2 || file.HandEdited || file.More != 0 {
		t.Errorf("file = %+v", file)
	}
	if !slices.Equal(refsOf(file.Added), []string{f.pnp.Ref}) || !strings.Contains(file.Added[0].Text, "Use pnpm, not npm") {
		t.Errorf("added = %+v", file.Added)
	}
	if !slices.Equal(refsOf(file.Changed), []string{f.deploy.Ref}) || !strings.Contains(file.Changed[0].Text, "fly deploy -c") {
		t.Errorf("changed = %+v", file.Changed)
	}
	if !slices.Equal(file.Gone, []string{f.old.Ref}) {
		t.Errorf("gone = %v, want %s (the forgotten %s is never gone)", file.Gone, f.old.Ref, f.forgot.Ref)
	}
	if len(got.Forgotten) != 1 || got.Forgotten[0].Ref != f.forgot.Ref || !got.Forgotten[0].InFiles {
		t.Errorf("forgotten = %+v", got.Forgotten)
	}
	if len(got.Gates) != 1 || got.Gates[0].Ref != f.gate.Ref || got.Gates[0].Agent == "" || got.Gates[0].Question != f.gate.Question {
		t.Errorf("gates = %+v", got.Gates)
	}
	if len(got.Notices) != 1 || got.Notices[0].Kind != "forgotten" || !slices.Equal(got.Notices[0].Refs, []string{f.forgot.Ref}) {
		t.Errorf("notices = %+v", got.Notices)
	}
	if got.Handoffs == nil || len(got.Handoffs) != 0 {
		t.Errorf("handoffs = %v", got.Handoffs)
	}

	// One compile load of C-1 by the connection, and a digest read of the
	// two lines sent.
	if n := f.loads(f.conn); n != 1 {
		t.Errorf("%d compile loads, want 1", n)
	}
	var reads struct {
		Items []readWire `json:"items"`
	}
	f.do(call{method: "GET", path: "/v2/spaces/" + f.sp.slug + "/reads", token: f.tok}).ok(http.StatusOK, &reads)
	kinds := map[string]readWire{}
	for _, r := range reads.Items {
		kinds[r.Kind] = r
	}
	if l := kinds["compile_load"]; l.Compile != f.c1 || l.SessionRef != "cp-1" || l.Agent != "copilot" || l.Via != "cli" ||
		l.ConnectionID == nil || *l.ConnectionID != f.conn {
		t.Errorf("compile load = %+v", l)
	}
	if d := kinds["digest"]; len(d.MemoryRefs) != 2 || !slices.Contains(d.MemoryRefs, f.pnp.Ref) || !slices.Contains(d.MemoryRefs, f.deploy.Ref) {
		t.Errorf("digest read = %+v", d)
	}

	// The same key again (a retry, or a second hook in the session) counts
	// nothing more; the notices were told once.
	again := f.start(f.key, startBody(agentsFile(f.c1, f.drift1)), map[string]string{"Idempotency-Key": key})
	if n := f.loads(f.conn); n != 1 {
		t.Errorf("after a replay, %d compile loads, want 1", n)
	}
	if len(again.Notices) != 0 || len(again.Files) != 1 || len(again.Files[0].Added) != 1 {
		t.Errorf("replay = %+v", again)
	}

	// The files at the latest compile: nothing changed in them, though the
	// gate still waits.
	latest := f.start(f.key, startBody(agentsFile(f.c2, "")), nil)
	lf := latest.Files[0]
	if lf.Here != f.c2 || lf.Latest != f.c2 || len(lf.Added)+len(lf.Changed)+len(lf.Gone) != 0 || len(latest.Forgotten) != 0 {
		t.Errorf("at the latest compile = %+v (forgotten %+v)", lf, latest.Forgotten)
	}
	if len(latest.Gates) != 1 {
		t.Errorf("gates = %+v", latest.Gates)
	}

	// A hand edit of the committed file.
	edited := f.start(f.key, startBody(agentsFile(f.c1, strings.Repeat("a", 64))), nil)
	if !edited.Files[0].HandEdited || len(edited.Files[0].Added) != 1 {
		t.Errorf("hand edited = %+v", edited.Files[0])
	}

	// A compact start counts nothing.
	before := f.loads(f.conn)
	compact := startBody(agentsFile(f.c1, f.drift1))
	compact["source"] = "compact"
	f.start(f.key, compact, nil)
	if n := f.loads(f.conn); n != before {
		t.Errorf("a compact start counted: %d loads, was %d", n, before)
	}

	// A person's session names the agent.
	f.start(f.tok, startBody(agentsFile(f.c1, f.drift1)), nil)
	if n := f.count(`SELECT count(*) FROM v2.reads WHERE kind = 'compile_load' AND reader_kind = 'person' AND agent = 'copilot'`); n != 1 {
		t.Errorf("a person's start: %d loads", n)
	}

	// A key with no connection gets its answer, and counts nothing.
	bare, bareID := f.apiKey(f.owner, keyOpts{agent: "copilot", unconnected: true})
	reads0 := f.count(`SELECT count(*) FROM v2.reads`)
	unconnected := f.start(bare, startBody(agentsFile(f.c1, f.drift1)), nil)
	if len(unconnected.Files) != 1 || len(unconnected.Files[0].Added) != 1 || len(unconnected.Notices) != 0 {
		t.Errorf("an unconnected key's answer = %+v", unconnected)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads`); n != reads0 {
		t.Errorf("an unconnected key (%s) counted %d reads", bareID, n-reads0)
	}
}

// Without the older compile's artifact, the record's refs stand in: what
// the older compile didn't cite is new, and nothing is "changed".
func TestSessionStartWithoutTheOlderArtifact(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	var key string
	if err := f.pool.QueryRow(context.Background(), `SELECT artifact_key FROM v2.compile_runs WHERE target_id = $1 ORDER BY seq LIMIT 1`, f.target).
		Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	got := f.start(f.key, startBody(agentsFile(f.c1, f.drift1)), nil).Files[0]
	if !slices.Equal(refsOf(got.Added), []string{f.pnp.Ref}) || len(got.Changed) != 0 || !slices.Equal(got.Gone, []string{f.old.Ref}) {
		t.Errorf("from the refs = %+v", got)
	}
}

// Rule 13: a start reaches only the path's space, in the credential's
// scope, and only that space's compiles.
func TestSessionStartIsolation(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	jy := f.user("jy")
	theirs := f.space(jy, policy.SpaceProject, "theirs")
	jyTok := f.session(jy)
	for i := range 4 {
		f.remember(jyTok, theirs, "Their rule "+string(rune('a'+i))+".")
	}
	f.do(call{method: "POST", path: briefPath(theirs), token: jyTok, body: briefBody("theirs")}).ok(http.StatusCreated, nil)
	// Three targets, so their latest compile number is one this space
	// (which compiled twice) doesn't have.
	for _, kind := range []string{"agents_md", "claude_md", "cursor_mdc"} {
		f.do(call{method: "POST", path: targetsPath(theirs), token: jyTok, body: map[string]any{"kind": kind}}).ok(http.StatusCreated, nil)
	}
	f.compileAll()
	var theirRef string
	if err := f.pool.QueryRow(context.Background(), `SELECT 'C-' || lpad(max(seq)::text, 4, '0') FROM v2.compile_runs WHERE space_id = $1`, theirs.id).
		Scan(&theirRef); err != nil {
		t.Fatal(err)
	}

	// Another person's key: the space isn't there.
	jyKey, _ := f.apiKey(jy, keyOpts{agent: "copilot"})
	f.do(call{method: "POST", path: startsPath(f.sp), token: jyKey, body: startBody(agentsFile(f.c1, ""))}).fails(http.StatusNotFound, "not_found")
	// A key held to another of the owner's spaces.
	other := f.space(f.owner, policy.SpaceProject, "other")
	held, _ := f.apiKey(f.owner, keyOpts{agent: "copilot", hubs: []uuid.UUID{other.id}})
	f.do(call{method: "POST", path: startsPath(f.sp), token: held, body: startBody(agentsFile(f.c1, ""))}).fails(http.StatusNotFound, "not_found")
	// Their compile, named in this space: not this space's file. A compile
	// nobody has, the same.
	got := f.start(f.key, startBody(agentsFile(theirRef, "")), nil)
	if len(got.Files) != 0 {
		t.Errorf("their %s answered as this space's: %+v", theirRef, got.Files)
	}
	if got := f.start(f.key, startBody(agentsFile("C-9999", "")), nil); len(got.Files) != 0 {
		t.Errorf("C-9999 answered: %+v", got.Files)
	}
	// A path the compile didn't write is not its file.
	if got := f.start(f.key, startBody(map[string]any{"path": "docs/AGENTS.md", "compile": f.c1}), nil); len(got.Files) != 0 {
		t.Errorf("another path answered: %+v", got.Files)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads WHERE compile_id IN (SELECT id FROM v2.compile_runs WHERE space_id = $1)`, theirs.id); n != 0 {
		t.Errorf("%d reads of their compiles", n)
	}
}

// Rule 7: after a Forget, no start answer holds the words, whichever
// compile it names: before the Forget's propagation re-renders the
// artifacts and the files compile again, and after.
func TestSessionStartAfterForget(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	// Forget a memory both compiles cite, before anything compiles again.
	f.do(call{method: "POST", path: memoryPath(f.river, ":forget"), token: f.tok, header: map[string]string{"If-Match": `"1"`}}).
		ok(http.StatusOK, nil)
	words := []string{"Background jobs run on River", "Zebra tokens"}
	check := func(when string) {
		t.Helper()
		for _, c := range []string{f.c1, f.c2, f.lastRun().Ref} {
			r := f.do(call{method: "POST", path: startsPath(f.sp), token: f.key, body: startBody(agentsFile(c, ""))}).ok(http.StatusOK, nil)
			for _, w := range words {
				if strings.Contains(string(r.body), w) {
					t.Errorf("%s, a start naming %s says %q: %s", when, c, w, r.body)
				}
			}
			var got startAnswer
			r.ok(http.StatusOK, &got)
			listed := slices.ContainsFunc(got.Forgotten, func(x startForget) bool { return x.Ref == f.river.Ref && x.InFiles })
			if !listed && c != f.lastRun().Ref {
				t.Errorf("%s, a start naming %s doesn't list %s as in its files: %+v", when, c, f.river.Ref, got.Forgotten)
			}
		}
	}
	check("before the recompile")
	f.compileAll()
	check("after the recompile")
}

// TestSessionStartLatency times a counted start with a start key at 0 and
// 24 ms of round-trip time (MEMAX_LATENCY=1), the artifacts warm: under
// 300 ms at p95 at 24 ms, recall's bar (N2).
func TestSessionStartLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	db := testdb.Open(t, testdb.Options{Proxy: true})
	f := newStartFixtureOn(t, db, false)
	cloud := f.cloudAgent(f.cliSession(f.owner), map[string]any{"agent": "copilot", "space": f.sp.slug}, nil).Key.Secret
	var rows []string
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		db.Proxy.SetOneWay(rtt / 2)
		s := &netsim.Sample{Name: "/v2 session start (start key)", RTT: rtt}
		for i := range 22 {
			ctx, c := netsim.Track(context.Background())
			db.Trips.SetFallback(c)
			began := time.Now()
			f.do(call{method: "POST", path: startsPath(f.sp), token: cloud, ctx: ctx, body: startBody(agentsFile(f.c1, f.drift1)),
				header: map[string]string{"X-Memax-Via": "cli"}}).ok(http.StatusOK, nil)
			took := time.Since(began)
			db.Trips.SetFallback(nil)
			f.h.Wait()
			netsim.Settle(db.Pool, 2*time.Second)
			if i >= 2 { // the first two warm the statement and artifact caches
				s.Add(took, c.RoundTrips())
			}
		}
		rows = append(rows, s.Row())
		if rtt == netsim.ProductionRTT && s.P(0.95) > 300*time.Millisecond {
			t.Errorf("a session start at %v: p95 %v, want under 300 ms", rtt, s.P(0.95))
		}
	}
	t.Logf("\n%s\n%s", netsim.Header, strings.Join(rows, "\n"))
}

// TestSessionStartRoundTrips guards a start's round trips, as a cloud
// sandbox sends it with a start key (an API key today), a person's CLI,
// and a key with no connection. The wire audit checks every statement.
func TestSessionStartRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	f := newStartFixtureOn(t, db, false)
	bare, _ := f.apiKey(f.owner, keyOpts{agent: "copilot", unconnected: true})
	cloud := f.cloudAgent(f.cliSession(f.owner), map[string]any{"agent": "copilot", "space": f.sp.slug}, nil).Key.Secret
	audit.Arm()
	defer audit.Require(t)

	measure := func(name string, run func(ctx context.Context)) *netsim.Counter {
		t.Helper()
		ctx, c := netsim.Track(context.Background())
		// The auth middleware's store queries with context.Background().
		db.Trips.SetFallback(c)
		defer db.Trips.SetFallback(nil)
		run(ctx)
		f.h.Wait()
		if !netsim.Settle(db.Pool, 2*time.Second) {
			t.Fatalf("%s: connections still checked out after 2 s", name)
		}
		return c
	}
	start := func(token, key string) func(ctx context.Context) {
		return func(ctx context.Context) {
			f.do(call{method: "POST", path: startsPath(f.sp), token: token, ctx: ctx, body: startBody(agentsFile(f.c1, f.drift1)),
				header: map[string]string{"X-Memax-Via": "cli", "Idempotency-Key": key}}).ok(http.StatusOK, nil)
		}
	}
	// The start itself is two round trips when it counts: one pipeline
	// that reads everything, takes the notices and claims the loads with
	// their R- numbers, then the loads with COMMIT. A start that writes
	// nothing is one read-only round trip. The rest is the credential. A
	// key on the start's route is read with its person's scope and its
	// last_used in one pipeline (AuthHandler.ResolveStartKey); a start key
	// skips V1's hub reads, so it adds only its connection's read and that
	// read's COMMIT, which nobody waits for: 5 in all. Any other API key
	// still reads V1's hubs (and, with no Personal space, looks for one),
	// and a session costs 3.
	budgets := []struct {
		name   string
		budget int
		run    func(ctx context.Context)
	}{
		{"a start key's start (counted)", 5, func(ctx context.Context) { start(cloud, uuid.NewString())(ctx) }},
		{"the same start again (a replay)", 5, start(cloud, "replayed")},
		{"another API key's start (counted)", 7, func(ctx context.Context) { start(f.key, uuid.NewString())(ctx) }},
		{"a person's CLI start (counted)", 5, func(ctx context.Context) { start(f.tok, uuid.NewString())(ctx) }},
		{"a key with no connection", 6, start(bare, "bare")},
	}
	for _, b := range budgets {
		for range 2 {
			measure("warm", b.run)
		}
		c := measure(b.name, b.run)
		t.Logf("%s: %d round trips\n%s", b.name, c.RoundTrips(), c)
		if c.RoundTrips() > b.budget {
			t.Errorf("%s: %d round trips, budget %d\n%s", b.name, c.RoundTrips(), b.budget, c)
		}
	}
}
