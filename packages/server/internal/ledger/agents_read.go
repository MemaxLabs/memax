package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reads of agent connections. Like every ledger read they run as
// memax_v2 inside the caller's scope: a person sees their own
// connections, and other people's only in the spaces they share.

const connectionSelect = `
	SELECT c.id, c.person_id, c.agent, c.display_name, c.surface, c.credential_kind, c.credential_id,
	       COALESCE(c.client_id, ''), c.state, c.stream_version, c.last_seen_at, c.disconnected_at,
	       c.created_receipt_id, c.last_receipt_id, c.created_at, c.updated_at,
	       COALESCE(v2.agent_credential_access(c.credential_kind, c.credential_id, c.person_id), ''),
	       COALESCE(r.actor_kind, ''), r.actor_id
	  FROM v2.agent_connections c
	  LEFT JOIN v2.receipts r ON r.id = c.created_receipt_id`

func scanConnection(row pgx.Row) (*Connection, error) {
	var c Connection
	var access, byKind string
	if err := row.Scan(&c.ID, &c.PersonID, &c.Agent, &c.DisplayName, &c.Surface, &c.Credential.Kind, &c.Credential.ID,
		&c.ClientID, &c.State, &c.streamVersion, &c.LastSeenAt, &c.DisconnectedAt,
		&c.CreatedReceiptID, &c.LastReceiptID, &c.CreatedAt, &c.UpdatedAt,
		&access, &byKind, &c.ConnectedBy); err != nil {
		return nil, err
	}
	c.Credential.Active = access != ""
	c.MaxAutonomy = maxAutonomy(c.Credential.Kind, access)
	c.ConnectedByKind = policy.ActorKind(byKind)
	c.Spaces = []ConnectionSpace{}
	return &c, nil
}

// maxAutonomy is the most a credential allows: an API key proposes at
// most (policy caps it there anyway), and a credential that can't write,
// or no longer works, only reads.
func maxAutonomy(kind CredentialKind, access string) policy.Autonomy {
	switch {
	case access != "write":
		return policy.AutonomyRead
	case kind == CredentialAPIKey:
		return policy.AutonomyPropose
	}
	return policy.AutonomyWrite
}

// loadConnection reads one connection in scope, with its spaces and
// counts.
func loadConnection(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID) (*Connection, error) {
	c, err := scanConnection(tx.QueryRow(ctx, connectionSelect+` WHERE c.id = $1`, id))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load connection: %w", err)
	}
	if err := fillConnections(ctx, tx, scope, []*Connection{c}); err != nil {
		return nil, err
	}
	return c, nil
}

