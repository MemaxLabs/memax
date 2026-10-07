package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Undo (Review's ⌘Z, and "Unfold" on a judge fold).
//
// Every undoable command writes an undo entry in its own transaction: for
// each memory it changed, the state before (lifecycle, flags, version,
// section, trust, decision status) and the stream version after, plus the
// links it made and ended. It never stores words: a restored statement is
// a restored version number, and versions are append-only.
//
// Undo applies the inverse as one command, with an `undid` receipt per
// memory whose source names the receipt undone. It is refused when:
//
//   - someone else made the decision (a person undoes their own; any
//     person who may keep undoes the judge's folds);
//   - the window has passed (10 minutes for a person's decision, 14 days
//     for a fold, by default);
//   - it was already undone;
//   - something later depends on it: a memory changed again since, a
//     link to one of them was made since, or the Brief now cites a memory
//     the undo would take out of the kept set.
//
// What is undoable: Keep, Reject and Edit (with or without Keep) by a
// person, a conflict resolution, and the judge's folds (duplicates and
// re-proposals). Forget never is, and neither is the judge putting a
// Write agent's write back in Review (`returned`): undoing it would make
// the write kept again on the agent's authority while it contradicts a
// decision in force, which is what rule 11 forbids, and with none of
// Keep's assurance (a decision that needs a person on the web would go
// back in force from the CLI). Settling the conflict is the way out, and
// it is one step: "keep this" or "keep both" keeps the write as the
// person's own decision, with its receipt.

// UndoKind names the command an undo entry undoes.
type UndoKind string

// The undoable commands.
const (
	UndoKeep      UndoKind = "keep"
	UndoReject    UndoKind = "reject"
	UndoEdit      UndoKind = "edit"
	UndoResolve   UndoKind = "resolve_conflict"
	UndoJudgeFold UndoKind = "judge_fold"
)

// The default undo windows.
const (
	DefaultUndoWindow      = 10 * time.Minute
	DefaultJudgeUndoWindow = 14 * 24 * time.Hour
)

// WithUndoWindows sets how long a person's decision, and one of the
// judge's folds, can be undone.
func WithUndoWindows(person, judge time.Duration) Option {
	return func(l *Ledger) { l.undoWindow, l.judgeUndoWindow = person, judge }
}

// CommandUndo names Undo.
const CommandUndo CommandName = "undo"

// Undo reverses the command that wrote Receipt (any of its receipts).
type Undo struct {
	Meta
	Receipt uuid.UUID
}

// Name implements Command.
func (*Undo) Name() CommandName { return CommandUndo }

// ErrUndoRefused: the command can't be undone (see *UndoError).
var ErrUndoRefused = errors.New("ledger: undo refused")

// Why an undo was refused.
const (
	UndoWindowPassed  = "window_passed"
	UndoLaterChanges  = "later_changes"
	UndoAlreadyUndone = "already_undone"
	UndoNotUndoable   = "not_undoable"
)

// UndoRefusals lists every reason.
var UndoRefusals = []string{UndoWindowPassed, UndoLaterChanges, UndoAlreadyUndone, UndoNotUndoable}

// UndoError says why a command can't be undone, and what to do instead.
type UndoError struct {
	Reason string
	// Ref is the memory (or Brief version) in the way, if any.
	Ref     string
	Message string
}

func (e *UndoError) Error() string { return e.Message }

// Is makes errors.Is(err, ErrUndoRefused) match.
func (e *UndoError) Is(target error) bool { return target == ErrUndoRefused }

// memorySnapshot is a memory's state as Undo restores it. No words.
type memorySnapshot struct {
	Lifecycle      lifecycle.Lifecycle `json:"lifecycle"`
	Flags          []string            `json:"flags"`
	Version        int                 `json:"version"`
	Section        Section             `json:"section"`
	Trust          policy.Trust        `json:"trust"`
	HasDecision    bool                `json:"has_decision"`
	DecisionStatus string              `json:"decision_status,omitempty"`
}

type undoMemory struct {
	ID                 uuid.UUID      `json:"id"`
	Ref                string         `json:"ref"`
	Before             memorySnapshot `json:"before"`
	AfterStreamVersion int            `json:"after_stream_version"`
}

type undoLink struct {
	ID   uuid.UUID `json:"id"`
	Kind LinkKind  `json:"kind"`
	From uuid.UUID `json:"from"`
	To   uuid.UUID `json:"to"`
}

// undoJournal collects a command's inverse while it runs.
type undoJournal struct {
	kind         UndoKind
	window       time.Duration
	Memories     []undoMemory `json:"memories"`
	LinksCreated []uuid.UUID  `json:"links_created"`
	LinksEnded   []undoLink   `json:"links_ended"`
}

// startUndo makes the command undoable. Call it before the first change.
func (w *writer) startUndo(kind UndoKind, window time.Duration) {
	if w.undo == nil {
		w.undo = &undoJournal{kind: kind, window: window, LinksCreated: []uuid.UUID{}, LinksEnded: []undoLink{}}
	}
}

// touch records a memory's state before the command changes it.
func (j *undoJournal) touch(m *Memory) {
	if j == nil || slices.ContainsFunc(j.Memories, func(u undoMemory) bool { return u.ID == m.ID }) {
		return
	}
	snap := memorySnapshot{
		Lifecycle: m.Lifecycle, Flags: m.Flags.Strings(), Version: m.Version, Section: m.Section, Trust: m.Trust,
		HasDecision: m.Decision != nil,
	}
	if m.Decision != nil {
		snap.DecisionStatus = m.Decision.Status
	}
	j.Memories = append(j.Memories, undoMemory{ID: m.ID, Ref: m.Ref, Before: snap})
}

// writeUndo stores the journal, if the command keeps one.
func (w *writer) writeUndo(ctx context.Context, sp spaceRow, receipts []Receipt) error {
	ids := w.journaled(receipts)
	if ids == nil {
		return nil
	}
	after, err := streamVersions(w.tx.Query(ctx, journalSQL, ids, sp.ID))
	if err != nil {
		return err
	}
	return w.storeUndo(ctx, sp, receipts, ids, after)
}

// markDirtyAndJournal is markDirty(sp) then writeUndo, with the targets'
// UPDATE and the journal's read in one round trip: neither needs the
// other's answer, and they reach the server in the same order as before.
func (w *writer) markDirtyAndJournal(ctx context.Context, sp spaceRow, receipts []Receipt) error {
	ids := w.journaled(receipts)
	if ids == nil {
		return w.markDirty(ctx, sp.ID)
	}
	b := &pgx.Batch{}
	b.Queue(dirtySQL, sp.ID, []uuid.UUID(nil))
	b.Queue(journalSQL, ids, sp.ID)
	br := w.tx.SendBatch(ctx, b)
	targets, err := dirtyTargets(br.Query())
	if err != nil {
		_ = br.Close()
		return err
	}
	after, err := streamVersions(br.Query())
	if cerr := br.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("ledger: journal: %w", cerr)
	}
	if err != nil {
		return err
	}
	w.queueCompiles(sp.ID, targets)
	return w.storeUndo(ctx, sp, receipts, ids, after)
}

