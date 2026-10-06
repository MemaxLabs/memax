package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
)

// writer applies one command inside its transaction.
type writer struct {
	tx      pgx.Tx
	meta    *Meta
	command CommandName
	hash    []byte
	space   uuid.UUID // the space the idempotency key was claimed in

	// The follow-up jobs the command queued (jobs.go), and how to insert
	// them: River, as the role the transaction began with.
	jobs      []river.InsertManyParams
	finishers []Finisher
	inserter  Jobs
	loginRole string
	// indexJobs queues index_memory for every new version (embeddings.go).
	indexJobs bool

	// undo collects the command's inverse when it is undoable (undo.go),
	// with the windows the ledger was configured with.
	undo            *undoJournal
	undoWindow      time.Duration
	judgeUndoWindow time.Duration

	// now is the ledger's clock when the command began: when a gate
	// expires, and whether it has.
	now time.Time
}

// write is Remember and Propose.
func (w *writer) write(ctx context.Context, nm NewMemory, propose bool) (Result, error) {
	grant, ok := w.meta.Scope.Grant(nm.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, nm.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		return deref(replay), err
	}

	actorTrust := policy.ActorTrust(w.meta.Actor.Kind, w.meta.Via)
	srcs := resolveSources(nm.Sources, actorTrust)
	if srcs, err = w.resolveMemorySources(ctx, sp.ID, srcs, actorTrust); err != nil {
		return Result{}, err
	}
	trusts := []policy.Trust{actorTrust}
	texts := []string{nm.Statement, w.meta.Reason}
	for _, s := range srcs {
		trusts = append(trusts, s.trust)
		texts = append(texts, s.Quote)
	}
	trust := policy.MinTrust(trusts...)

	action := policy.ActionRemember
	if propose {
		action = policy.ActionPropose
	}
	obj := policy.Object{
		Decision:            nm.Kind == KindDecision,
		External:            trust.External(),
		ContradictsDecision: nm.ContradictsDecision,
		Secrets:             findSecrets(texts...),
	}
	dec := policy.Decide(w.policyActor(grant), action, obj, sp.policy())
	// A Write-level agent's statement is kept at once, unless it touches a
	// decision in force: then it waits for the judge and a person (rule 11;
	// the judge itself takes seconds, which a write can't wait for).
	if (dec.Effect == policy.EffectApply || dec.Effect == policy.EffectConfirm) && w.meta.Actor.Kind == policy.ActorAgent {
		area := ""
		if nm.Decision != nil {
			area = nm.Decision.Area
		}
		if obj.TouchesDecision, err = w.touchesDecision(ctx, sp.ID, uuid.Nil, nm.Statement, area); err != nil {
			return Result{}, err
		}
		if obj.TouchesDecision {
			dec = policy.Decide(w.policyActor(grant), action, obj, sp.policy())
		}
	}
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}

	verb := lifecycle.VerbPropose
	if dec.Effect == policy.EffectApply {
		verb = lifecycle.VerbKeep
	}
	state, err := lifecycle.Transition(lifecycle.State{}, verb)
	if err != nil {
		return Result{}, err
	}
	var primary *ReceiptSource
	if len(srcs) > 0 {
		primary = &ReceiptSource{Kind: string(srcs[0].Kind), Ref: srcs[0].Ref}
	}
	id, rc, err := w.insertMemory(ctx, sp, grant, memoryRow{
		Statement: nm.Statement, Section: nm.Section, Kind: nm.Kind, Decision: nm.Decision, Trust: trust,
		StaleAfter: nm.StaleAfter, Conditions: nm.Conditions, Applies: nm.Applies,
		ValidFrom: nm.ValidFrom, ValidTo: nm.ValidTo,
	}, state, primary)
	if err != nil {
		return Result{}, err
	}
	if err := w.insertSources(ctx, sp.ID, id, rc.ID, srcs); err != nil {
		return Result{}, err
	}
	if state.Lifecycle == lifecycle.Kept {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	return w.finish(ctx, Result{Outcome: outcomeFor(dec.Effect), Policy: dec, Receipts: []Receipt{rc}}, id)
}

