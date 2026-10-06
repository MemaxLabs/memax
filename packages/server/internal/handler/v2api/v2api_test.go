package v2api_test

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func memoriesPath(sp space) string { return "/v2/spaces/" + sp.id.String() + "/memories" }

func memoryPath(m memory, command string) string {
	return "/v2/memories/" + m.ID.String() + command
}

func TestRememberAsAPersonIsKept(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)

	body := map[string]any{
		"statement": "  River is our queue, not Kafka.  ", "section": "decisions", "kind": "decision",
		"decision": map[string]any{"why": "Postgres-backed", "status": "in_force", "options": []map[string]string{{"label": "Kafka"}}},
		"sources":  []map[string]any{{"kind": "pr", "ref": "PR #212", "uri": "https://github.com/MemaxLabs/memax/pull/212"}},
		"reason":   "decided in standup",
	}
	// By slug, from the CLI.
	r := e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories", token: tok, body: body,
		header: map[string]string{"X-Memax-Via": "cli"}})
	var res result
	r.ok(http.StatusCreated, &res)

	m := res.Memory
	if res.Outcome != "applied" || res.Policy.Effect != "apply" {
		t.Errorf("outcome = %s/%s, want applied", res.Outcome, res.Policy.Effect)
	}
	if m.Ref != "M-0001" || m.State != "kept" || m.Statement != "River is our queue, not Kafka." || m.Version != 1 {
		t.Errorf("memory = %s %s %q v%d", m.Ref, m.State, m.Statement, m.Version)
	}
	if m.Trust != "repository" || len(m.Sources) != 1 || m.Sources[0].Ref != "PR #212" {
		t.Errorf("trust %s, sources %+v", m.Trust, m.Sources)
	}
	if got := r.header.Get("ETag"); got != `"1"` {
		t.Errorf("ETag = %q", got)
	}
	if got := r.header.Get("Location"); got != "/v2/memories/"+m.ID.String() {
		t.Errorf("Location = %q", got)
	}
	if len(res.Receipts) != 1 {
		t.Fatalf("receipts = %+v", res.Receipts)
	}
	rc := res.Receipts[0]
	if rc.Action != "kept" || rc.ActorKind != "person" || rc.ActorID == nil || *rc.ActorID != zz || rc.Via != "cli" {
		t.Errorf("receipt = %+v", rc)
	}
	// Nothing on /v2 can claim a person on the web yet.
	if rc.Assurance != "client_attested" {
		t.Errorf("assurance = %q, want client_attested", rc.Assurance)
	}
	if r.header.Get("Idempotent-Replayed") != "" {
		t.Error("a first write is not a replay")
	}
}

