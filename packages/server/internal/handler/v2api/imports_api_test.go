package v2api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

type spaceOut struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Kind        string    `json:"kind"`
	Role        string    `json:"role"`
	Repository  string    `json:"repository"`
	V2EnabledAt *string   `json:"v2_enabled_at"`
}

// memax init's first call for a repository with no space: a project space
// on V2, idempotent by key, never by an agent.
func TestCreateAndSwitchSpaces(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	tok := e.session(zz)
	key := map[string]string{"Idempotency-Key": "create-1"}
	var sp spaceOut
	r := e.do(call{method: "POST", path: "/v2/spaces", token: tok, header: key,
		body: map[string]any{"name": "Acme web", "repository": "acme/web"}}).ok(http.StatusCreated, &sp)
	if sp.Slug != "acme-web" || sp.Kind != "project" || sp.Role != "owner" || sp.V2EnabledAt == nil || sp.Repository != "acme/web" {
		t.Fatalf("created %+v", sp)
	}
	if r.header.Get("Idempotent-Replayed") != "" {
		t.Error("first create says replayed")
	}
	var again spaceOut
	r = e.do(call{method: "POST", path: "/v2/spaces", token: tok, header: key,
		body: map[string]any{"name": "Acme web", "repository": "acme/web"}}).ok(http.StatusCreated, &again)
	if again.ID != sp.ID || r.header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("retry: %+v replayed=%q", again, r.header.Get("Idempotent-Replayed"))
	}
	e.do(call{method: "POST", path: "/v2/spaces", token: tok, body: map[string]any{"name": "x", "slug": "acme-web"}}).
		fails(http.StatusConflict, "slug_taken")
	e.do(call{method: "POST", path: "/v2/spaces", token: tok, header: key, body: map[string]any{"name": "Other"}}).
		fails(http.StatusUnprocessableEntity, "idempotency_key_reused")
	keyTok, _ := e.apiKey(zz, keyOpts{perms: []string{"memory:read", "memory:write"}, agent: "codex"})
	if err := e.do(call{method: "POST", path: "/v2/spaces", token: keyTok, body: map[string]any{"name": "keyed"}}).
		fails(http.StatusForbidden, "refused"); err.Details.Policy.Code != policy.CodeSpaceByPerson {
		t.Errorf("an API key creates a space: %+v", err.Details.Policy)
	}

	// The new space is in the person's list, on V2.
	var list page[spaceOut]
	e.do(call{method: "GET", path: "/v2/spaces", token: tok}).ok(http.StatusOK, &list)
	found := false
	for _, s := range list.Items {
		found = found || (s.ID == sp.ID && s.V2EnabledAt != nil)
	}
	if !found {
		t.Errorf("the new space isn't listed on V2: %+v", list.Items)
	}

	// An empty personal space switches within the request.
	personal := e.space(zz, policy.SpacePersonal, "personal")
	var switched struct {
		Space      spaceOut `json:"space"`
		State      string   `json:"state"`
		Background bool     `json:"background"`
	}
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + ":switch", token: tok}).ok(http.StatusOK, &switched)
	if switched.Space.V2EnabledAt == nil || switched.Space.ID != personal.id || switched.State != "switched" || switched.Background {
		t.Errorf("switched %+v", switched)
	}
	e.do(call{method: "POST", path: "/v2/spaces/" + uuid.NewString() + ":switch", token: tok}).fails(http.StatusNotFound, "not_found")
}

func importBody(items ...map[string]any) map[string]any {
	return map[string]any{
		"client":      "memax-cli test",
		"session_ref": "init-test",
		"files": []map[string]any{{"path": "CLAUDE.md", "kind": "claude_md", "agent": "claude-code", "location": "repository",
			"trust": "repository", "statements": len(items), "skipped": 1, "hidden_characters": 0}},
		"skipped": []map[string]any{{"ref": "CLAUDE.md:31", "reason": "secret", "detail": "GitHub token"}},
		"items":   items,
	}
}

func importLine(key, ref, statement string) map[string]any {
	return map[string]any{"key": key, "ref": ref, "location": "repository", "statement": statement, "section": "conventions",
		"sources": []map[string]any{{"kind": "file", "ref": ref, "locator": map[string]any{"path": ref}}}}
}

type importOut struct {
	Import struct {
		ID     uuid.UUID `json:"id"`
		Counts struct {
			Items, Proposed, Folded, Existing, Refused, Conflicts int
		} `json:"counts"`
		Check struct {
			State string `json:"state"`
		} `json:"check"`
	} `json:"import"`
	Items []struct {
		Key     string `json:"key"`
		Outcome string `json:"outcome"`
		Memory  struct {
			ID  uuid.UUID `json:"id"`
			Ref string    `json:"ref"`
		} `json:"memory"`
		Policy struct {
			Code string `json:"code"`
		} `json:"policy"`
	} `json:"items"`
}

