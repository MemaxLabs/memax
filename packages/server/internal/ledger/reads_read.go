package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reading reads back: Activity's R- list, a memory's "read by", an agent's
// reads, fading's question (when was each memory last read, and is it in
// a file whose loads Memax can't see), and the north star. Like every
// ledger read they run as memax_v2 inside the caller's scope.

const readSelect = `
	SELECT r.id, r.space_id, r.seq, r.reader_kind, r.connection_id, r.person_id, COALESCE(r.agent, ''), r.kind, r.via,
	       COALESCE(r.session_ref, ''), c.seq, r.memories, r.read_at, r.recorded_at,
	       COALESCE((SELECT array_agg(m.seq ORDER BY m.seq) FROM v2.memories m WHERE m.id = ANY (r.memory_ids)), '{}')
	  FROM v2.reads r
	  LEFT JOIN v2.compile_runs c ON c.id = r.compile_id`

func scanRead(row pgx.CollectableRow) (Read, error) {
	var r Read
	var compile *int64
	var seqs []int64
	if err := row.Scan(&r.ID, &r.SpaceID, &r.seq, &r.ReaderKind, &r.ConnectionID, &r.PersonID, &r.Agent, &r.Kind, &r.Via,
		&r.SessionRef, &compile, &r.Memories, &r.ReadAt, &r.RecordedAt, &seqs); err != nil {
		return Read{}, err
	}
	r.Ref = FormatRef(PrefixRead, r.seq)
	if compile != nil {
		r.Compile = FormatRef(PrefixCompile, *compile)
	}
	r.MemoryRefs = make([]string, len(seqs))
	for i, n := range seqs {
		r.MemoryRefs[i] = FormatRef(PrefixMemory, n)
	}
	return r, nil
}

// loadRead reads one read by id in the transaction's scope.
func loadRead(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Read, error) {
	rows, err := tx.Query(ctx, readSelect+` WHERE r.id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("ledger: load read: %w", err)
	}
	r, err := pgx.CollectExactlyOneRow(rows, scanRead)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load read: %w", err)
	}
	return &r, nil
}

// ReadQuery pages through a space's reads.
type ReadQuery struct {
	SpaceID uuid.UUID
	Cursor  string
	// Limit defaults to DefaultPageSize and is capped at MaxPageSize.
	Limit int
}

// ReadPage is one page of reads, newest first, with the space's reads in
// the last 7 days.
type ReadPage struct {
	Reads      []Read
	NextCursor string
	HasMore    bool
	Week       int
}

// ListReads pages through a space's reads (Activity's R- rows).
func (l *Ledger) ListReads(ctx context.Context, scope Scope, q ReadQuery) (ReadPage, error) {
	if l == nil {
		return ReadPage{}, ErrDisabled
	}
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return ReadPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return ReadPage{}, err
	}
	before, err := decodeCursor(q.Cursor, 'R')
	if err != nil {
		return ReadPage{}, err
	}
	var page ReadPage
	err = l.Read(ctx, scope.Narrow(q.SpaceID), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, readSelect+`
			 WHERE r.space_id = $1 AND ($2::bigint = 0 OR r.seq < $2)
			 ORDER BY r.seq DESC
			 LIMIT $3`, q.SpaceID, before, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list reads: %w", err)
		}
		if page.Reads, err = pgx.CollectRows(rows, scanRead); err != nil {
			return fmt.Errorf("ledger: list reads: %w", err)
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.reads WHERE space_id = $1 AND read_at > $2`,
			q.SpaceID, l.now().Add(-7*24*time.Hour)).Scan(&page.Week)
	})
	if err != nil {
		return ReadPage{}, err
	}
	if len(page.Reads) > limit {
		page.Reads = page.Reads[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('R', page.Reads[limit-1].seq)
	}
	return page, nil
}

// MemoryReads is how a memory has been read: directly (recall, search,
// get, list) and through every compile that contained it (a digest, a
// session-start load).
type MemoryReads struct {
	// Reads counts reads over the retained history (ReadRetentionMonths).
	Reads      int        `json:"reads"`
	Reads7d    int        `json:"reads_7d"`
	LastReadAt *time.Time `json:"last_read_at,omitempty"`
	// Readers are who read it, most recent first (at most MaxReaders).
	Readers []MemoryReader `json:"readers"`
	// Agents counts the distinct agents that read it.
	Agents int `json:"agents"`
	// UnobservedTarget: it is in a compiled file whose loads Memax can't
	// see (an agent that reads the file natively, with no hook reporting
	// it), so it may be read more than the counts show, and fading skips
	// it (plan 25 §5.10).
	UnobservedTarget bool `json:"unobserved_target"`
}

