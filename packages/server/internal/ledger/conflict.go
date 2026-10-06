package ledger

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Conflicts (rule 11): the judge flags a memory that contradicts a
// decision in force and links it to the decision (conflicts_with). One
// answer must win, so a person settles it (ReviewConflict):
//
//   - keep_this / keep_other: one side wins. A winning proposal is kept.
//     The losing side gives way: a proposal is rejected; a kept decision
//     is superseded (it stays kept with its history, decision status
//     "superseded", and stops compiling); a kept fact fades (restorable).
//     The winner gets a `supersedes` link to a kept loser.
//   - keep_both: both stand, usually with narrower words (an edit of
//     either side, or both), and a flagged proposal is kept.
//   - leave_open: it is undecided. The flagged side becomes an open
//     question (kept, in the open-question section), and a decision in
//     force on the other side is set to "open" too, so no agent reads it
//     as decided.
//
// Every change has its receipt, the flag is cleared, the conflicts_with
// link ends, and the whole resolution is one Undo away.

// ConflictChoice is one of ReviewConflict's answers.
type ConflictChoice string

// The choices, relative to the memory the command names ("this side").
const (
	ChooseThis  ConflictChoice = "keep_this"
	ChooseOther ConflictChoice = "keep_other"
	ChooseBoth  ConflictChoice = "keep_both"
	ChooseOpen  ConflictChoice = "leave_open"
)

// ConflictChoices lists every choice, in ReviewConflict's order.
var ConflictChoices = []ConflictChoice{ChooseThis, ChooseOther, ChooseBoth, ChooseOpen}

// What a resolution does to each side (ConflictEffect.Change).
const (
	EffectKept       = "kept"
	EffectRejected   = "rejected"
	EffectSuperseded = "superseded"
	EffectFaded      = "faded"
	EffectOpen       = "open"
	EffectStays      = "stays"
)

// ConflictChanges lists every effect.
var ConflictChanges = []string{EffectKept, EffectRejected, EffectSuperseded, EffectFaded, EffectOpen, EffectStays}

// CommandResolveConflict names ResolveConflict.
const CommandResolveConflict CommandName = "resolve_conflict"

// ResolveConflict settles the conflict between Memory and Other.
type ResolveConflict struct {
	Meta
	// Memory is this side, by display ID or uuid.
	Memory string
	// Other is the other side; optional when this side has one conflict.
	Other  string
	Choice ConflictChoice
	// ExpectedVersion, when set, must be this side's version (If-Match).
	ExpectedVersion int
	// Statement and OtherStatement are narrower words for keep_both.
	Statement      string
	OtherStatement string
}

// Name implements Command.
func (*ResolveConflict) Name() CommandName { return CommandResolveConflict }

func (c *ResolveConflict) validate() error {
	if err := validateTarget(c.Memory, c.ExpectedVersion, false); err != nil {
		return err
	}
	if !slices.Contains(ConflictChoices, c.Choice) {
		return invalid("choice", "use keep_this, keep_other, keep_both or leave_open")
	}
	c.Statement, c.OtherStatement = strings.TrimSpace(c.Statement), strings.TrimSpace(c.OtherStatement)
	if c.Choice != ChooseBoth && (c.Statement != "" || c.OtherStatement != "") {
		return invalid("statement", "new words go with keep_both only")
	}
	if err := checkText("statement", c.Statement, MaxStatementRunes, false); err != nil {
		return err
	}
	return checkText("other_statement", c.OtherStatement, MaxStatementRunes, false)
}

// conflictPair is one active conflict seen from this side.
type conflictPair struct {
	this, other *Memory
	// flagged carries the flag (the link's from side); decision is the
	// other end (the link's to side).
	flagged, decision *Memory
	link              Link
	// alsoConflicts are the flagged side's other active conflicts.
	alsoConflicts []Link
}

func conflictStateError(ref, msg string) error {
	return &TransitionError{Ref: ref, Err: &lifecycle.TransitionError{Verb: "resolve_conflict", Message: msg}}
}

