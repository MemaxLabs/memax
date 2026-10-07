package mcpv2_test

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// A V1 user's spaces switch to V2 and back (plan 25 §10, epic 2.8), seen
// from an agent over MCP:
//
//   - before: V1 answers, exactly as a server without V2;
//   - switched: the space is served through the ledger; its V1 memories are
//     notes, out of recall and search, and memax_search include_notes finds
//     them; the agent is told once that it now proposes; with every space
//     on V2, V1's pipeline doesn't run for an unscoped read;
//   - switched back: V1 answers exactly as before (the differential), the
//     V1 pipeline runs again for unscoped reads, and the V1 rows are as
//     they were.
func TestSwitchingToV2AndBackOverMCP(t *testing.T) {
	e := newEnv(t)
	v1 := e.v1Server()
	ctx := context.Background()
	user := e.user("zz")
	personal := e.personal(user)
	team := e.space(user, policy.SpaceTeam, "acme")
	memID := e.seedV1(user, team, "The lighthouse keeper logs every ship")
	e.seedV1(user, personal, "My lighthouse notebook is green")
	tok, _ := e.grant(user, "claude-code", "")
	type tc = struct {
		name string
		args map[string]any
	}
	calls := []tc{
		{"memax_recall", map[string]any{"query": "lighthouse"}},
		{"memax_search", map[string]any{"query": "lighthouse"}},
		{"memax_get", map[string]any{"id": memID}},
		{"memax_list", map[string]any{"hub_id": team.id.String()}},
		{"memax_hubs", nil},
		{"memax_hub_members", map[string]any{"hub_id": team.id.String()}},
		{"memax_topics", map[string]any{"hub_id": team.id.String()}},
		{"memax_push", map[string]any{"content": "Pushed while on V1", "hub_id": team.id.String(), "hub_reason": "shared"}},
		{"memax_request_decision", map[string]any{"question": "Which way?", "options": []string{"a", "b"}, "space_id": team.id.String()}},
	}
	differential := func(when string) {
		t.Helper()
		withV2 := e.connectClient(tok, "/mcp", older, nil)
		wired := e.srv
		e.srv = v1
		plain := e.connectClient(tok, "/mcp", older, nil)
		e.srv = wired
		for _, c := range calls {
			ja, _ := json.Marshal(call(t, withV2, c.name, c.args))
			jb, _ := json.Marshal(call(t, plain, c.name, c.args))
			// IDs a write mints are masked, and so are V1's scores past four
			// digits: they decay with the clock between the two calls.
			mask := func(b []byte) string {
				return scoreRE.ReplaceAllString(uuidRE.ReplaceAllString(string(b), "<id>"), "$1")
			}
			if ma, mb := mask(ja), mask(jb); ma != mb {
				t.Errorf("%s: %s differs with V2 wired:\n  v2: %s\n  v1: %s", when, c.name, ma, mb)
			}
		}
	}
	v1Rows := func() string {
		var h string
		if err := e.pool.QueryRow(ctx, `SELECT md5(string_agg(m::text, '|' ORDER BY m.id)) FROM memories m WHERE hub_id IN ($1, $2)`,
			team.id, personal.id).Scan(&h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	differential("before the switch")
	before := v1Rows()

	// The owner switches both spaces (the API's :switch, run in-process).
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	owner := ledger.Actor{Kind: policy.ActorPerson, ID: user}
	for _, sp := range []space{team, personal} {
		st, err := e.ledger.StartSwitchInline(ctx, owner, policy.ViaWeb, scope, sp.id, ledger.SwitchOptions{Key: "s-" + sp.slug})
		if err != nil || st.State != ledger.SwitchStateSwitched {
			t.Fatalf("switch %s: %v %+v", sp.name, err, st)
		}
	}
	if v1Rows() != before {
		t.Error("the switch changed V1 rows")
	}

	cs := e.connectClient(tok, "/mcp", modern, nil)
	e.metered.take()
	// Recall: nothing kept yet, no V1 notes, V1's pipeline not run (every
	// reachable space is on V2), and the notice, once.
	res := call(t, cs, "memax_recall", map[string]any{"query": "lighthouse"})
	got := text(res)
	if strings.Contains(got, "lighthouse keeper") || strings.Contains(got, "not on V2 yet") {
		t.Errorf("recall served V1 notes from spaces on V2:\n%s", got)
	}
	mustContain(t, got, "moved to Memax V2", "is now proposed")
	if events, ops := e.metered.take(); len(events)+len(ops) > 0 {
		t.Errorf("V1's pipeline ran with every space on V2: logged %v, charged %v", events, ops)
	}
	notices, _ := res.Meta[mcpv2MetaNotices].([]any)
	if len(notices) != 2 {
		t.Errorf("notices = %v", res.Meta)
	}
	if got := text(call(t, cs, "memax_recall", map[string]any{"query": "lighthouse"})); strings.Contains(got, "moved to Memax V2") {
		t.Errorf("the notice came twice:\n%s", got)
	}
	// memax_search: kept memories only, unless notes are asked for.
	if got := text(call(t, cs, "memax_search", map[string]any{"query": "lighthouse"})); strings.Contains(got, "lighthouse keeper") {
		t.Errorf("search served a note without include_notes:\n%s", got)
	}
	res = call(t, cs, "memax_search", map[string]any{"query": "lighthouse", "include_notes": true})
	validates(t, "agent", "memax_search", res)
	out := structured[handler.MCPSearchOutput](t, res)
	var refs []string
	for _, it := range out.Results {
		if it.Record == handler.MCPRecordNote {
			refs = append(refs, it.Ref)
		}
	}
	if len(refs) != 2 || !strings.HasPrefix(refs[0], "N-") {
		t.Errorf("notes found = %v\n%s", refs, text(res))
	}
	mustContain(t, text(res), "Notes (V1 memories, never kept context):", "The lighthouse keeper logs every ship")
	// The agent now proposes in the space.
	push := structured[handler.MCPPushOutput](t, call(t, cs, "memax_push", map[string]any{
		"content": "Ships are logged by the keeper on duty.", "hub_id": team.id.String()}))
	if push.Status != handler.MCPPushProposed {
		t.Errorf("push = %+v", push)
	}

	// Switch back: V1 again, exactly.
	for _, sp := range []space{team, personal} {
		st, err := e.ledger.SwitchBack(ctx, owner, policy.ViaWeb, scope, sp.id, "back-"+sp.slug)
		if err != nil || st.State != ledger.SwitchStateOff {
			t.Fatalf("switch back %s: %v %+v", sp.name, err, st)
		}
	}
	if v1Rows() != before {
		t.Error("switching back left V1 rows changed")
	}
	cs = e.connectClient(tok, "/mcp", modern, nil)
	e.metered.take()
	got = text(call(t, cs, "memax_recall", map[string]any{"query": "lighthouse"}))
	mustContain(t, got, "The lighthouse keeper logs every ship")
	if events, ops := e.metered.take(); !slices.Contains(events, "recall mcp") || !slices.Contains(ops, "recall") {
		t.Errorf("after switching back, V1's recall didn't run: logged %v, charged %v", events, ops)
	}
	// Each pass asks V1 for a decision card on each server, and V1 holds at
	// most three open: clear the board so the second pass compares answers,
	// not what the first pass left.
	e.exec(`DELETE FROM board_slots`)
	differential("after switching back")
	if e.count(`SELECT count(*) FROM memories WHERE id = $1`, uuid.MustParse(memID)) != 1 {
		t.Error("the V1 memory is gone")
	}
}

// scoreRE keeps a score's first four decimals.
var scoreRE = regexp.MustCompile(`("score":[0-9]+\.[0-9]{1,4})[0-9]*`)

// mcpv2MetaNotices is mcpv2.MetaNotices, spelled out.
const mcpv2MetaNotices = "app.memax/notices"