// journaled is the ids of the memories the journal holds, or nil when the
// command keeps none.
func (w *writer) journaled(receipts []Receipt) []uuid.UUID {
	j := w.undo
	if j == nil || len(j.Memories) == 0 || len(receipts) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(j.Memories))
	for i, m := range j.Memories {
		ids[i] = m.ID
	}
	return ids
}

// journalSQL reads the stream versions of the memories $1 in the space $2,
// after the command's changes.
const journalSQL = `SELECT id, stream_version FROM v2.memories WHERE id = ANY ($1) AND space_id = $2`

// streamVersions reads journalSQL's answer.
func streamVersions(rows pgx.Rows, err error) (map[uuid.UUID]int, error) {
	if err != nil {
		return nil, fmt.Errorf("ledger: journal: %w", err)
	}
	defer rows.Close()
	after := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("ledger: journal: %w", err)
		}
		after[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: journal: %w", err)
	}
	return after, nil
}

// storeUndo stores the journal with the memories' versions after the
// command.
func (w *writer) storeUndo(ctx context.Context, sp spaceRow, receipts []Receipt, ids []uuid.UUID, after map[uuid.UUID]int) error {
	j := w.undo
	for i := range j.Memories {
		j.Memories[i].AfterStreamVersion = after[j.Memories[i].ID]
	}
	inverse, err := json.Marshal(j)
	if err != nil {
		return err
	}
	rids := make([]uuid.UUID, len(receipts))
	for i, rc := range receipts {
		rids[i] = rc.ID
	}
	actorKind, actorID := string(w.meta.Actor.Kind), w.actorID()
	if w.meta.Actor.Kind != policy.ActorPerson {
		actorKind, actorID = string(policy.ActorMemax), nil
	}
	// Nothing reads the insert's result: it goes out with the next
	// statement (execDeferred).
	if err := execDeferred(ctx, w.tx, `
		INSERT INTO v2.undo_entries (id, tenant_id, space_id, command, actor_kind, actor_id, receipt_ids, memory_ids,
		                             inverse, expires_at, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now() + make_interval(secs => $10), $11, $11)`,
		newID(), sp.TenantID, sp.ID, string(j.kind), actorKind, actorID, rids, ids, inverse,
		j.window.Seconds(), receipts[0].ID); err != nil {
		return fmt.Errorf("ledger: journal: %w", err)
	}
	return nil
}

