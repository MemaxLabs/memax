package v2api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// The personal layer's hot paths over a simulated network, as the CLI and
// its daemon call them: linking a machine (the first, which writes the
// Brief too, and every one after), a daemon's poll of its machine's
// targets, and a Keep in Personal with home files on two machines. The
// wire audit checks every statement they send.
func TestLinkMachineRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	e := newEnvOn(t, db, func(*env) []v2api.Option { return nil })
	zz := e.user("zz")
	tok := e.session(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	for i := range 40 {
		e.remember(tok, personal, fmt.Sprintf("Preference %d: short paragraphs.", i))
	}
	audit.Arm()
	defer audit.Require(t)

	measure := func(name string, run func(ctx context.Context)) *netsim.Counter {
		t.Helper()
		// Past River's 100 ms fetch cooldown, so the insert notification is
		// counted, as nearly every call at alpha traffic sends it.
		time.Sleep(150 * time.Millisecond)
		ctx, c := netsim.Track(context.Background())
		// The auth middleware's store queries with context.Background().
		db.Trips.SetFallback(c)
		defer db.Trips.SetFallback(nil)
		run(ctx)
		e.h.Wait()
		if !netsim.Settle(db.Pool, 2*time.Second) {
			t.Fatalf("%s: connections still checked out after 2 s", name)
		}
		return c
	}
	link := func(ctx context.Context) wireMachineResult {
		var res wireMachineResult
		e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok, ctx: ctx,
			body: linkBody(uuid.New(), "work-mbp", "claude_user", "codex_user", "agents_md")}).ok(http.StatusCreated, &res)
		return res
	}
	check := func(name string, budget int, c *netsim.Counter) {
		t.Helper()
		t.Logf("%s: %d round trips\n%s", name, c.RoundTrips(), c)
		if c.RoundTrips() > budget {
			t.Errorf("%s: %d round trips, budget %d\n%s", name, c.RoundTrips(), budget, c)
		}
	}

	// The first link writes the space's first Brief too. Statement caches
	// are warmed in other personal spaces first, so the count is steady
	// state, as for the other budgets.
	for i := range 2 {
		other := e.space(zz, policy.SpacePersonal, fmt.Sprintf("warm-%d", i))
		measure("warm", func(ctx context.Context) {
			e.do(call{method: "POST", path: "/v2/spaces/" + other.slug + "/machines", token: tok, ctx: ctx,
				body: linkBody(uuid.New(), "warm", "claude_user", "codex_user", "agents_md")}).ok(http.StatusCreated, nil)
		})
	}
	var first wireMachineResult
	check("the first machine (+ Brief, + 3 files)", 7, measure("first link", func(ctx context.Context) { first = link(ctx) }))
	// It wrote the space's first Brief (read through the API: the audit
	// refuses any read of the record outside a scoped transaction).
	e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/brief", token: tok}).ok(http.StatusOK, nil)
	check("another machine (+ 3 files)", 7, measure("link", func(ctx context.Context) { link(ctx) }))

	poll := func(ctx context.Context) {
		e.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/targets?machine=" + first.Machine.ID.String(),
			token: tok, ctx: ctx}).ok(http.StatusOK, nil)
	}
	for range 2 {
		measure("warm", poll)
	}
	check("a daemon's poll (?machine=)", 3, measure("poll", poll))

	// A Keep in Personal bumps every machine's home files in one statement.
	var proposals []string
	for i := range 3 {
		proposals = append(proposals, e.remember(key, personal, fmt.Sprintf("Proposed by Codex: rule %d.", i)).Memory.Ref)
	}
	keep := func(ctx context.Context) {
		ref := proposals[0]
		proposals = proposals[1:]
		e.do(call{method: "POST", path: "/v2/memories/" + ref + ":keep?space=" + personal.slug, token: tok, ctx: ctx,
			header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	}
	for range 2 {
		measure("warm", keep)
	}
	check("a Keep in Personal (+ 12 home jobs)", 12, measure("keep", keep))
}
