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

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Forgetting a note (rule 7 for N-; migration 048).
//
// A note's words live in one V1 row (a memory with its chunks, a persona,
// an agent config) until cutover. ForgetNote takes them out of Memax in one
// transaction, as Forget does a memory's:
//
//   - a forgot receipt about the note (object_kind note, its N- ref);
//   - v2.purge_note_words, which runs only beside that receipt, deletes the
//     V1 row as V1's own delete does (chunks, attachment rows and topic
//     links cascade) and scrubs what V1 derived from it that names it: the
//     board cards citing it, notifications about it, Dream's reasons for
//     actions on it, and the activity log's summary of its title;
//   - the memories carrying its words go with it, named by the person
//     (Carries) or the command is refused with *ForgetCarriesError: the
//     bulk-keep proposal the switch made of it, Dream's proposals citing it,
//     and what carries theirs, each purged as Forget purges a memory;
//   - its tombstone, the steps of its propagation (the targets that held a
//     carried memory, the stored copies, its attachments' objects, the
//     caches and the forget ledger's copy) and a notice for every agent
//     connected to the space, as Forget writes them.
//
// A deferred trigger refuses the commit if the forgotten note's V1 row is
// still there, or it has no tombstone. A note the switch didn't number (one
// written after it) is numbered in the same transaction, beside its forgot
// receipt.

// CommandForgetNote names ForgetNote.
const CommandForgetNote CommandName = "forget_note"

// ForgetNote forgets a note everywhere.
type ForgetNote struct {
	Meta
	SpaceID uuid.UUID
	// Note is its display ID ("N-0042") or id.
	Note string
	// Remark is the person's own note on the tombstone.
	Remark string
	// Carries are the memories the person saw going with it.
	Carries []string
}

// Name implements Command.
func (*ForgetNote) Name() CommandName { return CommandForgetNote }

func (c *ForgetNote) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space the note is in")
	}
	if strings.TrimSpace(c.Note) == "" {
		return invalid("note", "say which note, by display ID (N-0042) or id")
	}
	c.Remark = strings.TrimSpace(c.Remark)
	if err := checkText("note", c.Remark, MaxNoteRunes, false); err != nil {
		return err
	}
	if len(c.Carries) > MaxCarried {
		return invalid("carries", "at most %d", MaxCarried)
	}
	for _, r := range c.Carries {
		if p, _, ok := ParseRef(r); !ok || p != PrefixMemory {
			return invalid("carries", "%q isn't a memory's display ID", r)
		}
	}
	return nil
}

// noteTarget is a note as Forget locks it.
type noteTarget struct {
	ID       uuid.UUID
	Ref      string
	Origin   NoteOrigin
	V1ID     uuid.UUID
	OwnerID  uuid.UUID
	Stream   int
	Numbered bool
	// Seq is the number allocated for a note the switch didn't number.
	Seq int64
	Row noteRow
}

// lockNote finds a note of the space by ref or id, locked, as the person
// may read it (their own, or any in a space they own). A note the switch
// didn't number comes back unnumbered.
func lockNote(ctx context.Context, tx pgx.Tx, g SpaceGrant, person uuid.UUID, ref string) (*noteTarget, error) {
	id, err := resolveNoteRef(ctx, tx, g, ref)
	if err != nil {
		return nil, err
	}
	n := &noteTarget{ID: id}
	var forgotten *time.Time
	var seq int64
	err = tx.QueryRow(ctx, `
		SELECT seq, origin, v1_id, COALESCE(author_id, '00000000-0000-0000-0000-000000000000'), stream_version, forgotten_at
		  FROM v2.note_refs WHERE note_id = $1 AND space_id = $2 FOR UPDATE`, id, g.SpaceID).
		Scan(&seq, &n.Origin, &n.V1ID, &n.OwnerID, &n.Stream, &forgotten)
	switch {
	case err == nil:
		n.Numbered, n.Ref = true, FormatRef(PrefixNote, seq)
		if forgotten != nil {
			return nil, &TransitionError{Ref: n.Ref, Err: &lifecycle.TransitionError{
				Verb: lifecycle.VerbForget, Message: "it is forgotten already; nothing is left to forget"}}
		}
	case errNoRows(err):
		n.Origin, n.V1ID = NoteFromMemory, id
	default:
		return nil, fmt.Errorf("ledger: lock note: %w", err)
	}
	// Its words, as the person may read them: the note must still exist.
	var owner uuid.UUID
	var authorKind, agent string
	err = tx.QueryRow(ctx, `SELECT owner_id, author_kind, COALESCE(agent, '') FROM v2.notes WHERE id = $1 AND space_id = $2`,
		id, g.SpaceID).Scan(&owner, &authorKind, &agent)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: lock note: %w", err)
	}
	if g.Role != policy.RoleOwner && owner != person {
		return nil, ErrNotFound
	}
	if n.OwnerID == uuid.Nil {
		n.OwnerID = owner
	}
	n.Row = noteRow{ID: id, Origin: n.Origin, V1ID: n.V1ID, AuthorKind: authorKind, AuthorID: owner, Agent: agent,
		Disposition: NoteFold}
	return n, nil
}

