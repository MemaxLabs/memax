package ledger_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// apiKey inserts a V1 API key for user and returns its id.
func (f *fixture) apiKey(user uuid.UUID, agent string, perms []string, hubs ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	if perms == nil {
		perms = []string{"memory:read", "memory:write"}
	}
	id := uuid.New()
	sum := sha256.Sum256([]byte(id.String()))
	mode := "all_accessible"
	if len(hubs) > 0 {
		mode = "hub_allowlist"
	}
	if hubs == nil {
		hubs = []uuid.UUID{}
	}
	f.exec(`INSERT INTO api_keys (id, user_id, name, key_hash, prefix, agent_name, hub_scope_mode, hub_ids, default_permissions)
	        VALUES ($1, $2, 'ci-deploy', $3, 'mxk_test', $4, $5, $6, $7)`,
		id, user, hex.EncodeToString(sum[:]), agent, mode, hubs, perms)
	return id
}

// grant inserts a V1 OAuth grant for user and returns its id.
func (f *fixture) grant(user uuid.UUID, agent string, hubs ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('test-client', 'Claude') ON CONFLICT DO NOTHING`)
	mode := "all_accessible"
	if len(hubs) > 0 {
		mode = "hub_allowlist"
	}
	if hubs == nil {
		hubs = []uuid.UUID{}
	}
	f.exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name, hub_scope_mode, hub_ids, default_permissions)
	        VALUES ($1, $2, 'test-client', $3, $4, $5, '{memory:read,memory:write}')`, id, user, agent, mode, hubs)
	return id
}

// connect connects a credential as its person, on the web.
func (f *fixture) connect(user uuid.UUID, kind ledger.CredentialKind, cred uuid.UUID, agent ledger.AgentKind, spaces ...ledger.SpaceAutonomy) *ledger.Connection {
	f.t.Helper()
	res := f.apply(&ledger.ConnectAgent{
		Meta: meta(person(user), f.scope(user), policy.ViaWeb), Credential: kind, CredentialID: cred, Agent: agent, Spaces: spaces,
	})
	if res.Outcome != ledger.OutcomeApplied || res.Connection == nil {
		f.t.Fatalf("connect: %s %s (%s)", res.Outcome, res.Policy.Code, res.Policy.Message)
	}
	return res.Connection
}

func at(space uuid.UUID, level policy.Autonomy) ledger.SpaceAutonomy {
	return ledger.SpaceAutonomy{SpaceID: space, Autonomy: level}
}

// agentActor is the connection acting, the way the /v2 principal builds
// it: the person's scope with the connection's levels.
func (f *fixture) agentActor(user uuid.UUID, c *ledger.Connection, limit policy.Autonomy) (ledger.Actor, ledger.Scope) {
	f.t.Helper()
	got, err := f.l.ConnectionForCredential(context.Background(), f.scope(user), c.Credential.Kind, c.Credential.ID)
	if err != nil {
		f.t.Fatalf("ConnectionForCredential: %v", err)
	}
	return ledger.Actor{Kind: policy.ActorAgent, ID: c.ID, Name: c.DisplayName, Agent: string(c.Agent),
		Credential: c.Credential.Kind.Policy()}, f.scope(user).WithConnection(got, limit)
}

func refusedWith(t *testing.T, res ledger.Result, code string) {
	t.Helper()
	if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != code {
		t.Errorf("outcome %s %s (%s), want refused %s", res.Outcome, res.Policy.Code, res.Policy.Message, code)
	}
}

