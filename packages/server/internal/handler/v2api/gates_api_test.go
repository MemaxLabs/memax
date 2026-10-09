package v2api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The gate wire shapes the tests read back.
type gate struct {
	ID       uuid.UUID `json:"id"`
	Ref      string    `json:"ref"`
	SpaceID  uuid.UUID `json:"space_id"`
	Question string    `json:"question"`
	Context  string    `json:"context"`
	Options  []struct {
		Label  string `json:"label"`
		Detail string `json:"detail"`
	} `json:"options"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	AskedBy   uuid.UUID `json:"asked_by"`
	Agent     string    `json:"agent"`
	NeedsWeb  bool      `json:"needs_web"`
	Version   int       `json:"version"`
	Answer    *struct {
		Option int    `json:"option"`
		Label  string `json:"label"`
		Memory struct {
			ID  uuid.UUID `json:"id"`
			Ref string    `json:"ref"`
		} `json:"memory"`
		AnsweredBy uuid.UUID `json:"answered_by"`
		Assurance  string    `json:"assurance"`
	} `json:"answer"`
	Withdrawn *struct {
		ByKind string    `json:"by_kind"`
		By     uuid.UUID `json:"by"`
	} `json:"withdrawn"`
}

type gateResult struct {
	Outcome  string    `json:"outcome"`
	Gate     gate      `json:"gate"`
	Memory   *memory   `json:"memory"`
	Receipts []receipt `json:"receipts"`
}

func gatesPath(sp space) string { return "/v2/spaces/" + sp.id.String() + "/gates" }

func gatePath(sp space, g gate, command string) string {
	return "/v2/gates/" + g.Ref + command + "?space=" + sp.slug
}

func deployGate() map[string]any {
	return map[string]any{
		"question": "Which deploy target should the v2 API use?",
		"context":  "A kept note and a proposal disagree: M-0174 (Railway) and M-0431 (Fly.io).",
		"options": []map[string]any{
			{"label": "Fly.io, iad and ams", "detail": "Matches the current API and workers."},
			{"label": "Railway"},
			{"label": "Decide later"},
		},
		"session_ref": "cx-9f1c",
	}
}

// gateErrorStatus reads details.status of a 409 on a gate.
func gateErrorStatus(t *testing.T, r *resp) string {
	t.Helper()
	r.fails(http.StatusConflict, "invalid_transition")
	var env struct {
		Error struct {
			Details struct {
				Ref    string `json:"ref"`
				Status string `json:"status"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Details.Ref == "" {
		t.Errorf("no details.ref: %s", r.body)
	}
	return env.Error.Details.Status
}

