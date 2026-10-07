package sessions_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// clock is a settable test clock.
type clock struct{ t atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.t.Store(time.Now().UTC().Truncate(time.Microsecond).UnixNano())
	return c
}
func (c *clock) now() time.Time          { return time.Unix(0, c.t.Load()).UTC() }
func (c *clock) advance(d time.Duration) { c.t.Add(int64(d)) }

type fixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	clock *clock
	store *sessions.Store
	user  uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, pool: pool, clock: newClock(), user: uuid.New()}
	f.store = sessions.New(pool, sessions.WithClock(f.clock.now), sessions.WithLogger(quiet))
	t.Cleanup(f.store.Wait)
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, f.user, f.user.String()[:8]+"@sessions.test")
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) issue(kind sessions.Kind) *sessions.Issued {
	f.t.Helper()
	is, err := f.store.Issue(context.Background(), sessions.Start{
		UserID: f.user, Kind: kind, Surface: "web", Client: "Chrome on macOS",
		Where: sessions.Where{IP: "203.0.113.7", City: "Lisbon", UserAgent: "Mozilla/5.0"}, TTL: 30 * 24 * time.Hour,
	})
	if err != nil {
		f.t.Fatalf("issue: %v", err)
	}
	return is
}

func (f *fixture) refresh(token string) (*sessions.Issued, error) {
	return f.store.Refresh(context.Background(), token, sessions.RefreshOptions{})
}

func (f *fixture) mustRefresh(token string) *sessions.Issued {
	f.t.Helper()
	is, err := f.refresh(token)
	if err != nil {
		f.t.Fatalf("refresh: %v", err)
	}
	return is
}

// dump is every stored row of both tables, as text.
func (f *fixture) dump() string {
	f.t.Helper()
	var b strings.Builder
	for _, q := range []string{
		`SELECT row_to_json(s)::text FROM sessions s`,
		`SELECT row_to_json(r)::text || encode(COALESCE(r.successor_sealed, ''), 'hex') FROM session_retired_tokens r`,
	} {
		rows, err := f.pool.Query(context.Background(), q)
		if err != nil {
			f.t.Fatal(err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				f.t.Fatal(err)
			}
			b.WriteString(s + "\n")
		}
		rows.Close()
	}
	return b.String()
}

func TestNoPlainTokenIsStored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.issue(sessions.KindWeb)
	second := f.mustRefresh(first.RefreshToken)
	third := f.mustRefresh(second.RefreshToken)
	stored := f.dump()
	for _, tok := range []string{first.RefreshToken, second.RefreshToken, third.RefreshToken} {
		if len(tok) != 64 {
			t.Fatalf("token %q isn't 256 bits of hex", tok)
		}
		if strings.Contains(stored, tok) {
			t.Fatalf("a refresh token is stored in plain text:\n%s", stored)
		}
	}
	var hash string
	var plain *string
	if err := f.pool.QueryRow(context.Background(), `SELECT refresh_token_hash, refresh_token FROM sessions WHERE id = $1`,
		first.Session.ID).Scan(&hash, &plain); err != nil {
		t.Fatal(err)
	}
	if hash != sessions.HashToken(third.RefreshToken) || plain != nil {
		t.Fatalf("stored hash %s (plain %v), want the SHA-256 of the current token", hash, plain)
	}
	if !strings.Contains(stored, sessions.HashToken(first.RefreshToken)) {
		t.Fatal("the first token's hash isn't kept for reuse detection")
	}
}

func TestRefreshRotates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindCLI)
	seen := map[string]bool{is.RefreshToken: true}
	tok := is.RefreshToken
	for i := 1; i <= 3; i++ {
		f.clock.advance(time.Hour)
		next := f.mustRefresh(tok)
		if seen[next.RefreshToken] || next.Replayed {
			t.Fatalf("refresh %d returned a token already used", i)
		}
		if next.Session.ID != is.Session.ID || next.Session.Generation != i {
			t.Fatalf("refresh %d: session %s generation %d", i, next.Session.ID, next.Session.Generation)
		}
		seen[next.RefreshToken] = true
		tok = next.RefreshToken
	}
	if _, err := f.refresh("not-a-token"); !errors.Is(err, sessions.ErrUnknown) {
		t.Fatalf("an unknown token: %v", err)
	}
}

func TestReuseRevokesTheWholeSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := f.issue(sessions.KindCLI) // another session of the same person
	is := f.issue(sessions.KindWeb)
	t1 := f.mustRefresh(is.RefreshToken)
	f.clock.advance(time.Hour)
	t2 := f.mustRefresh(t1.RefreshToken)

	// The first token again, long after it was retired: someone else holds
	// this session. Everything in it stops working, the newest token too.
	if _, err := f.refresh(is.RefreshToken); !errors.Is(err, sessions.ErrReused) {
		t.Fatalf("reusing a retired token: %v, want ErrReused", err)
	}
	if _, err := f.refresh(t2.RefreshToken); !errors.Is(err, sessions.ErrRevoked) {
		t.Fatalf("the newest token after reuse: %v, want ErrRevoked", err)
	}
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT revoked_reason FROM sessions WHERE id = $1`, is.Session.ID).Scan(&reason); err != nil || reason != "reuse_detected" {
		t.Fatalf("revoked_reason = %q, %v", reason, err)
	}
	// Only that session: the person's other one still refreshes.
	if _, err := f.refresh(other.RefreshToken); err != nil {
		t.Fatalf("another session after reuse: %v", err)
	}
	list, err := f.store.List(context.Background(), f.user)
	if err != nil || len(list) != 1 || list[0].ID != other.Session.ID {
		t.Fatalf("live sessions after reuse: %+v, %v", list, err)
	}
}

// TestConcurrentRefreshesShareTheNextToken: tabs, or the daemon and an MCP
// server sharing a credentials file, refresh with the same token at once.
// Every one of them succeeds, and they all end up with the same newest
// token, which keeps working; the session rotated once.
func TestConcurrentRefreshesShareTheNextToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindWeb)
	const n = 8
	var wg sync.WaitGroup
	got := make([]*sessions.Issued, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = f.refresh(is.RefreshToken)
		}()
	}
	wg.Wait()
	replays := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if got[i].RefreshToken != got[0].RefreshToken {
			t.Fatalf("racers got different tokens: %s vs %s", got[i].RefreshToken[:8], got[0].RefreshToken[:8])
		}
		if got[i].Replayed {
			replays++
		}
	}
	if replays != n-1 {
		t.Fatalf("%d of %d racers replayed; exactly one should rotate", replays, n)
	}
	var gen int
	if err := f.pool.QueryRow(context.Background(), `SELECT generation FROM sessions WHERE id = $1`, is.Session.ID).Scan(&gen); err != nil || gen != 1 {
		t.Fatalf("generation = %d, %v; want one rotation", gen, err)
	}
	f.clock.advance(time.Hour)
	if _, err := f.refresh(got[0].RefreshToken); err != nil {
		t.Fatalf("the shared token afterwards: %v", err)
	}
}

// TestGraceWalksToTheNewestToken: a retired token within the window yields
// the session's current token even when its successor was rotated too.
func TestGraceWalksToTheNewestToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindCLI)
	t1 := f.mustRefresh(is.RefreshToken)
	f.clock.advance(10 * time.Second)
	t2 := f.mustRefresh(t1.RefreshToken)
	f.clock.advance(10 * time.Second)
	again := f.mustRefresh(is.RefreshToken)
	if !again.Replayed || again.RefreshToken != t2.RefreshToken {
		t.Fatalf("a token retired 20 s ago yielded %q (replayed %v), want the newest", again.RefreshToken[:8], again.Replayed)
	}
	// Past the window from its own retirement, the same token is reuse.
	f.clock.advance(sessions.ReuseGrace)
	if _, err := f.refresh(is.RefreshToken); !errors.Is(err, sessions.ErrReused) {
		t.Fatalf("after the window: %v, want ErrReused", err)
	}
}

// TestSealsAreWipedAfterTheWindow: once the grace window passes, nothing
// in the database opens to a newer token.
func TestSealsAreWipedAfterTheWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindWeb)
	f.mustRefresh(is.RefreshToken)
	sealed := func() int {
		var n int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM session_retired_tokens WHERE successor_sealed IS NOT NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if sealed() != 1 {
		t.Fatal("the retired token has no sealed successor")
	}
	f.clock.advance(2 * time.Minute)
	f.issue(sessions.KindCLI) // any write reaps, at most once a minute
	if n := sealed(); n != 0 {
		t.Fatalf("%d seals left after the window", n)
	}
}

func TestExpiredAndRevokedSessionsDontRefresh(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindWeb)
	f.clock.advance(31 * 24 * time.Hour)
	if _, err := f.refresh(is.RefreshToken); !errors.Is(err, sessions.ErrExpired) {
		t.Fatalf("an expired session: %v", err)
	}

	g := newFixture(t)
	a := g.issue(sessions.KindWeb)
	if _, err := g.store.Revoke(context.Background(), g.user, a.Session.ID, sessions.ReasonRevoked); err != nil {
		t.Fatal(err)
	}
	if _, err := g.refresh(a.RefreshToken); !errors.Is(err, sessions.ErrRevoked) {
		t.Fatalf("a revoked session: %v", err)
	}
	if _, err := g.store.Revoke(context.Background(), g.user, a.Session.ID, sessions.ReasonRevoked); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("revoking twice: %v", err)
	}
	// Another person can't revoke it, nor see it.
	if _, err := g.store.Revoke(context.Background(), uuid.New(), g.issue(sessions.KindCLI).Session.ID, sessions.ReasonRevoked); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("someone else revoking: %v", err)
	}
}

func TestRevokeOthersAndSignOut(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	keep := f.issue(sessions.KindWeb)
	a := f.issue(sessions.KindCLI)
	b := f.issue(sessions.KindDevice)
	n, err := f.store.RevokeOthers(context.Background(), f.user, keep.Session.ID)
	if err != nil || n != 2 {
		t.Fatalf("revoked %d, %v", n, err)
	}
	for _, is := range []*sessions.Issued{a, b} {
		if _, err := f.refresh(is.RefreshToken); !errors.Is(err, sessions.ErrRevoked) {
			t.Fatalf("a revoked session refreshed: %v", err)
		}
	}
	// Signing out by refresh token, even one just retired.
	next := f.mustRefresh(keep.RefreshToken)
	ss, err := f.store.RevokeToken(context.Background(), keep.RefreshToken, sessions.ReasonSignedOut)
	if err != nil || ss == nil || ss.ID != keep.Session.ID {
		t.Fatalf("sign out by the retired token: %+v, %v", ss, err)
	}
	if _, err := f.refresh(next.RefreshToken); !errors.Is(err, sessions.ErrRevoked) {
		t.Fatalf("after sign-out: %v", err)
	}
	if ss, err := f.store.RevokeToken(context.Background(), "nothing", sessions.ReasonSignedOut); ss != nil || err != nil {
		t.Fatalf("an unknown token: %+v, %v", ss, err)
	}
}

// TestGrantSessionsEndWithTheirGrant: an MCP session whose grant was
// revoked neither lists nor refreshes, and the refresh revokes it.
func TestGrantSessionsEndWithTheirGrant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	grant := uuid.New()
	f.exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('c', 'Claude') ON CONFLICT DO NOTHING`)
	f.exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name) VALUES ($1, $2, 'c', 'claude-ai')`, grant, f.user)
	is, err := f.store.Issue(context.Background(), sessions.Start{UserID: f.user, Kind: sessions.KindMCP, AgentName: "claude-ai",
		GrantID: grant.String(), Client: "Claude", TTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := f.store.List(context.Background(), f.user); len(list) != 1 {
		t.Fatalf("the MCP session isn't listed: %+v", list)
	}
	f.exec(`UPDATE oauth_grants SET revoked_at = now() WHERE id = $1`, grant)
	if list, _ := f.store.List(context.Background(), f.user); len(list) != 0 {
		t.Fatalf("a revoked grant's session is listed: %+v", list)
	}
	accept := func(sessions.Session) error { return sessions.ErrGrantRevoked }
	if _, err := f.store.Refresh(context.Background(), is.RefreshToken, sessions.RefreshOptions{Accept: accept}); !errors.Is(err, sessions.ErrGrantRevoked) {
		t.Fatalf("refresh: %v", err)
	}
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT revoked_reason FROM sessions WHERE id = $1`, is.Session.ID).Scan(&reason); err != nil || reason != "grant_revoked" {
		t.Fatalf("revoked_reason = %q, %v", reason, err)
	}
}

