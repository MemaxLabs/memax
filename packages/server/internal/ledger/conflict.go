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
//     either side, or both), and a flagged proposal is kept. Narrower
//     words that touch another decision in force wait for the judge
//     first (rule 11; see planBoth).
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
		// The memory the command named first, as the first answer had it.
		ids := []uuid.UUID{this.ID}
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
	if err := w.refuseImportPair(ctx, sp.ID, this, p.link.MemoryID); err != nil {
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
		// A decision in force is in the way (the judge may have found it
		// in words narrowed to settle this one): name it.
		refs := make([]string, len(p.alsoConflicts))
		for i, l := range p.alsoConflicts {
			refs[i] = l.Ref
		}
		return Result{}, &InConflictError{Ref: p.flagged.Ref, With: refs[0], Message: fmt.Sprintf(
			"%s also conflicts with %s, %s in force. Settle that first.", p.flagged.Ref, strings.Join(refs, " and "), pluralDecision(len(refs)))}
	}
	// "Keep both" keeps words the judge hasn't seen only when they touch no
	// other decision in force; otherwise they are saved and wait for it.
	var both []narrowing
	if c.Choice == ChooseBoth {
		var hold bool
		if both, hold, err = w.planBoth(ctx, p, c); err != nil {
			return Result{}, err
		}
		if hold {
			return w.holdBoth(ctx, sp, p, both)
		}
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
		for _, n := range both {
			if n.change {
				// A kept side's draft the judge has seen becomes its words;
				// any other new words are a new version.
				if err := add(w.putVersion(ctx, sp, n.m, n.draft, n.words, "Narrowed to settle a conflict.")); err != nil {
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

// narrowing is one side of a "keep both": the words it will stand with.
type narrowing struct {
	m, other *Memory
	words    string
	// change: the words differ from the side's words in force.
	change bool
	// draft is a kept side's draft of these words the judge has seen (or
	// that waited out JudgeGrace), to adopt; 0 when there is none.
	draft int
	// hold: the words touch another decision in force and the judge hasn't
	// seen them, so the resolution waits (holdBoth).
	hold bool
}

// planBoth checks "keep both"'s words against rule 11 before anything is
// kept. Each side's new words are checked inline (Touches) against the
// decisions in force other than the two sides of this conflict, which the
// person is narrowing to stand together:
//
//   - words that touch none are kept at once, as before;
//   - words that touch one wait for the judge: hold is set, and the
//     caller saves them (holdBoth) and answers judge_pending;
//   - on the retry, the saved words are what the person sends again: a
//     proposal's are its current version, which waits while it is inside
//     JudgeGrace without a verdict (503 judge_pending), and a kept side's
//     are its draft, which waits the same way. A contradiction the judge
//     found is a conflict (409 in_conflict naming the decision): on a
//     proposal it is a new conflicts_with link, refused before this, and
//     on a draft it is the draft's verdict.
//
// A refusal outranks a hold, and a hold outranks a wait, so new words are
// never refused for an older wait.
func (w *writer) planBoth(ctx context.Context, p conflictPair, c *ResolveConflict) ([]narrowing, bool, error) {
	pair := []uuid.UUID{p.this.ID, p.other.ID}
	var sides []narrowing
	var wait, conflict error
	hold := false
	for _, s := range []struct {
		m, other *Memory
		words    string
	}{{p.this, p.other, c.Statement}, {p.other, p.this, c.OtherStatement}} {
		n := narrowing{m: s.m, other: s.other, words: s.m.Statement}
		if s.words != "" && s.words != s.m.Statement {
			n.words, n.change = s.words, true
		}
		switch {
		case !n.change && s.m.Lifecycle == lifecycle.Proposed:
			pending, err := w.verdictPending(ctx, s.m.ID, s.m.Version)
			if err != nil {
				return nil, false, err
			}
			if pending {
				touches, err := w.touchesOther(ctx, s.m.SpaceID, pair, n.words, s.m.area())
				if err != nil {
					return nil, false, err
				}
				if touches && wait == nil {
					wait = &JudgePendingError{Ref: s.m.Ref}
				}
			}
		case n.change && s.m.Lifecycle == lifecycle.Kept:
			d, err := w.findDraft(ctx, s.m, n.words)
			if err != nil {
				return nil, false, err
			}
			switch {
			case d == nil:
				if n.hold, err = w.touchesOther(ctx, s.m.SpaceID, pair, n.words, s.m.area()); err != nil {
					return nil, false, err
				}
			case d.contradicts != "":
				if conflict == nil {
					conflict = &InConflictError{Ref: s.m.Ref, With: d.contradicts, Message: fmt.Sprintf(
						"The narrower words for %s contradict %s, a decision in force. Change them, or settle the conflict another way.",
						s.m.Ref, d.contradicts)}
				}
			case d.pending:
				n.draft = d.version
				if wait == nil {
					wait = &JudgePendingError{Ref: s.m.Ref}
				}
			default:
				n.draft = d.version
			}
		case n.change:
			var err error
			if n.hold, err = w.touchesOther(ctx, s.m.SpaceID, pair, n.words, s.m.area()); err != nil {
				return nil, false, err
			}
		}
		hold = hold || n.hold
		sides = append(sides, n)
	}
	switch {
	case conflict != nil:
		return nil, false, conflict
	case hold:
		return sides, true, nil
	case wait != nil:
		return nil, false, wait
	}
	return sides, false, nil
}

// holdBoth saves "keep both"'s words for the judge and settles nothing:
// a proposal's new words become its new version (it stays a flagged
// proposal), and a kept side's words that touch another decision in force
// become a draft, so the words in force stay until the resolution is
// applied. Each is judged in settling mode, beside the other side, in this
// transaction. The answer is "proposed" with policy code judge_pending,
// like edit-then-keep's; the person sends the same resolution again,
// which waits for the verdicts (planBoth).
func (w *writer) holdBoth(ctx context.Context, sp spaceRow, p conflictPair, sides []narrowing) (Result, error) {
	var receipts []Receipt
	var refs []string
	for _, n := range sides {
		if !n.change || n.draft != 0 {
			continue
		}
		switch n.m.Lifecycle {
		case lifecycle.Proposed:
			rc, err := w.writeVersion(ctx, sp, n.m, n.words, "Narrowed to settle a conflict; Memax checks the words before both are kept.")
			if err != nil {
				return Result{}, err
			}
			receipts = append(receipts, rc)
			w.enqueueJudge(JudgeArgs{MemoryID: n.m.ID, SpaceID: sp.ID, Version: n.m.Version, Mode: JudgeSettling, Beside: n.other.ID})
		case lifecycle.Kept:
			if !n.hold {
				continue // words that touch nothing else are kept with the resolution
			}
			rc, version, err := w.writeDraft(ctx, sp, n.m, n.words)
			if err != nil {
				return Result{}, err
			}
			receipts = append(receipts, rc)
			w.enqueueJudge(JudgeArgs{MemoryID: n.m.ID, SpaceID: sp.ID, Version: version, Mode: JudgeSettling, Beside: n.other.ID})
		default:
			continue
		}
		refs = append(refs, n.m.Ref)
	}
	// The memory the command named, then any other the hold saved words
	// for, as a replay lists them.
	ids := []uuid.UUID{p.this.ID}
	for _, rc := range receipts {
		if !slices.Contains(ids, rc.ObjectID) {
			ids = append(ids, rc.ObjectID)
		}
	}
	res, err := w.finish(ctx, Result{Outcome: OutcomeProposed, Policy: heldNarrowing(refs), Receipts: receipts}, p.this.ID)
	if err == nil {
		err = w.loadMemories(ctx, &res, ids)
	}
	return res, err
}

// heldNarrowing is the outcome of a "keep both" whose words wait for the
// judge: saved, not settled.
func heldNarrowing(refs []string) policy.Decision {
	return policy.Decision{Effect: policy.EffectPropose, Code: policy.CodeJudgePending, Message: fmt.Sprintf(
		"Saved the narrower words for %s. Memax is checking them against the other decisions in force before both are kept: "+
			"settle it the same way again in a moment.", strings.Join(refs, " and "))}
}

// writeDraft saves words for a kept memory as a draft: a version above
// its current one, written by a `drafted` receipt. The words in force are
// unchanged; only a resolution the judge has cleared adopts them.
func (w *writer) writeDraft(ctx context.Context, sp spaceRow, m *Memory, statement string) (Receipt, int, error) {
	if secrets := findSecrets(statement); len(secrets) > 0 {
		return Receipt{}, 0, invalid("statement", "looks like a credential (%s); Memax never stores secrets", strings.Join(secrets, ", "))
	}
	rc := w.receipt(sp, m.ID, m.Ref, ActionDrafted, m.streamVersion+1,
		"Narrower words to settle a conflict, held for Memax's check; the words in force are unchanged.")
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, 0, err
	}
	version, err := w.nextVersion(ctx, m.ID)
	if err != nil {
		return Receipt{}, 0, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $5)`, m.ID, version, sp.ID, statement, rc.ID); err != nil {
		return Receipt{}, 0, fmt.Errorf("ledger: write draft: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories SET stream_version = $2, last_receipt_id = $3 WHERE id = $1 AND space_id = $4`,
		m.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Receipt{}, 0, fmt.Errorf("ledger: update memory: %w", err)
	}
	m.streamVersion = rc.StreamVersion
	return rc, version, nil
}

// draftState is a kept memory's newest draft of some words.
type draftState struct {
	version int
	// pending: no verdict yet, inside JudgeGrace.
	pending bool
	// contradicts is the decision in force the judge found the words
	// contradict, if it still is one.
	contradicts string
}

// findDraft looks up a kept memory's newest draft of exactly these words,
// above its current version; nil when there's none.
func (w *writer) findDraft(ctx context.Context, m *Memory, words string) (*draftState, error) {
	var d draftState
	var outcome *string
	var related *int64
	var inForce *bool
	err := w.tx.QueryRow(ctx, `
		SELECT v.version, j.outcome IS NULL AND v.created_at > now() - make_interval(secs => $5), j.outcome, rm.seq,
		       rm.kind = 'decision' AND rm.lifecycle = 'kept' AND COALESCE(rm.decision ->> 'status', '') IN ('', 'in_force')
		  FROM v2.memory_versions v
		  JOIN v2.receipts r ON r.id = v.receipt_id AND r.action = 'drafted'
		  LEFT JOIN LATERAL (SELECT outcome, related_memory_id FROM v2.judge_verdicts
		                      WHERE memory_id = v.memory_id AND version = v.version ORDER BY round DESC LIMIT 1) j ON true
		  LEFT JOIN v2.memories rm ON rm.id = j.related_memory_id
		 WHERE v.memory_id = $1 AND v.space_id = $2 AND v.version > $3 AND v.statement = $4
		 ORDER BY v.version DESC LIMIT 1`,
		m.ID, m.SpaceID, m.Version, words, JudgeGrace.Seconds()).Scan(&d.version, &d.pending, &outcome, &related, &inForce)
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read draft: %w", err)
	}
	if outcome != nil && VerdictOutcome(*outcome) == OutcomeFlagged && related != nil && inForce != nil && *inForce {
		d.contradicts = FormatRef(PrefixMemory, *related)
	}
	return &d, nil
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
	return w.putVersion(ctx, sp, m, 0, statement, reason)
}

// putVersion makes statement a memory's words, with an `edited` receipt:
// a new version, or (draft > 0) the draft that holds those words already,
// once the judge has seen it (planBoth).
func (w *writer) putVersion(ctx context.Context, sp spaceRow, m *Memory, draft int, statement, reason string) (Receipt, error) {
	if secrets := findSecrets(statement); len(secrets) > 0 {
		return Receipt{}, invalid("statement", "looks like a credential (%s); Memax never stores secrets", strings.Join(secrets, ", "))
	}
	rc := w.receipt(sp, m.ID, m.Ref, ActionEdited, m.streamVersion+1, reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	version := draft
	if version == 0 {
		var err error
		if version, err = w.nextVersion(ctx, m.ID); err != nil {
			return Receipt{}, err
		}
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $5)`, m.ID, version, sp.ID, statement, rc.ID); err != nil {
			return Receipt{}, fmt.Errorf("ledger: write version: %w", err)
		}
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
