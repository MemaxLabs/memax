package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Forgetting a whole space, an account, and the forget ledger (plan 25
// §5.13; the Phase 0 carry-over "account and space deletion").
//
// ForgetSpace forgets every memory in the space through the same purge as
// Forget (each with its forgot receipt and tombstone, all hanging from the
// space's own tombstone), withdraws the gates still waiting and purges
// every gate's words, takes every prose line out of the Brief (a new,
// empty B-), clears the drift evidence, redacts every receipt reason in
// the space (v2.redact_space_receipt_reasons, beside the space's forgot
// receipt), tells every agent that read it or is connected to it, and
// recompiles the targets, which then hold nothing. Retire goes further,
// for deleting the space: v2.retire_space deletes every V2 row of it that
// refers to its hub, so V1 can delete the hub. What remains is
// content-free: the receipts (reasons redacted), the seals, the tombstones
// and notices, and the space's ledger row, so its chain still verifies and
// the forget ledger can be re-applied after a restore.

// forgetSpace is ForgetSpace.
func (w *writer) forgetSpace(ctx context.Context, c *ForgetSpace) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
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
		if err != nil {
			return Result{}, err
		}
		res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, sp.ID, w.honesty())
		if errors.Is(err, ErrNotFound) {
			err = nil
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionForgetSpace, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	// Nothing left to forget (the space was emptied, and nothing was kept
	// since): no new receipts, no notices.
	if !c.Retire {
		words, err := w.spaceHoldsWords(ctx, sp.ID)
		if err != nil {
			return Result{}, err
		}
		if !words {
			res := Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}
			res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, sp.ID, w.honesty())
			if errors.Is(err, ErrNotFound) {
				err = nil
			}
			return res, err
		}
	}
	res, err := w.forgetWholeSpace(ctx, sp, c.Retire, nil)
	if err != nil {
		return Result{}, err
	}
	res.Policy = dec
	if err := w.record(ctx, res, uuid.Nil); err != nil {
		return Result{}, err
	}
	// A retired space's rows are gone; its tombstone stays.
	res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, sp.ID, w.honesty())
	return res, err
}