// findConflict picks this side's conflict (with other, when named; field
// is what the caller calls other, for the error).
func findConflict(ctx context.Context, tx pgx.Tx, scope Scope, this *Memory, other, field string) (conflictPair, error) {
	links, err := activeLinks(ctx, tx, []uuid.UUID{this.ID})
	if err != nil {
		return conflictPair{}, err
	}
	var cands []Link
	for _, l := range links[this.ID] {
		if l.Kind == LinkConflictsWith {
			cands = append(cands, l)
		}
	}
	if other != "" {
		id, err := resolveRef(ctx, tx, scope.Narrow(this.SpaceID), other)
		if err != nil {
			return conflictPair{}, err
		}
		cands = slices.DeleteFunc(cands, func(l Link) bool { return l.MemoryID != id })
		if len(cands) == 0 {
			return conflictPair{}, conflictStateError(this.Ref, fmt.Sprintf("%s doesn't conflict with %s.", this.Ref, other))
		}
	}
	switch {
	case len(cands) == 0:
		return conflictPair{}, conflictStateError(this.Ref, fmt.Sprintf("%s has no conflict to settle.", this.Ref))
	case len(cands) > 1:
		refs := make([]string, len(cands))
		for i, l := range cands {
			refs[i] = l.Ref
		}
		return conflictPair{}, invalid(field, "%s conflicts with %s; say which one with %s", this.Ref, strings.Join(refs, " and "), field)
	}
	return conflictPair{this: this, link: cands[0]}, nil
}

// settle completes the pair once both sides are loaded.
func (p *conflictPair) settle(ctx context.Context, tx pgx.Tx, other *Memory) error {
	p.other = other
	p.flagged, p.decision = p.this, other
	if p.link.Direction == LinkIn {
		p.flagged, p.decision = other, p.this
	}
	links, err := activeLinks(ctx, tx, []uuid.UUID{p.flagged.ID})
	if err != nil {
		return err
	}
	for _, l := range links[p.flagged.ID] {
		if l.Kind == LinkConflictsWith && l.Direction == LinkOut && l.ID != p.link.ID {
			p.alsoConflicts = append(p.alsoConflicts, l)
		}
	}
	return nil
}

// plan says what a choice does to each side.
func (p *conflictPair) plan(choice ConflictChoice) []ConflictEffect {
	keepOrStay := func(m *Memory) string {
		if m.Lifecycle == lifecycle.Proposed {
			return EffectKept
		}
		return EffectStays
	}
	giveWay := func(m *Memory) string {
		switch {
		case m.Lifecycle == lifecycle.Proposed:
			return EffectRejected
		case m.Kind == KindDecision:
			return EffectSuperseded
		}
		return EffectFaded
	}
	switch choice {
	case ChooseThis:
		return []ConflictEffect{{p.this.Ref, keepOrStay(p.this)}, {p.other.Ref, giveWay(p.other)}}
	case ChooseOther:
		return []ConflictEffect{{p.this.Ref, giveWay(p.this)}, {p.other.Ref, keepOrStay(p.other)}}
	case ChooseBoth:
		return []ConflictEffect{{p.this.Ref, keepOrStay(p.this)}, {p.other.Ref, keepOrStay(p.other)}}
	}
	effects := []ConflictEffect{}
	for _, m := range []*Memory{p.this, p.other} {
		if m == p.flagged || m.inForce() {
			effects = append(effects, ConflictEffect{m.Ref, EffectOpen})
		} else {
			effects = append(effects, ConflictEffect{m.Ref, EffectStays})
		}
	}
	return effects
}

// object is what policy sees for a choice: a decision is involved, and
// (when a proposal would be kept) whether it is quarantined.
func (p *conflictPair) object(choice ConflictChoice) policy.Object {
	o := policy.Object{Ref: p.flagged.Ref, Decision: p.flagged.Kind == KindDecision || p.decision.Kind == KindDecision}
	for _, e := range p.plan(choice) {
		for _, m := range []*Memory{p.this, p.other} {
			if m.Ref == e.Ref && m.Lifecycle == lifecycle.Proposed && (e.Change == EffectKept || e.Change == EffectOpen) {
				o.External = o.External || m.Trust.External()
			}
		}
	}
	return o
}

