package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Why PublishEdition skipped a planned action (DreamStats.Skipped).
const (
	DreamSkipChanged   = "changed"   // the memory moved on since Dream read it
	DreamSkipForgotten = "forgotten" // a memory it rests on was forgotten during the run
	DreamSkipGone      = "gone"      // its notes are gone
	DreamSkipRepeat    = "repeat"    // the space already has these words, or rejected them
	DreamSkipSecret    = "secret"    // the words look like a credential
	DreamSkipDone      = "done"      // nothing left to do (already folded, flagged, linked)
	DreamSkipInvalid   = "invalid"   // it didn't hold up against the record (an uncited line)
	DreamSkipBrief     = "brief"     // the Brief changed since Dream read it
)

// QueueDream is the River queue Dream's jobs run on.
const QueueDream = "dream"

// DreamEmailArgs is the River job that sends an edition's morning email,
// queued in the transaction that publishes it (internal/v2dream works it).
type DreamEmailArgs struct {
	EditionID uuid.UUID `json:"edition_id"`
	SpaceID   uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (DreamEmailArgs) Kind() string { return "dream_email" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DreamEmailArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueDream, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// publishEdition applies PublishEdition.
func (w *writer) publishEdition(ctx context.Context, c *PublishEdition) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	replay, err := w.claimKey(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := replay.result(ctx, w)
		if err == nil && replay.objectID != nil {
			res.Edition, err = loadEdition(ctx, w.tx, w.meta.Scope, sp.ID, *replay.objectID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionPublishEdition, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	// One edition per night: a second run for the slot changes nothing.
	var existing uuid.UUID
	err = w.tx.QueryRow(ctx, `SELECT id FROM v2.dream_editions WHERE space_id = $1 AND slot = $2`, sp.ID, c.Slot).Scan(&existing)
	switch {
	case err == nil:
		e, err := loadEdition(ctx, w.tx, w.meta.Scope, sp.ID, existing)
		return Result{Outcome: OutcomeApplied, Policy: dec, Edition: e, Unchanged: true}, err
	case !errNoRows(err):
		return Result{}, fmt.Errorf("ledger: read editions: %w", err)
	}

	seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixDream)
	if err != nil {
		return Result{}, err
	}
	ed := &editionWrite{id: newID(), ref: FormatRef(PrefixDream, seq), seq: seq, sp: sp, grant: grant, c: c,
		applied: map[string]int{}, skipped: map[string]int{}}

	// The notes it read, still there, with their N- numbers.
	if err := w.dreamNotes(ctx, ed); err != nil {
		return Result{}, err
	}
	// Every memory the plan names, and whether any it rests on is gone.
	if err := w.dreamForgotten(ctx, ed); err != nil {
		return Result{}, err
	}

	for i := range c.Actions {
		a := &c.Actions[i]
		reason, err := w.applyDreamAction(ctx, ed, a)
		if err != nil {
			return Result{}, err
		}
		if reason != "" {
			ed.skipped[reason]++
		}
	}

	// The edition's own receipt and row come last: everything above is
	// checked against it at commit (deferred foreign keys and triggers).
	until := c.Until
	reason := fmt.Sprintf("Dream read %d notes and %d changes; %d actions.", len(ed.notes), c.Stats.Changes, len(ed.actions))
	rc := w.objectReceipt(sp, ObjectDream, ed.id, ed.ref, ActionPublished, 1, reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	for i := range ed.noteRefs {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.note_refs (note_id, tenant_id, space_id, seq, edition_id, receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6)`, ed.noteRefs[i].id, sp.TenantID, sp.ID, ed.noteRefs[i].seq, ed.id, rc.ID); err != nil {
			return Result{}, fmt.Errorf("ledger: number notes: %w", err)
		}
	}
	stats := c.Stats
	stats.Applied, stats.Skipped = ed.applied, ed.skipped
	stats.Notes = ed.notes
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return Result{}, err
	}
	surfaced, err := w.stillSurfaced(ctx, sp.ID, c.Surfaced, ed)
	if err != nil {
		return Result{}, err
	}
	surfacedJSON, err := json.Marshal(nonNilSlice(surfaced))
	if err != nil {
		return Result{}, err
	}
	noteIDs := make([]uuid.UUID, len(ed.notes))
	for i, n := range ed.notes {
		noteIDs[i] = n.ID
	}
	var requested any
	if c.RequestedBy != uuid.Nil {
		requested = c.RequestedBy
	}
	var cursorAt, cursorID any
	if c.Cursor != nil {
		cursorAt, cursorID = c.Cursor.At, c.Cursor.ID
	} else if err := w.tx.QueryRow(ctx, `
		SELECT note_cursor_at, note_cursor_id FROM v2.dream_editions
		 WHERE space_id = $1 ORDER BY seq DESC LIMIT 1`, sp.ID).Scan(&cursorAt, &cursorID); err != nil && !errNoRows(err) {
		return Result{}, fmt.Errorf("ledger: read the last edition: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.dream_editions (id, tenant_id, space_id, seq, slot, trigger, requested_by, since, until,
		                               note_cursor_at, note_cursor_id, started_at, finished_at, notes_read, stats, surfaced,
		                               receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, greatest(now(), $12), $13, $14, $15, $16, $16)`,
		ed.id, sp.TenantID, sp.ID, seq, c.Slot, string(c.Trigger), requested, c.Since, until,
		cursorAt, cursorID, c.StartedAt, noteIDs, statsJSON, surfacedJSON, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write edition: %w", err)
	}
	for _, a := range ed.actions {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.dream_actions (id, tenant_id, space_id, edition_id, n, kind, memory_id, version, related_memory_id,
			                              brief_id, brief_version, note_ids, receipt_ids, inverse, receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)`,
			a.id, sp.TenantID, sp.ID, ed.id, a.n, string(a.kind), uuidOrNil(a.memory), intOrNil(a.version),
			uuidOrNil(a.related), uuidOrNil(a.briefID), intOrNil(a.briefVersion), nonNilSlice(a.notes),
			a.receipts, a.inverse, a.receipts[0]); err != nil {
			return Result{}, fmt.Errorf("ledger: write dream action: %w", err)
		}
	}
	if ed.dirty {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	// The morning email, when the edition has something to say.
	if len(ed.actions) > 0 || len(surfaced) > 0 {
		w.jobs = append(w.jobs, river.InsertManyParams{Args: DreamEmailArgs{EditionID: ed.id, SpaceID: sp.ID}})
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: append([]Receipt{rc}, ed.receipts...)}
	if err := w.record(ctx, res, ed.id); err != nil {
		return Result{}, err
	}
	res.Edition, err = loadEdition(ctx, w.tx, w.meta.Scope, sp.ID, ed.id)
	return res, err
}

// editionWrite is an edition being published.
type editionWrite struct {
	id    uuid.UUID
	ref   string
	seq   int64
	sp    spaceRow
	grant SpaceGrant
	c     *PublishEdition

	notes    []NoteRead             // the notes read that still exist
	present  map[uuid.UUID]NoteRead // by id
	noteRef  map[uuid.UUID]string   // N- ref of every note read
	noteRefs []struct {             // the N- numbers allocated now
		id  uuid.UUID
		seq int64
	}
	forgotten map[uuid.UUID]bool // memories the plan names that are forgotten now

	actions  []actionWrite
	receipts []Receipt
	applied  map[string]int
	skipped  map[string]int
	dirty    bool
}

type actionWrite struct {
	id           uuid.UUID
	n            int
	kind         DreamActionKind
	memory       uuid.UUID
	version      int
	related      uuid.UUID
	briefID      uuid.UUID
	briefVersion int
	notes        []uuid.UUID
	receipts     []uuid.UUID
	inverse      []byte
}

func uuidOrNil(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func intOrNil(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// dreamNotes keeps the notes still in the space and numbers them (N-):
// the ones an earlier edition read keep their numbers.
func (w *writer) dreamNotes(ctx context.Context, ed *editionWrite) error {
	ed.present, ed.noteRef = map[uuid.UUID]NoteRead{}, map[uuid.UUID]string{}
	ids := make([]uuid.UUID, 0, len(ed.c.Notes))
	for _, n := range ed.c.Notes {
		ids = append(ids, n.ID)
	}
	for _, a := range ed.c.Actions {
		ids = append(ids, a.Notes...)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	ids = slices.Compact(ids)
	rows, err := w.tx.Query(ctx, `
		SELECT n.id, r.seq FROM v2.notes n LEFT JOIN v2.note_refs r ON r.note_id = n.id
		 WHERE n.space_id = $1 AND n.id = ANY ($2)`, ed.sp.ID, ids)
	if err != nil {
		return fmt.Errorf("ledger: read notes: %w", err)
	}
	have := map[uuid.UUID]*int64{}
	for rows.Next() {
		var id uuid.UUID
		var seq *int64
		if err := rows.Scan(&id, &seq); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: read notes: %w", err)
		}
		have[id] = seq
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: read notes: %w", err)
	}
	var fresh []uuid.UUID
	for _, n := range ed.c.Notes {
		if _, ok := have[n.ID]; !ok {
			continue
		}
		ed.notes = append(ed.notes, n)
		ed.present[n.ID] = n
	}
	// Numbered in the order they were read (oldest first), then any other
	// note an action names.
	order := make([]uuid.UUID, 0, len(ids)+len(ed.c.Notes))
	for _, n := range ed.c.Notes {
		order = append(order, n.ID)
	}
	order = append(order, ids...)
	for _, id := range order {
		seq, ok := have[id]
		if _, done := ed.noteRef[id]; !ok || done || slices.Contains(fresh, id) {
			continue
		}
		if seq != nil {
			ed.noteRef[id] = FormatRef(PrefixNote, *seq)
		} else {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	var first int64
	if err := w.tx.QueryRow(ctx, `
		INSERT INTO v2.id_counters AS c (tenant_id, prefix, next) VALUES ($1, 'N', $2::bigint + 1)
		ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + $2::bigint
		RETURNING next - $2::bigint`, ed.sp.TenantID, len(fresh)).Scan(&first); err != nil {
		return fmt.Errorf("ledger: allocate N- numbers: %w", err)
	}
	for i, id := range fresh {
		seq := first + int64(i)
		ed.noteRef[id] = FormatRef(PrefixNote, seq)
		ed.noteRefs = append(ed.noteRefs, struct {
			id  uuid.UUID
			seq int64
		}{id, seq})
	}
	return nil
}

// dreamForgotten finds which memories the plan names, or whose words the
// model read, are forgotten now.
func (w *writer) dreamForgotten(ctx context.Context, ed *editionWrite) error {
	var ids []uuid.UUID
	for _, a := range ed.c.Actions {
		ids = append(ids, a.Saw...)
		for _, id := range []uuid.UUID{a.Memory, a.Related} {
			if id != uuid.Nil {
				ids = append(ids, id)
			}
		}
	}
	ed.forgotten = map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return nil
	}
	rows, err := w.tx.Query(ctx, `SELECT id FROM v2.memories WHERE space_id = $1 AND id = ANY ($2) AND lifecycle = 'forgotten'`,
		ed.sp.ID, ids)
	if err != nil {
		return fmt.Errorf("ledger: read forgotten: %w", err)
	}
	gone, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("ledger: read forgotten: %w", err)
	}
	for _, id := range gone {
		ed.forgotten[id] = true
	}
	return nil
}

// applyDreamAction applies one planned action, or says why it skipped it.
func (w *writer) applyDreamAction(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, error) {
	for _, id := range a.Saw {
		if ed.forgotten[id] {
			return DreamSkipForgotten, nil
		}
	}
	if ed.forgotten[a.Memory] || ed.forgotten[a.Related] {
		return DreamSkipForgotten, nil
	}
	var notes []uuid.UUID
	for _, id := range a.Notes {
		if _, ok := ed.noteRef[id]; ok && !slices.Contains(notes, id) {
			notes = append(notes, id)
		}
	}
	if (a.Kind == DreamFold || a.Kind == DreamPropose) && len(notes) == 0 {
		return DreamSkipGone, nil
	}
	var (
		skip string
		act  *actionWrite
		err  error
	)
	switch a.Kind {
	case DreamFold:
		skip, act, err = w.dreamFold(ctx, ed, a, notes)
	case DreamPropose:
		skip, act, err = w.dreamPropose(ctx, ed, a, notes)
	case DreamDedupe:
		skip, act, err = w.dreamDedupe(ctx, ed, a)
	case DreamConflict:
		skip, act, err = w.dreamConflict(ctx, ed, a)
	case DreamStale:
		skip, act, err = w.dreamStale(ctx, ed, a)
	case DreamFade:
		skip, act, err = w.dreamFade(ctx, ed, a)
	case DreamBrief:
		skip, act, err = w.dreamBrief(ctx, ed, a)
	}
	if err != nil || skip != "" {
		return skip, err
	}
	act.id, act.n, act.kind = newID(), len(ed.actions)+1, a.Kind
	ed.actions = append(ed.actions, *act)
	ed.applied[string(a.Kind)]++
	return "", nil
}

// dreamMemory locks one memory of the space for an action, or reports it
// moved on (nil).
func (w *writer) dreamMemory(ctx context.Context, ed *editionWrite, id uuid.UUID) (*Memory, error) {
	m, err := loadMemory(ctx, w.tx, w.meta.Scope, id, true)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.SpaceID != ed.sp.ID || m.Lifecycle == lifecycle.Forgotten {
		return nil, nil
	}
	return m, nil
}

// dreamReceipt writes a receipt by Dream on a memory, citing the edition,
// and moves the memory's stream to it.
func (w *writer) dreamReceipt(ctx context.Context, ed *editionWrite, m *Memory, action Action, reason string) (Receipt, error) {
	rc := w.receipt(ed.sp, m.ID, m.Ref, action, m.streamVersion+1, reason)
	rc.Source = dreamSource(ed.ref)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	ed.receipts = append(ed.receipts, rc)
	m.streamVersion = rc.StreamVersion
	return rc, nil
}

// setDreamState moves a memory to next with the receipt rc.
func (w *writer) setDreamState(ctx context.Context, ed *editionWrite, m *Memory, next lifecycle.State, rc Receipt) error {
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET lifecycle = $2, flags = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $6`,
		m.ID, string(next.Lifecycle), next.Flags.Strings(), rc.StreamVersion, rc.ID, ed.sp.ID); err != nil {
		return fmt.Errorf("ledger: update memory: %w", err)
	}
	m.Lifecycle, m.Flags = next.Lifecycle, next.Flags
	return nil
}

func snapshotOf(m *Memory) undoMemory {
	snap := memorySnapshot{
		Lifecycle: m.Lifecycle, Flags: m.Flags.Strings(), Version: m.Version, Section: m.Section, Trust: m.Trust,
		HasDecision: m.Decision != nil,
	}
	if m.Decision != nil {
		snap.DecisionStatus = m.Decision.Status
	}
	return undoMemory{ID: m.ID, Ref: m.Ref, Before: snap, AfterStreamVersion: m.streamVersion}
}

func (ed *editionWrite) refsOf(notes []uuid.UUID) string {
	refs := make([]string, 0, len(notes))
	for _, id := range notes {
		refs = append(refs, ed.noteRef[id])
	}
	slices.Sort(refs)
	if len(refs) > 6 {
		return fmt.Sprintf("%s to %s", refs[0], refs[len(refs)-1])
	}
	return strings.Join(refs, ", ")
}

// dreamFold: notes become lineage of a kept memory. Its words, state and
// trust stay; only its stream moves.
func (w *writer) dreamFold(ctx context.Context, ed *editionWrite, a *PlannedAction, notes []uuid.UUID) (string, *actionWrite, error) {
	m, err := w.dreamMemory(ctx, ed, a.Memory)
	if err != nil || m == nil {
		return DreamSkipChanged, nil, err
	}
	if m.Lifecycle != lifecycle.Kept || m.Version != a.Version {
		return DreamSkipChanged, nil, nil
	}
	rows, err := w.tx.Query(ctx, `
		SELECT to_note_id FROM v2.memory_links
		 WHERE from_memory_id = $1 AND kind = 'folded_from' AND ended_receipt_id IS NULL AND to_note_id = ANY ($2)`, m.ID, notes)
	if err != nil {
		return "", nil, fmt.Errorf("ledger: read folds: %w", err)
	}
	folded, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return "", nil, fmt.Errorf("ledger: read folds: %w", err)
	}
	notes = slices.DeleteFunc(slices.Clone(notes), func(id uuid.UUID) bool { return slices.Contains(folded, id) })
	if len(notes) == 0 {
		return DreamSkipDone, nil, nil
	}
	inv := dreamInverse{Memories: []undoMemory{snapshotOf(m)}, FoldedNoteIDs: len(notes)}
	what := "a note"
	if len(notes) > 1 {
		what = fmt.Sprintf("%d notes", len(notes))
	}
	rc, err := w.dreamReceipt(ctx, ed, m, ActionFolded, fmt.Sprintf("Dream folded %s into it: %s.", what, ed.refsOf(notes)))
	if err != nil {
		return "", nil, err
	}
	for _, n := range notes {
		id := newID()
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.memory_links (id, space_id, kind, from_memory_id, to_note_id, receipt_id)
			VALUES ($1, $2, 'folded_from', $3, $4, $5)`, id, ed.sp.ID, m.ID, n, rc.ID); err != nil {
			return "", nil, fmt.Errorf("ledger: fold a note: %w", err)
		}
		inv.LinksCreated = append(inv.LinksCreated, id)
	}
	if err := w.setDreamState(ctx, ed, m, m.state(), rc); err != nil {
		return "", nil, err
	}
	inv.Memories[0].AfterStreamVersion = rc.StreamVersion
	b, err := marshalInverse(inv)
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: m.ID, version: m.Version, notes: notes, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// noteTrust is a note's trust class: what an agent wrote is its own work,
// what a person wrote is theirs, and a page fetched from the web or an
// email is external. Dream's own trust caps all of them.
func noteTrust(n NoteRead, external bool) policy.Trust {
	switch {
	case external:
		return policy.TrustExternal
	case n.AuthorKind == "person":
		return policy.TrustPerson
	}
	return policy.TrustAgentOwnWork
}

// dreamPropose: a new fact from notes, as a proposal citing them.
func (w *writer) dreamPropose(ctx context.Context, ed *editionWrite, a *PlannedAction, notes []uuid.UUID) (string, *actionWrite, error) {
	nf := a.New
	if len(findSecrets(nf.Statement)) > 0 {
		return DreamSkipSecret, nil, nil
	}
	// The same words, kept, waiting or rejected lately: nothing new to
	// propose (the judge would fold it at once).
	var repeat bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.memories
		                WHERE space_id = $1 AND content_sha256 = $2
		                  AND (lifecycle IN ('kept', 'proposed') OR (lifecycle = 'rejected' AND updated_at > now() - interval '90 days')))`,
		ed.sp.ID, textsig.ContentSHA256(nf.Statement)).Scan(&repeat); err != nil {
		return "", nil, fmt.Errorf("ledger: read repeats: %w", err)
	}
	if repeat {
		return DreamSkipRepeat, nil, nil
	}
	external, err := w.externalNotes(ctx, ed.sp.ID, notes)
	if err != nil {
		return "", nil, err
	}
	actorTrust := policy.ActorTrust(w.meta.Actor.Kind, w.meta.Via)
	srcs := make([]SourceInput, 0, len(notes))
	trusts := []policy.Trust{actorTrust}
	for _, id := range notes {
		n := ed.present[id]
		if n.ID == uuid.Nil {
			n = NoteRead{ID: id, AuthorKind: "agent"}
		}
		locator, _ := json.Marshal(map[string]string{dreamNoteSourceKey: id.String()})
		t := noteTrust(n, external[id])
		srcs = append(srcs, SourceInput{Kind: SourceNote, Ref: ed.noteRef[id], Locator: locator, Trust: t})
		trusts = append(trusts, t)
	}
	trust := policy.MinTrust(trusts...)
	obj := policy.Object{Decision: nf.Kind == KindDecision, External: trust.External(), Secrets: findSecrets(nf.Statement)}
	dec := policy.Decide(w.policyActor(ed.grant), policy.ActionPropose, obj, ed.sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return DreamSkipInvalid, nil, nil
	}
	state, err := lifecycle.Transition(lifecycle.State{}, lifecycle.VerbPropose)
	if err != nil {
		return "", nil, err
	}
	reason := fmt.Sprintf("Dream proposed it from %s.", ed.refsOf(notes))
	saved := w.meta.Reason
	w.meta.Reason = reason
	id, rc, err := w.insertMemory(ctx, ed.sp, ed.grant, memoryRow{
		Statement: nf.Statement, Section: nf.Section, Kind: nf.Kind, Decision: nf.Decision, Trust: trust,
		Conditions: json.RawMessage(`[]`), Applies: json.RawMessage(`{}`),
	}, state, dreamSource(ed.ref))
	w.meta.Reason = saved
	if err != nil {
		return "", nil, err
	}
	ed.receipts = append(ed.receipts, rc)
	if err := w.insertSources(ctx, ed.sp.ID, id, rc.ID, resolveSources(srcs, actorTrust)); err != nil {
		return "", nil, err
	}
	b, err := marshalInverse(dreamInverse{Created: true, Memories: []undoMemory{{ID: id, Ref: rc.ObjectRef, AfterStreamVersion: 1,
		Before: memorySnapshot{Lifecycle: lifecycle.None, Flags: []string{}, Version: 1, Section: nf.Section, Trust: trust}}}})
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: id, version: 1, notes: notes, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// externalNotes reports which notes hold third-party content: a page
// fetched from a URL, an email.
func (w *writer) externalNotes(ctx context.Context, spaceID uuid.UUID, notes []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := w.tx.Query(ctx, `
		SELECT id, (COALESCE(source, '') IN ('email', 'url', 'link', 'web_clip')
		            OR COALESCE(content_type, '') IN ('html', 'url', 'link')
		            OR COALESCE(source_path, '') ~* '^https?://')
		  FROM v2.notes WHERE space_id = $1 AND id = ANY ($2)`, spaceID, notes)
	if err != nil {
		return nil, fmt.Errorf("ledger: read notes: %w", err)
	}
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		var ext bool
		if err := rows.Scan(&id, &ext); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ledger: read notes: %w", err)
		}
		out[id] = ext
	}
	rows.Close()
	return out, rows.Err()
}

// dreamDedupe folds a proposal that repeats another one waiting in Review
// into it, before anyone reviews either.
func (w *writer) dreamDedupe(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, *actionWrite, error) {
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, []uuid.UUID{a.Memory, a.Related}, "FOR UPDATE")
	if err != nil {
		return "", nil, err
	}
	dup, into := locked[a.Memory], locked[a.Related]
	switch {
	case dup == nil || into == nil || dup.SpaceID != ed.sp.ID || into.SpaceID != ed.sp.ID:
		return DreamSkipChanged, nil, nil
	case dup.Lifecycle != lifecycle.Proposed || dup.Flags.Has(lifecycle.Conflict) || dup.Version != a.Version:
		return DreamSkipChanged, nil, nil
	case (into.Lifecycle != lifecycle.Proposed && into.Lifecycle != lifecycle.Kept) || into.Version != a.RelatedVersion:
		return DreamSkipChanged, nil, nil
	}
	next, err := transition(dup, lifecycle.VerbMerge)
	if err != nil {
		return DreamSkipChanged, nil, nil
	}
	inv := dreamInverse{Memories: []undoMemory{snapshotOf(dup)}}
	rc, err := w.dreamReceipt(ctx, ed, dup, ActionMerged, fmt.Sprintf("A duplicate of %s, waiting in Review.", into.Ref))
	if err != nil {
		return "", nil, err
	}
	if err := w.setDreamState(ctx, ed, dup, next, rc); err != nil {
		return "", nil, err
	}
	link, err := w.insertLink(ctx, ed.sp.ID, LinkMergedInto, dup.ID, into.ID, rc.ID)
	if err != nil {
		return "", nil, err
	}
	inv.LinksCreated = []uuid.UUID{link}
	inv.Memories[0].AfterStreamVersion = rc.StreamVersion
	b, err := marshalInverse(inv)
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: dup.ID, version: dup.Version, related: into.ID, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// dreamConflict flags a memory that contradicts another kept one, with a
// conflicts_with link, for a person to settle.
func (w *writer) dreamConflict(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, *actionWrite, error) {
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, []uuid.UUID{a.Memory, a.Related}, "FOR UPDATE")
	if err != nil {
		return "", nil, err
	}
	x, y := locked[a.Memory], locked[a.Related]
	switch {
	case x == nil || y == nil || x.SpaceID != ed.sp.ID || y.SpaceID != ed.sp.ID:
		return DreamSkipChanged, nil, nil
	case (x.Lifecycle != lifecycle.Proposed && x.Lifecycle != lifecycle.Kept) || x.Version != a.Version:
		return DreamSkipChanged, nil, nil
	case y.Lifecycle != lifecycle.Kept || y.Version != a.RelatedVersion:
		return DreamSkipChanged, nil, nil
	}
	links, err := activeLinks(ctx, w.tx, []uuid.UUID{x.ID})
	if err != nil {
		return "", nil, err
	}
	for _, l := range links[x.ID] {
		if l.Kind == LinkConflictsWith && l.MemoryID == y.ID {
			return DreamSkipDone, nil, nil
		}
	}
	next := x.state()
	if !x.Flags.Has(lifecycle.Conflict) {
		if next, err = transition(x, lifecycle.VerbFlagConflict); err != nil {
			return DreamSkipChanged, nil, nil
		}
	}
	inv := dreamInverse{Memories: []undoMemory{snapshotOf(x)}}
	rc, err := w.dreamReceipt(ctx, ed, x, ActionFlagged, fmt.Sprintf("Contradicts %s. Dream found it; a person settles it.", y.Ref))
	if err != nil {
		return "", nil, err
	}
	if err := w.setDreamState(ctx, ed, x, next, rc); err != nil {
		return "", nil, err
	}
	link, err := w.insertLink(ctx, ed.sp.ID, LinkConflictsWith, x.ID, y.ID, rc.ID)
	if err != nil {
		return "", nil, err
	}
	inv.LinksCreated = []uuid.UUID{link}
	inv.Memories[0].AfterStreamVersion = rc.StreamVersion
	if x.Lifecycle == lifecycle.Kept {
		ed.dirty = true // a kept memory in conflict compiles marked
	}
	b, err := marshalInverse(inv)
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: x.ID, version: x.Version, related: y.ID, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// dreamStale flags a kept memory whose stale_after date has passed, unless
// a person has acted on it since that date (which is a verify in all but
// name, and so is undoing an earlier stale flag). Writing it with a date
// already past doesn't count.
func (w *writer) dreamStale(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, *actionWrite, error) {
	m, err := w.dreamMemory(ctx, ed, a.Memory)
	if err != nil || m == nil {
		return DreamSkipChanged, nil, err
	}
	if m.Lifecycle != lifecycle.Kept || m.Flags.Has(lifecycle.Stale) || m.Version != a.Version ||
		m.StaleAfter == nil || m.StaleAfter.After(w.now) {
		return DreamSkipChanged, nil, nil
	}
	var touched bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.receipts WHERE stream_id = $1 AND space_id = $2 AND actor_kind = 'person'
		                 AND recorded_at > $3 AND id <> $4)`,
		m.ID, ed.sp.ID, *m.StaleAfter, m.CreatedReceiptID).Scan(&touched); err != nil {
		return "", nil, fmt.Errorf("ledger: read history: %w", err)
	}
	if touched {
		return DreamSkipDone, nil, nil
	}
	next, err := transition(m, lifecycle.VerbFlagStale)
	if err != nil {
		return DreamSkipChanged, nil, nil
	}
	inv := dreamInverse{Memories: []undoMemory{snapshotOf(m)}, StaleAfter: m.StaleAfter.UTC().Format(time.RFC3339)}
	rc, err := w.dreamReceipt(ctx, ed, m, ActionFlagged, fmt.Sprintf("Its date to check again, %s, has passed. Verify it.",
		m.StaleAfter.UTC().Format("Jan 2, 2006")))
	if err != nil {
		return "", nil, err
	}
	if err := w.setDreamState(ctx, ed, m, next, rc); err != nil {
		return "", nil, err
	}
	inv.Memories[0].AfterStreamVersion = rc.StreamVersion
	ed.dirty = true
	b, err := marshalInverse(inv)
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: m.ID, version: m.Version, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// dreamFade fades a kept memory nobody has read or touched since a.Unread.
// The engine chose it from ReadStatus; here the ledger re-checks what can
// change in a minute: its state, the Brief, and anything a person or agent
// did to it.
func (w *writer) dreamFade(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, *actionWrite, error) {
	m, err := w.dreamMemory(ctx, ed, a.Memory)
	if err != nil || m == nil {
		return DreamSkipChanged, nil, err
	}
	if m.Lifecycle != lifecycle.Kept || len(m.Flags) > 0 || m.Version != a.Version || m.inForce() || a.Unread == nil {
		return DreamSkipChanged, nil, nil
	}
	var touched bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.receipts WHERE stream_id = $1 AND space_id = $2
		                 AND actor_kind IN ('person', 'agent') AND recorded_at > $3)
		    OR EXISTS (SELECT 1 FROM v2.read_rollups WHERE space_id = $2 AND subject_kind = 'memory'
		                 AND subject_id = $1 AND last_read_at > $3)`,
		m.ID, ed.sp.ID, *a.Unread).Scan(&touched); err != nil {
		return "", nil, fmt.Errorf("ledger: read history: %w", err)
	}
	if touched {
		return DreamSkipChanged, nil, nil
	}
	if b, err := w.briefCiting(ctx, ed.sp.ID, m.Ref); err != nil || b != "" {
		if err != nil {
			return "", nil, err
		}
		return DreamSkipChanged, nil, nil
	}
	next, err := transition(m, lifecycle.VerbFade)
	if err != nil {
		return DreamSkipChanged, nil, nil
	}
	inv := dreamInverse{Memories: []undoMemory{snapshotOf(m)}}
	rc, err := w.dreamReceipt(ctx, ed, m, ActionFaded, "No agent has read it in 60 days. It is restorable.")
	if err != nil {
		return "", nil, err
	}
	if err := w.setDreamState(ctx, ed, m, next, rc); err != nil {
		return "", nil, err
	}
	inv.Memories[0].AfterStreamVersion = rc.StreamVersion
	ed.dirty = true
	b, err := marshalInverse(inv)
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{memory: m.ID, version: m.Version, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// dreamBrief writes a new Brief version from small operations on the one
// Dream read, if it is still the one in force.
func (w *writer) dreamBrief(ctx context.Context, ed *editionWrite, a *PlannedAction) (string, *actionWrite, error) {
	cur, err := lockBrief(ctx, w.tx, ed.sp.ID)
	if err != nil {
		return "", nil, err
	}
	if cur == nil || cur.version != a.Brief.BaseVersion {
		return DreamSkipBrief, nil, nil
	}
	base, err := currentBriefVersion(ctx, w.tx, ed.sp.ID, cur)
	if err != nil {
		return "", nil, err
	}
	kept, err := keptRefs(ctx, w.tx, ed.sp.ID)
	if err != nil {
		return "", nil, err
	}
	sections, err := ApplyBriefOps(base.Sections, a.Brief.Ops, func(ref string) bool { return kept[ref] })
	if err != nil {
		return DreamSkipInvalid, nil, nil
	}
	texts := []string{}
	for _, op := range a.Brief.Ops {
		texts = append(texts, op.Text)
	}
	if len(findSecrets(texts...)) > 0 {
		return DreamSkipSecret, nil, nil
	}
	if err := checkBriefCites(ctx, w.tx, ed.sp.ID, sections); err != nil {
		return DreamSkipInvalid, nil, nil
	}
	baseVersion := cur.version
	what := "a small change"
	if n := len(a.Brief.Ops); n > 1 {
		what = fmt.Sprintf("%d small changes", n)
	}
	rc, version, err := w.writeBriefVersion(ctx, ed.sp, cur, base.Title, base.Summary, sections,
		fmt.Sprintf("Dream made %s, each citing the memories it rests on.", what), dreamSource(ed.ref), ActionRevised)
	if err != nil {
		return "", nil, err
	}
	ed.receipts = append(ed.receipts, rc)
	ed.dirty = true
	b, err := marshalInverse(dreamInverse{BriefID: cur.id, BriefBefore: baseVersion, BriefAfter: version, BriefOps: len(a.Brief.Ops)})
	if err != nil {
		return "", nil, err
	}
	return "", &actionWrite{briefID: cur.id, briefVersion: version, receipts: []uuid.UUID{rc.ID}, inverse: b}, nil
}

// currentBriefVersion reads the Brief version in force.
func currentBriefVersion(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, cur *briefRow) (*Brief, error) {
	return briefVersion(ctx, tx, spaceID, cur.id, cur.version)
}

// briefVersion reads one Brief version's title, summary and sections.
func briefVersion(ctx context.Context, tx pgx.Tx, spaceID, briefID uuid.UUID, version int) (*Brief, error) {
	var b Brief
	var summary *string
	var structure []byte
	err := tx.QueryRow(ctx, `
		SELECT title, summary, structure FROM v2.brief_versions WHERE brief_id = $1 AND version = $2 AND space_id = $3`,
		briefID, version, spaceID).Scan(&b.Title, &summary, &structure)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read Brief version: %w", err)
	}
	if summary != nil {
		b.Summary = *summary
	}
	var st briefStructure
	if err := json.Unmarshal(structure, &st); err != nil {
		return nil, fmt.Errorf("ledger: read Brief version: %w", err)
	}
	b.Sections, b.Version = st.Sections, version
	return &b, nil
}

// keptRefs lists the space's kept memories by ref.
func keptRefs(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT seq FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: read kept memories: %w", err)
	}
	seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("ledger: read kept memories: %w", err)
	}
	out := make(map[string]bool, len(seqs))
	for _, s := range seqs {
		out[FormatRef(PrefixMemory, s)] = true
	}
	return out, nil
}

