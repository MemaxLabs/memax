package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Forget (plan 25 §5.13, rule 7). One command, in one transaction, takes
// a memory's words out of everything Memax holds in Postgres:
//
//   - every statement version, its sources' quotes, URIs, refs, locators
//     and content hashes, the decision fields, conditions and scope, the
//     search vector and the judge's fingerprints (the embeddings go by
//     037's triggers in the same statement);
//   - the reasons (and their salts) on its receipts;
//   - the judge's rationale and merged statement on every verdict that
//     names it, on either side of the pair;
//   - the idempotency request hashes of the commands that carried its
//     words;
//   - a gate's question, context and options when it is the decision the
//     gate became (with a forgot receipt about the gate);
//   - the prose of every Brief version that cited it (the current version
//     gets a new B- without it);
//   - the drift evidence that quoted it (a purged receipt about the target);
//
// and writes a forgot receipt, a tombstone, the steps of its propagation
// and a notice for every agent connection that read it or is connected to
// the space, dirties every target, and queues forget_propagate (River,
// same transaction) to recompile the targets, re-render the old artifacts,
// purge the caches and copy the forget ledger. A deferred trigger refuses
// the commit if a forgotten memory still has words in a version or a
// source, or no tombstone.
//
// # What goes with it
//
// Some memories carry its words and go with it: proposals folded into it
// (copies of its words), proposals that would change it (its words,
// edited), and memories built from it (a kept Ask answer citing it as a
// source of kind memory), transitively. They are forgotten in the same
// transaction, each with its own receipt and tombstone, but never without
// the person seeing them: Forget must name them (Carries), or it is
// refused with *ForgetCarriesError listing them. Flagging them for Review,
// or proposing to forget them, would leave the forgotten words readable
// in them until someone acted, possibly never; cutting the forgotten
// memory's part out of an answer can't be done faithfully; forgetting
// them silently would take a memory a person kept without them seeing it.
//
// # What it never touches
//
// A decision it superseded stays superseded (forgetting the newer decision
// doesn't put the older one back in force; a person re-keeps it). Other
// memories' own words stay. The note the person leaves stays on the
// tombstone.

// forgetOp is one Forget in progress: the tombstone it hangs from and
// everything it forgets.
type forgetOp struct {
	id       uuid.UUID // the primary tombstone's id
	kind     string    // memory | space
	primary  string    // the primary's ref ("M-0201", or "space")
	retire   bool
	note     string
	request  *uuid.UUID // the agent connection whose request led to it
	refs     []string
	ids      []uuid.UUID
	words    []string // every version's statement, held in memory only to find quotes of it
	receipts []Receipt
	gone     TombstoneGone
}

func (op *forgetOp) has(ref string) bool { return slices.Contains(op.refs, ref) }