// undoEntry is a stored undo entry.
type undoEntry struct {
	id         uuid.UUID
	spaceID    uuid.UUID
	kind       UndoKind
	actorKind  policy.ActorKind
	actorID    *uuid.UUID
	receiptIDs []uuid.UUID
	memoryIDs  []uuid.UUID
	inverse    undoJournal
	expired    bool
	undone     bool
	receiptID  uuid.UUID
	createdAt  time.Time
}

// undoCommand applies Undo.
func (w *writer) undoCommand(ctx context.Context, c *Undo) (Result, error) {
	e, err := w.loadUndoEntry(ctx, c.Receipt)
	if err != nil {
		return Result{}, err
	}
	grant, ok := w.meta.Scope.Grant(e.spaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, e.spaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		if err == nil {
			err = w.loadMemories(ctx, replay, e.memoryIDs)
		}
		return deref(replay), err
	}
	first := e.inverse.Memories[0]
	dec := policy.Decide(w.policyActor(grant), policy.ActionUndo, policy.Object{
		Ref:        first.Ref,
		UndoOwn:    e.actorKind == policy.ActorPerson && e.actorID != nil && *e.actorID == w.meta.Actor.ID,
		UndoSystem: e.actorKind == policy.ActorMemax,
	}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	switch {
	case e.undone:
		return Result{}, &UndoError{Reason: UndoAlreadyUndone, Ref: first.Ref,
			Message: fmt.Sprintf("This was already undone. %s is as it was before.", first.Ref)}
	case e.expired:
		what := "decision"
		if e.kind == UndoJudgeFold {
			what = "fold"
		}
		return Result{}, &UndoError{Reason: UndoWindowPassed, Ref: first.Ref, Message: fmt.Sprintf(
			"This %s is too old to undo. Change %s instead: edit it, reject it or forget it.", what, first.Ref)}
	}

	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, e.memoryIDs, "FOR UPDATE")
	if err != nil {
		return Result{}, err
	}
	for _, um := range e.inverse.Memories {
		m := locked[um.ID]
		if m == nil {
			return Result{}, ErrNotFound
		}
		// Nothing brings a forgotten memory back, Undo included.
		if m.Lifecycle == lifecycle.Forgotten {
			return Result{}, &UndoError{Reason: UndoNotUndoable, Ref: m.Ref, Message: fmt.Sprintf(
				"%s was forgotten, and forgetting can't be undone. If it becomes true again, remember it fresh.", m.Ref)}
		}
		if m.streamVersion != um.AfterStreamVersion {
			later, err := w.changedSince(ctx, e.spaceID, m.ID, um.AfterStreamVersion)
			if err != nil {
				return Result{}, err
			}
			if later {
				return Result{}, &UndoError{Reason: UndoLaterChanges, Ref: m.Ref, Message: fmt.Sprintf(
					"%s changed after this, so undoing it would lose that change. Undo the later change first, or change %s directly.", m.Ref, m.Ref)}
			}
		}
	}
	if err := w.checkUndoLinks(ctx, e); err != nil {
		return Result{}, err
	}
	for _, um := range e.inverse.Memories {
		m := locked[um.ID]
		if m.Lifecycle == lifecycle.Kept && um.Before.Lifecycle != lifecycle.Kept {
			if b, err := w.briefCiting(ctx, sp.ID, m.Ref); err != nil || b != "" {
				if err != nil {
					return Result{}, err
				}
				return Result{}, &UndoError{Reason: UndoLaterChanges, Ref: b, Message: fmt.Sprintf(
					"The Brief (%s) cites %s now. Take it out of the Brief first, then undo.", b, m.Ref)}
			}
		}
	}

	// Apply the inverse, newest change last undone first: the order of the
	// receipts doesn't matter, but keep it stable.
	reason := w.meta.Reason
	if reason == "" {
		reason = fmt.Sprintf("Undid the %s.", undoNoun(e.kind))
	}
	receiptFor := map[uuid.UUID]uuid.UUID{}
	var receipts []Receipt
	dirty := false
	for _, um := range e.inverse.Memories {
		m := locked[um.ID]
		before := um.Before
		flags, err := lifecycle.ParseFlags(before.Flags)
		if err != nil {
			return Result{}, err
		}
		to := lifecycle.State{Lifecycle: before.Lifecycle, Flags: flags}
		if err := lifecycle.CanRestore(m.state(), to); err != nil {
			var te *lifecycle.TransitionError
			if errors.As(err, &te) {
				return Result{}, &TransitionError{Ref: m.Ref, Err: te}
			}
			return Result{}, err
		}
		var statement string
		if err := w.tx.QueryRow(ctx, `SELECT COALESCE(statement, '') FROM v2.memory_versions WHERE memory_id = $1 AND version = $2`,
			m.ID, before.Version).Scan(&statement); err != nil {
			return Result{}, fmt.Errorf("ledger: undo %s: version %d: %w", m.Ref, before.Version, err)
		}
		rc := w.receipt(sp, m.ID, m.Ref, ActionUndid, m.streamVersion+1, reason)
		rc.Source = &ReceiptSource{Kind: "receipt", Ref: e.receiptID.String()}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return Result{}, err
		}
		receipts = append(receipts, rc)
		receiptFor[m.ID] = rc.ID
		hash, bands := signature(statement)
		if _, err := w.tx.Exec(ctx, fmt.Sprintf(`
			UPDATE v2.memories
			   SET lifecycle = $2, flags = $3, current_version = $4, section = $5, trust = $6,
			       decision = CASE WHEN NOT $7 THEN NULL
			                       WHEN $8 = '' THEN COALESCE(decision, '{}'::jsonb) - 'status'
			                       ELSE jsonb_set(COALESCE(decision, '{}'::jsonb), '{status}', to_jsonb($8::text)) END,
			       search = %s, content_sha256 = $10, minhash_bands = $11,
			       stream_version = $12, last_receipt_id = $13, updated_at = now()
			 WHERE id = $1 AND space_id = $14`, fmt.Sprintf(searchExpr, "$9::text")),
			m.ID, string(to.Lifecycle), to.Flags.Strings(), before.Version, string(before.Section), string(before.Trust),
			before.HasDecision, before.DecisionStatus, statement, hash, bands, rc.StreamVersion, rc.ID, sp.ID); err != nil {
			return Result{}, fmt.Errorf("ledger: undo %s: %w", m.Ref, err)
		}
		if to.Lifecycle == lifecycle.Proposed || to.Lifecycle == lifecycle.Kept {
			// The restored version is searchable again; its job is a no-op
			// when its embedding is still stored.
			w.indexVersion(sp.ID, m.ID, before.Version)
		}
		if m.Lifecycle == lifecycle.Kept || to.Lifecycle == lifecycle.Kept {
			dirty = true
		}
		if to.Lifecycle == lifecycle.Proposed && m.Lifecycle != lifecycle.Proposed {
			if err := w.rejudge(ctx, e, m.ID, before.Version); err != nil {
				return Result{}, err
			}
		}
	}
	on := func(from, to uuid.UUID) uuid.UUID {
		if id, ok := receiptFor[from]; ok {
			return id
		}
		return receiptFor[to]
	}
	for _, id := range e.inverse.LinksCreated {
		var from, to uuid.UUID
		if err := w.tx.QueryRow(ctx, `SELECT from_memory_id, to_memory_id FROM v2.memory_links WHERE id = $1`, id).Scan(&from, &to); err != nil {
			return Result{}, fmt.Errorf("ledger: undo link: %w", err)
		}
		if _, err := w.tx.Exec(ctx, `UPDATE v2.memory_links SET ended_receipt_id = $2, ended_at = now() WHERE id = $1 AND ended_receipt_id IS NULL`,
			id, on(from, to)); err != nil {
			return Result{}, fmt.Errorf("ledger: undo link: %w", err)
		}
	}
	for _, l := range e.inverse.LinksEnded {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.memory_links (id, space_id, kind, from_memory_id, to_memory_id, receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6)`, newID(), sp.ID, string(l.Kind), l.From, l.To, on(l.From, l.To)); err != nil {
			return Result{}, fmt.Errorf("ledger: undo link: %w", err)
		}
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.undo_entries SET undone_receipt_id = $2, undone_at = now(), last_receipt_id = $2 WHERE id = $1`,
		e.id, receipts[0].ID); err != nil {
		return Result{}, fmt.Errorf("ledger: undo: %w", err)
	}
	if dirty {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	res, err := w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, first.ID)
	if err == nil {
		err = w.loadMemories(ctx, &res, e.memoryIDs)
	}
	return res, err
}

