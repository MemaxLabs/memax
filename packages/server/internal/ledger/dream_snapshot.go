package ledger

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// What a Dream run reads (internal/v2dream). One REPEATABLE READ snapshot
// of the space, in Dream's scope; the plan built from it is re-checked by
// PublishEdition against the record as it is when the edition lands.

// DreamSnapshotOptions bound what a run reads.
type DreamSnapshotOptions struct {
	// MaxNotes is how many notes one edition reads, oldest first; the rest
	// wait for the next one (60).
	MaxNotes int
	// MaxMemories bounds each list of memories (proposals, changed, …).
	MaxMemories int
	// FadeAfter is how long a kept memory goes unread before it fades
	// (60 days).
	FadeAfter time.Duration
	// JudgeGrace: a version without a verdict counts as missed by the judge
	// only once it is older than this (10 minutes).
	JudgeGrace time.Duration
}

// DreamNote is a note a run reads, with its words for the model.
type DreamNote struct {
	NoteRead
	// Ref is its N- ref, if an earlier edition numbered it.
	Ref      string
	Title    string
	Body     string
	External bool
}

// DreamMemory is a memory a run reads, with its words.
type DreamMemory struct {
	ID         uuid.UUID
	Ref        string
	Statement  string
	Kind       Kind
	Section    Section
	Lifecycle  lifecycle.Lifecycle
	Flags      lifecycle.Flags
	Version    int
	Trust      policy.Trust
	Area       string
	InForce    bool
	StaleAfter *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DreamSnapshot is everything one run reads.
type DreamSnapshot struct {
	SpaceID  uuid.UUID
	TenantID uuid.UUID
	Name     string
	Now      time.Time
	// Last is the previous edition (nil before the first).
	Last *DreamEdition
	// Since is the previous edition's Until: record changes after it are
	// this run's input. Nil reads everything.
	Since  *time.Time
	Cursor *NoteCursor
	// Notes are the notes after the cursor, oldest first; MoreNotes says
	// more wait for the next edition.
	Notes     []DreamNote
	MoreNotes bool
	// Changes counts record changes by people, agents and the repository
	// since Since.
	Changes int
	// Proposals wait in Review (not in conflict), oldest first.
	Proposals []DreamMemory
	// Changed are memories kept, edited or restored since Since (by anyone
	// but Dream), and Unjudged the proposals and agents' kept memories
	// whose words the judge never got to or failed on: the conflict
	// phase's input.
	Changed  []DreamMemory
	Unjudged []DreamMemory
	// Decisions are the decisions in force.
	Decisions []DreamMemory
	// StaleDue are kept memories past their stale_after date that nobody
	// has looked at since.
	StaleDue []DreamMemory
	// Brief is the version in force (nil: the space has none), and
	// Unplaced the kept memories it doesn't place or cite.
	Brief    *Brief
	Unplaced []DreamMemory
	// Surfaced are conflicts the judge flagged since Since, still open.
	Surfaced []Surfaced
	// Acted are the memory pairs (and single memories, Related nil) Dream
	// already acted on, keyed by kind, memory, version and related: a pair
	// it flagged or folded once, even if a person undid it, it doesn't
	// raise again for the same words.
	Acted map[DreamActed]bool
}

// DreamActed names something Dream did once.
type DreamActed struct {
	Kind    DreamActionKind
	Memory  uuid.UUID
	Version int
	Related uuid.UUID
}

// HasInput reports whether the space has anything new since the last
// edition: notes, record changes or a stale_after date that came due. No
// input, no run (plan 25 §5.10: empty nights cost nothing).
func (s *DreamSnapshot) HasInput() bool {
	return len(s.Notes) > 0 || s.Changes > 0 || len(s.StaleDue) > 0
}

const dreamMemorySelect = `
	SELECT m.id, m.seq, COALESCE(v.statement, ''), m.kind, m.section, m.lifecycle, m.flags, m.current_version, m.trust,
	       COALESCE(m.decision ->> 'area', ''), COALESCE(m.decision ->> 'status', ''), m.stale_after, m.created_at, m.updated_at
	  FROM v2.memories m
	  JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version`

func scanDreamMemory(r pgx.CollectableRow) (DreamMemory, error) {
	var m DreamMemory
	var seq int64
	var flags []string
	var status string
	if err := r.Scan(&m.ID, &seq, &m.Statement, &m.Kind, &m.Section, &m.Lifecycle, &flags, &m.Version, &m.Trust,
		&m.Area, &status, &m.StaleAfter, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return DreamMemory{}, err
	}
	fs, err := lifecycle.ParseFlags(flags)
	if err != nil {
		return DreamMemory{}, err
	}
	m.Flags, m.Ref = fs, FormatRef(PrefixMemory, seq)
	m.InForce = m.Kind == KindDecision && m.Lifecycle == lifecycle.Kept && (status == "" || status == DecisionInForce)
	return m, nil
}

func dreamMemories(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]DreamMemory, error) {
	rows, err := tx.Query(ctx, dreamMemorySelect+where, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: dream snapshot: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanDreamMemory)
	if err != nil {
		return nil, fmt.Errorf("ledger: dream snapshot: %w", err)
	}
	return out, nil
}