// validate checks a Forget.
func (c *Forget) validate() error {
	if err := validateTarget(c.Memory, c.ExpectedVersion, true); err != nil {
		return err
	}
	c.Note = strings.TrimSpace(c.Note)
	if err := checkText("note", c.Note, MaxNoteRunes, false); err != nil {
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

// forget is Forget.
func (w *writer) forget(ctx context.Context, c *Forget) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayForget(ctx, replay)
	}
	if c.ExpectedVersion != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: c.ExpectedVersion, Current: mem.Version}
	}
	if _, err := transition(mem, lifecycle.VerbForget); err != nil {
		return Result{}, err
	}
	pa := w.policyActor(grant)
	dec := policy.Decide(pa, policy.ActionForget, policy.Object{Ref: mem.Ref, Lifecycle: mem.Lifecycle,
		Decision: mem.Kind == KindDecision, Secrets: findSecrets(c.Note)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}

	carried, err := w.carriedBy(ctx, sp.ID, []*Memory{mem})
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
			d.Message = fmt.Sprintf("%s goes with %s, and you can't forget it: %s", m.Ref, mem.Ref, d.Message)
			return refused(d), nil
		}
	}
	refs := make([]string, len(carried))
	for i, cm := range carried {
		refs[i] = cm.Ref
	}
	if !sameRefs(c.Carries, refs) {
		return Result{}, &ForgetCarriesError{Ref: mem.Ref, Carries: carried}
	}

	op := &forgetOp{id: newID(), kind: ObjectMemory, primary: mem.Ref, note: c.Note}
	op.request, err = w.forgetRequester(ctx, mem.ID)
	if err != nil {
		return Result{}, err
	}
	op.refs = append([]string{mem.Ref}, refs...)
	op.ids = []uuid.UUID{mem.ID}
	for _, cm := range carried {
		op.ids = append(op.ids, cm.ID)
	}
	files, err := w.filesHolding(ctx, sp.ID, op.refs)
	if err != nil {
		return Result{}, err
	}
	if _, err := w.purgeMemory(ctx, sp, op, mem, nil); err != nil {
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
	if err := w.afterPurge(ctx, sp, op, files); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: op.receipts}
	res, err = w.finish(ctx, res, mem.ID)
	if err != nil {
		return Result{}, err
	}
	if res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, mem.ID, w.honesty()); err != nil {
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

// honesty is the ledger's ForgetHonesty, through the writer.
func (w *writer) honesty() ForgetHonesty { return w.forgetHonesty }

// replayForget is a retried Forget: the original receipts, the forgotten
// memory and its tombstone.
func (w *writer) replayForget(ctx context.Context, replay *Result) (Result, error) {
	res := *replay
	if res.Memory == nil {
		return res, nil
	}
	var err error
	res.Tombstone, err = loadTombstone(ctx, w.tx, w.meta.Scope, res.Memory.ID, w.honesty())
	if errors.Is(err, ErrNotFound) {
		err = nil
	}
	return res, err
}

// carriedBy finds what goes with the roots' Forget, transitively: the
// proposals folded into them, the proposals that would change them, and
// every memory citing one of them as a source. Forgotten memories are
// left out (their words are gone already).
func (w *writer) carriedBy(ctx context.Context, spaceID uuid.UUID, roots []*Memory) ([]Carried, error) {
	return carriedBy(ctx, w.tx, spaceID, roots)
}

func carriedBy(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, roots []*Memory) ([]Carried, error) {
	seen := map[uuid.UUID]bool{}
	frontier := make([]uuid.UUID, 0, len(roots))
	refOf := map[uuid.UUID]string{}
	for _, r := range roots {
		seen[r.ID] = true
		frontier = append(frontier, r.ID)
		refOf[r.ID] = r.Ref
	}
	var out []Carried
	for len(frontier) > 0 {
		texts := make([]string, len(frontier))
		for i, id := range frontier {
			texts[i] = id.String()
		}
		rows, err := tx.Query(ctx, `
			SELECT m.id, m.seq, m.lifecycle, m.kind, 'folded', l.to_memory_id
			  FROM v2.memory_links l JOIN v2.memories m ON m.id = l.from_memory_id
			 WHERE l.space_id = $1 AND l.kind = 'merged_into' AND l.ended_receipt_id IS NULL
			   AND l.to_memory_id = ANY ($2) AND m.lifecycle = 'merged'
			UNION ALL
			SELECT m.id, m.seq, m.lifecycle, m.kind, 'updates', l.to_memory_id
			  FROM v2.memory_links l JOIN v2.memories m ON m.id = l.from_memory_id
			 WHERE l.space_id = $1 AND l.kind = 'supersedes' AND l.ended_receipt_id IS NULL
			   AND l.to_memory_id = ANY ($2) AND m.lifecycle = 'proposed'
			UNION ALL
			SELECT m.id, m.seq, m.lifecycle, m.kind, 'cites', (s.locator ->> 'memory')::uuid
			  FROM v2.sources s
			  JOIN v2.memory_sources ms ON ms.source_id = s.id
			  JOIN v2.memories m ON m.id = ms.memory_id
			 WHERE s.space_id = $1 AND s.kind = 'memory' AND s.locator ->> 'memory' = ANY ($3)
			   AND m.lifecycle <> 'forgotten'
			 ORDER BY 2`, spaceID, frontier, texts)
		if err != nil {
			return nil, fmt.Errorf("ledger: what goes with the forget: %w", err)
		}
		var next []uuid.UUID
		for rows.Next() {
			var c Carried
			var seq int64
			var with uuid.UUID
			if err := rows.Scan(&c.ID, &seq, &c.Lifecycle, &c.Kind, &c.Reason, &with); err != nil {
				rows.Close()
				return nil, fmt.Errorf("ledger: what goes with the forget: %w", err)
			}
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			c.Ref = FormatRef(PrefixMemory, seq)
			c.With = refOf[with]
			refOf[c.ID] = c.Ref
			out = append(out, c)
			next = append(next, c.ID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("ledger: what goes with the forget: %w", err)
		}
		if len(out) > MaxCarried {
			return nil, invalid("carries", "more than %d memories go with this one; forget some of them first", MaxCarried)
		}
		frontier = next
	}
	return out, nil
}

// lockCarried locks the carried memories (after the primary, in id order).
func (w *writer) lockCarried(ctx context.Context, carried []Carried) (map[uuid.UUID]*Memory, error) {
	if len(carried) == 0 {
		return map[uuid.UUID]*Memory{}, nil
	}
	ids := make([]uuid.UUID, len(carried))
	for i, c := range carried {
		ids[i] = c.ID
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR UPDATE")
	if err != nil {
		return nil, err
	}
	for _, c := range carried {
		if locked[c.ID] == nil {
			return nil, fmt.Errorf("%w: %s changed meanwhile; try again", ErrEditClash, c.Ref)
		}
	}
	return locked, nil
}

// forgetRequester is the agent connection whose waiting request asked to
// forget the memory, if any (the newest).
func (w *writer) forgetRequester(ctx context.Context, memoryID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := w.tx.QueryRow(ctx, `
		SELECT requested_by FROM v2.forget_requests
		 WHERE memory_id = $1 AND status = 'waiting' ORDER BY created_at DESC LIMIT 1`, memoryID).Scan(&id)
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: forget requests: %w", err)
	}
	return &id, nil
}

// filesHolding lists the targets whose latest good compile, or the one on
// disk, holds any of refs: the files a Forget rewrites.
func (w *writer) filesHolding(ctx context.Context, spaceID uuid.UUID, refs []string) ([]uuid.UUID, error) {
	return filesHolding(ctx, w.tx, spaceID, refs)
}

func filesHolding(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, refs []string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id FROM v2.targets t
		 WHERE t.space_id = $1
		   AND (EXISTS (SELECT 1 FROM v2.compile_runs c WHERE c.id = t.delivered_compile_id AND c.refs && $2)
		        OR EXISTS (SELECT 1 FROM (SELECT c.refs FROM v2.compile_runs c
		                                   WHERE c.target_id = t.id AND c.status <> 'failed'
		                                   ORDER BY c.seq DESC LIMIT 1) latest
		                    WHERE latest.refs && $2))
		 ORDER BY t.created_at, t.id`, spaceID, refs)
	if err != nil {
		return nil, fmt.Errorf("ledger: files holding the memory: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("ledger: files holding the memory: %w", err)
	}
	return ids, nil
}

// purgeMemory forgets one memory: its forgot receipt, the words in every
// row that holds them, and its tombstone. carried is nil for the memory
// the person named.
func (w *writer) purgeMemory(ctx context.Context, sp spaceRow, op *forgetOp, m *Memory, carried *Carried) (Receipt, error) {
	rc := w.receipt(sp, m.ID, m.Ref, ActionForgot, m.streamVersion+1, "")
	switch {
	case carried != nil && carried.Reason != CarrySpace:
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: carried.With}
	case op.kind == ObjectSpace:
		rc.Source = &ReceiptSource{Kind: ObjectSpace, Ref: SpaceObjectRef}
	}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	op.receipts = append(op.receipts, rc)

	// What it had, before the words go: counts for the tombstone, and the
	// words themselves (in memory, for finding quotes of them).
	var g TombstoneGone
	var keptAt *time.Time
	var reads int
	candidate := fmt.Sprintf(`[{"memory_id": %q}]`, m.ID.String())
	err := w.tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL),
		  (SELECT count(*) FROM v2.memory_sources WHERE memory_id = $1),
		  (SELECT count(*) FROM v2.memory_embeddings WHERE memory_id = $1),
		  (SELECT count(*) FROM v2.judge_verdicts WHERE space_id = $2
		      AND (memory_id = $1 OR related_memory_id = $1 OR candidates @> $4::jsonb)),
		  (SELECT count(*) FROM v2.judge_verdicts WHERE space_id = $2 AND model IS NOT NULL
		      AND (memory_id = $1 OR related_memory_id = $1 OR candidates @> $4::jsonb)),
		  (SELECT count(*) FROM v2.decision_gates WHERE space_id = $2 AND answer_memory_id = $1),
		  (SELECT min(occurred_at) FROM v2.receipts WHERE stream_id = $1 AND action = 'kept'),
		  (SELECT COALESCE(sum(r.reads), 0)::int FROM v2.read_rollups r
		    WHERE r.space_id = $2
		      AND (r.subject_id = $1 OR r.subject_id IN (
		          SELECT c.id FROM v2.compile_runs c WHERE c.space_id = $2 AND c.refs @> ARRAY[$3::text])))`,
		m.ID, sp.ID, m.Ref, candidate).Scan(&g.Versions, &g.Sources, &g.Embeddings, &g.Verdicts, &g.ModelVerdicts,
		&g.Gates, &keptAt, &reads)
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: %w", m.Ref, err)
	}
	rows, err := w.tx.Query(ctx, `SELECT statement FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL`, m.ID)
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: %w", m.Ref, err)
	}
	words, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: %w", m.Ref, err)
	}
	op.words = append(op.words, words...)

	// The memory: forgotten, with nothing derived from its words left.
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET lifecycle = 'forgotten', flags = '{}', search = NULL, content_sha256 = NULL, minhash_bands = NULL,
		       embedding = NULL, decision = NULL, conditions = '[]'::jsonb, scope = '{}'::jsonb,
		       stream_version = $2, last_receipt_id = $3, updated_at = now()
		 WHERE id = $1 AND space_id = $4`, m.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: %w", m.Ref, err)
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memory_versions SET statement = NULL, last_receipt_id = $2
		 WHERE memory_id = $1 AND space_id = $3 AND statement IS NOT NULL`, m.ID, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: versions: %w", m.Ref, err)
	}
	// Its sources: a source belongs to one memory. The ref becomes the
	// kind ("pr"), since people write refs ("PR #212 · Move jobs to River").
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.sources s
		   SET quote = NULL, uri = NULL, content_hash = NULL, ref = s.kind, locator = '{}'::jsonb, last_receipt_id = $2
		  FROM v2.memory_sources ms
		 WHERE ms.source_id = s.id AND ms.memory_id = $1 AND s.space_id = $3`, m.ID, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: sources: %w", m.Ref, err)
	}
	if _, err := w.tx.Exec(ctx, `SELECT v2.redact_receipt_reasons($1)`, m.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: receipts: %w", m.Ref, err)
	}
	// The judge's words, on both sides of every pair it is part of.
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.judge_verdicts SET rationale = NULL, merged_statement = NULL, last_receipt_id = $2
		 WHERE space_id = $3 AND (rationale IS NOT NULL OR merged_statement IS NOT NULL)
		   AND (memory_id = $1 OR related_memory_id = $1 OR candidates @> $4::jsonb)`,
		m.ID, rc.ID, sp.ID, candidate); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: verdicts: %w", m.Ref, err)
	}
	// The model's words about an import disagreement it is a member of.
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.import_conflicts SET subject = NULL, rationale = NULL, suggestion = NULL, last_receipt_id = $2
		 WHERE space_id = $3 AND $1 = ANY (members)
		   AND (subject IS NOT NULL OR rationale IS NOT NULL OR suggestion IS NOT NULL)`,
		m.ID, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: import conflicts: %w", m.Ref, err)
	}
	// The stored request hashes of the commands that carried its words.
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.command_keys SET request_hash = NULL
		 WHERE space_id = $2 AND request_hash IS NOT NULL
		   AND (object_id = $1
		        OR receipt_ids && ARRAY(SELECT r.id FROM v2.receipts r WHERE r.space_id = $2 AND r.object_id = $1))`,
		m.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: idempotency: %w", m.Ref, err)
	}
	if err := w.purgeGates(ctx, sp, op, m); err != nil {
		return Receipt{}, err
	}
	// Requests to forget it are answered.
	var by *uuid.UUID
	if w.meta.Actor.Kind == policy.ActorPerson {
		by = w.actorID()
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.forget_requests SET status = 'forgotten', decided_by = $2, decided_at = now(), last_receipt_id = $3, updated_at = now()
		 WHERE memory_id = $1 AND space_id = $4 AND status = 'waiting'`, m.ID, by, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: forget %s: requests: %w", m.Ref, err)
	}
	if err := w.endConflicts(ctx, sp, op, m, rc); err != nil {
		return Receipt{}, err
	}

	g.Files = 0
	if err := w.insertTombstone(ctx, sp, op, m.ID, m.Ref, ObjectMemory, carried, rc, keptAt, reads, g); err != nil {
		return Receipt{}, err
	}
	op.gone.Versions += g.Versions
	op.gone.Sources += g.Sources
	op.gone.Embeddings += g.Embeddings
	op.gone.Verdicts += g.Verdicts
	op.gone.ModelVerdicts += g.ModelVerdicts
	op.gone.Gates += g.Gates
	op.gone.Memories++
	return rc, nil
}

// purgeGates forgets the words of the gates whose answer is m: the
// question was the asking agent's, and the decision m holds them.
func (w *writer) purgeGates(ctx context.Context, sp spaceRow, op *forgetOp, m *Memory) error {
	rows, err := w.tx.Query(ctx, `
		SELECT id, seq, stream_version FROM v2.decision_gates
		 WHERE space_id = $1 AND answer_memory_id = $2
		   AND (question IS NOT NULL OR context IS NOT NULL OR options IS NOT NULL)
		 ORDER BY id FOR UPDATE`, sp.ID, m.ID)
	if err != nil {
		return fmt.Errorf("ledger: forget %s: gates: %w", m.Ref, err)
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
		return fmt.Errorf("ledger: forget %s: gates: %w", m.Ref, err)
	}
	for _, g := range gates {
		rc := w.objectReceipt(sp, ObjectGate, g.id, FormatRef(PrefixDecision, g.seq), ActionForgot, g.version+1, "")
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: m.Ref}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return err
		}
		op.receipts = append(op.receipts, rc)
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.decision_gates SET question = NULL, context = NULL, options = NULL,
			       stream_version = $2, last_receipt_id = $3, updated_at = now()
			 WHERE id = $1 AND space_id = $4`, g.id, rc.StreamVersion, rc.ID, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget %s: gate words: %w", m.Ref, err)
		}
		if _, err := w.tx.Exec(ctx, `SELECT v2.redact_receipt_reasons($1)`, g.id); err != nil {
			return fmt.Errorf("ledger: forget %s: gate receipts: %w", m.Ref, err)
		}
	}
	return nil
}