// review is Keep and Reject.
func (w *writer) review(ctx context.Context, ref string, expected int, cmd CommandName) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, ref)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	if expected != 0 && expected != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: expected, Current: mem.Version}
	}
	action, verb, receiptAction := policy.ActionKeep, lifecycle.VerbKeep, ActionKept
	if cmd == CommandReject {
		action, verb, receiptAction = policy.ActionReject, lifecycle.VerbReject, ActionRejected
	}
	dec := policy.Decide(w.policyActor(grant), action, w.object(mem, false), sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if cmd == CommandKeep {
		// A flagged proposal is settled, not kept: say so, and name the
		// decision in the way, rather than a bare invalid transition.
		if mem.Lifecycle == lifecycle.Proposed && mem.Flags.Has(lifecycle.Conflict) {
			return Result{}, w.inConflict(ctx, mem)
		}
		if err := w.awaitJudge(ctx, mem); err != nil {
			return Result{}, err
		}
	}
	next, err := transition(mem, verb)
	if err != nil {
		return Result{}, err
	}
	if w.meta.Actor.Kind == policy.ActorPerson {
		kind := UndoKeep
		if cmd == CommandReject {
			kind = UndoReject
		}
		w.startUndo(kind, w.undoWindow)
		w.undo.touch(mem)
	}
	rc, err := w.changeState(ctx, sp, grant, mem, next, receiptAction, mem.streamVersion+1, w.meta.Reason)
	if err != nil {
		return Result{}, err
	}
	receipts := []Receipt{rc}
	// A Keep changes the kept set, so every target recompiles. A Reject
	// takes a proposal out of Review, and proposals never compile.
	if next.Lifecycle == lifecycle.Kept {
		mem.Lifecycle, mem.Flags, mem.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
		if receipts, err = w.supersedeOnKeep(ctx, sp, mem, receipts); err != nil {
			return Result{}, err
		}
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	if err := w.writeUndo(ctx, sp, receipts); err != nil {
		return Result{}, err
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, mem.ID)
}

// supersedeOnKeep: keeping a memory that supersedes a decision in force
// (an explicit change the judge linked, or an edit sent to Review)
// supersedes that decision (TEPA's keyed update). The decision stays kept,
// with its history, and stops compiling.
func (w *writer) supersedeOnKeep(ctx context.Context, sp spaceRow, kept *Memory, receipts []Receipt) ([]Receipt, error) {
	links, err := activeLinks(ctx, w.tx, []uuid.UUID{kept.ID})
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for _, l := range links[kept.ID] {
		if l.Kind == LinkSupersedes && l.Direction == LinkOut {
			ids = append(ids, l.MemoryID)
		}
	}
	if len(ids) == 0 {
		return receipts, nil
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR UPDATE")
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		d := locked[id]
		if d == nil || !d.inForce() {
			continue
		}
		rc, err := w.supersedeDecision(ctx, sp, d, kept)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, rc)
	}
	return receipts, nil
}