// TestWhoKeepsWhat runs the write policy end to end through HTTP: who
// remembers, who proposes, and who may keep.
func TestWhoKeepsWhat(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, jy, vi := e.user("zz"), e.user("jy"), e.user("vi")
	sp := e.space(zz, policy.SpaceTeam, "acme")
	e.join(sp, jy, "contributor") // member
	e.join(sp, vi, "viewer")

	key, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	readKey, _ := e.apiKey(zz, keyOpts{perms: []string{"memory:read"}, agent: "codex"})
	grantTok, grantID := e.grant(zz, "claude-ai", []string{"memory:read", "memory:write"})
	legacy, err := auth.SignAgentAccessToken(zz.String(), "claude-code", []byte(testSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	writes := []struct {
		name, token string
		status      int
		outcome     string
		code        string
		actorKind   string
		actorID     uuid.UUID
		agent       string
	}{
		{"owner remembers", e.session(zz), 201, "applied", "", "person", zz, ""},
		{"member remembers", e.session(jy), 201, "applied", "", "person", jy, ""},
		{"viewer proposes", e.session(vi), 201, "proposed", "viewer", "person", vi, ""},
		{"API key proposes", key, 201, "proposed", "api_key", "agent", keyID, "codex"},
		{"OAuth grant proposes", grantTok, 201, "proposed", "autonomy_propose", "agent", grantID, "claude-ai"},
		{"legacy agent token proposes", legacy, 201, "proposed", "autonomy_propose", "agent", zz, "claude-code"},
		{"read-only key is refused", readKey, 403, "", "key_read_only", "", uuid.Nil, ""},
	}
	for _, w := range writes {
		r := e.do(call{method: "POST", path: memoriesPath(sp), token: w.token, body: remember(w.name+".", "conventions")})
		if w.status == 403 {
			if got := r.fails(403, "refused"); got.Details.Policy.Code != w.code || got.Details.Policy.Effect != "refuse" {
				t.Errorf("%s: policy = %+v", w.name, got.Details.Policy)
			}
			continue
		}
		var res result
		r.ok(w.status, &res)
		rc := res.Receipts[0]
		if res.Outcome != w.outcome || res.Policy.Code != w.code || rc.ActorKind != w.actorKind || *rc.ActorID != w.actorID || rc.Agent != w.agent {
			t.Errorf("%s: %s/%s by %s %s %q", w.name, res.Outcome, res.Policy.Code, rc.ActorKind, rc.ActorID, rc.Agent)
		}
	}

	propose := func() memory {
		var res result
		e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: remember("Proposed by Codex.", "conventions")}).ok(201, &res)
		return res.Memory
	}
	reviews := []struct {
		name, token, command string
		status               int
		code                 string // the policy code of a refusal
		state                string
	}{
		{"API key can't keep", key, ":keep", 403, "person_must_review", ""},
		{"API key can't reject", key, ":reject", 403, "person_must_review", ""},
		{"OAuth grant can't keep", grantTok, ":keep", 403, "person_must_review", ""},
		{"viewer can't keep", e.session(vi), ":keep", 403, "viewer", ""},
		{"viewer can't reject", e.session(vi), ":reject", 403, "viewer", ""},
		{"member keeps", e.session(jy), ":keep", 200, "", "kept"},
		{"owner rejects", e.session(zz), ":reject", 200, "", "rejected"},
	}
	for _, rv := range reviews {
		m := propose()
		r := e.do(call{method: "POST", path: memoryPath(m, rv.command), token: rv.token,
			header: map[string]string{"If-Match": `"1"`}})
		if rv.status == 403 {
			if got := r.fails(403, "refused"); got.Details.Policy.Code != rv.code {
				t.Errorf("%s: policy code %q, want %q", rv.name, got.Details.Policy.Code, rv.code)
			}
			continue
		}
		var res result
		r.ok(200, &res)
		if res.Memory.State != rv.state || res.Outcome != "applied" {
			t.Errorf("%s: %s/%s", rv.name, res.Outcome, res.Memory.State)
		}
		last := res.Receipts[len(res.Receipts)-1]
		if last.ActorKind != "person" || (rv.state == "kept" && last.Assurance != "client_attested") {
			t.Errorf("%s: receipt %+v", rv.name, last)
		}
	}
}

func TestEditNeedsIfMatch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	m := e.remember(tok, sp, "Deploys run on Fridays.").Memory
	edit := func(ifMatch, statement string) *resp {
		return e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: tok,
			header: map[string]string{"If-Match": ifMatch}, body: map[string]any{"statement": statement, "reason": "moved"}})
	}

	var res result
	r := edit(`"1"`, "Deploys run on Thursdays.").ok(200, &res)
	if res.Memory.Version != 2 || res.Memory.Statement != "Deploys run on Thursdays." || r.header.Get("ETag") != `"2"` {
		t.Errorf("after edit: v%d %q ETag %s", res.Memory.Version, res.Memory.Statement, r.header.Get("ETag"))
	}
	if len(res.Receipts) != 1 || res.Receipts[0].Action != "edited" || res.Receipts[0].Reason != "moved" {
		t.Errorf("edit receipts = %+v", res.Receipts)
	}

	clash := edit(`"1"`, "Deploys run on Mondays.").fails(http.StatusPreconditionFailed, "edit_clash")
	if clash.Details.Ref != m.Ref || clash.Details.ExpectedVersion != 1 || clash.Details.CurrentVersion != 2 {
		t.Errorf("clash details = %+v", clash.Details)
	}
	// A bare version works too.
	edit(`2`, "Deploys run on Mondays.").ok(200, nil)

	e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: tok, invalid: true,
		body: map[string]any{"statement": "No If-Match."}}).fails(http.StatusPreconditionRequired, "precondition_required")
	e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: tok, invalid: true,
		header: map[string]string{"If-Match": `W/"3"`}, body: map[string]any{"statement": "Weak tag."}}).fails(400, "invalid_request")
	if got := edit(`"3"`, "Deploys run on Mondays.").fails(400, "invalid_request"); got.Details.Field != "statement" {
		t.Errorf("unchanged statement: field %q", got.Details.Field)
	}
	// Keep checks If-Match too, and a kept memory can't be kept again.
	e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: tok, header: map[string]string{"If-Match": `"1"`}}).
		fails(http.StatusPreconditionFailed, "edit_clash")
	if got := e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: tok}).fails(http.StatusConflict, "invalid_transition"); got.Details.Ref != m.Ref {
		t.Errorf("transition details = %+v", got.Details)
	}
}