// forgetWholeSpace forgets everything in sp (and retires it). reapply is
// set when the forget ledger re-applies a space's Forget after a restore:
// the space's tombstone keeps its id.
func (w *writer) forgetWholeSpace(ctx context.Context, sp spaceRow, retire bool, reapply *ForgetLedgerOp) (Result, error) {
	// Every memory, locked, in id order.
	rows, err := w.tx.Query(ctx, `SELECT id FROM v2.memories WHERE space_id = $1 AND lifecycle <> 'forgotten' ORDER BY id`, sp.ID)
	if err != nil {
		return Result{}, fmt.Errorf("ledger: forget space: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return Result{}, fmt.Errorf("ledger: forget space: %w", err)
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR UPDATE")
	if err != nil {
		return Result{}, err
	}

	// The space's own forgot receipt, which lets the space's reasons be
	// redacted and the space retired.
	var stream int
	if err := w.tx.QueryRow(ctx, `SELECT COALESCE(max(stream_version), 0) FROM v2.receipts WHERE stream_id = $1`, sp.ID).Scan(&stream); err != nil {
		return Result{}, fmt.Errorf("ledger: forget space: %w", err)
	}
	src := w.objectReceipt(sp, ObjectSpace, sp.ID, SpaceObjectRef, ActionForgot, stream+1, "")
	if err := insertReceipt(ctx, w.tx, &src); err != nil {
		return Result{}, err
	}
	op := &forgetOp{id: newID(), kind: ObjectSpace, primary: SpaceObjectRef, retire: retire}
	if reapply != nil {
		op.id = reapply.OpID
	}
	op.receipts = append(op.receipts, src)
	// Each Forget of the whole space has its own tombstone (a space can be
	// emptied more than once); a re-applied one keeps the ledger's id.
	exists, err := w.tombstoneIDExists(ctx, op.id)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		if err := w.insertTombstone(ctx, sp, op, sp.ID, SpaceObjectRef, ObjectSpace, nil, src, nil, 0, TombstoneGone{}); err != nil {
			return Result{}, err
		}
	}

	// Gates still waiting are withdrawn first: a waiting gate must keep its
	// words (the question is open).
	if err := w.withdrawWaitingGates(ctx, sp); err != nil {
		return Result{}, err
	}
	files, err := w.allTargets(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	carried := Carried{Reason: CarrySpace}
	for _, id := range ids {
		m := locked[id]
		if m == nil || m.Lifecycle == lifecycle.Forgotten {
			continue
		}
		op.refs = append(op.refs, m.Ref)
		op.ids = append(op.ids, m.ID)
	}
	for _, id := range op.ids {
		if _, err := w.purgeMemory(ctx, sp, op, locked[id], &carried); err != nil {
			return Result{}, err
		}
	}
	// Gates answered by memories forgotten earlier still hold words if the
	// gate predates 043; purge any left.
	if err := w.purgeOrphanGates(ctx, sp, op); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `SELECT v2.redact_space_receipt_reasons($1)`, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: forget space: receipts: %w", err)
	}
	if err := w.afterPurge(ctx, sp, op, files); err != nil {
		return Result{}, err
	}
	if retire {
		if _, err := w.tx.Exec(ctx, `SELECT v2.retire_space($1)`, sp.ID); err != nil {
			return Result{}, fmt.Errorf("ledger: retire space: %w", err)
		}
	}
	return Result{Outcome: OutcomeApplied, Receipts: op.receipts}, nil
}

func (w *writer) tombstoneIDExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var yes bool
	if err := w.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.tombstones WHERE id = $1)`, id).Scan(&yes); err != nil {
		return false, fmt.Errorf("ledger: tombstone: %w", err)
	}
	return yes, nil
}

// spaceHoldsWords reports whether anything in the space still holds words
// a Forget of it would take: a memory not forgotten, a gate's question, a
// receipt's reason, the Brief's prose or title, drift evidence.
func (w *writer) spaceHoldsWords(ctx context.Context, spaceID uuid.UUID) (bool, error) {
	return spaceHoldsWords(ctx, w.tx, spaceID)
}

// SpaceHoldsWords reports whether a Forget of the space would take
// anything: V1's delete of a person's data asks before it forgets.
func (l *Ledger) SpaceHoldsWords(ctx context.Context, spaceID uuid.UUID) (bool, error) {
	if l == nil {
		return false, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var yes bool
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		yes, err = spaceHoldsWords(ctx, tx, spaceID)
		return err
	})
	return yes, err
}

func spaceHoldsWords(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (bool, error) {
	var yes bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.memories WHERE space_id = $1 AND lifecycle <> 'forgotten')
		    OR EXISTS (SELECT 1 FROM v2.decision_gates WHERE space_id = $1 AND (question IS NOT NULL OR options IS NOT NULL))
		    OR EXISTS (SELECT 1 FROM v2.receipts WHERE space_id = $1 AND reason IS NOT NULL)
		    OR EXISTS (SELECT 1 FROM v2.brief_versions WHERE space_id = $1 AND (title <> 'Brief' OR summary IS NOT NULL))
		    OR EXISTS (SELECT 1 FROM v2.brief_versions v, jsonb_array_elements(v.structure -> 'sections') s,
		                             jsonb_array_elements(s -> 'items') i
		                WHERE v.space_id = $1 AND i ? 'text')
		    OR EXISTS (SELECT 1 FROM v2.target_observations WHERE space_id = $1 AND jsonb_array_length(changeset -> 'changes') > 0)
		    OR EXISTS (SELECT 1 FROM v2.import_conflicts WHERE space_id = $1
		                  AND (subject IS NOT NULL OR rationale IS NOT NULL OR suggestion IS NOT NULL))`,
		spaceID).Scan(&yes)
	if err != nil {
		return false, fmt.Errorf("ledger: forget space: %w", err)
	}
	return yes, nil
}

