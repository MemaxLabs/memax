package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reading editions back. An edition holds ids and counts; the words of the
// memories it names are read live from memory_versions, so a memory
// forgotten since shows as forgotten, with no words.

const editionSelect = `
	SELECT e.id, e.seq, e.space_id, e.slot, e.trigger, e.requested_by, e.since, e.until, e.started_at, e.finished_at,
	       e.notes_read, e.stats, e.surfaced, e.receipt_id
	  FROM v2.dream_editions e`

func scanEdition(r pgx.Row) (*DreamEdition, error) {
	var e DreamEdition
	var stats, surfaced []byte
	if err := r.Scan(&e.ID, &e.seq, &e.SpaceID, &e.Slot, &e.Trigger, &e.RequestedBy, &e.Since, &e.Until,
		&e.StartedAt, &e.FinishedAt, &e.notes, &stats, &surfaced, &e.ReceiptID); err != nil {
		return nil, err
	}
	e.N, e.Ref = e.seq, FormatRef(PrefixDream, e.seq)
	if err := json.Unmarshal(stats, &e.Stats); err != nil {
		return nil, fmt.Errorf("ledger: edition %s stats: %w", e.Ref, err)
	}
	if err := json.Unmarshal(surfaced, &e.surfaced); err != nil {
		return nil, fmt.Errorf("ledger: edition %s: %w", e.Ref, err)
	}
	e.NotesRead = len(e.notes)
	e.NotesBy = notesBy(e.Stats.Notes)
	e.Counts = map[DreamActionKind]int{}
	return &e, nil
}

// notesBy counts notes by who wrote them: each agent, people, and chats.
func notesBy(notes []NoteRead) []NoteAuthorCount {
	var out []NoteAuthorCount
	add := func(kind, agent string) {
		for i := range out {
			if out[i].Kind == kind && out[i].Agent == agent {
				out[i].Count++
				return
			}
		}
		out = append(out, NoteAuthorCount{Kind: kind, Agent: agent, Count: 1})
	}
	for _, n := range notes {
		switch {
		case n.Agent != "":
			add("agent", n.Agent)
		case strings.Contains(strings.ToLower(n.Source), "chat"):
			add("chat", "")
		case n.AuthorKind == "agent":
			add("agent", "")
		default:
			add("person", "")
		}
	}
	slices.SortStableFunc(out, func(a, b NoteAuthorCount) int { return b.Count - a.Count })
	return nonNilSlice(out)
}

