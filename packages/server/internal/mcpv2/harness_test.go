package mcpv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/mcpv2"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/reads"
	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

const testSecret = "mcpv2-test-secret-0123456789abcdef"

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMain(m *testing.M) {
	_ = os.Setenv("JWT_SECRET", testSecret)
	os.Exit(m.Run())
}

// env is one database behind the real auth middleware and both MCP
// profiles, with V2 wired (unless built withoutV2).
type env struct {
	t      *testing.T
	pool   *pgxpool.Pool
	st     store.Store
	ledger *ledger.Ledger
	spaces *spacemode.Resolver
	srv    *httptest.Server
	reads  *recordedReads
	// recorder is the production recorder, writing v2.reads; every read
	// goes to both.
	recorder *reads.Recorder
	compile  *compile.Service
}

// tee hands every read to each recorder.
type tee []ledger.ReadRecorder

func (t tee) Record(e ledger.ReadEvent) {
	for _, r := range t {
		r.Record(e)
	}
}

// recordedReads keeps the reads handed to the recorder, for assertions on
// what each tool reports.
type recordedReads struct {
	mu    sync.Mutex
	reads []ledger.ReadEvent
}

func (r *recordedReads) Record(read ledger.ReadEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, read)
}

func (r *recordedReads) all() []ledger.ReadEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ledger.ReadEvent(nil), r.reads...)
}

func newEnv(t *testing.T) *env { return buildEnv(t, true) }

func buildEnv(t *testing.T, withV2 bool) *env {
	t.Helper()
	st, pool := testdb.Acquire(t)
	authH, err := handler.NewAuthHandler(pool)
	if err != nil {
		t.Fatalf("NewAuthHandler: %v", err)
	}
	authH.SetStore(st)
	e := &env{t: t, pool: pool, st: st, ledger: ledger.New(pool, ledger.WithLogger(quiet)), spaces: spacemode.New(pool), reads: &recordedReads{}}
	e.recorder = reads.New(e.ledger, reads.Options{Logger: quiet})
	t.Cleanup(e.recorder.Close) // before the database goes away
	recallH := handler.NewRecallHandler(st, nil, nil, nil, nil)
	agentH := handler.NewMCPHandler(st, recallH, nil, nil)
	chatH := handler.NewChatGPTMCPHandler(st, recallH, nil, nil)
	if withV2 {
		// The compile pipeline with the in-process compiler: the digest
		// serves a space's latest compile once it has one.
		e.compile = compile.New(e.ledger, &compiletest.Fake{}, mockobjectstore.New(), compile.Config{Log: quiet})
		v2h := v2api.New(e.ledger, quiet, v2api.WithCompile(e.compile))
		t.Cleanup(v2h.Wait)
		srv := mcpv2.New(mcpv2.Options{V2: v2h, Spaces: e.spaces, StateSecret: []byte(testSecret),
			AppBaseURL: "https://memax.test", Reads: tee{e.reads, e.recorder}, Logger: quiet, Compile: v2h.Compile()})
		agentH.SetV2(srv)
		chatH.SetV2(srv)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", agentH)
	mux.Handle("/mcp/chatgpt", chatH)
	chain := handler.RequireAuth([]byte(testSecret), authH.ResolveAPIKey, authH.ResolveOAuthGrant)(
		handler.HubContext(st)(handler.AuthorizeHTTP(mux)))
	e.srv = httptest.NewServer(chain)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec: %v", err)
	}
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("count: %v", err)
	}
	return n
}

// user creates a person with a personal hub.
func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	e.space(id, policy.SpacePersonal, name+"-personal")
	return id
}

type space struct {
	id   uuid.UUID
	slug string
	name string
}

func (e *env) space(owner uuid.UUID, kind policy.SpaceKind, name string) space {
	e.t.Helper()
	id := uuid.New()
	hubType := "team"
	if kind == policy.SpacePersonal {
		hubType = "personal"
	}
	slug := name + "-" + id.String()[:8]
	e.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, name, slug, hubType, owner, string(kind))
	e.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return space{id: id, slug: slug, name: name}
}

