package mcpv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// v1Server is a second server on the same database with V2 not wired: the
// MCP server exactly as V1 users have it.
func (e *env) v1Server() *httptest.Server {
	e.t.Helper()
	authH, err := handler.NewAuthHandler(e.pool)
	if err != nil {
		e.t.Fatal(err)
	}
	authH.SetStore(e.st)
	recallH := handler.NewRecallHandler(e.st, nil, nil, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler.NewMCPHandler(e.st, recallH, nil, nil))
	mux.Handle("/mcp/chatgpt", handler.NewChatGPTMCPHandler(e.st, recallH, nil, nil))
	srv := httptest.NewServer(handler.RequireAuth([]byte(testSecret), authH.ResolveAPIKey, authH.ResolveOAuthGrant)(
		handler.HubContext(e.st)(handler.AuthorizeHTTP(mux))))
	e.t.Cleanup(srv.Close)
	return srv
}

// seedV1 writes a recallable V1 memory (a memory and its chunk).
func (e *env) seedV1(owner uuid.UUID, hub space, content string) string {
	e.t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	e.exec(`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash, kind, stability, state, created_at, updated_at)
	        VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'text/plain', $6, $7, $8, 'active', $9, $9)`,
		id, hub.id, owner, content[:min(30, len(content))], content, uuid.NewString(), model.MemoryKindSemantic, model.MemoryStabilityEvolving, now)
	e.exec(`INSERT INTO chunks (id, memory_id, content, chunk_index, token_count, created_at, kind, stability, retrieval_weight, hint, tags_text, metadata_text, project_repo, language, search_config, heading_chain)
	        VALUES ($1::uuid, $2::uuid, $3, 0, 10, $4, $5, $6, 1.0, '', '', '', '', 'und', 'simple', '{}'::text[])`,
		uuid.NewString(), id, content, now, model.MemoryKindSemantic, model.MemoryStabilityEvolving)
	return id
}

var uuidRE = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// TestV1SpacesBehaveExactlyAsV1 drives every tool through the server with
// V2 wired and through one without it, on the same data, and requires the
// same answer whenever the call stays in V1 spaces: for a user with no
// space on V2 (every tool), and for one with a space on V2 when the call
// names a V1 space. IDs a write mints are masked.
func TestV1SpacesBehaveExactlyAsV1(t *testing.T) {
	e := newEnv(t)
	v1 := e.v1Server()

	run := func(t *testing.T, token string, calls []struct {
		name string
		args map[string]any
	}) {
		withV2 := e.connectClient(token, "/mcp", older, nil)
		wired := e.srv
		e.srv = v1
		plain := e.connectClient(token, "/mcp", older, nil)
		e.srv = wired
		for _, c := range calls {
			a := call(t, withV2, c.name, c.args)
			b := call(t, plain, c.name, c.args)
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			ma, mb := uuidRE.ReplaceAllString(string(ja), "<id>"), uuidRE.ReplaceAllString(string(jb), "<id>")
			if ma != mb {
				t.Errorf("%s differs with V2 wired:\n  v2: %s\n  v1: %s", c.name, ma, mb)
			}
		}
	}
	type tc = struct {
		name string
		args map[string]any
	}

	t.Run("no space on V2", func(t *testing.T) {
		user := e.user("v1only")
		team := e.space(user, policy.SpaceTeam, "team")
		memID := e.seedV1(user, team, "The lighthouse keeper logs every ship")
		tok, _ := e.grant(user, "claude-code", "")
		run(t, tok, []tc{
			{"memax_recall", map[string]any{"query": "lighthouse"}},
			{"memax_search", map[string]any{"query": "lighthouse"}},
			{"memax_get", map[string]any{"id": memID}},
			{"memax_list", map[string]any{}},
			{"memax_list", map[string]any{"hub_id": team.id.String()}},
			{"memax_hubs", nil},
			{"memax_hub_members", map[string]any{"hub_id": team.id.String()}},
			{"memax_topics", map[string]any{"hub_id": team.id.String()}},
			{"memax_push", map[string]any{"content": "V1 pushes stay V1", "hub_id": "personal"}},
			{"memax_push", map[string]any{"content": "Team push", "hub_id": team.id.String(), "hub_reason": "shared"}},
			{"memax_capture", map[string]any{"summary": "A V1 session"}},
			{"memax_request_decision", map[string]any{"question": "Which way?", "options": []string{"a", "b"}}},
			{"memax_forget", map[string]any{"id": uuid.NewString()}},
		})
	})

	t.Run("a space on V2 elsewhere", func(t *testing.T) {
		user := e.user("mixed")
		v1team := e.space(user, policy.SpaceTeam, "still-v1")
		onV2 := e.space(user, policy.SpaceProject, "on-v2")
		e.toV2(onV2)
		memID := e.seedV1(user, v1team, "The harbour master signs every manifest")
		tok, grant := e.grant(user, "claude-code", "memax:read memax:propose")
		e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, onV2)
		run(t, tok, []tc{
			{"memax_get", map[string]any{"id": memID}},
			{"memax_list", map[string]any{"hub_id": v1team.id.String()}},
			{"memax_hub_members", map[string]any{"hub_id": v1team.id.String()}},
			{"memax_topics", map[string]any{"hub_id": v1team.id.String()}},
			{"memax_push", map[string]any{"content": "Still a V1 push", "hub_id": v1team.id.String(), "hub_reason": "shared"}},
			{"memax_forget", map[string]any{"id": memID}},
		})
	})
}

