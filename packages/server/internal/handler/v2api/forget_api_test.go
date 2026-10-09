package v2api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

type tombstone struct {
	ID      uuid.UUID `json:"id"`
	OpID    uuid.UUID `json:"op_id"`
	Ref     string    `json:"ref"`
	Kind    string    `json:"kind"`
	Carried string    `json:"carried"`
	Primary string    `json:"primary"`
	With    []string  `json:"with"`
	Note    string    `json:"note"`
	Status  string    `json:"status"`
	Steps   []struct {
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"steps"`
	Unreachable []struct {
		Kind string `json:"kind"`
		Days int    `json:"days"`
	} `json:"unreachable"`
}

type forgetPreview struct {
	Ref     string `json:"ref"`
	Version int    `json:"version"`
	Carries []struct {
		Ref    string `json:"ref"`
		Reason string `json:"reason"`
	} `json:"carries"`
	Files []struct {
		Label string `json:"label"`
	} `json:"files"`
	Agents  int  `json:"agents"`
	Readers int  `json:"readers"`
	Allowed bool `json:"allowed"`
	Policy  *struct {
		Code string `json:"code"`
	} `json:"policy"`
}

type forgetResult struct {
	Outcome   string    `json:"outcome"`
	Memory    memory    `json:"memory"`
	Receipts  []receipt `json:"receipts"`
	Tombstone tombstone `json:"tombstone"`
	Memories  []memory  `json:"memories"`
}

// Forget over /v2: If-Match is required, what carries its words must be
// named, the result has the tombstone, a replay answers the same, and the
// tombstone and the space's list read it back.
func TestForgetOverV2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	a := e.remember(tok, sp, "Jiahao is away from Oct 12 to Oct 26.").Memory
	var cited result
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.id.String() + "/memories", token: tok, body: map[string]any{
		"statement": "Reviews go to Ziyang until Oct 26.", "section": "conventions",
		"sources": []map[string]any{{"kind": "memory", "ref": a.Ref}}}}).ok(http.StatusCreated, &cited)
	path := "/v2/memories/" + a.Ref + ":forget?space=" + sp.slug

	// The preview says what goes with it, before anything changes.
	var pv forgetPreview
	e.do(call{method: "GET", path: "/v2/memories/" + a.Ref + "/forget-preview?space=" + sp.slug, token: tok}).ok(http.StatusOK, &pv)
	if pv.Ref != a.Ref || pv.Version != 1 || !pv.Allowed || len(pv.Carries) != 1 || pv.Carries[0].Ref != cited.Memory.Ref ||
		pv.Carries[0].Reason != "cites" || len(pv.Files) != 0 || pv.Agents != 0 {
		t.Fatalf("preview = %+v", pv)
	}

	e.do(call{method: "POST", path: path, token: tok, invalid: true}).fails(http.StatusPreconditionRequired, "precondition_required")
	e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"If-Match": `"7"`}}).fails(http.StatusPreconditionFailed, "edit_clash")
	carries := e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"If-Match": `"1"`}})
	ce := carries.fails(http.StatusConflict, "forget_carries")
	if ce.Details.Ref != a.Ref || !strings.Contains(string(carries.body), cited.Memory.Ref) {
		t.Fatalf("forget_carries: %s", carries.body)
	}

	key := uuid.NewString()
	body := map[string]any{"note": "personal, and not something agents need", "carries": []string{cited.Memory.Ref}}
	var res forgetResult
	e.do(call{method: "POST", path: path, token: tok, body: body, header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}}).
		ok(http.StatusOK, &res)
	if res.Outcome != "applied" || res.Memory.State != "forgotten" || res.Memory.Statement != "" || res.Tombstone.Ref != a.Ref ||
		res.Tombstone.Status != "propagating" || res.Tombstone.Note != body["note"] || len(res.Memories) != 1 ||
		res.Memories[0].Ref != cited.Memory.Ref || len(res.Tombstone.With) != 1 {
		t.Fatalf("forget = %+v", res)
	}
	replay := e.do(call{method: "POST", path: path, token: tok, body: body, header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}})
	var again forgetResult
	replay.ok(http.StatusOK, &again)
	if replay.header.Get("Idempotent-Replayed") != "true" || again.Tombstone.ID != res.Tombstone.ID {
		t.Errorf("replay: %v %+v", replay.header, again.Tombstone)
	}
	// Forgetting again is a conflict, not a second tombstone.
	e.do(call{method: "POST", path: path, token: tok, header: map[string]string{"If-Match": `"1"`}}).fails(http.StatusConflict, "invalid_transition")

	var ts tombstone
	e.do(call{method: "GET", path: "/v2/memories/" + a.Ref + "/tombstone?space=" + sp.slug, token: tok}).ok(http.StatusOK, &ts)
	if ts.ID != res.Tombstone.ID || len(ts.Steps) < 2 || ts.Steps[0].Kind != "asked" || ts.Steps[1].Kind != "removed" {
		t.Errorf("tombstone = %+v", ts)
	}
	backups := false
	for _, u := range ts.Unreachable {
		backups = backups || (u.Kind == "backups" && u.Days == 7)
	}
	if !backups {
		t.Errorf("the tombstone doesn't say backups keep it: %+v", ts.Unreachable)
	}
	var carried tombstone
	e.do(call{method: "GET", path: "/v2/memories/" + cited.Memory.ID.String() + "/tombstone", token: tok}).ok(http.StatusOK, &carried)
	if carried.Carried != "cites" || carried.Primary != a.Ref || carried.OpID != res.Tombstone.ID {
		t.Errorf("carried tombstone = %+v", carried)
	}
	// A memory that isn't forgotten has none.
	b := e.remember(tok, sp, "Use pnpm workspaces only.").Memory
	e.do(call{method: "GET", path: "/v2/memories/" + b.Ref + "/tombstone?space=" + sp.slug, token: tok}).fails(http.StatusNotFound, "not_found")

	var list struct {
		Tombstones []tombstone `json:"tombstones"`
		HasMore    bool        `json:"has_more"`
		NextCursor string      `json:"next_cursor"`
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/tombstones?limit=1", token: tok}).ok(http.StatusOK, &list)
	if len(list.Tombstones) != 1 || !list.HasMore || list.NextCursor == "" || len(list.Tombstones[0].Steps) != 0 {
		t.Fatalf("list = %+v", list)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/tombstones?cursor=" + list.NextCursor, token: tok}).ok(http.StatusOK, &list)
	if len(list.Tombstones) != 1 || list.HasMore {
		t.Errorf("second page = %+v", list)
	}
}

// Who may forget: a viewer, an API key and a member (by default) are
// refused; another person's space is not found.
func TestForgetIsRefusedOverV2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, vi, jy, other := e.user("zz"), e.user("vi"), e.user("jy"), e.user("other")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(sp, vi, "viewer")
	e.join(sp, jy, "contributor")
	m := e.remember(e.session(zz), sp, "Pin Node 24 in CI.").Memory
	path := "/v2/memories/" + m.Ref + ":forget?space=" + sp.id.String()
	ifm := map[string]string{"If-Match": `"1"`}
	var pv forgetPreview
	e.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "/forget-preview?space=" + sp.slug, token: e.session(vi)}).ok(http.StatusOK, &pv)
	if pv.Allowed || pv.Policy == nil || pv.Policy.Code != policy.CodeForgetNotAllowed {
		t.Errorf("a viewer's preview: %+v", pv)
	}
	if c := e.do(call{method: "POST", path: path, token: e.session(vi), header: ifm}).fails(http.StatusForbidden, "refused"); c.Details.Policy.Code != policy.CodeForgetNotAllowed {
		t.Errorf("viewer: %+v", c)
	}
	if c := e.do(call{method: "POST", path: path, token: e.session(jy), header: ifm}).fails(http.StatusForbidden, "refused"); c.Details.Policy.Code != policy.CodeForgetNotAllowed {
		t.Errorf("member: %+v", c)
	}
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	if c := e.do(call{method: "POST", path: path, token: key, header: ifm}).fails(http.StatusForbidden, "refused"); c.Details.Policy.Code != policy.CodeKeyCannotForget {
		t.Errorf("api key: %+v", c)
	}
	e.do(call{method: "POST", path: "/v2/memories/" + m.ID.String() + ":forget", token: e.session(other), header: ifm}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.id.String() + "/tombstones", token: e.session(other)}).fails(http.StatusNotFound, "not_found")
	if n := e.count(`SELECT count(*) FROM v2.tombstones`); n != 0 {
		t.Errorf("%d tombstones after refusals", n)
	}
}

// An agent asks; the request shows on the memory; a person keeps it
// instead. Once a person forgets a memory, the agent's notices list it
// until it acknowledges them.
func TestForgetRequestsAndNoticesOverV2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	m := e.remember(tok, sp, "The demo uses the memax-v2 space.").Memory
	n := e.remember(tok, sp, "The staging database is memax-staging-2.").Memory

	var req struct {
		Outcome       string `json:"outcome"`
		ForgetRequest struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
			Agent  struct {
				Agent string `json:"agent"`
			} `json:"agent"`
		} `json:"forget_request"`
	}
	e.do(call{method: "POST", path: "/v2/memories/" + m.Ref + ":request-forget?space=" + sp.slug, token: key,
		body: map[string]any{"reason": "it was a test value"}}).ok(http.StatusOK, &req)
	if req.Outcome != "applied" || req.ForgetRequest.Status != "waiting" || req.ForgetRequest.Reason != "it was a test value" {
		t.Fatalf("request = %+v", req)
	}
	var detail struct {
		ForgetRequests []struct {
			Status string `json:"status"`
		} `json:"forget_requests"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "?space=" + sp.slug, token: tok}).ok(http.StatusOK, &detail)
	if len(detail.ForgetRequests) != 1 {
		t.Errorf("memory's requests = %+v", detail.ForgetRequests)
	}
	// People forget, they don't request.
	if c := e.do(call{method: "POST", path: "/v2/memories/" + m.Ref + ":request-forget?space=" + sp.slug, token: tok}).
		fails(http.StatusForbidden, "refused"); c.Details.Policy.Code != policy.CodeForgetByPerson {
		t.Errorf("a person's request: %+v", c)
	}
	var kept result
	e.do(call{method: "POST", path: "/v2/memories/" + m.Ref + ":decline-forget?space=" + sp.slug, token: tok}).ok(http.StatusOK, &kept)
	if kept.Memory.Lifecycle != "kept" || kept.Receipts[0].Action != "forget_declined" {
		t.Errorf("keep it: %+v", kept)
	}
	e.do(call{method: "POST", path: "/v2/memories/" + m.Ref + ":decline-forget?space=" + sp.slug, token: tok}).fails(http.StatusConflict, "invalid_transition")

	// A person forgets n: the agent, connected to the space, is told.
	e.do(call{method: "POST", path: "/v2/memories/" + n.Ref + ":forget?space=" + sp.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	var notices struct {
		Notices []struct {
			ID   uuid.UUID `json:"id"`
			Kind string    `json:"kind"`
			Refs []string  `json:"refs"`
		} `json:"notices"`
	}
	e.do(call{method: "GET", path: "/v2/notices", token: key}).ok(http.StatusOK, &notices)
	if len(notices.Notices) != 1 || notices.Notices[0].Kind != "forgotten" || len(notices.Notices[0].Refs) != 1 || notices.Notices[0].Refs[0] != n.Ref {
		t.Fatalf("notices = %+v", notices)
	}
	var ack struct {
		Acknowledged int `json:"acknowledged"`
	}
	e.do(call{method: "POST", path: "/v2/notices:ack", token: key, body: map[string]any{"ids": []uuid.UUID{notices.Notices[0].ID}}}).ok(http.StatusOK, &ack)
	if ack.Acknowledged != 1 {
		t.Errorf("acknowledged = %d", ack.Acknowledged)
	}
	e.do(call{method: "POST", path: "/v2/notices:ack", token: key, body: map[string]any{"ids": []uuid.UUID{notices.Notices[0].ID}}}).ok(http.StatusOK, &ack)
	if ack.Acknowledged != 0 {
		t.Errorf("acknowledged again = %d", ack.Acknowledged)
	}
	e.do(call{method: "GET", path: "/v2/notices", token: key}).ok(http.StatusOK, &notices)
	if len(notices.Notices) != 0 {
		t.Errorf("still told: %+v", notices)
	}
	// A person has none.
	e.do(call{method: "GET", path: "/v2/notices", token: tok}).ok(http.StatusOK, &notices)
	if len(notices.Notices) != 0 {
		t.Errorf("a person's notices: %+v", notices)
	}
}
