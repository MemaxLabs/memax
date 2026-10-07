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

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Notes (N-; plan 25 §5.4, §10, D13).
//
// A note is raw material: a V1 memory, a V1 persona or a V1 agent config,
// in a space on the V2 record. Notes are never compiled and never served as
// kept context. A person's own short V1 memories are offered once for bulk
// keep (the switch's V1 import, switch.go); the rest wait for Dream, which
// folds them into proposals (its own branch, v2-dream). The owner can
// search them (SearchNotes: memax_search include_notes, and
// GET /v2/spaces/{space}/notes).
//
// The words stay in the V1 rows until cutover (3.8): v2.notes and
// v2.note_chunks read them, scoped to the transaction's spaces, and
// v2.note_refs (migrations 047 and 048) numbers each note and says what the switch
// did with it. Forget of a note (forget_note.go) deletes its V1 row, as
// V1's own delete does.

// NoteOrigin is the V1 table a note's words live in.
type NoteOrigin string

// The origins.
const (
	NoteFromMemory      NoteOrigin = "memory"
	NoteFromPersona     NoteOrigin = "persona"
	NoteFromAgentConfig NoteOrigin = "agent_config"
)

// NoteOrigins lists every origin.
var NoteOrigins = []NoteOrigin{NoteFromMemory, NoteFromPersona, NoteFromAgentConfig}

// NoteDisposition is what the switch did with a note: the hand-off to
// Review and to Dream.
type NoteDisposition string

// The dispositions.
const (
	// NoteCandidate: a person's own V1 memory, short enough to be one
	// statement, offered for bulk keep through the switch's V1 import.
	NoteCandidate NoteDisposition = "candidate"
	// NoteFold: for Dream to fold into proposals (agent-written V1
	// memories, personas, agent configs, a person's documents longer than
	// one statement, and every note written after the switch).
	NoteFold NoteDisposition = "fold"
	// NoteOnly: it stays a note, searchable and never proposed (archived in
	// V1, or it holds a credential).
	NoteOnly NoteDisposition = "note"
)

// NoteDispositions lists every disposition.
var NoteDispositions = []NoteDisposition{NoteCandidate, NoteFold, NoteOnly}

// NoteHold is why a person's note isn't a bulk-keep candidate.
type NoteHold string

// The holds.
const (
	HoldLong     NoteHold = "long"     // longer than one statement: Dream folds it
	HoldSecret   NoteHold = "secret"   // it holds a credential: never proposed
	HoldArchived NoteHold = "archived" // archived in V1: kept as a note only
	HoldFormat   NoteHold = "format"   // a file or a page, not text a person typed
	HoldExternal NoteHold = "external" // content from the web or an email: Dream proposes it quarantined
)

// NoteHolds lists every hold.
var NoteHolds = []NoteHold{HoldLong, HoldSecret, HoldArchived, HoldFormat, HoldExternal}

// noteNamespace names the notes of personas and agent configs, whose V1
// ids aren't memories': note_id = SHA-1(namespace, origin:id).
var noteNamespace = uuid.MustParse("2a7f43c1-9b6e-4f0a-8d35-5c1e0b9f7a24")

// noteID is a note's id for a V1 row.
func noteID(origin NoteOrigin, v1 uuid.UUID) uuid.UUID {
	if origin == NoteFromMemory {
		return v1
	}
	return uuid.NewSHA1(noteNamespace, []byte(string(origin)+":"+v1.String()))
}

// NoteSourceKey is the locator key a source of kind note names its note by:
// {"note": "<id>"}, as Dream's proposals cite notes too.
const NoteSourceKey = "note"