func TestConnectAgent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	cautious := f.space(zz, policy.SpaceProject, "cautious")
	f.setRules(cautious, `{"new_agent_autonomy": "read"}`)
	theirs := f.space(jy, policy.SpaceProject, "side-project")
	grant := f.grant(zz, "claude-code")

	cmd := func(via policy.Via, spaces ...ledger.SpaceAutonomy) *ledger.ConnectAgent {
		return &ledger.ConnectAgent{Meta: meta(person(zz), f.scope(zz), via), Credential: ledger.CredentialOAuthGrant,
			CredentialID: grant, Agent: ledger.AgentClaudeCode, Spaces: spaces}
	}
	// From a terminal, at most the space's default.
	refusedWith(t, f.apply(cmd(policy.ViaCLI, at(project, policy.AutonomyWrite))), policy.CodeAutonomyNeedsWeb)
	refusedWith(t, f.apply(cmd(policy.ViaCLI, at(cautious, policy.AutonomyPropose))), policy.CodeAutonomyNeedsWeb)
	// A space outside the scope is not found.
	if _, err := f.l.Apply(ctx, cmd(policy.ViaWeb, at(theirs, ""))); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("connect to someone else's space: %v", err)
	}

	c := cmd(policy.ViaCLI, at(project, ""), at(cautious, ""), at(personal, policy.AutonomyRead))
	res := f.apply(c)
	conn := res.Connection
	if res.Outcome != ledger.OutcomeApplied || conn == nil {
		t.Fatalf("connect: %+v", res)
	}
	if conn.Agent != ledger.AgentClaudeCode || conn.DisplayName != "Claude Code" || conn.Surface != ledger.SurfaceCLI ||
		conn.State != ledger.ConnectionActive || conn.PersonID != zz || !conn.Credential.Active ||
		conn.MaxAutonomy != policy.AutonomyWrite || conn.ConnectedByKind != policy.ActorPerson || *conn.ConnectedBy != zz {
		t.Errorf("connection = %+v", conn)
	}
	levels := map[uuid.UUID]policy.Autonomy{}
	for _, s := range conn.Spaces {
		levels[s.SpaceID] = s.Autonomy
	}
	want := map[uuid.UUID]policy.Autonomy{project: policy.AutonomyPropose, cautious: policy.AutonomyRead, personal: policy.AutonomyRead}
	if len(levels) != 3 || levels[project] != want[project] || levels[cautious] != want[cautious] || levels[personal] != want[personal] {
		t.Errorf("levels = %v, want %v (the space's default, or what was asked)", levels, want)
	}
	if conn.Spaces[0].Kind != policy.SpacePersonal {
		t.Errorf("the personal space comes first: %+v", conn.Spaces)
	}
	// One receipt per space, about the agent, naming the level.
	if len(res.Receipts) != 3 {
		t.Fatalf("receipts = %+v", res.Receipts)
	}
	for i, rc := range res.Receipts {
		if rc.ObjectKind != "agent" || rc.ObjectID != conn.ID || rc.ObjectRef != "claude-code" || rc.Action != ledger.ActionConnected ||
			rc.ActorKind != policy.ActorPerson || *rc.ActorID != zz || rc.Via != policy.ViaCLI ||
			rc.Assurance != policy.AssuranceClientAttested || rc.StreamID != conn.ID || rc.StreamVersion != i+1 ||
			rc.Source == nil || rc.Source.Kind != ledger.SourceAutonomy || rc.Source.Ref != string(levels[rc.SpaceID]) {
			t.Errorf("receipt %d = %+v source %+v", i, rc, rc.Source)
		}
	}

	// A retry is a replay; a new key is "already connected".
	again, err := f.l.Apply(ctx, c)
	if err != nil || !again.Replayed || again.Connection == nil || again.Connection.ID != conn.ID || len(again.Receipts) != 3 {
		t.Errorf("replay: %+v %v", again, err)
	}
	if _, err := f.l.Apply(ctx, cmd(policy.ViaWeb, at(project, ""))); !errors.Is(err, ledger.ErrAlreadyConnected) {
		t.Errorf("connect twice: %v", err)
	}

	// Credentials that aren't the person's, or don't work, are not found.
	jyKey := f.apiKey(jy, "codex", nil)
	revoked := f.apiKey(zz, "codex", nil)
	f.exec(`UPDATE api_keys SET revoked_at = now() WHERE id = $1`, revoked)
	expired := f.grant(zz, "codex")
	f.exec(`UPDATE oauth_grants SET expires_at = now() - interval '1 hour' WHERE id = $1`, expired)
	for name, c := range map[string]*ledger.ConnectAgent{
		"someone else's key": {Credential: ledger.CredentialAPIKey, CredentialID: jyKey},
		"a revoked key":      {Credential: ledger.CredentialAPIKey, CredentialID: revoked},
		"an expired grant":   {Credential: ledger.CredentialOAuthGrant, CredentialID: expired},
		"a key as a grant":   {Credential: ledger.CredentialOAuthGrant, CredentialID: jyKey},
		"no such credential": {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New()},
	} {
		c.Meta, c.Agent, c.Spaces = meta(person(zz), f.scope(zz), policy.ViaWeb), ledger.AgentCodex, []ledger.SpaceAutonomy{at(project, "")}
		if _, err := f.l.Apply(ctx, c); !errors.Is(err, ledger.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
	}

	// An API key's agent proposes at most; a read-only key reads.
	key := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "codex", nil), ledger.AgentCodex, at(project, ""))
	readKey := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "", []string{"memory:read"}), ledger.AgentOther, at(project, ""))
	if key.MaxAutonomy != policy.AutonomyPropose || readKey.MaxAutonomy != policy.AutonomyRead || readKey.DisplayName != "Agent" {
		t.Errorf("max autonomy: key %s, read-only key %s %q", key.MaxAutonomy, readKey.MaxAutonomy, readKey.DisplayName)
	}
	refusedWith(t, f.apply(&ledger.ConnectAgent{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Credential: ledger.CredentialAPIKey,
		CredentialID: f.apiKey(zz, "codex", nil), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, policy.AutonomyWrite)}}),
		policy.CodeKeyMaxPropose)

	// Agents can't connect agents.
	agent, agentScope := f.agentActor(zz, key, policy.AutonomyWrite)
	refusedWith(t, f.apply(&ledger.ConnectAgent{Meta: meta(agent, agentScope, policy.ViaMCP), Person: zz, Credential: ledger.CredentialAPIKey,
		CredentialID: f.apiKey(zz, "codex", nil), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, "")}}),
		policy.CodePersonMustManage)

	// Malformed commands.
	for name, c := range map[string]*ledger.ConnectAgent{
		"no spaces":      {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: ledger.AgentCodex},
		"twice a space":  {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, ""), at(project, "")}},
		"unknown agent":  {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: "hal", Spaces: []ledger.SpaceAutonomy{at(project, "")}},
		"bad level":      {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, "admin")}},
		"bad credential": {Credential: "password", CredentialID: uuid.New(), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, "")}},
		"bad client id":  {Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: ledger.AgentCodex, ClientID: "http://x", Spaces: []ledger.SpaceAutonomy{at(project, "")}},
		"someone else":   {Person: jy, Credential: ledger.CredentialAPIKey, CredentialID: uuid.New(), Agent: ledger.AgentCodex, Spaces: []ledger.SpaceAutonomy{at(project, "")}},
	} {
		c.Meta = meta(person(zz), f.scope(zz), policy.ViaWeb)
		if _, err := f.l.Apply(ctx, c); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

func TestSetAutonomy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(jy, policy.SpaceTeam, "acme")
	f.join(team, zz, "contributor")
	conn := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(project, ""))

	set := func(actor ledger.Actor, scope ledger.Scope, via policy.Via, space uuid.UUID, level policy.Autonomy) (ledger.Result, error) {
		return f.l.Apply(ctx, &ledger.SetAutonomy{Meta: meta(actor, scope, via), Connection: conn.ID, SpaceID: space, Autonomy: level})
	}
	mustSet := func(actor ledger.Actor, scope ledger.Scope, via policy.Via, space uuid.UUID, level policy.Autonomy) ledger.Result {
		t.Helper()
		res, err := set(actor, scope, via, space, level)
		if err != nil {
			t.Fatalf("set %s: %v", level, err)
		}
		return res
	}

	// Raising from the CLI is refused: an agent with the person's login
	// can't raise itself.
	refusedWith(t, mustSet(person(zz), f.scope(zz), policy.ViaCLI, project, policy.AutonomyWrite), policy.CodeAutonomyNeedsWeb)
	res := mustSet(person(zz), f.scope(zz), policy.ViaWeb, project, policy.AutonomyWrite)
	if lvl, _ := res.Connection.AutonomyIn(project); lvl != policy.AutonomyWrite || len(res.Receipts) != 1 {
		t.Fatalf("raise on the web: %s %+v", lvl, res.Receipts)
	}
	rc := res.Receipts[0]
	if rc.Action != ledger.ActionAutonomyChanged || rc.Assurance != policy.AssuranceHumanWeb || rc.Source.Ref != "write" || rc.StreamVersion != 2 {
		t.Errorf("raise receipt = %+v %+v", rc, rc.Source)
	}
	// The same level writes nothing.
	same := mustSet(person(zz), f.scope(zz), policy.ViaCLI, project, policy.AutonomyWrite)
	if same.Outcome != ledger.OutcomeApplied || len(same.Receipts) != 0 {
		t.Errorf("same level: %+v", same)
	}
	// Lowering works from anywhere.
	res = mustSet(person(zz), f.scope(zz), policy.ViaCLI, project, policy.AutonomyRead)
	if lvl, _ := res.Connection.AutonomyIn(project); lvl != policy.AutonomyRead || res.Receipts[0].StreamVersion != 3 {
		t.Errorf("lower: %s %+v", lvl, res.Receipts)
	}
	// Setting a level where it isn't connected connects it there, quietly
	// at the default from a terminal.
	res = mustSet(person(zz), f.scope(zz), policy.ViaCLI, team, policy.AutonomyPropose)
	if lvl, ok := res.Connection.AutonomyIn(team); !ok || lvl != policy.AutonomyPropose || res.Receipts[0].Action != ledger.ActionConnected {
		t.Errorf("connect to the team space: %s %v %+v", lvl, ok, res.Receipts)
	}

	// The team's owner can lower zz's agent in the team space, not raise it,
	// and not touch it elsewhere.
	jyScope := f.scope(jy)
	refusedWith(t, mustSet(person(jy), jyScope, policy.ViaWeb, team, policy.AutonomyWrite), policy.CodeNotYourAgent)
	res = mustSet(person(jy), jyScope, policy.ViaCLI, team, policy.AutonomyRead)
	if lvl, _ := res.Connection.AutonomyIn(team); lvl != policy.AutonomyRead || *res.Receipts[0].ActorID != jy {
		t.Errorf("owner lowers: %s %+v", lvl, res.Receipts)
	}
	if _, err := set(person(jy), jyScope, policy.ViaWeb, project, policy.AutonomyRead); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("owner of another space: %v", err)
	}
	// What jy sees of zz's connection is the team space only.
	if len(res.Connection.Spaces) != 1 || res.Connection.Spaces[0].SpaceID != team {
		t.Errorf("jy sees %+v", res.Connection.Spaces)
	}

	// Agents never change autonomy, their own included.
	agent, agentScope := f.agentActor(zz, conn, policy.AutonomyWrite)
	refusedWith(t, mustSet(agent, agentScope, policy.ViaMCP, project, policy.AutonomyWrite), policy.CodePersonMustManage)
	refusedWith(t, mustSet(agent, agentScope, policy.ViaMCP, project, policy.AutonomyRead), policy.CodePersonMustManage)

	// An idempotent retry.
	key := uuid.NewString()
	cmd := &ledger.SetAutonomy{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Connection: conn.ID, SpaceID: project, Autonomy: policy.AutonomyPropose}
	cmd.IdempotencyKey = key
	first := f.apply(cmd)
	second := f.apply(cmd)
	if !second.Replayed || len(second.Receipts) != 1 || second.Receipts[0].ID != first.Receipts[0].ID {
		t.Errorf("replay: %+v", second)
	}

	// Unknown connections and spaces are not found.
	if _, err := f.l.Apply(ctx, &ledger.SetAutonomy{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Connection: uuid.New(), SpaceID: project, Autonomy: policy.AutonomyRead}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("unknown connection: %v", err)
	}
	if _, err := set(person(zz), f.scope(zz), policy.ViaWeb, uuid.New(), policy.AutonomyRead); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("unknown space: %v", err)
	}
	if _, err := set(person(zz), f.scope(zz), policy.ViaWeb, project, "root"); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("unknown level: %v", err)
	}
}

