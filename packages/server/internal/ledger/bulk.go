package ledger

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Bulk Keep and Reject (POST /v2/spaces/{space}/memories:keep and :reject).
//
// Each item is its own Keep (or Reject), as Apply applies it: its own
// receipt, idempotency key, policy decision and undo entry, in review's
// order of checks. Applied one at a time, a Keep costs about ten dependent
// round trips, a quarter of a second each on production's 24 ms network;
// ApplyBulk applies them all in one transaction, in round trips that don't
// grow with their number:
//
//  1. One round trip reads what every decision needs: the space, the
//     memories (locked, in id order), each ref's resolution, the items'
//     earlier applications and their receipts, the memories' links, which
//     proposals the judge hasn't seen yet, and the space's decisions in
//     force.
//  2. Only when a Keep would supersede a decision: one more locks those
//     decisions and reads the decisions in force again.
//  3. Each item is decided in request order on the memories as the items
//     before it left them, so an item sees what they did: a decision one
//     kept is in force for the next, one it superseded no longer is.
//  4. One round trip writes it all: the receipts, the memories, the undo
//     entries, the idempotency records and (once, for every Keep) the
//     targets' generation, and reads the memories back.
//  5. River's insert of the compile jobs, then COMMIT, as any command's.
//
// # Locks
//
// The transaction never waits for a memory: it locks with SKIP LOCKED, in
// id order. A memory another transaction holds (a single Keep, a Forget,
// the judge), and a Keep that would supersede a decision another one
// holds, is left out and applied after the commit, through Apply, in
// request order: it waits for its memory as it always did. A transaction
// that waits for no memory can't close a cycle of waits with one that
// does. Its one waiting lock is the targets' UPDATE, taken last, as a
// single Keep takes it.
//
// # Idempotency
//
// Each item keeps the key the one-by-one path gave it. Its record is
// written with its outcome, in the same statement, only when it applied:
// a refusal or a failure is decided afresh on a retry, as Apply's rollback
// leaves it. Nobody else can be claiming an item's key meanwhile: every
// path that keeps or rejects locks the memory before it claims the key.
//
// If the transaction fails (a lock timeout on the targets, say), every
// item is applied one by one through Apply instead, so a failure is one
// item's, as before.

// BulkResult is one command's answer from ApplyBulk: what Apply would have
// returned for it.
type BulkResult struct {
	Result Result
	Err    error
}

// bulkItem is one command of a bulk Keep or Reject.
type bulkItem struct {
	pos      int
	cmd      Command
	key      string
	ref      string
	expected int
	hash     []byte
	// id or seq is the ref, parsed.
	id  uuid.UUID
	seq int64
}

// ApplyBulk applies Keeps, or Rejects, of memories in one space: one
// result per command, in order, each what Apply would have answered. The
// commands share their envelope but for the idempotency key, as a bulk
// request's items do; commands that don't (or of any other kind) are
// applied one by one through Apply.
//
// A result's Memory is the memory's row as the bulk left it: without the
// sources, links and verdict GetMemory reads.
func (l *Ledger) ApplyBulk(ctx context.Context, cmds []Command) []BulkResult {
	out := make([]BulkResult, len(cmds))
	if l == nil {
		for i := range out {
			out[i].Err = ErrDisabled
		}
		return out
	}
	verb, meta, items, ok := l.bulkItems(cmds, out)
	if !ok {
		all := make([]*bulkItem, len(cmds))
		for i, c := range cmds {
			all[i] = &bulkItem{pos: i, cmd: c}
		}
		return l.applyEach(ctx, out, all)
	}
	if len(items) == 0 {
		return out
	}
	contended, err := l.bulkReview(ctx, verb, meta, items, out)
	if err != nil {
		if ctx.Err() != nil {
			for _, it := range items {
				out[it.pos] = BulkResult{Err: mapDBError(err)}
			}
			return out
		}
		l.log.Error("ledger: bulk review failed; applying its items one by one",
			"command", string(verb), "items", len(items), "error", err)
		contended = items
	}
	return l.applyEach(ctx, out, contended)
}

// applyEach applies the commands of items through Apply, in order.
func (l *Ledger) applyEach(ctx context.Context, out []BulkResult, items []*bulkItem) []BulkResult {
	for _, it := range items {
		res, err := l.Apply(ctx, it.cmd)
		out[it.pos] = BulkResult{Result: res, Err: err}
	}
	return out
}