// Note is one note, as its owner reads it.
type Note struct {
	ID      uuid.UUID `json:"id"`
	Ref     string    `json:"ref,omitempty"`
	SpaceID uuid.UUID `json:"space_id"`
	// OwnerID is who it belongs to in V1: the person (or the person the
	// agent that wrote it worked for).
	OwnerID     uuid.UUID       `json:"owner_id"`
	Origin      NoteOrigin      `json:"origin"`
	Title       string          `json:"title"`
	Excerpt     string          `json:"excerpt"`
	AuthorKind  string          `json:"author_kind"`
	Agent       string          `json:"agent,omitempty"`
	State       string          `json:"state"`
	Disposition NoteDisposition `json:"disposition"`
	Hold        NoteHold        `json:"hold,omitempty"`
	Trust       policy.Trust    `json:"trust,omitempty"`
	// Path is where it came from: a V1 source path, a persona's or agent
	// config's file.
	Path string `json:"path,omitempty"`
	// Length counts its characters (the excerpt is the first 280).
	Length    int       `json:"length"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Score orders search results, best first.
	Score float64 `json:"score,omitempty"`
}

// NoteQuery filters SearchNotes.
type NoteQuery struct {
	SpaceIDs []uuid.UUID
	// Text is what to search for; empty lists the newest notes.
	Text  string
	Limit int
}

// MaxNoteQuery bounds a notes search's words.
const MaxNoteQuery = 500

// notesFilter lists the spaces in scope its person owns. Notes are the
// owner's (plan 25 §10): a person reads the notes they wrote in V1 and, in
// a space they own, every note in it.
func notesFilter(scope Scope) (owned []uuid.UUID) {
	for _, g := range scope.Spaces {
		if g.Role == policy.RoleOwner {
			owned = append(owned, g.SpaceID)
		}
	}
	return owned
}

// SearchNotes searches the notes of the spaces in scope that the scope's
// person may read (their own notes, and every note in a space they own),
// lexically over V1's own index of the words (the chunks' search vector,
// and a trigram match on their text). Notes of personas and agent configs
// match on their title and words. Forgotten notes are gone. Results are
// best first.
func (l *Ledger) SearchNotes(ctx context.Context, scope Scope, q NoteQuery) ([]Note, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return []Note{}, nil
	}
	q.Text = strings.TrimSpace(q.Text)
	if err := checkText("q", q.Text, MaxNoteQuery, false); err != nil {
		return nil, err
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	spaces := q.SpaceIDs
	if len(spaces) == 0 {
		spaces = scope.SpaceIDs()
	}
	var in []uuid.UUID
	for _, id := range spaces {
		if _, ok := scope.Grant(id); ok && !slices.Contains(in, id) {
			in = append(in, id)
		}
	}
	if len(in) == 0 {
		return []Note{}, nil
	}
	owned := notesFilter(scope)
	var out []Note
	err := l.Read(ctx, scope.Narrow(in...), func(tx pgx.Tx) error {
		var rows pgx.Rows
		var err error
		if q.Text == "" {
			rows, err = tx.Query(ctx, noteSelect+`
				 WHERE n.space_id = ANY ($1) AND (n.owner_id = $2 OR n.space_id = ANY ($3))
				 ORDER BY n.created_at DESC, n.id DESC LIMIT $4`, in, scope.PersonID, owned, q.Limit)
		} else {
			// Each note's best chunk, by V1's own index; personas and agent
			// configs (no chunks) by their words. websearch_to_tsquery takes
			// whatever a person types.
			rows, err = tx.Query(ctx, `
				WITH q AS (SELECT websearch_to_tsquery('simple', $5) AS ts, $5::text AS raw),
				hits AS (
				    SELECT c.note_id, max(ts_rank(COALESCE(c.search_vector, ''::tsvector), q.ts)
				                          + COALESCE(similarity(c.search_text, q.raw), 0)) AS score
				      FROM v2.note_chunks c, q
				     WHERE c.space_id = ANY ($1) AND (c.owner_id = $2 OR c.space_id = ANY ($3))
				       AND (c.search_vector @@ q.ts OR strpos(lower(COALESCE(c.search_text, c.content)), lower(q.raw)) > 0)
				     GROUP BY c.note_id
				    UNION ALL
				    SELECT n.id, ts_rank(to_tsvector('simple', n.title || ' ' || n.body), q.ts) + 0.1
				      FROM v2.notes n, q
				     WHERE n.origin <> 'memory' AND n.space_id = ANY ($1) AND (n.owner_id = $2 OR n.space_id = ANY ($3))
				       AND (to_tsvector('simple', n.title || ' ' || n.body) @@ q.ts
				            OR strpos(lower(n.title || ' ' || n.body), lower(q.raw)) > 0)
				    UNION ALL
				    SELECT n.id, 0.05
				      FROM v2.notes n, q
				     WHERE n.origin = 'memory' AND n.space_id = ANY ($1) AND (n.owner_id = $2 OR n.space_id = ANY ($3))
				       AND strpos(lower(n.title), lower(q.raw)) > 0
				),
				best AS (SELECT note_id, max(score) AS score FROM hits GROUP BY note_id ORDER BY 2 DESC LIMIT $4)
				SELECT `+noteColumns+`, best.score
				  FROM best JOIN v2.notes n ON n.id = best.note_id
				 ORDER BY best.score DESC, n.created_at DESC, n.id`, in, scope.PersonID, owned, q.Limit, q.Text)
		}
		if err != nil {
			return fmt.Errorf("ledger: search notes: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Note, error) { return scanNote(r, q.Text != "") })
		if err != nil {
			return fmt.Errorf("ledger: search notes: %w", err)
		}
		return nil
	})
	if out == nil {
		out = []Note{}
	}
	return out, err
}

// GetNote reads one note by its N- ref or id, if the scope's person may
// read it.
func (l *Ledger) GetNote(ctx context.Context, scope Scope, spaceID uuid.UUID, ref string) (*Note, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok || scope.PersonID == uuid.Nil {
		return nil, ErrNotFound
	}
	var n *Note
	err := l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		id, err := resolveNoteRef(ctx, tx, g, ref)
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, noteSelect+` WHERE n.space_id = $1 AND n.id = $2 AND (n.owner_id = $3 OR $4)`,
			spaceID, id, scope.PersonID, g.Role == policy.RoleOwner)
		got, err := scanNote(row, false)
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: read note: %w", err)
		}
		n = &got
		return nil
	})
	return n, err
}

// resolveNoteRef turns "N-0042" or a uuid into a note id in the grant's
// space (and tenant).
func resolveNoteRef(ctx context.Context, tx pgx.Tx, g SpaceGrant, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	p, n, ok := ParseRef(ref)
	if !ok || p != PrefixNote {
		return uuid.Nil, ErrNotFound
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT note_id FROM v2.note_refs WHERE tenant_id = $1 AND seq = $2 AND space_id = $3`,
		g.TenantID, n, g.SpaceID).Scan(&id)
	if errNoRows(err) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("ledger: resolve note: %w", err)
	}
	return id, nil
}