func (w *writer) tombstoneExists(ctx context.Context, objectID uuid.UUID) (bool, error) {
	var yes bool
	if err := w.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.tombstones WHERE object_id = $1)`, objectID).Scan(&yes); err != nil {
		return false, fmt.Errorf("ledger: tombstone: %w", err)
	}
	return yes, nil
}

// allTargets lists every target of the space.
func (w *writer) allTargets(ctx context.Context, spaceID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := w.tx.Query(ctx, `SELECT id FROM v2.targets WHERE space_id = $1 ORDER BY created_at, id`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: targets: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// withdrawWaitingGates withdraws every gate still waiting in the space,
// by the person forgetting it.
func (w *writer) withdrawWaitingGates(ctx context.Context, sp spaceRow) error {
	rows, err := w.tx.Query(ctx, `
		SELECT id, seq, stream_version FROM v2.decision_gates
		 WHERE space_id = $1 AND status = 'waiting' ORDER BY id FOR UPDATE`, sp.ID)
	if err != nil {
		return fmt.Errorf("ledger: forget space: gates: %w", err)
	}
	type gateRow struct {
		id      uuid.UUID
		seq     int64
		version int
	}
	gates, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (gateRow, error) {
		var g gateRow
		err := r.Scan(&g.id, &g.seq, &g.version)
		return g, err
	})
	if err != nil {
		return fmt.Errorf("ledger: forget space: gates: %w", err)
	}
	if len(gates) == 0 {
		return nil
	}
	// A gate is withdrawn by a person: the one forgetting the space, or,
	// when Memax re-applies the forget ledger, the space's owner, who
	// forgot it.
	byID := w.meta.Actor.ID
	if w.meta.Actor.Kind != policy.ActorPerson {
		if err := w.tx.QueryRow(ctx, `SELECT owner_id FROM v2.spaces WHERE id = $1`, sp.ID).Scan(&byID); err != nil {
			return fmt.Errorf("ledger: forget space: owner: %w", err)
		}
	}
	kind := string(policy.ActorPerson)
	for _, g := range gates {
		rc := w.objectReceipt(sp, ObjectGate, g.id, FormatRef(PrefixDecision, g.seq), ActionWithdrawn, g.version+1, "")
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return err
		}
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.decision_gates
			   SET status = 'withdrawn', withdrawn_by_kind = $2, withdrawn_by = $3, withdrawn_at = now(),
			       stream_version = $4, last_receipt_id = $5, updated_at = now()
			 WHERE id = $1 AND space_id = $6`, g.id, kind, byID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget space: withdraw gate: %w", err)
		}
	}
	return nil
}