// DreamSnapshot reads what one run of Dream on a space needs.
func (l *Ledger) DreamSnapshot(ctx context.Context, scope Scope, spaceID uuid.UUID, o DreamSnapshotOptions) (*DreamSnapshot, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	if o.MaxNotes <= 0 {
		o.MaxNotes = 60
	}
	if o.MaxMemories <= 0 {
		o.MaxMemories = 200
	}
	if o.FadeAfter <= 0 {
		o.FadeAfter = 60 * 24 * time.Hour
	}
	if o.JudgeGrace <= 0 {
		o.JudgeGrace = 10 * time.Minute
	}
	s := &DreamSnapshot{SpaceID: spaceID, TenantID: g.TenantID, Now: l.now().UTC().Truncate(time.Microsecond), Acted: map[DreamActed]bool{}}
	err := l.readSnapshot(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		sp, err := loadSpace(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		s.Name = sp.Name
		var lastID uuid.UUID
		var cursorAt *time.Time
		var cursorID *uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id, until, note_cursor_at, note_cursor_id FROM v2.dream_editions
		                          WHERE space_id = $1 ORDER BY seq DESC LIMIT 1`, spaceID).Scan(&lastID, &s.Since, &cursorAt, &cursorID)
		switch {
		case errNoRows(err):
			s.Since = nil
		case err != nil:
			return fmt.Errorf("ledger: dream snapshot: %w", err)
		default:
			if s.Last, err = loadEdition(ctx, tx, scope, spaceID, lastID); err != nil {
				return err
			}
			if cursorAt != nil && cursorID != nil {
				s.Cursor = &NoteCursor{At: *cursorAt, ID: *cursorID}
			}
		}
		since := time.Time{}
		if s.Since != nil {
			since = *s.Since
		}

		// Notes after the cursor, oldest first.
		var afterAt time.Time
		afterID := uuid.Nil
		if s.Cursor != nil {
			afterAt, afterID = s.Cursor.At, s.Cursor.ID
		}
		rows, err := tx.Query(ctx, `
			SELECT n.id, n.created_at, n.author_kind, COALESCE(n.agent, ''), COALESCE(n.source, ''), COALESCE(n.title, ''),
			       COALESCE(n.body, ''), r.seq,
			       (COALESCE(n.source, '') IN ('email', 'url', 'link', 'web_clip')
			        OR COALESCE(n.content_type, '') IN ('html', 'url', 'link')
			        OR COALESCE(n.source_path, '') ~* '^https?://')
			  FROM v2.notes n LEFT JOIN v2.note_refs r ON r.note_id = n.id
			 WHERE n.space_id = $1 AND n.state <> 'archived' AND (n.created_at, n.id) > ($2, $3)
			 ORDER BY n.created_at, n.id LIMIT $4`, spaceID, afterAt, afterID, o.MaxNotes+1)
		if err != nil {
			return fmt.Errorf("ledger: dream snapshot: notes: %w", err)
		}
		for rows.Next() {
			var n DreamNote
			var seq *int64
			if err := rows.Scan(&n.ID, &n.CreatedAt, &n.AuthorKind, &n.Agent, &n.Source, &n.Title, &n.Body, &seq, &n.External); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: dream snapshot: notes: %w", err)
			}
			if seq != nil {
				n.Ref = FormatRef(PrefixNote, *seq)
			}
			s.Notes = append(s.Notes, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: dream snapshot: notes: %w", err)
		}
		if len(s.Notes) > o.MaxNotes {
			s.Notes, s.MoreNotes = s.Notes[:o.MaxNotes], true
		}

		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM v2.receipts
			 WHERE space_id = $1 AND recorded_at > $2 AND actor_kind IN ('person', 'agent', 'repository')`,
			spaceID, since).Scan(&s.Changes); err != nil {
			return fmt.Errorf("ledger: dream snapshot: changes: %w", err)
		}
		limit := o.MaxMemories
		if s.Proposals, err = dreamMemories(ctx, tx, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'proposed' AND NOT ('conflict' = ANY (m.flags))
			 ORDER BY m.seq LIMIT $2`, spaceID, limit); err != nil {
			return err
		}
		if s.Changed, err = dreamMemories(ctx, tx, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND NOT ('conflict' = ANY (m.flags))
			   AND EXISTS (SELECT 1 FROM v2.receipts r WHERE r.stream_id = m.id AND r.space_id = m.space_id
			                AND r.recorded_at > $2 AND r.actor_kind IN ('person', 'agent', 'repository')
			                AND r.action IN ('kept', 'edited', 'restored', 'resolved', 'undid'))
			 ORDER BY m.seq DESC LIMIT $3`, spaceID, since, limit); err != nil {
			return err
		}
		if s.Unjudged, err = dreamMemories(ctx, tx, `
			 WHERE m.space_id = $1 AND NOT ('conflict' = ANY (m.flags))
			   AND (m.lifecycle = 'proposed'
			        OR (m.lifecycle = 'kept' AND EXISTS (SELECT 1 FROM v2.receipts r WHERE r.id = v.receipt_id AND r.actor_kind = 'agent')))
			   AND v.created_at < $2
			   AND NOT EXISTS (SELECT 1 FROM v2.judge_verdicts j WHERE j.memory_id = m.id AND j.version = m.current_version
			                    AND j.outcome <> 'failed')
			 ORDER BY m.seq DESC LIMIT $3`, spaceID, s.Now.Add(-o.JudgeGrace), limit); err != nil {
			return err
		}
		if s.Decisions, err = dreamMemories(ctx, tx, `
			 WHERE m.space_id = $1 AND m.kind = 'decision' AND m.lifecycle = 'kept'
			   AND COALESCE(m.decision ->> 'status', '') IN ('', 'in_force')
			 ORDER BY m.seq DESC LIMIT $2`, spaceID, judgeMaxDecisions); err != nil {
			return err
		}
		if s.StaleDue, err = dreamMemories(ctx, tx, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND NOT ('stale' = ANY (m.flags))
			   AND m.stale_after IS NOT NULL AND m.stale_after <= $2
			   AND NOT EXISTS (SELECT 1 FROM v2.receipts r WHERE r.stream_id = m.id AND r.space_id = m.space_id
			                    AND r.actor_kind = 'person' AND r.recorded_at > m.stale_after AND r.id <> m.created_receipt_id)
			 ORDER BY m.stale_after LIMIT $3`, spaceID, s.Now, limit); err != nil {
			return err
		}

		// The Brief in force, and the kept memories it doesn't place.
		cur, err := lockBriefRead(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if cur != nil {
			if s.Brief, err = currentBriefVersion(ctx, tx, spaceID, cur); err != nil {
				return err
			}
			s.Brief.ID, s.Brief.SpaceID = cur.id, spaceID
			s.Brief.Ref = FormatRef(PrefixBrief, cur.seq)
			placed := briefRefs(s.Brief.Sections)
			kept, err := dreamMemories(ctx, tx, ` WHERE m.space_id = $1 AND m.lifecycle = 'kept' ORDER BY m.seq DESC LIMIT $2`,
				spaceID, limit)
			if err != nil {
				return err
			}
			for _, m := range kept {
				if !slices.Contains(placed, m.Ref) {
					s.Unplaced = append(s.Unplaced, m)
				}
			}
		}

		// Conflicts the judge flagged since the last edition, still open.
		rows, err = tx.Query(ctx, `
			SELECT l.from_memory_id, l.to_memory_id
			  FROM v2.memory_links l
			  JOIN v2.receipts r ON r.id = l.receipt_id
			  JOIN v2.memories m ON m.id = l.from_memory_id
			 WHERE l.space_id = $1 AND l.kind = 'conflicts_with' AND l.ended_receipt_id IS NULL
			   AND r.actor_kind = 'memax' AND r.recorded_at > $2 AND 'conflict' = ANY (m.flags)
			 ORDER BY l.created_at LIMIT $3`, spaceID, since, MaxDreamSurfaced)
		if err != nil {
			return fmt.Errorf("ledger: dream snapshot: conflicts: %w", err)
		}
		for rows.Next() {
			var sf Surfaced
			if err := rows.Scan(&sf.Memory, &sf.With); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: dream snapshot: conflicts: %w", err)
			}
			sf.Kind = "conflict"
			s.Surfaced = append(s.Surfaced, sf)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: dream snapshot: conflicts: %w", err)
		}

		// What Dream did before, so it doesn't raise the same thing twice.
		rows, err = tx.Query(ctx, `
			SELECT kind, memory_id, COALESCE(version, 0), COALESCE(related_memory_id, '00000000-0000-0000-0000-000000000000'::uuid)
			  FROM v2.dream_actions WHERE space_id = $1 AND kind IN ('dedupe', 'conflict', 'fade', 'stale') AND memory_id IS NOT NULL`, spaceID)
		if err != nil {
			return fmt.Errorf("ledger: dream snapshot: acted: %w", err)
		}
		for rows.Next() {
			var a DreamActed
			if err := rows.Scan(&a.Kind, &a.Memory, &a.Version, &a.Related); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: dream snapshot: acted: %w", err)
			}
			s.Acted[a] = true
		}
		rows.Close()
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// lockBriefRead reads the Brief's head without a lock (a read snapshot).
func lockBriefRead(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (*briefRow, error) {
	var b briefRow
	err := tx.QueryRow(ctx, `
		SELECT b.id, b.current_version, b.stream_version, v.seq
		  FROM v2.briefs b
		  JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
		 WHERE b.space_id = $1`, spaceID).Scan(&b.id, &b.version, &b.streamVersion, &b.seq)
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load Brief: %w", err)
	}
	return &b, nil
}

// FadeCandidate is a kept memory that may fade: unread, and untouched by
// people and agents, since Unread.
type FadeCandidate struct {
	ID      uuid.UUID
	Ref     string
	Version int
	Unread  time.Time
}

// FadeCandidates lists the kept memories nobody has read or touched for
// `after` (60 days), by ReadStatus: never one in a file whose loads Memax
// can't observe (plan 25 §5.10: otherwise the facts agents use most would
// fade), a decision in force, a flagged memory, or one the Brief places or
// cites. Oldest first, at most limit.
func (l *Ledger) FadeCandidates(ctx context.Context, scope Scope, spaceID uuid.UUID, after time.Duration, limit int) ([]FadeCandidate, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	status, err := l.ReadStatus(ctx, scope, spaceID)
	if err != nil {
		return nil, err
	}
	cut := l.now().Add(-after)
	unread := map[uuid.UUID]*time.Time{}
	var ids []uuid.UUID
	for _, s := range status {
		if s.Unread(cut) {
			unread[s.ID] = s.LastReadAt
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var out []FadeCandidate
	err = l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		var placed []string
		if cur, err := lockBriefRead(ctx, tx, spaceID); err != nil {
			return err
		} else if cur != nil {
			b, err := currentBriefVersion(ctx, tx, spaceID, cur)
			if err != nil {
				return err
			}
			placed = briefRefs(b.Sections)
		}
		rows, err := tx.Query(ctx, `
			SELECT m.id, m.seq, m.current_version, m.kind, COALESCE(m.decision ->> 'status', ''),
			       COALESCE((SELECT max(r.recorded_at) FROM v2.receipts r
			                  WHERE r.stream_id = m.id AND r.space_id = m.space_id AND r.actor_kind IN ('person', 'agent')), m.created_at)
			  FROM v2.memories m
			 WHERE m.space_id = $1 AND m.id = ANY ($2) AND m.lifecycle = 'kept' AND cardinality(m.flags) = 0`, spaceID, ids)
		if err != nil {
			return fmt.Errorf("ledger: fade candidates: %w", err)
		}
		for rows.Next() {
			var c FadeCandidate
			var seq int64
			var kind Kind
			var status string
			var touched time.Time
			if err := rows.Scan(&c.ID, &seq, &c.Version, &kind, &status, &touched); err != nil {
				rows.Close()
				return fmt.Errorf("ledger: fade candidates: %w", err)
			}
			c.Ref = FormatRef(PrefixMemory, seq)
			if kind == KindDecision && (status == "" || status == DecisionInForce) {
				continue
			}
			if slices.Contains(placed, c.Ref) || touched.After(cut) {
				continue
			}
			c.Unread = touched
			if r := unread[c.ID]; r != nil && r.After(c.Unread) {
				c.Unread = *r
			}
			out = append(out, c)
		}
		rows.Close()
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b FadeCandidate) int { return a.Unread.Compare(b.Unread) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
