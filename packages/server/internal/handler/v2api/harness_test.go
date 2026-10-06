package v2api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/contract"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/openapi"
)

const testSecret = "v2api-test-secret-0123456789abcdef"

var (
	// spec checks every exchange in this package against v2.yaml, and
	// remembers which operations and statuses the tests exercised.
	spec *contract.Spec
	// dbTests counts tests that reached a real database.
	dbTests atomic.Int64
	quiet   = slog.New(slog.NewTextHandler(io.Discard, nil))
)

func TestMain(m *testing.M) {
	// NewAuthHandler (the API-key and OAuth-grant resolvers) reads it.
	_ = os.Setenv("JWT_SECRET", testSecret)
	var err error
	if spec, err = contract.Load(openapi.V2); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if code == 0 && dbTests.Load() > 0 && flag.Lookup("test.run").Value.String() == "" {
		code = checkCoverage()
	}
	os.Exit(code)
}

// checkCoverage fails the run when an operation in v2.yaml was never
// answered with a success by the integration tests: an endpoint nobody
// tests is an endpoint whose contract nobody checks.
func checkCoverage() int {
	seen := spec.Exercised()
	var missing []string
	for _, op := range spec.Operations() {
		ok := false
		for _, code := range seen[op.ID] {
			ok = ok || (code >= 200 && code < 300)
		}
		if !ok {
			missing = append(missing, op.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		fmt.Fprintf(os.Stderr, "contract coverage: no test got a 2xx from %v\n", missing)
		return 1
	}
	if testing.Verbose() {
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("contract coverage: %-16s %v\n", id, seen[id])
		}
	}
	return 0
}

// env is one isolated database behind the real auth middleware and the
// /v2 handler, with every exchange checked against the spec.
type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	srv  http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, pool := testdb.Acquire(t)
	dbTests.Add(1)
	authH, err := handler.NewAuthHandler(pool)
	if err != nil {
		t.Fatalf("NewAuthHandler: %v", err)
	}
	authH.SetStore(st)
	// The chain serverapp.authChain builds, minus the Redis-backed rate
	// limiter and meter (both pass /v2 through when unset).
	chain := func(h http.Handler) http.Handler {
		return handler.RequireAuth([]byte(testSecret), authH.ResolveAPIKey, authH.ResolveOAuthGrant)(
			handler.HubContext(st)(handler.AuthorizeHTTP(h)))
	}
	mux := http.NewServeMux()
	v2api.New(ledger.New(pool, ledger.WithLogger(quiet)), quiet).Mount(mux, chain)
	return &env{t: t, pool: pool, srv: spec.Handler(t, mux)}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec: %v", err)
	}
}

func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

type space struct {
	id   uuid.UUID
	slug string
}

// space creates a hub of the given V2 kind with its owner's membership,
// as V1 does.
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
	return space{id: id, slug: slug}
}

func (e *env) join(sp space, user uuid.UUID, v1Role string) {
	e.t.Helper()
	e.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3)`, sp.id, user, v1Role)
}

// session is a signed-in person's token.
func (e *env) session(user uuid.UUID) string {
	e.t.Helper()
	tok, err := auth.SignAccessToken(user.String(), []byte(testSecret), time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}

type keyOpts struct {
	perms []string
	agent string
	hubs  []uuid.UUID // a hub allowlist; empty means every hub
	// legacyHub binds the key the old way: api_keys.hub_id set, scope
	// mode left at all_accessible.
	legacyHub *uuid.UUID
}

// apiKey creates an API key for the user and returns it with its id.
func (e *env) apiKey(user uuid.UUID, o keyOpts) (string, uuid.UUID) {
	e.t.Helper()
	if o.perms == nil {
		o.perms = []string{"memory:read", "memory:write", "hub:read"}
	}
	key := "mxk_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(key))
	id := uuid.New()
	mode := "all_accessible"
	if len(o.hubs) > 0 {
		mode = "hub_allowlist"
	}
	if o.hubs == nil {
		o.hubs = []uuid.UUID{} // the column is NOT NULL
	}
	e.exec(`INSERT INTO api_keys (id, user_id, name, key_hash, prefix, agent_name, hub_scope_mode, hub_ids, default_permissions, hub_id)
	        VALUES ($1, $2, 'test', $3, $4, $5, $6, $7, $8, $9)`,
		id, user, hex.EncodeToString(sum[:]), key[:12], o.agent, mode, o.hubs, o.perms, o.legacyHub)
	return key, id
}

// grant creates an OAuth grant for an MCP client and returns its token.
func (e *env) grant(user uuid.UUID, agent string, perms []string) (string, uuid.UUID) {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('test-client', 'Test') ON CONFLICT DO NOTHING`)
	e.exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name, hub_scope_mode, default_permissions)
	        VALUES ($1, $2, 'test-client', $3, 'all_accessible', $4)`, id, user, agent, perms)
	tok, err := auth.SignGrantAccessToken(user.String(), agent, id.String(), []byte(testSecret), time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok, id
}

// call is one request.
type call struct {
	method, path string
	token        string
	body         any // marshalled to JSON; a string is sent as is
	header       map[string]string
	// invalid marks a request that breaks the spec on purpose (a missing
	// header, an unknown field), so only its response is checked.
	invalid bool
}

type resp struct {
	t      *testing.T
	status int
	header http.Header
	body   []byte
}

func (e *env) do(c call) *resp {
	e.t.Helper()
	var body io.Reader
	switch b := c.body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(c.method, c.path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.method == http.MethodPost {
		r.Header.Set("Idempotency-Key", uuid.NewString())
	}
	for k, v := range c.header {
		if v == "" {
			r.Header.Del(k)
			continue
		}
		r.Header.Set(k, v)
	}
	if c.invalid {
		r = contract.ExpectInvalidRequest(r)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, r)
	return &resp{t: e.t, status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
}

// ok asserts the status and decodes data into v.
func (r *resp) ok(status int, v any) *resp {
	r.t.Helper()
	if r.status != status {
		r.t.Fatalf("status %d, want %d: %s", r.status, status, r.body)
	}
	if v != nil {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(r.body, &env); err != nil {
			r.t.Fatalf("decode: %v", err)
		}
		if err := json.Unmarshal(env.Data, v); err != nil {
			r.t.Fatalf("decode data: %v: %s", err, env.Data)
		}
	}
	return r
}

type apiErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details struct {
		Field  string `json:"field"`
		Policy struct {
			Effect string `json:"effect"`
			Code   string `json:"code"`
		} `json:"policy"`
		Ref             string `json:"ref"`
		ExpectedVersion int    `json:"expected_version"`
		CurrentVersion  int    `json:"current_version"`
	} `json:"details"`
}

// fails asserts an error status and code and returns the error.
func (r *resp) fails(status int, code string) apiErr {
	r.t.Helper()
	var env struct {
		Error apiErr `json:"error"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		r.t.Fatalf("decode error: %v: %s", err, r.body)
	}
	if r.status != status || env.Error.Code != code {
		r.t.Fatalf("got %d %s, want %d %s: %s", r.status, env.Error.Code, status, code, r.body)
	}
	if env.Error.Message == "" {
		r.t.Errorf("error %s has no message", code)
	}
	return env.Error
}