// purgeOrphanGates purges the words of any ended gate left with them
// (its decision was never kept, or forgotten before the gate was purged).
func (w *writer) purgeOrphanGates(ctx context.Context, sp spaceRow, op *forgetOp) error {
	rows, err := w.tx.Query(ctx, `
		SELECT id, seq, stream_version FROM v2.decision_gates
		 WHERE space_id = $1 AND status <> 'waiting'
		   AND (question IS NOT NULL OR context IS NOT NULL OR options IS NOT NULL)
		 ORDER BY id FOR UPDATE`, sp.ID)
	if err != nil {
		return fmt.Errorf("ledger: forget space: gates: %w", err)
	}
	type gateRow struct {
		id      uuid.UUID
		seq     int64
		version int
	}
	gates, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (gateRow, error) {
		var g gateRow
		err := r.Scan(&g.id, &g.seq, &g.version)
		return g, err
	})
	if err != nil {
		return fmt.Errorf("ledger: forget space: gates: %w", err)
	}
	for _, g := range gates {
		rc := w.objectReceipt(sp, ObjectGate, g.id, FormatRef(PrefixDecision, g.seq), ActionForgot, g.version+1, "")
		rc.Source = &ReceiptSource{Kind: ObjectSpace, Ref: SpaceObjectRef}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return err
		}
		op.receipts = append(op.receipts, rc)
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.decision_gates SET question = NULL, context = NULL, options = NULL,
			       stream_version = $2, last_receipt_id = $3, updated_at = now()
			 WHERE id = $1 AND space_id = $4`, g.id, rc.StreamVersion, rc.ID, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget space: gate words: %w", err)
		}
	}
	return nil
}

// AccountForget is what ForgetAccount did.
type AccountForget struct {
	// Spaces are the spaces whose record was forgotten.
	Spaces []uuid.UUID
	// Disconnected are the agent connections disconnected (credentials
	// revoked).
	Disconnected []uuid.UUID
	// Kept are the team spaces left with their members.
	Kept []uuid.UUID
}

// ForgetAccount is deleting a person's account: ForgetAccountRecord, then
// every agent connection of theirs is disconnected and its credential
// revoked, so nothing can write to their record again.
func (l *Ledger) ForgetAccount(ctx context.Context, person uuid.UUID, via policy.Via) (AccountForget, error) {
	out, err := l.ForgetAccountRecord(ctx, person, via)
	if err != nil {
		return out, err
	}
	scope, err := l.UserScope(ctx, person)
	if err != nil {
		return out, err
	}
	actor := Actor{Kind: policy.ActorPerson, ID: person, Credential: policy.CredentialSession}
	conns, err := l.ListConnections(ctx, scope, ConnectionQuery{})
	if err != nil {
		return out, err
	}
	for _, c := range conns {
		if c.State == ConnectionDisconnected || c.PersonID != person {
			continue
		}
		res, err := l.Apply(ctx, &DisconnectAgent{
			Meta:       Meta{Actor: actor, Scope: scope, Via: via, IdempotencyKey: "forget-account:" + uuid.NewString()},
			Connection: c.ID,
		})
		if err != nil {
			return out, fmt.Errorf("ledger: forget account: disconnect %s: %w", c.ID, err)
		}
		if res.Outcome != OutcomeRefused {
			out.Disconnected = append(out.Disconnected, c.ID)
		}
	}
	return out, nil
}

// ForgetAccountRecord forgets a person's record (V1's DELETE
// /v1/account/data, its "Forget all", which keeps the account and its
// agents): everything in their personal space and the project spaces they
// own, each through ForgetSpace (the spaces stay, empty). Team spaces stay
// with their members: their record is the team's. Each space is its own
// transaction, so a failure leaves the spaces done so far forgotten and
// the call can be repeated.
func (l *Ledger) ForgetAccountRecord(ctx context.Context, person uuid.UUID, via policy.Via) (AccountForget, error) {
	var out AccountForget
	if l == nil {
		return out, ErrDisabled
	}
	scope, err := l.UserScope(ctx, person)
	if err != nil {
		return out, err
	}
	actor := Actor{Kind: policy.ActorPerson, ID: person, Credential: policy.CredentialSession}
	for _, g := range scope.Spaces {
		if g.Role != policy.RoleOwner {
			continue
		}
		if g.Kind == policy.SpaceTeam {
			out.Kept = append(out.Kept, g.SpaceID)
			continue
		}
		held, err := l.SpaceHoldsWords(ctx, g.SpaceID)
		if err != nil {
			return out, err
		}
		if !held {
			continue
		}
		res, err := l.Apply(ctx, &ForgetSpace{
			Meta:    Meta{Actor: actor, Scope: scope.Narrow(g.SpaceID), Via: via, IdempotencyKey: "forget-account:" + uuid.NewString()},
			SpaceID: g.SpaceID,
		})
		if err != nil {
			return out, fmt.Errorf("ledger: forget account: space %s: %w", g.SpaceID, err)
		}
		if res.Outcome == OutcomeRefused {
			return out, &RefusedError{Decision: res.Policy}
		}
		out.Spaces = append(out.Spaces, g.SpaceID)
	}
	return out, nil
}

// HasAccountRecord reports whether ForgetAccountRecord has anything to
// forget: a personal or project space the person owns holds words.
func (l *Ledger) HasAccountRecord(ctx context.Context, person uuid.UUID) (bool, error) {
	if l == nil {
		return false, ErrDisabled
	}
	scope, err := l.UserScope(ctx, person)
	if err != nil {
		return false, err
	}
	for _, g := range scope.Spaces {
		if g.Role != policy.RoleOwner || g.Kind == policy.SpaceTeam {
			continue
		}
		held, err := l.SpaceHoldsWords(ctx, g.SpaceID)
		if err != nil || held {
			return held, err
		}
	}
	return false, nil
}

// SpaceHasRecord reports whether a space holds V2 records (any receipt):
// then V1's deletes must go through ForgetSpace first.
func (l *Ledger) SpaceHasRecord(ctx context.Context, spaceID uuid.UUID) (bool, error) {
	if l == nil {
		return false, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var yes bool
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.receipts WHERE space_id = $1)`, spaceID).Scan(&yes)
	})
	return yes, err
}

