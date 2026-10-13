package ledger

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Cloud agents and their start keys (cloud warm-start design §4, epic
// 3.2). A cloud agent's sandbox starts its sessions with a start key: an
// API key whose one permission is compile:read, bound to a connection on
// the cloud surface, at Read, in one space. ConnectCloudAgent makes the
// connection and the key in one transaction (migration 061's
// v2.mint_start_key, beside its `connected` receipt); RekeyAgent gives the
// key a new secret under the same connection (v2.rekey_start_key, beside
// a `rekeyed` receipt). The ledger never sees a secret: the caller makes
// it and passes its SHA-256.
//
// Lock order (locks.go): a new connection locks nothing it didn't make; a
// rekey's subject is its connection, locked first (lockConnection), and it
// takes no other lock.

// StartKeyLifetime is how long a start key lasts (design decision 7).
const StartKeyLifetime = 180 * 24 * time.Hour

// ActionRekeyed is the receipt of a cloud agent's new start key.
const ActionRekeyed Action = "rekeyed"

// The cloud agent commands.
const (
	CommandConnectCloudAgent CommandName = "connect_cloud_agent"
	CommandRekeyAgent        CommandName = "rekey_agent"
)

// CloudAgentKinds are the agents a start key is made for: those whose
// cloud sessions run a committed session-start hook.
var CloudAgentKinds = []AgentKind{AgentClaudeCode, AgentCopilot, AgentCodex}

// StartKey is a start key as the ledger stores it: its id (the
// connection's credential), the SHA-256 of its secret, the prefix the key
// lists show, and when it expires. Never the secret.
type StartKey struct {
	ID        uuid.UUID
	SHA256    string
	Prefix    string
	ExpiresAt time.Time
}

var startKeyPrefix = regexp.MustCompile(`^mxk_[0-9a-f]{4}$`)

func (k StartKey) validate(now time.Time, needID bool) error {
	switch {
	case needID && k.ID == uuid.Nil:
		return invalid("key", "say which key")
	case !sha256Hex.MatchString(k.SHA256):
		return invalid("key", "send the key's SHA-256, in lowercase hex")
	case !startKeyPrefix.MatchString(k.Prefix):
		return invalid("key", "a start key's prefix is mxk_ and four hex characters")
	case !k.ExpiresAt.After(now) || k.ExpiresAt.After(now.Add(366*24*time.Hour)):
		return invalid("key", "a start key expires within a year")
	}
	return nil
}

// ConnectCloudAgent connects a cloud agent to one space with a new start
// key. Only a person, for themselves; Read unless they ask for Propose,
// which needs them on the web (policy.CodeCloudNeedsWeb). The reads it
// counts are theirs (design decision 5).
type ConnectCloudAgent struct {
	Meta
	Agent   AgentKind
	SpaceID uuid.UUID
	// Autonomy is read (the default) or propose.
	Autonomy policy.Autonomy
	Key      StartKey
}

// Name implements Command.
func (*ConnectCloudAgent) Name() CommandName { return CommandConnectCloudAgent }

// RekeyAgent gives a cloud agent's start key a new secret and a new
// expiry under the same connection; the old secret stops working at once.
type RekeyAgent struct {
	Meta
	Connection uuid.UUID
	// Key is the new key; its ID is the connection's credential.
	Key StartKey
}

// Name implements Command.
func (*RekeyAgent) Name() CommandName { return CommandRekeyAgent }

func (c *ConnectCloudAgent) validate(now time.Time) error {
	if !slicesContains(CloudAgentKinds, c.Agent) {
		return invalid("agent", "use claude-code, copilot or codex")
	}
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space")
	}
	switch c.Autonomy {
	case "":
		c.Autonomy = policy.AutonomyRead
	case policy.AutonomyRead, policy.AutonomyPropose:
	default:
		return invalid("autonomy", "a cloud agent reads or proposes; it never keeps")
	}
	return c.Key.validate(now, true)
}

func (c *RekeyAgent) validate(now time.Time) error {
	if err := validateConnectionTarget(c.Connection); err != nil {
		return err
	}
	return c.Key.validate(now, false)
}

func slicesContains(ks []AgentKind, k AgentKind) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

// CloudDisplayName is how a cloud agent's connection is shown: "Copilot
// cloud agent · acme-web".
func CloudDisplayName(agent AgentKind, space string) string {
	name := fmt.Sprintf("%s cloud agent · %s", agent.Name(), strings.TrimSpace(space))
	if r := []rune(name); len(r) > MaxDisplayName {
		name = string(r[:MaxDisplayName-1]) + "…"
	}
	return name
}