// MemoryReader is one reader of a memory.
type MemoryReader struct {
	ReaderKind   ReaderKind `json:"reader_kind"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	PersonID     uuid.UUID  `json:"person_id"`
	Agent        string     `json:"agent,omitempty"`
	// DisplayName is the agent connection's name, when the caller can see
	// the connection.
	DisplayName string    `json:"display_name,omitempty"`
	Reads       int       `json:"reads"`
	Reads7d     int       `json:"reads_7d"`
	LastReadAt  time.Time `json:"last_read_at"`
}

// MaxReaders bounds MemoryReads.Readers.
const MaxReaders = 20

// ObservedWindow is how recent a compile load must be for a target's loads
// to count as observed: the fade window (60 days unread).
const ObservedWindow = 60 * 24 * time.Hour

// GetMemoryReads reads a memory's reads in scope.
func (l *Ledger) GetMemoryReads(ctx context.Context, scope Scope, memoryID uuid.UUID) (*MemoryReads, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	now := l.now().UTC()
	out := &MemoryReads{Readers: []MemoryReader{}}
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		var spaceID uuid.UUID
		var seq int64
		err := tx.QueryRow(ctx, `SELECT space_id, seq FROM v2.memories WHERE id = $1`, memoryID).Scan(&spaceID, &seq)
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: memory reads: %w", err)
		}
		ref := FormatRef(PrefixMemory, seq)
		rows, err := tx.Query(ctx, `
			WITH subjects AS (
			    SELECT $2::uuid AS id
			    UNION
			    SELECT c.id FROM v2.compile_runs c WHERE c.space_id = $1 AND c.refs @> ARRAY[$3::text]
			)
			SELECT r.reader_kind, r.reader_key, r.person_id, r.agent,
			       sum(r.reads)::int,
			       COALESCE(sum(r.reads) FILTER (WHERE r.day >= $4::date - 6), 0)::int,
			       max(r.last_read_at),
			       COALESCE(max(ac.display_name), '')
			  FROM v2.read_rollups r
			  JOIN subjects s ON s.id = r.subject_id
			  LEFT JOIN v2.agent_connections ac ON r.reader_kind = 'agent' AND ac.id = r.reader_key
			 WHERE r.space_id = $1
			 GROUP BY r.reader_kind, r.reader_key, r.person_id, r.agent
			 ORDER BY max(r.last_read_at) DESC, r.reader_key`, spaceID, memoryID, ref, now)
		if err != nil {
			return fmt.Errorf("ledger: memory reads: %w", err)
		}
		agents := map[string]bool{}
		for rows.Next() {
			var rd MemoryReader
			var key uuid.UUID
			if err := rows.Scan(&rd.ReaderKind, &key, &rd.PersonID, &rd.Agent, &rd.Reads, &rd.Reads7d, &rd.LastReadAt, &rd.DisplayName); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: memory reads: %w", err)
			}
			if rd.ReaderKind == ReaderAgent {
				id := key
				rd.ConnectionID = &id
			}
			out.Reads += rd.Reads
			out.Reads7d += rd.Reads7d
			if out.LastReadAt == nil || rd.LastReadAt.After(*out.LastReadAt) {
				t := rd.LastReadAt
				out.LastReadAt = &t
			}
			agent := rd.Agent
			if agent == "" {
				agent = key.String()
			}
			agents[agent] = true
			if len(out.Readers) < MaxReaders {
				out.Readers = append(out.Readers, rd)
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: memory reads: %w", err)
		}
		out.Agents = len(agents)
		return tx.QueryRow(ctx, `SELECT EXISTS (`+unobservedRefs+` AND ref = $3)`, spaceID, now.Add(-ObservedWindow), ref).
			Scan(&out.UnobservedTarget)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// unobservedRefs selects (as `ref`) the memories in a space ($1) that are
// in a compiled file whose loads Memax can't observe: a target that is on,
// not delivered over MCP (which Memax serves itself, so every load is a
// read), and with no session-start compile load reported since $2. The
// file's memories are those of its latest good compile and of the compile
// on disk, when they differ.
const unobservedRefs = `
	SELECT ref
	  FROM v2.targets t
	  CROSS JOIN LATERAL (
	      SELECT cr.refs FROM v2.compile_runs cr
	       WHERE cr.target_id = t.id AND cr.status <> 'failed'
	       ORDER BY cr.seq DESC LIMIT 1
	  ) latest
	  CROSS JOIN LATERAL unnest(latest.refs || COALESCE(
	      (SELECT d.refs FROM v2.compile_runs d WHERE d.id = t.delivered_compile_id), '{}')) AS ref
	 WHERE t.space_id = $1 AND t.sync_state <> 'off' AND t.delivery <> 'mcp'
	   AND NOT EXISTS (
	       SELECT 1 FROM v2.reads l JOIN v2.compile_runs lc ON lc.id = l.compile_id
	        WHERE l.space_id = $1 AND l.kind = 'compile_load' AND l.read_at >= $2 AND lc.target_id = t.id)`

// MemoryReadStatus is fading's view of one kept memory.
type MemoryReadStatus struct {
	ID  uuid.UUID
	Ref string
	// LastReadAt is its latest read by any observed path (directly, or in
	// a compile served by the digest or reported loaded); nil if never
	// within the retained history.
	LastReadAt *time.Time
	// UnobservedTarget: it is in a file whose loads Memax can't see, so it
	// is exempt from fading (plan 25 §5.10).
	UnobservedTarget bool
}

// Unread reports whether the memory may fade: unread since `since`, and
// not in a file whose loads Memax can't see.
func (s MemoryReadStatus) Unread(since time.Time) bool {
	return !s.UnobservedTarget && (s.LastReadAt == nil || s.LastReadAt.Before(since))
}

// ReadStatus answers fading's question for every kept memory of a space,
// in one query: when it was last read by any observed path, and whether
// it is in a target whose loads Memax can't observe (a target counts as
// observed when a session-start load of it was reported within
// ObservedWindow, or when it is delivered over MCP).
func (l *Ledger) ReadStatus(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]MemoryReadStatus, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []MemoryReadStatus
	err := l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH kept AS (
			    SELECT m.id, m.seq FROM v2.memories m WHERE m.space_id = $1 AND m.lifecycle = 'kept'
			), direct AS (
			    SELECT subject_id AS id, max(last_read_at) AS at
			      FROM v2.read_rollups
			     WHERE space_id = $1 AND subject_kind = 'memory'
			     GROUP BY subject_id
			), compiles AS (
			    SELECT subject_id AS id, max(last_read_at) AS at
			      FROM v2.read_rollups
			     WHERE space_id = $1 AND subject_kind = 'compile'
			     GROUP BY subject_id
			), via_compile AS (
			    SELECT split_part(ref, '-', 2)::bigint AS seq, max(c.at) AS at
			      FROM compiles c
			      JOIN v2.compile_runs cr ON cr.id = c.id
			      CROSS JOIN LATERAL unnest(cr.refs) AS ref
			     WHERE ref ~ '^M-[0-9]+$'
			     GROUP BY 1
			), unobserved AS (
			    SELECT DISTINCT split_part(u.ref, '-', 2)::bigint AS seq
			      FROM (`+unobservedRefs+`) u
			     WHERE u.ref ~ '^M-[0-9]+$'
			)
			SELECT k.id, k.seq, greatest(d.at, v.at), u.seq IS NOT NULL
			  FROM kept k
			  LEFT JOIN direct d ON d.id = k.id
			  LEFT JOIN via_compile v ON v.seq = k.seq
			  LEFT JOIN unobserved u ON u.seq = k.seq
			 ORDER BY k.seq`, spaceID, l.now().Add(-ObservedWindow))
		if err != nil {
			return fmt.Errorf("ledger: read status: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemoryReadStatus, error) {
			var s MemoryReadStatus
			var seq int64
			err := r.Scan(&s.ID, &seq, &s.LastReadAt, &s.UnobservedTarget)
			s.Ref = FormatRef(PrefixMemory, seq)
			return s, err
		})
		if err != nil {
			return fmt.Errorf("ledger: read status: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadMetrics is the north star (plan 25 §5.18) over the 7 UTC days
// ending Day, with what it can't see beside it.
type ReadMetrics struct {
	Day time.Time
	// SpacesRead had at least one memory or compile read.
	SpacesRead int64
	// SpacesTwoAgents were read by 2+ agent kinds: the north star.
	SpacesTwoAgents int64
	// SpacesTwoConnections were read by 2+ agent connections (two Claude
	// Codes on two laptops count here, not above).
	SpacesTwoConnections int64
	// SpacesHookLoads had a session-start compile load reported.
	SpacesHookLoads int64
	// ConnectionsReading had a recorded read; ConnectionsSeen were used at
	// all. Their ratio is the coverage: agents that load files natively
	// with no hook are seen but never read.
	ConnectionsReading, ConnectionsSeen int64
}

// Coverage is ConnectionsReading / ConnectionsSeen, or 0.
func (m ReadMetrics) Coverage() float64 {
	if m.ConnectionsSeen == 0 {
		return 0
	}
	return float64(m.ConnectionsReading) / float64(m.ConnectionsSeen)
}

// GetReadMetrics computes the north star for the week ending day, across
// every space, as counts only (v2.read_metrics).
func (l *Ledger) GetReadMetrics(ctx context.Context, day time.Time) (ReadMetrics, error) {
	if l == nil {
		return ReadMetrics{}, ErrDisabled
	}
	m := ReadMetrics{Day: time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)}
	err := l.Read(ctx, Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM v2.read_metrics($1::date)`, m.Day).Scan(
			&m.SpacesRead, &m.SpacesTwoAgents, &m.SpacesTwoConnections, &m.SpacesHookLoads,
			&m.ConnectionsReading, &m.ConnectionsSeen)
	})
	if err != nil {
		return ReadMetrics{}, fmt.Errorf("ledger: read metrics: %w", err)
	}
	return m, nil
}