// ForgetSpaceForV1 is V1's delete of a hub (retire) or of a person's data
// (not), through the ledger: the person must own the space. It answers
// whether anything was forgotten (false: the space holds no V2 record,
// and V1 deletes as it always did).
func (l *Ledger) ForgetSpaceForV1(ctx context.Context, person, spaceID uuid.UUID, retire bool) (bool, error) {
	held, err := l.SpaceHasRecord(ctx, spaceID)
	if err != nil || !held {
		return false, err
	}
	scope, err := l.UserScope(ctx, person)
	if err != nil {
		return false, err
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return false, ErrNotFound
	}
	res, err := l.Apply(ctx, &ForgetSpace{
		Meta: Meta{Actor: Actor{Kind: policy.ActorPerson, ID: person, Credential: policy.CredentialSession},
			Scope: scope.Narrow(spaceID), Via: policy.ViaAPI,
			IdempotencyKey: "v1-delete:" + uuid.NewString()},
		SpaceID: spaceID, Retire: retire,
	})
	if err != nil {
		return false, err
	}
	if res.Outcome == OutcomeRefused {
		return false, &RefusedError{Decision: res.Policy}
	}
	return true, nil
}

// RefusedError is a policy refusal returned as an error, for callers
// outside Apply (V1's handlers).
type RefusedError struct{ Decision policy.Decision }

func (e *RefusedError) Error() string { return e.Decision.Message }

// validate checks a forget-ledger op.
func (op *ForgetLedgerOp) validate() error {
	switch {
	case op.OpID == uuid.Nil || op.SpaceID == uuid.Nil:
		return invalid("op", "a forget-ledger op names its op and space")
	case op.Kind != ObjectMemory && op.Kind != ObjectSpace:
		return invalid("op.kind", "memory or space")
	case len(op.Entries) == 0:
		return invalid("op.entries", "an op forgot at least one object")
	}
	return nil
}

// reapplyForget is ReapplyForget: the forget ledger's op, re-run after a
// restore. Anything it forgot that is back (a memory with words, a
// lifecycle that isn't forgotten, a tombstone missing) is forgotten again,
// as Memax, with new receipts; the tombstones keep the ids and times the
// ledger recorded. An op whose objects are all still forgotten writes
// nothing.
func (w *writer) reapplyForget(ctx context.Context, c *ReapplyForget) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.Op.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.Op.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claimKey(ctx, sp.ID); err != nil || replay != nil {
		if err != nil {
			return Result{}, err
		}
		return replay.result(ctx, w)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionReapplyForget, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if c.Op.Kind == ObjectSpace {
		var left int
		if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle <> 'forgotten'`, sp.ID).Scan(&left); err != nil {
			return Result{}, err
		}
		if left == 0 && !c.Op.Retired {
			return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
		}
		res, err := w.forgetWholeSpace(ctx, sp, c.Op.Retired, &c.Op)
		if err != nil {
			return Result{}, err
		}
		res.Policy = dec
		if err := w.markReapplied(ctx, c.Op); err != nil {
			return Result{}, err
		}
		return res, w.record(ctx, res, uuid.Nil)
	}

	// A memory op: the primary first, so the carried tombstones' op exists.
	op := &forgetOp{id: c.Op.OpID, kind: ObjectMemory}
	type back struct {
		m       *Memory
		entry   ForgetLedgerEntry
		carried *Carried
	}
	var todo []back
	for _, e := range c.Op.Entries {
		m, err := loadMemory(ctx, w.tx, w.meta.Scope, e.ObjectID, true)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Result{}, err
		}
		if e.Carried == "" {
			op.primary = m.Ref
		}
		purged, err := w.alreadyPurged(ctx, m)
		if err != nil {
			return Result{}, err
		}
		if purged {
			continue
		}
		b := back{m: m, entry: e}
		if e.Carried != "" {
			b.carried = &Carried{ID: m.ID, Ref: m.Ref, Reason: e.Carried, With: op.primary}
		}
		todo = append(todo, b)
		op.refs = append(op.refs, m.Ref)
		op.ids = append(op.ids, m.ID)
	}
	if len(todo) == 0 {
		return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
	}
	files, err := w.filesHolding(ctx, sp.ID, op.refs)
	if err != nil {
		return Result{}, err
	}
	for _, b := range todo {
		if err := w.repurge(ctx, sp, op, b.m, b.entry, b.carried); err != nil {
			return Result{}, err
		}
	}
	if err := w.afterPurge(ctx, sp, op, files); err != nil {
		return Result{}, err
	}
	if err := w.markReapplied(ctx, c.Op); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: op.receipts}
	return res, w.record(ctx, res, uuid.Nil)
}

// alreadyPurged reports whether a memory is forgotten, without words and
// with its tombstone.
func (w *writer) alreadyPurged(ctx context.Context, m *Memory) (bool, error) {
	if m.Lifecycle != lifecycle.Forgotten {
		return false, nil
	}
	var words, tomb bool
	err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL),
		       EXISTS (SELECT 1 FROM v2.tombstones WHERE object_id = $1)`, m.ID).Scan(&words, &tomb)
	if err != nil {
		return false, fmt.Errorf("ledger: reapply: %w", err)
	}
	return !words && tomb, nil
}