const noteColumns = `n.id, n.seq, n.space_id, n.owner_id, n.origin, COALESCE(n.title, ''), COALESCE(n.excerpt, ''),
	n.author_kind, COALESCE(n.agent, ''), COALESCE(n.state, ''), n.disposition, COALESCE(n.hold, ''), COALESCE(n.trust, ''),
	COALESCE(n.source_path, ''), COALESCE(n.length, 0), n.created_at, n.updated_at`

const noteSelect = `SELECT ` + noteColumns + ` FROM v2.notes n`

func scanNote(r pgx.Row, scored bool) (Note, error) {
	var n Note
	var seq *int64
	dest := []any{&n.ID, &seq, &n.SpaceID, &n.OwnerID, &n.Origin, &n.Title, &n.Excerpt, &n.AuthorKind, &n.Agent, &n.State,
		&n.Disposition, &n.Hold, &n.Trust, &n.Path, &n.Length, &n.CreatedAt, &n.UpdatedAt}
	if scored {
		dest = append(dest, &n.Score)
	}
	if err := r.Scan(dest...); err != nil {
		return Note{}, err
	}
	if seq != nil {
		n.Ref = FormatRef(PrefixNote, *seq)
	}
	return n, nil
}

// ---------------------------------------------------------------------
// Numbering notes (the switch, and Dream's notes written later)
// ---------------------------------------------------------------------

// noteRow is one note to number.
type noteRow struct {
	ID          uuid.UUID
	Origin      NoteOrigin
	V1ID        uuid.UUID
	AuthorKind  string
	AuthorID    uuid.UUID
	Agent       string
	Trust       policy.Trust
	Disposition NoteDisposition
	Hold        NoteHold
}

// numberNotes allocates N- numbers for rows in one block of the tenant's
// counter, in the order given, and writes them, citing receipt (about the
// space). Rows already numbered in the space are skipped (a resumed
// switch). It returns how many it numbered.
func numberNotes(ctx context.Context, tx pgx.Tx, sp spaceRow, receipt uuid.UUID, rows []noteRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	var first int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO v2.id_counters AS c (tenant_id, prefix, next) VALUES ($1, 'N', $2 + 1)
		ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + $2
		RETURNING next - $2`, sp.TenantID, len(rows)).Scan(&first); err != nil {
		return 0, fmt.Errorf("ledger: allocate N- numbers: %w", err)
	}
	const chunk = 1000
	n := 0
	for start := 0; start < len(rows); start += chunk {
		part := rows[start:min(start+chunk, len(rows))]
		ids := make([]uuid.UUID, len(part))
		seqs := make([]int64, len(part))
		origins := make([]string, len(part))
		v1 := make([]uuid.UUID, len(part))
		authors := make([]*string, len(part))
		authorIDs := make([]*uuid.UUID, len(part))
		agents := make([]*string, len(part))
		trusts := make([]*string, len(part))
		disps := make([]string, len(part))
		holds := make([]*string, len(part))
		for i, r := range part {
			ids[i], seqs[i], origins[i], v1[i] = r.ID, first+int64(start+i), string(r.Origin), r.V1ID
			authors[i], agents[i], trusts[i] = strPtr(r.AuthorKind), strPtr(r.Agent), strPtr(string(r.Trust))
			if r.AuthorID != uuid.Nil {
				id := r.AuthorID
				authorIDs[i] = &id
			}
			disps[i], holds[i] = string(r.Disposition), strPtr(string(r.Hold))
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO v2.note_refs (note_id, tenant_id, space_id, seq, origin, v1_id, author_kind, author_id, agent, trust,
			                          disposition, hold, receipt_id, last_receipt_id)
			SELECT u.id, $1, $2, u.seq, u.origin, u.v1, u.author, u.author_id, u.agent, u.trust, u.disp, u.hold, $3, $3
			  FROM unnest($4::uuid[], $5::bigint[], $6::text[], $7::uuid[], $8::text[], $9::uuid[], $10::text[], $11::text[],
			              $12::text[], $13::text[]) AS u(id, seq, origin, v1, author, author_id, agent, trust, disp, hold)
			ON CONFLICT DO NOTHING`,
			sp.TenantID, sp.ID, receipt, ids, seqs, origins, v1, authors, authorIDs, agents, trusts, disps, holds)
		if err != nil {
			return n, fmt.Errorf("ledger: number notes: %w", err)
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// noteLocator is a source locator naming a note.
func noteLocator(id uuid.UUID) json.RawMessage {
	b, _ := json.Marshal(map[string]string{NoteSourceKey: id.String()})
	return b
}

// strPtr is s, or nil when empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