// keepsFlagged reports whether a choice keeps the flagged side (so its
// other conflicts must be settled first).
func (p *conflictPair) keepsFlagged(choice ConflictChoice) bool {
	for _, e := range p.plan(choice) {
		if e.Ref == p.flagged.Ref {
			return e.Change == EffectKept || e.Change == EffectStays || e.Change == EffectOpen
		}
	}
	return false
}

// resolveConflict applies ResolveConflict.
func (w *writer) resolveConflict(ctx context.Context, c *ResolveConflict) (Result, error) {
	this, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		ids := []uuid.UUID{}
		for _, rc := range replay.Receipts {
			if !slices.Contains(ids, rc.ObjectID) {
				ids = append(ids, rc.ObjectID)
			}
		}
		err := w.loadMemories(ctx, replay, ids)
		return *replay, err
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != this.Version {
		return Result{}, &EditClashError{Ref: this.Ref, Expected: c.ExpectedVersion, Current: this.Version}
	}
	p, err := findConflict(ctx, w.tx, w.meta.Scope, this, c.Other, "other")
	if err != nil {
		return Result{}, err
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, []uuid.UUID{p.link.MemoryID}, "FOR UPDATE")
	if err != nil {
		return Result{}, err
	}
	other := locked[p.link.MemoryID]
	if other == nil {
		return Result{}, ErrNotFound
	}
	if err := p.settle(ctx, w.tx, other); err != nil {
		return Result{}, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionResolveConflict, p.object(c.Choice), sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if !p.flagged.Flags.Has(lifecycle.Conflict) {
		return Result{}, conflictStateError(p.flagged.Ref, fmt.Sprintf("%s isn't flagged as a conflict any more. Reload it.", p.flagged.Ref))
	}
	if len(p.alsoConflicts) > 0 && p.keepsFlagged(c.Choice) {
		refs := make([]string, len(p.alsoConflicts))
		for i, l := range p.alsoConflicts {
			refs[i] = l.Ref
		}
		return Result{}, conflictStateError(p.flagged.Ref, fmt.Sprintf(
			"%s also conflicts with %s. Settle that first.", p.flagged.Ref, strings.Join(refs, " and ")))
	}

	w.startUndo(UndoResolve, w.undoWindow)
	w.undo.touch(p.this)
	w.undo.touch(p.other)
	reason := w.meta.Reason
	if reason == "" {
		reason = resolutionReason(c.Choice, p)
	}
	var receipts []Receipt
	add := func(rc Receipt, err error) error {
		if err == nil {
			receipts = append(receipts, rc)
		}
		return err
	}

	// The flagged side: the conflict is settled, the link ends, and (once
	// no other conflict is left) the flag goes.
	ends := []Link{p.link}
	losesFlagged := !p.keepsFlagged(c.Choice)
	if losesFlagged {
		ends = append(ends, p.alsoConflicts...)
	}
	rc := w.receipt(sp, p.flagged.ID, p.flagged.Ref, ActionResolved, p.flagged.streamVersion+1, reason)
	rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: p.decision.Ref}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	receipts = append(receipts, rc)
	for _, l := range ends {
		if err := w.endLink(ctx, l, rc.ID); err != nil {
			return Result{}, err
		}
	}
	cleared, err := transition(p.flagged, lifecycle.VerbClearConflict)
	if err != nil {
		return Result{}, err
	}
	if err := w.setState(ctx, sp, p.flagged, cleared, rc); err != nil {
		return Result{}, err
	}

	switch c.Choice {
	case ChooseThis, ChooseOther:
		winner, loser := p.this, p.other
		if c.Choice == ChooseOther {
			winner, loser = p.other, p.this
		}
		if winner.Lifecycle == lifecycle.Proposed {
			if err := add(w.keepIt(ctx, sp, grant, winner, "")); err != nil {
				return Result{}, err
			}
		}
		if err := w.giveWay(ctx, sp, grant, loser, winner, &receipts); err != nil {
			return Result{}, err
		}
	case ChooseBoth:
		for _, s := range []struct {
			m         *Memory
			statement string
		}{{p.this, c.Statement}, {p.other, c.OtherStatement}} {
			if s.statement != "" && s.statement != s.m.Statement {
				if err := add(w.writeVersion(ctx, sp, s.m, s.statement, "Narrowed to settle a conflict.")); err != nil {
					return Result{}, err
				}
			}
		}
		if p.flagged.Lifecycle == lifecycle.Proposed {
			if err := add(w.keepIt(ctx, sp, grant, p.flagged, "")); err != nil {
				return Result{}, err
			}
		}
	case ChooseOpen:
		for _, m := range []*Memory{p.flagged, p.decision} {
			if m != p.flagged && !m.inForce() {
				continue
			}
			if m.Lifecycle == lifecycle.Proposed {
				if err := add(w.keepIt(ctx, sp, grant, m, "")); err != nil {
					return Result{}, err
				}
			}
			status := ""
			if m.Kind == KindDecision {
				status = DecisionOpen
			}
			if err := add(w.reshape(ctx, sp, m, ActionResolved, "Left open: it is undecided.", SectionOpenQuestion, status, nil)); err != nil {
				return Result{}, err
			}
		}
	}
	if err := w.markDirty(ctx, sp.ID); err != nil {
		return Result{}, err
	}
	if err := w.writeUndo(ctx, sp, receipts); err != nil {
		return Result{}, err
	}
	res, err := w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, this.ID)
	if err == nil {
		err = w.loadMemories(ctx, &res, []uuid.UUID{p.this.ID, p.other.ID})
	}
	return res, err
}

