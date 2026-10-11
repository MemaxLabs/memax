package v2api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// scoped is a memory as the tests read its scope back.
type scoped struct {
	Ref     string `json:"ref"`
	Version int    `json:"version"`
	Scope   struct {
		Paths []string `json:"paths"`
		Repos []string `json:"repos"`
	} `json:"scope"`
}

// The repository scope over /v2 (personal-layer design §3.3): an import
// item's scope.repos (any spelling) arrives as owner/name, stays through
// Keep, is in the export, and the memory's page widens it to everywhere
// with :scope, one scoped receipt; asking again changes nothing, agents
// and API keys are refused, a repository without its owner is a 400, and a
// forgotten memory can't change.
func TestRepoScopeOverV2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "personal")

	line := func(key, ref, statement string, repos ...string) map[string]any {
		it := map[string]any{"key": key, "ref": ref, "location": "home", "statement": statement, "section": "preferences",
			"sources": []map[string]any{{"kind": "file", "ref": ref, "trust": "person",
				"locator": map[string]any{"path": "CLAUDE.local.md", "file_kind": "claude_local"}}}}
		if len(repos) > 0 {
			it["scope"] = map[string]any{"repos": repos}
		}
		return it
	}
	body := map[string]any{
		"client": "memax-cli test", "session_ref": "init-0000aaaa",
		"files": []map[string]any{{"path": "CLAUDE.local.md", "kind": "claude_local", "agent": "claude-code", "location": "home",
			"trust": "person", "statements": 2, "skipped": 0, "hidden_characters": 0}},
		"items": []map[string]any{
			line("a", "CLAUDE.local.md:3", "Use the staging database when working on acme.", "Acme/Web", "acme/web"),
			line("b", "CLAUDE.local.md:5", "Answer in British English."),
		},
	}
	var out importOut
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/imports", token: tok,
		header: map[string]string{"Idempotency-Key": "init-0000aaaa:" + personal.id.String() + ":0", "X-Memax-Via": "cli"}, body: body}).
		ok(http.StatusCreated, &out)
	ref := out.Items[0].Memory.Ref
	var got struct {
		Memory scoped `json:"memory"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + ref + "?space=" + personal.slug, token: tok}).ok(http.StatusOK, &got)
	if !slices.Equal(got.Memory.Scope.Repos, []string{"acme/web"}) {
		t.Fatalf("scope.repos = %v, want [acme/web]", got.Memory.Scope.Repos)
	}

	var kept struct {
		Memory scoped `json:"memory"`
	}
	e.do(call{method: "POST", path: "/v2/memories/" + ref + ":keep?space=" + personal.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, &kept)
	if !slices.Equal(kept.Memory.Scope.Repos, []string{"acme/web"}) {
		t.Errorf("after Keep, scope.repos = %v", kept.Memory.Scope.Repos)
	}

	_, files, rec := readExport(t, e.exportSpace(tok, personal, "export-1"))
	var found bool
	for _, m := range rec.Memories {
		if m.Memory.Ref == ref {
			found = slices.Equal(ledger.ScopeRepos(m.Memory.Applies), []string{"acme/web"})
		}
	}
	var md string
	for name, b := range files {
		if strings.HasSuffix(name, ref+".md") {
			md = string(b)
		}
	}
	if !found || !strings.Contains(md, `scope: {"repos": ["acme/web"]}`) {
		t.Errorf("the export's %s: found=%v\n%s", ref, found, md)
	}

	path := "/v2/memories/" + ref + ":scope?space=" + personal.slug
	// Agents (an API key is an agent connection) never change where a
	// memory applies.
	key, _ := e.apiKey(zz, keyOpts{})
	if got := e.do(call{method: "POST", path: path, token: key, body: map[string]any{"repos": []string{}}}).
		fails(http.StatusForbidden, "refused"); got.Details.Policy.Code != policy.CodePersonMustReview {
		t.Errorf("api key: %+v", got.Details.Policy)
	}
	e.do(call{method: "POST", path: path, token: tok, body: map[string]any{"repos": []string{"web"}}, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "POST", path: path, token: tok, body: map[string]any{}, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"If-Match": `"3"`},
		body: map[string]any{"repos": []string{}}}).fails(http.StatusPreconditionFailed, "edit_clash")

	// Widen it: everywhere.
	var widened struct {
		Outcome  string    `json:"outcome"`
		Memory   scoped    `json:"memory"`
		Receipts []receipt `json:"receipts"`
	}
	e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"repos": []string{}, "reason": "I use the staging database everywhere."}}).ok(http.StatusOK, &widened)
	if widened.Outcome != "applied" || len(widened.Receipts) != 1 || widened.Receipts[0].Action != "scoped" ||
		widened.Memory.Scope.Repos != nil || widened.Memory.Version != 1 {
		t.Fatalf("widen: %+v", widened)
	}
	var raw struct {
		Memory struct {
			Scope json.RawMessage `json:"scope"`
		} `json:"memory"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + ref + "?space=" + personal.slug, token: tok}).ok(http.StatusOK, &raw)
	if string(raw.Memory.Scope) != "{}" {
		t.Errorf("scope = %s, want {}", raw.Memory.Scope)
	}
	// The same again is nothing to change; a retry of the same request
	// replays.
	e.do(call{method: "POST", path: path, token: tok, body: map[string]any{"repos": []string{}}}).
		fails(http.StatusBadRequest, "invalid_request")
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'scoped'`, personal.id); n != 1 {
		t.Errorf("%d scoped receipts, want 1", n)
	}

	// The person undoes it by its receipt: it applies in acme/web again.
	var undone struct {
		Receipts []receipt `json:"receipts"`
	}
	e.do(call{method: "POST", path: "/v2/receipts/" + widened.Receipts[0].ID.String() + ":undo", token: tok}).ok(http.StatusOK, &undone)
	if len(undone.Receipts) != 1 || undone.Receipts[0].Action != "undid" {
		t.Errorf("undo: %+v", undone.Receipts)
	}
	e.do(call{method: "GET", path: "/v2/memories/" + ref + "?space=" + personal.slug, token: tok}).ok(http.StatusOK, &got)
	if !slices.Equal(got.Memory.Scope.Repos, []string{"acme/web"}) {
		t.Errorf("after undo, scope.repos = %v, want [acme/web]", got.Memory.Scope.Repos)
	}
	e.do(call{method: "POST", path: "/v2/receipts/" + widened.Receipts[0].ID.String() + ":undo", token: tok}).
		fails(http.StatusConflict, "undo_refused")

	// A forgotten memory can't change.
	e.do(call{method: "POST", path: "/v2/memories/" + ref + ":forget?space=" + personal.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"carries": []string{}}}).ok(http.StatusOK, nil)
	e.do(call{method: "POST", path: path, token: tok, body: map[string]any{"repos": []string{"acme/web"}}}).
		fails(http.StatusConflict, "invalid_transition")
}