// repurge forgets a memory again from its forget-ledger entry: the purge,
// with the tombstone the ledger recorded (its id), if it is missing.
func (w *writer) repurge(ctx context.Context, sp spaceRow, op *forgetOp, m *Memory, e ForgetLedgerEntry, carried *Carried) error {
	exists, err := w.tombstoneExists(ctx, m.ID)
	if err != nil {
		return err
	}
	if exists {
		// The tombstone survived and the words came back: purge them
		// without a second tombstone.
		return w.purgeWordsOnly(ctx, sp, op, m)
	}
	// purgeMemory writes the tombstone with op.id (the ledger's op) for the
	// primary, and a new id for a carried one.
	if _, err := w.purgeMemory(ctx, sp, op, m, carried); err != nil {
		return err
	}
	if carried != nil {
		if _, err := w.tx.Exec(ctx, `UPDATE v2.tombstones SET reapplied_at = now() WHERE object_id = $1`, m.ID); err != nil {
			return fmt.Errorf("ledger: reapply: %w", err)
		}
	}
	return nil
}

// purgeWordsOnly is purgeMemory without a tombstone, for a memory whose
// tombstone survived a restore that brought its words back.
func (w *writer) purgeWordsOnly(ctx context.Context, sp spaceRow, op *forgetOp, m *Memory) error {
	// The memory's row may be forgotten already; the purge statements are
	// the same, and the forgot receipt lets them through.
	rc := w.receipt(sp, m.ID, m.Ref, ActionForgot, m.streamVersion+1, "")
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return err
	}
	op.receipts = append(op.receipts, rc)
	steps := []string{
		`UPDATE v2.memory_versions SET statement = NULL, last_receipt_id = $2 WHERE memory_id = $1 AND statement IS NOT NULL`,
		`UPDATE v2.sources s SET quote = NULL, uri = NULL, content_hash = NULL, ref = s.kind, locator = '{}'::jsonb, last_receipt_id = $2
		   FROM v2.memory_sources ms WHERE ms.source_id = s.id AND ms.memory_id = $1
		    AND (s.quote IS NOT NULL OR s.uri IS NOT NULL OR s.content_hash IS NOT NULL)`,
		`UPDATE v2.import_conflicts SET subject = NULL, rationale = NULL, suggestion = NULL, last_receipt_id = $2
		  WHERE $1 = ANY (members) AND (subject IS NOT NULL OR rationale IS NOT NULL OR suggestion IS NOT NULL)`,
	}
	for _, q := range steps {
		if _, err := w.tx.Exec(ctx, q, m.ID, rc.ID); err != nil {
			return fmt.Errorf("ledger: reapply %s: %w", m.Ref, err)
		}
	}
	if m.Lifecycle != lifecycle.Forgotten {
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.memories
			   SET lifecycle = 'forgotten', flags = '{}', search = NULL, content_sha256 = NULL, minhash_bands = NULL,
			       embedding = NULL, decision = NULL, conditions = '[]'::jsonb, scope = '{}'::jsonb,
			       stream_version = $2, last_receipt_id = $3, updated_at = now()
			 WHERE id = $1`, m.ID, rc.StreamVersion, rc.ID); err != nil {
			return fmt.Errorf("ledger: reapply %s: %w", m.Ref, err)
		}
	} else if _, err := w.tx.Exec(ctx, `UPDATE v2.memories SET stream_version = $2, last_receipt_id = $3 WHERE id = $1`,
		m.ID, rc.StreamVersion, rc.ID); err != nil {
		return fmt.Errorf("ledger: reapply %s: %w", m.Ref, err)
	}
	_, err := w.tx.Exec(ctx, `SELECT v2.redact_receipt_reasons($1)`, m.ID)
	return err
}

// markReapplied stamps the op's tombstones.
func (w *writer) markReapplied(ctx context.Context, op ForgetLedgerOp) error {
	if _, err := w.tx.Exec(ctx, `UPDATE v2.tombstones SET reapplied_at = now(), updated_at = now() WHERE op_id = $1`, op.OpID); err != nil {
		return fmt.Errorf("ledger: reapply: %w", err)
	}
	return nil
}

// ForgottenSpaces lists every space with a tombstone (v2.forgotten_spaces),
// for re-applying the forget ledger. It reads as the login role.
func (l *Ledger) ForgottenSpaces(ctx context.Context) ([]uuid.UUID, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	rows, err := l.pool.Query(ctx, `SELECT space_id FROM v2.forgotten_spaces() ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("ledger: forgotten spaces: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ForgetLedgerOps reads a space's ops from its tombstones (the database's
// copy of the forget ledger), oldest first.
func (l *Ledger) ForgetLedgerOps(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]ForgetLedgerOp, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out []ForgetLedgerOp
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		out, err = forgetLedgerOps(ctx, tx, spaceID, nil)
		return err
	})
	return out, err
}

