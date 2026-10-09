package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The agent commands. Each one, like the memory commands, claims its
// idempotency key, asks policy (policy.DecideConnection), checks the state
// machine, and writes a receipt in every space it changes before touching
// the projections; the database refuses the commit otherwise (migration
// 029). Receipts about a connection have object_kind "agent", its id as
// object_id and stream, and the agent's slug as object_ref.

// connect is ConnectAgent.
func (w *writer) connect(ctx context.Context, c *ConnectAgent) (Result, error) {
	a := w.meta.Actor
	person := c.Person
	if a.Kind == policy.ActorPerson {
		if person == uuid.Nil {
			person = a.ID
		}
		if person != a.ID {
			return Result{}, invalid("person", "people connect their own agents")
		}
	}
	if person == uuid.Nil {
		return Result{}, invalid("person", "say who the agent works for")
	}
	if w.meta.Scope.PersonID != person {
		return Result{}, invalid("scope", "use the scope of the person the agent works for (ResolveUserScope)")
	}

	type target struct {
		sp    spaceRow
		g     SpaceGrant
		level policy.Autonomy
	}
	targets := make([]target, 0, len(c.Spaces))
	for _, s := range c.Spaces {
		g, ok := w.meta.Scope.Grant(s.SpaceID)
		if !ok {
			return Result{}, ErrNotFound
		}
		sp, err := loadSpace(ctx, w.tx, s.SpaceID)
		if err != nil {
			return Result{}, err
		}
		targets = append(targets, target{sp: sp, g: g, level: s.Autonomy})
	}
	if replay, err := w.claim(ctx, targets[0].sp.ID); err != nil || replay != nil {
		return deref(replay), err
	}

	// The credential must be the person's and active. Anything else is not
	// found, so this never confirms whose a credential is.
	access, err := credentialAccess(ctx, w.tx, c.Credential, c.CredentialID, person)
	if err != nil {
		return Result{}, err
	}
	if access == "" {
		return Result{}, ErrNotFound
	}
	var exists bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.agent_connections WHERE credential_kind = $1 AND credential_id = $2)`,
		string(c.Credential), c.CredentialID).Scan(&exists); err != nil {
		return Result{}, fmt.Errorf("ledger: look up connection: %w", err)
	}
	if exists {
		return Result{}, ErrAlreadyConnected
	}

	mine := a.Kind == policy.ActorPerson && a.ID == person
	var dec policy.Decision
	for i := range targets {
		t := &targets[i]
		if t.level == "" {
			t.level = policy.DefaultAutonomy(t.g.Role, t.sp.policy())
		}
		if c.Cap != "" {
			t.level = policy.MinAutonomy(t.level, c.Cap)
		}
		dec = policy.DecideConnection(w.policyActor(t.g), policy.ConnectionConnect, policy.Connection{
			Name: c.DisplayName, Mine: mine, Credential: c.Credential.Policy(), To: t.level,
		}, t.sp.policy())
		if dec.Effect == policy.EffectRefuse {
			return refused(dec), nil
		}
	}

	id := newID()
	receipts := make([]Receipt, 0, len(targets))
	for i, t := range targets {
		rc := w.agentReceipt(t.sp, t.g, id, string(c.Agent), ActionConnected, i+1)
		rc.Source = &ReceiptSource{Kind: SourceAutonomy, Ref: string(t.level)}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return Result{}, err
		}
		receipts = append(receipts, rc)
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.agent_connections (id, tenant_id, person_id, agent, display_name, surface, credential_kind,
		                                  credential_id, client_id, state, stream_version, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, 'active', $9, $10, $11)`,
		id, person, string(c.Agent), c.DisplayName, string(c.Surface), string(c.Credential), c.CredentialID,
		nullText(c.ClientID), len(receipts), receipts[0].ID, receipts[len(receipts)-1].ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write connection: %w", err)
	}
	b := &pgx.Batch{}
	for i, t := range targets {
		b.Queue(`
			INSERT INTO v2.agent_connection_spaces (connection_id, space_id, tenant_id, person_id, autonomy,
			                                        created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			id, t.sp.ID, t.sp.TenantID, person, string(t.level), receipts[i].ID)
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return Result{}, fmt.Errorf("ledger: write connection spaces: %w", err)
	}
	return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, id)
}

// setAutonomy is SetAutonomy: the level in one space, connecting the
// agent there if it isn't yet.
func (w *writer) setAutonomy(ctx context.Context, c *SetAutonomy) (Result, error) {
	conn, err := lockConnection(ctx, w.tx, c.Connection)
	if err != nil {
		return Result{}, err
	}
	g, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		return deref(replay), err
	}
	var from policy.Autonomy
	err = w.tx.QueryRow(ctx, `
		SELECT autonomy FROM v2.agent_connection_spaces WHERE connection_id = $1 AND space_id = $2 FOR UPDATE`,
		conn.ID, sp.ID).Scan(&from)
	if err != nil && !errNoRows(err) {
		return Result{}, fmt.Errorf("ledger: load autonomy: %w", err)
	}
	connected := err == nil
	act := policy.ConnectionSetAutonomy
	if !connected {
		act = policy.ConnectionConnect
	}
	dec := policy.DecideConnection(w.policyActor(g), act, policy.Connection{
		Name: conn.DisplayName, Mine: w.mine(conn), Credential: conn.Credential.Kind.Policy(), From: from, To: c.Autonomy,
	}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if conn.State == ConnectionDisconnected {
		return Result{}, &ConnectionStateError{Agent: conn.DisplayName, State: conn.State, Command: CommandSetAutonomy}
	}
	if connected && from == c.Autonomy {
		// Nothing changes, so nothing is written.
		return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec}, conn.ID)
	}

	action := ActionAutonomyChanged
	if !connected {
		action = ActionConnected
	}
	rc := w.agentReceipt(sp, g, conn.ID, string(conn.Agent), action, conn.streamVersion+1)
	rc.Source = &ReceiptSource{Kind: SourceAutonomy, Ref: string(c.Autonomy)}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.agent_connections SET stream_version = $2, last_receipt_id = $3, updated_at = now() WHERE id = $1`,
		conn.ID, rc.StreamVersion, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update connection: %w", err)
	}
	if connected {
		_, err = w.tx.Exec(ctx, `
			UPDATE v2.agent_connection_spaces SET autonomy = $3, last_receipt_id = $4, updated_at = now()
			 WHERE connection_id = $1 AND space_id = $2`, conn.ID, sp.ID, string(c.Autonomy), rc.ID)
	} else {
		_, err = w.tx.Exec(ctx, `
			INSERT INTO v2.agent_connection_spaces (connection_id, space_id, tenant_id, person_id, autonomy,
			                                        created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, $6)`, conn.ID, sp.ID, sp.TenantID, conn.PersonID, string(c.Autonomy), rc.ID)
	}
	if err != nil {
		return Result{}, fmt.Errorf("ledger: write autonomy: %w", err)
	}
	return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, conn.ID)
}

