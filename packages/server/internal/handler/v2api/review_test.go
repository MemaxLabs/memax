package v2api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// contradicting is a model that finds every decision in force contradicted
// and everything else unrelated.
type contradicting struct{}

var candidateTag = regexp.MustCompile(`<candidate id="(M-\d+)"[^>]* in_force="(true|false)"`)

func (contradicting) Complete(_ context.Context, c judge.Call) (string, error) {
	type pair struct {
		Candidate       string  `json:"candidate"`
		Relation        string  `json:"relation"`
		Confidence      float64 `json:"confidence"`
		ExplicitChange  bool    `json:"explicit_change"`
		Rationale       string  `json:"rationale"`
		MergedStatement string  `json:"merged_statement"`
		Question        string  `json:"question"`
		Labels          struct {
			Proposal string `json:"proposal"`
			Decision string `json:"decision"`
			Both     string `json:"both"`
			Open     string `json:"open"`
		} `json:"labels"`
		Suggested string `json:"suggested"`
	}
	out := struct {
		Pairs      []pair `json:"pairs"`
		Conditions []any  `json:"conditions"`
	}{Pairs: []pair{}, Conditions: []any{}}
	for _, m := range candidateTag.FindAllStringSubmatch(c.Prompt, -1) {
		p := pair{Candidate: m[1], Relation: "unrelated", Confidence: 0.9, Rationale: "Different subject.", Suggested: "none"}
		if m[2] == "true" {
			p.Relation, p.Confidence, p.Rationale = "contradicts", 0.93, "It picks another host than "+m[1]+"."
			p.Question, p.Suggested = "Which host holds for the v2 API?", "both"
			p.Labels.Proposal, p.Labels.Decision = "The new host", "The host as kept"
			p.Labels.Both, p.Labels.Open = "Both, each with its own scope", "Leave it undecided"
		}
		out.Pairs = append(out.Pairs, p)
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// judgeAll runs the judge on every judge job waiting, as the worker would.
func (e *env) judgeAll(j *judge.Judge) {
	e.t.Helper()
	ctx := context.Background()
	rows, err := e.pool.Query(ctx, `SELECT args FROM river_job WHERE kind = 'judge_proposal' AND state = 'available' ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	var jobs []ledger.JudgeArgs
	for rows.Next() {
		var raw []byte
		var a ledger.JudgeArgs
		if err := rows.Scan(&raw); err != nil {
			e.t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			e.t.Fatal(err)
		}
		jobs = append(jobs, a)
	}
	rows.Close()
	for _, a := range jobs {
		if _, err := j.Run(ctx, a, judge.RunOptions{}); err != nil {
			e.t.Fatalf("judge %s: %v", a.MemoryID, err)
		}
	}
	e.exec(`UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'judge_proposal' AND state = 'available'`)
}

// The judged memory, as /v2 returns it.
type judged struct {
	memory
	Links []struct {
		Kind      string    `json:"kind"`
		Direction string    `json:"direction"`
		MemoryID  uuid.UUID `json:"memory_id"`
		Ref       string    `json:"ref"`
	} `json:"links"`
	Judge *struct {
		State   string `json:"state"`
		Verdict string `json:"verdict"`
		Outcome string `json:"outcome"`
		Related struct {
			Ref string `json:"ref"`
		} `json:"related"`
	} `json:"judge"`
}

type changes struct {
	Outcome  string    `json:"outcome"`
	Memory   judged    `json:"memory"`
	Memories []judged  `json:"memories"`
	Receipts []receipt `json:"receipts"`
}

func decisionBody(statement, area string) map[string]any {
	return map[string]any{"statement": statement, "section": "decisions", "kind": "decision",
		"decision": map[string]any{"area": area}}
}

// Rule 11 end to end, over /v2: an agent's proposal that contradicts a
// decision in force is flagged before anyone keeps it, Review returns it
// with the conflict, Keep is refused until a person settles it, the
// comparison shows both sides and the four answers, and the settlement
// is one Undo away.
func TestConflictFlaggedSettledAndUndone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	owner := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	j := judge.New(e.ledger, contradicting{}, judge.Config{Primary: judge.Tier{Model: "fake"}, Log: quiet})

	var railway, fly result
	e.do(call{method: "POST", path: memoriesPath(sp), token: owner,
		body: decisionBody("Deploy the v2 API to Railway for its preview environments.", "deploy target")}).ok(201, &railway)
	e.do(call{method: "POST", path: memoriesPath(sp), token: key,
		body: decisionBody("Deploy the v2 API to Fly.io in iad and ams.", "deploy target")}).ok(201, &fly)
	if fly.Outcome != "proposed" {
		t.Fatalf("agent's proposal = %s", fly.Outcome)
	}
	// Before the judge runs, Review shows it as working.
	var before page[judged]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/review", token: owner}).ok(200, &before)
	if len(before.Items) != 1 || before.Items[0].Judge == nil || before.Items[0].Judge.State != "working" {
		t.Fatalf("review before = %+v", before.Items)
	}

	// It touches a decision in force, so a Keep before the judge is busy.
	if r := e.do(call{method: "POST", path: memoryPath(fly.Memory, ":keep"), token: owner}); r.header.Get("Retry-After") == "" {
		t.Errorf("no Retry-After: %v", r.header)
	} else {
		r.fails(503, "judge_pending")
	}

	e.judgeAll(j)
	var review page[judged]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/review", token: owner}).ok(200, &review)
	if len(review.Items) != 1 {
		t.Fatalf("review = %+v", review.Items)
	}
	item := review.Items[0]
	if item.State != "conflict" || item.Judge.Verdict != "contradicts" || item.Judge.Related.Ref != railway.Memory.Ref ||
		len(item.Links) != 1 || item.Links[0].Kind != "conflicts_with" || item.Links[0].Ref != railway.Memory.Ref {
		t.Fatalf("review item = %+v %+v", item.memory, item.Links)
	}
	// Keep says it's in conflict, and names the decision in the way.
	if got := e.do(call{method: "POST", path: memoryPath(fly.Memory, ":keep"), token: owner}).
		fails(409, "in_conflict"); got.Details.Ref != railway.Memory.Ref {
		t.Errorf("in_conflict names %q, want %s", got.Details.Ref, railway.Memory.Ref)
	}

	// ReviewConflict.
	var cf struct {
		Memory      judged `json:"memory"`
		Other       judged `json:"other"`
		FlaggedRef  string `json:"flagged_ref"`
		DecisionRef string `json:"decision_ref"`
		Options     []struct {
			Choice  string `json:"choice"`
			Allowed bool   `json:"allowed"`
			Label   string `json:"label"`
			Effects []struct {
				Ref    string `json:"ref"`
				Change string `json:"change"`
			} `json:"effects"`
		} `json:"options"`
		Receipts  []receipt `json:"receipts"`
		Question  string    `json:"question"`
		Suggested string    `json:"suggested"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + fly.Memory.Ref + "/conflict?space=" + sp.slug, token: owner}).ok(200, &cf)
	if cf.Memory.ID != fly.Memory.ID || cf.Other.ID != railway.Memory.ID || cf.FlaggedRef != fly.Memory.Ref ||
		cf.DecisionRef != railway.Memory.Ref || len(cf.Options) != 4 || !cf.Options[0].Allowed || len(cf.Receipts) < 3 {
		t.Fatalf("conflict = %+v", cf)
	}
	if o := cf.Options[0]; o.Choice != "keep_this" || o.Effects[1].Change != "superseded" {
		t.Errorf("keep_this = %+v", o)
	}
	// The judge's question, a label per answer and its suggestion.
	labels := []string{}
	for _, o := range cf.Options {
		labels = append(labels, o.Choice+": "+o.Label)
	}
	if cf.Question != "Which host holds for the v2 API?" || cf.Suggested != "keep_both" || strings.Join(labels, " | ") !=
		"keep_this: The new host | keep_other: The host as kept | keep_both: Both, each with its own scope | leave_open: Leave it undecided" {
		t.Errorf("question %q, suggested %q, labels %v", cf.Question, cf.Suggested, labels)
	}
	// From the decision's side, the same words for the same answers.
	var fromDecision struct {
		Options []struct {
			Choice string `json:"choice"`
			Label  string `json:"label"`
		} `json:"options"`
		Question string `json:"question"`
	}
	e.do(call{method: "GET", path: memoryPath(railway.Memory, "/conflict"), token: owner}).ok(200, &fromDecision)
	if fromDecision.Question != cf.Question || fromDecision.Options[0].Label != "The host as kept" ||
		fromDecision.Options[1].Label != "The new host" {
		t.Errorf("from the decision = %+v", fromDecision)
	}
	// The agent can read the conflict but no option is open to it.
	var agentView struct {
		Options []struct {
			Allowed bool `json:"allowed"`
		} `json:"options"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + fly.Memory.ID.String() + "/conflict", token: key}).ok(200, &agentView)
	for _, o := range agentView.Options {
		if o.Allowed {
			t.Error("an agent may settle a conflict")
		}
	}
	e.do(call{method: "POST", path: memoryPath(fly.Memory, ":resolve-conflict"), token: key,
		body: map[string]any{"choice": "keep_this"}}).fails(403, "refused")

	// Settle it: Fly.io replaces Railway.
	var settled changes
	r := e.do(call{method: "POST", path: memoryPath(fly.Memory, ":resolve-conflict"), token: owner,
		body: map[string]any{"choice": "keep_this", "reason": "Fly.io is where production runs."}})
	r.ok(200, &settled)
	if settled.Outcome != "applied" || settled.Memory.State != "kept" || len(settled.Memories) != 2 || len(settled.Receipts) != 3 ||
		r.header.Get("ETag") == "" {
		t.Fatalf("settled = %+v", settled)
	}
	var old struct {
		Memory struct {
			Decision struct {
				Status string `json:"status"`
			} `json:"decision"`
		} `json:"memory"`
	}
	e.do(call{method: "GET", path: memoryPath(railway.Memory, ""), token: owner}).ok(200, &old)
	if old.Memory.Decision.Status != "superseded" {
		t.Errorf("Railway = %+v", old.Memory.Decision)
	}

	// Undo it, by any of its receipts.
	var undone changes
	e.do(call{method: "POST", path: "/v2/receipts/" + settled.Receipts[1].ID.String() + ":undo", token: owner}).ok(200, &undone)
	if len(undone.Memories) != 2 || undone.Receipts[0].Action != "undid" {
		t.Fatalf("undone = %+v", undone)
	}
	for _, m := range undone.Memories {
		if m.ID == fly.Memory.ID && m.State != "conflict" {
			t.Errorf("after undo: %s", m.State)
		}
	}
	// Twice is refused, with the reason.
	var twice struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	r = e.do(call{method: "POST", path: "/v2/receipts/" + settled.Receipts[0].ID.String() + ":undo", token: owner})
	r.fails(409, "undo_refused")
	if err := json.Unmarshal(r.body, &twice); err != nil || twice.Error.Details.Reason != "already_undone" {
		t.Errorf("twice = %s", r.body)
	}
	// A receipt that names no undoable command, an unknown one, and a bad id.
	e.do(call{method: "POST", path: "/v2/receipts/" + railway.Receipts[0].ID.String() + ":undo", token: owner}).fails(409, "undo_refused")
	e.do(call{method: "POST", path: "/v2/receipts/" + uuid.NewString() + ":undo", token: owner}).fails(404, "not_found")
	e.do(call{method: "POST", path: "/v2/receipts/not-a-receipt:undo", token: owner, invalid: true}).fails(http.StatusBadRequest, "invalid_request")
	// No conflict left to compare once settled the other way.
	e.do(call{method: "POST", path: memoryPath(fly.Memory, ":resolve-conflict"), token: owner,
		body: map[string]any{"choice": "keep_other"}}).ok(200, nil)
	e.do(call{method: "GET", path: memoryPath(fly.Memory, "/conflict"), token: owner}).fails(409, "invalid_transition")
	e.do(call{method: "POST", path: memoryPath(fly.Memory, ":resolve-conflict"), token: owner,
		body: map[string]any{"choice": "sideways"}, invalid: true}).fails(400, "invalid_request")
}

// Rule 11 for "edit, then keep", over /v2: a person's new words that touch
// a decision in force are saved as the proposal's new version and not kept
// (200, outcome proposed, policy judge_pending); Keep on that version waits
// for the judge (503 judge_pending), then keeps it, or says it's in
// conflict. The saved edit is undoable as an edit.
func TestEditThenKeepWaitsForTheJudge(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	owner := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	var railway result
	e.do(call{method: "POST", path: memoriesPath(sp), token: owner,
		body: decisionBody("Deploy the v2 API to Railway for its preview environments.", "deploy target")}).ok(201, &railway)
	propose := func(statement string) memory {
		var p result
		e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: decisionBody(statement, "deploy target")}).ok(201, &p)
		return p.Memory
	}
	editKeep := func(m memory, statement string) *resp {
		return e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: owner,
			header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"statement": statement, "keep": true}})
	}
	keep := func(m memory, version int) *resp {
		return e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: owner,
			header: map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)}})
	}

	// Saved, not kept.
	fly := propose("Deploy the v2 API to Fly.io.")
	var held result
	editKeep(fly, "Deploy the v2 API to Fly.io in iad.").ok(200, &held)
	if held.Outcome != "proposed" || held.Policy.Code != "judge_pending" || held.Policy.Effect != "propose" ||
		held.Memory.Lifecycle != "proposed" || held.Memory.Version != 2 || held.Memory.Statement != "Deploy the v2 API to Fly.io in iad." {
		t.Fatalf("held = %+v", held)
	}
	if len(held.Receipts) != 1 || held.Receipts[0].Action != "edited" {
		t.Errorf("held receipts = %+v", held.Receipts)
	}
	// Keep on the new version waits for the judge, with Retry-After.
	r := keep(fly, 2)
	if r.header.Get("Retry-After") == "" {
		t.Errorf("no Retry-After: %v", r.header)
	}
	if got := r.fails(503, "judge_pending"); got.Details.Ref != fly.Ref {
		t.Errorf("judge_pending names %q", got.Details.Ref)
	}
	// The judge flags it: Keep says it's in conflict, naming the decision.
	e.judgeAll(judge.New(e.ledger, contradicting{}, judge.Config{Primary: judge.Tier{Model: "fake"}, Log: quiet}))
	if got := keep(fly, 2).fails(409, "in_conflict"); got.Details.Ref != railway.Memory.Ref {
		t.Errorf("in_conflict names %q", got.Details.Ref)
	}

	// A check that clears it: the same Keep goes through.
	other := propose("Preview environments for the v2 API need seed data on Railway.")
	editKeep(other, "Preview environments for the v2 API need seed data on Railway, refreshed nightly.").ok(200, &held)
	if held.Policy.Code != "judge_pending" {
		t.Fatalf("held = %+v", held)
	}
	e.judgeAll(judge.New(e.ledger, nil, judge.Config{Log: quiet}))
	var kept result
	keep(other, 2).ok(200, &kept)
	if kept.Outcome != "applied" || kept.Memory.Lifecycle != "kept" {
		t.Errorf("kept = %+v", kept)
	}
	// Undo the Keep, then the saved edit: back to the agent's words.
	e.do(call{method: "POST", path: "/v2/receipts/" + kept.Receipts[0].ID.String() + ":undo", token: owner}).ok(200, nil)
	var undone changes
	e.do(call{method: "POST", path: "/v2/receipts/" + held.Receipts[0].ID.String() + ":undo", token: owner}).ok(200, &undone)
	if undone.Memory.Lifecycle != "proposed" || undone.Memory.Version != 1 ||
		undone.Memory.Statement != "Preview environments for the v2 API need seed data on Railway." {
		t.Errorf("after undoing the edit: %+v", undone.Memory)
	}

	// Words that touch no decision are kept at once, as before.
	var p result
	e.do(call{method: "POST", path: memoriesPath(sp), token: key,
		body: map[string]any{"statement": "Workers must be idempotent.", "section": "conventions"}}).ok(201, &p)
	plain := p.Memory
	editKeep(plain, "Workers must be idempotent and retry safely.").ok(200, &kept)
	if kept.Outcome != "applied" || kept.Memory.Lifecycle != "kept" || kept.Policy.Code == "judge_pending" {
		t.Errorf("plain edit then keep = %+v", kept)
	}
}

// A keep through /v2 is undoable by the person who kept it, and the undo
// is a receipt that names it.
func TestUndoAKeep(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(sp, jy, "contributor")
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	p := e.remember(key, sp, "Workers must be idempotent.")
	var kept result
	e.do(call{method: "POST", path: memoryPath(p.Memory, ":keep"), token: e.session(zz)}).ok(200, &kept)
	// Someone else can't undo it.
	if got := e.do(call{method: "POST", path: "/v2/receipts/" + kept.Receipts[0].ID.String() + ":undo", token: e.session(jy)}).
		fails(403, "refused"); got.Details.Policy.Code != "undo_by_decider" {
		t.Errorf("policy = %+v", got.Details.Policy)
	}
	var undone changes
	e.do(call{method: "POST", path: "/v2/receipts/" + kept.Receipts[0].ID.String() + ":undo", token: e.session(zz),
		body: map[string]any{"reason": "Kept the wrong card."}}).ok(200, &undone)
	if undone.Memory.State != "proposed" || undone.Receipts[0].Reason != "Kept the wrong card." {
		t.Errorf("undone = %+v", undone)
	}
	var rcs page[struct {
		Action string `json:"action"`
		Source struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"source"`
	}]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/receipts?memory=" + p.Memory.ID.String(), token: e.session(zz)}).ok(200, &rcs)
	if rcs.Items[0].Action != "undid" || rcs.Items[0].Source.Kind != "receipt" || rcs.Items[0].Source.Ref != kept.Receipts[0].ID.String() {
		t.Errorf("activity = %+v", rcs.Items[0])
	}
}