// noteCarriers are the memories citing the note as a source (the switch's
// bulk-keep proposal, Dream's proposals), not yet forgotten.
func noteCarriers(ctx context.Context, tx pgx.Tx, spaceID, noteID uuid.UUID, ref string) ([]Carried, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT m.id, m.seq, m.lifecycle, m.kind
		  FROM v2.sources s
		  JOIN v2.memory_sources ms ON ms.source_id = s.id
		  JOIN v2.memories m ON m.id = ms.memory_id
		 WHERE s.space_id = $1 AND s.kind = 'note' AND s.locator ->> 'note' = $2 AND m.lifecycle <> 'forgotten'
		 ORDER BY m.seq`, spaceID, noteID.String())
	if err != nil {
		return nil, fmt.Errorf("ledger: what goes with the note: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Carried, error) {
		var c Carried
		var seq int64
		err := r.Scan(&c.ID, &seq, &c.Lifecycle, &c.Kind)
		c.Ref, c.Reason, c.With = FormatRef(PrefixMemory, seq), CarryCites, ref
		return c, err
	})
}

// allNoteCarriers is noteCarriers, then what carries their words.
func allNoteCarriers(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, n *noteTarget) ([]Carried, error) {
	direct, err := noteCarriers(ctx, tx, spaceID, n.ID, n.Ref)
	if err != nil || len(direct) == 0 {
		return direct, err
	}
	roots := make([]*Memory, len(direct))
	for i, c := range direct {
		roots[i] = &Memory{ID: c.ID, Ref: c.Ref}
	}
	more, err := carriedBy(ctx, tx, spaceID, roots)
	if err != nil {
		return nil, err
	}
	out := direct
	for _, c := range more {
		if !slices.ContainsFunc(out, func(x Carried) bool { return x.ID == c.ID }) {
			out = append(out, c)
		}
	}
	if len(out) > MaxCarried {
		return nil, invalid("carries", "more than %d memories go with this note; forget some of them first", MaxCarried)
	}
	return out, nil
}

// forgetNote is ForgetNote.
func (w *writer) forgetNote(ctx context.Context, c *ForgetNote) (Result, error) {
	g, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claimKey(ctx, sp.ID); err != nil || replay != nil {
		if err != nil {
			return Result{}, err
		}
		res, err := replay.result(ctx, w)
		if err != nil || len(res.Receipts) == 0 {
			return res, err
		}
		res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, res.Receipts[0].ObjectID, w.honesty())
		return res, err
	}
	n, err := lockNote(ctx, w.tx, g, w.meta.Scope.PersonID, c.Note)
	if err != nil {
		return Result{}, err
	}
	if !n.Numbered {
		// Numbered beside its forgot receipt (purgeNote).
		if n.Seq, err = allocateRef(ctx, w.tx, sp.TenantID, PrefixNote); err != nil {
			return Result{}, err
		}
		n.Ref = FormatRef(PrefixNote, n.Seq)
	}
	ref := n.Ref
	pa := w.policyActor(g)
	dec := policy.Decide(pa, policy.ActionForget, policy.Object{Ref: ref, Secrets: findSecrets(c.Remark)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	carried, err := allNoteCarriers(ctx, w.tx, sp.ID, n)
	if err != nil {
		return Result{}, err
	}
	locked, err := w.lockCarried(ctx, carried)
	if err != nil {
		return Result{}, err
	}
	for _, cm := range carried {
		m := locked[cm.ID]
		d := policy.Decide(pa, policy.ActionForget, policy.Object{Ref: m.Ref, Lifecycle: m.Lifecycle,
			Decision: m.Kind == KindDecision}, sp.policy())
		if d.Effect == policy.EffectRefuse {
			d.Message = fmt.Sprintf("%s goes with %s, and you can't forget it: %s", m.Ref, ref, d.Message)
			return refused(d), nil
		}
	}
	refs := make([]string, len(carried))
	for i, cm := range carried {
		refs[i] = cm.Ref
	}
	if !sameRefs(c.Carries, refs) {
		return Result{}, &ForgetCarriesError{Ref: ref, Carries: carried}
	}

	op := &forgetOp{id: newID(), kind: ObjectNote, primary: ref, note: c.Remark}
	op.refs = append([]string{ref}, refs...)
	op.ids = []uuid.UUID{n.ID}
	for _, cm := range carried {
		op.ids = append(op.ids, cm.ID)
	}
	files, err := w.filesHolding(ctx, sp.ID, op.refs)
	if err != nil {
		return Result{}, err
	}
	keys, err := w.purgeNote(ctx, sp, op, n)
	if err != nil {
		return Result{}, err
	}
	var forgotten []uuid.UUID
	for _, cm := range carried {
		cm := cm
		if _, err := w.purgeMemory(ctx, sp, op, locked[cm.ID], &cm); err != nil {
			return Result{}, err
		}
		forgotten = append(forgotten, cm.ID)
	}
	if len(keys) > 0 {
		detail, err := json.Marshal(map[string]any{"delete": keys})
		if err != nil {
			return Result{}, err
		}
		if err := w.insertStep(ctx, sp, op, "attachments", nil, "", "pending", detail); err != nil {
			return Result{}, err
		}
	}
	if err := w.afterPurge(ctx, sp, op, files); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: op.receipts}
	if err := w.record(ctx, res, uuid.Nil); err != nil {
		return Result{}, err
	}
	if res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, n.ID, w.honesty()); err != nil {
		return Result{}, err
	}
	for _, id := range forgotten {
		m, err := loadMemory(ctx, w.tx, w.meta.Scope, id, false)
		if err != nil {
			return Result{}, err
		}
		res.Memories = append(res.Memories, *m)
	}
	return res, nil
}

// purgeNote writes the note's forgot receipt, numbers it if it wasn't,
// deletes its V1 row (v2.purge_note_words) and writes its tombstone. It
// returns the storage keys of its attachments, for the propagation.
func (w *writer) purgeNote(ctx context.Context, sp spaceRow, op *forgetOp, n *noteTarget) ([]string, error) {
	rc := w.objectReceipt(sp, ObjectNote, n.ID, n.Ref, ActionForgot, n.Stream+1, "")
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return nil, err
	}
	op.receipts = append(op.receipts, rc)
	if !n.Numbered {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.note_refs (note_id, tenant_id, space_id, seq, origin, v1_id, author_kind, author_id, agent,
			                          disposition, receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, 'memory', $1, $5, $6, $7, 'fold', $8, $8)`,
			n.ID, sp.TenantID, sp.ID, n.Seq, strPtr(n.Row.AuthorKind), n.OwnerID, strPtr(n.Row.Agent), rc.ID); err != nil {
			return nil, fmt.Errorf("ledger: forget %s: %w", n.Ref, err)
		}
	}
	var keys []string
	if err := w.tx.QueryRow(ctx, `SELECT v2.purge_note_words($1)`, n.ID).Scan(&keys); err != nil {
		return nil, fmt.Errorf("ledger: forget %s: %w", n.Ref, err)
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.note_refs SET forgotten_at = now(), stream_version = $2, last_receipt_id = $3, updated_at = now()
		 WHERE note_id = $1 AND space_id = $4`, n.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return nil, fmt.Errorf("ledger: forget %s: %w", n.Ref, err)
	}
	if err := w.insertTombstone(ctx, sp, op, n.ID, n.Ref, ObjectNote, nil, rc, nil, 0, TombstoneGone{}); err != nil {
		return nil, err
	}
	return keys, nil
}

// NoteForgetPreview is what forgetting a note would do, before anyone
// confirms it.
type NoteForgetPreview struct {
	Ref     string           `json:"ref"`
	Carries []Carried        `json:"carries"`
	Allowed bool             `json:"allowed"`
	Policy  *policy.Decision `json:"policy,omitempty"`
}

// PreviewForgetNote says what forgetting a note would take with it, and
// whether the actor may.
func (l *Ledger) PreviewForgetNote(ctx context.Context, actor Actor, via policy.Via, scope Scope, spaceID uuid.UUID, ref string) (*NoteForgetPreview, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	g, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	var out *NoteForgetPreview
	err := l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		sp, err := loadSpace(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		id, err := resolveNoteRef(ctx, tx, g, ref)
		if err != nil {
			return err
		}
		var seq *int64
		var forgotten *time.Time
		var owner uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT r.seq, r.forgotten_at, n.owner_id
			  FROM v2.notes n LEFT JOIN v2.note_refs r ON r.note_id = n.id
			 WHERE n.id = $1 AND n.space_id = $2`, id, spaceID).Scan(&seq, &forgotten, &owner)
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: forget preview: %w", err)
		}
		if g.Role != policy.RoleOwner && owner != scope.PersonID {
			return ErrNotFound
		}
		n := &noteTarget{ID: id}
		if seq != nil {
			n.Ref = FormatRef(PrefixNote, *seq)
		}
		carried, err := allNoteCarriers(ctx, tx, spaceID, n)
		if err != nil {
			return err
		}
		out = &NoteForgetPreview{Ref: n.Ref, Carries: nonNilSlice(carried)}
		d := policy.Decide(toPolicyActor(actor, via, g), policy.ActionForget, policy.Object{Ref: n.Ref}, sp.policy())
		out.Allowed = d.Effect != policy.EffectRefuse
		if !out.Allowed {
			out.Policy = &d
		}
		return nil
	})
	return out, err
}