func TestIdempotentReplay(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key := uuid.NewString()
	send := func(statement string, idem string) *resp {
		return e.do(call{method: "POST", path: memoriesPath(sp), token: tok, body: remember(statement, "preferences"),
			header: map[string]string{"Idempotency-Key": idem}})
	}

	first := send("Prefers diffs over prose.", key).ok(201, nil)
	again := send("Prefers diffs over prose.", key).ok(201, nil)
	if !bytes.Equal(first.body, again.body) {
		t.Errorf("a replay must return the same body:\n%s\n%s", first.body, again.body)
	}
	if again.header.Get("Idempotent-Replayed") != "true" || first.header.Get("ETag") != again.header.Get("ETag") ||
		first.header.Get("Location") != again.header.Get("Location") {
		t.Errorf("replay headers = %v (first %v)", again.header, first.header)
	}
	var page page[memory]
	e.do(call{method: "GET", path: memoriesPath(sp), token: tok}).ok(200, &page)
	if len(page.Items) != 1 {
		t.Errorf("a replay wrote again: %d memories", len(page.Items))
	}

	send("Prefers prose over diffs.", key).fails(http.StatusUnprocessableEntity, "idempotency_key_reused")
	e.do(call{method: "POST", path: memoriesPath(sp), token: tok, body: remember("No key.", "preferences"), invalid: true,
		header: map[string]string{"Idempotency-Key": ""}}).fails(400, "idempotency_key_required")

	// The same key from another actor is another command.
	other := e.user("jy")
	e.join(sp, other, "contributor")
	e.do(call{method: "POST", path: memoriesPath(sp), token: e.session(other), body: remember("Prefers diffs over prose.", "preferences"),
		header: map[string]string{"Idempotency-Key": key}}).ok(201, nil)
	if r := e.do(call{method: "GET", path: memoriesPath(sp), token: tok}).ok(200, &page); len(page.Items) != 2 {
		t.Errorf("memories = %d, want 2 (%s)", len(page.Items), r.body)
	}
}

