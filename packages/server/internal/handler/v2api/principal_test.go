package v2api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// setAutonomy sets a connection's level in a space as its person, on the
// web (through the ledger, the way the Agents screen will).
func (e *env) setAutonomy(user, conn uuid.UUID, sp space, level policy.Autonomy) {
	e.t.Helper()
	scope, err := e.ledger.UserScope(context.Background(), user)
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.ledger.Apply(context.Background(), &ledger.SetAutonomy{
		Meta:       ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		Connection: conn, SpaceID: sp.id, Autonomy: level,
	})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		e.t.Fatalf("set autonomy %s: %+v %v", level, res.Policy, err)
	}
}

// TestPrincipalResolvesThroughConnections: a credential acts as its agent
// connection, at the connection's level in the target space.
func TestPrincipalResolvesThroughConnections(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	other := e.space(zz, policy.SpaceProject, "elsewhere")
	tok, grantID := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	conn := e.connection(grantID)
	later := e.space(zz, policy.SpaceProject, "created-later") // after the connection

	write := func(sp space, statement string) *resp {
		return e.do(call{method: "POST", path: memoriesPath(sp), token: tok, body: remember(statement, "conventions"),
			header: map[string]string{"X-Memax-Via": "mcp"}})
	}
	refused := func(r *resp, code string) {
		t.Helper()
		if got := r.fails(http.StatusForbidden, "refused"); got.Details.Policy.Code != code {
			t.Errorf("policy code %q, want %q (%s)", got.Details.Policy.Code, code, got.Message)
		}
	}

	// Propose (the backfill's level) proposes, and the receipt names the
	// connection and the agent.
	var res result
	write(project, "Codex proposes.").ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || res.Policy.Code != "autonomy_propose" {
		t.Errorf("propose: %s %s", res.Outcome, res.Policy.Code)
	}
	if rc := res.Receipts[0]; rc.ActorKind != "agent" || rc.ActorID == nil || *rc.ActorID != conn || rc.Agent != "codex" || rc.Via != "mcp" {
		t.Errorf("receipt = %+v", rc)
	}
	// Write keeps; an external source is still proposed and quarantined.
	e.setAutonomy(zz, conn, project, policy.AutonomyWrite)
	write(project, "Codex keeps.").ok(http.StatusCreated, &res)
	if res.Outcome != "applied" || res.Memory.State != "kept" || res.Receipts[0].Assurance != "" {
		t.Errorf("write: %s %s %+v", res.Outcome, res.Memory.State, res.Receipts[0])
	}
	e.do(call{method: "POST", path: memoriesPath(project), token: tok, body: map[string]any{
		"statement": "A blog said so.", "section": "conventions",
		"sources": []map[string]any{{"kind": "url", "ref": "a blog", "uri": "https://example.com/post"}},
	}}).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || res.Policy.Code != "external_source" || !res.Policy.Quarantine {
		t.Errorf("write, external: %s %s quarantine %v", res.Outcome, res.Policy.Code, res.Policy.Quarantine)
	}
	// Agents still can't keep.
	refused(e.do(call{method: "POST", path: memoryPath(res.Memory, ":keep"), token: tok}), "person_must_review")
	// Read is refused, with where to change it.
	e.setAutonomy(zz, conn, project, policy.AutonomyRead)
	r := write(project, "Codex reads only.")
	refused(r, "read_only")
	// The other space keeps its own level.
	write(other, "Codex proposes elsewhere.").ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" {
		t.Errorf("other space: %s", res.Outcome)
	}
	// A space created after the connection isn't connected: reads only.
	refused(write(later, "Not connected here."), "agent_not_connected")
	e.do(call{method: "GET", path: memoriesPath(later), token: tok}).ok(http.StatusOK, nil)
}

// TestPausedAgentsOnlyRead: pause stops writes everywhere, reads go on.
func TestPausedAgentsOnlyRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	key, keyID := e.apiKey(zz, keyOpts{agent: "claude-code"})
	conn := e.connection(keyID)
	scope, _ := e.ledger.UserScope(context.Background(), zz)
	if _, err := e.ledger.Apply(context.Background(), &ledger.PauseAgent{
		Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: zz}, Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}, Connection: conn,
	}); err != nil {
		t.Fatal(err)
	}
	got := e.do(call{method: "POST", path: memoriesPath(project), token: key, body: remember("Paused.", "conventions")}).fails(http.StatusForbidden, "refused")
	if got.Details.Policy.Code != "agent_paused" || got.Message != "Claude Code is paused, so it can only read. Resume it in Agents." {
		t.Errorf("paused write: %+v %q", got.Details.Policy, got.Message)
	}
	e.do(call{method: "GET", path: memoriesPath(project), token: key}).ok(http.StatusOK, nil)
}

// TestLastSeenIsRecorded: using a credential stamps its connection's
// last_seen_at in the background, at most once a minute.
func TestLastSeenIsRecorded(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	key, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	conn := e.connection(keyID)
	lastSeen := func() *time.Time {
		var at *time.Time
		if err := e.pool.QueryRow(context.Background(), `SELECT last_seen_at FROM v2.agent_connections WHERE id = $1`, conn).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	if lastSeen() != nil {
		t.Fatal("a connection nobody used has a last-seen time")
	}
	e.do(call{method: "GET", path: memoriesPath(project), token: key}).ok(http.StatusOK, nil)
	e.h.Wait()
	first := lastSeen()
	if first == nil || time.Since(*first) > time.Minute {
		t.Fatalf("last seen = %v", first)
	}
	e.do(call{method: "GET", path: memoriesPath(project), token: key}).ok(http.StatusOK, nil)
	e.h.Wait()
	if again := lastSeen(); !again.Equal(*first) {
		t.Errorf("a second request within the minute wrote again: %v → %v", first, again)
	}
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1`, conn); n != 1 {
		t.Errorf("last seen wrote receipts: %d on the connection, want only its connected receipt", n)
	}
}