// attachEditionFacts fills each edition's counts, note refs, fact refs and
// what still needs a person: three reads for the whole batch, in one round
// trip.
func attachEditionFacts(ctx context.Context, tx pgx.Tx, es []*DreamEdition) error {
	if len(es) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(es))
	var notes, surfaced []uuid.UUID
	byID := map[uuid.UUID]*DreamEdition{}
	for i, e := range es {
		ids[i] = e.ID
		notes = append(notes, e.notes...)
		for _, s := range e.surfaced {
			surfaced = append(surfaced, s.Memory)
		}
		byID[e.ID] = e
		e.FactRefs, e.NoteRefs = []string{}, []string{}
	}
	type actionFact struct {
		edition      uuid.UUID
		kind         DreamActionKind
		seq          *int64
		undone, need bool
	}
	var facts []actionFact
	flagged := map[uuid.UUID]bool{}
	refs := map[uuid.UUID]string{}
	b := &pgx.Batch{}
	b.Queue(`
		SELECT a.edition_id, a.kind, m.seq, a.undone_by IS NOT NULL,
		       a.kind IN ('conflict', 'stale') AND a.undone_by IS NULL
		         AND ((a.kind = 'conflict' AND 'conflict' = ANY (m.flags)) OR (a.kind = 'stale' AND 'stale' = ANY (m.flags)))
		  FROM v2.dream_actions a
		  LEFT JOIN v2.memories m ON m.id = a.memory_id
		 WHERE a.edition_id = ANY ($1)
		 ORDER BY a.edition_id, a.n`, ids).Query(func(rows pgx.Rows) error {
		var err error
		facts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (actionFact, error) {
			var f actionFact
			err := r.Scan(&f.edition, &f.kind, &f.seq, &f.undone, &f.need)
			return f, err
		})
		if err != nil {
			return fmt.Errorf("ledger: read dream actions: %w", err)
		}
		return nil
	})
	// What an edition surfaced still needs a person while it is flagged.
	if len(surfaced) > 0 {
		b.Queue(`SELECT id FROM v2.memories WHERE id = ANY ($1) AND 'conflict' = ANY (flags)`, surfaced).
			Query(func(rows pgx.Rows) error {
				var id uuid.UUID
				_, err := pgx.ForEachRow(rows, []any{&id}, func() error {
					flagged[id] = true
					return nil
				})
				if err != nil {
					return fmt.Errorf("ledger: read surfaced: %w", err)
				}
				return nil
			})
	}
	if len(notes) > 0 {
		b.Queue(`SELECT note_id, seq FROM v2.note_refs WHERE note_id = ANY ($1)`, notes).Query(func(rows pgx.Rows) error {
			var id uuid.UUID
			var seq int64
			_, err := pgx.ForEachRow(rows, []any{&id, &seq}, func() error {
				refs[id] = FormatRef(PrefixNote, seq)
				return nil
			})
			if err != nil {
				return fmt.Errorf("ledger: read note refs: %w", err)
			}
			return nil
		})
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return err
	}
	for _, f := range facts {
		e := byID[f.edition]
		e.Counts[f.kind]++
		if f.undone {
			e.Undone++
		}
		if f.need {
			e.NeedsYou++
		}
		if (f.kind == DreamFold || f.kind == DreamPropose) && !f.undone && f.seq != nil {
			if ref := FormatRef(PrefixMemory, *f.seq); !slices.Contains(e.FactRefs, ref) {
				e.FactRefs = append(e.FactRefs, ref)
			}
		}
	}
	for _, e := range es {
		for _, s := range e.surfaced {
			if flagged[s.Memory] {
				e.NeedsYou++
			}
		}
		for _, n := range e.notes {
			if r := refs[n]; r != "" {
				e.NoteRefs = append(e.NoteRefs, r)
			}
		}
	}
	return nil
}

// loadEdition reads one edition of a space, with its counts.
func loadEdition(ctx context.Context, tx pgx.Tx, scope Scope, spaceID, id uuid.UUID) (*DreamEdition, error) {
	e, err := scanEdition(tx.QueryRow(ctx, editionSelect+` WHERE e.id = $1 AND e.space_id = $2 AND e.space_id = ANY ($3)`,
		id, spaceID, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read edition: %w", err)
	}
	return e, attachEditionFacts(ctx, tx, []*DreamEdition{e})
}

// EditionQuery pages a space's editions, newest first.
type EditionQuery struct {
	SpaceID uuid.UUID
	Limit   int
	Cursor  string
}

// EditionPage is a page of editions.
type EditionPage struct {
	Editions   []DreamEdition `json:"editions"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor,omitempty"`
	// Schedule is when the next edition is due, if the space has a
	// schedule yet (the sweep makes one for every V2 space).
	Schedule *DreamSchedule `json:"schedule,omitempty"`
}

// DreamSchedule is when a space's next edition is due.
type DreamSchedule struct {
	Cadence  string    `json:"cadence"`
	TimeZone string    `json:"time_zone"`
	NextAt   time.Time `json:"next_at"`
}

// ListEditions lists a space's editions, newest first, with its schedule.
func (l *Ledger) ListEditions(ctx context.Context, scope Scope, q EditionQuery) (EditionPage, error) {
	if l == nil {
		return EditionPage{}, ErrDisabled
	}
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return EditionPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return EditionPage{}, err
	}
	after, err := decodeCursor(q.Cursor, 'd')
	if err != nil {
		return EditionPage{}, err
	}
	var page EditionPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		// The page and the schedule in one round trip, then the editions'
		// facts in another.
		var es []*DreamEdition
		b := &pgx.Batch{}
		b.Queue(editionSelect+`
			 WHERE e.space_id = $1 AND e.space_id = ANY ($2) AND ($3::bigint = 0 OR e.seq < $3)
			 ORDER BY e.seq DESC LIMIT $4`, q.SpaceID, scope.SpaceIDs(), after, limit+1).Query(func(rows pgx.Rows) error {
			var err error
			es, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*DreamEdition, error) { return scanEdition(r) })
			if err != nil {
				return fmt.Errorf("ledger: list editions: %w", err)
			}
			return nil
		})
		queueSchedule(b, q.SpaceID, &page.Schedule)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err
		}
		if len(es) > limit {
			es, page.HasMore = es[:limit], true
			page.NextCursor = encodeCursor('d', es[limit-1].seq)
		}
		if err := attachEditionFacts(ctx, tx, es); err != nil {
			return err
		}
		page.Editions = make([]DreamEdition, len(es))
		for i, e := range es {
			page.Editions[i] = *e
		}
		return nil
	})
	return page, err
}