// edit writes a new version, or, when policy downgrades it, a new
// proposal that supersedes the memory.
func (w *writer) edit(ctx context.Context, c *Edit) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	if c.ExpectedVersion != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: c.ExpectedVersion, Current: mem.Version}
	}
	statement := c.Statement
	section := c.Section
	if section == "" {
		section = mem.Section
	}
	if statement == mem.Statement && section == mem.Section {
		return Result{}, invalid("statement", "is unchanged; there is nothing to edit")
	}

	obj := w.object(mem, c.ContradictsDecision)
	if obj.PersonKept, err = w.personKept(ctx, mem.ID); err != nil {
		return Result{}, err
	}
	obj.Secrets = findSecrets(statement, w.meta.Reason)
	pa := w.policyActor(grant)
	dec := policy.Decide(pa, policy.ActionEdit, obj, sp.policy())
	actorTrust := policy.ActorTrust(w.meta.Actor.Kind, w.meta.Via)
	trust := policy.MinTrust(mem.Trust, actorTrust)

	if dec.Effect == policy.EffectApply && w.meta.Actor.Kind == policy.ActorAgent {
		if obj.TouchesDecision, err = w.touchesDecision(ctx, sp.ID, mem.ID, statement, mem.area()); err != nil {
			return Result{}, err
		}
		if obj.TouchesDecision {
			dec = policy.Decide(pa, policy.ActionEdit, obj, sp.policy())
		}
	}
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	// Only kept memories and proposals can change, in place or by a
	// superseding proposal.
	next, err := transition(mem, lifecycle.VerbEdit)
	if err != nil {
		return Result{}, err
	}
	if dec.Effect != policy.EffectApply {
		return w.supersede(ctx, sp, grant, mem, dec, memoryRow{
			Statement: statement, Section: section, Kind: mem.Kind, Decision: mem.Decision, Trust: trust,
			StaleAfter: mem.StaleAfter, Conditions: mem.Conditions, Applies: mem.Applies,
			ValidFrom: mem.ValidFrom, ValidTo: mem.ValidTo,
		})
	}

	keep := c.Keep && mem.Lifecycle == lifecycle.Proposed
	if keep {
		obj.Secrets = nil
		if kd := policy.Decide(pa, policy.ActionKeep, obj, sp.policy()); kd.Effect == policy.EffectRefuse {
			return refused(kd), nil
		}
		if mem.Flags.Has(lifecycle.Conflict) {
			return Result{}, w.inConflict(ctx, mem)
		}
	}
	// Rule 11 for "edit, then keep": new words that touch a decision in
	// force are saved as the proposal's new version and judged like any
	// proposal's, but not kept until the judge has looked (Keep waits for
	// it, 503 judge_pending). Refusing the whole edit instead would roll
	// the words back, so the judge would never see them.
	var held bool
	if keep {
		if held, err = w.holdEditForJudge(ctx, mem, statement); err != nil {
			return Result{}, err
		}
		keep = !held
	}
	var kept lifecycle.State
	if keep {
		if kept, err = transition(&Memory{Ref: mem.Ref, Lifecycle: next.Lifecycle, Flags: next.Flags}, lifecycle.VerbKeep); err != nil {
			return Result{}, err
		}
	}

	if w.meta.Actor.Kind == policy.ActorPerson {
		w.startUndo(UndoEdit, w.undoWindow)
		w.undo.touch(mem)
	}
	rc := w.receipt(sp, mem.ID, mem.Ref, ActionEdited, mem.streamVersion+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	// Undo can move current_version back, so the next version counts from
	// the highest stored.
	version, err := w.nextVersion(ctx, mem.ID)
	if err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $5)`, mem.ID, version, sp.ID, statement, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write version: %w", err)
	}
	w.indexVersion(sp.ID, mem.ID, version)
	hash, bands := signature(statement)
	if _, err := w.tx.Exec(ctx, fmt.Sprintf(`
		UPDATE v2.memories
		   SET section = $2, current_version = $3, trust = $4, search = %s, content_sha256 = $11, minhash_bands = $12,
		       lifecycle = $6, flags = $7, stream_version = $8, last_receipt_id = $9, updated_at = now()
		 WHERE id = $1 AND space_id = $10`, fmt.Sprintf(searchExpr, "$5::text")),
		mem.ID, string(section), version, string(trust), statement,
		string(next.Lifecycle), next.Flags.Strings(), rc.StreamVersion, rc.ID, sp.ID, hash, bands); err != nil {
		return Result{}, fmt.Errorf("ledger: update memory: %w", err)
	}
	receipts := []Receipt{rc}
	wasKept := mem.Lifecycle == lifecycle.Kept
	mem.Lifecycle, mem.Flags, mem.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
	if keep {
		krc, err := w.changeState(ctx, sp, grant, mem, kept, ActionKept, rc.StreamVersion+1, "")
		if err != nil {
			return Result{}, err
		}
		receipts = append(receipts, krc)
		mem.Lifecycle, mem.Flags, mem.streamVersion = kept.Lifecycle, kept.Flags, krc.StreamVersion
		if receipts, err = w.supersedeOnKeep(ctx, sp, mem, receipts); err != nil {
			return Result{}, err
		}
	} else {
		// New words for a proposal (or for an agent's kept memory) are
		// judged again.
		w.judgeAfterWrite(sp.ID, mem.ID, version, next.Lifecycle)
	}
	// New words for a kept memory, or a proposal edited and kept, change
	// what compiles.
	if wasKept || keep {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	if err := w.writeUndo(ctx, sp, receipts); err != nil {
		return Result{}, err
	}
	if held {
		// The edit is applied (and undoable as an edit); the Keep waits.
		return w.finish(ctx, Result{Outcome: OutcomeProposed, Policy: heldForJudge(mem.Ref), Receipts: receipts}, mem.ID)
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, mem.ID)
}

// supersede writes a downgraded edit as a new proposal linked to the
// memory it would change ("Updates M-0156" in Review). The original is
// untouched.
func (w *writer) supersede(ctx context.Context, sp spaceRow, grant SpaceGrant, old *Memory, dec policy.Decision, row memoryRow) (Result, error) {
	state, err := lifecycle.Transition(lifecycle.State{}, lifecycle.VerbPropose)
	if err != nil {
		return Result{}, err
	}
	id, rc, err := w.insertMemory(ctx, sp, grant, row, state, &ReceiptSource{Kind: ObjectMemory, Ref: old.Ref})
	if err != nil {
		return Result{}, err
	}
	if _, err := w.insertLink(ctx, sp.ID, LinkSupersedes, id, old.ID, rc.ID); err != nil {
		return Result{}, err
	}
	return w.finish(ctx, Result{Outcome: outcomeFor(dec.Effect), Policy: dec, Receipts: []Receipt{rc}}, id)
}

// open loads and locks the target memory, then claims the idempotency
// key in its space (returning the original result on a replay).
func (w *writer) open(ctx context.Context, ref string) (*Memory, SpaceGrant, spaceRow, *Result, error) {
	id, err := resolveRef(ctx, w.tx, w.meta.Scope, ref)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	mem, err := loadMemory(ctx, w.tx, w.meta.Scope, id, true)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	grant, ok := w.meta.Scope.Grant(mem.SpaceID)
	if !ok {
		return nil, SpaceGrant{}, spaceRow{}, nil, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, mem.SpaceID)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	replay, err := w.claim(ctx, sp.ID)
	return mem, grant, sp, replay, err
}

type memoryRow struct {
	Statement  string
	Section    Section
	Kind       Kind
	Decision   *DecisionFields
	Trust      policy.Trust
	StaleAfter *time.Time
	Conditions json.RawMessage
	Applies    json.RawMessage
	ValidFrom  *time.Time
	ValidTo    *time.Time
}

// insertMemory allocates the display ID and writes the creating receipt,
// the memory row and its first version.
func (w *writer) insertMemory(ctx context.Context, sp spaceRow, grant SpaceGrant, row memoryRow, state lifecycle.State, src *ReceiptSource) (uuid.UUID, Receipt, error) {
	seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixMemory)
	if err != nil {
		return uuid.Nil, Receipt{}, err
	}
	id := newID()
	ref := FormatRef(PrefixMemory, seq)
	action := ActionProposed
	if state.Lifecycle == lifecycle.Kept {
		action = ActionKept
	}
	rc := w.receipt(sp, id, ref, action, 1, w.meta.Reason)
	rc.Source = src
	if action == ActionKept {
		rc.Assurance = w.policyActor(grant).Assurance()
	}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return uuid.Nil, Receipt{}, err
	}
	var decision any
	if row.Decision != nil {
		b, err := json.Marshal(row.Decision)
		if err != nil {
			return uuid.Nil, Receipt{}, err
		}
		decision = b
	}
	hash, bands := signature(row.Statement)
	if _, err := w.tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO v2.memories (id, tenant_id, space_id, seq, section, kind, lifecycle, flags, trust,
		                         current_version, stream_version, stale_after, conditions, decision, scope,
		                         valid_from, valid_to, search, content_sha256, minhash_bands, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, 1, $10, $11, $12, $13, $14, $15, %s, $18, $19, $17, $17)`,
		fmt.Sprintf(searchExpr, "$16::text")),
		id, sp.TenantID, sp.ID, seq, string(row.Section), string(row.Kind), string(state.Lifecycle),
		state.Flags.Strings(), string(row.Trust), row.StaleAfter, []byte(row.Conditions), decision,
		[]byte(row.Applies), row.ValidFrom, row.ValidTo, row.Statement, rc.ID, hash, bands); err != nil {
		return uuid.Nil, Receipt{}, fmt.Errorf("ledger: write memory: %w", err)
	}
	w.judgeAfterWrite(sp.ID, id, 1, state.Lifecycle)
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
		VALUES ($1, 1, $2, $3, $4, $4)`, id, sp.ID, row.Statement, rc.ID); err != nil {
		return uuid.Nil, Receipt{}, fmt.Errorf("ledger: write version: %w", err)
	}
	w.indexVersion(sp.ID, id, 1)
	return id, rc, nil
}

type resolvedSource struct {
	SourceInput
	trust policy.Trust
}

// resolveSources settles each source's trust: the caller's class or the
// kind's default, never above what the actor could write itself, and
// always external for URLs, email and issues.
func resolveSources(in []SourceInput, actorTrust policy.Trust) []resolvedSource {
	out := make([]resolvedSource, 0, len(in))
	for _, s := range in {
		t := s.Trust
		if t == "" {
			t = s.Kind.defaultTrust(actorTrust)
		}
		if s.Kind.alwaysExternal() {
			t = policy.TrustExternal
		}
		out = append(out, resolvedSource{SourceInput: s, trust: policy.MinTrust(t, actorTrust)})
	}
	return out
}

func (w *writer) insertSources(ctx context.Context, spaceID, memoryID, receiptID uuid.UUID, srcs []resolvedSource) error {
	if len(srcs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, s := range srcs {
		id := newID()
		b.Queue(`
			INSERT INTO v2.sources (id, space_id, kind, uri, ref, locator, external, trust_class, quote, content_hash,
			                        created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`,
			id, spaceID, string(s.Kind), nullText(s.URI), s.Ref, []byte(s.Locator), s.trust == policy.TrustExternal,
			string(s.trust), nullText(s.Quote), nullText(s.ContentHash), receiptID)
		b.Queue(`INSERT INTO v2.memory_sources (memory_id, source_id, space_id, receipt_id) VALUES ($1, $2, $3, $4)`,
			memoryID, id, spaceID, receiptID)
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("ledger: write sources: %w", err)
	}
	return nil
}

// changeState writes a receipt and moves the memory to next.
func (w *writer) changeState(ctx context.Context, sp spaceRow, grant SpaceGrant, mem *Memory, next lifecycle.State, action Action, version int, reason string) (Receipt, error) {
	rc := w.receipt(sp, mem.ID, mem.Ref, action, version, reason)
	if action == ActionKept {
		rc.Assurance = w.policyActor(grant).Assurance()
	}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Receipt{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET lifecycle = $2, flags = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $6`,
		mem.ID, string(next.Lifecycle), next.Flags.Strings(), version, rc.ID, sp.ID); err != nil {
		return Receipt{}, fmt.Errorf("ledger: update memory: %w", err)
	}
	return rc, nil
}