// connectCloud is ConnectCloudAgent.
func (w *writer) connectCloud(ctx context.Context, c *ConnectCloudAgent) (Result, error) {
	a := w.meta.Actor
	g, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, replay, err := w.spaceAndClaim(ctx, c.SpaceID)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	name := CloudDisplayName(c.Agent, g.Slug)
	mine := a.Kind == policy.ActorPerson && a.ID == w.meta.Scope.PersonID
	pa := w.policyActor(g)
	// The kinds of agent the person has connected, only where their plan
	// bounds them (D9): a round trip a connection without one never takes.
	var kinds int
	var newKind bool
	if pa.Plan.Enforced && pa.Plan.AgentKinds >= 0 {
		if err := w.tx.QueryRow(ctx, connectionKindsSQL, w.meta.Scope.PersonID, string(c.Agent)).Scan(&kinds, &newKind); err != nil {
			return Result{}, fmt.Errorf("ledger: count agent kinds: %w", err)
		}
	}
	dec := policy.DecideConnection(pa, policy.ConnectionConnect, policy.Connection{
		Name: name, Mine: mine, Credential: policy.CredentialAPIKey, To: c.Autonomy, Cloud: true,
		NewKind: newKind, Kinds: kinds,
	}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if sp.Kind == policy.SpacePersonal {
		return Result{}, invalid("space", "a cloud agent starts in a project's repository: choose a project or team space")
	}

	id := newID()
	rc := w.agentReceipt(sp, g, id, string(c.Agent), ActionConnected, 1)
	rc.Source = &ReceiptSource{Kind: SourceAutonomy, Ref: string(c.Autonomy)}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	person := w.meta.Scope.PersonID
	if err := execDeferred(ctx, w.tx, `
		INSERT INTO v2.agent_connections (id, tenant_id, person_id, agent, display_name, surface, credential_kind,
		                                  credential_id, state, stream_version, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $2, $3, $4, 'cloud', 'api_key', $5, 'active', 1, $6, $6)`,
		id, person, string(c.Agent), name, c.Key.ID, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write connection: %w", err)
	}
	if err := execDeferred(ctx, w.tx, `
		INSERT INTO v2.agent_connection_spaces (connection_id, space_id, tenant_id, person_id, autonomy,
		                                        created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		id, sp.ID, sp.TenantID, person, string(c.Autonomy), rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write connection space: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `SELECT v2.mint_start_key($1, $2, $3, $4, $5)`,
		id, c.Key.SHA256, c.Key.Prefix, name, c.Key.ExpiresAt); err != nil {
		return Result{}, fmt.Errorf("ledger: make start key: %w", err)
	}
	return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, id)
}

// rekey is RekeyAgent.
func (w *writer) rekey(ctx context.Context, c *RekeyAgent) (Result, error) {
	conn, err := lockConnection(ctx, w.tx, c.Connection)
	if err != nil {
		return Result{}, err
	}
	// The connection's one space (a cloud agent's), in the scope.
	var spaceID uuid.UUID
	err = w.tx.QueryRow(ctx, `
		SELECT space_id FROM v2.agent_connection_spaces
		 WHERE connection_id = $1 AND space_id = ANY($2)
		 ORDER BY space_id LIMIT 1`, conn.ID, w.meta.Scope.SpaceIDs()).Scan(&spaceID)
	if errNoRows(err) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("ledger: load connection spaces: %w", err)
	}
	g, _ := w.meta.Scope.Grant(spaceID)
	sp, replay, err := w.spaceAndClaim(ctx, spaceID)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	dec := policy.DecideConnection(w.policyActor(g), policy.ConnectionRekey, policy.Connection{
		Name: conn.DisplayName, Mine: w.mine(conn), Credential: conn.Credential.Kind.Policy(),
		Cloud: conn.Surface == SurfaceCloud && conn.Credential.Kind == CredentialAPIKey,
	}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if conn.State == ConnectionDisconnected {
		return Result{}, &ConnectionStateError{Agent: conn.DisplayName, State: conn.State, Command: CommandRekeyAgent}
	}
	rc := w.agentReceipt(sp, g, conn.ID, string(conn.Agent), ActionRekeyed, conn.streamVersion+1)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if err := execDeferred(ctx, w.tx, `
		UPDATE v2.agent_connections SET stream_version = $2, last_receipt_id = $3, updated_at = now() WHERE id = $1`,
		conn.ID, rc.StreamVersion, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update connection: %w", err)
	}
	var rekeyed bool
	if err := w.tx.QueryRow(ctx, `SELECT v2.rekey_start_key($1, $2, $3, $4)`,
		conn.ID, c.Key.SHA256, c.Key.Prefix, c.Key.ExpiresAt).Scan(&rekeyed); err != nil {
		return Result{}, fmt.Errorf("ledger: rekey start key: %w", err)
	}
	if !rekeyed {
		return Result{}, &ConnectionStateError{Agent: conn.DisplayName, State: conn.State, Command: CommandRekeyAgent,
			Detail: "its start key was revoked or deleted in Settings: disconnect it and connect it again"}
	}
	return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, conn.ID)
}
