package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
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
// the last 7 days. RetentionDays is how far back the space's plan lists
// them (0: as far as they are kept).
type ReadPage struct {
	Reads         []Read
	NextCursor    string
	HasMore       bool
	Week          int
	RetentionDays int
}

// ListReads pages through a space's reads (Activity's R- rows).
func (l *Ledger) ListReads(ctx context.Context, scope Scope, q ReadQuery) (ReadPage, error) {
	if l == nil {
		return ReadPage{}, ErrDisabled
	}
	g, ok := scope.Grant(q.SpaceID)
	if !ok {
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
	// The space's plan says how far back Activity goes (D9); older reads
	// stay, for fading, and aren't listed.
	plan := l.SpacePlan(g)
	since := activitySince(plan, l.now())
	page := ReadPage{RetentionDays: policy.ActivityDays(plan)}
	err = l.Read(ctx, scope.Narrow(q.SpaceID), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, readSelect+`
			 WHERE r.space_id = $1 AND ($2::bigint = 0 OR r.seq < $2) AND ($4::timestamptz IS NULL OR r.read_at >= $4)
			 ORDER BY r.seq DESC
			 LIMIT $3`, q.SpaceID, before, limit+1, since)
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
	var out *MemoryReads
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
		b := &pgx.Batch{}
		out = queueMemoryReads(b, spaceID, memoryID, FormatRef(PrefixMemory, seq), l.now().UTC())
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MemoryChecked is when a memory's conditions were last checked: the
// space's latest check run since one first reported on them. Every run
// reads every condition, so one with nothing new to report still checked
// them. Where, by what, and for which agent's session start. No words.
type MemoryChecked struct {
	At       time.Time `json:"at"`
	Branch   string    `json:"branch"`
	Commit   string    `json:"commit"`
	Observer string    `json:"observer"`
	Trigger  string    `json:"trigger"`
	Agent    string    `json:"agent,omitempty"`
}

// MemoryExtras are what a memory's page shows beside the record: how it
// was read, agents' requests to forget it, and its conditions' last check.
type MemoryExtras struct {
	Reads          *MemoryReads
	ForgetRequests []ForgetRequest
	// Checked is nil until a check reported on its conditions.
	Checked *MemoryChecked
}

// GetMemoryExtras reads what a memory's page shows beside its history, for
// a memory the caller has read already (GetMemoryHistory), in one round
// trip (ReadBatch): its reads, with requests the forget requests waiting
// on it, and its conditions' last check when it has conditions.
func (l *Ledger) GetMemoryExtras(ctx context.Context, scope Scope, m *Memory, requests bool) (*MemoryExtras, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	b := &pgx.Batch{}
	out := &MemoryExtras{Reads: queueMemoryReads(b, m.SpaceID, m.ID, m.Ref, l.now().UTC())}
	if requests {
		queueForgetRequests(b, &out.ForgetRequests, m.ID, scope.SpaceIDs())
	}
	if hasConditions(m.Conditions) {
		b.Queue(`
			SELECT r.checked_at, r.branch, r.commit_sha, r.observer, r.trigger, COALESCE(r.agent, '')
			  FROM v2.check_runs r
			 WHERE r.space_id = $1
			   AND EXISTS (SELECT 1 FROM v2.condition_checks c
			                WHERE c.space_id = $1 AND c.memory_id = $2 AND c.context = 'default')
			 ORDER BY r.recorded_at DESC
			 LIMIT 1`, m.SpaceID, m.ID).QueryRow(func(row pgx.Row) error {
			var c MemoryChecked
			err := row.Scan(&c.At, &c.Branch, &c.Commit, &c.Observer, &c.Trigger, &c.Agent)
			if errNoRows(err) {
				return nil
			}
			if err == nil {
				out.Checked = &c
			}
			return err
		})
	}
	if err := l.ReadBatch(ctx, scope, b); err != nil {
		return nil, err
	}
	if out.ForgetRequests == nil {
		out.ForgetRequests = []ForgetRequest{}
	}
	return out, nil
}

// hasConditions says whether a memory's stored conditions list any.
func hasConditions(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return t != "" && t != "null" && t != "[]"
}

// queueMemoryReads queues a memory's reads (its own and those of every
// compile that held it) and whether a file whose loads Memax can't see
// holds it; the answer is filled in when b's results are read.
func queueMemoryReads(b *pgx.Batch, spaceID, memoryID uuid.UUID, ref string, now time.Time) *MemoryReads {
	out := &MemoryReads{Readers: []MemoryReader{}}
	b.Queue(`
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
		 ORDER BY max(r.last_read_at) DESC, r.reader_key`, spaceID, memoryID, ref, now).
		Query(func(rows pgx.Rows) error {
			agents := map[string]bool{}
			for rows.Next() {
				var rd MemoryReader
				var key uuid.UUID
				if err := rows.Scan(&rd.ReaderKind, &key, &rd.PersonID, &rd.Agent, &rd.Reads, &rd.Reads7d, &rd.LastReadAt, &rd.DisplayName); err != nil {
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
			return nil
		})
	b.Queue(`SELECT EXISTS (`+unobservedRefs+` AND ref = $3)`, spaceID, now.Add(-ObservedWindow), ref).
		QueryRow(func(row pgx.Row) error { return row.Scan(&out.UnobservedTarget) })
	return out
}

// hookedAgents are the agents whose session-start hook reports the compile
// loads of the files they read (packages/cli lib/connect/hooks.ts). A home
// file read by any other (Windsurf, OpenCode, Antigravity until its hook is
// wired) has loads Memax can't see.
var hookedAgents = []string{"claude-code", "codex", "gemini-cli", "cursor", "copilot"}

// unobservedRefs selects (as `ref`) the memories in a space ($1) that are
// in a compiled file whose loads Memax can't observe: a target that is on,
// not delivered over MCP (which Memax serves itself, so every load is a
// read), and with no session-start compile load reported since $2. The
// file's memories are those of its latest good compile and of the compile
// on disk, when they differ.
//
// A home file says who reads it (settings.read_by, personal-layer design
// §7.3). One nobody reads (the canonical copy, until OpenCode points at
// it) isn't unobserved: nothing loads it, so its facts mustn't be kept from
// fading for it, or no personal fact would ever fade. One that an agent
// without a session-start hook reads is unobserved even with loads
// reported, since that agent's reads are never among them. A repository's
// file has no read_by and is judged by its loads alone, as before.
var unobservedRefs = `
	SELECT ref
	  FROM v2.targets t
	  CROSS JOIN LATERAL (
	      SELECT cr.refs FROM v2.compile_runs cr
	       WHERE cr.target_id = t.id AND cr.status <> 'failed'
	       ORDER BY cr.seq DESC LIMIT 1
	  ) latest
	  CROSS JOIN LATERAL unnest(latest.refs || COALESCE(
	      (SELECT d.refs FROM v2.compile_runs d WHERE d.id = t.delivered_compile_id), '{}')) AS ref
	  CROSS JOIN LATERAL (
	      SELECT CASE WHEN jsonb_typeof(t.settings->'read_by') = 'array'
	                  THEN t.settings->'read_by' ELSE '[]'::jsonb END AS agents
	  ) rb
	 WHERE t.space_id = $1 AND t.sync_state <> 'off' AND t.delivery <> 'mcp'
	   AND NOT (t.root = 'home' AND jsonb_array_length(rb.agents) = 0)
	   AND (EXISTS (
	           SELECT 1 FROM jsonb_array_elements_text(rb.agents) AS r(agent)
	            WHERE r.agent <> ALL (ARRAY['` + strings.Join(hookedAgents, "','") + `']))
	        OR NOT EXISTS (
	           SELECT 1 FROM v2.reads l JOIN v2.compile_runs lc ON lc.id = l.compile_id
	            WHERE l.space_id = $1 AND l.kind = 'compile_load' AND l.read_at >= $2 AND lc.target_id = t.id))`

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