// TestSpacesDontLeak: another user's spaces and memories are not found,
// never forbidden, whichever way they are addressed.
func TestSpacesDontLeak(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, b := e.user("a"), e.user("b")
	aSpace := e.space(a, policy.SpaceProject, "alpha")
	bSpace := e.space(b, policy.SpaceProject, "beta")
	aTok, bTok := e.session(a), e.session(b)
	mine := e.remember(aTok, aSpace, "Alpha's first memory.").Memory
	theirs := e.remember(bTok, bSpace, "Beta's first memory.").Memory
	if mine.Ref != theirs.Ref {
		t.Fatalf("both tenants should start at M-0001: %s, %s", mine.Ref, theirs.Ref)
	}

	notFound := []call{
		{method: "GET", path: "/v2/memories/" + theirs.Ref + "?space=" + bSpace.id.String()},
		{method: "GET", path: "/v2/memories/" + theirs.Ref + "?space=" + bSpace.slug},
		{method: "GET", path: "/v2/memories/" + theirs.ID.String()},
		{method: "GET", path: "/v2/memories/" + theirs.ID.String() + "?space=" + aSpace.id.String()},
		{method: "POST", path: "/v2/memories/" + theirs.ID.String() + ":keep"},
		{method: "POST", path: "/v2/memories/" + theirs.ID.String() + ":reject"},
		{method: "POST", path: "/v2/memories/" + theirs.ID.String() + ":edit", body: map[string]any{"statement": "Mine now."},
			header: map[string]string{"If-Match": `"1"`}},
		{method: "GET", path: memoriesPath(bSpace)},
		{method: "GET", path: "/v2/spaces/" + bSpace.slug + "/memories"},
		{method: "GET", path: "/v2/spaces/" + bSpace.id.String() + "/review"},
		{method: "GET", path: "/v2/spaces/" + bSpace.id.String() + "/receipts"},
		{method: "GET", path: "/v2/spaces/" + aSpace.id.String() + "/receipts?memory=" + theirs.ID.String()},
		{method: "POST", path: memoriesPath(bSpace), body: remember("Planted.", "decisions")},
	}
	for _, c := range notFound {
		c.token = aTok
		e.do(c).fails(404, "not_found")
	}

	// A's own M-0001 is A's, not B's.
	var detail struct {
		Memory memory `json:"memory"`
	}
	e.do(call{method: "GET", path: "/v2/memories/M-0001?space=" + aSpace.slug, token: aTok}).ok(200, &detail)
	if detail.Memory.ID != mine.ID {
		t.Errorf("M-0001 in alpha resolved to %s", detail.Memory.ID)
	}
	// A display ID always travels with its space.
	e.do(call{method: "GET", path: "/v2/memories/M-0001", token: aTok}).fails(400, "space_required")

	var spaces page[struct {
		ID uuid.UUID `json:"id"`
	}]
	e.do(call{method: "GET", path: "/v2/spaces", token: aTok}).ok(200, &spaces)
	if len(spaces.Items) != 1 || spaces.Items[0].ID != aSpace.id {
		t.Errorf("spaces = %+v", spaces.Items)
	}

	// A key scoped to one hub sees only that hub, even of its owner's.
	other := e.space(a, policy.SpaceProject, "gamma")
	scoped, _ := e.apiKey(a, keyOpts{hubs: []uuid.UUID{aSpace.id}})
	e.do(call{method: "GET", path: memoriesPath(other), token: scoped}).fails(404, "not_found")
	e.do(call{method: "GET", path: "/v2/spaces", token: scoped}).ok(200, &spaces)
	if len(spaces.Items) != 1 || spaces.Items[0].ID != aSpace.id {
		t.Errorf("scoped key spaces = %+v", spaces.Items)
	}
}

func TestListMemories(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	for i, s := range []string{"One.", "Two.", "Three."} {
		section := "conventions"
		if i == 2 {
			section = "decisions"
		}
		e.do(call{method: "POST", path: memoriesPath(sp), token: tok, body: remember(s, section)}).ok(201, nil)
	}
	e.remember(key, sp, "Proposed four.")
	e.remember(key, sp, "Proposed five.")

	var refs []string
	cursor := ""
	for range 10 {
		path := memoriesPath(sp) + "?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var p page[memory]
		e.do(call{method: "GET", path: path, token: tok}).ok(200, &p)
		for _, m := range p.Items {
			refs = append(refs, m.Ref)
		}
		if !p.HasMore {
			if p.NextCursor != "" {
				t.Error("the last page has a cursor")
			}
			break
		}
		cursor = p.NextCursor
	}
	if !slices.Equal(refs, []string{"M-0005", "M-0004", "M-0003", "M-0002", "M-0001"}) {
		t.Errorf("paged refs = %v, want newest first", refs)
	}

	count := func(query string) int {
		var p page[memory]
		e.do(call{method: "GET", path: memoriesPath(sp) + query, token: tok}).ok(200, &p)
		return len(p.Items)
	}
	if n := count("?state=proposed"); n != 2 {
		t.Errorf("proposed = %d", n)
	}
	if n := count("?state=kept&state=proposed&section=decisions"); n != 1 {
		t.Errorf("kept or proposed decisions = %d", n)
	}
	if n := count("?state=rejected"); n != 0 {
		t.Errorf("rejected = %d", n)
	}

	e.do(call{method: "GET", path: memoriesPath(sp) + "?state=archived", token: tok, invalid: true}).fails(400, "invalid_request")
	e.do(call{method: "GET", path: memoriesPath(sp) + "?limit=0", token: tok, invalid: true}).fails(400, "invalid_request")
	e.do(call{method: "GET", path: memoriesPath(sp) + "?cursor=bm9wZQ", token: tok}).fails(400, "invalid_request")
	var rc page[receipt]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.id.String() + "/receipts?limit=1", token: tok}).ok(200, &rc)
	e.do(call{method: "GET", path: memoriesPath(sp) + "?cursor=" + rc.NextCursor, token: tok}).fails(400, "invalid_request")
}