// queueSchedule queues a read of the space's schedule into b; *out stays
// nil until the sweep has made one.
func queueSchedule(b *pgx.Batch, spaceID uuid.UUID, out **DreamSchedule) {
	b.Queue(`SELECT cadence, time_zone, due_at FROM v2.dream_schedules WHERE space_id = $1`, spaceID).
		QueryRow(func(r pgx.Row) error {
			var s DreamSchedule
			err := r.Scan(&s.Cadence, &s.TimeZone, &s.NextAt)
			if errNoRows(err) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("ledger: read the schedule: %w", err)
			}
			*out = &s
			return nil
		})
}

// GetEdition reads one edition of a space (by D- ref, uuid or "latest"),
// with every action and what it surfaced, each with its memory as it is
// now.
func (l *Ledger) GetEdition(ctx context.Context, scope Scope, spaceID uuid.UUID, ref string) (*DreamEdition, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out *DreamEdition
	now := l.now()
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		where, args, err := editionWhere(spaceID, ref)
		if err != nil {
			return err
		}
		e, err := scanEdition(tx.QueryRow(ctx, editionSelect+where, args...))
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: read edition: %w", err)
		}
		if err := attachEditionFacts(ctx, tx, []*DreamEdition{e}); err != nil {
			return err
		}
		var surfaced []uuid.UUID
		for _, s := range e.surfaced {
			surfaced = append(surfaced, s.Memory)
			if s.With != uuid.Nil {
				surfaced = append(surfaced, s.With)
			}
		}
		as, err := scanEditionActions(ctx, tx, scope, e.ID, "", 0, MaxDreamActions)
		if err != nil {
			return err
		}
		ms, err := attachActionDetails(ctx, tx, scope, as, surfaced...)
		if err != nil {
			return err
		}
		e.Actions = make([]DreamAction, len(as))
		for i, a := range as {
			e.Actions[i] = *a
			e.Actions[i].Undoable = dreamUndoable(&e.Actions[i], l.dreamUndoWindow, now)
		}
		e.Surfaced = []SurfacedMemory{}
		for _, s := range e.surfaced {
			if ms[s.Memory] == nil {
				continue
			}
			e.Surfaced = append(e.Surfaced, SurfacedMemory{Kind: s.Kind, Memory: ms[s.Memory], With: ms[s.With]})
		}
		out = e
		return nil
	})
	return out, err
}

// editionWhere is the WHERE clause (and its arguments) that finds one of a
// space's editions by "D-0214", 214, a uuid or "latest".
func editionWhere(spaceID uuid.UUID, ref string) (string, []any, error) {
	ref = strings.TrimSpace(ref)
	if strings.EqualFold(ref, "latest") {
		return ` WHERE e.space_id = $1 ORDER BY e.seq DESC LIMIT 1`, []any{spaceID}, nil
	}
	if parsed, err := uuid.Parse(ref); err == nil {
		return ` WHERE e.space_id = $1 AND e.id = $2`, []any{spaceID, parsed}, nil
	}
	p, n, ok := ParseRef(ref)
	if !ok && ref != "" && strings.Trim(ref, "0123456789") == "" {
		p, ok = PrefixDream, true
		_, _ = fmt.Sscan(ref, &n)
	}
	if !ok || p != PrefixDream || n < 1 {
		return "", nil, invalid("edition", "use an edition's number (214), its ID (D-0214) or latest")
	}
	return ` WHERE e.space_id = $1 AND e.seq = $2`, []any{spaceID, n}, nil
}

// resolveEdition turns "D-0214", a uuid or "latest" into an edition id.
func resolveEdition(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, ref string) (uuid.UUID, error) {
	where, args, err := editionWhere(spaceID, ref)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `SELECT e.id FROM v2.dream_editions e`+where, args...).Scan(&id)
	if errNoRows(err) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("ledger: find edition: %w", err)
	}
	return id, nil
}