// fillConnections adds each connection's spaces in scope and its writes
// in the last 7 days there.
func fillConnections(ctx context.Context, tx pgx.Tx, scope Scope, cs []*Connection) error {
	if len(cs) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]*Connection, len(cs))
	ids := make([]uuid.UUID, 0, len(cs))
	for _, c := range cs {
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	type spaceRef struct{ conn, space uuid.UUID }
	at := map[spaceRef]int{}
	rows, err := tx.Query(ctx, `
		SELECT s.connection_id, s.space_id, sp.slug, sp.name, sp.kind, s.autonomy, s.updated_at
		  FROM v2.agent_connection_spaces s
		  JOIN v2.spaces sp ON sp.id = s.space_id
		 WHERE s.connection_id = ANY($1) AND s.space_id = ANY($2)
		 ORDER BY sp.kind = 'personal' DESC, lower(sp.name), sp.id`, ids, scope.SpaceIDs())
	if err != nil {
		return fmt.Errorf("ledger: load connection spaces: %w", err)
	}
	for rows.Next() {
		var conn uuid.UUID
		var s ConnectionSpace
		if err := rows.Scan(&conn, &s.SpaceID, &s.Slug, &s.Name, &s.Kind, &s.Autonomy, &s.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: load connection spaces: %w", err)
		}
		c := byID[conn]
		at[spaceRef{conn, s.SpaceID}] = len(c.Spaces)
		c.Spaces = append(c.Spaces, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: load connection spaces: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT actor_id, space_id, count(*)
		  FROM v2.receipts
		 WHERE actor_kind = 'agent' AND actor_id = ANY($1) AND object_kind = 'memory'
		   AND action IN ('proposed', 'kept', 'edited') AND recorded_at > now() - interval '7 days'
		   AND space_id = ANY($2)
		 GROUP BY actor_id, space_id`, ids, scope.SpaceIDs())
	if err != nil {
		return fmt.Errorf("ledger: count agent writes: %w", err)
	}
	for rows.Next() {
		var conn, space uuid.UUID
		var n int
		if err := rows.Scan(&conn, &space, &n); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: count agent writes: %w", err)
		}
		c := byID[conn]
		c.WritesWeek += n
		if i, ok := at[spaceRef{conn, space}]; ok {
			c.Spaces[i].WritesWeek = n
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: count agent writes: %w", err)
	}
	return nil
}

// ConnectionQuery filters ListConnections.
type ConnectionQuery struct {
	// SpaceID lists the connections in one space, whoever they work for.
	// Without it, ListConnections lists the scope's person's own.
	SpaceID uuid.UUID
	// IncludeDisconnected lists disconnected connections too.
	IncludeDisconnected bool
}

// ListConnections lists agent connections, oldest first.
func (l *Ledger) ListConnections(ctx context.Context, scope Scope, q ConnectionQuery) ([]Connection, error) {
	if q.SpaceID != uuid.Nil {
		if _, ok := scope.Grant(q.SpaceID); !ok {
			return nil, ErrNotFound
		}
	} else if scope.PersonID == uuid.Nil {
		return []Connection{}, nil
	}
	var out []Connection
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		where := ` WHERE c.person_id = $1`
		arg := scope.PersonID
		if q.SpaceID != uuid.Nil {
			where = ` WHERE c.id IN (SELECT connection_id FROM v2.agent_connection_spaces WHERE space_id = $1)`
			arg = q.SpaceID
		}
		rows, err := tx.Query(ctx, connectionSelect+where+`
			   AND ($2 OR c.state <> 'disconnected')
			 ORDER BY c.created_at, c.id`, arg, q.IncludeDisconnected)
		if err != nil {
			return fmt.Errorf("ledger: list connections: %w", err)
		}
		cs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Connection, error) { return scanConnection(r) })
		if err != nil {
			return fmt.Errorf("ledger: list connections: %w", err)
		}
		if err := fillConnections(ctx, tx, scope, cs); err != nil {
			return err
		}
		out = make([]Connection, len(cs))
		for i, c := range cs {
			out[i] = *c
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AgentWeek is what an agent did in the last 7 days, in the reader's
// spaces: its writes (proposed, kept or edited), and what became of the
// memories it wrote (kept, rejected, still waiting in Review). Reads are
// 0 until reads are recorded.
type AgentWeek struct {
	Reads     int `json:"reads"`
	Writes    int `json:"writes"`
	Proposals int `json:"proposals"`
	Kept      int `json:"kept"`
	Rejected  int `json:"rejected"`
	Waiting   int `json:"waiting"`
}

// AgentSession is one of an agent's sessions, from the session_ref on its
// receipts.
type AgentSession struct {
	SessionRef string    `json:"session_ref"`
	Reads      int       `json:"reads"`
	Writes     int       `json:"writes"`
	LastAt     time.Time `json:"last_at"`
}

// ConnectionDetail is one connection with what AgentDetail shows.
type ConnectionDetail struct {
	Connection *Connection
	Week       AgentWeek
	// RecentWrites are its latest receipts on memories, newest first.
	RecentWrites []Receipt
	// Sessions are its latest sessions, most recent first.
	Sessions []AgentSession
}

// DetailLimit bounds RecentWrites and Sessions.
const DetailLimit = 10

// GetConnection reads one connection in scope with its week, recent
// writes and sessions.
func (l *Ledger) GetConnection(ctx context.Context, scope Scope, id uuid.UUID) (*ConnectionDetail, error) {
	var out *ConnectionDetail
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		c, err := loadConnection(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		d := &ConnectionDetail{Connection: c, Week: AgentWeek{Writes: c.WritesWeek}}
		rows, err := tx.Query(ctx, `
			SELECT m.lifecycle, r.action, count(*)
			  FROM v2.memories m
			  JOIN v2.receipts r ON r.id = m.created_receipt_id
			 WHERE r.actor_kind = 'agent' AND r.actor_id = $1 AND r.recorded_at > now() - interval '7 days'
			   AND m.space_id = ANY($2)
			 GROUP BY m.lifecycle, r.action`, c.ID, scope.SpaceIDs())
		if err != nil {
			return fmt.Errorf("ledger: agent week: %w", err)
		}
		for rows.Next() {
			var lifecycle, action string
			var n int
			if err := rows.Scan(&lifecycle, &action, &n); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: agent week: %w", err)
			}
			if action == string(ActionProposed) {
				d.Week.Proposals += n
			}
			switch lifecycle {
			case "kept":
				d.Week.Kept += n
			case "rejected":
				d.Week.Rejected += n
			case "proposed":
				d.Week.Waiting += n
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: agent week: %w", err)
		}

		rows, err = tx.Query(ctx, receiptSelect+`
			 WHERE actor_kind = 'agent' AND actor_id = $1 AND object_kind = 'memory' AND space_id = ANY($2)
			 ORDER BY seq DESC
			 LIMIT $3`, c.ID, scope.SpaceIDs(), DetailLimit)
		if err != nil {
			return fmt.Errorf("ledger: agent writes: %w", err)
		}
		if d.RecentWrites, err = pgx.CollectRows(rows, scanReceipt); err != nil {
			return fmt.Errorf("ledger: agent writes: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT session_ref, count(*), max(recorded_at)
			  FROM v2.receipts
			 WHERE actor_kind = 'agent' AND actor_id = $1 AND session_ref IS NOT NULL AND space_id = ANY($2)
			 GROUP BY session_ref
			 ORDER BY max(recorded_at) DESC
			 LIMIT $3`, c.ID, scope.SpaceIDs(), DetailLimit)
		if err != nil {
			return fmt.Errorf("ledger: agent sessions: %w", err)
		}
		d.Sessions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (AgentSession, error) {
			var s AgentSession
			err := r.Scan(&s.SessionRef, &s.Writes, &s.LastAt)
			return s, err
		})
		if err != nil {
			return fmt.Errorf("ledger: agent sessions: %w", err)
		}
		out = d
		return nil
	})
	return out, err
}

