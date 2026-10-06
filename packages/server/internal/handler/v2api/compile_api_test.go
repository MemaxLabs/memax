package v2api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The wire shapes these tests read back.
type briefItem struct {
	Ref   string   `json:"ref"`
	Text  string   `json:"text"`
	Cites []string `json:"cites"`
}

type briefSection struct {
	Key     string      `json:"key"`
	Heading string      `json:"heading"`
	Items   []briefItem `json:"items"`
}

type brief struct {
	ID       uuid.UUID      `json:"id"`
	Ref      string         `json:"ref"`
	Version  int            `json:"version"`
	Title    string         `json:"title"`
	Sections []briefSection `json:"sections"`
	Facts    int            `json:"facts"`
	Current  bool           `json:"current"`
	Receipt  *receipt       `json:"receipt"`
}

type briefResult struct {
	Outcome  string    `json:"outcome"`
	Brief    brief     `json:"brief"`
	Receipts []receipt `json:"receipts"`
}

type compileRun struct {
	ID          uuid.UUID `json:"id"`
	Ref         string    `json:"ref"`
	Status      string    `json:"status"`
	DriftSHA256 string    `json:"drift_sha256"`
	Files       []struct {
		Path        string `json:"path"`
		Label       string `json:"label"`
		DriftSHA256 string `json:"drift_sha256"`
	} `json:"files"`
}

type target struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Path        string    `json:"path"`
	Label       string    `json:"label"`
	SyncState   string    `json:"sync_state"`
	Version     int       `json:"version"`
	DirtyGen    int64     `json:"dirty_gen"`
	CompiledGen int64     `json:"compiled_gen"`
	OpenDrift   int       `json:"open_drift"`
	Settings    struct {
		Include    string `json:"include"`
		SizeBudget int    `json:"size_budget"`
	} `json:"settings"`
	LastCompile *compileRun `json:"last_compile"`
	Delivered   *struct {
		Compile string `json:"compile"`
		SHA256  string `json:"sha256"`
	} `json:"delivered"`
}

type targetResult struct {
	Outcome  string    `json:"outcome"`
	Target   target    `json:"target"`
	Receipts []receipt `json:"receipts"`
}

type preview struct {
	Target  target      `json:"target"`
	Compile *compileRun `json:"compile"`
	Reads   string      `json:"reads"`
	Files   []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	} `json:"files"`
	Copies []struct {
		Label   string `json:"label"`
		Content string `json:"content"`
	} `json:"copies"`
}