func (w *writer) receipt(sp spaceRow, objectID uuid.UUID, ref string, action Action, version int, reason string) Receipt {
	return w.objectReceipt(sp, ObjectMemory, objectID, ref, action, version, reason)
}

// objectReceipt is a receipt by the command's actor about any object.
func (w *writer) objectReceipt(sp spaceRow, kind string, objectID uuid.UUID, ref string, action Action, version int, reason string) Receipt {
	a := w.meta.Actor
	rc := Receipt{
		ID: newID(), TenantID: sp.TenantID, SpaceID: sp.ID,
		ObjectKind: kind, ObjectID: objectID, ObjectRef: ref, Action: action,
		ActorKind: a.Kind, Agent: a.Agent, Via: w.meta.Via, SessionRef: w.meta.SessionRef, Reason: reason,
		OccurredAt: w.meta.OccurredAt, StreamID: objectID, StreamVersion: version,
	}
	if a.ID != uuid.Nil {
		id := a.ID
		rc.ActorID = &id
	}
	return rc
}

// policyActor is the actor as policy sees it in one space: with the
// person's role there and, for an agent, its connection's autonomy and
// status there (SpaceGrant, from WithConnection), falling back to
// Actor.Autonomy.
func (w *writer) policyActor(g SpaceGrant) policy.Actor {
	return toPolicyActor(w.meta.Actor, w.meta.Via, g)
}