const dreamActionSelect = `
	SELECT a.id, a.space_id, a.edition_id, e.seq, a.n, a.kind, a.memory_id, COALESCE(a.version, 0), a.related_memory_id,
	       a.brief_id, COALESCE(a.brief_version, 0), bv.seq, a.note_ids, a.receipt_ids, a.inverse, a.undone_by, a.undone_at,
	       u.actor_id, a.receipt_id, a.created_at
	  FROM v2.dream_actions a
	  JOIN v2.dream_editions e ON e.id = a.edition_id
	  LEFT JOIN v2.brief_versions bv ON bv.brief_id = a.brief_id AND bv.version = a.brief_version
	  LEFT JOIN v2.receipts u ON u.id = a.undone_by`

func scanDreamAction(r pgx.Row) (*DreamAction, error) {
	var a DreamAction
	var editionSeq int64
	var memoryID, relatedID, briefID, undoneBy, undoneActor *uuid.UUID
	var briefSeq *int64
	var undoneAt *time.Time
	if err := r.Scan(&a.ID, &a.spaceID, &a.EditionID, &editionSeq, &a.N, &a.Kind, &memoryID, &a.Version, &relatedID,
		&briefID, &a.briefVersion, &briefSeq, &a.noteIDs, &a.ReceiptIDs, &a.inverse, &undoneBy, &undoneAt,
		&undoneActor, &a.receiptID, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.EditionRef = FormatRef(PrefixDream, editionSeq)
	if memoryID != nil {
		a.memoryID = *memoryID
	}
	if relatedID != nil {
		a.relatedID = *relatedID
	}
	if briefID != nil {
		a.briefID = *briefID
		var inv dreamInverse
		_ = json.Unmarshal(a.inverse, &inv)
		a.Brief = &DreamBriefChange{Version: a.briefVersion, Ops: inv.BriefOps}
		if briefSeq != nil {
			a.Brief.Ref = FormatRef(PrefixBrief, *briefSeq)
		}
	}
	if undoneBy != nil && undoneAt != nil {
		a.Undone = &DreamUndone{ReceiptID: *undoneBy, By: undoneActor, At: *undoneAt}
	}
	return &a, nil
}

// loadDreamAction reads one action in scope (lock: FOR UPDATE), with its
// memories.
func loadDreamAction(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, lock bool) (*DreamAction, error) {
	q := dreamActionSelect + ` WHERE a.id = $1 AND a.space_id = ANY ($2)`
	if lock {
		q += ` FOR UPDATE OF a`
	}
	a, err := scanDreamAction(tx.QueryRow(ctx, q, id, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read dream action: %w", err)
	}
	if _, err := attachActionDetails(ctx, tx, scope, []*DreamAction{a}); err != nil {
		return nil, err
	}
	return a, nil
}

// editionActions reads an edition's actions in order (kind filters).
func editionActions(ctx context.Context, tx pgx.Tx, scope Scope, editionID uuid.UUID, kind DreamActionKind, after, limit int) ([]DreamAction, error) {
	as, err := scanEditionActions(ctx, tx, scope, editionID, kind, after, limit)
	if err != nil {
		return nil, err
	}
	if _, err := attachActionDetails(ctx, tx, scope, as); err != nil {
		return nil, err
	}
	out := make([]DreamAction, len(as))
	for i, a := range as {
		out[i] = *a
	}
	return out, nil
}

// scanEditionActions reads an edition's action rows, without their
// memories (attachActionDetails).
func scanEditionActions(ctx context.Context, tx pgx.Tx, scope Scope, editionID uuid.UUID, kind DreamActionKind, after, limit int) ([]*DreamAction, error) {
	rows, err := tx.Query(ctx, dreamActionSelect+`
		 WHERE a.edition_id = $1 AND a.space_id = ANY ($2) AND ($3 = '' OR a.kind = $3) AND a.n > $4
		 ORDER BY a.n LIMIT $5`, editionID, scope.SpaceIDs(), string(kind), after, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: read dream actions: %w", err)
	}
	as, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*DreamAction, error) { return scanDreamAction(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: read dream actions: %w", err)
	}
	return as, nil
}

// attachActionDetails fills each action's memories and note refs, and
// returns the memories it read, extra (the surfaced ones) included. The
// memories and the note refs go out together, then the memories' details.
func attachActionDetails(ctx context.Context, tx pgx.Tx, scope Scope, as []*DreamAction, extra ...uuid.UUID) (map[uuid.UUID]*Memory, error) {
	ids := slices.Clone(extra)
	var notes []uuid.UUID
	for _, a := range as {
		for _, id := range []uuid.UUID{a.memoryID, a.relatedID} {
			if id != uuid.Nil {
				ids = append(ids, id)
			}
		}
		notes = append(notes, a.noteIDs...)
	}
	var list []*Memory
	refs := map[uuid.UUID]string{}
	b := &pgx.Batch{}
	if len(ids) > 0 {
		b.Queue(memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2)`, ids, scope.SpaceIDs()).
			Query(func(rows pgx.Rows) error {
				var err error
				list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
				if err != nil {
					return fmt.Errorf("ledger: read memories: %w", err)
				}
				return nil
			})
	}
	if len(notes) > 0 {
		b.Queue(`SELECT note_id, seq FROM v2.note_refs WHERE note_id = ANY ($1)`, notes).Query(func(rows pgx.Rows) error {
			var id uuid.UUID
			var seq int64
			_, err := pgx.ForEachRow(rows, []any{&id, &seq}, func() error {
				refs[id] = FormatRef(PrefixNote, seq)
				return nil
			})
			if err != nil {
				return fmt.Errorf("ledger: read note refs: %w", err)
			}
			return nil
		})
	}
	if b.Len() > 0 {
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return nil, err
		}
	}
	if err := attachDetails(ctx, tx, list); err != nil {
		return nil, err
	}
	ms := make(map[uuid.UUID]*Memory, len(list))
	for _, m := range list {
		ms[m.ID] = m
	}
	for _, a := range as {
		a.Memory, a.Related = ms[a.memoryID], ms[a.relatedID]
		a.NoteRefs = make([]string, 0, len(a.noteIDs))
		for _, n := range a.noteIDs {
			if r := refs[n]; r != "" {
				a.NoteRefs = append(a.NoteRefs, r)
			}
		}
		slices.Sort(a.NoteRefs)
	}
	return ms, nil
}

// DreamActionQuery pages an edition's actions, in the edition's order.
type DreamActionQuery struct {
	SpaceID uuid.UUID
	Edition string
	Kind    DreamActionKind
	Limit   int
	Cursor  string
}

// DreamActionPage is a page of actions.
type DreamActionPage struct {
	Actions    []DreamAction `json:"actions"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ListDreamActions lists an edition's actions in order, of one kind or all.
func (l *Ledger) ListDreamActions(ctx context.Context, scope Scope, q DreamActionQuery) (DreamActionPage, error) {
	if l == nil {
		return DreamActionPage{}, ErrDisabled
	}
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return DreamActionPage{}, ErrNotFound
	}
	if q.Kind != "" && !q.Kind.Valid() {
		return DreamActionPage{}, invalid("kind", "use fold, propose, dedupe, conflict, stale, fade or brief")
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return DreamActionPage{}, err
	}
	after, err := decodeCursor(q.Cursor, 'a')
	if err != nil {
		return DreamActionPage{}, err
	}
	now := l.now()
	var page DreamActionPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveEdition(ctx, tx, q.SpaceID, q.Edition)
		if err != nil {
			return err
		}
		as, err := editionActions(ctx, tx, scope, id, q.Kind, int(after), limit+1)
		if err != nil {
			return err
		}
		if len(as) > limit {
			as, page.HasMore = as[:limit], true
			page.NextCursor = encodeCursor('a', int64(as[limit-1].N))
		}
		for i := range as {
			as[i].Undoable = dreamUndoable(&as[i], l.dreamUndoWindow, now)
		}
		page.Actions = nonNilSlice(as)
		return nil
	})
	return page, err
}

// GetDreamAction reads one of Dream's actions.
func (l *Ledger) GetDreamAction(ctx context.Context, scope Scope, id uuid.UUID) (*DreamAction, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *DreamAction
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		a, err := loadDreamAction(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		a.Undoable = dreamUndoable(a, l.dreamUndoWindow, l.now())
		out = a
		return nil
	})
	return out, err
}