func (e *env) personal(user uuid.UUID) space {
	e.t.Helper()
	var sp space
	if err := e.pool.QueryRow(context.Background(), `SELECT id, slug, name FROM hubs WHERE owner_id = $1 AND hub_type = 'personal'`, user).
		Scan(&sp.id, &sp.slug, &sp.name); err != nil {
		e.t.Fatal(err)
	}
	return sp
}

// toV2 switches spaces to the V2 record.
func (e *env) toV2(spaces ...space) {
	e.t.Helper()
	for _, sp := range spaces {
		if err := e.spaces.Enable(context.Background(), sp.id, time.Now()); err != nil {
			e.t.Fatal(err)
		}
	}
}

// grant creates an OAuth grant for agent and its token. scope "" leaves
// the grant's scope unrecorded (a V1 grant).
func (e *env) grant(user uuid.UUID, agent, scope string, hubs ...space) (string, uuid.UUID) {
	e.t.Helper()
	id := uuid.New()
	perms := []string{"memory:read", "memory:write", "hub:read", "hub:members:read", "topic:read"}
	if scope == handler.ScopeRead {
		perms = []string{"memory:read", "hub:read", "hub:members:read", "topic:read"}
	}
	mode, hubIDs := "all_accessible", []uuid.UUID{}
	if len(hubs) > 0 {
		mode = "hub_allowlist"
		for _, h := range hubs {
			hubIDs = append(hubIDs, h.id)
		}
	}
	e.exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('test-client', 'Test') ON CONFLICT DO NOTHING`)
	e.exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name, hub_scope_mode, hub_ids, default_permissions, scope)
	        VALUES ($1, $2, 'test-client', $3, $4, $5, $6, NULLIF($7, ''))`, id, user, agent, mode, hubIDs, perms, scope)
	tok, err := auth.SignGrantAccessToken(user.String(), agent, id.String(), []byte(testSecret), time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok, id
}

// connect connects a grant's agent to spaces at a level, as a person on
// the web would (raising to write needs human_web).
func (e *env) connect(user, grantID uuid.UUID, agent ledger.AgentKind, level policy.Autonomy, spaces ...space) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		e.t.Fatal(err)
	}
	var sa []ledger.SpaceAutonomy
	for _, sp := range spaces {
		sa = append(sa, ledger.SpaceAutonomy{SpaceID: sp.id, Autonomy: level})
	}
	res, err := e.ledger.Apply(ctx, &ledger.ConnectAgent{
		Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope, Via: policy.ViaWeb,
			IdempotencyKey: uuid.NewString()},
		Credential: ledger.CredentialOAuthGrant, CredentialID: grantID, Agent: agent, Spaces: sa,
	})
	if err != nil || res.Outcome == ledger.OutcomeRefused {
		e.t.Fatalf("connect: %v %+v", err, res.Policy)
	}
	return res.Connection.ID
}

// keep writes a kept memory as the person.
func (e *env) keep(user uuid.UUID, sp space, statement string, section ledger.Section) *ledger.Memory {
	e.t.Helper()
	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		e.t.Fatal(err)
	}
	kind := ledger.KindFact
	if section == ledger.SectionDecisions {
		kind = ledger.KindDecision
	}
	res, err := e.ledger.Apply(ctx, &ledger.Remember{
		Meta:      ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope.Narrow(sp.id), Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: sp.id, Statement: statement, Section: section, Kind: kind},
	})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		e.t.Fatalf("keep: %v %+v", err, res.Policy)
	}
	return res.Memory
}

