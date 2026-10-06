package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The gate commands. Each one claims its idempotency key, asks policy,
// checks the gate's status, and writes its receipts before the
// projections; the database refuses the commit otherwise (migration 036).
// A gate's receipts have object_kind "gate", its id as object_id and
// stream, and its display ID (G-0012) as object_ref.

// requestDecision is RequestDecision: a waiting G- gate, asked by an agent.
func (w *writer) requestDecision(ctx context.Context, c *RequestDecision) (Result, error) {
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
		return w.replayGate(ctx, replay, sp)
	}
	expires := w.now.Add(DefaultGateTTL)
	if c.ExpiresAt != nil {
		expires = c.ExpiresAt.Truncate(time.Microsecond)
		if expires.Before(w.now.Add(MinGateTTL)) || expires.After(w.now.Add(MaxGateTTL)) {
			return Result{}, invalid("expires_at", "must be between %d minutes and %d days from now",
				int(MinGateTTL.Minutes()), int(MaxGateTTL.Hours()/24))
		}
	}
	waiting, err := w.waitingGates(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	texts := []string{c.Question, c.Context, w.meta.Reason}
	for _, o := range c.Options {
		texts = append(texts, o.Label, o.Detail)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionRequestDecision,
		policy.Object{WaitingGates: waiting, Secrets: findSecrets(texts...)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixDecision)
	if err != nil {
		return Result{}, err
	}
	id := newID()
	rc := w.objectReceipt(sp, ObjectGate, id, FormatRef(PrefixDecision, seq), ActionAsked, 1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	options, err := json.Marshal(c.Options)
	if err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.decision_gates (id, tenant_id, space_id, seq, question, context, options, status, expires_at,
		                               asked_by, agent, requesting_session, stream_version, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'waiting', $8, $9, $10, $11, 1, $12, $12)`,
		id, sp.TenantID, sp.ID, seq, c.Question, nullText(c.Context), options, expires,
		w.meta.Actor.ID, nullText(w.meta.Actor.Agent), nullText(w.meta.SessionRef), rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write gate: %w", err)
	}
	return w.finishGate(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, id, sp)
}

// waitingGates counts the decisions the asking agent has waiting in the
// space, under a lock on that agent and space, so two questions asked at
// once can't both take the last place (policy.MaxWaitingGates).
func (w *writer) waitingGates(ctx context.Context, spaceID uuid.UUID) (int, error) {
	a := w.meta.Actor
	if a.Kind != policy.ActorAgent {
		return 0, nil // policy refuses anyone else
	}
	if _, err := w.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"memax.v2.gates:"+a.ID.String()+":"+spaceID.String()); err != nil {
		return 0, fmt.Errorf("ledger: lock the agent's gates: %w", err)
	}
	var n int
	if err := w.tx.QueryRow(ctx, `
		SELECT count(*) FROM v2.decision_gates
		 WHERE asked_by = $1 AND space_id = $2 AND status = 'waiting' AND expires_at > $3`,
		a.ID, spaceID, w.now).Scan(&n); err != nil {
		return 0, fmt.Errorf("ledger: count waiting gates: %w", err)
	}
	return n, nil
}

// answerGate is AnswerGate: the answer, kept as a decision the person
// authored, linked both ways to the gate, in one transaction.
func (w *writer) answerGate(ctx context.Context, c *AnswerGate) (Result, error) {
	g, grant, sp, replay, err := w.openGate(ctx, c.Gate)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayGate(ctx, replay, sp)
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != g.Version {
		return Result{}, &EditClashError{Ref: g.Ref, Expected: c.ExpectedVersion, Current: g.Version}
	}
	pa := w.policyActor(grant)
	dec := policy.Decide(pa, policy.ActionAnswerGate,
		policy.Object{Ref: g.Ref, Decision: true, Secrets: findSecrets(w.meta.Reason)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if err := g.waiting(CommandAnswerGate); err != nil {
		return Result{}, err
	}
	if c.Option < 1 || c.Option > len(g.Options) {
		return Result{}, invalid("option", "choose one of the %d options of %s, counting from 1", len(g.Options), g.Ref)
	}
	if !GateTransitionAllowed(g.stored, GateAnswered) {
		return Result{}, &GateStateError{Ref: g.Ref, Status: g.Status, Command: CommandAnswerGate}
	}

	// The decision. Its words are the agent's question and option, so it
	// carries the gate as a source of the agent's own work: a person
	// choosing between them doesn't raise their trust (plan §5.6). It is
	// a person's kept write, which the judge doesn't check (the plan's log:
	// a person's own remembers aren't judged), and like any Keep it
	// recompiles every target.
	kept, err := lifecycle.Transition(lifecycle.State{}, lifecycle.VerbKeep)
	if err != nil {
		return Result{}, err
	}
	locator, err := json.Marshal(map[string]string{"gate": g.ID.String(), "session_ref": g.SessionRef})
	if err != nil {
		return Result{}, err
	}
	actorTrust := policy.ActorTrust(w.meta.Actor.Kind, w.meta.Via)
	srcs := resolveSources([]SourceInput{{Kind: SourceSession, Ref: g.Ref, Locator: locator, Trust: policy.TrustAgentOwnWork}}, actorTrust)
	opt := g.Options[c.Option-1]
	memID, krc, err := w.insertMemory(ctx, sp, grant, memoryRow{
		Statement: gateStatement(g.Question, opt.Label), Section: SectionDecisions, Kind: KindDecision,
		Decision:   &DecisionFields{Why: g.Context, Options: g.Options, Status: DecisionInForce},
		Trust:      policy.MinTrust(actorTrust, srcs[0].trust),
		Conditions: json.RawMessage("[]"), Applies: json.RawMessage("{}"),
	}, kept, &ReceiptSource{Kind: ObjectGate, Ref: g.Ref})
	if err != nil {
		return Result{}, err
	}
	if err := w.insertSources(ctx, sp.ID, memID, krc.ID, srcs); err != nil {
		return Result{}, err
	}
	if err := w.markDirty(ctx, sp.ID); err != nil {
		return Result{}, err
	}

	rc := w.objectReceipt(sp, ObjectGate, g.ID, g.Ref, ActionAnswered, g.Version+1, w.meta.Reason)
	rc.Assurance = pa.Assurance()
	rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: krc.ObjectRef}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.decision_gates
		   SET status = 'answered', answer_option = $2, answer_memory_id = $3, answered_by = $4, answered_at = $5,
		       assurance = $6, stream_version = $7, last_receipt_id = $8, updated_at = now(),
		       delivered_at = CASE WHEN $9 THEN $10::timestamptz ELSE delivered_at END
		 WHERE id = $1 AND space_id = $11`,
		g.ID, c.Option, memID, w.meta.Actor.ID, w.meta.OccurredAt, string(rc.Assurance), rc.StreamVersion, rc.ID,
		c.Delivered, w.now, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: answer gate: %w", err)
	}
	return w.finishGate(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{krc, rc}}, g.ID, sp)
}