func forgetLedgerOps(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, only *uuid.UUID) ([]ForgetLedgerOp, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.op_id, t.id, t.tenant_id, t.object_kind, t.object_id, t.object_ref, COALESCE(t.carried, ''), t.forgotten_at,
		       p.object_kind, (SELECT l.retired_at IS NOT NULL FROM v2.space_ledgers l WHERE l.space_id = t.space_id)
		  FROM v2.tombstones t JOIN v2.tombstones p ON p.id = t.op_id
		 WHERE t.space_id = $1 AND ($2::uuid IS NULL OR t.op_id = $2)
		 ORDER BY p.forgotten_at, t.op_id, (t.id = t.op_id) DESC, t.forgotten_at, t.id`, spaceID, only)
	if err != nil {
		return nil, fmt.Errorf("ledger: forget ledger: %w", err)
	}
	defer rows.Close()
	var out []ForgetLedgerOp
	for rows.Next() {
		var opID, tenant uuid.UUID
		var e ForgetLedgerEntry
		var at time.Time
		var opKind string
		var retired *bool
		if err := rows.Scan(&opID, &e.TombstoneID, &tenant, &e.ObjectKind, &e.ObjectID, &e.Ref, &e.Carried, &at, &opKind, &retired); err != nil {
			return nil, fmt.Errorf("ledger: forget ledger: %w", err)
		}
		if n := len(out); n == 0 || out[n-1].OpID != opID {
			out = append(out, ForgetLedgerOp{Version: 1, OpID: opID, SpaceID: spaceID, TenantID: tenant, Kind: opKind,
				Retired: opKind == ObjectSpace && retired != nil && *retired, ForgottenAt: at.UTC()})
		}
		out[len(out)-1].Entries = append(out[len(out)-1].Entries, e)
	}
	return out, rows.Err()
}