func TestPauseResumeDisconnect(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(project, jy, "contributor")
	grant := f.grant(zz, "codex")
	conn := f.connect(zz, ledger.CredentialOAuthGrant, grant, ledger.AgentCodex, at(project, ""), at(personal, ""))
	keyID := f.apiKey(zz, "claude-code", nil)
	keyConn := f.connect(zz, ledger.CredentialAPIKey, keyID, ledger.AgentClaudeCode, at(project, ""))

	do := func(cmd ledger.Command) (ledger.Result, error) { return f.l.Apply(ctx, cmd) }
	as := func(user uuid.UUID, via policy.Via) ledger.Meta { return meta(person(user), f.scope(user), via) }

	// Only its person pauses it.
	res, err := do(&ledger.PauseAgent{Meta: as(jy, policy.ViaWeb), Connection: conn.ID})
	if err != nil {
		t.Fatal(err)
	}
	refusedWith(t, res, policy.CodeNotYourAgent)
	res, err = do(&ledger.PauseAgent{Meta: as(zz, policy.ViaCLI), Connection: conn.ID})
	if err != nil || res.Connection.State != ledger.ConnectionPaused || len(res.Receipts) != 2 {
		t.Fatalf("pause: %+v %v", res, err)
	}
	var spaces []uuid.UUID
	for _, rc := range res.Receipts {
		spaces = append(spaces, rc.SpaceID)
		if rc.Action != ledger.ActionPaused || rc.ObjectID != conn.ID {
			t.Errorf("pause receipt %+v", rc)
		}
	}
	if !slices.Contains(spaces, project) || !slices.Contains(spaces, personal) {
		t.Errorf("a pause is receipted in every space it is connected to: %v", spaces)
	}
	// A paused agent only reads, everywhere.
	agent, agentScope := f.agentActor(zz, conn, policy.AutonomyWrite)
	refusedWith(t, f.apply(&ledger.Propose{Meta: meta(agent, agentScope, policy.ViaMCP), NewMemory: fact(project, "Paused agents can't write.")}), policy.CodeAgentPaused)
	if _, err := do(&ledger.PauseAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("pause twice: %v", err)
	}
	// Resuming is raising: it needs the web.
	res, _ = do(&ledger.ResumeAgent{Meta: as(zz, policy.ViaCLI), Connection: conn.ID})
	refusedWith(t, res, policy.CodeAutonomyNeedsWeb)
	res, err = do(&ledger.ResumeAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID})
	if err != nil || res.Connection.State != ledger.ConnectionActive || res.Receipts[0].Assurance != policy.AssuranceHumanWeb {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if _, err := do(&ledger.ResumeAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("resume an active agent: %v", err)
	}

	// Disconnect revokes the credential in the same command.
	revokedAt := func(table string, id uuid.UUID) *time.Time {
		var at *time.Time
		if err := f.pool.QueryRow(ctx, `SELECT revoked_at FROM `+table+` WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	res, _ = do(&ledger.DisconnectAgent{Meta: as(jy, policy.ViaWeb), Connection: conn.ID})
	refusedWith(t, res, policy.CodeNotYourAgent)
	if revokedAt("oauth_grants", grant) != nil {
		t.Fatal("a refused disconnect revoked the grant")
	}
	res, err = do(&ledger.DisconnectAgent{Meta: as(zz, policy.ViaCLI), Connection: conn.ID})
	if err != nil || res.Connection.State != ledger.ConnectionDisconnected || res.Connection.DisconnectedAt == nil ||
		res.Connection.Credential.Active || res.Receipts[0].Action != ledger.ActionDisconnected {
		t.Fatalf("disconnect: %+v %v", res, err)
	}
	if revokedAt("oauth_grants", grant) == nil {
		t.Error("disconnect left the OAuth grant active")
	}
	res = f.apply(&ledger.DisconnectAgent{Meta: as(zz, policy.ViaWeb), Connection: keyConn.ID})
	if revokedAt("api_keys", keyID) == nil || res.Connection.Credential.Active {
		t.Error("disconnect left the API key active")
	}
	// Disconnected is terminal.
	for _, cmd := range []ledger.Command{
		&ledger.PauseAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID},
		&ledger.ResumeAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID},
		&ledger.DisconnectAgent{Meta: as(zz, policy.ViaWeb), Connection: conn.ID},
		&ledger.SetAutonomy{Meta: as(zz, policy.ViaWeb), Connection: conn.ID, SpaceID: project, Autonomy: policy.AutonomyRead},
	} {
		_, err := do(cmd)
		var se *ledger.ConnectionStateError
		if !errors.Is(err, ledger.ErrInvalidTransition) || !errors.As(err, &se) || !strings.Contains(err.Error(), "disconnected") {
			t.Errorf("%s on a disconnected agent: %v", cmd.Name(), err)
		}
	}
	// A disconnected agent's scope makes it read-only even if its
	// credential somehow still worked.
	agent, agentScope = f.agentActor(zz, conn, policy.AutonomyWrite)
	refusedWith(t, f.apply(&ledger.Propose{Meta: meta(agent, agentScope, policy.ViaMCP), NewMemory: fact(project, "Gone.")}), policy.CodeAgentNotConnected)
	// The stream is gap-free: connected ×2, paused ×2, resumed ×2, disconnected ×2.
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1`, conn.ID); n != 8 {
		t.Errorf("receipts on the stream = %d, want 8", n)
	}
	if n := f.count(`SELECT max(stream_version) FROM v2.receipts WHERE stream_id = $1`, conn.ID); n != 8 {
		t.Errorf("stream version = %d, want 8", n)
	}
}

// The policy rules of plan 25 §5.6, through connections: autonomy comes
// from the connection, per space.
func TestConnectionAutonomyDrivesWrites(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	other := f.space(zz, policy.SpaceProject, "elsewhere")
	conn := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(project, policy.AutonomyWrite))

	propose := func(space uuid.UUID, limit policy.Autonomy, sources ...ledger.SourceInput) ledger.Result {
		t.Helper()
		agent, scope := f.agentActor(zz, conn, limit)
		nm := fact(space, "An agent writes "+uuid.NewString()[:8]+".")
		nm.Sources = sources
		return f.apply(&ledger.Propose{Meta: meta(agent, scope, policy.ViaMCP), NewMemory: nm})
	}
	setLevel := func(level policy.Autonomy) {
		f.apply(&ledger.SetAutonomy{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Connection: conn.ID, SpaceID: project, Autonomy: level})
	}

	if res := propose(project, policy.AutonomyWrite); res.Outcome != ledger.OutcomeApplied || res.Receipts[0].ActorID == nil || *res.Receipts[0].ActorID != conn.ID {
		t.Errorf("write agent: %s %+v", res.Outcome, res.Receipts)
	}
	ext := ledger.SourceInput{Kind: ledger.SourceURL, Ref: "a blog", URI: "https://example.com"}
	if res := propose(project, policy.AutonomyWrite, ext); res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeExternalSource || !res.Policy.Quarantine {
		t.Errorf("write agent, external source: %s %s", res.Outcome, res.Policy.Code)
	}
	// The credential's limit caps the connection.
	if res := propose(project, policy.AutonomyRead); res.Outcome != ledger.OutcomeRefused || res.Policy.Code != policy.CodeReadOnly {
		t.Errorf("write connection, read-only credential: %s %s", res.Outcome, res.Policy.Code)
	}
	setLevel(policy.AutonomyPropose)
	if res := propose(project, policy.AutonomyWrite); res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeAutonomyPropose {
		t.Errorf("propose agent: %s %s", res.Outcome, res.Policy.Code)
	}
	setLevel(policy.AutonomyRead)
	if res := propose(project, policy.AutonomyWrite); res.Outcome != ledger.OutcomeRefused || res.Policy.Code != policy.CodeReadOnly ||
		!strings.Contains(res.Policy.Message, "Codex is read-only in memax-v2") {
		t.Errorf("read agent: %s %s %q", res.Outcome, res.Policy.Code, res.Policy.Message)
	}
	// A space it isn't connected to: read-only, with a message that says so.
	if res := propose(other, policy.AutonomyWrite); res.Outcome != ledger.OutcomeRefused || res.Policy.Code != policy.CodeAgentNotConnected ||
		!strings.Contains(res.Policy.Message, "isn't connected to elsewhere") {
		t.Errorf("unconnected space: %s %s %q", res.Outcome, res.Policy.Code, res.Policy.Message)
	}
	// No connection at all.
	agent := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Cursor", Credential: policy.CredentialOAuth, Autonomy: policy.AutonomyWrite}
	scope := f.scope(zz).WithConnection(nil, policy.AutonomyWrite)
	refusedWith(t, f.apply(&ledger.Propose{Meta: meta(agent, scope, policy.ViaMCP), NewMemory: fact(project, "Unconnected.")}), policy.CodeAgentNotConnected)
}