// writeBriefVersion writes the next version of a space's Brief, as the
// command's actor, with a receipt of the given action, and recompiles.
// ReviseBrief's write, for Dream and for undoing Dream.
func (w *writer) writeBriefVersion(ctx context.Context, sp spaceRow, cur *briefRow, title, summary string,
	sections []BriefSection, reason string, src *ReceiptSource, action Action) (Receipt, int, error) {
	seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixBrief)
	if err != nil {
		return Receipt{}, 0, err
	}
	ref := FormatRef(PrefixBrief, seq)
	version, stream := cur.version+1, cur.streamVersion+1
	rc := w.objectReceipt(sp, ObjectBrief, cur.id, ref, action, stream, reason)
	rc.Source = src
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, 0, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.briefs SET current_version = $2, stream_version = $3, last_receipt_id = $4, updated_at = now()
		 WHERE id = $1 AND space_id = $5`, cur.id, version, stream, rc.ID, sp.ID); err != nil {
		return Receipt{}, 0, fmt.Errorf("ledger: update Brief: %w", err)
	}
	structure, err := json.Marshal(briefStructure{Sections: sections})
	if err != nil {
		return Receipt{}, 0, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.brief_versions (id, brief_id, version, tenant_id, space_id, seq, parent_version, title, summary,
		                               structure, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`,
		newID(), cur.id, version, sp.TenantID, sp.ID, seq, cur.version, title, nullText(summary), structure, rc.ID); err != nil {
		return Receipt{}, 0, fmt.Errorf("ledger: write Brief version: %w", err)
	}
	cur.version, cur.streamVersion, cur.seq = version, stream, seq
	return rc, version, nil
}

// stillSurfaced keeps what an edition lists for a person that still needs
// one: a memory still flagged as a conflict, and not one Dream flagged
// itself (those are its actions).
func (w *writer) stillSurfaced(ctx context.Context, spaceID uuid.UUID, in []Surfaced, ed *editionWrite) ([]Surfaced, error) {
	var out []Surfaced
	mine := map[uuid.UUID]bool{}
	for _, a := range ed.actions {
		if a.kind == DreamConflict {
			mine[a.memory] = true
		}
	}
	for _, s := range in {
		if s.Kind != "conflict" || mine[s.Memory] || slices.ContainsFunc(out, func(x Surfaced) bool { return x.Memory == s.Memory }) {
			continue
		}
		var flagged bool
		if err := w.tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM v2.memories WHERE id = $1 AND space_id = $2 AND 'conflict' = ANY (flags))`,
			s.Memory, spaceID).Scan(&flagged); err != nil {
			return nil, fmt.Errorf("ledger: read conflicts: %w", err)
		}
		if flagged {
			out = append(out, s)
		}
	}
	return out, nil
}