type importView struct {
	Items []struct {
		Ref     string `json:"ref"`
		Outcome string `json:"outcome"`
	} `json:"items"`
	Memories []struct {
		Memory   memory `json:"memory"`
		Items    int    `json:"items"`
		Bulk     bool   `json:"bulk"`
		Held     string `json:"held"`
		Conflict int    `json:"conflict"`
	} `json:"memories"`
	Conflicts []struct {
		N       int    `json:"n"`
		Subject string `json:"subject"`
		State   string `json:"state"`
		Members []struct {
			Ref string `json:"ref"`
		} `json:"members"`
	} `json:"conflicts"`
	Progress struct {
		Ready   bool `json:"ready"`
		Working int  `json:"working"`
	} `json:"progress"`
}

// recordCheck stands in for the judge's import check.
func (e *env) recordCheck(space, importID uuid.UUID, members ...uuid.UUID) {
	e.t.Helper()
	ctx := context.Background()
	scope, err := e.ledger.SpaceScope(ctx, space)
	if err != nil {
		e.t.Fatal(err)
	}
	conf := 0.95
	cmd := &ledger.RecordImportCheck{
		Meta:    ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax, Name: "Memax"}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: "judge-import:" + importID.String()},
		SpaceID: space, Import: importID, State: ledger.CheckChecked,
	}
	if len(members) > 0 {
		cmd.Conflicts = []ledger.ImportConflictInput{{Members: members, Subject: "Test command", Confidence: &conf}}
	}
	if _, err := e.ledger.Apply(ctx, cmd); err != nil {
		e.t.Fatal(err)
	}
}

// The init flow over /v2: upload, read the import back, settle the
// disagreement, keep the rest in bulk.
func TestImportSettleAndKeepInBulk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	tok := e.session(zz)
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	key := map[string]string{"Idempotency-Key": "import-1", "X-Memax-Via": "cli"}
	body := importBody(
		importLine("a", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		importLine("b", "AGENTS.md:8", "Run `npm run test` before committing."),
		importLine("c", "CLAUDE.md:4", "pnpm workspaces only."),
		importLine("d", "AGENTS.md:3", "pnpm workspaces only"),
		importLine("e", "CLAUDE.md:9", "Commit messages are short and imperative."),
	)
	var out importOut
	r := e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: tok, header: key, body: body}).
		ok(http.StatusCreated, &out)
	if loc := r.header.Get("Location"); loc != "/v2/spaces/"+sp.id.String()+"/imports/"+out.Import.ID.String() {
		t.Errorf("Location %q", loc)
	}
	if c := out.Import.Counts; c.Items != 5 || c.Proposed != 4 || c.Folded != 1 {
		t.Fatalf("counts %+v", c)
	}
	var replay importOut
	r = e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: tok, header: key, body: body}).
		ok(http.StatusCreated, &replay)
	if replay.Import.ID != out.Import.ID || r.header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("replay: %v %q", replay.Import.ID, r.header.Get("Idempotent-Replayed"))
	}
	// Every statement's receipt says it came through the import, from the run.
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND via = 'import' AND session_ref = 'init-test'`, sp.id); n != 4 {
		t.Errorf("%d import receipts, want 4", n)
	}

	a, b := out.Items[0].Memory, out.Items[1].Memory
	e.recordCheck(sp.id, out.Import.ID, a.ID, b.ID)
	var v importView
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/imports/" + out.Import.ID.String(), token: tok}).ok(http.StatusOK, &v)
	if len(v.Conflicts) != 1 || v.Conflicts[0].Subject != "Test command" || len(v.Conflicts[0].Members) != 2 {
		t.Fatalf("conflicts %+v", v.Conflicts)
	}
	var bulk []string
	for _, m := range v.Memories {
		if m.Bulk {
			bulk = append(bulk, m.Memory.Ref)
		}
		if m.Memory.Statement == "pnpm workspaces only." && m.Items != 2 {
			t.Errorf("the fold stands for %d statements, want 2", m.Items)
		}
	}
	if len(bulk) != 0 {
		// Nothing has been judged yet: the judge's verdicts are pending.
		t.Errorf("bulk before the judge: %v", bulk)
	}
	var list page[struct {
		ID uuid.UUID `json:"id"`
	}]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/imports", token: tok}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != out.Import.ID {
		t.Errorf("list %+v", list.Items)
	}

	// Settle the disagreement: keep CLAUDE.md's test command.
	var settled struct {
		Outcome  string   `json:"outcome"`
		Memories []memory `json:"memories"`
		Conflict struct {
			State  string `json:"state"`
			Choice string `json:"choice"`
		} `json:"conflict"`
	}
	path := "/v2/spaces/" + sp.slug + "/imports/" + out.Import.ID.String() + "/conflicts/1:settle"
	e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"choice": "keep_one", "keep": a.Ref}}).ok(http.StatusOK, &settled)
	if settled.Conflict.State != "settled" || settled.Conflict.Choice != "keep_one" {
		t.Errorf("settled %+v", settled.Conflict)
	}
	states := map[string]string{}
	for _, m := range settled.Memories {
		states[m.Ref] = m.Lifecycle
	}
	if states[a.Ref] != "kept" || states[b.Ref] != "rejected" {
		t.Errorf("after settling: %v", states)
	}
	e.do(call{method: "POST", path: path, token: tok, body: map[string]any{"choice": "keep_all"}}).fails(http.StatusConflict, "invalid_transition")

	// Keep the rest in bulk: one of them is already kept, one isn't known.
	c, d := out.Items[2].Memory, out.Items[4].Memory
	var kept struct {
		Items []struct {
			Memory  string `json:"memory"`
			Ref     string `json:"ref"`
			Outcome string `json:"outcome"`
			State   string `json:"state"`
			Error   struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"items"`
		Applied, Refused, Failed int
	}
	bulkKey := map[string]string{"Idempotency-Key": "keep-1", "X-Memax-Via": "cli"}
	bulkBody := map[string]any{"items": []map[string]any{{"memory": c.Ref, "version": 1}, {"memory": d.ID.String()},
		{"memory": a.Ref}, {"memory": "M-9999"}}}
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:keep", token: tok, header: bulkKey, body: bulkBody}).
		ok(http.StatusOK, &kept)
	if kept.Applied != 2 || kept.Failed != 2 || kept.Items[0].State != "kept" || kept.Items[2].Error.Code != "invalid_transition" ||
		kept.Items[3].Error.Code != "not_found" {
		t.Fatalf("bulk keep %+v", kept)
	}
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'kept' AND assurance = 'client_attested'`, sp.id); n != 3 {
		t.Errorf("%d keeps, want 3 (the settled one and two in bulk)", n)
	}
	// A retry keeps nothing twice.
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:keep", token: tok, header: bulkKey, body: bulkBody}).
		ok(http.StatusOK, &kept)
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'kept'`, sp.id); n != 3 {
		t.Errorf("after the retry, %d keeps", n)
	}

	// Bulk reject what's left; an agent's key can't keep in bulk.
	var rejected struct{ Applied, Refused, Failed int }
	p := e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: tok,
		body: importBody(importLine("z", "GEMINI.md:1", "Use bun for scripts."))})
	var second importOut
	p.ok(http.StatusCreated, &second)
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:reject", token: tok,
		body: map[string]any{"items": []map[string]any{{"memory": second.Items[0].Memory.Ref}}}}).ok(http.StatusOK, &rejected)
	if rejected.Applied != 1 {
		t.Errorf("bulk reject %+v", rejected)
	}
	keyTok, _ := e.apiKey(zz, keyOpts{perms: []string{"memory:read", "memory:write"}, agent: "codex"})
	var byKey struct{ Applied, Refused, Failed int }
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:keep", token: keyTok,
		body: map[string]any{"items": []map[string]any{{"memory": d.Ref}}}}).ok(http.StatusOK, &byKey)
	if byKey.Refused != 1 {
		t.Errorf("an API key keeps in bulk: %+v", byKey)
	}
}