// A person's own Remember is undone from its toast: the memory is
// withdrawn (rejected), and nobody else may undo it.
func TestUndoARemember(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(sp, jy, "contributor")
	r := e.remember(e.session(zz), sp, "Pin Node 24 in CI.")
	if r.Outcome != "applied" || r.Memory.State != "kept" {
		t.Fatalf("remember = %+v", r)
	}
	path := "/v2/receipts/" + r.Receipts[0].ID.String() + ":undo"
	if got := e.do(call{method: "POST", path: path, token: e.session(jy)}).fails(403, "refused"); got.Details.Policy.Code != "undo_by_decider" {
		t.Errorf("policy = %+v", got.Details.Policy)
	}
	var undone changes
	e.do(call{method: "POST", path: path, token: e.session(zz)}).ok(200, &undone)
	if undone.Memory.State != "rejected" || undone.Memory.Lifecycle != "rejected" || undone.Receipts[0].Action != "undid" ||
		undone.Receipts[0].Reason != "Withdrawn: undid remembering it." {
		t.Errorf("undone = %+v", undone)
	}
	twice := e.do(call{method: "POST", path: path, token: e.session(zz)})
	twice.fails(409, "undo_refused")
	var body struct {
		Error struct {
			Details struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(twice.body, &body); err != nil || body.Error.Details.Reason != "already_undone" {
		t.Errorf("twice = %s", twice.body)
	}
}
