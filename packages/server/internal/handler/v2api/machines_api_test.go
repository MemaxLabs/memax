package v2api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Machines over /v2 (personal-layer design §5.6): link, list, poll a
// machine's targets, rename, unlink, add a home file, and who may.

type wireMachine struct {
	ID         uuid.UUID `json:"id"`
	Label      string    `json:"label"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform"`
	DeviceID   uuid.UUID `json:"device_id"`
	Linked     bool      `json:"linked"`
	Version    int       `json:"version"`
	LastSeenAt *string   `json:"last_seen_at"`
}

type wireTarget struct {
	ID      uuid.UUID `json:"id"`
	Kind    string    `json:"kind"`
	Root    string    `json:"root"`
	Path    string    `json:"path"`
	Label   string    `json:"label"`
	State   string    `json:"sync_state"`
	Machine *struct {
		ID    uuid.UUID `json:"id"`
		Label string    `json:"label"`
		Name  string    `json:"name"`
	} `json:"machine"`
	Settings struct {
		SizeBudget int      `json:"size_budget"`
		UserOwned  bool     `json:"user_owned"`
		ReadBy     []string `json:"read_by"`
	} `json:"settings"`
}

type wireMachineResult struct {
	Outcome  string       `json:"outcome"`
	Machine  wireMachine  `json:"machine"`
	Targets  []wireTarget `json:"targets"`
	Receipts []receipt    `json:"receipts"`
}

func linkBody(device uuid.UUID, name string, kinds ...string) map[string]any {
	files := []map[string]any{}
	for _, k := range kinds {
		files = append(files, map[string]any{"kind": k})
	}
	return map[string]any{"device_id": device, "name": name, "platform": "darwin", "files": files}
}