// memory reads a memory as superuser (no RLS), by display ID in a space.
func (e *env) memory(sp space, ref string) (lifecycle string, version int, statement string) {
	e.t.Helper()
	_, n, ok := ledger.ParseRef(ref)
	if !ok {
		e.t.Fatalf("bad ref %q", ref)
	}
	if err := e.pool.QueryRow(context.Background(), `
		SELECT m.lifecycle, m.current_version, COALESCE(v.statement, '')
		  FROM v2.memories m LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
		 WHERE m.space_id = $1 AND m.seq = $2`, sp.id, n).Scan(&lifecycle, &version, &statement); err != nil {
		e.t.Fatalf("memory %s: %v", ref, err)
	}
	return
}

type receiptRow struct {
	Action, ActorKind, Agent, Via, Assurance, SessionRef string
}

func (e *env) receipts(sp space, ref string) []receiptRow {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT action, actor_kind, COALESCE(agent, ''), via, COALESCE(assurance, ''), COALESCE(session_ref, '')
		  FROM v2.receipts WHERE space_id = $1 AND object_ref = $2 ORDER BY seq`, sp.id, ref)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []receiptRow
	for rows.Next() {
		var r receiptRow
		if err := rows.Scan(&r.Action, &r.ActorKind, &r.Agent, &r.Via, &r.Assurance, &r.SessionRef); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// --- Clients ---

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// elicitor answers elicitations and remembers what it was asked.
type elicitor struct {
	asked  []*mcp.ElicitParams
	answer func(*mcp.ElicitParams) *mcp.ElicitResult
}

func accept(choice, statement string) func(*mcp.ElicitParams) *mcp.ElicitResult {
	return func(*mcp.ElicitParams) *mcp.ElicitResult {
		content := map[string]any{"choice": choice}
		if statement != "" {
			content["statement"] = statement
		}
		return &mcp.ElicitResult{Action: "accept", Content: content}
	}
}

func decline(*mcp.ElicitParams) *mcp.ElicitResult { return &mcp.ElicitResult{Action: "decline"} }

// connectClient opens an MCP session with the official go-sdk client at
// a protocol version. A non-nil elicitor makes the client advertise
// elicitation.
func (e *env) connectClient(token, path, version string, el *elicitor) *mcp.ClientSession {
	e.t.Helper()
	opts := &mcp.ClientOptions{Logger: quiet}
	if el != nil {
		opts.ElicitationHandler = func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			el.asked = append(el.asked, req.Params)
			return el.answer(req.Params), nil
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "memax-test", Version: "1"}, opts)
	tr := &mcp.StreamableClientTransport{
		Endpoint:   e.srv.URL + path,
		HTTPClient: &http.Client{Transport: bearer{token: token, next: http.DefaultTransport}, Timeout: 30 * time.Second},
		MaxRetries: -1,
	}
	cs, err := client.Connect(context.Background(), tr, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		e.t.Fatalf("connect %s: %v", version, err)
	}
	e.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// structured decodes a result's structured content.
func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil || json.Unmarshal(b, &out) != nil {
		t.Fatalf("structured content %s: %v", b, err)
	}
	return out
}

// validates checks a result's structured content against the tool's
// served output schema.
func validates(t *testing.T, profile, tool string, res *mcp.CallToolResult) {
	t.Helper()
	raw, err := handler.MCPOutputSchema(profile, tool)
	if err != nil || len(raw) == 0 {
		t.Fatalf("%s has no output schema: %v", tool, err)
	}
	schemaDoc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("out.json", schemaDoc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("out.json")
	if err != nil {
		t.Fatalf("compile %s output schema: %v", tool, err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(inst); err != nil {
		t.Errorf("%s structured content doesn't match its output schema: %v\n%s", tool, err, b)
	}
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%q\ndoes not contain %q", got, w)
		}
	}
}

func refOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	out := structured[handler.MCPPushOutput](t, res)
	if _, _, ok := ledger.ParseRef(out.ID); !ok {
		t.Fatalf("push returned %q, not a display ID: %s", out.ID, text(res))
	}
	return out.ID
}

var _ = fmt.Sprintf