func resolutionReason(choice ConflictChoice, p conflictPair) string {
	switch choice {
	case ChooseThis:
		return fmt.Sprintf("Settled: %s over %s.", p.this.Ref, p.other.Ref)
	case ChooseOther:
		return fmt.Sprintf("Settled: %s over %s.", p.other.Ref, p.this.Ref)
	case ChooseBoth:
		return fmt.Sprintf("Settled: %s and %s both stand.", p.this.Ref, p.other.Ref)
	}
	return fmt.Sprintf("Left open: %s and %s are undecided.", p.this.Ref, p.other.Ref)
}

// keepIt keeps a proposal as part of a resolution: a person's Keep, with
// its assurance.
func (w *writer) keepIt(ctx context.Context, sp spaceRow, grant SpaceGrant, m *Memory, reason string) (Receipt, error) {
	next, err := transition(m, lifecycle.VerbKeep)
	if err != nil {
		return Receipt{}, err
	}
	rc, err := w.changeState(ctx, sp, grant, m, next, ActionKept, m.streamVersion+1, reason)
	if err != nil {
		return Receipt{}, err
	}
	m.Lifecycle, m.Flags, m.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
	return rc, nil
}

// giveWay makes the losing side of a conflict give way to the winner.
func (w *writer) giveWay(ctx context.Context, sp spaceRow, grant SpaceGrant, loser, winner *Memory, receipts *[]Receipt) error {
	reason := fmt.Sprintf("Settled for %s.", winner.Ref)
	switch {
	case loser.Lifecycle == lifecycle.Proposed:
		next, err := transition(loser, lifecycle.VerbReject)
		if err != nil {
			return err
		}
		rc, err := w.changeState(ctx, sp, grant, loser, next, ActionRejected, loser.streamVersion+1, reason)
		if err != nil {
			return err
		}
		loser.Lifecycle, loser.Flags, loser.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
		*receipts = append(*receipts, rc)
		return nil
	case loser.Kind == KindDecision:
		rc, err := w.supersedeDecision(ctx, sp, loser, winner)
		if err != nil {
			return err
		}
		*receipts = append(*receipts, rc)
	default:
		next, err := transition(loser, lifecycle.VerbFade)
		if err != nil {
			return err
		}
		rc := w.receipt(sp, loser.ID, loser.Ref, ActionFaded, loser.streamVersion+1, reason)
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: winner.Ref}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return err
		}
		if err := w.setState(ctx, sp, loser, next, rc); err != nil {
			return err
		}
		*receipts = append(*receipts, rc)
	}
	_, err := w.insertLink(ctx, sp.ID, LinkSupersedes, winner.ID, loser.ID, (*receipts)[len(*receipts)-1].ID)
	return err
}