// bulkItems validates the commands as Apply would, filling the errors it
// would return, and reports whether they form one bulk: Keeps or Rejects
// of one space, sharing their envelope but for the key.
func (l *Ledger) bulkItems(cmds []Command, out []BulkResult) (CommandName, Meta, []*bulkItem, bool) {
	var verb CommandName
	var shared *Meta
	now := l.now()
	var items []*bulkItem
	for i, cmd := range cmds {
		var ref string
		var expected int
		switch c := cmd.(type) {
		case *Keep:
			ref, expected = c.Memory, c.ExpectedVersion
		case *Reject:
			ref, expected = c.Memory, c.ExpectedVersion
		default:
			return "", Meta{}, nil, false
		}
		if verb != "" && cmd.Name() != verb {
			return "", Meta{}, nil, false
		}
		verb = cmd.Name()
		m := cmd.envelope()
		if err := validateMeta(m, now); err != nil {
			out[i].Err = err
			continue
		}
		if shared == nil {
			if len(m.Scope.Spaces) != 1 {
				return "", Meta{}, nil, false
			}
			shared = m
		} else if !sameEnvelope(shared, m) {
			return "", Meta{}, nil, false
		}
		if err := validateCommand(cmd, now); err != nil {
			out[i].Err = err
			continue
		}
		hash, err := requestHash(cmd)
		if err != nil {
			out[i].Err = err
			continue
		}
		it := &bulkItem{pos: i, cmd: cmd, key: m.IdempotencyKey, ref: strings.TrimSpace(ref), expected: expected, hash: hash}
		if id, err := uuid.Parse(it.ref); err == nil {
			it.id = id
		} else if it.seq, err = memorySeq(it.ref); err != nil {
			out[i].Err = err
			continue
		}
		items = append(items, it)
	}
	if shared == nil {
		return verb, Meta{}, nil, true
	}
	meta := *shared
	meta.IdempotencyKey = ""
	return verb, meta, items, true
}

// sameEnvelope reports whether two commands' envelopes differ only in
// their idempotency keys.
func sameEnvelope(a, b *Meta) bool {
	return a.Actor == b.Actor && a.Via == b.Via && a.SessionRef == b.SessionRef && a.OccurredAt.Equal(b.OccurredAt) &&
		a.Reason == b.Reason && a.Scope.PersonID == b.Scope.PersonID && slices.Equal(a.Scope.Spaces, b.Scope.Spaces)
}

// The reads of a bulk review's first round trip. $1 and $2 are the refs
// (ids and display numbers) and $3 the scope's spaces.
const (
	bulkWant = `m.space_id = ANY ($3) AND (m.id = ANY ($1) OR m.seq = ANY ($2))`
	// bulkLockSQL locks what it can of the memories, in id order, waiting
	// for none.
	bulkLockSQL = memorySelect + ` WHERE ` + bulkWant + ` ORDER BY m.id FOR UPDATE OF m SKIP LOCKED`
	// bulkResolveSQL resolves the refs, locked or not.
	bulkResolveSQL = `SELECT m.id, m.seq FROM v2.memories m WHERE ` + bulkWant
	// bulkLinksSQL reads the memories' active links out (linksSQL's order).
	bulkLinksSQL = `
		SELECT l.id, l.kind, l.from_memory_id, l.to_memory_id, t.seq
		  FROM v2.memory_links l
		  JOIN v2.memories m ON m.id = l.from_memory_id
		  JOIN v2.memories t ON t.id = l.to_memory_id
		 WHERE l.ended_receipt_id IS NULL AND l.to_memory_id IS NOT NULL
		   AND l.kind IN ('supersedes', 'conflicts_with') AND ` + bulkWant + `
		 ORDER BY l.created_at, l.id`
	// bulkPendingSQL lists the proposals whose current words the judge
	// hasn't seen yet, within JudgeGrace (verdictPending).
	bulkPendingSQL = `
		SELECT m.id FROM v2.memories m
		  JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
		 WHERE ` + bulkWant + ` AND m.lifecycle = 'proposed'
		   AND v.created_at > now() - make_interval(secs => $4)
		   AND NOT EXISTS (SELECT 1 FROM v2.judge_verdicts j WHERE j.memory_id = m.id AND j.version = m.current_version)`
	// bulkDecisionsSQL is decisionsInForce without its exception, enough
	// deeper to leave out any one memory and those the bulk supersedes.
	bulkDecisionsSQL = judgeCandidateSelect + `
		 WHERE m.space_id = $1 AND m.kind = 'decision' AND m.lifecycle = 'kept'
		   AND COALESCE(m.decision ->> 'status', '') IN ('', 'in_force')
		 ORDER BY m.seq DESC LIMIT $2`
	// bulkReplayReceiptsSQL reads the receipts of the keys' earlier
	// applications (claimsSQL's keys; $5 the scope's spaces).
	bulkReplayReceiptsSQL = receiptSelect + `
		 WHERE id IN (SELECT unnest(receipt_ids) FROM v2.command_keys
		               WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = ANY ($4))
		   AND space_id = ANY ($5)
		 ORDER BY seq`
	// bulkLockTargetsSQL locks the decisions Keeps would supersede.
	bulkLockTargetsSQL = memorySelect + ` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2) ORDER BY m.id FOR UPDATE OF m SKIP LOCKED`
)

