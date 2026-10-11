package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Verify (plan 25 §5.9, epic 3.1; the stale-detection design §3.3).
//
// A stale flag says a kept memory may no longer be true: its date to check
// again passed (Dream), or one of its conditions stopped holding (the
// checker, later). Verify is a person's answer, one command:
//
//   - still true: a `verified` receipt, whose source names what flagged it
//     (a date, a check, a pull request), and the flag clears. A date that
//     has passed moves to the new date the person gives, or goes: memories
//     don't expire, and the compiler marks anything past its date as being
//     verified, so a date left in the past would keep the mark.
//   - update: an `edited` receipt with the new words (a new version), then
//     `verified`, and the flag clears the same way.
//
// Both are Keep's decisions (policy.ActionVerify): a person who may keep,
// on the web with their passkey where the space's decisions need it. Both
// are undoable, by the same person within the undo window, as one command.
// "Forget it" is Forget itself: the API applies Forget for that answer, so
// every one of its rules holds.

// CommandVerify names Verify.
const CommandVerify CommandName = "verify"

// ActionVerified is the receipt a person's Verify writes (in 028's list).
const ActionVerified Action = "verified"

// UndoVerify undoes a Verify: the flag, the words and the date come back.
const UndoVerify UndoKind = "verify"

// VerifyAnswer is what the person says about a stale memory.
type VerifyAnswer string

// The answers the ledger applies. Forget is its own command.
const (
	VerifyStillTrue VerifyAnswer = "still_true"
	VerifyUpdate    VerifyAnswer = "update"
	// VerifyForget is the API's third answer, which applies Forget.
	VerifyForget VerifyAnswer = "forget"
)

// The receipt sources a stale flag, and the Verify that answers it, name.
// They are pointers, never words: a date, a check run, a commit or a pull
// request, so they survive Forget without naming a path.
const (
	StaleSourceDate   = "date"
	StaleSourceCheck  = "check"
	StaleSourceCommit = "commit"
	StaleSourcePR     = "pr"
)

// Verify answers a stale flag on a kept memory.
type Verify struct {
	Meta
	// Memory is a display ID ("M-0187") or a uuid.
	Memory string
	// ExpectedVersion is required (If-Match): the words the person checked.
	ExpectedVersion int
	Answer          VerifyAnswer
	// Statement is the new words, for VerifyUpdate.
	Statement string
	// StaleAfter, when set, is when to check it again (a date answer). It
	// must be in the future. Without it, a date that has passed goes.
	StaleAfter *time.Time
}

// Name implements Command.
func (*Verify) Name() CommandName { return CommandVerify }

func (c *Verify) validate(now time.Time) error {
	if err := validateTarget(c.Memory, c.ExpectedVersion, true); err != nil {
		return err
	}
	switch c.Answer {
	case VerifyStillTrue:
		if c.Statement != "" {
			return invalid("statement", "only an update carries new words; send answer update")
		}
	case VerifyUpdate:
		if err := checkText("statement", c.Statement, MaxStatementRunes, true); err != nil {
			return err
		}
	case VerifyForget:
		return invalid("answer", "forget is Forget: send it to :forget, or to :verify, which applies it")
	default:
		return invalid("answer", "use still_true or update")
	}
	if c.StaleAfter != nil {
		if !c.StaleAfter.After(now) {
			return invalid("stale_after", "must be in the future: it is when to check it again")
		}
		t := c.StaleAfter.UTC().Truncate(time.Microsecond)
		c.StaleAfter = &t
	}
	return nil
}