// changedSince reports whether a memory changed after the given stream
// version in a way an undo would lose. Three kinds of later receipt don't
// count: the judge's `judged` (a verdict that changed nothing, which every
// proposal edit gets within seconds), a `drafted` one (words for a kept
// memory held out of force for the judge, which change nothing until a
// resolution applies them), and a later command that was itself undone,
// with its `undid` receipts: undoing the later change first, as the
// refusal says to, puts the memory back where this command left it.
func (w *writer) changedSince(ctx context.Context, spaceID, memoryID uuid.UUID, version int) (bool, error) {
	var later bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM v2.receipts r
		   WHERE r.stream_id = $1 AND r.space_id = $2 AND r.stream_version > $3
		     AND r.action NOT IN ('judged', 'drafted', 'forget_requested', 'forget_declined')
		     AND NOT EXISTS (
		       SELECT 1 FROM v2.undo_entries u
		        WHERE u.space_id = $2 AND u.undone_receipt_id IS NOT NULL
		          AND (r.id = ANY (u.receipt_ids)
		               OR (r.action = 'undid' AND r.source->>'kind' = 'receipt' AND r.source->>'ref' = u.receipt_id::text))))`,
		memoryID, spaceID, version).Scan(&later); err != nil {
		return false, fmt.Errorf("ledger: undo: read later changes: %w", err)
	}
	return later, nil
}

func undoNoun(k UndoKind) string {
	switch k {
	case UndoKeep:
		return "keep"
	case UndoReject:
		return "rejection"
	case UndoEdit:
		return "edit"
	case UndoResolve:
		return "conflict resolution"
	}
	return "fold"
}

// loadUndoEntry finds the undo entry a receipt belongs to, locked. A
// receipt in scope with no entry is not undoable; one outside it is not
// found.
func (w *writer) loadUndoEntry(ctx context.Context, receiptID uuid.UUID) (*undoEntry, error) {
	var e undoEntry
	var inverse []byte
	var undone *uuid.UUID
	err := w.tx.QueryRow(ctx, `
		SELECT id, space_id, command, actor_kind, actor_id, receipt_ids, memory_ids, inverse,
		       expires_at < now(), undone_receipt_id, receipt_id, created_at
		  FROM v2.undo_entries
		 WHERE $1 = ANY (receipt_ids) AND space_id = ANY ($2)
		 FOR UPDATE`, receiptID, w.meta.Scope.SpaceIDs()).
		Scan(&e.id, &e.spaceID, &e.kind, &e.actorKind, &e.actorID, &e.receiptIDs, &e.memoryIDs, &inverse,
			&e.expired, &undone, &e.receiptID, &e.createdAt)
	if errNoRows(err) {
		var ref string
		var action Action
		err := w.tx.QueryRow(ctx, `SELECT object_ref, action FROM v2.receipts WHERE id = $1 AND space_id = ANY ($2)`,
			receiptID, w.meta.Scope.SpaceIDs()).Scan(&ref, &action)
		if errNoRows(err) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("ledger: load receipt: %w", err)
		}
		msg := fmt.Sprintf("That receipt (%s %s) can't be undone. Keeps, rejections, edits, conflict resolutions and the judge's folds can.", action, ref)
		switch action {
		case ActionUndid:
			msg = "That receipt is itself an undo. Make the change again instead."
		case ActionForgot:
			msg = fmt.Sprintf("Forgetting can't be undone: %s's words are gone. If it becomes true again, remember it fresh; it gets a new ID.", ref)
		case ActionReturned:
			msg = fmt.Sprintf("Memax put %s back in Review because it contradicts a decision in force, so it can't simply be kept again. "+
				"Settle the conflict instead: compare both sides and keep it, keep the decision, or keep both.", ref)
		}
		return nil, &UndoError{Reason: UndoNotUndoable, Ref: ref, Message: msg}
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load undo entry: %w", err)
	}
	if err := json.Unmarshal(inverse, &e.inverse); err != nil {
		return nil, fmt.Errorf("ledger: undo entry %s: %w", e.id, err)
	}
	if len(e.inverse.Memories) == 0 {
		return nil, fmt.Errorf("ledger: undo entry %s has no memories", e.id)
	}
	e.undone = undone != nil
	return &e, nil
}

// checkUndoLinks refuses an undo when the command's links changed since,
// or a later link points at one of its memories (a proposal folded into
// a memory this keep made kept, say).
func (w *writer) checkUndoLinks(ctx context.Context, e *undoEntry) error {
	if len(e.inverse.LinksCreated) > 0 {
		var active int
		if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM v2.memory_links WHERE id = ANY ($1) AND ended_receipt_id IS NULL`,
			e.inverse.LinksCreated).Scan(&active); err != nil {
			return fmt.Errorf("ledger: undo links: %w", err)
		}
		if active != len(e.inverse.LinksCreated) {
			return &UndoError{Reason: UndoLaterChanges, Ref: e.inverse.Memories[0].Ref, Message: fmt.Sprintf(
				"%s was linked differently after this. Undo the later change first.", e.inverse.Memories[0].Ref)}
		}
	}
	var seq int64
	err := w.tx.QueryRow(ctx, `
		SELECT CASE WHEN l.from_memory_id = ANY ($1) THEN t.seq ELSE f.seq END
		  FROM v2.memory_links l
		  JOIN v2.memories f ON f.id = l.from_memory_id
		  JOIN v2.memories t ON t.id = l.to_memory_id
		 WHERE l.ended_receipt_id IS NULL AND (l.from_memory_id = ANY ($1) OR l.to_memory_id = ANY ($1))
		   AND l.created_at > $2 AND NOT (l.id = ANY ($3))
		 LIMIT 1`, e.memoryIDs, e.createdAt, nonNilSlice(e.inverse.LinksCreated)).Scan(&seq)
	if errNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ledger: undo links: %w", err)
	}
	ref := FormatRef(PrefixMemory, seq)
	return &UndoError{Reason: UndoLaterChanges, Ref: ref, Message: fmt.Sprintf(
		"%s was linked to this since. Undo or settle that first.", ref)}
}