// ConnectionForCredential is the connection a credential is bound to,
// with its levels in the scope's spaces, or nil when it has none. It is
// on the path of every agent request, so it reads only what principal
// resolution needs, in one query. The scope's PersonID must be the
// credential's owner.
func (l *Ledger) ConnectionForCredential(ctx context.Context, scope Scope, kind CredentialKind, id uuid.UUID) (*Connection, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *Connection
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		var c Connection
		var spaces []uuid.UUID
		var levels []string
		err := tx.QueryRow(ctx, `
			SELECT c.id, c.person_id, c.agent, c.display_name, c.surface, c.credential_kind, c.credential_id,
			       c.state, c.last_seen_at,
			       COALESCE(array_agg(s.space_id ORDER BY s.space_id) FILTER (WHERE s.space_id IS NOT NULL), '{}'),
			       COALESCE(array_agg(s.autonomy ORDER BY s.space_id) FILTER (WHERE s.space_id IS NOT NULL), '{}')
			  FROM v2.agent_connections c
			  LEFT JOIN v2.agent_connection_spaces s ON s.connection_id = c.id AND s.space_id = ANY($4)
			 WHERE c.credential_kind = $1 AND c.credential_id = $2 AND c.person_id = $3
			 GROUP BY c.id`, string(kind), id, scope.PersonID, scope.SpaceIDs()).
			Scan(&c.ID, &c.PersonID, &c.Agent, &c.DisplayName, &c.Surface, &c.Credential.Kind, &c.Credential.ID,
				&c.State, &c.LastSeenAt, &spaces, &levels)
		if errNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ledger: resolve connection: %w", err)
		}
		c.Spaces = make([]ConnectionSpace, len(spaces))
		for i := range spaces {
			c.Spaces[i] = ConnectionSpace{SpaceID: spaces[i], Autonomy: policy.Autonomy(levels[i])}
		}
		out = &c
		return nil
	})
	return out, err
}

// TouchConnection records that the agent was seen at `at`. It writes
// last_seen_at only (usage data, so no receipt; migration 029), only
// forward, and never for a disconnected connection. Call it off the
// request's path.
func (l *Ledger) TouchConnection(ctx context.Context, personID, id uuid.UUID, at time.Time) error {
	if l == nil {
		return ErrDisabled
	}
	tx, _, err := l.begin(ctx, Scope{PersonID: personID}, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE v2.agent_connections SET last_seen_at = $2
		 WHERE id = $1 AND person_id = $3 AND state <> 'disconnected'
		   AND (last_seen_at IS NULL OR last_seen_at < $2)`, id, at, personID); err != nil {
		return mapDBError(fmt.Errorf("ledger: touch connection: %w", err))
	}
	return tx.Commit(ctx)
}