// toPolicyActor is an actor as policy sees it in one space (policyActor).
func toPolicyActor(a Actor, via policy.Via, g SpaceGrant) policy.Actor {
	autonomy := a.Autonomy
	if g.Autonomy != "" {
		autonomy = g.Autonomy
	}
	return policy.Actor{
		Kind: a.Kind, Name: a.Name, Role: g.Role, CanForget: g.CanForget,
		Autonomy: autonomy, AgentStatus: g.AgentStatus, Credential: a.Credential, Via: via,
		PersonPresent: a.PersonPresent, CanElicit: a.CanElicit,
	}
}

func (w *writer) object(mem *Memory, contradicts bool) policy.Object {
	return policy.Object{
		Ref: mem.Ref, Lifecycle: mem.Lifecycle, Decision: mem.Kind == KindDecision,
		External: mem.Trust.External(), ContradictsDecision: contradicts,
	}
}

// personKept reports whether a person ever kept or edited the memory.
func (w *writer) personKept(ctx context.Context, id uuid.UUID) (bool, error) {
	var yes bool
	err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.receipts
		                WHERE stream_id = $1 AND actor_kind = 'person' AND action IN ('kept', 'edited'))`, id).Scan(&yes)
	if err != nil {
		return false, fmt.Errorf("ledger: read history: %w", err)
	}
	return yes, nil
}

// claim takes the command's idempotency key in a space. A key another
// transaction is applying makes this one wait; once that commits, the
// key is taken and claim returns the original result (Replayed). A key
// reused for different content is ErrIdempotencyKeyReused.
func (w *writer) claim(ctx context.Context, spaceID uuid.UUID) (*Result, error) {
	c, err := w.claimKey(ctx, spaceID)
	if err != nil || c == nil {
		return nil, err
	}
	res, err := c.result(ctx, w)
	if err != nil {
		return nil, err
	}
	if c.objectID != nil && isAgentCommand(w.command) {
		if res.Connection, err = loadConnection(ctx, w.tx, w.meta.Scope, *c.objectID); err != nil {
			return nil, err
		}
		return &res, nil
	}
	if c.objectID != nil {
		if res.Memory, err = loadMemory(ctx, w.tx, w.meta.Scope, *c.objectID, false); err != nil {
			return nil, err
		}
		if res.Memory.Sources, err = loadSources(ctx, w.tx, *c.objectID); err != nil {
			return nil, err
		}
		if err := attachDetails(ctx, w.tx, []*Memory{res.Memory}); err != nil {
			return nil, err
		}
	}
	return &res, nil
}

// claimed is an idempotency key that was already applied.
type claimed struct {
	outcome    Outcome
	policy     []byte
	objectID   *uuid.UUID
	receiptIDs []uuid.UUID
}

// result is the replayed result: the original outcome, policy and
// receipts. The caller loads the object's projection.
func (c *claimed) result(ctx context.Context, w *writer) (Result, error) {
	res := Result{Outcome: c.outcome, Replayed: true}
	if err := json.Unmarshal(c.policy, &res.Policy); err != nil {
		return Result{}, fmt.Errorf("ledger: read idempotency key: %w", err)
	}
	var err error
	if res.Receipts, err = loadReceipts(ctx, w.tx, w.meta.Scope, c.receiptIDs); err != nil {
		return Result{}, err
	}
	return res, nil
}

// claimKey takes the key, returning nil, or the record of its earlier
// application.
func (w *writer) claimKey(ctx context.Context, spaceID uuid.UUID) (*claimed, error) {
	w.space = spaceID
	actorID := w.actorID()
	tag, err := w.tx.Exec(ctx, `
		INSERT INTO v2.command_keys (space_id, actor_kind, actor_id, idempotency_key, command, request_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT ON CONSTRAINT command_keys_key DO NOTHING`,
		spaceID, string(w.meta.Actor.Kind), actorID, w.meta.IdempotencyKey, string(w.command), w.hash)
	if err != nil {
		return nil, fmt.Errorf("ledger: claim idempotency key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}

	var command string
	var hash []byte
	var outcome *string
	c := &claimed{}
	err = w.tx.QueryRow(ctx, `
		SELECT command, request_hash, outcome, policy, object_id, receipt_ids
		  FROM v2.command_keys
		 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
		spaceID, string(w.meta.Actor.Kind), actorID, w.meta.IdempotencyKey,
	).Scan(&command, &hash, &outcome, &c.policy, &c.objectID, &c.receiptIDs)
	if err != nil {
		return nil, fmt.Errorf("ledger: read idempotency key: %w", err)
	}
	if command != string(w.command) || !bytes.Equal(hash, w.hash) {
		return nil, fmt.Errorf("%w: key %q was used for another %s; send a new key for a new command",
			ErrIdempotencyKeyReused, w.meta.IdempotencyKey, command)
	}
	if outcome == nil {
		return nil, errors.New("ledger: idempotency record without an outcome")
	}
	c.outcome = Outcome(*outcome)
	return c, nil
}