// bulkLink is an active link out of a memory.
type bulkLink struct {
	kind LinkKind
	to   uuid.UUID
	ref  string
}

// bulkReview applies items in one transaction, filling out, and returns
// the items it left for Apply: their memory (or a decision they would
// supersede) was held by another transaction.
func (l *Ledger) bulkReview(ctx context.Context, verb CommandName, meta Meta, items []*bulkItem, out []BulkResult) ([]*bulkItem, error) {
	keep := verb == CommandKeep
	grant := meta.Scope.Spaces[0]
	spaceID := grant.SpaceID
	scopeIDs := meta.Scope.SpaceIDs()
	tx, loginRole, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := l.now()
	w := &writer{tx: tx, meta: &meta, command: verb, inserter: l.inserter, loginRole: loginRole, space: spaceID,
		undoWindow: l.undoWindow, judgeUndoWindow: l.judgeUndoWindow, returnWindow: l.returnWindow, now: now,
		indexJobs: l.indexJobs, forgetHonesty: l.honesty, dreamUndoWindow: l.dreamUndoWindow}

	ids, seqs, keys := []uuid.UUID{}, []int64{}, make([]string, len(items))
	for i, it := range items {
		if it.id != uuid.Nil {
			ids = append(ids, it.id)
		} else {
			seqs = append(seqs, it.seq)
		}
		keys[i] = it.key
	}

	// 1. Every read, in one round trip.
	var (
		sp        spaceRow
		spaceErr  error
		read      bool
		locked    = map[uuid.UUID]*Memory{}
		byID      = map[uuid.UUID]bool{}
		bySeq     = map[int64][]uuid.UUID{}
		claims    map[string]*storedClaim
		replayed  = map[uuid.UUID]Receipt{}
		links     = map[uuid.UUID][]bulkLink{}
		pending   = map[uuid.UUID]bool{}
		decisions []JudgeCandidate
	)
	b := &pgx.Batch{}
	b.Queue(spaceSQL, spaceID).QueryRow(func(row pgx.Row) error {
		read = true
		sp, spaceErr = scanSpace(ctx, row, spaceID)
		return nil
	})
	b.Queue(bulkLockSQL, ids, seqs, scopeIDs).Query(func(rows pgx.Rows) error {
		return collectMemories(rows, locked)
	})
	b.Queue(bulkResolveSQL, ids, seqs, scopeIDs).Query(func(rows pgx.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var seq int64
			if err := rows.Scan(&id, &seq); err != nil {
				return fmt.Errorf("ledger: resolve memories: %w", err)
			}
			byID[id] = true
			bySeq[seq] = append(bySeq[seq], id)
		}
		return rows.Err()
	})
	b.Queue(claimsSQL, spaceID, string(meta.Actor.Kind), w.actorID(), keys).Query(func(rows pgx.Rows) error {
		claims, err = scanClaims(rows)
		return err
	})
	b.Queue(bulkReplayReceiptsSQL, spaceID, string(meta.Actor.Kind), w.actorID(), keys, scopeIDs).Query(func(rows pgx.Rows) error {
		rcs, err := pgx.CollectRows(rows, scanReceipt)
		for _, rc := range rcs {
			replayed[rc.ID] = rc
		}
		if err != nil {
			return fmt.Errorf("ledger: load receipts: %w", err)
		}
		return nil
	})
	if keep {
		b.Queue(bulkLinksSQL, ids, seqs, scopeIDs).Query(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				var id, from, to uuid.UUID
				var kind LinkKind
				var seq int64
				if err := rows.Scan(&id, &kind, &from, &to, &seq); err != nil {
					return fmt.Errorf("ledger: load links: %w", err)
				}
				links[from] = append(links[from], bulkLink{kind: kind, to: to, ref: FormatRef(PrefixMemory, seq)})
			}
			return rows.Err()
		})
		b.Queue(bulkPendingSQL, ids, seqs, scopeIDs, JudgeGrace.Seconds()).Query(func(rows pgx.Rows) error {
			got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			for _, id := range got {
				pending[id] = true
			}
			return err
		})
		b.Queue(bulkDecisionsSQL, spaceID, judgeMaxDecisions+1).Query(func(rows pgx.Rows) error {
			decisions, err = pgx.CollectRows(rows, scanJudgeCandidate)
			return err
		})
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("ledger: bulk %s: %w", verb, err)
	}
	if !read {
		return nil, fmt.Errorf("ledger: bulk %s: the space wasn't read", verb)
	}
	// Resolve each item, and sort out replays and items left for Apply.
	var contended, fresh []*bulkItem
	mems := map[*bulkItem]*Memory{}
	for _, it := range items {
		id := it.id
		if id == uuid.Nil {
			switch found := bySeq[it.seq]; len(found) {
			case 0:
				out[it.pos].Err = ErrNotFound
				continue
			case 1:
				id = found[0]
			default:
				out[it.pos].Err = ErrAmbiguousRef
				continue
			}
		} else if !byID[id] {
			out[it.pos].Err = ErrNotFound
			continue
		}
		mem := locked[id]
		if mem == nil {
			contended = append(contended, it)
			continue
		}
		if spaceErr != nil {
			out[it.pos].Err = spaceErr
			continue
		}
		if stored := claims[it.key]; stored != nil {
			c, err := matchClaim(it.key, verb, it.hash, stored)
			if err != nil {
				out[it.pos].Err = err
				continue
			}
			if c.objectID != nil && *c.objectID != mem.ID {
				contended = append(contended, it)
				continue
			}
			res := Result{Outcome: c.outcome, Replayed: true}
			if err := json.Unmarshal(c.policy, &res.Policy); err != nil {
				out[it.pos].Err = fmt.Errorf("ledger: read idempotency key: %w", err)
				continue
			}
			for _, rid := range c.receiptIDs {
				if rc, ok := replayed[rid]; ok {
					res.Receipts = append(res.Receipts, rc)
				}
			}
			slices.SortFunc(res.Receipts, func(a, b Receipt) int { return cmp.Compare(a.Seq, b.Seq) })
			if c.objectID != nil {
				cp := *mem
				res.Memory = &cp
			}
			out[it.pos].Result = res
			continue
		}
		mems[it] = mem
		fresh = append(fresh, it)
	}

	// 2. The decisions the Keeps would supersede, when there are any.
	if keep {
		var targets []uuid.UUID
		for _, it := range fresh {
			if mems[it].Lifecycle != lifecycle.Proposed {
				continue
			}
			for _, ln := range links[mems[it].ID] {
				if ln.kind == LinkSupersedes && locked[ln.to] == nil && !slices.Contains(targets, ln.to) {
					targets = append(targets, ln.to)
				}
			}
		}
		if len(targets) > 0 {
			b := &pgx.Batch{}
			b.Queue(bulkLockTargetsSQL, targets, scopeIDs).Query(func(rows pgx.Rows) error {
				return collectMemories(rows, locked)
			})
			b.Queue(bulkDecisionsSQL, spaceID, judgeMaxDecisions+1+len(targets)).Query(func(rows pgx.Rows) error {
				decisions, err = pgx.CollectRows(rows, scanJudgeCandidate)
				return err
			})
			if err := tx.SendBatch(ctx, b).Close(); err != nil {
				return nil, fmt.Errorf("ledger: bulk %s: %w", verb, err)
			}
		}
	}

	// 3. Decide each item on the record as the items before it left it.
	inForce := newForceSet(decisions)
	var (
		receipts []*Receipt
		touched  []uuid.UUID
		statuses = map[uuid.UUID]bool{}
		undos    []bulkUndo
		records  []claimRow
		applied  []*bulkItem
		written  = map[*bulkItem][]*Receipt{}
		kept     bool
	)
	action, lverb, receiptAction := policy.ActionKeep, lifecycle.VerbKeep, ActionKept
	undoKind := UndoKeep
	if !keep {
		action, lverb, receiptAction, undoKind = policy.ActionReject, lifecycle.VerbReject, ActionRejected, UndoReject
	}
	touch := func(id uuid.UUID) {
		if !slices.Contains(touched, id) {
			touched = append(touched, id)
		}
	}
	for _, it := range fresh {
		mem := mems[it]
		if it.expected != 0 && it.expected != mem.Version {
			out[it.pos].Err = &EditClashError{Ref: mem.Ref, Expected: it.expected, Current: mem.Version}
			continue
		}
		dec := policy.Decide(w.policyActor(grant), action, w.object(mem, false), sp.policy())
		if dec.Effect == policy.EffectRefuse {
			out[it.pos].Result = refused(dec)
			continue
		}
		if keep && mem.Lifecycle == lifecycle.Proposed {
			if mem.Flags.Has(lifecycle.Conflict) {
				e := &InConflictError{Ref: mem.Ref}
				for _, ln := range links[mem.ID] {
					if ln.kind == LinkConflictsWith {
						e.With = ln.ref
						break
					}
				}
				out[it.pos].Err = e
				continue
			}
			if pending[mem.ID] && len(Touches(mem.Statement, mem.decisionArea(), inForce.except(mem.ID))) > 0 {
				out[it.pos].Err = &JudgePendingError{Ref: mem.Ref}
				continue
			}
		}
		next, err := transition(mem, lverb)
		if err != nil {
			out[it.pos].Err = err
			continue
		}
		// The decisions this Keep supersedes, all of which this
		// transaction must hold.
		var supersedes []*Memory
		held := true
		if next.Lifecycle == lifecycle.Kept {
			for _, ln := range links[mem.ID] {
				if ln.kind != LinkSupersedes {
					continue
				}
				d := locked[ln.to]
				if d == nil {
					held = false
					break
				}
				if d.inForce() && !slices.Contains(supersedes, d) {
					supersedes = append(supersedes, d)
				}
			}
		}
		if !held {
			contended = append(contended, it)
			continue
		}

		var j *undoJournal
		if meta.Actor.Kind == policy.ActorPerson {
			j = &undoJournal{kind: undoKind, window: l.undoWindow, LinksCreated: []uuid.UUID{}, LinksEnded: []undoLink{}}
			j.touch(mem)
		}
		rc := w.receipt(sp, mem.ID, mem.Ref, receiptAction, mem.streamVersion+1, meta.Reason)
		if receiptAction == ActionKept {
			rc.Assurance = w.policyActor(grant).Assurance()
		}
		mine := []*Receipt{&rc}
		mem.Lifecycle, mem.Flags, mem.streamVersion, mem.LastReceiptID = next.Lifecycle, next.Flags, rc.StreamVersion, rc.ID
		mem.State = lifecycle.DisplayState(mem.Lifecycle, mem.Flags)
		touch(mem.ID)
		if next.Lifecycle == lifecycle.Kept {
			kept = true
			for _, d := range supersedes {
				j.touch(d)
				src := ReceiptSource{Kind: ObjectMemory, Ref: mem.Ref}
				drc := w.receipt(sp, d.ID, d.Ref, ActionSuperseded, d.streamVersion+1, fmt.Sprintf("Superseded by %s.", mem.Ref))
				drc.Source = &src
				d.streamVersion, d.LastReceiptID = drc.StreamVersion, drc.ID
				if d.Decision == nil {
					d.Decision = &DecisionFields{}
				}
				d.Decision.Status = DecisionSuperseded
				statuses[d.ID] = true
				touch(d.ID)
				inForce.remove(d.ID)
				mine = append(mine, &drc)
			}
			if mem.inForce() {
				inForce.add(mem)
			}
		}
		receipts = append(receipts, mine...)
		rids := make([]uuid.UUID, len(mine))
		for i, r := range mine {
			rids[i] = r.ID
		}
		if j != nil {
			for i := range j.Memories {
				j.Memories[i].AfterStreamVersion = locked[j.Memories[i].ID].streamVersion
			}
			undos = append(undos, bulkUndo{journal: j, receipts: rids})
		}
		policyRaw, err := policyJSON(dec)
		if err != nil {
			return nil, err
		}
		id := mem.ID
		records = append(records, claimRow{key: it.key, hash: it.hash, outcome: OutcomeApplied, policy: policyRaw,
			object: &id, receipts: rids})
		out[it.pos].Result = Result{Outcome: OutcomeApplied, Policy: dec}
		written[it] = mine
		applied = append(applied, it)
	}
	if len(applied) == 0 {
		return contended, nil
	}

	// 4. Every write, in one round trip, and the memories read back.
	b = &pgx.Batch{}
	if err := queueReceipts(b, receipts); err != nil {
		return nil, err
	}
	queueMemoryStates(b, spaceID, touched, locked, statuses)
	if err := queueUndos(b, sp, meta, undos); err != nil {
		return nil, err
	}
	w.queueClaims(b, spaceID, verb, records)
	if kept {
		b.Queue(dirtySQL, spaceID, []uuid.UUID(nil)).Query(func(rows pgx.Rows) error {
			targets, err := dirtyTargets(rows, nil)
			w.queueCompiles(spaceID, targets)
			return err
		})
	}
	after := map[uuid.UUID]*Memory{}
	b.Queue(memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2)`, touched, scopeIDs).Query(func(rows pgx.Rows) error {
		return collectMemories(rows, after)
	})
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("ledger: bulk %s: %w", verb, err)
	}
	// The receipts have their seq and recorded_at now.
	for _, it := range applied {
		res := &out[it.pos].Result
		for _, rc := range written[it] {
			res.Receipts = append(res.Receipts, *rc)
		}
		m := after[mems[it].ID]
		if m == nil {
			return nil, fmt.Errorf("ledger: bulk %s: %s wasn't read back", verb, mems[it].Ref)
		}
		cp := *m
		res.Memory = &cp
	}

	// 5. River's insert, then COMMIT.
	if err := w.flush(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("ledger: commit bulk %s: %w", verb, err)
	}
	for _, it := range applied {
		res := out[it.pos].Result
		l.log.Info("ledger: applied",
			"command", string(verb), "outcome", string(res.Outcome), "policy", res.Policy.Code,
			"memory", res.Memory.Ref, "space_id", res.Memory.SpaceID.String(),
			"actor_kind", string(meta.Actor.Kind), "via", string(meta.Via), "receipts", len(res.Receipts),
			"jobs", len(w.jobs), "bulk", len(items))
	}
	return contended, nil
}

// collectMemories reads memorySelect rows into into, by id.
func collectMemories(rows pgx.Rows, into map[uuid.UUID]*Memory) error {
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return fmt.Errorf("ledger: load memories: %w", err)
	}
	for _, m := range ms {
		into[m.ID] = m
	}
	return nil
}

// decisionArea is the area Touches is given for a memory (awaitJudge's).
func (m *Memory) decisionArea() string {
	if m.Decision == nil {
		return ""
	}
	return m.Decision.Area
}

// forceSet is a space's decisions in force as a bulk review changes them:
// decisionsInForce at each step.
type forceSet struct {
	ds []JudgeCandidate
}

func newForceSet(ds []JudgeCandidate) *forceSet { return &forceSet{ds: slices.Clone(ds)} }

// except is decisionsInForce(space, id): the newest judgeMaxDecisions,
// leaving id out.
func (f *forceSet) except(id uuid.UUID) []JudgeCandidate {
	out := make([]JudgeCandidate, 0, len(f.ds))
	for _, d := range f.ds {
		if d.ID != id {
			out = append(out, d)
		}
	}
	if len(out) > judgeMaxDecisions {
		out = out[:judgeMaxDecisions]
	}
	return out
}

func (f *forceSet) remove(id uuid.UUID) {
	f.ds = slices.DeleteFunc(f.ds, func(d JudgeCandidate) bool { return d.ID == id })
}

// add puts a memory just kept among the decisions in force, in seq order
// (newest first).
func (f *forceSet) add(m *Memory) {
	f.remove(m.ID)
	c := JudgeCandidate{ID: m.ID, Ref: m.Ref, Statement: m.Statement, Kind: m.Kind, Section: m.Section,
		Lifecycle: m.Lifecycle, Trust: m.Trust, Area: m.area(), InForce: true, UpdatedAt: m.UpdatedAt}
	if m.Decision != nil {
		c.DecisionStatus = m.Decision.Status
	}
	i, _ := slices.BinarySearchFunc(f.ds, m.seq, func(d JudgeCandidate, seq int64) int {
		_, n, _ := ParseRef(d.Ref)
		switch {
		case n > seq:
			return -1
		case n < seq:
			return 1
		}
		return 0
	})
	f.ds = slices.Insert(f.ds, i, c)
}

// queueMemoryStates queues one UPDATE of every memory a bulk review
// changed, to its final state: lifecycle, flags, stream version and last
// receipt, and the decision status of those it superseded.
func queueMemoryStates(b *pgx.Batch, spaceID uuid.UUID, ids []uuid.UUID, state map[uuid.UUID]*Memory, statuses map[uuid.UUID]bool) {
	n := len(ids)
	lifecycles, flags, status := make([]string, n), make([]string, n), make([]string, n)
	versions, lasts := make([]int32, n), make([]uuid.UUID, n)
	for i, id := range ids {
		m := state[id]
		lifecycles[i], versions[i], lasts[i] = string(m.Lifecycle), int32(m.streamVersion), m.LastReceiptID
		flags[i] = strings.Join(m.Flags.Strings(), ",")
		if statuses[id] {
			status[i] = m.Decision.Status
		}
	}
	b.Queue(`
		UPDATE v2.memories m
		   SET lifecycle = u.lifecycle, flags = string_to_array(u.flags, ','), stream_version = u.stream_version,
		       last_receipt_id = u.last_receipt_id, updated_at = now(),
		       decision = CASE WHEN u.status = '' THEN m.decision
		                       ELSE jsonb_set(COALESCE(m.decision, '{}'::jsonb), '{status}', to_jsonb(u.status)) END
		  FROM unnest($1::uuid[], $2::text[], $3::text[], $4::int[], $5::uuid[], $6::text[])
		       AS u(id, lifecycle, flags, stream_version, last_receipt_id, status)
		 WHERE m.id = u.id AND m.space_id = $7`,
		ids, lifecycles, flags, versions, lasts, status, spaceID)
}

// bulkUndo is one item's undo entry.
type bulkUndo struct {
	journal  *undoJournal
	receipts []uuid.UUID
}

// queueUndos queues the INSERT of the items' undo entries (storeUndo's).
func queueUndos(b *pgx.Batch, sp spaceRow, meta Meta, undos []bulkUndo) error {
	if len(undos) == 0 {
		return nil
	}
	n := len(undos)
	ids, firsts := make([]uuid.UUID, n), make([]uuid.UUID, n)
	commands, receipts, memories, inverses := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	secs := make([]float64, n)
	for i, u := range undos {
		inverse, err := json.Marshal(u.journal)
		if err != nil {
			return err
		}
		mids := make([]uuid.UUID, len(u.journal.Memories))
		for k, m := range u.journal.Memories {
			mids[k] = m.ID
		}
		ids[i], firsts[i], commands[i] = newID(), u.receipts[0], string(u.journal.kind)
		receipts[i], memories[i], inverses[i], secs[i] = uuidArray(u.receipts), uuidArray(mids), string(inverse), u.journal.window.Seconds()
	}
	var actorID *uuid.UUID
	if meta.Actor.ID != uuid.Nil {
		id := meta.Actor.ID
		actorID = &id
	}
	b.Queue(`
		INSERT INTO v2.undo_entries (id, tenant_id, space_id, command, actor_kind, actor_id, receipt_ids, memory_ids,
		                             inverse, expires_at, receipt_id, last_receipt_id)
		SELECT u.id, $1, $2, u.command, $3, $4, u.receipt_ids::uuid[], u.memory_ids::uuid[], u.inverse::jsonb,
		       now() + make_interval(secs => u.secs), u.receipt_id, u.receipt_id
		  FROM unnest($5::uuid[], $6::text[], $7::text[], $8::text[], $9::text[], $10::float8[], $11::uuid[])
		       AS u(id, command, receipt_ids, memory_ids, inverse, secs, receipt_id)`,
		sp.TenantID, sp.ID, string(policy.ActorPerson), actorID, ids, commands, receipts, memories, inverses, secs, firsts)
	return nil
}