func TestMachinesOverV2(t *testing.T) {
	e := newEnv(t)
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	e.remember(tok, personal, "Answer in British English.")
	device := uuid.New()
	base := "/v2/spaces/" + personal.slug

	// Link: the machine, its first Brief and three home files, 201.
	var linked wireMachineResult
	r := e.do(call{method: "POST", path: base + "/machines", token: tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: linkBody(device, "work-mbp", "claude_user", "codex_user", "agents_md")}).ok(http.StatusCreated, &linked)
	if r.header.Get("ETag") != `"1"` || linked.Machine.Label != "machine-1" || !linked.Machine.Linked ||
		linked.Machine.DeviceID != device || len(linked.Targets) != 3 {
		t.Fatalf("link = %+v (ETag %s)", linked, r.header.Get("ETag"))
	}
	for _, tg := range linked.Targets {
		if tg.Root != "home" || tg.Machine == nil || tg.Machine.Label != "machine-1" || tg.State != "compiling" ||
			!strings.HasPrefix(tg.Label, "~/") || tg.Settings.SizeBudget != 6144 {
			t.Errorf("target %+v", tg)
		}
	}
	if len(linked.Receipts) != 5 || linked.Receipts[0].ObjectKind != "machine" || linked.Receipts[0].Action != "linked" ||
		linked.Receipts[0].Via != "cli" {
		t.Errorf("receipts = %+v", linked.Receipts)
	}

	// The same again writes nothing: 200, no receipts.
	var again wireMachineResult
	e.do(call{method: "POST", path: base + "/machines", token: tok,
		body: linkBody(device, "work-mbp", "claude_user", "codex_user", "agents_md")}).ok(http.StatusOK, &again)
	if len(again.Receipts) != 0 || again.Machine.ID != linked.Machine.ID {
		t.Errorf("relink = %+v", again)
	}

	// The list, and the machine's own targets (what its daemon polls).
	var machines struct {
		Items []wireMachine `json:"items"`
	}
	e.do(call{method: "GET", path: base + "/machines", token: tok}).ok(http.StatusOK, &machines)
	if len(machines.Items) != 1 || machines.Items[0].Name != "work-mbp" {
		t.Errorf("machines = %+v", machines.Items)
	}
	var polled struct {
		Items []wireTarget `json:"items"`
	}
	e.do(call{method: "GET", path: base + "/targets?machine=" + linked.Machine.ID.String(), token: tok}).ok(http.StatusOK, &polled)
	if len(polled.Items) != 3 {
		t.Errorf("the machine's targets = %d", len(polled.Items))
	}
	e.do(call{method: "GET", path: base + "/machines", token: tok}).ok(http.StatusOK, &machines)
	if machines.Items[0].LastSeenAt == nil {
		t.Error("polling its targets didn't note the machine as seen")
	}
	var all struct {
		Items []wireTarget `json:"items"`
	}
	e.do(call{method: "GET", path: base + "/targets", token: tok}).ok(http.StatusOK, &all)
	if len(all.Items) != 3 {
		t.Errorf("the space's targets = %d", len(all.Items))
	}

	// Rename, with If-Match.
	var renamed wireMachineResult
	e.do(call{method: "PATCH", path: "/v2/machines/" + linked.Machine.ID.String(), token: tok,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"name": "Work laptop"}}).ok(http.StatusOK, &renamed)
	if renamed.Machine.Name != "Work laptop" || renamed.Machine.Version != 2 || renamed.Receipts[0].ObjectRef != "machine-1" {
		t.Errorf("rename = %+v", renamed)
	}
	e.do(call{method: "PATCH", path: "/v2/machines/" + linked.Machine.ID.String(), token: tok,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"name": "stale"}}).fails(http.StatusPreconditionFailed, "edit_clash")

	// Unlink: every file stopped.
	var unlinked wireMachineResult
	e.do(call{method: "POST", path: "/v2/machines/" + linked.Machine.ID.String() + ":unlink", token: tok}).ok(http.StatusOK, &unlinked)
	if unlinked.Machine.Linked || len(unlinked.Receipts) != 4 {
		t.Errorf("unlink = %+v", unlinked)
	}
	for _, tg := range unlinked.Targets {
		if tg.State != "off" {
			t.Errorf("%s after unlink: %s", tg.Path, tg.State)
		}
	}
	// A home file can't be added to an unlinked machine: 409.
	e.do(call{method: "POST", path: base + "/targets", token: tok,
		body: map[string]any{"kind": "codex_user", "root": "home", "machine": linked.Machine.ID, "path": ".codex/AGENTS.override.md"}}).
		fails(http.StatusConflict, "invalid_transition")

	// Linked again with one file, then Codex's added through createTarget.
	var back wireMachineResult
	e.do(call{method: "POST", path: base + "/machines", token: tok,
		body: linkBody(device, "Work laptop", "claude_user")}).ok(http.StatusCreated, &back)
	var added struct {
		Target wireTarget `json:"target"`
	}
	e.do(call{method: "POST", path: base + "/targets", token: tok,
		body: map[string]any{"kind": "codex_user", "root": "home", "machine": back.Machine.ID, "path": ".codex/AGENTS.override.md"}}).
		ok(http.StatusCreated, &added)
	if added.Target.Root != "home" || !added.Target.Settings.UserOwned || added.Target.Label != "~/.codex/AGENTS.override.md" {
		t.Errorf("added = %+v", added.Target)
	}
}

