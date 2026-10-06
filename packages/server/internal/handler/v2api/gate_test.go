package v2api_test

// The Phase 1 gate (plan 25 §12), end to end: memax-v2 compiles itself to
// AGENTS.md, a CLAUDE.md shim, a scoped Cursor rule and the ChatGPT
// copy-out; a Keep recompiles every target in well under 10 s; a hand
// edit to a compiled file is detected and pulled back as proposals;
// overwrite and stop do what they say; spaces stay isolated and every
// write carries its receipt.
//
// It runs the real stack: Postgres (testdb), the ledger, River with the
// compile worker and the production quiet window, the real compile
// service under Node (packages/compile-service), and the /v2 API behind
// the real auth middleware, with every exchange checked against v2.yaml.
// A small daemon stands in for the CLI: it reads each target's preview,
// "writes" the files to a map, and acknowledges the delivery.

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/devseed"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// lateJobs lets the ledger be built before the River client that works
// its jobs.
type lateJobs struct{ client *river.Client[pgx.Tx] }

func (j *lateJobs) InsertManyTx(ctx context.Context, tx pgx.Tx, p []river.InsertManyParams) ([]*rivertype.JobInsertResult, error) {
	return j.client.InsertManyTx(ctx, tx, p)
}

// newGateEnv is newEnv with the real compile service and a River worker.
func newGateEnv(t *testing.T) (*env, string) {
	t.Helper()
	serviceURL := compiletest.StartService(t)
	st, pool := testdb.Acquire(t)
	dbTests.Add(1)
	authH, err := handler.NewAuthHandler(pool)
	if err != nil {
		t.Fatal(err)
	}
	authH.SetStore(st)
	chain := func(h http.Handler) http.Handler {
		return handler.RequireAuth([]byte(testSecret), authH.ResolveAPIKey, authH.ResolveOAuthGrant)(
			handler.HubContext(st)(handler.AuthorizeHTTP(h)))
	}
	jobs := &lateJobs{}
	e := &env{t: t, pool: pool, store: mockobjectstore.New()}
	e.ledger = ledger.New(pool, ledger.WithLogger(quiet), ledger.WithJobs(jobs))
	e.svc = compile.New(e.ledger, compile.NewClient(serviceURL), e.store,
		compile.Config{AppBaseURL: "https://memax.app", Log: quiet})
	workers := river.NewWorkers()
	compile.AddWorkers(workers, e.ledger, e.svc)
	// The judge works its queue too, as in production; no model is
	// configured, so it runs stage 0 alone.
	judge.AddWorkers(workers, judge.New(e.ledger, nil, judge.Config{Log: quiet}))
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: quiet, Workers: workers,
		Queues: map[string]river.QueueConfig{ledger.QueueCompile: {MaxWorkers: compile.MaxWorkers},
			ledger.QueueJudge: {MaxWorkers: judge.MaxWorkers}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs.client = client
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = client.Stop(stop)
		cancel()
	})
	mux := http.NewServeMux()
	h := v2api.New(e.ledger, quiet, v2api.WithCompile(e.svc))
	t.Cleanup(h.Wait)
	h.Mount(mux, chain)
	e.h, e.srv = h, spec.Handler(t, mux)
	return e, serviceURL
}

type gateTarget struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Label       string    `json:"label"`
	Delivery    string    `json:"delivery"`
	SyncState   string    `json:"sync_state"`
	DirtyGen    int64     `json:"dirty_gen"`
	CompiledGen int64     `json:"compiled_gen"`
	LastCompile *struct {
		Ref        string    `json:"ref"`
		Status     string    `json:"status"`
		CompiledAt time.Time `json:"compiled_at"`
	} `json:"last_compile"`
	Delivered *struct {
		Compile string `json:"compile"`
		SHA256  string `json:"sha256"`
		Files   []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	} `json:"delivered"`
}