// endConflicts ends m's conflicts: the conflicts_with links end with its
// forgot receipt, and a memory left with no conflict loses its flag, with
// a resolved receipt naming m (the other side no longer exists).
func (w *writer) endConflicts(ctx context.Context, sp spaceRow, op *forgetOp, m *Memory, forgot Receipt) error {
	links, err := activeLinks(ctx, w.tx, []uuid.UUID{m.ID})
	if err != nil {
		return err
	}
	var others []uuid.UUID
	for _, l := range links[m.ID] {
		if l.Kind != LinkConflictsWith {
			continue
		}
		if err := w.endLink(ctx, l, forgot.ID); err != nil {
			return err
		}
		if !slices.Contains(op.ids, l.MemoryID) {
			others = append(others, l.MemoryID)
		}
	}
	if len(others) == 0 {
		return nil
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, others, "FOR UPDATE")
	if err != nil {
		return err
	}
	remaining, err := activeLinks(ctx, w.tx, others)
	if err != nil {
		return err
	}
	for _, id := range others {
		a := locked[id]
		if a == nil || !a.Flags.Has(lifecycle.Conflict) {
			continue
		}
		still := slices.ContainsFunc(remaining[id], func(l Link) bool { return l.Kind == LinkConflictsWith })
		if still {
			continue
		}
		next, err := transition(a, lifecycle.VerbClearConflict)
		if err != nil {
			return err
		}
		rc := w.receipt(sp, a.ID, a.Ref, ActionResolved, a.streamVersion+1, "")
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: m.Ref}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return err
		}
		op.receipts = append(op.receipts, rc)
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.memories SET flags = $2, stream_version = $3, last_receipt_id = $4, updated_at = now()
			 WHERE id = $1 AND space_id = $5`, a.ID, next.Flags.Strings(), rc.StreamVersion, rc.ID, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget %s: clear %s's conflict: %w", m.Ref, a.Ref, err)
		}
	}
	return nil
}

// insertTombstone writes one forgotten object's tombstone. The op's
// primary tombstone has op.id as its id.
func (w *writer) insertTombstone(ctx context.Context, sp spaceRow, op *forgetOp, objectID uuid.UUID, ref, kind string,
	carried *Carried, rc Receipt, keptAt *time.Time, reads int, g TombstoneGone) error {
	id := op.id
	var carriedReason, note any
	if carried != nil {
		id = newID()
		carriedReason = carried.Reason
	} else if op.note != "" {
		note = op.note
	}
	byKind := string(policy.ActorPerson)
	if w.meta.Actor.Kind != policy.ActorPerson {
		byKind = string(policy.ActorMemax)
	}
	var request any
	if carried == nil && op.request != nil {
		request = *op.request
	}
	gone, err := json.Marshal(g)
	if err != nil {
		return err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.tombstones (id, tenant_id, space_id, op_id, object_kind, object_id, object_ref, carried, note,
		                           by_kind, by_id, requested_by, via, receipt_id, forgotten_at, kept_at, reads_before, gone)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		id, sp.TenantID, sp.ID, op.id, kind, objectID, ref, carriedReason, note,
		byKind, w.actorID(), request, string(w.meta.Via), rc.ID, rc.RecordedAt, keptAt, reads, gone); err != nil {
		return fmt.Errorf("ledger: tombstone %s: %w", ref, err)
	}
	return nil
}

// afterPurge is what a Forget does once the memories are purged: the
// Brief and the drift evidence lose the words, the targets recompile, the
// steps of the propagation and the agents' notices are written, and the
// propagation job is queued.
func (w *writer) afterPurge(ctx context.Context, sp spaceRow, op *forgetOp, files []uuid.UUID) error {
	if err := w.purgeBrief(ctx, sp, op); err != nil {
		return err
	}
	if err := w.purgeObservations(ctx, sp, op); err != nil {
		return err
	}
	if !op.retire {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return err
		}
	}
	if err := w.queuePropagation(ctx, sp, op, files); err != nil {
		return err
	}
	if err := w.queueNotices(ctx, sp, op); err != nil {
		return err
	}
	// The primary tombstone counts the files; a space's also counts what
	// every memory in it had.
	nfiles := len(files)
	if op.kind == ObjectSpace {
		g := op.gone
		g.Files = nfiles
		gone, err := json.Marshal(g)
		if err != nil {
			return err
		}
		if _, err := w.tx.Exec(ctx, `UPDATE v2.tombstones SET gone = $2, updated_at = now() WHERE id = $1`, op.id, gone); err != nil {
			return fmt.Errorf("ledger: tombstone: %w", err)
		}
	} else if _, err := w.tx.Exec(ctx, `
		UPDATE v2.tombstones SET gone = jsonb_set(gone, '{files}', to_jsonb($2::int)), updated_at = now() WHERE id = $1`,
		op.id, nfiles); err != nil {
		return fmt.Errorf("ledger: tombstone: %w", err)
	}
	w.jobs = append(w.jobs, river.InsertManyParams{Args: ForgetPropagateArgs{OpID: op.id, SpaceID: sp.ID}})
	return nil
}

// purgeBrief takes the forgotten memories out of the Brief: the current
// version gets a new B- without them (and without any prose that cited
// them), and older versions keep that prose's citations without its words.
// A space's Forget also clears every version's title and summary.
func (w *writer) purgeBrief(ctx context.Context, sp spaceRow, op *forgetOp) error {
	if op.retire {
		return nil // the Brief goes with the space
	}
	cur, err := lockBrief(ctx, w.tx, sp.ID)
	if err != nil || cur == nil {
		return err
	}
	rows, err := w.tx.Query(ctx, `
		SELECT id, version, title, COALESCE(summary, ''), structure FROM v2.brief_versions
		 WHERE brief_id = $1 AND space_id = $2 ORDER BY version`, cur.id, sp.ID)
	if err != nil {
		return fmt.Errorf("ledger: forget: Brief: %w", err)
	}
	type version struct {
		id             uuid.UUID
		version        int
		title, summary string
		st             briefStructure
	}
	var versions []version
	for rows.Next() {
		var v version
		var raw []byte
		if err := rows.Scan(&v.id, &v.version, &v.title, &v.summary, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: forget: Brief: %w", err)
		}
		if err := json.Unmarshal(raw, &v.st); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: forget: Brief %d: %w", v.version, err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: forget: Brief: %w", err)
	}
	wipe := op.kind == ObjectSpace
	cites := func(it BriefItem) bool {
		return slices.ContainsFunc(it.Cites, op.has)
	}

	// The current version, without them.
	var current *version
	for i := range versions {
		if versions[i].version == cur.version {
			current = &versions[i]
		}
	}
	stream := cur.streamVersion
	var rc *Receipt
	if current != nil {
		next := briefStructure{Sections: []BriefSection{}}
		changed := wipe && (current.title != "Brief" || current.summary != "")
		for _, s := range current.st.Sections {
			ns := BriefSection{Key: s.Key, Heading: s.Heading, Items: []BriefItem{}}
			for _, it := range s.Items {
				if (it.Ref != "" && op.has(it.Ref)) || (it.Ref == "" && (cites(it) || wipe)) {
					changed = true
					continue
				}
				ns.Items = append(ns.Items, it)
			}
			next.Sections = append(next.Sections, ns)
		}
		if changed {
			seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixBrief)
			if err != nil {
				return err
			}
			r := w.objectReceipt(sp, ObjectBrief, cur.id, FormatRef(PrefixBrief, seq), ActionRevised, stream+1, "")
			r.Source = &ReceiptSource{Kind: op.kind, Ref: op.primary}
			if err := insertReceipt(ctx, w.tx, &r); err != nil {
				return err
			}
			rc, stream = &r, stream+1
			op.receipts = append(op.receipts, r)
			title, summary := current.title, any(nullText(current.summary))
			if wipe {
				title, summary = "Brief", nil
			}
			structure, err := json.Marshal(next)
			if err != nil {
				return err
			}
			if _, err := w.tx.Exec(ctx, `
				INSERT INTO v2.brief_versions (id, brief_id, version, tenant_id, space_id, seq, parent_version, title, summary,
				                               structure, receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`,
				newID(), cur.id, cur.version+1, sp.TenantID, sp.ID, seq, cur.version, title, summary, structure, r.ID); err != nil {
				return fmt.Errorf("ledger: forget: new Brief version: %w", err)
			}
			if _, err := w.tx.Exec(ctx, `
				UPDATE v2.briefs SET current_version = $2, stream_version = $3, last_receipt_id = $4, updated_at = now()
				 WHERE id = $1 AND space_id = $5`, cur.id, cur.version+1, stream, r.ID, sp.ID); err != nil {
				return fmt.Errorf("ledger: forget: Brief: %w", err)
			}
		}
	}

	// Older versions keep the citations, not the words.
	for _, v := range versions {
		changed := false
		for si := range v.st.Sections {
			for ii, it := range v.st.Sections[si].Items {
				if it.Ref == "" && it.Text != "" && (cites(it) || wipe) {
					v.st.Sections[si].Items[ii] = BriefItem{Cites: it.Cites, Forgotten: true}
					changed = true
				}
			}
		}
		if wipe && (v.title != "Brief" || v.summary != "") {
			changed = true
		}
		if !changed {
			continue
		}
		if rc == nil {
			r := w.objectReceipt(sp, ObjectBrief, cur.id, FormatRef(PrefixBrief, cur.seq), ActionPurged, stream+1, "")
			r.Source = &ReceiptSource{Kind: op.kind, Ref: op.primary}
			if err := insertReceipt(ctx, w.tx, &r); err != nil {
				return err
			}
			rc, stream = &r, stream+1
			op.receipts = append(op.receipts, r)
			if _, err := w.tx.Exec(ctx, `
				UPDATE v2.briefs SET stream_version = $2, last_receipt_id = $3, updated_at = now()
				 WHERE id = $1 AND space_id = $4`, cur.id, stream, r.ID, sp.ID); err != nil {
				return fmt.Errorf("ledger: forget: Brief: %w", err)
			}
		}
		structure, err := json.Marshal(v.st)
		if err != nil {
			return err
		}
		title, summary := v.title, any(nullText(v.summary))
		if wipe {
			title, summary = "Brief", nil
		}
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.brief_versions SET structure = $2, title = $3, summary = $4, last_receipt_id = $5
			 WHERE id = $1 AND space_id = $6`, v.id, structure, title, summary, rc.ID, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget: Brief version %d: %w", v.version, err)
		}
	}
	return nil
}