// record stores the result against the idempotency key.
func (w *writer) record(ctx context.Context, res Result, objectID uuid.UUID) error {
	ids := make([]uuid.UUID, 0, len(res.Receipts))
	for _, rc := range res.Receipts {
		ids = append(ids, rc.ID)
	}
	policyJSON, err := json.Marshal(res.Policy)
	if err != nil {
		return err
	}
	var object *uuid.UUID
	if objectID != uuid.Nil {
		object = &objectID
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.command_keys SET outcome = $5, policy = $6, object_id = $7, receipt_ids = $8
		 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
		w.space, string(w.meta.Actor.Kind), w.actorID(), w.meta.IdempotencyKey,
		string(res.Outcome), policyJSON, object, ids); err != nil {
		return fmt.Errorf("ledger: record idempotency key: %w", err)
	}
	return nil
}

// finish records the result against the idempotency key and loads the
// memory's new projection.
func (w *writer) finish(ctx context.Context, res Result, memoryID uuid.UUID) (Result, error) {
	if err := w.record(ctx, res, memoryID); err != nil {
		return Result{}, err
	}
	var err error
	if res.Memory, err = loadMemory(ctx, w.tx, w.meta.Scope, memoryID, false); err != nil {
		return Result{}, err
	}
	if res.Memory.Sources, err = loadSources(ctx, w.tx, memoryID); err != nil {
		return Result{}, err
	}
	if err := attachDetails(ctx, w.tx, []*Memory{res.Memory}); err != nil {
		return Result{}, err
	}
	return res, nil
}

func (w *writer) actorID() *uuid.UUID {
	if w.meta.Actor.ID == uuid.Nil {
		return nil
	}
	id := w.meta.Actor.ID
	return &id
}

func transition(mem *Memory, v lifecycle.Verb) (lifecycle.State, error) {
	next, err := lifecycle.Transition(mem.state(), v)
	var te *lifecycle.TransitionError
	if errors.As(err, &te) {
		return lifecycle.State{}, &TransitionError{Ref: mem.Ref, Err: te}
	}
	return next, err
}

func refused(d policy.Decision) Result { return Result{Outcome: OutcomeRefused, Policy: d} }

func deref(r *Result) Result {
	if r == nil {
		return Result{}
	}
	return *r
}

// findSecrets lists the credential patterns found in any of the texts.
func findSecrets(texts ...string) []string {
	var found []string
	for _, t := range texts {
		for _, name := range secrets.DetectCredentials(t) {
			if !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
	}
	return found
}

// newID returns a uuidv7: time-ordered, so receipts and memories index
// well. NewV7 fails only if the system's random source does.
func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