func TestListAndGetConnections(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy, out := f.user("zz"), f.user("jy"), f.user("out")
	f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	private := f.space(zz, policy.SpaceProject, "private")
	f.join(project, jy, "contributor")
	f.space(jy, policy.SpacePersonal, "Personal")
	codex := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(project, policy.AutonomyWrite), at(private, ""))
	cursor := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "cursor", nil), ledger.AgentCursor, at(private, ""))
	gone := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "gemini", nil), ledger.AgentGeminiCLI, at(project, ""))
	f.apply(&ledger.DisconnectAgent{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Connection: gone.ID})
	jyAgent := f.connect(jy, ledger.CredentialOAuthGrant, f.grant(jy, "claude-code"), ledger.AgentClaudeCode, at(project, ""))

	// Codex writes: two kept, one proposed and rejected, one waiting, in sessions.
	agent, scope := f.agentActor(zz, codex, policy.AutonomyWrite)
	write := func(space uuid.UUID, session string, external bool) *ledger.Memory {
		m := meta(agent, scope, policy.ViaMCP)
		m.SessionRef = session
		nm := fact(space, "Codex learned "+uuid.NewString()[:8]+".")
		if external {
			nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceIssue, Ref: "#12"}}
		}
		return f.apply(&ledger.Propose{Meta: m, NewMemory: nm}).Memory
	}
	write(project, "cx-1", false)
	write(project, "cx-1", false)
	rejected := write(project, "cx-2", true)
	write(private, "cx-2", true)
	f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: rejected.ID.String()})

	ids := func(cs []ledger.Connection) []uuid.UUID {
		out := make([]uuid.UUID, len(cs))
		for i, c := range cs {
			out[i] = c.ID
		}
		return out
	}
	mine, err := f.l.ListConnections(ctx, f.scope(zz), ledger.ConnectionQuery{})
	if err != nil || !slices.Equal(ids(mine), []uuid.UUID{codex.ID, cursor.ID}) {
		t.Fatalf("zz's connections = %v %v", ids(mine), err)
	}
	if mine[0].WritesWeek != 4 || len(mine[0].Spaces) != 2 {
		t.Errorf("codex: writes %d spaces %+v", mine[0].WritesWeek, mine[0].Spaces)
	}
	for _, s := range mine[0].Spaces {
		if (s.SpaceID == project && (s.WritesWeek != 3 || s.Slug == "" || s.Name != "memax-v2")) || (s.SpaceID == private && s.WritesWeek != 1) {
			t.Errorf("codex in %s: %+v", s.Name, s)
		}
	}
	all, _ := f.l.ListConnections(ctx, f.scope(zz), ledger.ConnectionQuery{IncludeDisconnected: true})
	if len(all) != 3 {
		t.Errorf("with disconnected: %v", ids(all))
	}
	// A space lists everyone's agents there.
	inProject, err := f.l.ListConnections(ctx, f.scope(zz), ledger.ConnectionQuery{SpaceID: project})
	if err != nil || !slices.Equal(ids(inProject), []uuid.UUID{codex.ID, jyAgent.ID}) {
		t.Errorf("agents in memax-v2 = %v %v", ids(inProject), err)
	}
	// jy sees zz's Codex in the shared space, and only that space of it.
	jySees, _ := f.l.ListConnections(ctx, f.scope(jy), ledger.ConnectionQuery{SpaceID: project})
	for _, c := range jySees {
		if c.ID == codex.ID && (len(c.Spaces) != 1 || c.Spaces[0].SpaceID != project || c.WritesWeek != 3) {
			t.Errorf("jy sees codex as %+v", c)
		}
	}
	if _, err := f.l.GetConnection(ctx, f.scope(jy), cursor.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("jy reads zz's cursor (no shared space): %v", err)
	}
	if _, err := f.l.ListConnections(ctx, f.scope(jy), ledger.ConnectionQuery{SpaceID: private}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("jy lists a space outside their scope: %v", err)
	}
	if got, err := f.l.ListConnections(ctx, f.scope(out), ledger.ConnectionQuery{}); err != nil || len(got) != 0 {
		t.Errorf("an outsider's connections: %v %v", got, err)
	}

	d, err := f.l.GetConnection(ctx, f.scope(zz), codex.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := d.Week
	if w.Writes != 4 || w.Proposals != 2 || w.Kept != 2 || w.Rejected != 1 || w.Waiting != 1 || w.Reads != 0 {
		t.Errorf("week = %+v", w)
	}
	if len(d.RecentWrites) != 4 || d.RecentWrites[0].ObjectKind != "memory" || d.RecentWrites[0].Seq < d.RecentWrites[3].Seq {
		t.Errorf("recent writes = %+v", d.RecentWrites)
	}
	if len(d.Sessions) != 2 || d.Sessions[0].SessionRef != "cx-2" || d.Sessions[0].Writes != 2 || d.Sessions[1].Writes != 2 {
		t.Errorf("sessions = %+v", d.Sessions)
	}
	// The other connection's detail counts none of Codex's work.
	if d, err := f.l.GetConnection(ctx, f.scope(zz), cursor.ID); err != nil || d.Week.Writes != 0 || len(d.RecentWrites) != 0 {
		t.Errorf("cursor detail = %+v %v", d, err)
	}
}