func TestReviewQueue(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, vi := e.user("zz"), e.user("vi")
	sp := e.space(zz, policy.SpaceTeam, "acme")
	e.join(sp, vi, "viewer")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	stale := e.remember(tok, sp, "Kept, then stale.").Memory
	p1 := e.remember(key, sp, "Codex proposes.").Memory
	e.remember(tok, sp, "Kept and fine.")
	conflict := e.remember(tok, sp, "Kept, then in conflict.").Memory
	p2 := e.remember(e.session(vi), sp, "A viewer proposes.").Memory
	gone := e.remember(key, sp, "Rejected soon.").Memory
	e.do(call{method: "POST", path: memoryPath(gone, ":reject"), token: tok, body: map[string]any{"reason": "duplicate"}}).ok(200, nil)
	e.flag(stale, "stale")
	e.flag(conflict, "conflict")

	want := []string{p1.Ref, p2.Ref, conflict.Ref, stale.Ref}
	var q page[memory]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/review", token: tok}).ok(200, &q)
	var got []string
	for _, m := range q.Items {
		got = append(got, m.Ref)
	}
	if !slices.Equal(got, want) || q.Total != 4 || q.HasMore {
		t.Fatalf("review = %v total %d, want %v total 4", got, q.Total, want)
	}
	if q.Items[2].State != "conflict" || q.Items[3].State != "stale" {
		t.Errorf("states = %s %s", q.Items[2].State, q.Items[3].State)
	}

	got = nil
	cursor := ""
	for range 10 {
		path := "/v2/spaces/" + sp.id.String() + "/review?limit=3"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var p page[memory]
		e.do(call{method: "GET", path: path, token: tok}).ok(200, &p)
		for _, m := range p.Items {
			got = append(got, m.Ref)
		}
		if !p.HasMore {
			break
		}
		cursor = p.NextCursor
	}
	if !slices.Equal(got, want) {
		t.Errorf("paged review = %v", got)
	}
}

