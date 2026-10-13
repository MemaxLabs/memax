package serverapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/meter"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/ratelimit"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// TestAuthChainRoundTrips holds the middleware every authenticated route
// sits behind (authChain: RequireAuth, HubContext, AuthorizeHTTP, the rate
// limit and the meter, as production wires them, with Redis) to a count of
// round trips to Postgres per credential, around a handler that does
// nothing. The handlers' own round-trip tests (TestAPIRoundTrips,
// TestMCPRoundTrips) run without the rate limiter, which once read every
// request's plan overrides twice where no test could see it.
//
// Steady state, as production runs: Redis already holds the effective
// plan, and the overrides were read within plans.OverridesTTL.
func TestAuthChainRoundTrips(t *testing.T) {
	db := testdb.Open(t, testdb.Options{})
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	secret := []byte("auth-chain-round-trips-secret-0123")
	user := uuid.New()
	exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, user, user.String()[:8]+"@chain.test")
	personal := uuid.New()
	exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, 'Personal', $2, 'personal', $3)`, personal, personal.String(), user)
	exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`, personal, user)

	key := "mxk_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(key))
	exec(`INSERT INTO api_keys (id, user_id, name, key_hash, prefix, agent_name, hub_scope_mode, hub_ids, default_permissions)
	      VALUES ($1, $2, 'test', $3, $4, 'claude-code', 'all_accessible', '{}', '{memory:read,memory:write,hub:read}')`,
		uuid.New(), user, hex.EncodeToString(sum[:]), key[:12])
	grantID := uuid.New()
	exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('chain-client', 'Chain') ON CONFLICT DO NOTHING`)
	exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name, hub_scope_mode, default_permissions)
	      VALUES ($1, $2, 'chain-client', 'claude-ai', 'all_accessible', '{memory:read,memory:write,hub:read}')`, grantID, user)
	grant, err := auth.SignGrantAccessToken(user.String(), "claude-ai", grantID.String(), secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.SignSessionToken(user.String(), auth.SurfaceWeb, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("JWT_SECRET", string(secret))
	authH, err := handler.NewAuthHandler(db.Pool)
	if err != nil {
		t.Fatal(err)
	}
	authH.SetStore(db.Store)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	registry := plans.NewRegistry(db.Store)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}
	resolver := planresolver.New(rdb, db.Store, registry)
	m := meter.New(rdb, db.Store, registry)
	m.SetResolver(resolver)
	deps := routeDeps{
		store:        db.Store,
		planRegistry: registry,
		hubResolver:  resolver,
		rateLimiter:  ratelimit.New(rdb),
		meter:        m,
		authMiddleware: handler.RequireAuth(secret, authH.ResolveAPIKey, authH.ResolveOAuthGrant,
			handler.WithStartKeys(authH.ResolveStartKey), handler.WithCredentialPrefetch(authH)),
		hubMiddleware: handler.HubContext(db.Store),
	}
	chain := authChain(deps)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	run := func(token, method, path string) *netsim.Counter {
		t.Helper()
		reqCtx, c := netsim.Track(ctx)
		// The middleware's store reads run on context.Background(): count
		// those too.
		db.Trips.SetFallback(c)
		defer db.Trips.SetFallback(nil)
		req := httptest.NewRequest(method, path, nil).WithContext(reqCtx)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d: %s", rec.Code, rec.Body.String())
		}
		// last_used goes out in a goroutine: wait for it to be counted.
		if !netsim.Settle(db.Pool, 2*time.Second) {
			t.Fatal("connections still checked out after 2 s")
		}
		return c
	}
	// Each credential, one: a person's session the hub list (with the V2
	// scope for /v2 and /mcp); an API key or an OAuth grant its row with
	// the hub list and the scope, in one pipeline (WithCredentialPrefetch).
	// They were two, the row and then the list. The rate limit and the meter (a recall is
	// metered: its limits are resolved again, and a slot reserved in
	// Redis) read nothing once Redis holds the effective plan and the
	// overrides are cached.
	budgets := []struct {
		name, token, method, path string
		budget                    int
	}{
		{"session", session, http.MethodGet, "/v2/spaces", 1},
		{"api key", key, http.MethodGet, "/v2/spaces", 1},
		{"oauth grant", grant, http.MethodGet, "/v2/spaces", 1},
		{"api key on MCP", key, http.MethodPost, "/mcp", 1},
		{"session, a metered recall", session, http.MethodPost, "/v1/recall", 1},
		{"api key, a metered recall", key, http.MethodPost, "/v1/recall", 1},
	}
	// A credential's first requests each write last_used in a goroutine,
	// until one write lands: wait for it, so none lands in the measured run.
	lastUsedSet := map[string]string{
		key:   `SELECT last_used IS NOT NULL FROM api_keys WHERE user_id = $1`,
		grant: `SELECT last_used IS NOT NULL FROM oauth_grants WHERE user_id = $1`,
	}
	for _, b := range budgets {
		for range 3 {
			run(b.token, b.method, b.path) // warm the caches and last_used
		}
		if q, ok := lastUsedSet[b.token]; ok {
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				var set bool
				if err := db.Pool.QueryRow(ctx, q, user).Scan(&set); err != nil {
					t.Fatal(err)
				}
				if set {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s: last_used never landed", b.name)
				}
			}
			run(b.token, b.method, b.path)
		}
		c := run(b.token, b.method, b.path)
		t.Logf("%s: %d round trips\n%s", b.name, c.RoundTrips(), c)
		if got := c.RoundTrips(); got > b.budget {
			t.Errorf("%s: %d round trips through the auth chain, budget %d\n%s", b.name, got, b.budget, c)
		}
	}
}