// The wire shapes the tests read back.
type memory struct {
	ID        uuid.UUID `json:"id"`
	Ref       string    `json:"ref"`
	SpaceID   uuid.UUID `json:"space_id"`
	Statement string    `json:"statement"`
	Section   string    `json:"section"`
	State     string    `json:"state"`
	Lifecycle string    `json:"lifecycle"`
	Flags     []string  `json:"flags"`
	Trust     string    `json:"trust"`
	Version   int       `json:"version"`
	Sources   []struct {
		Kind     string `json:"kind"`
		Ref      string `json:"ref"`
		External bool   `json:"external"`
		Trust    string `json:"trust"`
	} `json:"sources"`
}

type receipt struct {
	ID        uuid.UUID  `json:"id"`
	ObjectRef string     `json:"object_ref"`
	Action    string     `json:"action"`
	ActorKind string     `json:"actor_kind"`
	ActorID   *uuid.UUID `json:"actor_id"`
	Agent     string     `json:"agent"`
	Via       string     `json:"via"`
	Assurance string     `json:"assurance"`
	Reason    string     `json:"reason"`
}

type result struct {
	Outcome string `json:"outcome"`
	Policy  struct {
		Effect     string `json:"effect"`
		Code       string `json:"code"`
		Quarantine bool   `json:"quarantine"`
	} `json:"policy"`
	Memory   memory    `json:"memory"`
	Receipts []receipt `json:"receipts"`
}

type page[T any] struct {
	Items      []T    `json:"items"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
	Total      int    `json:"total"`
}

func remember(statement, section string) map[string]any {
	return map[string]any{"statement": statement, "section": section}
}

// remember writes a statement as the token's holder and returns the result.
func (e *env) remember(token string, sp space, statement string) result {
	e.t.Helper()
	var res result
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.id.String() + "/memories", token: token,
		body: remember(statement, "conventions")}).ok(http.StatusCreated, &res)
	return res
}

// flag sets a flag on a memory with a receipt, standing in for the judge
// and the stale checker (later epics).
func (e *env) flag(m memory, fl string) {
	e.t.Helper()
	ctx := context.Background()
	var tenant uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT tenant_id FROM hubs WHERE id = $1`, m.SpaceID).Scan(&tenant); err != nil {
		e.t.Fatal(err)
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.space_ids', $1, true), set_config('app.tenant_ids', $2, true)`,
			"{"+m.SpaceID.String()+"}", "{"+tenant.String()+"}"); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRow(ctx, `SELECT stream_version + 1 FROM v2.memories WHERE id = $1`, m.ID).Scan(&version); err != nil {
			return err
		}
		rc := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action,
			                         actor_kind, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'flagged', 'dream', 'system', now(), $4, $6)`,
			rc, tenant, m.SpaceID, m.ID, m.Ref, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE v2.memories SET flags = array_append(flags, $2), stream_version = $3, last_receipt_id = $4 WHERE id = $1`,
			m.ID, fl, version, rc)
		return err
	}(tx)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		e.t.Fatalf("flag %s: %v", m.Ref, err)
	}
}