// An import never keeps anything, and an agent at Read can't import.
func TestImportIsAlwaysAProposal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	readTok, _ := e.apiKey(zz, keyOpts{perms: []string{"memory:read"}, agent: "cursor"})
	if err := e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: readTok,
		body: importBody(importLine("a", "CLAUDE.md:1", "Use pnpm."))}).fails(http.StatusForbidden, "refused"); err.Details.Policy.Code == "" {
		t.Error("no policy code on the refusal")
	}
	var out importOut
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: e.session(zz),
		body: importBody(importLine("a", "CLAUDE.md:1", "Use pnpm."),
			map[string]any{"key": "b", "location": "home", "statement": "I prefer short answers.", "section": "preferences",
				"sources": []map[string]any{{"kind": "file", "ref": "~/.claude/CLAUDE.md:2", "trust": "person"}}})}).
		ok(http.StatusCreated, &out)
	if n := e.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, sp.id); n != 0 {
		t.Errorf("%d kept by an import", n)
	}
	var m struct {
		Memory memory `json:"memory"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + out.Items[1].Memory.ID.String(), token: e.session(zz)}).ok(http.StatusOK, &m)
	if m.Memory.Trust != "person" || m.Memory.Sources[0].Trust != "person" {
		t.Errorf("the person's own file: trust %s", m.Memory.Trust)
	}
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/imports", token: e.session(zz), invalid: true,
		body: map[string]any{"items": []map[string]any{{"key": "a b", "location": "repository", "statement": "x", "section": "conventions"}}}}).
		fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/imports/" + uuid.NewString(), token: e.session(zz)}).
		fails(http.StatusNotFound, "not_found")
}