// changeConnection is PauseAgent, ResumeAgent and DisconnectAgent. They
// apply to the whole connection, so they write a receipt in every space
// of the scope the agent is connected to (or the person's personal space
// when there is none). Disconnect also revokes the credential.
func (w *writer) changeConnection(ctx context.Context, id uuid.UUID, cmd CommandName) (Result, error) {
	conn, err := lockConnection(ctx, w.tx, id)
	if err != nil {
		return Result{}, err
	}
	var act policy.ConnectionAction
	var to ConnectionState
	var action Action
	switch cmd {
	case CommandPauseAgent:
		act, to, action = policy.ConnectionPause, ConnectionPaused, ActionPaused
	case CommandResumeAgent:
		act, to, action = policy.ConnectionResume, ConnectionActive, ActionResumed
	case CommandDisconnectAgent:
		act, to, action = policy.ConnectionDisconnect, ConnectionDisconnected, ActionDisconnected
	default:
		return Result{}, invalid("command", "unknown agent command %s", cmd)
	}

	rows, err := w.tx.Query(ctx, `
		SELECT space_id FROM v2.agent_connection_spaces
		 WHERE connection_id = $1 AND space_id = ANY($2)
		 ORDER BY space_id`, conn.ID, w.meta.Scope.SpaceIDs())
	if err != nil {
		return Result{}, fmt.Errorf("ledger: load connection spaces: %w", err)
	}
	spaceIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return Result{}, fmt.Errorf("ledger: load connection spaces: %w", err)
	}
	// The key is claimed (and policy asked) in the person's personal space,
	// so a retry finds it wherever the agent is connected by then.
	home, hasHome := w.meta.Scope.personalSpace()
	if len(spaceIDs) == 0 {
		if !hasHome {
			return Result{}, ErrNotFound
		}
		spaceIDs = []uuid.UUID{home.SpaceID}
	}
	if !hasHome {
		home, _ = w.meta.Scope.Grant(spaceIDs[0])
	}
	homeSpace, err := loadSpace(ctx, w.tx, home.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, homeSpace.ID); err != nil || replay != nil {
		return deref(replay), err
	}

	dec := policy.DecideConnection(w.policyActor(home), act, policy.Connection{
		Name: conn.DisplayName, Mine: w.mine(conn), Credential: conn.Credential.Kind.Policy(),
	}, homeSpace.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if conn.State == to || !ConnectionTransitionAllowed(conn.State, to) {
		return Result{}, &ConnectionStateError{Agent: conn.DisplayName, State: conn.State, Command: cmd}
	}

	receipts := make([]Receipt, 0, len(spaceIDs))
	for i, sid := range spaceIDs {
		g, _ := w.meta.Scope.Grant(sid)
		sp := homeSpace
		if sid != homeSpace.ID {
			if sp, err = loadSpace(ctx, w.tx, sid); err != nil {
				return Result{}, err
			}
		}
		rc := w.agentReceipt(sp, g, conn.ID, string(conn.Agent), action, conn.streamVersion+1+i)
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return Result{}, err
		}
		receipts = append(receipts, rc)
	}
	last := receipts[len(receipts)-1]
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.agent_connections
		   SET state = $2, disconnected_at = CASE WHEN $2 = 'disconnected' THEN now() END,
		       stream_version = $3, last_receipt_id = $4, updated_at = now()
		 WHERE id = $1`, conn.ID, string(to), last.StreamVersion, last.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update connection: %w", err)
	}
	if to == ConnectionDisconnected {
		// The credential stops working at once (migration 029's
		// v2.revoke_agent_credential sets revoked_at, which V1's resolvers
		// refuse). False means V1 had already deleted or revoked it.
		var revoked bool
		if err := w.tx.QueryRow(ctx, `SELECT v2.revoke_agent_credential($1)`, conn.ID).Scan(&revoked); err != nil {
			return Result{}, fmt.Errorf("ledger: revoke credential: %w", err)
		}
	}
	return w.finishConnection(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, conn.ID)
}

// agentReceipt is a receipt about a connection in one space. A person's
// receipt carries their assurance, so a raise on the web reads as
// human_web in the history.
func (w *writer) agentReceipt(sp spaceRow, g SpaceGrant, connection uuid.UUID, ref string, action Action, version int) Receipt {
	rc := w.receipt(sp, connection, ref, action, version, w.meta.Reason)
	rc.ObjectKind = ObjectAgent
	rc.Assurance = w.policyActor(g).Assurance()
	return rc
}

// mine reports whether the actor is the person the agent works for.
func (w *writer) mine(c *Connection) bool {
	a := w.meta.Actor
	return a.Kind == policy.ActorPerson && a.ID == c.PersonID
}

// finishConnection records the result against the idempotency key and
// loads the connection's new projection.
func (w *writer) finishConnection(ctx context.Context, res Result, id uuid.UUID) (Result, error) {
	if err := w.record(ctx, res, id); err != nil {
		return Result{}, err
	}
	c, err := loadConnection(ctx, w.tx, w.meta.Scope, id)
	if err != nil {
		return Result{}, err
	}
	res.Connection = c
	return res, nil
}

// personalSpace is the scope's personal space owned by the person, if the
// scope has one.
func (s Scope) personalSpace() (SpaceGrant, bool) {
	for _, g := range s.Spaces {
		if g.Kind == policy.SpacePersonal && g.Role == policy.RoleOwner {
			return g, true
		}
	}
	return SpaceGrant{}, false
}

// lockConnection loads a connection in scope and takes the row lock that
// serialises commands on it.
func lockConnection(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Connection, error) {
	var c Connection
	err := tx.QueryRow(ctx, `
		SELECT id, person_id, agent, display_name, credential_kind, credential_id, state, stream_version
		  FROM v2.agent_connections WHERE id = $1 FOR UPDATE`, id).
		Scan(&c.ID, &c.PersonID, &c.Agent, &c.DisplayName, &c.Credential.Kind, &c.Credential.ID, &c.State, &c.streamVersion)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load connection: %w", err)
	}
	return &c, nil
}

// credentialAccess is what a credential allows ("write", "read"), or ""
// when it isn't the person's or isn't active (migration 029's
// v2.agent_credential_access).
func credentialAccess(ctx context.Context, tx pgx.Tx, kind CredentialKind, id, person uuid.UUID) (string, error) {
	var access string
	err := tx.QueryRow(ctx, `SELECT COALESCE(v2.agent_credential_access($1, $2, $3), '')`,
		string(kind), id, person).Scan(&access)
	if err != nil {
		return "", fmt.Errorf("ledger: check credential: %w", err)
	}
	return strings.TrimSpace(access), nil
}