type gatePreview struct {
	Target  gateTarget `json:"target"`
	Compile *struct {
		Ref         string `json:"ref"`
		DriftSHA256 string `json:"drift_sha256"`
	} `json:"compile"`
	Files []struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		DriftSHA256 string `json:"drift_sha256"`
	} `json:"files"`
	Copies []struct {
		Label   string `json:"label"`
		Content string `json:"content"`
	} `json:"copies"`
}

// daemon is the CLI's local delivery, reduced to what the gate needs: it
// writes each local target's latest compile to its "disk" unless the file
// there departs from what Memax delivered (a hand edit), and
// acknowledges what it wrote.
type daemon struct {
	e    *env
	tok  string
	cl   *compile.Client // for drift hashes, as the CLI uses the compiler's
	mu   sync.Mutex
	disk map[string]string
}

// driftOf is a file's drift hash (its managed block's, or the whole
// file's), as the compiler computes it.
func (d *daemon) driftOf(content string) string {
	d.e.t.Helper()
	pb, err := d.cl.ParseBack(context.Background(), &compile.ParseBackRequest{Last: compile.LastFile{Content: content}, Current: content})
	if err != nil {
		d.e.t.Fatalf("drift hash: %v", err)
	}
	return pb.DriftSHA256
}

func (d *daemon) file(path string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.disk[path]
}

func (d *daemon) read(path string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.disk[path]
	return c, ok
}

func (d *daemon) edit(path, content string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disk[path] = content
}

// sync delivers every local target whose latest compile isn't on disk.
func (d *daemon) sync(targets []gateTarget) {
	d.e.t.Helper()
	for _, tg := range targets {
		if tg.Delivery != "local" || tg.SyncState == "drifted" || tg.SyncState == "off" || tg.LastCompile == nil ||
			(tg.Delivered != nil && tg.Delivered.Compile == tg.LastCompile.Ref) {
			continue
		}
		var pv gatePreview
		d.e.do(call{method: "GET", path: targetPath(tg.ID, "/preview"), token: d.tok}).ok(http.StatusOK, &pv)
		if pv.Compile == nil {
			continue
		}
		baseline := map[string]string{}
		if pv.Target.Delivered != nil {
			for _, f := range pv.Target.Delivered.Files {
				baseline[f.Path] = f.SHA256
			}
		}
		manifest := map[string]string{}
		blocked := false
		for _, f := range pv.Files {
			if cur, ok := d.read(f.Path); ok {
				if h := d.driftOf(cur); h != baseline[f.Path] && h != f.DriftSHA256 {
					blocked = true // a hand edit: never written over
				}
			}
		}
		if blocked {
			continue
		}
		d.mu.Lock()
		for _, f := range pv.Files {
			d.disk[f.Path] = f.Content
			manifest[f.Path] = f.DriftSHA256
		}
		d.mu.Unlock()
		d.e.do(call{method: "POST", path: targetPath(tg.ID, "/deliveries"), token: d.tok,
			header: map[string]string{"X-Memax-Via": "cli"},
			body:   map[string]any{"compile": pv.Compile.Ref, "sha256": ledger.ManifestSHA256(manifest)}}).ok(http.StatusOK, nil)
	}
}

func (e *env) targets(tok string) []gateTarget {
	e.t.Helper()
	var list page[gateTarget]
	e.do(call{method: "GET", path: "/v2/spaces/" + devseed.SpaceSlug + "/targets", token: tok}).ok(http.StatusOK, &list)
	return list.Items
}

// settled: every target compiled its latest generation, and every local
// one has it on disk.
func settled(ts []gateTarget) bool {
	for _, tg := range ts {
		if tg.SyncState == "off" {
			continue
		}
		if tg.CompiledGen < tg.DirtyGen || tg.LastCompile == nil {
			return false
		}
		if tg.Delivery == "local" && tg.SyncState != "in_sync" {
			return false
		}
	}
	return true
}

