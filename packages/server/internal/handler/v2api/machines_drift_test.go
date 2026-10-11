package v2api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 6 per machine (design §3.1, §6.4): each linked machine has its own
// targets, so a hand edit of Codex's block on one machine drifts only that
// machine's file; the other keeps updating. What the daemon reports for a
// block is the block alone.
func TestAHandEditHoldsOnlyItsMachine(t *testing.T) {
	// The real compiler: a block, as the daemon writes it.
	e := newEnvWith(t, func(e *env) []v2api.Option {
		e.svc = compile.New(e.ledger, compiletest.Client(compiletest.StartService(t)), e.store, compile.Config{Log: quiet})
		return []v2api.Option{v2api.WithCompile(e.svc)}
	})
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	e.remember(tok, personal, "Answer in British English.")
	codexOf := map[string]uuid.UUID{}
	for _, name := range []string{"work-mbp", "home-linux"} {
		var res wireMachineResult
		e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
			body: linkBody(uuid.New(), name, "codex_user")}).ok(http.StatusCreated, &res)
		codexOf[name] = res.Targets[0].ID
	}
	e.compileAll()

	// Both machines' daemons write the block and acknowledge it.
	blocks := map[string]string{}
	for name, id := range codexOf {
		var pv preview
		e.do(call{method: "GET", path: targetPath(id, "/preview"), token: tok}).ok(http.StatusOK, &pv)
		if len(pv.Files) != 1 || !strings.HasPrefix(pv.Files[0].Content, "<!-- memax:start -->") {
			t.Fatalf("%s's Codex file = %+v", name, pv.Files)
		}
		blocks[name] = pv.Files[0].Content
		e.do(call{method: "POST", path: targetPath(id, "/deliveries"), token: tok,
			body: map[string]any{"compile": pv.Compile.Ref, "sha256": pv.Compile.DriftSHA256}}).ok(http.StatusOK, nil)
	}

	// A hand edit in work-mbp's block.
	edited := strings.Replace(blocks["work-mbp"], "<!-- memax:end -->", "- Prefer tabs.\n<!-- memax:end -->", 1)
	var obs observationResult
	e.do(call{method: "POST", path: targetPath(codexOf["work-mbp"], "/observations"), token: tok,
		body: map[string]any{"path": ".codex/AGENTS.md", "content": edited, "device_id": "work-mbp"}}).ok(http.StatusCreated, &obs)
	if !obs.Drifted || obs.Target.SyncState != "drifted" {
		t.Fatalf("work-mbp's hand edit = %+v", obs)
	}

	// A Keep: home-linux's file compiles and waits for its daemon;
	// work-mbp's stays as its person left it until they settle it.
	e.remember(tok, personal, "Prefer small commits.")
	e.compileAll()
	state := func(name string) string {
		var tr target
		var list struct {
			Items []target `json:"items"`
		}
		e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/targets", token: tok}).ok(http.StatusOK, &list)
		for _, it := range list.Items {
			if it.ID == codexOf[name] {
				tr = it
			}
		}
		return tr.SyncState
	}
	if got := state("work-mbp"); got != "drifted" {
		t.Errorf("work-mbp after the Keep: %s, want drifted", got)
	}
	if got := state("home-linux"); got != "pending_delivery" {
		t.Errorf("home-linux after the Keep: %s, want pending_delivery", got)
	}
}