// verify applies Verify.
func (w *writer) verify(ctx context.Context, c *Verify) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	if c.ExpectedVersion != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: c.ExpectedVersion, Current: mem.Version}
	}
	update := c.Answer == VerifyUpdate
	if update && c.Statement == mem.Statement {
		return Result{}, invalid("statement", "is the words it has; answer still_true to keep them")
	}
	obj := w.object(mem, false)
	if update {
		obj.Secrets = findSecrets(c.Statement, w.meta.Reason)
	} else {
		obj.Secrets = findSecrets(w.meta.Reason)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionVerify, obj, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	// Only a kept, stale memory has a flag to answer.
	next, err := transition(mem, lifecycle.VerbClearStale)
	if err != nil {
		return Result{}, err
	}
	flag, err := staleFlagOf(ctx, w.tx, sp.ID, mem.ID)
	if err != nil {
		return Result{}, err
	}

	w.startUndo(UndoVerify, w.undoWindow)
	w.undo.touchWithDate(mem)
	var receipts []Receipt
	if update {
		rc, err := w.writeVersion(ctx, sp, mem, c.Statement, "")
		if err != nil {
			return Result{}, err
		}
		receipts = append(receipts, rc)
	}
	staleAfter := nextStaleAfter(mem.StaleAfter, c.StaleAfter, w.now)
	rc := w.receipt(sp, mem.ID, mem.Ref, ActionVerified, mem.streamVersion+1, w.meta.Reason)
	rc.Source = verifySource(flag, mem.StaleAfter)
	rc.Assurance = w.policyActor(grant).Assurance()
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if err := execDeferred(ctx, w.tx, `
		UPDATE v2.memories
		   SET flags = $2, stale_after = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $6`,
		mem.ID, next.Flags.Strings(), staleAfter, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: verify %s: %w", mem.Ref, err)
	}
	receipts = append(receipts, rc)
	// The compiled files said "(Being verified)": every target recompiles.
	if err := w.markDirtyAndJournal(ctx, sp, receipts); err != nil {
		return Result{}, err
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, mem.ID)
}

// nextStaleAfter is a memory's date to check again after a person checked
// it: the date they gave, else the date it had while that is still ahead,
// else none (memories don't expire, and a date in the past compiles as
// being verified).
func nextStaleAfter(had, given *time.Time, now time.Time) *time.Time {
	switch {
	case given != nil:
		return given
	case had != nil && had.After(now):
		return had
	}
	return nil
}

// verifySource is what a `verified` receipt cites: the source of the flag
// it answers (a check run, a commit, a pull request or a date), or, for
// Dream's flags, which cite their edition, the date that passed.
func verifySource(flag *Receipt, staleAfter *time.Time) *ReceiptSource {
	if flag != nil && flag.Source != nil {
		switch flag.Source.Kind {
		case StaleSourceCheck, StaleSourceCommit, StaleSourcePR, StaleSourceDate:
			s := *flag.Source
			return &s
		}
	}
	if staleAfter != nil {
		return &ReceiptSource{Kind: StaleSourceDate, Ref: staleAfter.UTC().Format(time.DateOnly)}
	}
	if flag != nil && flag.Source != nil {
		s := *flag.Source
		return &s
	}
	return nil
}

// staleFlagCond picks, among a memory's `flagged` receipts (the alias r),
// the ones that set the stale flag: Dream's date flags (its stale actions)
// and the checker's, which cite a check, a commit, a pull request or a
// date. Conflict flags cite the memory they contradict, or are Dream's
// conflict actions.
const staleFlagCond = `r.action = 'flagged'
	   AND (r.source->>'kind' IN ('check', 'commit', 'pr', 'date')
	        OR EXISTS (SELECT 1 FROM v2.dream_actions d
	                    WHERE d.space_id = r.space_id AND d.kind = 'stale' AND r.id = ANY (d.receipt_ids)))`

// staleFlagSelect is receiptSelect over the alias r.
const staleFlagSelect = `
	SELECT r.id, r.seq, r.tenant_id, r.space_id, r.object_kind, r.object_id, r.object_ref, r.action, r.actor_kind, r.actor_id,
	       COALESCE(r.agent, ''), r.via, COALESCE(r.assurance, ''), COALESCE(r.session_ref, ''), r.source,
	       COALESCE(r.reason, ''), r.occurred_at, r.recorded_at, r.stream_id, r.stream_version
	  FROM v2.receipts r`

// staleFlagOf is the receipt that set a memory's stale flag, the latest
// one; nil when none did (the flag was put back by an undo of a receipt
// since pruned, say).
func staleFlagOf(ctx context.Context, tx pgx.Tx, spaceID, memoryID uuid.UUID) (*Receipt, error) {
	rows, err := tx.Query(ctx, staleFlagSelect+`
		 WHERE r.stream_id = $1 AND r.space_id = $2 AND `+staleFlagCond+`
		 ORDER BY r.stream_version DESC LIMIT 1`, memoryID, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: read the stale flag: %w", err)
	}
	rcs, err := pgx.CollectRows(rows, scanReceipt)
	if err != nil {
		return nil, fmt.Errorf("ledger: read the stale flag: %w", err)
	}
	if len(rcs) == 0 {
		return nil, nil
	}
	return &rcs[0], nil
}