// withdrawGate is WithdrawGate.
func (w *writer) withdrawGate(ctx context.Context, c *WithdrawGate) (Result, error) {
	g, grant, sp, replay, err := w.openGate(ctx, c.Gate)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayGate(ctx, replay, sp)
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != g.Version {
		return Result{}, &EditClashError{Ref: g.Ref, Expected: c.ExpectedVersion, Current: g.Version}
	}
	mine, err := w.gateMine(ctx, g)
	if err != nil {
		return Result{}, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionWithdrawGate,
		policy.Object{Ref: g.Ref, GateMine: mine, Secrets: findSecrets(w.meta.Reason)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if err := g.waiting(CommandWithdrawGate); err != nil {
		return Result{}, err
	}
	if !GateTransitionAllowed(g.stored, GateWithdrawn) {
		return Result{}, &GateStateError{Ref: g.Ref, Status: g.Status, Command: CommandWithdrawGate}
	}
	rc := w.objectReceipt(sp, ObjectGate, g.ID, g.Ref, ActionWithdrawn, g.Version+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	// The agent that asked knows already when it withdrew the question.
	asker := w.meta.Actor.Kind == policy.ActorAgent && w.meta.Actor.ID == g.AskedBy
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.decision_gates
		   SET status = 'withdrawn', withdrawn_by_kind = $2, withdrawn_by = $3, withdrawn_at = $4,
		       stream_version = $5, last_receipt_id = $6, updated_at = now(),
		       delivered_at = CASE WHEN $7 THEN $8::timestamptz ELSE delivered_at END
		 WHERE id = $1 AND space_id = $9`,
		g.ID, string(w.meta.Actor.Kind), w.meta.Actor.ID, w.meta.OccurredAt, rc.StreamVersion, rc.ID,
		asker, w.now, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: withdraw gate: %w", err)
	}
	return w.finishGate(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, g.ID, sp)
}

// gateMine reports whether the actor asked the gate: the agent itself, or
// the person whose agent it is.
func (w *writer) gateMine(ctx context.Context, g *Gate) (bool, error) {
	a := w.meta.Actor
	switch a.Kind {
	case policy.ActorAgent:
		return a.ID == g.AskedBy, nil
	case policy.ActorPerson:
		var person uuid.UUID
		err := w.tx.QueryRow(ctx, `SELECT person_id FROM v2.agent_connections WHERE id = $1`, g.AskedBy).Scan(&person)
		if errNoRows(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("ledger: look up the asking agent: %w", err)
		}
		return person == a.ID, nil
	}
	return false, nil
}

// openGate loads and locks a gate, then claims the command's key in its
// space. A non-nil claimed means the key was already applied.
func (w *writer) openGate(ctx context.Context, ref string) (*Gate, SpaceGrant, spaceRow, *claimed, error) {
	id, err := resolveGateRef(ctx, w.tx, w.meta.Scope, ref)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	g, err := loadGate(ctx, w.tx, w.meta.Scope, id, true, w.now)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	grant, ok := w.meta.Scope.Grant(g.SpaceID)
	if !ok {
		return nil, SpaceGrant{}, spaceRow{}, nil, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, g.SpaceID)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	g.NeedsWeb = sp.Rules.DecisionsNeedPersonOnWeb(sp.Kind)
	c, err := w.claimKey(ctx, sp.ID)
	return g, grant, sp, c, err
}

// finishGate records the result against the idempotency key and loads the
// gate's new projection (and, for an answer, the decision it became).
func (w *writer) finishGate(ctx context.Context, res Result, gateID uuid.UUID, sp spaceRow) (Result, error) {
	if err := w.record(ctx, res, gateID); err != nil {
		return Result{}, err
	}
	return w.gateResult(ctx, res, gateID, sp)
}

// replayGate answers a replayed gate command: the original receipts, and
// the gate as it is now.
func (w *writer) replayGate(ctx context.Context, c *claimed, sp spaceRow) (Result, error) {
	res, err := c.result(ctx, w)
	if err != nil || c.objectID == nil {
		return res, err
	}
	return w.gateResult(ctx, res, *c.objectID, sp)
}

func (w *writer) gateResult(ctx context.Context, res Result, gateID uuid.UUID, sp spaceRow) (Result, error) {
	g, err := loadGate(ctx, w.tx, w.meta.Scope, gateID, false, w.now)
	if err != nil {
		return Result{}, err
	}
	g.NeedsWeb = sp.Rules.DecisionsNeedPersonOnWeb(sp.Kind)
	res.Gate = g
	if w.command != CommandAnswerGate || g.Answer == nil {
		return res, nil
	}
	if res.Memory, err = loadMemory(ctx, w.tx, w.meta.Scope, g.Answer.Memory.ID, false); err != nil {
		return Result{}, err
	}
	if res.Memory.Sources, err = loadSources(ctx, w.tx, res.Memory.ID); err != nil {
		return Result{}, err
	}
	return res, attachDetails(ctx, w.tx, []*Memory{res.Memory})
}