// TestReceiptsHoldNoWords: the Activity feed names actors, actions and
// objects, never the statement.
func TestReceiptsHoldNoWords(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	kept := e.remember(tok, sp, "Unicorn-7f3a ships on Fridays.").Memory
	e.do(call{method: "POST", path: memoryPath(kept, ":edit"), token: tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"statement": "Unicorn-7f3a ships on Thursdays."}}).ok(200, nil)
	prop := e.remember(key, sp, "Unicorn-7f3a has a staging twin.").Memory
	e.do(call{method: "POST", path: memoryPath(prop, ":keep"), token: tok}).ok(200, nil)

	r := e.do(call{method: "GET", path: "/v2/spaces/" + sp.id.String() + "/receipts", token: tok})
	var p page[receipt]
	r.ok(200, &p)
	if bytes.Contains(r.body, []byte("Unicorn-7f3a")) || bytes.Contains(r.body, []byte("Fridays")) {
		t.Errorf("receipts carry memory text: %s", r.body)
	}
	var actions []string
	for _, rc := range p.Items {
		actions = append(actions, rc.ObjectRef+" "+rc.Action)
	}
	want := []string{prop.Ref + " kept", prop.Ref + " proposed", kept.Ref + " edited", kept.Ref + " kept"}
	if !slices.Equal(actions, want) {
		t.Errorf("activity = %v, want %v", actions, want)
	}
	if p.Items[1].ActorKind != "agent" || p.Items[1].Agent != "codex" || p.Items[0].ActorKind != "person" {
		t.Errorf("actors = %+v", p.Items[:2])
	}

	// One memory's history, by display ID within its space.
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/receipts?memory=" + kept.Ref, token: tok}).ok(200, &p)
	if len(p.Items) != 2 || p.Items[0].Action != "edited" {
		t.Errorf("history of %s = %+v", kept.Ref, p.Items)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/receipts?limit=3", token: tok}).ok(200, &p)
	if len(p.Items) != 3 || !p.HasMore {
		t.Errorf("page of 3 = %d has_more %v", len(p.Items), p.HasMore)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/receipts?cursor=" + p.NextCursor, token: tok}).ok(200, &p)
	if len(p.Items) != 1 || p.HasMore {
		t.Errorf("last page = %d has_more %v", len(p.Items), p.HasMore)
	}
}

func TestGetMemory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	var res result
	e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: map[string]any{
		"statement": "MCP write tools must ask with input_required.", "section": "conventions",
		"sources": []map[string]any{
			{"kind": "url", "ref": "MCP spec", "uri": "https://modelcontextprotocol.io/spec", "quote": "A server that needs input returns input_required."},
			{"kind": "session", "ref": "cc-3e1a", "external": true},
		},
	}}).ok(201, &res)
	if !res.Policy.Quarantine || res.Memory.Trust != "external" {
		t.Errorf("external sources: quarantine %v trust %s", res.Policy.Quarantine, res.Memory.Trust)
	}
	for _, s := range res.Memory.Sources {
		if !s.External || s.Trust != "external" {
			t.Errorf("source %s: external %v trust %s", s.Ref, s.External, s.Trust)
		}
	}
	m := res.Memory
	// Quarantined content needs a person on the web to keep it, and
	// nothing on /v2 is human_web yet: edit-and-keep is refused...
	edited := e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"statement": "MCP write tools ask with input_required.", "keep": true}})
	if got := edited.fails(403, "refused"); got.Details.Policy.Code != "external_needs_review" {
		t.Errorf("edit and keep of external content: %+v", got.Details.Policy)
	}
	// ...while the edit alone goes through, and the proposal waits.
	e.do(call{method: "POST", path: memoryPath(m, ":edit"), token: tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"statement": "MCP write tools ask with input_required."}}).ok(200, &res)
	if res.Memory.State != "proposed" || res.Memory.Version != 2 {
		t.Errorf("edit of a proposal: %s v%d", res.Memory.State, res.Memory.Version)
	}
	// Content that isn't external can be edited and kept in one step.
	own := e.remember(key, sp, "Codex's own finding.").Memory
	e.do(call{method: "POST", path: memoryPath(own, ":edit"), token: tok, header: map[string]string{"If-Match": `"1"`},
		body: map[string]any{"statement": "Codex's own finding, edited.", "keep": true}}).ok(200, &res)
	if res.Memory.State != "kept" || len(res.Receipts) != 2 || res.Receipts[1].Action != "kept" {
		t.Errorf("edit and keep: %s, %+v", res.Memory.State, res.Receipts)
	}

	var detail struct {
		Memory   memory `json:"memory"`
		Versions []struct {
			Version   int    `json:"version"`
			Statement string `json:"statement"`
		} `json:"versions"`
		Receipts page[receipt] `json:"receipts"`
	}
	r := e.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "?space=" + sp.slug, token: tok}).ok(200, &detail)
	if r.header.Get("ETag") != `"2"` || detail.Memory.Version != 2 || len(detail.Memory.Sources) != 2 {
		t.Errorf("detail: ETag %s v%d %d sources", r.header.Get("ETag"), detail.Memory.Version, len(detail.Memory.Sources))
	}
	if len(detail.Versions) != 2 || detail.Versions[0].Version != 2 || detail.Versions[1].Statement != "MCP write tools must ask with input_required." {
		t.Errorf("versions = %+v", detail.Versions)
	}
	var actions []string
	for _, rc := range detail.Receipts.Items {
		actions = append(actions, rc.Action)
	}
	if !slices.Equal(actions, []string{"edited", "proposed"}) || detail.Receipts.HasMore {
		t.Errorf("history = %v", actions)
	}
	// The uuid needs no space; a wrong space hides it.
	e.do(call{method: "GET", path: "/v2/memories/" + m.ID.String(), token: tok}).ok(200, nil)
	e.do(call{method: "GET", path: "/v2/memories/memory-one?space=" + sp.slug, token: tok, invalid: true}).fails(400, "invalid_request")
}

func TestRequestErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)

	e.do(call{method: "GET", path: "/v2/spaces"}).fails(401, "unauthorized")
	e.do(call{method: "GET", path: "/v2/spaces", token: "mxk_not-a-key"}).fails(401, "unauthorized")
	noRead, _ := e.apiKey(zz, keyOpts{perms: []string{"config:read", "config:write"}})
	e.do(call{method: "GET", path: "/v2/spaces", token: noRead}).fails(403, "permission_denied")

	cases := []struct {
		name    string
		body    any
		header  map[string]string
		invalid bool
		status  int
		code    string
		field   string
	}{
		{"unknown field", map[string]any{"statement": "x", "section": "decisions", "color": "red"}, nil, true, 400, "invalid_request", "color"},
		{"wrong type", map[string]any{"statement": 7, "section": "decisions"}, nil, true, 400, "invalid_request", "statement"},
		{"not JSON", "{", nil, true, 400, "invalid_request", "body"},
		{"no body", nil, nil, true, 400, "invalid_request", "body"},
		{"bad section", remember("x", "misc"), nil, true, 400, "invalid_request", "section"},
		{"decision fields on a fact", map[string]any{"statement": "x", "section": "decisions", "decision": map[string]any{"why": "y"}}, nil, false, 400, "invalid_request", "decision"},
		{"NUL in a locator", map[string]any{"statement": "x", "section": "decisions",
			"sources": []map[string]any{{"kind": "file", "ref": "go.mod", "locator": map[string]any{"path": "a\x00b"}}}}, nil, false, 400, "invalid_request", "body"},
		{"a surface you can't claim", remember("x", "decisions"), map[string]string{"X-Memax-Via": "web"}, true, 400, "invalid_request", "X-Memax-Via"},
		{"a credential in the statement", remember("Use AKIAIOSFODNN7EXAMPLE to deploy.", "decisions"), nil, false, 403, "refused", ""},
	}
	for _, c := range cases {
		got := e.do(call{method: "POST", path: memoriesPath(sp), token: tok, body: c.body, header: c.header, invalid: c.invalid}).fails(c.status, c.code)
		if got.Details.Field != c.field {
			t.Errorf("%s: field %q, want %q", c.name, got.Details.Field, c.field)
		}
		if c.code == "refused" && got.Details.Policy.Code != "secret_detected" {
			t.Errorf("%s: policy %+v", c.name, got.Details.Policy)
		}
	}
	var p page[memory]
	e.do(call{method: "GET", path: memoriesPath(sp), token: tok}).ok(200, &p)
	if len(p.Items) != 0 {
		t.Errorf("a refused or invalid write left %d memories", len(p.Items))
	}

	// An operator's impersonation session reads but doesn't write.
	admin := e.user("admin")
	imp, err := auth.SignImpersonationToken(zz.String(), admin.String(), []byte(testSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e.do(call{method: "GET", path: memoriesPath(sp), token: imp}).ok(200, nil)
	e.do(call{method: "POST", path: memoriesPath(sp), token: imp, body: remember("As someone else.", "decisions")}).
		fails(403, "impersonation_read_only")
	if !strings.Contains(e.do(call{method: "GET", path: "/v2/spaces", token: imp}).ok(200, nil).header.Get("Content-Type"), "json") {
		t.Error("impersonated reads still answer JSON")
	}
}