func TestTouchConnection(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	conn := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(project, ""))
	receipts := f.count(`SELECT count(*) FROM v2.receipts`)

	seen := func() *time.Time {
		c, err := f.l.ConnectionForCredential(ctx, f.scope(zz), conn.Credential.Kind, conn.Credential.ID)
		if err != nil {
			t.Fatal(err)
		}
		return c.LastSeenAt
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := f.l.TouchConnection(ctx, zz, conn.ID, now); err != nil {
		t.Fatal(err)
	}
	if got := seen(); got == nil || !got.Equal(now) {
		t.Errorf("last seen = %v, want %v", got, now)
	}
	// Never backwards, never someone else's, and no receipt.
	if err := f.l.TouchConnection(ctx, zz, conn.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.l.TouchConnection(ctx, uuid.New(), conn.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := seen(); !got.Equal(now) {
		t.Errorf("last seen moved to %v", got)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts`); n != receipts {
		t.Errorf("touching wrote %d receipts", n-receipts)
	}
}

// The database enforces the rules for agent tables as it does for
// memories: receipts, the state machine, isolation and grants.
func TestAgentTablesAreGuarded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	jySpace := f.space(jy, policy.SpaceProject, "side")
	conn := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(project, ""))
	jyConn := f.connect(jy, ledger.CredentialOAuthGrant, f.grant(jy, "codex"), ledger.AgentCodex, at(jySpace, ""))
	tenant := zz // a project space's tenant is its owner

	// asPerson runs fn as memax_v2 with zz's scope (person + the space).
	asPerson := func(fn func(tx pgx.Tx) error) error {
		return f.asV2([]uuid.UUID{project}, []uuid.UUID{tenant}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.person_id', $1, true)`, zz.String()); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	receipt := func(tx pgx.Tx, object uuid.UUID, action string, version int) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'agent', $4, 'codex', $5, 'person', $6, 'web', now(), $4, $7)`,
			id, tenant, project, object, action, zz, version)
		return id, err
	}

	cases := []struct {
		name string
		fn   func(tx pgx.Tx) error
		want string
	}{
		{"pause without a receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.agent_connections SET state = 'paused' WHERE id = $1`, conn.ID)
			return err
		}, "MXR01"},
		{"raise autonomy without a receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.agent_connection_spaces SET autonomy = 'write' WHERE connection_id = $1`, conn.ID)
			return err
		}, "MXR01"},
		{"raise autonomy with a receipt about a memory", func(tx pgx.Tx) error {
			id := uuid.Must(uuid.NewV7())
			if _, err := tx.Exec(ctx, `
				INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
				VALUES ($1, $2, $3, 'memory', $4, 'M-0001', 'edited', 'memax', 'system', now(), $4, 99)`, id, tenant, project, conn.ID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE v2.agent_connection_spaces SET autonomy = 'write', last_receipt_id = $2 WHERE connection_id = $1`, conn.ID, id)
			return err
		}, "MXR01"},
		{"raise autonomy with a receipt commits", func(tx pgx.Tx) error {
			rid, err := receipt(tx, conn.ID, "autonomy_changed", 2)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE v2.agent_connections SET stream_version = 2, last_receipt_id = $2 WHERE id = $1`, conn.ID, rid); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.agent_connection_spaces SET autonomy = 'write', last_receipt_id = $2 WHERE connection_id = $1`, conn.ID, rid)
			return err
		}, ""},
		{"last seen needs no receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.agent_connections SET last_seen_at = now() WHERE id = $1`, conn.ID)
			return err
		}, ""},
	}
	for _, c := range cases {
		var stmtErr error
		err := asPerson(func(tx pgx.Tx) error {
			stmtErr = c.fn(tx)
			return stmtErr
		})
		if stmtErr != nil {
			t.Errorf("%s: the statement failed (%v); the check must be deferred to commit", c.name, stmtErr)
			continue
		}
		if got := sqlstate(err); got != c.want {
			t.Errorf("%s: commit %v (SQLSTATE %q), want %q", c.name, err, got, c.want)
		}
	}

	// Disconnected is terminal in SQL too, receipt or not.
	err := asPerson(func(tx pgx.Tx) error {
		rid, err := receipt(tx, conn.ID, "disconnected", 3)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v2.agent_connections SET state = 'disconnected', disconnected_at = now(), stream_version = 3, last_receipt_id = $2 WHERE id = $1`, conn.ID, rid)
		return err
	})
	if err != nil {
		t.Fatalf("disconnect in SQL: %v", err)
	}
	err = asPerson(func(tx pgx.Tx) error {
		rid, err := receipt(tx, conn.ID, "resumed", 4)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v2.agent_connections SET state = 'active', disconnected_at = NULL, stream_version = 4, last_receipt_id = $2 WHERE id = $1`, conn.ID, rid)
		return err
	})
	if sqlstate(err) != "MXL02" {
		t.Errorf("reviving a disconnected connection: %v, want MXL02", err)
	}

	// Revoking a credential needs a disconnect receipt in the transaction.
	err = asPerson(func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT v2.revoke_agent_credential($1)`, conn.ID)
		return err
	})
	if sqlstate(err) != "MXR01" {
		t.Errorf("revoke without a receipt: %v, want MXR01", err)
	}

	// The credential check answers only about the person's own credentials.
	var access *string
	if err := asPerson(func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT v2.agent_credential_access('oauth_grant', $1, $2)`, jyConn.Credential.ID, zz).Scan(&access)
	}); err != nil || access != nil {
		t.Errorf("credential access for someone else's grant: %v %v", access, err)
	}

	// Isolation: jy's connection is invisible to zz; writes outside the
	// scope are refused; no scope sees nothing.
	countAs := func(person uuid.UUID, spaces []uuid.UUID, table string) int {
		var n int
		if err := f.asV2(spaces, nil, func(tx pgx.Tx) error {
			if person != uuid.Nil {
				if _, err := tx.Exec(ctx, `SELECT set_config('app.person_id', $1, true)`, person.String()); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, tbl := range []string{"v2.agent_connections", "v2.agent_connection_spaces"} {
		if n := countAs(uuid.Nil, nil, tbl); n != 0 {
			t.Errorf("%s: no scope sees %d rows", tbl, n)
		}
		if n := countAs(zz, nil, tbl); n != 1 {
			t.Errorf("%s: zz as a person sees %d rows, want their own 1", tbl, n)
		}
		if n := countAs(uuid.Nil, []uuid.UUID{jySpace}, tbl); n != 1 {
			t.Errorf("%s: jy's space sees %d rows, want 1", tbl, n)
		}
	}
	err = asPerson(func(tx pgx.Tx) error {
		rid, err := receipt(tx, jyConn.ID, "autonomy_changed", 9)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v2.agent_connection_spaces (connection_id, space_id, tenant_id, person_id, autonomy, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, 'write', $5, $5)`, jyConn.ID, jySpace, jy, jy, rid)
		return err
	})
	if sqlstate(err) != "42501" {
		t.Errorf("zz attaching jy's agent to jy's space: %v, want an RLS violation", err)
	}

	// Identity columns can't change and nothing can be deleted.
	for _, stmt := range []string{
		`UPDATE v2.agent_connections SET credential_id = gen_random_uuid()`,
		`UPDATE v2.agent_connections SET person_id = gen_random_uuid()`,
		`UPDATE v2.agent_connections SET agent = 'cursor'`,
		`UPDATE v2.agent_connection_spaces SET space_id = gen_random_uuid()`,
		`DELETE FROM v2.agent_connections`,
		`DELETE FROM v2.agent_connection_spaces`,
		`UPDATE public.api_keys SET revoked_at = now()`,
		`UPDATE public.oauth_grants SET revoked_at = now()`,
	} {
		if err := asPerson(func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err }); sqlstate(err) != "42501" {
			t.Errorf("%s: %v, want permission denied", stmt, err)
		}
	}
}

// The vocabularies are defined in Go and in SQL; they must agree.
func TestAgentVocabularyMatchesSQL(t *testing.T) {
	t.Parallel()
	sql, err := os.ReadFile("../../migrations/029_v2_agent_connections.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	check := func(constraint string) []string {
		m := regexp.MustCompile(`(?s)CONSTRAINT ` + constraint + ` CHECK \(\w+ IN\s*\((.*?)\)\)`).FindSubmatch(sql)
		if m == nil {
			t.Fatalf("no %s in migration 029", constraint)
		}
		var out []string
		for _, q := range regexp.MustCompile(`'([a-z_-]+)'`).FindAllSubmatch(m[1], -1) {
			out = append(out, string(q[1]))
		}
		sort.Strings(out)
		return out
	}
	sorted := func(vs []string) []string { sort.Strings(vs); return vs }
	asStrings := func(vs any) []string {
		var out []string
		switch v := vs.(type) {
		case []ledger.AgentKind:
			for _, x := range v {
				out = append(out, string(x))
			}
		case []ledger.AgentSurface:
			for _, x := range v {
				out = append(out, string(x))
			}
		case []ledger.CredentialKind:
			for _, x := range v {
				out = append(out, string(x))
			}
		case []ledger.ConnectionState:
			for _, x := range v {
				out = append(out, string(x))
			}
		case []policy.Autonomy:
			for _, x := range v {
				out = append(out, string(x))
			}
		}
		return sorted(out)
	}
	for _, c := range []struct {
		constraint string
		code       []string
	}{
		{"agent_connections_agent_check", asStrings(ledger.AgentKinds)},
		{"agent_connections_surface_check", asStrings(ledger.AgentSurfaces)},
		{"agent_connections_credential_kind_check", asStrings(ledger.CredentialKinds)},
		{"agent_connections_state_check", asStrings(ledger.ConnectionStates)},
		{"agent_connection_spaces_autonomy_check", asStrings(policy.Autonomies)},
	} {
		if got := check(c.constraint); !slices.Equal(got, c.code) {
			t.Errorf("%s: SQL %v, Go %v", c.constraint, got, c.code)
		}
	}

	f := newFixture(t)
	from := append([]ledger.ConnectionState{""}, ledger.ConnectionStates...)
	for _, a := range from {
		for _, b := range ledger.ConnectionStates {
			var sqlFrom any = string(a)
			if a == "" {
				sqlFrom = nil
			}
			var got bool
			if err := f.pool.QueryRow(context.Background(), `SELECT v2.agent_state_transition_allowed($1::text, $2)`, sqlFrom, string(b)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := ledger.ConnectionTransitionAllowed(a, b); got != want {
				t.Errorf("state %q → %q: SQL %v, Go %v", a, b, got, want)
			}
		}
	}
}

func TestAgentFromV1(t *testing.T) {
	t.Parallel()
	cases := map[string]ledger.AgentKind{
		"claude-code": ledger.AgentClaudeCode, "Claude Code": ledger.AgentClaudeCode, "claude-ai": ledger.AgentClaude,
		"codex": ledger.AgentCodex, "cursor": ledger.AgentCursor, "windsurf": ledger.AgentWindsurf, "gemini": ledger.AgentGeminiCLI,
		"copilot": ledger.AgentCopilot, "vscode": ledger.AgentCopilot, "opencode": ledger.AgentOpenCode, "chatgpt": ledger.AgentChatGPT,
		"openclaw": ledger.AgentOther, "hermes": ledger.AgentOther, "": ledger.AgentOther,
	}
	for in, want := range cases {
		if got := ledger.AgentFromV1(in); got != want {
			t.Errorf("AgentFromV1(%q) = %s, want %s", in, got, want)
		}
	}
	for _, k := range ledger.AgentKinds {
		if !k.Surface().Valid() || k.Name() == "" {
			t.Errorf("%s: surface %q name %q", k, k.Surface(), k.Name())
		}
	}
}

func TestBackfillConnections(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy, other := f.user("zz"), f.user("jy"), f.user("other")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(jy, policy.SpaceTeam, "acme")
	f.join(team, zz, "viewer")
	cautious := f.space(zz, policy.SpaceProject, "cautious")
	f.setRules(cautious, `{"new_agent_autonomy": "read"}`)
	left := f.space(jy, policy.SpaceProject, "left")
	f.space(other, policy.SpacePersonal, "Personal")

	key := f.apiKey(zz, "claude-code", nil)
	readKey := f.apiKey(zz, "", []string{"memory:read"})
	scoped := f.apiKey(zz, "cursor", nil, project)
	stranded := f.apiKey(zz, "codex", nil, left) // bound to a hub zz isn't in
	grant := f.grant(zz, "claude-ai")
	revoked := f.apiKey(zz, "codex", nil)
	f.exec(`UPDATE api_keys SET revoked_at = now() WHERE id = $1`, revoked)
	expired := f.grant(zz, "chatgpt")
	f.exec(`UPDATE oauth_grants SET expires_at = now() - interval '1 day' WHERE id = $1`, expired)
	otherKey := f.apiKey(other, "codex", nil)

	report, err := f.l.BackfillConnections(ctx, ledger.BackfillOptions{Users: []uuid.UUID{zz}})
	if err != nil {
		t.Fatal(err)
	}
	if report != (ledger.BackfillReport{Connected: 4, NoSpaces: 1}) {
		t.Errorf("report = %+v", report)
	}
	conns, err := f.l.ListConnections(ctx, f.scope(zz), ledger.ConnectionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	byCred := map[uuid.UUID]ledger.Connection{}
	for _, c := range conns {
		byCred[c.Credential.ID] = c
	}
	levels := func(cred uuid.UUID) map[uuid.UUID]policy.Autonomy {
		out := map[uuid.UUID]policy.Autonomy{}
		for _, s := range byCred[cred].Spaces {
			out[s.SpaceID] = s.Autonomy
		}
		return out
	}
	everywhere := map[uuid.UUID]policy.Autonomy{personal: "propose", project: "propose", team: "propose", cautious: "read"}
	if got := levels(key); fmt.Sprint(got) != fmt.Sprint(everywhere) {
		t.Errorf("key levels = %v, want %v (Propose, or the space's lower default)", got, everywhere)
	}
	if got := levels(grant); fmt.Sprint(got) != fmt.Sprint(everywhere) || byCred[grant].Agent != ledger.AgentClaude || byCred[grant].Surface != ledger.SurfaceChat {
		t.Errorf("grant = %+v", byCred[grant])
	}
	for sp, lvl := range levels(readKey) {
		if lvl != policy.AutonomyRead {
			t.Errorf("a read-only key got %s in %s", lvl, sp)
		}
	}
	if got := levels(scoped); len(got) != 1 || got[project] != policy.AutonomyPropose {
		t.Errorf("a hub-scoped key is connected only to its hub: %v", got)
	}
	for _, cred := range []uuid.UUID{stranded, revoked, expired, otherKey} {
		if _, ok := byCred[cred]; ok {
			t.Errorf("credential %s was connected", cred)
		}
	}
	c := byCred[key]
	if c.ConnectedByKind != policy.ActorMemax || c.ConnectedBy != nil || c.DisplayName != "Claude Code" {
		t.Errorf("backfilled connection = %+v", c)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1 AND actor_kind = 'memax' AND reason = 'Connected from a V1 credential.'`, c.ID); n != 4 {
		t.Errorf("backfill receipts = %d, want one per space", n)
	}

	// Idempotent: a second run connects nothing new.
	again, err := f.l.BackfillConnections(ctx, ledger.BackfillOptions{Users: []uuid.UUID{zz}})
	if err != nil || again != (ledger.BackfillReport{AlreadyConnected: 4, NoSpaces: 1}) {
		t.Errorf("second run = %+v %v", again, err)
	}
	// Everyone, now: other's key too.
	all, err := f.l.BackfillConnections(ctx, ledger.BackfillOptions{})
	if err != nil || all.Connected != 1 || all.AlreadyConnected != 4 {
		t.Errorf("everyone = %+v %v", all, err)
	}
}
