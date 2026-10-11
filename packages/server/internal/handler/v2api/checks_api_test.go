package v2api_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// spaceConditions is GET …/conditions as the tests read it.
type spaceConditions struct {
	Repository string `json:"repository"`
	Items      []struct {
		Memory     string `json:"memory"`
		Conditions []struct {
			Condition struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"condition"`
			Hash       string `json:"hash"`
			Armed      bool   `json:"armed"`
			HeldCommit string `json:"held_commit"`
		} `json:"conditions"`
	} `json:"items"`
}

type checkRunOut struct {
	Run struct {
		ID     string `json:"id"`
		Capped bool   `json:"capped"`
		Counts struct {
			Holds, Fails, Unknown, Ignored int
		} `json:"counts"`
	} `json:"run"`
	Flagged []string `json:"flagged"`
}

// checksEnv is a project space on MemaxLabs/memax with a Brief, AGENTS.md
// and a kept memory that names a path; its owner's CLI session.
type checksEnv struct {
	*env
	owner uuid.UUID
	tok   string
	sp    space
	mem   memory
	hash  string
}

func newChecksEnv(t *testing.T) *checksEnv {
	t.Helper()
	e := newEnv(t)
	c := &checksEnv{env: e, owner: e.user("zz")}
	c.tok = e.session(c.owner)
	c.sp = e.space(c.owner, policy.SpaceProject, "memax-v2")
	e.exec(`UPDATE hubs SET repository = 'MemaxLabs/memax' WHERE id = $1`, c.sp.id)
	e.do(call{method: "POST", path: briefPath(c.sp), token: c.tok, body: briefBody("memax-v2")}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: targetsPath(c.sp), token: c.tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, nil)
	var res result
	e.do(call{method: "POST", path: memoriesPath(c.sp), token: c.tok,
		body: map[string]any{"statement": "Ask's tiers are set in `packages/server/internal/ask/config.go`.", "section": "conventions"}}).
		ok(http.StatusCreated, &res)
	c.mem = res.Memory
	var list spaceConditions
	e.do(call{method: "GET", path: "/v2/spaces/" + c.sp.slug + "/conditions", token: c.tok}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].Memory != c.mem.Ref || list.Items[0].Conditions[0].Condition.Path != "packages/server/internal/ask/config.go" ||
		list.Items[0].Conditions[0].Armed || list.Items[0].Conditions[0].HeldCommit != "" || list.Repository != "MemaxLabs/memax" {
		t.Fatalf("conditions: %+v", list)
	}
	c.hash = list.Items[0].Conditions[0].Hash
	return c
}

func (c *checksEnv) runBody(n, hoursAgo int, outcome string) map[string]any {
	result := map[string]any{"memory": c.mem.Ref, "condition": "c1", "hash": c.hash, "outcome": outcome}
	if outcome == "fails" {
		result["evidence"] = map[string]any{"change": "removed", "path": "packages/server/internal/ask/config.go"}
	}
	return map[string]any{"repository": "MemaxLabs/memax", "branch": "main", "commit": fmt.Sprintf("%040x", n),
		"commit_at": time.Now().Add(-time.Duration(hoursAgo) * time.Hour).UTC().Format(time.RFC3339),
		"trigger":   "head_moved", "device": "dev-1", "complete": true, "results": []any{result}}
}

func (c *checksEnv) report(token, key string, body map[string]any) *resp {
	return c.do(call{method: "POST", path: "/v2/spaces/" + c.sp.slug + "/check-runs", token: token,
		header: map[string]string{"X-Memax-Via": "cli", "Idempotency-Key": key}, body: body})
}

// The daemon's loop over the API: the conditions, a hold that arms, a
// failure that marks the memory stale as Memax, why it is stale, and the
// same report again.
func TestChecksOverTheAPI(t *testing.T) {
	t.Parallel()
	c := newChecksEnv(t)
	var out checkRunOut
	c.report(c.tok, uuid.NewString(), c.runBody(1, 3, "holds")).ok(http.StatusCreated, &out)
	if out.Run.Counts.Holds != 1 || len(out.Flagged) != 0 {
		t.Fatalf("a hold: %+v", out)
	}
	var list spaceConditions
	c.do(call{method: "GET", path: "/v2/spaces/" + c.sp.slug + "/conditions", token: c.tok}).ok(http.StatusOK, &list)
	// Armed, with the commit it held at: where a machine that didn't see
	// it hold looks for a rename.
	if !list.Items[0].Conditions[0].Armed || list.Items[0].Conditions[0].HeldCommit != fmt.Sprintf("%040x", 1) {
		t.Fatalf("not armed: %+v", list)
	}

	key := uuid.NewString()
	fail := c.runBody(2, 1, "fails")
	c.report(c.tok, key, fail).ok(http.StatusCreated, &out)
	if fmt.Sprint(out.Flagged) != "["+c.mem.Ref+"]" {
		t.Fatalf("the failure: %+v", out)
	}
	// A failure keeps the commit it last held at.
	c.do(call{method: "GET", path: "/v2/spaces/" + c.sp.slug + "/conditions", token: c.tok}).ok(http.StatusOK, &list)
	if got := list.Items[0].Conditions[0]; !got.Armed || got.HeldCommit != fmt.Sprintf("%040x", 1) {
		t.Fatalf("after the failure: %+v", got)
	}
	var again checkRunOut
	r := c.report(c.tok, key, fail).ok(http.StatusCreated, &again)
	if r.header.Get("Idempotent-Replayed") != "true" || again.Run.ID != out.Run.ID || fmt.Sprint(again.Flagged) != fmt.Sprint(out.Flagged) {
		t.Errorf("replay: %+v", again)
	}

	var rep struct {
		Stale   bool     `json:"stale"`
		Flagged *receipt `json:"flagged"`
		Causes  []struct {
			Kind      string `json:"kind"`
			Branch    string `json:"branch"`
			Commit    string `json:"commit"`
			Observer  string `json:"observer"`
			Condition struct {
				Path string `json:"path"`
			} `json:"condition"`
			Evidence struct {
				Change string `json:"change"`
			} `json:"evidence"`
		} `json:"causes"`
	}
	c.do(call{method: "GET", path: "/v2/memories/" + c.mem.Ref + "/stale?space=" + c.sp.slug, token: c.tok}).ok(http.StatusOK, &rep)
	if !rep.Stale || rep.Flagged == nil || rep.Flagged.ActorKind != "memax" || len(rep.Causes) != 1 || rep.Causes[0].Kind != "check" ||
		rep.Causes[0].Evidence.Change != "removed" || rep.Causes[0].Branch != "main" || rep.Causes[0].Observer != "device" ||
		rep.Causes[0].Condition.Path != "packages/server/internal/ask/config.go" {
		t.Fatalf("stale report: %+v", rep)
	}
	// Still true: the flag clears, and the same failure doesn't come back.
	c.do(call{method: "POST", path: memoryPath(c.mem, ":verify"), token: c.tok,
		header: map[string]string{"If-Match": strconv.Quote("1")}, body: map[string]any{"answer": "still_true"}}).ok(http.StatusOK, nil)
	c.report(c.tok, uuid.NewString(), c.runBody(3, 0, "fails")).ok(http.StatusCreated, &out)
	if len(out.Flagged) != 0 {
		t.Errorf("flagged again after Verify: %+v", out)
	}
}

// A Forget's tombstone names the check results the machines kept: a
// checks step, waiting on the machine that checked the space until it
// checks it again, then done.
func TestForgetNamesTheMachinesChecksOverTheAPI(t *testing.T) {
	t.Parallel()
	c := newChecksEnv(t)
	c.report(c.tok, uuid.NewString(), c.runBody(1, 3, "holds")).ok(http.StatusCreated, nil)
	c.do(call{method: "POST", path: memoryPath(c.mem, ":forget"), token: c.tok,
		header: map[string]string{"If-Match": strconv.Quote("1")}}).ok(http.StatusOK, nil)
	type step struct {
		Kind   string `json:"kind"`
		Status string `json:"status"`
		Reason string `json:"reason"`
		Count  *int   `json:"count"`
	}
	checks := func() *step {
		var ts struct {
			Steps []step `json:"steps"`
		}
		c.do(call{method: "GET", path: memoryPath(c.mem, "/tombstone"), token: c.tok}).ok(http.StatusOK, &ts)
		for i := range ts.Steps {
			if ts.Steps[i].Kind == "checks" {
				return &ts.Steps[i]
			}
		}
		return nil
	}
	if s := checks(); s == nil || s.Status != "waiting" || s.Reason != "next_check" || s.Count == nil || *s.Count != 1 {
		t.Fatalf("after the Forget: %+v", s)
	}
	// The machine checks the space again: its daemon dropped what it kept
	// and reports its next run, here with nothing left to say.
	body := c.runBody(2, 0, "holds")
	body["checked_at"] = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
	body["results"] = []any{}
	var out checkRunOut
	c.report(c.tok, uuid.NewString(), body).ok(http.StatusCreated, &out)
	if out.Run.Counts.Holds+out.Run.Counts.Fails+out.Run.Counts.Unknown+out.Run.Counts.Ignored != 0 {
		t.Errorf("a run of nothing: %+v", out.Run.Counts)
	}
	if s := checks(); s == nil || s.Status != "done" || s.Count == nil || *s.Count != 0 {
		t.Fatalf("after its next check: %+v", s)
	}
}

// Refusals over the API: an agent's key, a viewer, a fork's clone and a
// malformed result.
func TestChecksRefusals(t *testing.T) {
	t.Parallel()
	c := newChecksEnv(t)
	key, _ := c.apiKey(c.owner, keyOpts{agent: "codex"})
	if f := c.report(key, uuid.NewString(), c.runBody(1, 1, "holds")).fails(http.StatusForbidden, "refused"); f.Details.Policy.Code != "checks_by_person" {
		t.Errorf("an agent's key: %+v", f)
	}
	viewer := c.user("viewer")
	c.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'viewer')`, c.sp.id, viewer)
	if f := c.report(c.session(viewer), uuid.NewString(), c.runBody(1, 1, "holds")).fails(http.StatusForbidden, "refused"); f.Details.Policy.Code != "viewer" {
		t.Errorf("a viewer: %+v", f)
	}
	fork := c.runBody(1, 1, "holds")
	fork["repository"] = "someone/memax"
	c.report(c.tok, uuid.NewString(), fork).fails(http.StatusBadRequest, "invalid_request")
	bad := c.runBody(1, 1, "fails")
	delete(bad["results"].([]any)[0].(map[string]any), "evidence") // a failure that doesn't say what changed
	c.report(c.tok, uuid.NewString(), bad).fails(http.StatusBadRequest, "invalid_request")
	other := c.user("other")
	c.do(call{method: "GET", path: "/v2/spaces/" + c.sp.slug + "/conditions", token: c.session(other)}).fails(http.StatusNotFound, "not_found")
	if n := c.count(`SELECT count(*) FROM v2.check_runs WHERE space_id = $1`, c.sp.id); n != 0 {
		t.Errorf("%d runs recorded", n)
	}
}