// supersedeDecision marks a kept decision superseded by another memory:
// it stays kept with its history, and stops compiling.
func (w *writer) supersedeDecision(ctx context.Context, sp spaceRow, d, by *Memory) (Receipt, error) {
	w.undo.touch(d)
	return w.reshape(ctx, sp, d, ActionSuperseded, fmt.Sprintf("Superseded by %s.", by.Ref), "", DecisionSuperseded,
		&ReceiptSource{Kind: ObjectMemory, Ref: by.Ref})
}

// setState writes a memory's lifecycle and flags with a receipt already
// inserted.
func (w *writer) setState(ctx context.Context, sp spaceRow, m *Memory, next lifecycle.State, rc Receipt) error {
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET lifecycle = $2, flags = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $6`,
		m.ID, string(next.Lifecycle), next.Flags.Strings(), rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return fmt.Errorf("ledger: update memory: %w", err)
	}
	m.Lifecycle, m.Flags, m.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
	return nil
}

// reshape moves a memory to another section and/or sets its decision
// status, with one receipt. Empty section or status leaves it as it is.
func (w *writer) reshape(ctx context.Context, sp spaceRow, m *Memory, action Action, reason string, section Section, status string, src *ReceiptSource) (Receipt, error) {
	rc := w.receipt(sp, m.ID, m.Ref, action, m.streamVersion+1, reason)
	rc.Source = src
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	if section == "" {
		section = m.Section
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET section = $2,
		       decision = CASE WHEN $3 = '' THEN decision
		                       ELSE jsonb_set(COALESCE(decision, '{}'::jsonb), '{status}', to_jsonb($3::text)) END,
		       stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $6`,
		m.ID, string(section), status, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: update memory: %w", err)
	}
	m.Section, m.streamVersion = section, rc.StreamVersion
	if status != "" {
		if m.Decision == nil {
			m.Decision = &DecisionFields{}
		}
		m.Decision.Status = status
	}
	return rc, nil
}

