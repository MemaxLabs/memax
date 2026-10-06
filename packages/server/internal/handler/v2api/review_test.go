package v2api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
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
	}
	out := struct {
		Pairs      []pair `json:"pairs"`
		Conditions []any  `json:"conditions"`
	}{Pairs: []pair{}, Conditions: []any{}}
	for _, m := range candidateTag.FindAllStringSubmatch(c.Prompt, -1) {
		p := pair{Candidate: m[1], Relation: "unrelated", Confidence: 0.9, Rationale: "Different subject."}
		if m[2] == "true" {
			p.Relation, p.Confidence, p.Rationale = "contradicts", 0.93, "It picks another host than "+m[1]+"."
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
	e.do(call{method: "POST", path: memoryPath(fly.Memory, ":keep"), token: owner}).fails(409, "invalid_transition")

	// ReviewConflict.
	var cf struct {
		Memory      judged `json:"memory"`
		Other       judged `json:"other"`
		FlaggedRef  string `json:"flagged_ref"`
		DecisionRef string `json:"decision_ref"`
		Options     []struct {
			Choice  string `json:"choice"`
			Allowed bool   `json:"allowed"`
			Effects []struct {
				Ref    string `json:"ref"`
				Change string `json:"change"`
			} `json:"effects"`
		} `json:"options"`
		Receipts []receipt `json:"receipts"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + fly.Memory.Ref + "/conflict?space=" + sp.slug, token: owner}).ok(200, &cf)
	if cf.Memory.ID != fly.Memory.ID || cf.Other.ID != railway.Memory.ID || cf.FlaggedRef != fly.Memory.Ref ||
		cf.DecisionRef != railway.Memory.Ref || len(cf.Options) != 4 || !cf.Options[0].Allowed || len(cf.Receipts) < 3 {
		t.Fatalf("conflict = %+v", cf)
	}
	if o := cf.Options[0]; o.Choice != "keep_this" || o.Effects[1].Change != "superseded" {
		t.Errorf("keep_this = %+v", o)
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
