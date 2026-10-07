package ledger

import (
	"context"
	"encoding/json"
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
	// Brief is the version in force (nil: the space has none), Placed the
	// kept memories it places or cites, by ref, and Unplaced the kept
	// memories it doesn't.
	Brief    *Brief
	Placed   map[string]DreamMemory
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

// queueDreamMemories queues a dreamMemorySelect read into b, collected
// into *out when the batch is read.
func queueDreamMemories(b *pgx.Batch, out *[]DreamMemory, where string, args ...any) {
	b.Queue(dreamMemorySelect+where, args...).Query(func(rows pgx.Rows) error {
		ms, err := pgx.CollectRows(rows, scanDreamMemory)
		if err != nil {
			return fmt.Errorf("ledger: dream snapshot: %w", err)
		}
		*out = ms
		return nil
	})
}

// queueBriefInForce queues a read of the Brief version in force into b:
// *out stays nil when the space has none.
func queueBriefInForce(b *pgx.Batch, spaceID uuid.UUID, out **Brief) {
	b.Queue(`
		SELECT b.id, b.current_version, v.seq, v.title, v.summary, v.structure
		  FROM v2.briefs b
		  JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
		 WHERE b.space_id = $1`, spaceID).QueryRow(func(r pgx.Row) error {
		var br Brief
		var seq int64
		var summary *string
		var structure []byte
		err := r.Scan(&br.ID, &br.Version, &seq, &br.Title, &summary, &structure)
		if errNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ledger: read Brief: %w", err)
		}
		var st briefStructure
		if err := json.Unmarshal(structure, &st); err != nil {
			return fmt.Errorf("ledger: read Brief: %w", err)
		}
		if summary != nil {
			br.Summary = *summary
		}
		br.SpaceID, br.Ref, br.Sections = spaceID, FormatRef(PrefixBrief, seq), st.Sections
		*out = &br
		return nil
	})
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
	// Three round trips, each a batch of reads that don't wait on each
	// other: the space and the last edition's head; then the notes and the
	// record; then the memories the Brief places.
	err := l.readSnapshot(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		found := false
		var cursorAt *time.Time
		var cursorID *uuid.UUID
		b := &pgx.Batch{}
		b.Queue(`SELECT name FROM v2.spaces WHERE id = $1`, spaceID).QueryRow(func(r pgx.Row) error {
			err := r.Scan(&s.Name)
			if errNoRows(err) {
				return nil
			}
			found = err == nil
			return err
		})
		b.Queue(`SELECT until, note_cursor_at, note_cursor_id FROM v2.dream_editions
		          WHERE space_id = $1 ORDER BY seq DESC LIMIT 1`, spaceID).QueryRow(func(r pgx.Row) error {
			var until time.Time
			err := r.Scan(&until, &cursorAt, &cursorID)
			if errNoRows(err) {
				return nil
			}
			if err == nil {
				s.Since = &until
			}
			return err
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("ledger: dream snapshot: %w", err)
		}
		if !found {
			return ErrNotFound
		}
		if cursorAt != nil && cursorID != nil {
			s.Cursor = &NoteCursor{At: *cursorAt, ID: *cursorID}
		}
		since := time.Time{}
		if s.Since != nil {
			since = *s.Since
		}
		var afterAt time.Time
		afterID := uuid.Nil
		if s.Cursor != nil {
			afterAt, afterID = s.Cursor.At, s.Cursor.ID
		}
		limit := o.MaxMemories
		// The first edition doesn't re-check a whole record for conflicts:
		// what changed in the last week is its input.
		changedSince := since
		if s.Since == nil {
			changedSince = s.Now.Add(-7 * 24 * time.Hour)
		}

		b = &pgx.Batch{}
		// Notes after the cursor, oldest first.
		b.Queue(`
			SELECT n.id, n.created_at, n.author_kind, COALESCE(n.agent, ''), COALESCE(n.source, ''), COALESCE(n.title, ''),
			       COALESCE(n.body, ''), r.seq,
			       (COALESCE(n.source, '') IN ('email', 'url', 'link', 'web_clip')
			        OR COALESCE(n.content_type, '') IN ('html', 'url', 'link')
			        OR COALESCE(n.source_path, '') ~* '^https?://')
			  FROM v2.notes n LEFT JOIN v2.note_refs r ON r.note_id = n.id
			 WHERE n.space_id = $1 AND n.state <> 'archived' AND (n.created_at, n.id) > ($2, $3)
			 ORDER BY n.created_at, n.id LIMIT $4`, spaceID, afterAt, afterID, o.MaxNotes+1).
			Query(func(rows pgx.Rows) error {
				var err error
				s.Notes, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DreamNote, error) {
					var n DreamNote
					var seq *int64
					if err := r.Scan(&n.ID, &n.CreatedAt, &n.AuthorKind, &n.Agent, &n.Source, &n.Title, &n.Body, &seq,
						&n.External); err != nil {
						return DreamNote{}, err
					}
					if seq != nil {
						n.Ref = FormatRef(PrefixNote, *seq)
					}
					return n, nil
				})
				if err != nil {
					return fmt.Errorf("ledger: dream snapshot: notes: %w", err)
				}
				return nil
			})
		b.Queue(`
			SELECT count(*) FROM v2.receipts
			 WHERE space_id = $1 AND recorded_at > $2 AND actor_kind IN ('person', 'agent', 'repository')`,
			spaceID, since).QueryRow(func(r pgx.Row) error { return r.Scan(&s.Changes) })
		queueDreamMemories(b, &s.Proposals, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'proposed' AND NOT ('conflict' = ANY (m.flags))
			 ORDER BY m.seq LIMIT $2`, spaceID, limit)
		queueDreamMemories(b, &s.Changed, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND NOT ('conflict' = ANY (m.flags))
			   AND EXISTS (SELECT 1 FROM v2.receipts r WHERE r.stream_id = m.id AND r.space_id = m.space_id
			                AND r.recorded_at > $2 AND r.actor_kind IN ('person', 'agent', 'repository')
			                AND r.action IN ('kept', 'edited', 'restored', 'resolved', 'undid'))
			 ORDER BY m.seq DESC LIMIT $3`, spaceID, changedSince, limit)
		queueDreamMemories(b, &s.Unjudged, `
			 WHERE m.space_id = $1 AND NOT ('conflict' = ANY (m.flags))
			   AND (m.lifecycle = 'proposed'
			        OR (m.lifecycle = 'kept' AND EXISTS (SELECT 1 FROM v2.receipts r WHERE r.id = v.receipt_id AND r.actor_kind = 'agent')))
			   AND v.created_at < $2
			   AND NOT EXISTS (SELECT 1 FROM v2.judge_verdicts j WHERE j.memory_id = m.id AND j.version = m.current_version
			                    AND j.outcome <> 'failed')
			 ORDER BY m.seq DESC LIMIT $3`, spaceID, s.Now.Add(-o.JudgeGrace), limit)
		queueDreamMemories(b, &s.Decisions, `
			 WHERE m.space_id = $1 AND m.kind = 'decision' AND m.lifecycle = 'kept'
			   AND COALESCE(m.decision ->> 'status', '') IN ('', 'in_force')
			 ORDER BY m.seq DESC LIMIT $2`, spaceID, judgeMaxDecisions)
		queueDreamMemories(b, &s.StaleDue, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND NOT ('stale' = ANY (m.flags))
			   AND m.stale_after IS NOT NULL AND m.stale_after <= $2
			   AND NOT EXISTS (SELECT 1 FROM v2.receipts r WHERE r.stream_id = m.id AND r.space_id = m.space_id
			                    AND r.actor_kind = 'person' AND r.recorded_at > m.stale_after AND r.id <> m.created_receipt_id)
			 ORDER BY m.stale_after LIMIT $3`, spaceID, s.Now, limit)
		// The Brief in force.
		queueBriefInForce(b, spaceID, &s.Brief)
		// Conflicts the judge flagged since the last edition, still open.
		b.Queue(`
			SELECT l.from_memory_id, l.to_memory_id
			  FROM v2.memory_links l
			  JOIN v2.receipts r ON r.id = l.receipt_id
			  JOIN v2.memories m ON m.id = l.from_memory_id
			 WHERE l.space_id = $1 AND l.kind = 'conflicts_with' AND l.ended_receipt_id IS NULL
			   AND r.actor_kind = 'memax' AND r.recorded_at > $2 AND 'conflict' = ANY (m.flags)
			 ORDER BY l.created_at LIMIT $3`, spaceID, since, MaxDreamSurfaced).
			Query(func(rows pgx.Rows) error {
				var err error
				s.Surfaced, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Surfaced, error) {
					sf := Surfaced{Kind: "conflict"}
					err := r.Scan(&sf.Memory, &sf.With)
					return sf, err
				})
				if err != nil {
					return fmt.Errorf("ledger: dream snapshot: conflicts: %w", err)
				}
				return nil
			})
		// What Dream did before, so it doesn't raise the same thing twice.
		b.Queue(`
			SELECT kind, memory_id, COALESCE(version, 0), COALESCE(related_memory_id, '00000000-0000-0000-0000-000000000000'::uuid)
			  FROM v2.dream_actions WHERE space_id = $1 AND kind IN ('dedupe', 'conflict', 'fade', 'stale') AND memory_id IS NOT NULL`,
			spaceID).Query(func(rows pgx.Rows) error {
			var a DreamActed
			_, err := pgx.ForEachRow(rows, []any{&a.Kind, &a.Memory, &a.Version, &a.Related}, func() error {
				s.Acted[a] = true
				return nil
			})
			if err != nil {
				return fmt.Errorf("ledger: dream snapshot: acted: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err
		}
		if len(s.Notes) > o.MaxNotes {
			s.Notes, s.MoreNotes = s.Notes[:o.MaxNotes], true
		}
		if s.Brief == nil {
			return nil
		}

		// The kept memories the Brief places and those it doesn't.
		placed := briefRefs(s.Brief.Sections)
		seqs := make([]int64, 0, len(placed))
		for _, r := range placed {
			_, n, _ := ParseRef(r)
			seqs = append(seqs, n)
		}
		var in []DreamMemory
		b = &pgx.Batch{}
		queueDreamMemories(b, &in, ` WHERE m.space_id = $1 AND m.seq = ANY ($2)`, spaceID, seqs)
		queueDreamMemories(b, &s.Unplaced, `
			 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND NOT (m.seq = ANY ($2))
			 ORDER BY m.updated_at DESC, m.seq DESC LIMIT $3`, spaceID, seqs, limit)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err
		}
		s.Placed = map[string]DreamMemory{}
		for _, m := range in {
			s.Placed[m.Ref] = m
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
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
// cites, or one side of an open conflict. Oldest first, at most limit.
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
	type row struct {
		c       FadeCandidate
		kind    Kind
		status  string
		touched time.Time
	}
	var brief *Brief
	var rows []row
	b := &pgx.Batch{}
	queueBriefInForce(b, spaceID, &brief)
	b.Queue(`
		SELECT m.id, m.seq, m.current_version, m.kind, COALESCE(m.decision ->> 'status', ''),
		       COALESCE((SELECT max(r.recorded_at) FROM v2.receipts r
		                  WHERE r.stream_id = m.id AND r.space_id = m.space_id AND r.actor_kind IN ('person', 'agent')), m.created_at)
		  FROM v2.memories m
		 WHERE m.space_id = $1 AND m.id = ANY ($2) AND m.lifecycle = 'kept' AND cardinality(m.flags) = 0
		   AND NOT EXISTS (SELECT 1 FROM v2.memory_links l
		                    WHERE l.kind = 'conflicts_with' AND l.ended_receipt_id IS NULL
		                      AND (l.from_memory_id = m.id OR l.to_memory_id = m.id))`, spaceID, ids).
		Query(func(rs pgx.Rows) error {
			var err error
			rows, err = pgx.CollectRows(rs, func(r pgx.CollectableRow) (row, error) {
				var x row
				var seq int64
				err := r.Scan(&x.c.ID, &seq, &x.c.Version, &x.kind, &x.status, &x.touched)
				x.c.Ref = FormatRef(PrefixMemory, seq)
				return x, err
			})
			if err != nil {
				return fmt.Errorf("ledger: fade candidates: %w", err)
			}
			return nil
		})
	if err := l.ReadBatch(ctx, scope.Narrow(spaceID), b); err != nil {
		return nil, err
	}
	var placed []string
	if brief != nil {
		placed = briefRefs(brief.Sections)
	}
	var out []FadeCandidate
	for _, x := range rows {
		if x.kind == KindDecision && (x.status == "" || x.status == DecisionInForce) {
			continue
		}
		if slices.Contains(placed, x.c.Ref) || x.touched.After(cut) {
			continue
		}
		c := x.c
		c.Unread = x.touched
		if r := unread[c.ID]; r != nil && r.After(c.Unread) {
			c.Unread = *r
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b FadeCandidate) int { return a.Unread.Compare(b.Unread) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