// writeVersion writes a new version of a memory's statement, as part of
// a larger command, with its `edited` receipt.
func (w *writer) writeVersion(ctx context.Context, sp spaceRow, m *Memory, statement, reason string) (Receipt, error) {
	if secrets := findSecrets(statement); len(secrets) > 0 {
		return Receipt{}, invalid("statement", "looks like a credential (%s); Memax never stores secrets", strings.Join(secrets, ", "))
	}
	rc := w.receipt(sp, m.ID, m.Ref, ActionEdited, m.streamVersion+1, reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	version, err := w.nextVersion(ctx, m.ID)
	if err != nil {
		return Receipt{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $5)`, m.ID, version, sp.ID, statement, rc.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: write version: %w", err)
	}
	w.indexVersion(sp.ID, m.ID, version)
	hash, bands := signature(statement)
	if _, err := w.tx.Exec(ctx, fmt.Sprintf(`
		UPDATE v2.memories
		   SET current_version = $2, search = %s, content_sha256 = $4, minhash_bands = $5,
		       flags = $6, stream_version = $7, last_receipt_id = $8, updated_at = now()
		 WHERE id = $1 AND space_id = $9`, fmt.Sprintf(searchExpr, "$3::text")),
		m.ID, version, statement, hash, bands, m.Flags.Without(lifecycle.Stale).Strings(), rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: update memory: %w", err)
	}
	m.Version, m.Statement, m.Flags, m.streamVersion = version, statement, m.Flags.Without(lifecycle.Stale), rc.StreamVersion
	return rc, nil
}

// nextVersion is the number of a memory's next statement version. Undo
// can move current_version back, so it counts from the highest stored.
func (w *writer) nextVersion(ctx context.Context, id uuid.UUID) (int, error) {
	var v int
	if err := w.tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) + 1 FROM v2.memory_versions WHERE memory_id = $1`, id).Scan(&v); err != nil {
		return 0, fmt.Errorf("ledger: next version: %w", err)
	}
	return v, nil
}

// ---------------------------------------------------------------------
// Reading a conflict (ReviewConflict)
// ---------------------------------------------------------------------

// ConflictEffect is what a choice does to one side.
type ConflictEffect struct {
	Ref    string `json:"ref"`
	Change string `json:"change"`
}

// ConflictOption is one choice, what it would do, and whether the reader
// may take it.
type ConflictOption struct {
	Choice  ConflictChoice   `json:"choice"`
	Effects []ConflictEffect `json:"effects"`
	Allowed bool             `json:"allowed"`
	// Policy says why it isn't allowed.
	Policy *policy.Decision `json:"policy,omitempty"`
}

// ConflictView is both sides of a conflict, side by side.
type ConflictView struct {
	// Memory is this side and Other the other, each with its sources,
	// links and the judge's verdict.
	Memory *Memory `json:"memory"`
	Other  *Memory `json:"other"`
	// FlaggedRef is the side carrying the flag; DecisionRef the decision
	// in force it contradicts.
	FlaggedRef  string `json:"flagged_ref"`
	DecisionRef string `json:"decision_ref"`
	// Link is the conflicts_with link, seen from this side.
	Link Link `json:"link"`
	// Receipts are both sides' latest receipts, newest first.
	Receipts []Receipt `json:"receipts"`
	// Options are ReviewConflict's four answers.
	Options []ConflictOption `json:"options"`
}

// GetConflict reads one of this memory's conflicts (the one with other,
// when named) for ReviewConflict, with the choices the actor may take.
func (l *Ledger) GetConflict(ctx context.Context, scope Scope, actor Actor, via policy.Via, ref, other string) (*ConflictView, error) {
	var out *ConflictView
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		this, err := loadMemory(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		p, err := findConflict(ctx, tx, scope, this, other, "with")
		if err != nil {
			return err
		}
		that, err := loadMemory(ctx, tx, scope, p.link.MemoryID, false)
		if err != nil {
			return err
		}
		if err := p.settle(ctx, tx, that); err != nil {
			return err
		}
		for _, m := range []*Memory{this, that} {
			if m.Sources, err = loadSources(ctx, tx, m.ID); err != nil {
				return err
			}
		}
		if err := attachDetails(ctx, tx, []*Memory{this, that}); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, receiptSelect+`
			 WHERE object_id = ANY ($1) AND space_id = $2 ORDER BY seq DESC LIMIT 20`, []uuid.UUID{this.ID, that.ID}, this.SpaceID)
		if err != nil {
			return fmt.Errorf("ledger: conflict receipts: %w", err)
		}
		receipts, err := pgx.CollectRows(rows, scanReceipt)
		if err != nil {
			return fmt.Errorf("ledger: conflict receipts: %w", err)
		}
		sp, err := loadSpace(ctx, tx, this.SpaceID)
		if err != nil {
			return err
		}
		grant, _ := scope.Grant(this.SpaceID)
		pa := toPolicyActor(actor, via, grant)
		v := &ConflictView{Memory: this, Other: that, FlaggedRef: p.flagged.Ref, DecisionRef: p.decision.Ref,
			Link: p.link, Receipts: nonNilSlice(receipts)}
		for _, ch := range ConflictChoices {
			opt := ConflictOption{Choice: ch, Effects: p.plan(ch), Allowed: true}
			d := policy.Decide(pa, policy.ActionResolveConflict, p.object(ch), sp.policy())
			if d.Effect == policy.EffectRefuse {
				opt.Allowed, opt.Policy = false, &d
			} else if len(p.alsoConflicts) > 0 && p.keepsFlagged(ch) {
				opt.Allowed = false
			}
			v.Options = append(v.Options, opt)
		}
		out = v
		return nil
	})
	return out, err
}