// An agent asks over /v2, a person lists, reads and answers it from the
// CLI, and the answer is the person's kept decision; a second answer is a
// clean 409.
func TestGatesAPI(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	codex := e.connection(keyID)
	zzCLI := e.cliSession(zz)

	var asked gateResult
	r := e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: deployGate(), header: map[string]string{"X-Memax-Via": "mcp"}}).
		ok(http.StatusCreated, &asked)
	g := asked.Gate
	if asked.Outcome != "applied" || g.Ref != "G-0001" || g.Status != "waiting" || g.AskedBy != codex || g.Agent != "codex" ||
		g.Version != 1 || len(g.Options) != 3 || g.Options[0].Detail == "" || g.NeedsWeb || asked.Memory != nil {
		t.Fatalf("asked = %+v", asked)
	}
	if loc := r.header.Get("Location"); loc != "/v2/gates/"+g.ID.String() {
		t.Errorf("Location = %q", loc)
	}
	if et := r.header.Get("ETag"); et != `"1"` {
		t.Errorf("ETag = %q", et)
	}
	if rc := asked.Receipts; len(rc) != 1 || rc[0].Action != "asked" || rc[0].ObjectKind != "gate" || *rc[0].ActorID != codex || rc[0].Via != "mcp" {
		t.Errorf("receipts = %+v", rc)
	}

	var waiting page[gate]
	e.do(call{method: "GET", path: gatesPath(sp) + "?status=waiting", token: zzCLI}).ok(http.StatusOK, &waiting)
	if len(waiting.Items) != 1 || waiting.Items[0].ID != g.ID || waiting.HasMore {
		t.Errorf("waiting = %+v", waiting)
	}
	var got gate
	if r := e.do(call{method: "GET", path: gatePath(sp, g, ""), token: zzCLI}).ok(http.StatusOK, &got); r.header.Get("ETag") != `"1"` {
		t.Errorf("ETag = %q", r.header.Get("ETag"))
	}
	if got.Question != "Which deploy target should the v2 API use?" || got.Context == "" {
		t.Errorf("got = %+v", got)
	}

	key := uuid.NewString()
	answer := call{method: "POST", path: gatePath(sp, g, ":answer"), token: zzCLI, body: map[string]any{"option": 1, "reason": "The workers are there."},
		header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key, "X-Memax-Via": "cli"}}
	var answered gateResult
	r = e.do(answer).ok(http.StatusOK, &answered)
	a := answered.Gate.Answer
	if answered.Outcome != "applied" || answered.Gate.Status != "answered" || answered.Gate.Version != 2 || a == nil ||
		a.Option != 1 || a.Label != "Fly.io, iad and ams" || a.AnsweredBy != zz || a.Assurance != "client_attested" {
		t.Fatalf("answered = %+v", answered)
	}
	m := answered.Memory
	if m == nil || m.Statement != "Which deploy target should the v2 API use? Fly.io, iad and ams" || m.State != "kept" ||
		m.Section != "decisions" || a.Memory.ID != m.ID || a.Memory.Ref != m.Ref || m.Trust != "agent_own_work" {
		t.Fatalf("decision = %+v", m)
	}
	if rc := answered.Receipts; len(rc) != 2 || rc[0].Action != "kept" || rc[0].ObjectID != m.ID || rc[1].Action != "answered" ||
		rc[1].ObjectID != g.ID || rc[1].Via != "cli" || rc[1].Assurance != "client_attested" || *rc[1].ActorID != zz {
		t.Errorf("receipts = %+v", rc)
	}
	if r.header.Get("ETag") != `"2"` {
		t.Errorf("ETag after the answer = %q", r.header.Get("ETag"))
	}
	// The same key replays; a new answer is a 409 that says why.
	var replay gateResult
	if r := e.do(answer).ok(http.StatusOK, &replay); r.header.Get("Idempotent-Replayed") != "true" || replay.Memory == nil || replay.Memory.ID != m.ID {
		t.Errorf("replay = %+v", replay)
	}
	if s := gateErrorStatus(t, e.do(call{method: "POST", path: gatePath(sp, g, ":answer"), token: zzCLI, body: map[string]any{"option": 2}})); s != "answered" {
		t.Errorf("details.status = %q", s)
	}
	if s := gateErrorStatus(t, e.do(call{method: "POST", path: gatePath(sp, g, ":withdraw"), token: keyTok})); s != "answered" {
		t.Errorf("withdrawing an answered gate: details.status = %q", s)
	}
	var answeredList page[gate]
	e.do(call{method: "GET", path: gatesPath(sp) + "?status=answered&status=withdrawn", token: zzCLI}).ok(http.StatusOK, &answeredList)
	if len(answeredList.Items) != 1 || answeredList.Items[0].Answer == nil {
		t.Errorf("answered = %+v", answeredList)
	}
	// The decision is a kept memory of the space, with the gate as its source.
	var detail struct {
		Memory memory `json:"memory"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + m.ID.String(), token: zzCLI}).ok(http.StatusOK, &detail)
	if src := detail.Memory.Sources; len(src) != 1 || src[0].Ref != "G-0001" || src[0].Kind != "session" || src[0].Trust != "agent_own_work" {
		t.Errorf("sources = %+v", src)
	}
}

// The agent withdraws its own question; a person can withdraw one too;
// an answer to a withdrawn gate is a 409.
func TestWithdrawGateAPI(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	otherTok, _ := e.apiKey(zz, keyOpts{agent: "cursor"})
	codex := e.connection(keyID)

	var first, second gateResult
	e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: deployGate()}).ok(http.StatusCreated, &first)
	e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: deployGate()}).ok(http.StatusCreated, &second)

	if code := policyCode(t, e.do(call{method: "POST", path: gatePath(sp, first.Gate, ":withdraw"), token: otherTok})); code != "not_your_gate" {
		t.Errorf("another agent withdrawing: %s", code)
	}
	var w gateResult
	e.do(call{method: "POST", path: gatePath(sp, first.Gate, ":withdraw"), token: keyTok, body: map[string]any{"reason": "Found it in the code."},
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, &w)
	if w.Gate.Status != "withdrawn" || w.Gate.Withdrawn == nil || w.Gate.Withdrawn.ByKind != "agent" || w.Gate.Withdrawn.By != codex ||
		len(w.Receipts) != 1 || w.Receipts[0].Action != "withdrawn" || w.Receipts[0].Reason != "Found it in the code." {
		t.Errorf("withdrawn = %+v", w)
	}
	if s := gateErrorStatus(t, e.do(call{method: "POST", path: gatePath(sp, first.Gate, ":answer"), token: e.cliSession(zz), body: map[string]any{"option": 1}})); s != "withdrawn" {
		t.Errorf("answering a withdrawn gate: details.status = %q", s)
	}
	// A stale If-Match is an edit clash; the person then withdraws by id.
	e.do(call{method: "POST", path: gatePath(sp, second.Gate, ":withdraw"), token: e.cliSession(zz), header: map[string]string{"If-Match": `"2"`}}).
		fails(http.StatusPreconditionFailed, "edit_clash")
	e.do(call{method: "POST", path: "/v2/gates/" + second.Gate.ID.String() + ":withdraw", token: e.cliSession(zz)}).ok(http.StatusOK, &w)
	if w.Gate.Withdrawn == nil || w.Gate.Withdrawn.ByKind != "person" || w.Gate.Withdrawn.By != zz {
		t.Errorf("a person's withdrawal = %+v", w.Gate.Withdrawn)
	}
}

// Who may ask and answer over /v2, what a request may hold, and D15 held
// by assurance: in a team space only a signed web request answers.
func TestGatesAPIRefusals(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy, viewer, mallory := e.user("zz"), e.user("jy"), e.user("viewer"), e.user("mallory")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(zz, policy.SpaceTeam, "acme")
	e.join(team, jy, "contributor")
	e.join(sp, viewer, "viewer")
	theirs := e.space(mallory, policy.SpaceProject, "theirs")
	keyTok, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	readTok, _ := e.apiKey(zz, keyOpts{agent: "cursor", perms: []string{"memory:read", "hub:read"}})

	// People don't ask; a read-only key only reads.
	if code := policyCode(t, e.do(call{method: "POST", path: gatesPath(sp), token: e.cliSession(zz), body: deployGate()})); code != "gate_by_agent" {
		t.Errorf("a person asking: %s", code)
	}
	if code := policyCode(t, e.do(call{method: "POST", path: gatesPath(sp), token: readTok, body: deployGate()})); code != "key_read_only" {
		t.Errorf("a read-only key asking: %s", code)
	}
	// Malformed questions are 400 with the field.
	one := deployGate()
	one["options"] = []map[string]any{{"label": "Only one"}}
	e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: one, invalid: true}).fails(http.StatusBadRequest, "invalid_request")
	dup := deployGate()
	dup["options"] = []map[string]any{{"label": "Fly.io"}, {"label": "fly.io"}}
	if f := e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: dup}).fails(http.StatusBadRequest, "invalid_request"); f.Details.Field != "options.label" {
		t.Errorf("duplicate options: field %q", f.Details.Field)
	}
	soon := deployGate()
	soon["expires_at"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	if f := e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: soon}).fails(http.StatusBadRequest, "invalid_request"); f.Details.Field != "expires_at" {
		t.Errorf("expiring in a minute: field %q", f.Details.Field)
	}
	// Another tenant's space is not found.
	e.do(call{method: "POST", path: gatesPath(theirs), token: keyTok, body: deployGate()}).fails(http.StatusNotFound, "not_found")

	var asked, teamGate gateResult
	e.do(call{method: "POST", path: gatesPath(sp), token: keyTok, body: deployGate()}).ok(http.StatusCreated, &asked)
	e.do(call{method: "POST", path: gatesPath(team), token: keyTok, body: deployGate()}).ok(http.StatusCreated, &teamGate)
	if !teamGate.Gate.NeedsWeb || asked.Gate.NeedsWeb {
		t.Errorf("needs_web: team %v, project %v", teamGate.Gate.NeedsWeb, asked.Gate.NeedsWeb)
	}
	// Agents and viewers don't answer; options are counted from 1.
	if code := policyCode(t, e.do(call{method: "POST", path: gatePath(sp, asked.Gate, ":answer"), token: keyTok, body: map[string]any{"option": 1}})); code != "person_must_answer" {
		t.Errorf("an agent answering: %s", code)
	}
	if code := policyCode(t, e.do(call{method: "POST", path: gatePath(sp, asked.Gate, ":answer"), token: e.cliSession(viewer), body: map[string]any{"option": 1}})); code != "viewer" {
		t.Errorf("a viewer answering: %s", code)
	}
	if f := e.do(call{method: "POST", path: gatePath(sp, asked.Gate, ":answer"), token: e.cliSession(zz), body: map[string]any{"option": 4}}).
		fails(http.StatusBadRequest, "invalid_request"); f.Details.Field != "option" {
		t.Errorf("option 4 of 3: field %q", f.Details.Field)
	}
	// A display ID needs its space; a memory ID isn't a gate; another
	// tenant's gate is not found.
	e.do(call{method: "GET", path: "/v2/gates/" + asked.Gate.Ref, token: e.cliSession(zz)}).fails(http.StatusBadRequest, "space_required")
	e.do(call{method: "GET", path: "/v2/gates/M-0001?space=" + sp.slug, token: e.cliSession(zz), invalid: true}).fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "GET", path: "/v2/gates/" + asked.Gate.ID.String(), token: e.session(mallory)}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: "/v2/gates/" + asked.Gate.ID.String() + ":answer", token: e.cliSession(mallory), body: map[string]any{"option": 1}}).
		fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: gatesPath(sp) + "?status=open", token: e.cliSession(zz), invalid: true}).fails(http.StatusBadRequest, "invalid_request")

	// D15: in the team space the CLI is refused and the gate keeps waiting;
	// a signed web request answers, human_web.
	if code := policyCode(t, e.do(call{method: "POST", path: gatePath(team, teamGate.Gate, ":answer"), token: e.cliSession(jy), body: map[string]any{"option": 2}})); code != "decision_needs_web" {
		t.Errorf("a CLI answer in a team space: %s", code)
	}
	var still gate
	e.do(call{method: "GET", path: gatePath(team, teamGate.Gate, ""), token: e.cliSession(jy)}).ok(http.StatusOK, &still)
	if still.Status != "waiting" {
		t.Errorf("after the refused answer: %s", still.Status)
	}
	var web gateResult
	e.do(call{method: "POST", path: gatePath(team, teamGate.Gate, ":answer"), token: e.webSession(jy), sign: webSigned(jy, tamper{}),
		body: map[string]any{"option": 2}}).ok(http.StatusOK, &web)
	if web.Gate.Answer == nil || web.Gate.Answer.Assurance != "human_web" || web.Receipts[1].Via != "web" ||
		!strings.HasSuffix(web.Memory.Statement, "Railway") {
		t.Errorf("web answer = %+v", web)
	}
}