// await runs the daemon until the space settles, and says how long it took.
func (d *daemon) await(within time.Duration, what string) time.Duration {
	d.e.t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		ts := d.e.targets(d.tok)
		if settled(ts) {
			return time.Since(start)
		}
		d.sync(ts)
		time.Sleep(50 * time.Millisecond)
	}
	d.e.t.Fatalf("%s: the space didn't settle within %v: %+v", what, within, d.e.targets(d.tok))
	return 0
}

func TestPhase1Gate(t *testing.T) {
	t.Parallel()
	e, serviceURL := newGateEnv(t)
	ctx := context.Background()
	demo, err := devseed.SeedMemaxV2(ctx, e.pool, e.ledger, devseed.Options{Env: "test", SkipBriefAndTargets: true})
	if err != nil {
		t.Fatal(err)
	}
	zz := e.session(demo.ZZ)
	space := "/v2/spaces/" + devseed.SpaceSlug

	// The Brief and the four default targets, through the API.
	title, summary, sections := devseed.DemoBrief()
	body := map[string]any{"title": title, "summary": summary, "sections": sections}
	var br briefResult
	e.do(call{method: "POST", path: space + "/brief", token: zz, body: body}).ok(http.StatusCreated, &br)
	ids := map[string]uuid.UUID{}
	for _, kind := range []string{"agents_md", "claude_md", "cursor_mdc", "chatgpt"} {
		var tr targetResult
		e.do(call{method: "POST", path: space + "/targets", token: zz, body: map[string]any{"kind": kind}}).ok(http.StatusCreated, &tr)
		ids[kind] = tr.Target.ID
	}
	d := &daemon{e: e, tok: zz, cl: compile.NewClient(serviceURL), disk: map[string]string{}}
	first := d.await(20*time.Second, "the first compile")
	t.Logf("first compile of the four targets, delivered: %v", first.Round(time.Millisecond))

	// What memax-v2 compiles to.
	agents := d.file("AGENTS.md")
	for _, want := range []string{
		"<!-- Compiled by Memax from memax-v2 at ", "Edit it at https://memax.app/memax-v2/brief",
		"# Memax V2 engineering brief", "What every agent on this project reads before it writes code.",
		"## Decisions",
		"- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]",
		"- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026-07-28. [M-0102]",
		"- API errors are RFC 9457 problem+json, never bare strings. [M-0098]",
		"- (Being verified) Ask memax answers with the Haiku tier. [M-0187]",
		"## Conventions", "- pnpm workspaces only. Never run `npm install` at the root. [M-0071]",
		"- Every write tool returns a receipt ID the caller can cite. [M-0112]",
		"### In `packages/web/**`", "[M-0442]",
		"## Open", "Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]",
		"## Live context",
	} {
		if !strings.Contains(agents, want) {
			t.Errorf("AGENTS.md lacks %q", want)
		}
	}
	for _, never := range []string{"input_required", "Deploy the v2 API to Fly.io."} {
		if strings.Contains(agents, never) {
			t.Errorf("AGENTS.md carries a proposal: %q", never)
		}
	}
	if claude := d.file("CLAUDE.md"); !strings.Contains(claude, "@AGENTS.md") || strings.Contains(claude, "[M-0219]") {
		t.Errorf("CLAUDE.md isn't a shim:\n%s", claude)
	}
	cursorPath := ".cursor/rules/memax-packages-web.mdc"
	if rule := d.file(cursorPath); !strings.Contains(rule, "globs: packages/web/**") || !strings.Contains(rule, "alwaysApply: false") ||
		!strings.Contains(rule, "[M-0442]") || strings.Contains(rule, "[M-0219]") {
		t.Errorf("the scoped Cursor rule:\n%s", rule)
	}
	var gpt gatePreview
	e.do(call{method: "GET", path: targetPath(ids["chatgpt"], "/preview"), token: zz}).ok(http.StatusOK, &gpt)
	if len(gpt.Copies) != 1 || gpt.Copies[0].Label != "ChatGPT project instructions" || !strings.Contains(gpt.Copies[0].Content, "[M-0219]") ||
		gpt.Target.SyncState != "in_sync" {
		t.Errorf("ChatGPT copy-out = %+v", gpt)
	}
	t.Logf("AGENTS.md as compiled:\n%s", agents)

	// A Keep recompiles every target (N1: under 10 s, p95).
	var keep result
	r := e.do(call{method: "POST", path: "/v2/memories/M-0430:keep?space=" + devseed.SpaceSlug, token: zz})
	r.ok(http.StatusOK, &keep)
	took := d.await(10*time.Second, "the keep")
	if !strings.Contains(d.file("AGENTS.md"), "- MCP write tools must ask for confirmation with input_required. [M-0430]") {
		t.Errorf("AGENTS.md after the keep:\n%s", d.file("AGENTS.md"))
	}
	e.do(call{method: "GET", path: targetPath(ids["chatgpt"], "/preview"), token: zz}).ok(http.StatusOK, &gpt)
	if !strings.Contains(gpt.Copies[0].Content, "[M-0430]") {
		t.Error("the ChatGPT copy-out lacks the kept line")
	}
	var keptAt time.Time
	if err := e.pool.QueryRow(ctx, `SELECT recorded_at FROM v2.receipts WHERE id = $1`, keep.Receipts[0].ID).Scan(&keptAt); err != nil {
		t.Fatal(err)
	}
	for _, tg := range e.targets(zz) {
		var compiled, delivered *time.Time
		_ = e.pool.QueryRow(ctx, `SELECT max(compiled_at), max(delivered_at) FROM v2.compile_runs WHERE target_id = $1`, tg.ID).Scan(&compiled, &delivered)
		line := tg.Label + ": " + tg.SyncState
		if compiled != nil && compiled.After(keptAt) {
			line += ", compiled +" + compiled.Sub(keptAt).Round(time.Millisecond).String()
			if delivered != nil && delivered.After(keptAt) {
				line += ", delivered +" + delivered.Sub(keptAt).Round(time.Millisecond).String()
			}
		} else {
			line += ", output unchanged (no new run)"
		}
		t.Log("after the keep, " + line)
	}
	t.Logf("keep → every target in sync: %v", took.Round(time.Millisecond))
	if took > 10*time.Second {
		t.Errorf("N1: %v", took)
	}

	// A hand edit: one cited line changed, one line added, one cited line
	// deleted.
	agentsID := ids["agents_md"]
	before := d.file("AGENTS.md")
	edited := strings.Replace(before, "- pnpm workspaces only. Never run `npm install` at the root. [M-0071]",
		"- pnpm workspaces only. Never run `npm install` or `yarn` anywhere. [M-0071]", 1)
	edited = strings.Replace(edited, "- Every write tool returns a receipt ID the caller can cite. [M-0112]\n", "", 1)
	edited = strings.Replace(edited, "## Open\n", "## Open\n\n- Prefer named exports in React components.\n", 1)
	if edited == before {
		t.Fatal("the edit changed nothing")
	}
	d.edit("AGENTS.md", edited)
	var obs observationResult
	e.do(call{method: "POST", path: targetPath(agentsID, "/observations"), token: zz, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"path": "AGENTS.md", "content": edited, "device_id": "zz-laptop"}}).ok(http.StatusCreated, &obs)
	if !obs.Drifted || obs.Target.SyncState != "drifted" {
		t.Fatalf("the hand edit wasn't drift: %+v", obs)
	}
	kinds := []string{}
	for _, c := range obs.Observation.Changeset.Changes {
		kinds = append(kinds, c.Kind+":"+c.Ref)
	}
	if !slices.Equal(kinds, []string{"edit:M-0071", "new:", "remove:M-0112"}) {
		t.Errorf("changes = %v", kinds)
	}
	// Memax never writes over it: the daemon holds, and a Keep elsewhere
	// doesn't reach the file.
	var dv driftView
	e.do(call{method: "GET", path: targetPath(agentsID, "/drift"), token: zz}).ok(http.StatusOK, &dv)
	if len(dv.Items) != 1 || dv.Items[0].Compiled != before || dv.Items[0].Observed != edited {
		t.Fatalf("drift view = %+v", dv)
	}

	// Pull: the right proposals, with file:line sources, by the person on
	// the device; the removal waits for a person.
	var pull resolution
	e.do(call{method: "POST", path: targetPath(agentsID, "/drift:pull"), token: zz}).ok(http.StatusOK, &pull)
	if len(pull.Proposals) != 2 {
		t.Fatalf("proposals = %+v", pull.Proposals)
	}
	lineOf := func(text string) string {
		for i, l := range strings.Split(edited, "\n") {
			if strings.Contains(l, text) {
				return "AGENTS.md:" + strconv.Itoa(i+1)
			}
		}
		return "?"
	}
	editP, newP := pull.Proposals[0], pull.Proposals[1]
	if editP.Statement != "pnpm workspaces only. Never run `npm install` or `yarn` anywhere." || editP.State != "proposed" ||
		editP.Sources[0].Ref != lineOf("or `yarn` anywhere") || editP.Sources[0].Kind != "file" {
		t.Errorf("edit proposal = %+v", editP)
	}
	if newP.Statement != "Prefer named exports in React components." || newP.Section != "open_question" ||
		newP.Sources[0].Ref != lineOf("Prefer named exports") {
		t.Errorf("new proposal = %+v", newP)
	}
	var supersedes int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM v2.memory_links WHERE kind = 'supersedes' AND from_memory_id = $1 AND to_memory_id = $2`,
		editP.ID, demo.Memory["M-0071"].ID).Scan(&supersedes)
	if supersedes != 1 {
		t.Error("the edit proposal doesn't supersede M-0071")
	}
	for _, rc := range pull.Receipts[1:] {
		if rc.Action != "proposed" || rc.ActorKind != "person" || *rc.ActorID != demo.ZZ || rc.Via != "cli" {
			t.Errorf("proposal receipt = %+v", rc)
		}
	}
	var removal struct {
		Outcome string
		Ref     string
	}
	if err := e.pool.QueryRow(ctx, `
		SELECT c->>'outcome', c->>'ref' FROM v2.target_observations o, jsonb_array_elements(o.resolution->'changes') c
		 WHERE o.target_id = $1 AND c->>'kind' = 'remove'`, agentsID).Scan(&removal.Outcome, &removal.Ref); err != nil {
		t.Fatal(err)
	}
	if removal.Outcome != "review" || removal.Ref != "M-0112" {
		t.Errorf("removal = %+v", removal)
	}
	var m112 memoryDetailWire
	e.do(call{method: "GET", path: "/v2/memories/M-0112?space=" + devseed.SpaceSlug, token: zz}).ok(http.StatusOK, &m112)
	if m112.Memory.State != "kept" {
		t.Errorf("M-0112 is %s: a deleted line must never forget", m112.Memory.State)
	}
	if pull.Target.SyncState != "in_sync" || d.file("AGENTS.md") != edited {
		t.Errorf("after the pull the file stays as edited: %s", pull.Target.SyncState)
	}

	// Keeping the pulled edit recompiles the file, now over the accepted
	// baseline.
	e.do(call{method: "POST", path: "/v2/memories/" + editP.ID.String() + ":keep", token: zz}).ok(http.StatusOK, nil)
	d.await(10*time.Second, "keeping the pulled edit")
	after := d.file("AGENTS.md")
	if !strings.Contains(after, "or `yarn` anywhere. ["+editP.Ref+"]") || strings.Contains(after, "Prefer named exports") {
		t.Errorf("AGENTS.md after keeping the pulled edit:\n%s", after)
	}

	// Overwrite: the record wins, and the file comes back as compiled.
	d.edit("AGENTS.md", after+"- A stray line.\n")
	e.do(call{method: "POST", path: targetPath(agentsID, "/observations"), token: zz,
		body: map[string]any{"path": "AGENTS.md", "content": after + "- A stray line.\n", "device_id": "zz-laptop"}}).ok(http.StatusCreated, nil)
	var ow resolution
	e.do(call{method: "POST", path: targetPath(agentsID, "/drift:overwrite"), token: zz}).ok(http.StatusOK, &ow)
	if ow.Observations[0].Status != "overwritten" || len(ow.Proposals) != 0 {
		t.Errorf("overwrite = %+v", ow)
	}
	d.await(10*time.Second, "the overwrite")
	if d.file("AGENTS.md") != after {
		t.Errorf("after the overwrite the file is:\n%s", d.file("AGENTS.md"))
	}

	// Stop: the target is off, the file stays, and Keeps don't reach it.
	d.edit("AGENTS.md", after+"- Mine now.\n")
	e.do(call{method: "POST", path: targetPath(agentsID, "/observations"), token: zz,
		body: map[string]any{"path": "AGENTS.md", "content": after + "- Mine now.\n"}}).ok(http.StatusCreated, nil)
	var stop resolution
	e.do(call{method: "POST", path: targetPath(agentsID, "/drift:stop"), token: zz}).ok(http.StatusOK, &stop)
	if stop.Target.SyncState != "off" {
		t.Errorf("stop = %s", stop.Target.SyncState)
	}
	offGen := stop.Target.DirtyGen
	e.do(call{method: "POST", path: "/v2/memories/M-0431:keep?space=" + devseed.SpaceSlug, token: zz}).ok(http.StatusOK, nil)
	d.await(10*time.Second, "a keep with AGENTS.md stopped")
	for _, tg := range e.targets(zz) {
		if tg.ID == agentsID && (tg.SyncState != "off" || tg.DirtyGen != offGen) {
			t.Errorf("a stopped target was dirtied: %+v", tg)
		}
	}
	if d.file("AGENTS.md") != after+"- Mine now.\n" {
		t.Error("a stopped target's file was written")
	}

	// Isolation: a stranger with a Brief and a target of their own sees
	// none of memax-v2, ZZ sees none of theirs, and memax_v2 in the
	// stranger's scope reads only the stranger's rows, filter or no filter.
	stranger := e.user("stranger")
	theirs := e.space(stranger, "project", "side-project")
	tok := e.session(stranger)
	e.do(call{method: "POST", path: briefPath(theirs), token: tok, body: briefBody("Side project")}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: targetsPath(theirs), token: tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, nil)
	e.do(call{method: "GET", path: briefPath(theirs), token: zz}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: targetsPath(theirs), token: zz}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: space + "/brief", token: tok}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: space + "/targets", token: tok}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: targetPath(agentsID, "/preview"), token: tok}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: targetPath(agentsID, "/drift"), token: tok}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: targetPath(agentsID, "/observations"), token: tok,
		body: map[string]any{"path": "AGENTS.md", "content": "x"}}).fails(http.StatusNotFound, "not_found")
	scope, err := e.ledger.UserScope(ctx, stranger)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{"briefs", "brief_versions", "targets", "compile_runs", "target_observations", "memories", "receipts"}
	err = e.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		for _, tbl := range tables {
			var mine, others int
			if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE space_id = $1), count(*) FILTER (WHERE space_id <> $1) FROM v2.`+tbl,
				theirs.id).Scan(&mine, &others); err != nil {
				return err
			}
			if others != 0 {
				t.Errorf("the stranger sees %d rows of v2.%s outside their space", others, tbl)
			}
			if tbl == "targets" && mine != 1 {
				t.Errorf("the stranger sees %d of their own targets", mine)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM v2.targets WHERE space_id = $1`, demo.Space); n != 4 {
		t.Errorf("memax-v2 has %d targets", n)
	}

	// Every write carries its receipt: each row of the record points at a
	// receipt about it, in its space, with the verb that wrote it (the
	// deferred trigger refused anything else at commit; this reads it back).
	checks := []struct{ what, sql string }{
		{"brief versions", `SELECT count(*) FROM v2.brief_versions v LEFT JOIN v2.receipts r ON r.id = v.receipt_id AND r.space_id = v.space_id
			 WHERE v.space_id = $1 AND (r.id IS NULL OR r.action <> 'revised' OR r.object_id <> v.brief_id)`},
		{"targets", `SELECT count(*) FROM v2.targets t LEFT JOIN v2.receipts r ON r.id = t.created_receipt_id AND r.space_id = t.space_id
			 WHERE t.space_id = $1 AND (r.id IS NULL OR r.action <> 'configured' OR r.object_id <> t.id)`},
		{"compile runs", `SELECT count(*) FROM v2.compile_runs c LEFT JOIN v2.receipts r ON r.id = c.created_receipt_id AND r.space_id = c.space_id
			 WHERE c.space_id = $1 AND (r.id IS NULL OR r.action <> 'compiled' OR r.object_id <> c.id OR r.actor_kind <> 'memax')`},
		// A copy-out or MCP target is in sync when it compiles (the compiled
		// receipt covers it); a local file is delivered by a device's ack.
		{"delivered files", `SELECT count(*) FROM v2.compile_runs c JOIN v2.targets t ON t.id = c.target_id
			 WHERE c.space_id = $1 AND c.status = 'delivered' AND t.delivery = 'local'
			   AND NOT EXISTS (SELECT 1 FROM v2.receipts r WHERE r.space_id = c.space_id AND r.object_id = c.id AND r.action = 'delivered')`},
		{"observations", `SELECT count(*) FROM v2.target_observations o LEFT JOIN v2.receipts r ON r.id = o.created_receipt_id AND r.space_id = o.space_id
			 WHERE o.space_id = $1 AND (r.id IS NULL OR r.action <> 'observed' OR r.object_id <> o.target_id)`},
		{"resolved observations", `SELECT count(*) FROM v2.target_observations o LEFT JOIN v2.receipts r ON r.id = o.last_receipt_id AND r.space_id = o.space_id
			 WHERE o.space_id = $1 AND o.status <> 'open' AND (r.id IS NULL OR r.action NOT IN ('pulled', 'overwritten', 'stopped', 'observed'))`},
		{"memories", `SELECT count(*) FROM v2.memories m LEFT JOIN v2.receipts r ON r.id = m.created_receipt_id AND r.space_id = m.space_id
			 WHERE m.space_id = $1 AND (r.id IS NULL OR r.action NOT IN ('kept', 'proposed', 'edited') OR r.object_id <> m.id)`},
		{"latest receipts", `SELECT count(*) FROM (
			SELECT last_receipt_id AS rid, space_id FROM v2.targets UNION ALL
			SELECT last_receipt_id, space_id FROM v2.compile_runs UNION ALL
			SELECT last_receipt_id, space_id FROM v2.target_observations UNION ALL
			SELECT last_receipt_id, space_id FROM v2.briefs UNION ALL
			SELECT last_receipt_id, space_id FROM v2.brief_versions UNION ALL
			SELECT last_receipt_id, space_id FROM v2.memories) x
			LEFT JOIN v2.receipts r ON r.id = x.rid AND r.space_id = x.space_id
			WHERE x.space_id = $1 AND r.id IS NULL`},
	}
	for _, c := range checks {
		if bad := e.count(c.sql, demo.Space); bad != 0 {
			t.Errorf("%s: %d rows without their receipt", c.what, bad)
		}
	}
	if n := e.count(`SELECT count(*) FROM v2.compile_runs WHERE space_id = $1 AND status = 'delivered'`, demo.Space); n == 0 {
		t.Error("no run was ever delivered")
	}
}

type memoryDetailWire struct {
	Memory memory `json:"memory"`
}
