package v2api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 14 for the personal layer on the real stack (design §5.5): a
// Personal space's export says each home file's root and its machine in
// full (label, name, platform, linked and unlinked), and its receipts
// carry the machines' linked and unlinked receipts, with no machine's name
// in any receipt.
func TestExportOfAPersonalSpaceHasItsMachines(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	var work, home wireMachineResult
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "work-mbp", "claude_user")}).ok(http.StatusCreated, &work)
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
		body: linkBody(uuid.New(), "home-linux", "codex_user")}).ok(http.StatusCreated, &home)
	e.do(call{method: "POST", path: "/v2/machines/" + home.Machine.ID.String() + ":unlink", token: tok}).ok(http.StatusOK, nil)

	_, files, _ := readExport(t, e.exportSpace(tok, personal, "export-personal"))
	var targets struct {
		Targets []struct {
			Root    string `json:"root"`
			Label   string `json:"label"`
			Machine *struct {
				Label      string  `json:"label"`
				Name       string  `json:"name"`
				Platform   string  `json:"platform"`
				LinkedAt   string  `json:"linked_at"`
				UnlinkedAt *string `json:"unlinked_at"`
			} `json:"machine"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(files[export.TargetsFile], &targets); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tg := range targets.Targets {
		if tg.Root != "home" || tg.Machine == nil || tg.Machine.Platform != "darwin" || tg.Machine.LinkedAt == "" {
			t.Errorf("a home target's export: %+v %+v", tg, tg.Machine)
			continue
		}
		unlinked := "linked"
		if tg.Machine.UnlinkedAt != nil {
			unlinked = "unlinked"
		}
		got[tg.Label] = tg.Machine.Label + " " + tg.Machine.Name + " " + unlinked
	}
	if got["~/.claude/rules/memax-personal.md"] != "machine-1 work-mbp linked" ||
		got["~/.codex/AGENTS.md"] != "machine-2 home-linux unlinked" {
		t.Errorf("targets: %v", got)
	}
	receipts := string(files[export.ReceiptsFile])
	if strings.Count(receipts, `"action":"linked"`) != 2 || strings.Count(receipts, `"action":"unlinked"`) != 1 {
		t.Errorf("receipts.jsonl hasn't the machines' receipts:\n%s", receipts)
	}
	if strings.Contains(receipts, "work-mbp") || strings.Contains(receipts, "home-linux") {
		t.Error("a machine's name is in a receipt")
	}
}