type observation struct {
	ID        uuid.UUID `json:"id"`
	Path      string    `json:"path"`
	Status    string    `json:"status"`
	Changeset struct {
		Changes []struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"changes"`
	} `json:"changeset"`
}

type observationResult struct {
	Drifted     bool         `json:"drifted"`
	Target      target       `json:"target"`
	Observation *observation `json:"observation"`
	Receipts    []receipt    `json:"receipts"`
}

type driftView struct {
	Target target `json:"target"`
	Items  []struct {
		Observation observation `json:"observation"`
		Compiled    string      `json:"compiled"`
		Observed    string      `json:"observed"`
	} `json:"items"`
}

type resolution struct {
	Target       target        `json:"target"`
	Observations []observation `json:"observations"`
	Proposals    []memory      `json:"proposals"`
	Receipts     []receipt     `json:"receipts"`
}

func briefPath(sp space) string   { return "/v2/spaces/" + sp.id.String() + "/brief" }
func targetsPath(sp space) string { return "/v2/spaces/" + sp.id.String() + "/targets" }
func targetPath(id uuid.UUID, rest string) string {
	return "/v2/targets/" + id.String() + rest
}

func briefBody(title string, sections ...map[string]any) map[string]any {
	if sections == nil {
		sections = []map[string]any{}
	}
	return map[string]any{"title": title, "sections": sections}
}

func TestBriefEndpoints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, vi := e.user("zz"), e.user("vi")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(sp, vi, "viewer")
	tok := e.session(zz)
	river := e.remember(tok, sp, "Background jobs run on River.").Memory
	pnpm := e.remember(tok, sp, "pnpm workspaces only.").Memory

	e.do(call{method: "GET", path: briefPath(sp), token: tok}).fails(http.StatusNotFound, "not_found")

	first := briefBody("Memax V2 engineering brief",
		map[string]any{"key": "decisions", "heading": "Decisions", "items": []map[string]any{{"ref": strings.ToLower(river.Ref)}}})
	var res briefResult
	r := e.do(call{method: "POST", path: briefPath(sp), token: tok, body: first})
	r.ok(http.StatusCreated, &res)
	if res.Brief.Ref != "B-0001" || res.Brief.Version != 1 || res.Brief.Sections[0].Items[0].Ref != river.Ref ||
		res.Receipts[0].Action != "revised" || r.header.Get("ETag") != `"1"` {
		t.Errorf("first Brief = %+v, ETag %q", res, r.header.Get("ETag"))
	}
	var got brief
	r = e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/brief", token: e.session(vi)})
	r.ok(http.StatusOK, &got)
	if got.Ref != "B-0001" || !got.Current || got.Receipt == nil || got.Receipt.Action != "revised" || r.header.Get("ETag") != `"1"` {
		t.Errorf("GET brief = %+v", got)
	}

	second := briefBody("Memax V2 engineering brief",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{
			{"ref": pnpm.Ref}, {"text": "Ask before changing the toolchain.", "cites": []string{pnpm.Ref, river.Ref}}}})
	e.do(call{method: "POST", path: briefPath(sp), token: tok, body: second}).fails(http.StatusPreconditionRequired, "precondition_required")
	key := uuid.NewString()
	r = e.do(call{method: "POST", path: briefPath(sp), token: tok, body: second,
		header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}})
	r.ok(http.StatusCreated, &res)
	if res.Brief.Version != 2 || res.Brief.Facts != 2 || r.header.Get("ETag") != `"2"` {
		t.Errorf("second version = %+v", res.Brief)
	}
	replay := e.do(call{method: "POST", path: briefPath(sp), token: tok, body: second,
		header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}})
	replay.ok(http.StatusCreated, nil)
	if replay.header.Get("Idempotent-Replayed") != "true" {
		t.Error("a replayed revision isn't marked")
	}
	e.do(call{method: "POST", path: briefPath(sp), token: tok, body: second,
		header: map[string]string{"If-Match": `"1"`}}).fails(http.StatusPreconditionFailed, "edit_clash")

	var versions page[brief]
	e.do(call{method: "GET", path: briefPath(sp) + "/versions?limit=1", token: tok}).ok(http.StatusOK, &versions)
	if len(versions.Items) != 1 || versions.Items[0].Version != 2 || !versions.HasMore {
		t.Fatalf("versions = %+v", versions)
	}
	e.do(call{method: "GET", path: briefPath(sp) + "/versions?cursor=" + versions.NextCursor, token: tok}).ok(http.StatusOK, &versions)
	if len(versions.Items) != 1 || versions.Items[0].Version != 1 || versions.Items[0].Current {
		t.Errorf("versions page 2 = %+v", versions)
	}

	// Who may revise, and what may be cited.
	if f := e.do(call{method: "POST", path: briefPath(sp), token: e.session(vi), body: second,
		header: map[string]string{"If-Match": `"2"`}}).fails(http.StatusForbidden, "refused"); f.Details.Policy.Code != "viewer" {
		t.Errorf("viewer: %+v", f.Details.Policy)
	}
	agent, _ := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	if f := e.do(call{method: "POST", path: briefPath(sp), token: agent, body: second,
		header: map[string]string{"If-Match": `"2"`}}).fails(http.StatusForbidden, "refused"); f.Details.Policy.Code != "brief_by_person" {
		t.Errorf("agent: %+v", f.Details.Policy)
	}
	var prop result
	e.do(call{method: "POST", path: memoriesPath(sp), token: agent, body: remember("Deploy to Fly.io.", "decisions")}).ok(http.StatusCreated, &prop)
	bad := briefBody("Brief", map[string]any{"key": "decisions", "heading": "Decisions", "items": []map[string]any{{"ref": prop.Memory.Ref}}})
	if f := e.do(call{method: "POST", path: briefPath(sp), token: tok, body: bad,
		header: map[string]string{"If-Match": `"2"`}}).fails(http.StatusBadRequest, "invalid_request"); f.Details.Field != "sections.items.ref" {
		t.Errorf("a proposal in the Brief: %+v", f)
	}
	// Another person's space doesn't exist for you.
	other := e.user("other")
	e.do(call{method: "GET", path: briefPath(sp), token: e.session(other)}).fails(http.StatusNotFound, "not_found")
}

func TestTargetEndpoints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	river := e.remember(tok, sp, "Background jobs run on River.").Memory
	pnpm := e.remember(tok, sp, "pnpm workspaces only.").Memory
	e.do(call{method: "POST", path: briefPath(sp), token: tok, body: briefBody("Memax V2 engineering brief",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{{"ref": river.Ref}, {"ref": pnpm.Ref}}})}).
		ok(http.StatusCreated, nil)

	ids := map[string]uuid.UUID{}
	for _, kind := range []string{"agents_md", "claude_md", "cursor_mdc", "chatgpt"} {
		var res targetResult
		r := e.do(call{method: "POST", path: targetsPath(sp), token: tok, body: map[string]any{"kind": kind}})
		r.ok(http.StatusCreated, &res)
		if res.Target.Kind != kind || res.Target.SyncState != "compiling" || res.Receipts[0].Action != "configured" ||
			r.header.Get("Location") != "/v2/targets/"+res.Target.ID.String() || r.header.Get("ETag") != `"1"` {
			t.Errorf("%s = %+v", kind, res)
		}
		ids[kind] = res.Target.ID
	}
	e.do(call{method: "POST", path: targetsPath(sp), token: tok, body: map[string]any{"kind": "agents_md"}}).
		fails(http.StatusBadRequest, "invalid_request")
	var list page[target]
	e.do(call{method: "GET", path: targetsPath(sp), token: tok}).ok(http.StatusOK, &list)
	if len(list.Items) != 4 || list.Items[0].Kind != "agents_md" || list.Items[3].Label != "ChatGPT project" {
		t.Fatalf("targets = %+v", list.Items)
	}

	agents := ids["agents_md"]
	e.compileAll()
	var pv preview
	e.do(call{method: "GET", path: targetPath(agents, "/preview"), token: tok}).ok(http.StatusOK, &pv)
	if pv.Compile == nil || pv.Compile.Status != "compiled" || len(pv.Files) != 1 || pv.Files[0].Path != "AGENTS.md" ||
		!strings.Contains(pv.Files[0].Content, "Background jobs run on River. ["+river.Ref+"]") || pv.Target.SyncState != "pending_delivery" {
		t.Fatalf("preview = %+v", pv)
	}
	var gpt preview
	e.do(call{method: "GET", path: targetPath(ids["chatgpt"], "/preview"), token: tok}).ok(http.StatusOK, &gpt)
	if len(gpt.Copies) != 1 || gpt.Target.SyncState != "in_sync" {
		t.Errorf("chatgpt preview = %+v", gpt)
	}
	var runs page[compileRun]
	e.do(call{method: "GET", path: targetPath(agents, "/runs?limit=10"), token: tok}).ok(http.StatusOK, &runs)
	if len(runs.Items) != 1 || runs.Items[0].Ref != pv.Compile.Ref {
		t.Errorf("runs = %+v", runs)
	}

	// Settings, If-Match, and Compile now.
	var tr targetResult
	r := e.do(call{method: "PATCH", path: targetPath(ids["claude_md"], ""), token: tok,
		body: map[string]any{"settings": map[string]any{"include": "kept_only", "size_budget": 16384}}, header: map[string]string{"If-Match": `"1"`}})
	r.ok(http.StatusOK, &tr)
	if tr.Target.Settings.Include != "kept_only" || tr.Target.Settings.SizeBudget != 16384 || tr.Target.Version != 2 || r.header.Get("ETag") != `"2"` {
		t.Errorf("configured = %+v", tr.Target)
	}
	e.do(call{method: "PATCH", path: targetPath(ids["claude_md"], ""), token: tok,
		body: map[string]any{"settings": map[string]any{"stale": "omit"}}, header: map[string]string{"If-Match": `"1"`}}).
		fails(http.StatusPreconditionFailed, "edit_clash")
	e.do(call{method: "PATCH", path: targetPath(ids["claude_md"], ""), token: tok,
		body: map[string]any{"settings": map[string]any{"include": "kept_only"}}}).ok(http.StatusOK, &tr)
	if len(tr.Receipts) != 0 {
		t.Errorf("an unchanged PATCH wrote %d receipts", len(tr.Receipts))
	}
	e.do(call{method: "POST", path: targetPath(agents, ":compile"), token: tok}).ok(http.StatusAccepted, &tr)
	if tr.Target.SyncState != "compiling" || tr.Receipts[0].Action != "requested" {
		t.Errorf("compile now = %+v", tr)
	}
	e.compileAll()

	// Deliver, then a hand edit.
	e.do(call{method: "GET", path: targetPath(agents, "/preview"), token: tok}).ok(http.StatusOK, &pv)
	e.do(call{method: "POST", path: targetPath(agents, "/deliveries"), token: tok,
		body: map[string]any{"compile": pv.Compile.Ref, "sha256": strings.Repeat("0", 64)}}).fails(http.StatusBadRequest, "invalid_request")
	var del struct {
		Target   target     `json:"target"`
		Compile  compileRun `json:"compile"`
		Receipts []receipt  `json:"receipts"`
	}
	e.do(call{method: "POST", path: targetPath(agents, "/deliveries"), token: tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"compile": pv.Compile.Ref, "sha256": pv.Compile.DriftSHA256}}).ok(http.StatusOK, &del)
	if del.Target.SyncState != "in_sync" || del.Compile.Status != "delivered" || del.Receipts[0].Action != "delivered" {
		t.Fatalf("delivery = %+v", del)
	}
	content := pv.Files[0].Content
	var obs observationResult
	e.do(call{method: "POST", path: targetPath(agents, "/observations"), token: tok,
		body: map[string]any{"path": "AGENTS.md", "content": content, "device_id": "zz-laptop"}}).ok(http.StatusOK, &obs)
	if obs.Drifted || obs.Observation != nil || len(obs.Receipts) != 0 {
		t.Errorf("the delivered file is no drift: %+v", obs)
	}
	edited := strings.Replace(content, "pnpm workspaces only.", "pnpm workspaces only, never npm.", 1) + "- Prefer named exports.\n"
	e.do(call{method: "POST", path: targetPath(agents, "/observations"), token: tok,
		body: map[string]any{"path": "AGENTS.md", "content": edited, "device_id": "zz-laptop", "commit": "a41e9c2"}}).ok(http.StatusCreated, &obs)
	if !obs.Drifted || obs.Target.SyncState != "drifted" || obs.Observation == nil || len(obs.Observation.Changeset.Changes) != 2 {
		t.Fatalf("hand edit = %+v", obs)
	}
	var dv driftView
	e.do(call{method: "GET", path: targetPath(agents, "/drift"), token: tok}).ok(http.StatusOK, &dv)
	if len(dv.Items) != 1 || dv.Items[0].Compiled != content || dv.Items[0].Observed != edited {
		t.Errorf("drift = %+v", dv)
	}
	var res resolution
	e.do(call{method: "POST", path: targetPath(agents, "/drift:pull"), token: tok}).ok(http.StatusOK, &res)
	if len(res.Proposals) != 2 || res.Observations[0].Status != "pulled" || res.Target.SyncState != "in_sync" || res.Receipts[0].Action != "pulled" {
		t.Fatalf("pull = %+v", res)
	}
	for _, p := range res.Proposals {
		if p.State != "proposed" || len(p.Sources) != 1 || !strings.HasPrefix(p.Sources[0].Ref, "AGENTS.md:") {
			t.Errorf("proposal = %+v", p)
		}
	}
	e.do(call{method: "POST", path: targetPath(agents, "/drift:pull"), token: tok}).fails(http.StatusConflict, "invalid_transition")

	// Overwrite, then stop.
	e.do(call{method: "POST", path: targetPath(agents, "/observations"), token: tok,
		body: map[string]any{"path": "AGENTS.md", "content": edited + "- Again.\n"}}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: targetPath(agents, "/drift:overwrite"), token: tok, body: map[string]any{"reason": "the record wins"}}).ok(http.StatusOK, &res)
	if res.Target.SyncState != "compiling" || res.Observations[0].Status != "overwritten" {
		t.Errorf("overwrite = %+v", res.Target)
	}
	e.do(call{method: "POST", path: targetPath(agents, "/observations"), token: tok,
		body: map[string]any{"path": "AGENTS.md", "content": edited + "- And again.\n"}}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: targetPath(agents, "/drift:stop"), token: tok}).ok(http.StatusOK, &res)
	if res.Target.SyncState != "off" || res.Receipts[0].Action != "stopped" {
		t.Errorf("stop = %+v", res.Target)
	}
	e.do(call{method: "POST", path: targetPath(agents, ":compile"), token: tok}).fails(http.StatusConflict, "invalid_transition")
	e.do(call{method: "PATCH", path: targetPath(agents, ""), token: tok, body: map[string]any{"enabled": true}}).ok(http.StatusOK, &tr)
	if tr.Target.SyncState != "compiling" {
		t.Errorf("turned back on: %s", tr.Target.SyncState)
	}

	// Addressing and isolation.
	e.do(call{method: "GET", path: "/v2/targets/AGENTS.md/preview", token: tok, invalid: true}).fails(http.StatusBadRequest, "invalid_request")
	stranger := e.session(e.user("stranger"))
	e.do(call{method: "GET", path: targetPath(agents, "/preview"), token: stranger}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: targetPath(agents, "/runs"), token: stranger}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "PATCH", path: targetPath(agents, ""), token: stranger, body: map[string]any{"enabled": false}}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: targetsPath(sp), token: stranger}).fails(http.StatusNotFound, "not_found")
}

// Without a compile service the record still works; what needs the
// compiler answers 503.
func TestTargetEndpointsWithoutACompileService(t *testing.T) {
	t.Parallel()
	e := newEnv(t, v2api.WithCompile(nil))
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	e.do(call{method: "POST", path: briefPath(sp), token: tok, body: briefBody("Brief")}).ok(http.StatusCreated, nil)
	var tr targetResult
	e.do(call{method: "POST", path: targetsPath(sp), token: tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, &tr)
	for _, c := range []call{
		{method: "GET", path: targetPath(tr.Target.ID, "/preview"), token: tok},
		{method: "GET", path: targetPath(tr.Target.ID, "/drift"), token: tok},
		{method: "POST", path: targetPath(tr.Target.ID, "/observations"), token: tok, body: map[string]any{"path": "AGENTS.md", "content": "x"}},
	} {
		e.do(c).fails(http.StatusServiceUnavailable, "unavailable")
	}
}