// Recall across both records: kept memories from spaces on V2, V1 results
// from V1 hubs, and never the V1 memories (notes) inside a space on V2.
func TestMixedRecall(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	v1team := e.space(user, policy.SpaceTeam, "v1-team")
	onV2 := e.space(user, policy.SpaceProject, "on-v2")
	e.toV2(onV2)
	e.seedV1(user, v1team, "Lighthouse rotation happens every night in the v1 hub")
	e.seedV1(user, onV2, "Lighthouse note inside the space that moved to V2")
	e.keep(user, onV2, "Lighthouse lamps are checked every morning", ledger.SectionConventions)
	tok, grant := e.grant(user, "claude-code", "memax:read memax:propose")
	e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, onV2)
	cs := e.connectClient(tok, "/mcp", modern, nil)
	res := call(t, cs, "memax_recall", map[string]any{"query": "lighthouse", "limit": 10})
	got := text(res)
	mustContain(t, got, "Lighthouse lamps are checked every morning", "From spaces not on V2 yet:", "rotation happens every night")
	if strings.Contains(got, "note inside the space") {
		t.Errorf("recall served a note from a space on V2:\n%s", got)
	}
	validates(t, "agent", "memax_recall", res)
	out := structured[handler.MCPRecallOutput](t, res)
	records := map[string]int{}
	for _, it := range out.Results {
		records[it.Record]++
	}
	if records["v2"] != 1 || records["v1"] != 1 {
		t.Errorf("results by record = %v", records)
	}
	if len(e.reads.reads) == 0 || len(e.reads.reads[len(e.reads.reads)-1].Memories[onV2.id]) != 1 {
		t.Errorf("the read wasn't recorded: %+v", e.reads.reads)
	}
}

// The ChatGPT profile's names map onto the same V2 handlers.
func TestChatGPTProfileOnV2(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	e.keep(f.user, f.sp, "The connector reads kept memories", ledger.SectionConventions)
	cs := e.connectClient(f.token, "/mcp/chatgpt", legacy, nil)
	res := call(t, cs, "save_memory", map[string]any{"content": "ChatGPT proposes too", "hub_id": f.sp.id.String()})
	if out := structured[handler.MCPPushOutput](t, res); out.Status != handler.MCPPushProposed {
		t.Errorf("save_memory = %+v", out)
	}
	validates(t, "chatgpt", "save_memory", res)
	res = call(t, cs, "search_memories", map[string]any{"query": "connector"})
	mustContain(t, text(res), "The connector reads kept memories")
	validates(t, "chatgpt", "search_memories", res)
	res = call(t, cs, "list_topics", map[string]any{"hub_id": f.sp.slug})
	mustContain(t, text(res), "## Sections of memax-v2", "**Conventions** (1 kept)")
}