// purgeObservations takes the forgotten memories out of the drift
// evidence: every change that cites one, or quotes its words, leaves the
// changeset, with a purged receipt about the target. A space's Forget
// clears every changeset. (The observed files in object storage are the
// propagation job's: it removes the lines that cite them.)
func (w *writer) purgeObservations(ctx context.Context, sp spaceRow, op *forgetOp) error {
	rows, err := w.tx.Query(ctx, `
		SELECT o.id, o.target_id, o.changeset FROM v2.target_observations o
		 WHERE o.space_id = $1 AND jsonb_array_length(o.changeset -> 'changes') > 0
		 ORDER BY o.target_id, o.id`, sp.ID)
	if err != nil {
		return fmt.Errorf("ledger: forget: drift evidence: %w", err)
	}
	type obs struct {
		id, target uuid.UUID
		cs         ChangeSet
	}
	var all []obs
	for rows.Next() {
		var o obs
		var raw []byte
		if err := rows.Scan(&o.id, &o.target, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: forget: drift evidence: %w", err)
		}
		if err := json.Unmarshal(raw, &o.cs); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: forget: drift evidence: %w", err)
		}
		all = append(all, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: forget: drift evidence: %w", err)
	}
	wipe := op.kind == ObjectSpace
	quoted := quoteFinder(op.words)
	receipts := map[uuid.UUID]uuid.UUID{}
	for _, o := range all {
		kept := make([]DriftChange, 0, len(o.cs.Changes))
		for _, ch := range o.cs.Changes {
			if wipe || op.has(ch.Ref) || slices.ContainsFunc(ch.Refs, op.has) || slices.ContainsFunc(ch.Cites, op.has) ||
				quoted(ch.OldText) || quoted(ch.NewText) || quoted(ch.Text) {
				continue
			}
			kept = append(kept, ch)
		}
		if len(kept) == len(o.cs.Changes) {
			continue
		}
		rid, ok := receipts[o.target]
		if !ok {
			t, err := loadTarget(ctx, w.tx, w.meta.Scope, o.target, true)
			if err != nil {
				return err
			}
			rc := w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), ActionPurged, t.Version+1, "")
			rc.Source = &ReceiptSource{Kind: op.kind, Ref: op.primary}
			if err := insertReceipt(ctx, w.tx, &rc); err != nil {
				return err
			}
			op.receipts = append(op.receipts, rc)
			if _, err := w.tx.Exec(ctx, `
				UPDATE v2.targets SET stream_version = $2, last_receipt_id = $3, updated_at = now()
				 WHERE id = $1 AND space_id = $4`, t.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
				return fmt.Errorf("ledger: forget: target: %w", err)
			}
			rid = rc.ID
			receipts[o.target] = rid
		}
		o.cs.Changes = kept
		raw, err := json.Marshal(o.cs)
		if err != nil {
			return err
		}
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.target_observations SET changeset = $2, last_receipt_id = $3 WHERE id = $1 AND space_id = $4`,
			o.id, raw, rid, sp.ID); err != nil {
			return fmt.Errorf("ledger: forget: drift evidence: %w", err)
		}
	}
	return nil
}

// quoteFinder reports whether a text quotes any of the forgotten words,
// compared as the judge normalises them (case, accents, spacing).
func quoteFinder(words []string) func(string) bool {
	norm := make([]string, 0, len(words))
	for _, w := range words {
		if n := textsig.Normalize(w); utf8.RuneCountInString(n) >= 8 {
			norm = append(norm, n)
		}
	}
	return func(s string) bool {
		if s == "" || len(norm) == 0 {
			return false
		}
		n := textsig.Normalize(s)
		for _, w := range norm {
			if strings.Contains(n, w) || (utf8.RuneCountInString(n) >= 8 && strings.Contains(w, n)) {
				return true
			}
		}
		return false
	}
}

// queuePropagation writes the op's steps: each target that held a
// forgotten memory (every target, for a space), then the artifacts, the
// caches and the forget ledger's copy. Bookkeeping; no receipts.
func (w *writer) queuePropagation(ctx context.Context, sp spaceRow, op *forgetOp, files []uuid.UUID) error {
	if !op.retire {
		rows, err := w.tx.Query(ctx, `
			SELECT id, kind, COALESCE(path, ''), delivery, sync_state, dirty_gen FROM v2.targets
			 WHERE space_id = $1 AND id = ANY ($2) ORDER BY created_at, id`, sp.ID, files)
		if err != nil {
			return fmt.Errorf("ledger: forget: targets: %w", err)
		}
		type target struct {
			id       uuid.UUID
			kind     TargetKind
			path     string
			delivery Delivery
			state    SyncState
			gen      int64
		}
		targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (target, error) {
			var t target
			err := r.Scan(&t.id, &t.kind, &t.path, &t.delivery, &t.state, &t.gen)
			return t, err
		})
		if err != nil {
			return fmt.Errorf("ledger: forget: targets: %w", err)
		}
		for _, t := range targets {
			status := "pending"
			if t.state == SyncOff {
				status = "stopped"
			}
			detail, err := json.Marshal(map[string]any{"generation": t.gen, "kind": t.kind, "delivery": t.delivery})
			if err != nil {
				return err
			}
			if err := w.insertStep(ctx, sp, op, "target", &t.id, targetLabel(t.kind, t.path), status, detail); err != nil {
				return err
			}
		}
	}
	detail := []byte(`{}`)
	if op.retire {
		// The rows go with the space, so the keys go into the step.
		rows, err := w.tx.Query(ctx, `
			SELECT artifact_key FROM v2.compile_runs WHERE space_id = $1 AND artifact_key IS NOT NULL
			UNION
			SELECT artifact_key FROM v2.target_observations WHERE space_id = $1`, sp.ID)
		if err != nil {
			return fmt.Errorf("ledger: forget: artifacts: %w", err)
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("ledger: forget: artifacts: %w", err)
		}
		slices.Sort(keys)
		if detail, err = json.Marshal(map[string]any{"delete": keys}); err != nil {
			return err
		}
	}
	for _, s := range []struct {
		kind   string
		detail []byte
	}{{"artifacts", detail}, {"caches", []byte(`{}`)}, {"ledger", []byte(`{}`)}} {
		if err := w.insertStep(ctx, sp, op, s.kind, nil, "", "pending", s.detail); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) insertStep(ctx context.Context, sp spaceRow, op *forgetOp, kind string, dest *uuid.UUID, label, status string, detail []byte) error {
	var labelArg any
	if label != "" {
		labelArg = label
	}
	var done any
	if status == "stopped" {
		done = w.now
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.propagations (id, op_id, tenant_id, space_id, destination_kind, destination_id, label, status, detail, done_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		newID(), op.id, sp.TenantID, sp.ID, kind, dest, labelArg, status, detail, done); err != nil {
		return fmt.Errorf("ledger: forget: step %s: %w", kind, err)
	}
	return nil
}

// queueNotices tells every agent connection that read a forgotten memory
// (directly, or in a compile that held it), and every connection connected
// to the space, once, on its next MCP response.
func (w *writer) queueNotices(ctx context.Context, sp spaceRow, op *forgetOp) error {
	// A space forgotten again (its gates' words, say) may forget no memory.
	kind, refs := NoticeForgotten, append([]string{}, op.refs...)
	if op.kind == ObjectSpace {
		kind = NoticeSpaceForgotten
		if len(refs) > 1000 {
			refs = refs[:1000]
		}
	}
	_, err := w.tx.Exec(ctx, `
		WITH readers AS (
		    SELECT r.reader_key AS connection_id, r.person_id, max(NULLIF(r.agent, '')) AS agent, true AS read_it
		      FROM v2.read_rollups r
		     WHERE r.space_id = $1 AND r.reader_kind = 'agent'
		       AND (r.subject_id = ANY ($2) OR r.subject_id IN (
		           SELECT c.id FROM v2.compile_runs c WHERE c.space_id = $1 AND c.refs && $3))
		     GROUP BY r.reader_key, r.person_id
		    UNION ALL
		    SELECT s.connection_id, s.person_id, NULL, false
		      FROM v2.agent_connection_spaces s WHERE s.space_id = $1
		), each AS (
		    SELECT connection_id, min(person_id::text)::uuid AS person_id, max(agent) AS agent, bool_or(read_it) AS read_it
		      FROM readers GROUP BY connection_id
		)
		INSERT INTO v2.agent_notices (id, tenant_id, space_id, connection_id, person_id, op_id, kind, refs, read_it)
		SELECT gen_random_uuid(), $4, $1, e.connection_id, e.person_id, $5, $6, $7, e.read_it FROM each e
		ON CONFLICT (connection_id, op_id) DO NOTHING`,
		sp.ID, op.ids, op.refs, sp.TenantID, op.id, kind, refs)
	if err != nil {
		return fmt.Errorf("ledger: forget: notices: %w", err)
	}
	return nil
}
