package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Undoing one of Dream's actions (rule 9: every action type → undo → state
// restored exactly). The inverse names the memories' states before (no
// words), the links the action made and the Brief version it replaced.
// Undo applies it as one command, by a person who may keep, with an
// `undid` receipt citing the action's own receipt. What it restores:
//
//   - fold: the folded_from links end; the memory is as it was.
//   - propose: the proposal is withdrawn: it leaves Review as rejected (a
//     memory is never deleted, and a rejected one keeps Dream from
//     proposing the same words again for 90 days). Kept set, Review queue
//     and compiles are as before the edition.
//   - dedupe: the folded proposal is back in Review; the link ends.
//   - conflict: the flag goes (unless it was there before) and the link ends.
//   - stale: the flag goes.
//   - fade: the memory is kept again, and compiles again.
//   - brief: a new B- version with the structure Dream replaced.
//
// It is refused, with Undo's reasons, when it was undone already, when the
// window has passed (30 days by default), when a memory it touched was
// forgotten, and when something later depends on it: the memory changed
// again since (other than the judge's verdicts), a link it made is gone,
// or the Brief moved on.

// undoDreamAction applies UndoDreamAction.
func (w *writer) undoDreamAction(ctx context.Context, c *UndoDreamAction) (Result, error) {
	a, err := loadDreamAction(ctx, w.tx, w.meta.Scope, c.Action, true)
	if err != nil {
		return Result{}, err
	}
	grant, ok := w.meta.Scope.Grant(a.spaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, a.spaceID)
	if err != nil {
		return Result{}, err
	}
	replay, err := w.claimKey(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := replay.result(ctx, w)
		if err == nil {
			res.DreamAction, err = w.reloadDreamAction(ctx, a.ID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionUndoDream, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	what := dreamActionNoun(a.Kind)
	ref := a.EditionRef
	if a.Memory != nil {
		ref = a.Memory.Ref
	}
	switch {
	case a.Undone != nil:
		return Result{}, &UndoError{Reason: UndoAlreadyUndone, Ref: ref,
			Message: fmt.Sprintf("This was already undone. %s is as it was before %s.", ref, a.EditionRef)}
	case a.CreatedAt.Add(w.dreamUndoWindow).Before(w.now):
		return Result{}, &UndoError{Reason: UndoWindowPassed, Ref: ref, Message: fmt.Sprintf(
			"Dream's %s is too old to undo. Change %s directly instead.", what, ref)}
	}
	var inv dreamInverse
	if err := json.Unmarshal(a.inverse, &inv); err != nil {
		return Result{}, fmt.Errorf("ledger: dream action %s: %w", a.ID, err)
	}
	reason := w.meta.Reason
	if reason == "" {
		reason = fmt.Sprintf("Undid Dream's %s (%s).", what, a.EditionRef)
	}
	src := &ReceiptSource{Kind: "receipt", Ref: a.receiptID.String()}

	var receipts []Receipt
	var touched []uuid.UUID
	dirty := false
	if a.Kind == DreamBrief {
		rc, err := w.undoDreamBrief(ctx, sp, a, inv, reason, src)
		if err != nil {
			return Result{}, err
		}
		receipts, dirty = append(receipts, rc), true
	} else {
		var ids []uuid.UUID
		for _, um := range inv.Memories {
			ids = append(ids, um.ID)
		}
		locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR UPDATE")
		if err != nil {
			return Result{}, err
		}
		for _, um := range inv.Memories {
			m := locked[um.ID]
			if m == nil {
				return Result{}, ErrNotFound
			}
			if m.Lifecycle == lifecycle.Forgotten {
				return Result{}, &UndoError{Reason: UndoNotUndoable, Ref: m.Ref, Message: fmt.Sprintf(
					"%s was forgotten, and forgetting can't be undone. If it becomes true again, remember it fresh.", m.Ref)}
			}
			if m.streamVersion != um.AfterStreamVersion {
				later, err := w.changedSince(ctx, sp.ID, m.ID, um.AfterStreamVersion)
				if err != nil {
					return Result{}, err
				}
				if later {
					return Result{}, &UndoError{Reason: UndoLaterChanges, Ref: m.Ref, Message: fmt.Sprintf(
						"%s changed after Dream's %s, so undoing it would lose that change. Undo the later change first, or change %s directly.",
						m.Ref, what, m.Ref)}
				}
			}
			if m.Version != um.Before.Version && !inv.Created {
				return Result{}, &UndoError{Reason: UndoLaterChanges, Ref: m.Ref, Message: fmt.Sprintf(
					"%s has new words since Dream's %s. Change it directly instead.", m.Ref, what)}
			}
		}
		if len(inv.LinksCreated) > 0 {
			var active int
			if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM v2.memory_links WHERE id = ANY ($1) AND ended_receipt_id IS NULL`,
				inv.LinksCreated).Scan(&active); err != nil {
				return Result{}, fmt.Errorf("ledger: undo links: %w", err)
			}
			if active != len(inv.LinksCreated) {
				return Result{}, &UndoError{Reason: UndoLaterChanges, Ref: ref, Message: fmt.Sprintf(
					"What Dream linked to %s has changed since. Undo the later change first.", ref)}
			}
		}
		for _, um := range inv.Memories {
			m := locked[um.ID]
			to, err := dreamRestoreState(m, um, inv)
			if err != nil {
				var te *lifecycle.TransitionError
				if errors.As(err, &te) {
					return Result{}, &TransitionError{Ref: m.Ref, Err: te}
				}
				return Result{}, err
			}
			rc := w.receipt(sp, m.ID, m.Ref, ActionUndid, m.streamVersion+1, reason)
			rc.Source = src
			if err := insertReceipt(ctx, w.tx, &rc); err != nil {
				return Result{}, err
			}
			if _, err := w.tx.Exec(ctx, `
				UPDATE v2.memories
				   SET lifecycle = $2, flags = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
				 WHERE id = $1 AND space_id = $6`,
				m.ID, string(to.Lifecycle), to.Flags.Strings(), rc.StreamVersion, rc.ID, sp.ID); err != nil {
				return Result{}, fmt.Errorf("ledger: undo %s: %w", m.Ref, err)
			}
			if m.Lifecycle == lifecycle.Kept || to.Lifecycle == lifecycle.Kept {
				dirty = true
			}
			receipts = append(receipts, rc)
			touched = append(touched, m.ID)
		}
		for _, id := range inv.LinksCreated {
			if _, err := w.tx.Exec(ctx, `UPDATE v2.memory_links SET ended_receipt_id = $2, ended_at = now()
			                              WHERE id = $1 AND ended_receipt_id IS NULL`, id, receipts[0].ID); err != nil {
				return Result{}, fmt.Errorf("ledger: undo link: %w", err)
			}
		}
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.dream_actions SET undone_by = $2, undone_at = now(), last_receipt_id = $2 WHERE id = $1 AND space_id = $3`,
		a.ID, receipts[0].ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: undo dream action: %w", err)
	}
	if dirty {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}
	if err := w.record(ctx, res, a.ID); err != nil {
		return Result{}, err
	}
	if len(touched) > 0 {
		if err := w.loadMemories(ctx, &res, touched); err != nil {
			return Result{}, err
		}
	}
	res.DreamAction, err = w.reloadDreamAction(ctx, a.ID)
	return res, err
}

func (w *writer) reloadDreamAction(ctx context.Context, id uuid.UUID) (*DreamAction, error) {
	a, err := loadDreamAction(ctx, w.tx, w.meta.Scope, id, false)
	if err != nil {
		return nil, err
	}
	a.Undoable = a.Undone == nil && a.CreatedAt.Add(w.dreamUndoWindow).After(w.now)
	return a, nil
}

// dreamRestoreState is the state undoing an action puts a memory back in.
func dreamRestoreState(m *Memory, um undoMemory, inv dreamInverse) (lifecycle.State, error) {
	if inv.Created {
		// A proposal Dream made: withdrawn, as rejected.
		if m.Lifecycle != lifecycle.Proposed {
			return lifecycle.State{}, &lifecycle.TransitionError{From: m.state(), Verb: "undo",
				Message: fmt.Sprintf("%s isn't waiting in Review any more, so Dream's proposal can't be withdrawn. Change it directly.", m.Ref)}
		}
		return lifecycle.Transition(m.state(), lifecycle.VerbReject)
	}
	flags, err := lifecycle.ParseFlags(um.Before.Flags)
	if err != nil {
		return lifecycle.State{}, err
	}
	to := lifecycle.State{Lifecycle: um.Before.Lifecycle, Flags: flags}
	if err := lifecycle.CanRestore(m.state(), to); err != nil {
		return lifecycle.State{}, err
	}
	return to, nil
}

// undoDreamBrief writes the Brief version Dream replaced back, as a new
// version by the person, if Dream's is still the one in force and what it
// placed and cited still holds.
func (w *writer) undoDreamBrief(ctx context.Context, sp spaceRow, a *DreamAction, inv dreamInverse, reason string, src *ReceiptSource) (Receipt, error) {
	cur, err := lockBrief(ctx, w.tx, sp.ID)
	if err != nil {
		return Receipt{}, err
	}
	if cur == nil || cur.id != inv.BriefID || cur.version != inv.BriefAfter {
		return Receipt{}, &UndoError{Reason: UndoLaterChanges, Ref: a.Brief.Ref, Message: fmt.Sprintf(
			"The Brief changed after Dream's edit (%s). Restore an older version from the Brief's history instead.", a.Brief.Ref)}
	}
	before, err := briefVersion(ctx, w.tx, sp.ID, inv.BriefID, inv.BriefBefore)
	if err != nil {
		return Receipt{}, err
	}
	if err := checkBriefCites(ctx, w.tx, sp.ID, before.Sections); err != nil {
		return Receipt{}, &UndoError{Reason: UndoLaterChanges, Ref: a.Brief.Ref, Message:
		// The older version places or cites a memory that is no longer kept.
		"The Brief before Dream's edit rests on a memory that isn't kept any more. Edit the Brief directly instead."}
	}
	rc, _, err := w.writeBriefVersion(ctx, sp, cur, before.Title, before.Summary, before.Sections, reason, src, ActionUndid)
	return rc, err
}

func dreamActionNoun(k DreamActionKind) string {
	switch k {
	case DreamFold:
		return "fold"
	case DreamPropose:
		return "proposal"
	case DreamDedupe:
		return "fold of a duplicate"
	case DreamConflict:
		return "conflict flag"
	case DreamStale:
		return "stale flag"
	case DreamFade:
		return "fade"
	}
	return "change to the Brief"
}

// restore applies Restore: a person keeps a faded memory again.
func (w *writer) restore(ctx context.Context, c *Restore) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: c.ExpectedVersion, Current: mem.Version}
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionRestore, w.object(mem, false), sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	next, err := transition(mem, lifecycle.VerbRestore)
	if err != nil {
		return Result{}, err
	}
	rc, err := w.changeState(ctx, sp, grant, mem, next, ActionRestored, mem.streamVersion+1, w.meta.Reason)
	if err != nil {
		return Result{}, err
	}
	if err := w.markDirty(ctx, sp.ID); err != nil {
		return Result{}, err
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, mem.ID)
}

// dreamUndoable reports whether an action may still be undone at now.
func dreamUndoable(a *DreamAction, window time.Duration, now time.Time) bool {
	if a.Undone != nil || a.CreatedAt.Add(window).Before(now) {
		return false
	}
	if a.Memory != nil && a.Memory.Lifecycle == lifecycle.Forgotten {
		return false
	}
	return true
}