// Recall's V2 part holds its 250 ms budget; on a seeded corpus of a few
// thousand kept memories, a whole tool call over HTTP stays well under the
// 300 ms p95 of N2.
func TestRecallLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("latency: skipped in -short")
	}
	e, f := newFixture(t, policy.AutonomyPropose)
	const n = 3000
	seedKept(t, e, f.sp, n)
	cs := e.connectClient(f.token, "/mcp", modern, nil)
	queries := []string{"deploy target", "postgres migrations", "review queue", "rate limits", "lighthouse", "fly machines",
		"session ref", "compile budget", "staging database", "token audience"}
	var took []time.Duration
	for i := range 60 {
		q := queries[i%len(queries)]
		start := time.Now()
		// The space on V2 alone: the V1 pipeline that serves V1 hubs in a
		// mixed recall is V1's, with V1's latency, and isn't N2's.
		res := call(t, cs, "memax_recall", map[string]any{"query": q, "limit": 10, "session_ref": "s1", "hub_id": f.sp.id.String()})
		took = append(took, time.Since(start))
		if res.IsError {
			t.Fatalf("recall: %s", text(res))
		}
		if out := structured[handler.MCPRecallOutput](t, res); out.Partial {
			t.Errorf("recall %q ran out of its budget", q)
		}
	}
	digestStart := time.Now()
	call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})
	digest := time.Since(digestStart)
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	p50, p95 := took[len(took)/2], took[len(took)*95/100]
	t.Logf("recall over %d kept memories: p50 %v, p95 %v, max %v; digest %v", n, p50, p95, took[len(took)-1], digest)
	if p95 > 100*time.Millisecond {
		t.Errorf("recall p95 %v, want well under 300 ms", p95)
	}
}

// A misspelt query still finds the memory: the trigram lane runs when
// full-text search finds too little.
func TestRecallToleratesTypos(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	e.keep(f.user, f.sp, "Lighthouse lamps are checked every morning", ledger.SectionConventions)
	cs := e.connectClient(f.token, "/mcp", modern, nil)
	for _, q := range []string{"lighthouse", "lighthose", "lamp check"} {
		res := call(t, cs, "memax_search", map[string]any{"query": q, "space_id": f.sp.id.String()})
		mustContain(t, text(res), "Lighthouse lamps")
		validates(t, "agent", "memax_search", res)
	}
	res := call(t, cs, "memax_search", map[string]any{"query": "submarine", "space_id": f.sp.id.String()})
	if text(res) != "No results found." {
		t.Errorf("unrelated query: %s", text(res))
	}
}

// seedKept writes n kept memories in one transaction, receipts first, the
// way the ledger does (the receipt trigger checks it).
func seedKept(t *testing.T, e *env, sp space, n int) {
	t.Helper()
	ctx := context.Background()
	words := strings.Fields("deploy target postgres migrations review queue rate limits lighthouse fly machines session ref compile " +
		"budget staging database token audience agent proposal kept memory receipt space brief section convention decision " +
		"preference question harbour manifest release friday freeze vendor docs cache redis neon region worker river job")
	var tenant uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT tenant_id FROM hubs WHERE id = $1`, sp.id).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rng := rand.New(rand.NewPCG(1, 2))
	sections := []string{"decisions", "conventions", "preferences", "open_question"}
	for i := range n {
		var b strings.Builder
		for j := range 12 {
			if j > 0 {
				b.WriteString(" ")
			}
			b.WriteString(words[rng.IntN(len(words))])
		}
		statement := fmt.Sprintf("%s (%d)", b.String(), i)
		id, rc := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		section := sections[i%4]
		kind := "fact"
		if section == "decisions" {
			kind = "decision"
		}
		ref := ledger.FormatRef(ledger.PrefixMemory, int64(i+1))
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, assurance, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'kept', 'person', $6, 'web', 'human_web', now(), $4, 1)`,
			rc, tenant, sp.id, id, ref, tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.memories (id, tenant_id, space_id, seq, section, kind, lifecycle, trust, search, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, 'kept', 'person', to_tsvector('simple', public.immutable_unaccent(lower($7))), $8, $8)`,
			id, tenant, sp.id, i+1, section, kind, statement, rc); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
			VALUES ($1, 1, $2, $3, $4, $4)`, id, sp.id, statement, rc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v2.id_counters (tenant_id, prefix, next) VALUES ($1, 'M', $2)
		ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = EXCLUDED.next`, tenant, n+1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `ANALYZE v2.memories, v2.memory_versions`); err != nil {
		t.Fatal(err)
	}
}