// briefCiting returns the display ID of the space's current Brief version
// if it places or cites ref, or "".
func (w *writer) briefCiting(ctx context.Context, spaceID uuid.UUID, ref string) (string, error) {
	var seq int64
	err := w.tx.QueryRow(ctx, `
		SELECT bv.seq FROM v2.briefs b
		  JOIN v2.brief_versions bv ON bv.brief_id = b.id AND bv.version = b.current_version
		 WHERE b.space_id = $1
		   AND jsonb_path_exists(bv.structure, '$.sections[*].items[*] ? (@.ref == $r || @.cites[*] == $r)',
		                         jsonb_build_object('r', $2::text))`, spaceID, ref).Scan(&seq)
	if errNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("ledger: read the Brief: %w", err)
	}
	return FormatRef(PrefixBrief, seq), nil
}

// rejudge queues the judge for a memory Undo puts back in Review: always
// after an undone fold (forced, without stage 0's folds, so a person's
// "not a duplicate" stands and conflicts are still caught), otherwise
// only when the version was never judged (it was kept before the judge
// got to it).
func (w *writer) rejudge(ctx context.Context, e *undoEntry, memoryID uuid.UUID, version int) error {
	var maxRound *int
	if err := w.tx.QueryRow(ctx, `SELECT max(round) FROM v2.judge_verdicts WHERE memory_id = $1 AND version = $2`,
		memoryID, version).Scan(&maxRound); err != nil {
		return fmt.Errorf("ledger: read verdicts: %w", err)
	}
	fold := e.kind == UndoJudgeFold
	if maxRound != nil && !fold {
		return nil
	}
	round := 1
	if maxRound != nil {
		round = *maxRound + 1
	}
	w.enqueueJudge(JudgeArgs{MemoryID: memoryID, SpaceID: e.spaceID, Version: version, Mode: JudgeProposal,
		Round: round, Force: fold, SkipFold: fold, Cause: e.id.String()})
	return nil
}

// loadMemories fills res.Memories with the projections of ids.
func (w *writer) loadMemories(ctx context.Context, res *Result, ids []uuid.UUID) error {
	rows, err := w.tx.Query(ctx, memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2)`, ids, w.meta.Scope.SpaceIDs())
	if err != nil {
		return fmt.Errorf("ledger: load memories: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return fmt.Errorf("ledger: load memories: %w", err)
	}
	if err := attachDetails(ctx, w.tx, ms); err != nil {
		return err
	}
	res.Memories = make([]Memory, 0, len(ms))
	for _, id := range ids {
		for _, m := range ms {
			if m.ID == id {
				res.Memories = append(res.Memories, *m)
			}
		}
	}
	return nil
}