func TestMachinesAreThePersonsAlone(t *testing.T) {
	e := newEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	theirs := e.space(jy, policy.SpacePersonal, "JY")
	device := uuid.New()
	var linked wireMachineResult
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(device, "work-mbp", "codex_user")}).ok(http.StatusCreated, &linked)

	// Agents and API keys never link a machine or read its files.
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	refused := e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: key,
		body: linkBody(uuid.New(), "x", "codex_user")}).fails(http.StatusForbidden, "refused")
	if refused.Details.Policy.Code != policy.CodeMachinesByPerson {
		t.Errorf("an agent's link: %s", refused.Details.Policy.Code)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/machines", token: key}).fails(http.StatusForbidden, "refused")
	e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/targets?machine=" + linked.Machine.ID.String(), token: key}).
		fails(http.StatusForbidden, "refused")
	rep := e.do(call{method: "POST", path: "/v2/targets/" + linked.Targets[0].ID.String() + "/deliveries", token: key,
		body: map[string]any{"compile": "C-0001", "sha256": strings.Repeat("a", 64)}}).fails(http.StatusForbidden, "refused")
	if rep.Details.Policy.Code != policy.CodeMachinesByPerson {
		t.Errorf("an agent's delivery report: %s", rep.Details.Policy.Code)
	}

	// Only a personal space is written to machines.
	pr := e.do(call{method: "POST", path: "/v2/spaces/" + project.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "x", "codex_user")}).fails(http.StatusForbidden, "refused")
	if pr.Details.Policy.Code != policy.CodeMachinesPersonal {
		t.Errorf("a project space: %s", pr.Details.Policy.Code)
	}

	// Another person: not found, even claiming my device id; their own
	// link with it is theirs.
	other := e.session(jy)
	e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/machines", token: other}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: "/v2/spaces/" + theirs.slug + "/targets?machine=" + linked.Machine.ID.String(), token: other}).
		fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: "/v2/machines/" + linked.Machine.ID.String() + ":unlink", token: other}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "PATCH", path: "/v2/machines/" + linked.Machine.ID.String(), token: other,
		body: map[string]any{"name": "mine"}}).fails(http.StatusNotFound, "not_found")
	var mine wireMachineResult
	e.do(call{method: "POST", path: "/v2/spaces/" + theirs.slug + "/machines", token: other,
		body: linkBody(device, "same device", "codex_user")}).ok(http.StatusCreated, &mine)
	if mine.Machine.ID == linked.Machine.ID {
		t.Error("my device id gave another person my machine")
	}

	// Malformed: a path outside a dot folder, a repository kind, no files.
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok, invalid: true,
		body: map[string]any{"device_id": uuid.New(), "name": "x", "platform": "darwin",
			"files": []map[string]any{{"kind": "codex_user", "path": "code/AGENTS.md"}}}}).fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "x", "claude_md")}).fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/targets?machine=nope", token: tok, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")
}

// healthyFake is the fake compiler, saying it has every adapter but the
// listed ones.
type healthyFake struct {
	*compiletest.Fake
	lacks []string
}

func (f healthyFake) Health(context.Context) (*compile.Health, error) {
	h := &compile.Health{Status: "ok", ContractVersion: compile.ContractVersion}
	for _, k := range []string{"agents_md", "claude_md", "cursor_mdc", "chatgpt", "gemini_md", "copilot", "windsurf", "claude_rules", "claude_user", "codex_user"} {
		lacking := false
		for _, l := range f.lacks {
			lacking = lacking || l == k
		}
		if !lacking {
			h.Adapters = append(h.Adapters, compile.Adapter{Kind: k})
		}
	}
	return h, nil
}

// A kind the compile service doesn't list is refused before anything is
// written: a compile of it could only fail.
func TestAKindTheCompileServiceLacksIsRefused(t *testing.T) {
	e := newEnvWith(t, func(e *env) []v2api.Option {
		svc := compile.New(e.ledger, healthyFake{Fake: &compiletest.Fake{}, lacks: []string{"codex_user"}}, e.store, compile.Config{Log: quiet})
		return []v2api.Option{v2api.WithCompile(svc)}
	})
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	err := e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "work-mbp", "claude_user", "codex_user")}).fails(http.StatusBadRequest, "invalid_request")
	if !strings.Contains(err.Message, "codex_user adapter") {
		t.Errorf("message = %q", err.Message)
	}
	if n := e.count(`SELECT count(*) FROM v2.machines`); n != 0 {
		t.Errorf("%d machines written", n)
	}
	// What it has goes through.
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "work-mbp", "claude_user")}).ok(http.StatusCreated, nil)
}