// TestAnOlderServersPlainTextIsHashed: during a rolling deploy, a server
// from before migration 049 still inserts and rotates plain-text tokens.
// The database hashes them before storing, so they never land, and the
// new server refreshes them.
func TestAnOlderServersPlainTextIsHashed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.exec(`INSERT INTO sessions (user_id, refresh_token, expires_at, agent_name, grant_id, surface)
		VALUES ($1, 'old-server-token', now() + interval '1 day', '', NULL, 'web')`, f.user)
	if strings.Contains(f.dump(), "old-server-token") {
		t.Fatal("an older server's plain text was stored")
	}
	is, err := f.store.Refresh(context.Background(), "old-server-token", sessions.RefreshOptions{})
	if err != nil {
		t.Fatalf("refreshing an older server's token: %v", err)
	}
	if is.Session.Kind != sessions.KindWeb || is.Session.Surface != "web" {
		t.Fatalf("kind %s surface %s", is.Session.Kind, is.Session.Surface)
	}
	// Its rotation (UPDATE ... SET refresh_token) is hashed the same way.
	f.exec(`UPDATE sessions SET refresh_token = 'old-server-rotated' WHERE id = $1`, is.Session.ID)
	if _, err := f.store.Refresh(context.Background(), "old-server-rotated", sessions.RefreshOptions{}); err != nil {
		t.Fatalf("refreshing an older server's rotated token: %v", err)
	}
}

func TestTouchRecordsUse(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	is := f.issue(sessions.KindWeb)
	f.clock.advance(10 * time.Minute)
	f.store.Touch(is.Session.ID)
	f.store.Wait()
	got, err := f.store.Get(context.Background(), f.user, is.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastUsedAt.Equal(f.clock.now()) {
		t.Fatalf("last used %s, want %s", got.LastUsedAt, f.clock.now())
	}
	if got.CreatedIP != "203.0.113.7" || got.CreatedCity != "Lisbon" || got.Client != "Chrome on macOS" {
		t.Fatalf("where it signed in: %+v", got)
	}
}
