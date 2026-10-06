package ledger

import (
	"context"
	"encoding/json"
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

// The judge (plan 25 §5.8) runs as a River job on every proposal (and on
// every memory a Write-level agent kept, after the fact). The ledger's
// part is small and keeps the one-write-path rule:
//
//   - every command that writes a proposal, or a new version of one,
//     enqueues judge_proposal for that memory version in its own
//     transaction (InsertTx, like compile_target);
//   - JudgeSnapshot reads what the judge compares against, inside the
//     space's scope;
//   - RecordVerdict applies the verdict as Memax: fold a duplicate into
//     the memory it repeats, fold a re-proposal into its rejection, link
//     an update to what it updates, flag a contradiction of a decision in
//     force, or record that nothing was found. Each is receipted, and a
//     fold is undoable.
//
// The judge's own logic (fingerprints, candidates, the model call) lives
// in internal/judge.

// QueueJudge is the River queue judge jobs run on.
const QueueJudge = "judge"

// JudgeMode says what kind of memory a judge job checks.
type JudgeMode string

// The modes.
const (
	// JudgeProposal: a proposal, before anyone keeps it.
	JudgeProposal JudgeMode = "proposal"
	// JudgeKept: a memory a Write-level agent kept at once. The inline
	// pre-check let it through; the judge only looks for a contradiction
	// with a decision in force, and flags it.
	JudgeKept JudgeMode = "kept"
)

// JudgeArgs is the River job that judges one memory version.
//
// It is unique by args over every state River keeps (completed
// included), so a version is judged once per round. Round 0 is the first
// judgement; Undo enqueues a later round when a fold is undone (Force,
// with SkipFold so it doesn't fold again) or when a proposal it puts back
// in Review was never judged.
type JudgeArgs struct {
	MemoryID uuid.UUID `json:"memory_id"`
	SpaceID  uuid.UUID `json:"space_id"`
	Version  int       `json:"version"`
	Mode     JudgeMode `json:"mode"`
	Round    int       `json:"round,omitempty"`
	// Force judges the version again even if it has a verdict.
	Force bool `json:"force,omitempty"`
	// SkipFold skips stage 0's folds: a person undid one.
	SkipFold bool `json:"skip_fold,omitempty"`
	// Cause is the undo entry that queued this round, if any, so two undos
	// of the same version never collapse into one job.
	Cause string `json:"cause,omitempty"`
}

// Kind implements river.JobArgs.
func (JudgeArgs) Kind() string { return "judge_proposal" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (JudgeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueJudge,
		// The model call retries and falls back inside one attempt; River's
		// attempts cover the database (a busy memory, a restart).
		MaxAttempts: 3,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// IdempotencyKey is the key RecordVerdict uses for this job.
func (a JudgeArgs) IdempotencyKey() string {
	return fmt.Sprintf("judge:%s:%d:%d", a.MemoryID, a.Version, a.Round)
}

// enqueueJudge queues a judge job for a memory version; flush inserts it.
func (w *writer) enqueueJudge(args JudgeArgs) {
	if !slices.ContainsFunc(w.jobs, func(p river.InsertManyParams) bool { return p.Args == args }) {
		w.jobs = append(w.jobs, river.InsertManyParams{Args: args})
	}
}

// judgeAfterWrite queues the judge for a memory a command just wrote: a
// proposal always, and a memory an agent kept at once (mode kept). A
// person's own Keep is a person's decision and is not second-guessed.
func (w *writer) judgeAfterWrite(spaceID, memoryID uuid.UUID, version int, l lifecycle.Lifecycle) {
	switch {
	case l == lifecycle.Proposed:
		w.enqueueJudge(JudgeArgs{MemoryID: memoryID, SpaceID: spaceID, Version: version, Mode: JudgeProposal})
	case l == lifecycle.Kept && w.meta.Actor.Kind == policy.ActorAgent:
		w.enqueueJudge(JudgeArgs{MemoryID: memoryID, SpaceID: spaceID, Version: version, Mode: JudgeKept})
	}
}

// ---------------------------------------------------------------------
// Verdicts
// ---------------------------------------------------------------------

// Relation is how a proposal relates to one candidate.
type Relation string

// The relations (§5.8 step 3). RelationNone means nothing was compared.
const (
	RelationDuplicate   Relation = "duplicate"
	RelationUpdates     Relation = "updates"
	RelationExtends     Relation = "extends"
	RelationContradicts Relation = "contradicts"
	RelationUnrelated   Relation = "unrelated"
	RelationNone        Relation = "none"
)

// Relations lists every relation.
var Relations = []Relation{RelationDuplicate, RelationUpdates, RelationExtends, RelationContradicts, RelationUnrelated, RelationNone}

// Valid reports whether r is a known relation.
func (r Relation) Valid() bool { return slices.Contains(Relations, r) }

// JudgeStage is the stage that decided a verdict.
type JudgeStage string

// The stages.
const (
	StageExact      JudgeStage = "exact"      // the same words as a kept memory
	StageNear       JudgeStage = "near"       // a near-verbatim repeat (MinHash, Jaccard ≥ 0.9)
	StageReproposal JudgeStage = "reproposal" // a repeat of something rejected in the last 90 days
	StageLLM        JudgeStage = "llm"        // the model compared it with its candidates
	StageNone       JudgeStage = "none"       // nothing to compare against, or no model configured
)

// JudgeStages lists every stage.
var JudgeStages = []JudgeStage{StageExact, StageNear, StageReproposal, StageLLM, StageNone}

// VerdictOutcome is what a verdict did to the memory.
type VerdictOutcome string

// The outcomes.
const (
	OutcomeFolded      VerdictOutcome = "folded"      // merged into the memory it duplicates
	OutcomeSuppressed  VerdictOutcome = "suppressed"  // merged into the rejection it repeats
	OutcomeLinked      VerdictOutcome = "linked"      // linked as an update of a kept memory
	OutcomeSuperseding VerdictOutcome = "superseding" // linked as an explicit change of a decision in force
	OutcomeFlagged     VerdictOutcome = "flagged"     // flagged as a conflict with a decision in force
	OutcomeNone        VerdictOutcome = "none"        // nothing to do
	OutcomeFailed      VerdictOutcome = "failed"      // the model gave no usable answer; nothing flagged
	OutcomeSkipped     VerdictOutcome = "skipped"     // the target changed before the verdict landed
)

// VerdictOutcomes lists every outcome.
var VerdictOutcomes = []VerdictOutcome{OutcomeFolded, OutcomeSuppressed, OutcomeLinked, OutcomeSuperseding,
	OutcomeFlagged, OutcomeNone, OutcomeFailed, OutcomeSkipped}

// The judge's model tiers (plan 25 §4.3), as verdicts record them.
const (
	TierPrimary  = "primary"
	TierFallback = "fallback"
	TierStrong   = "strong"
)

// The judge's states, as a memory's projection shows them.
const (
	JudgeWorking = "working" // a proposal the judge hasn't judged yet: Review's neutral mark
	JudgeJudged  = "judged"
	JudgeFailed  = "failed" // the model gave no answer; it is reviewed as usual
)

// MemoryPointer names another memory.
type MemoryPointer struct {
	ID  uuid.UUID `json:"id"`
	Ref string    `json:"ref"`
}

// JudgeInfo is the judge's verdict on a memory's current version.
type JudgeInfo struct {
	State   string         `json:"state"`
	Version int            `json:"version"`
	Verdict Relation       `json:"verdict,omitempty"`
	Outcome VerdictOutcome `json:"outcome,omitempty"`
	Stage   JudgeStage     `json:"stage,omitempty"`
	// Related is the memory the verdict is about: the one it repeats,
	// updates or contradicts.
	Related    *MemoryPointer `json:"related,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	// Rationale is one line from the model, in English.
	Rationale string `json:"rationale,omitempty"`
	// MergedStatement is the model's merged wording, for Review to offer.
	MergedStatement string     `json:"merged_statement,omitempty"`
	Tier            string     `json:"tier,omitempty"`
	JudgedAt        *time.Time `json:"judged_at,omitempty"`
}

// VerdictCandidate is one compared memory, as a verdict records it: ids
// and labels, never words.
type VerdictCandidate struct {
	MemoryID   uuid.UUID `json:"memory_id"`
	Ref        string    `json:"ref"`
	Sets       []string  `json:"sets"` // lexical | vector | keyed | exact | near
	Relation   Relation  `json:"relation"`
	Confidence *float64  `json:"confidence,omitempty"`
	Tier       string    `json:"tier,omitempty"`
}

// Verdict is what the judge concluded about one memory version.
type Verdict struct {
	Stage    JudgeStage
	Relation Relation
	// Related is the memory the relation is with (uuid.Nil for none).
	Related    uuid.UUID
	Confidence *float64
	// Rationale and MergedStatement come from the model and may quote
	// memories; they are purged at Forget.
	Rationale       string
	MergedStatement string
	Tier            string
	Model           string
	Candidates      []VerdictCandidate
	// Error is a code (llm_failed, escalation_failed, …), never words.
	Error string
	// Timings are in milliseconds.
	Timings map[string]int64
}

// RecordVerdict applies a verdict, as Memax (the judge). The ledger
// re-checks the outcome against the record before applying it: if the
// memory moved on (kept, rejected, a newer version) it writes nothing
// (Unchanged), and if the memory the outcome points at changed, it
// records the verdict as skipped.
type RecordVerdict struct {
	Meta
	Memory  uuid.UUID
	Version int
	Mode    JudgeMode
	Round   int
	// Force records the round even if the version has a verdict from an
	// earlier one (a re-judgement after an undone fold).
	Force   bool
	Verdict Verdict
	// Outcome is what to do: folded, suppressed, linked, superseding,
	// flagged, none or failed.
	Outcome VerdictOutcome
	// Target is the memory a fold, link or flag points at.
	Target uuid.UUID
	// AlsoConflicts are further decisions in force the memory contradicts
	// (flagged only): each gets its conflicts_with link too.
	AlsoConflicts []uuid.UUID
	// Conditions are the "stays true while" conditions the judge proposes.
	// They are written only on a proposal that has none.
	Conditions json.RawMessage
}

// Name implements Command.
func (*RecordVerdict) Name() CommandName { return CommandRecordVerdict }

// CommandRecordVerdict names RecordVerdict.
const CommandRecordVerdict CommandName = "record_verdict"

const (
	maxVerdictRationale = 500
	maxVerdictModel     = 200
)

func (c *RecordVerdict) validate() error {
	if c.Memory == uuid.Nil || c.Version < 1 || c.Round < 0 {
		return invalid("memory", "say which memory version the verdict is about")
	}
	if c.Mode != JudgeProposal && c.Mode != JudgeKept {
		return invalid("mode", "use proposal or kept")
	}
	if !slices.Contains(VerdictOutcomes, c.Outcome) || c.Outcome == OutcomeSkipped {
		return invalid("outcome", "use folded, suppressed, linked, superseding, flagged, none or failed")
	}
	if c.Outcome != OutcomeNone && c.Outcome != OutcomeFailed && c.Target == uuid.Nil {
		return invalid("target", "a %s verdict needs the memory it points at", c.Outcome)
	}
	if c.Mode == JudgeKept && c.Outcome != OutcomeFlagged && c.Outcome != OutcomeNone && c.Outcome != OutcomeFailed {
		return invalid("outcome", "a kept memory is only ever flagged")
	}
	v := &c.Verdict
	if !slices.Contains(JudgeStages, v.Stage) || !v.Relation.Valid() {
		return invalid("verdict", "unknown stage or relation")
	}
	if v.Confidence != nil && (*v.Confidence < 0 || *v.Confidence > 1) {
		return invalid("verdict.confidence", "must be between 0 and 1")
	}
	if v.Tier != "" && v.Tier != TierPrimary && v.Tier != TierFallback && v.Tier != TierStrong {
		return invalid("verdict.tier", "use primary, fallback or strong")
	}
	v.Rationale = truncateRunes(strings.TrimSpace(v.Rationale), maxVerdictRationale)
	v.MergedStatement = truncateRunes(strings.TrimSpace(v.MergedStatement), MaxStatementRunes)
	v.Model = truncateRunes(v.Model, maxVerdictModel)
	for _, s := range []string{v.Rationale, v.MergedStatement, v.Model} {
		if err := checkText("verdict", s, MaxStatementRunes, false); err != nil {
			return err
		}
	}
	if v.Error != "" && !validErrorCode(v.Error) {
		return invalid("verdict.error", "use a code like llm_failed")
	}
	var err error
	if c.Conditions, err = jsonOr(c.Conditions, "conditions", "[]", '['); err != nil {
		return err
	}
	return nil
}

func validErrorCode(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}

// recordVerdict applies RecordVerdict.
func (w *writer) recordVerdict(ctx context.Context, c *RecordVerdict) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory.String())
	if err != nil || replay != nil {
		return deref(replay), err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionJudge, policy.Object{Ref: mem.Ref}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	want := lifecycle.Proposed
	if c.Mode == JudgeKept {
		want = lifecycle.Kept
	}
	var done bool
	if err := w.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM v2.judge_verdicts
		                WHERE memory_id = $1 AND version = $2 AND (round >= $3 OR NOT $4))`,
		mem.ID, c.Version, c.Round, c.Force).Scan(&done); err != nil {
		return Result{}, fmt.Errorf("ledger: read verdicts: %w", err)
	}
	// The memory moved on (kept, rejected, edited) or this round is
	// already recorded: the verdict is about words nobody is reviewing.
	if done || mem.Version != c.Version || mem.Lifecycle != want || (c.Mode == JudgeKept && mem.Flags.Has(lifecycle.Conflict)) {
		return Result{Outcome: OutcomeApplied, Policy: dec, Memory: mem, Unchanged: true}, nil
	}

	outcome := c.Outcome
	var target *Memory
	var conflicts []*Memory
	if outcome != OutcomeNone && outcome != OutcomeFailed {
		ids := append([]uuid.UUID{c.Target}, c.AlsoConflicts...)
		locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR SHARE")
		if err != nil {
			return Result{}, err
		}
		target = locked[c.Target]
		ok := target != nil && target.SpaceID == mem.SpaceID && target.ID != mem.ID
		switch outcome {
		case OutcomeFolded, OutcomeLinked:
			ok = ok && target.Lifecycle == lifecycle.Kept
		case OutcomeSuppressed:
			ok = ok && target.Lifecycle == lifecycle.Rejected
		case OutcomeSuperseding:
			ok = ok && target.inForce()
		case OutcomeFlagged:
			for _, id := range ids {
				if m := locked[id]; m != nil && m.SpaceID == mem.SpaceID && m.ID != mem.ID && m.inForce() &&
					!slices.ContainsFunc(conflicts, func(x *Memory) bool { return x.ID == m.ID }) {
					conflicts = append(conflicts, m)
				}
			}
			ok = len(conflicts) > 0
			if ok && !slices.ContainsFunc(conflicts, func(x *Memory) bool { return x.ID == c.Target }) {
				target = conflicts[0]
			}
		}
		if !ok {
			outcome, target, conflicts = OutcomeSkipped, nil, nil
		}
	}

	action := ActionJudged
	var reason string
	switch outcome {
	case OutcomeFolded:
		action = ActionMerged
		switch c.Verdict.Stage {
		case StageExact:
			reason = fmt.Sprintf("The same words as %s.", target.Ref)
		case StageNear:
			reason = fmt.Sprintf("A near-verbatim repeat of %s.", target.Ref)
		default:
			reason = fmt.Sprintf("A duplicate of %s.", target.Ref)
		}
	case OutcomeSuppressed:
		action = ActionMerged
		reason = fmt.Sprintf("Proposed again; previously rejected %s (%s).", target.UpdatedAt.UTC().Format("Jan 2"), target.Ref)
	case OutcomeLinked:
		action = ActionLinked
		reason = fmt.Sprintf("Updates %s.", target.Ref)
	case OutcomeSuperseding:
		action = ActionLinked
		reason = fmt.Sprintf("Supersedes %s, a decision in force, by saying it changed.", target.Ref)
	case OutcomeFlagged:
		action = ActionFlagged
		refs := make([]string, len(conflicts))
		for i, m := range conflicts {
			refs[i] = m.Ref
		}
		reason = fmt.Sprintf("Contradicts %s, %s in force.", strings.Join(refs, " and "), pluralDecision(len(refs)))
	case OutcomeFailed:
		reason = "The judge couldn't reach a verdict. Review it as usual."
	case OutcomeSkipped:
		reason = "The judge's verdict no longer applies: the memory it was about changed."
	default:
		reason = fmt.Sprintf("No duplicates or conflicts among %d memories.", len(c.Verdict.Candidates))
	}

	if outcome == OutcomeFolded || outcome == OutcomeSuppressed {
		w.startUndo(UndoJudgeFold, w.judgeUndoWindow)
		w.undo.touch(mem)
	}
	rc := w.receipt(sp, mem.ID, mem.Ref, action, mem.streamVersion+1, reason)
	if target != nil {
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: target.Ref}
	}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}

	next := mem.state()
	switch outcome {
	case OutcomeFolded, OutcomeSuppressed:
		if next, err = transition(mem, lifecycle.VerbMerge); err != nil {
			return Result{}, err
		}
		if _, err := w.insertLink(ctx, sp.ID, LinkMergedInto, mem.ID, target.ID, rc.ID); err != nil {
			return Result{}, err
		}
	case OutcomeLinked, OutcomeSuperseding:
		if _, err := w.insertLink(ctx, sp.ID, LinkSupersedes, mem.ID, target.ID, rc.ID); err != nil {
			return Result{}, err
		}
	case OutcomeFlagged:
		if next, err = transition(mem, lifecycle.VerbFlagConflict); err != nil {
			return Result{}, err
		}
		for _, d := range conflicts {
			if _, err := w.insertLink(ctx, sp.ID, LinkConflictsWith, mem.ID, d.ID, rc.ID); err != nil {
				return Result{}, err
			}
		}
	}
	conditions := mem.Conditions
	if c.Mode == JudgeProposal && outcome != OutcomeFolded && outcome != OutcomeSuppressed &&
		len(c.Conditions) > 2 && strings.TrimSpace(string(mem.Conditions)) == "[]" {
		conditions = c.Conditions
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.memories
		   SET lifecycle = $2, flags = $3, conditions = $4, stream_version = $5, last_receipt_id = $6, updated_at = now()
		 WHERE id = $1 AND space_id = $7`,
		mem.ID, string(next.Lifecycle), next.Flags.Strings(), []byte(conditions), rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update memory: %w", err)
	}
	// A kept memory in conflict compiles with an "In conflict" marker.
	if c.Mode == JudgeKept && outcome == OutcomeFlagged {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	if err := w.insertVerdict(ctx, sp.ID, mem.ID, c, outcome, target, rc.ID); err != nil {
		return Result{}, err
	}
	receipts := []Receipt{rc}
	if err := w.writeUndo(ctx, sp, receipts); err != nil {
		return Result{}, err
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, mem.ID)
}

func pluralDecision(n int) string {
	if n == 1 {
		return "a decision"
	}
	return "decisions"
}

func (w *writer) insertVerdict(ctx context.Context, spaceID, memoryID uuid.UUID, c *RecordVerdict, outcome VerdictOutcome, target *Memory, receiptID uuid.UUID) error {
	v := c.Verdict
	related := v.Related
	if target != nil {
		related = target.ID
	}
	var relatedArg any
	if related != uuid.Nil {
		relatedArg = related
	}
	cands, err := json.Marshal(nonNilSlice(v.Candidates))
	if err != nil {
		return err
	}
	timings := v.Timings
	if timings == nil {
		timings = map[string]int64{}
	}
	tj, err := json.Marshal(timings)
	if err != nil {
		return err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.judge_verdicts (memory_id, version, round, space_id, mode, stage, verdict, outcome, related_memory_id,
		                               confidence, rationale, merged_statement, tier, model, candidates, error, timings,
		                               receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $18)`,
		memoryID, c.Version, c.Round, spaceID, string(c.Mode), string(v.Stage), string(v.Relation), string(outcome), relatedArg,
		v.Confidence, nullText(v.Rationale), nullText(v.MergedStatement), nullText(v.Tier), nullText(v.Model),
		cands, nullText(v.Error), tj, receiptID); err != nil {
		return fmt.Errorf("ledger: write verdict: %w", err)
	}
	return nil
}

// attachVerdicts fills Judge on each memory from its current version's
// latest verdict, or "working" for a proposal without one.
func attachVerdicts(ctx context.Context, tx pgx.Tx, ms []*Memory) error {
	ids := make([]uuid.UUID, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (v.memory_id) v.memory_id, v.version, v.stage, v.verdict, v.outcome,
		       v.related_memory_id, r.seq, v.confidence, COALESCE(v.rationale, ''), COALESCE(v.merged_statement, ''),
		       COALESCE(v.tier, ''), v.created_at
		  FROM v2.judge_verdicts v
		  JOIN v2.memories m ON m.id = v.memory_id AND m.current_version = v.version
		  LEFT JOIN v2.memories r ON r.id = v.related_memory_id
		 WHERE v.memory_id = ANY ($1)
		 ORDER BY v.memory_id, v.round DESC`, ids)
	if err != nil {
		return fmt.Errorf("ledger: load verdicts: %w", err)
	}
	defer rows.Close()
	got := map[uuid.UUID]*JudgeInfo{}
	for rows.Next() {
		var id uuid.UUID
		var related *uuid.UUID
		var relatedSeq *int64
		var conf *float32
		var at time.Time
		j := &JudgeInfo{State: JudgeJudged}
		if err := rows.Scan(&id, &j.Version, &j.Stage, &j.Verdict, &j.Outcome, &related, &relatedSeq, &conf,
			&j.Rationale, &j.MergedStatement, &j.Tier, &at); err != nil {
			return fmt.Errorf("ledger: load verdicts: %w", err)
		}
		if related != nil && relatedSeq != nil {
			j.Related = &MemoryPointer{ID: *related, Ref: FormatRef(PrefixMemory, *relatedSeq)}
		}
		if conf != nil {
			c := float64(*conf)
			j.Confidence = &c
		}
		if j.Outcome == OutcomeFailed {
			j.State = JudgeFailed
		}
		j.JudgedAt = &at
		got[id] = j
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: load verdicts: %w", err)
	}
	for _, m := range ms {
		switch j := got[m.ID]; {
		case j != nil:
			m.Judge = j
		case m.Lifecycle == lifecycle.Proposed:
			m.Judge = &JudgeInfo{State: JudgeWorking, Version: m.Version}
		}
	}
	return nil
}

// inForce reports whether a memory is a decision in force: kept, of kind
// decision, and neither superseded nor left open.
func (m *Memory) inForce() bool {
	if m.Kind != KindDecision || m.Lifecycle != lifecycle.Kept {
		return false
	}
	return m.Decision == nil || m.Decision.Status == "" || m.Decision.Status == DecisionInForce
}

// area is a memory's decision area, normalised.
func (m *Memory) area() string {
	if m.Decision == nil {
		return ""
	}
	return textsig.NormalizeKey(m.Decision.Area)
}

// lockMemories loads memories by id with a row lock (lock is "FOR
// UPDATE" or "FOR SHARE"), in id order so two commands that lock the
// same memories can't deadlock on each other.
func lockMemories(ctx context.Context, tx pgx.Tx, scope Scope, ids []uuid.UUID, lock string) (map[uuid.UUID]*Memory, error) {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	sorted = slices.Compact(sorted)
	rows, err := tx.Query(ctx, memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2) ORDER BY m.id `+lock+` OF m`,
		sorted, scope.SpaceIDs())
	if err != nil {
		return nil, fmt.Errorf("ledger: lock memories: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: lock memories: %w", err)
	}
	out := make(map[uuid.UUID]*Memory, len(ms))
	for _, m := range ms {
		out[m.ID] = m
	}
	return out, nil
}

// ---------------------------------------------------------------------
// What the judge compares against
// ---------------------------------------------------------------------

// JudgeCandidate is a memory the judge may compare a proposal with.
type JudgeCandidate struct {
	ID        uuid.UUID
	Ref       string
	Statement string
	Kind      Kind
	Section   Section
	Lifecycle lifecycle.Lifecycle
	Trust     policy.Trust
	// Area is the decision's area key, normalised; empty for facts.
	Area           string
	DecisionStatus string
	InForce        bool
	UpdatedAt      time.Time
	// Score is the lane's score: ts_rank for FTS, similarity for trigram.
	Score float64
}

// JudgeSnapshot is what the judge needs about one memory version, read in
// one snapshot of the space.
type JudgeSnapshot struct {
	Memory *Memory
	// Eligible is false when the memory moved on since the job was queued
	// (another version, kept, rejected), or this round already has a
	// verdict: the job has nothing to do.
	Eligible bool
	// Judged is set when the version has a verdict from any round.
	Judged bool
	// Exact are memories with the same content hash, and Near those
	// sharing an LSH band: kept ones, and ones rejected within the
	// re-proposal window.
	Exact []JudgeCandidate
	Near  []JudgeCandidate
	// FTS and Trigram are the kept memories closest by full-text rank and
	// by trigram similarity, best first (the hybrid lexical lanes).
	FTS     []JudgeCandidate
	Trigram []JudgeCandidate
	// Decisions are every decision in force in the space.
	Decisions []JudgeCandidate
}

// Lane sizes and floors for the hybrid candidates.
const (
	judgeLaneLimit     = 30
	judgeTrigramFloor  = 0.12
	judgeMaxDecisions  = 200
	judgeMaxNear       = 50
	judgeMaxQueryTerms = 32
)

// JudgeSnapshot reads the memory a judge job is about and everything it
// is compared against, at one REPEATABLE READ snapshot of the space.
// rejectedWithin is the re-proposal window (90 days).
func (l *Ledger) JudgeSnapshot(ctx context.Context, scope Scope, args JudgeArgs, rejectedWithin time.Duration) (*JudgeSnapshot, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(args.SpaceID); !ok {
		return nil, ErrNotFound
	}
	var out *JudgeSnapshot
	err := l.readSnapshot(ctx, scope, func(tx pgx.Tx) error {
		m, err := loadMemory(ctx, tx, scope, args.MemoryID, false)
		if err != nil {
			return err
		}
		if m.SpaceID != args.SpaceID {
			return ErrNotFound
		}
		if m.Sources, err = loadSources(ctx, tx, m.ID); err != nil {
			return err
		}
		s := &JudgeSnapshot{Memory: m}
		var maxRound *int
		if err := tx.QueryRow(ctx, `SELECT max(round) FROM v2.judge_verdicts WHERE memory_id = $1 AND version = $2`,
			m.ID, args.Version).Scan(&maxRound); err != nil {
			return fmt.Errorf("ledger: read verdicts: %w", err)
		}
		s.Judged = maxRound != nil
		want := lifecycle.Proposed
		if args.Mode == JudgeKept {
			want = lifecycle.Kept
		}
		s.Eligible = m.Version == args.Version && m.Lifecycle == want &&
			(maxRound == nil || (args.Force && *maxRound < args.Round)) &&
			!(args.Mode == JudgeKept && m.Flags.Has(lifecycle.Conflict))
		if !s.Eligible {
			out = s
			return nil
		}
		window := fmt.Sprintf("%d seconds", int64(rejectedWithin.Seconds()))
		repeat := `(m.lifecycle = 'kept' OR (m.lifecycle = 'rejected' AND m.updated_at >= now() - $4::interval))`
		if s.Exact, err = judgeCandidates(ctx, tx, `
			 WHERE m.space_id = $1 AND m.id <> $2 AND m.content_sha256 = $3 AND `+repeat+`
			 ORDER BY m.lifecycle = 'kept' DESC, m.seq LIMIT 20`,
			m.SpaceID, m.ID, textsig.ContentSHA256(m.Statement), window); err != nil {
			return err
		}
		if s.Near, err = judgeCandidates(ctx, tx, `
			 WHERE m.space_id = $1 AND m.id <> $2 AND m.minhash_bands && $3::bigint[] AND `+repeat+`
			 ORDER BY m.lifecycle = 'kept' DESC, m.seq LIMIT `+fmt.Sprint(judgeMaxNear),
			m.SpaceID, m.ID, textsig.BandsOf(m.Statement), window); err != nil {
			return err
		}
		if q := ftsQuery(m.Statement); q != "" {
			if s.FTS, err = judgeCandidates(ctx, tx, `
				 , to_tsquery('simple', public.immutable_unaccent($3)) q
				 WHERE m.space_id = $1 AND m.id <> $2 AND m.lifecycle = 'kept' AND m.search @@ q
				 ORDER BY ts_rank_cd(m.search, q) DESC, m.seq DESC LIMIT `+fmt.Sprint(judgeLaneLimit),
				m.SpaceID, m.ID, q); err != nil {
				return err
			}
		}
		if s.Trigram, err = judgeCandidates(ctx, tx, `
			 WHERE m.space_id = $1 AND m.id <> $2 AND m.lifecycle = 'kept'
			   AND public.similarity(lower(v.statement), lower($3)) >= `+fmt.Sprint(judgeTrigramFloor)+`
			 ORDER BY public.similarity(lower(v.statement), lower($3)) DESC, m.seq DESC LIMIT `+fmt.Sprint(judgeLaneLimit),
			m.SpaceID, m.ID, m.Statement); err != nil {
			return err
		}
		if s.Decisions, err = decisionsInForce(ctx, tx, m.SpaceID, m.ID); err != nil {
			return err
		}
		out = s
		return nil
	})
	return out, err
}

// judgeCandidateSelect reads candidates: the current statement and what a
// verdict needs to know about each.
const judgeCandidateSelect = `
	SELECT m.id, m.seq, COALESCE(v.statement, ''), m.kind, m.section, m.lifecycle, m.trust,
	       COALESCE(m.decision ->> 'area', ''), COALESCE(m.decision ->> 'status', ''), m.updated_at
	  FROM v2.memories m
	  JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version`

func judgeCandidates(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]JudgeCandidate, error) {
	rows, err := tx.Query(ctx, judgeCandidateSelect+where, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: judge candidates: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanJudgeCandidate)
	if err != nil {
		return nil, fmt.Errorf("ledger: judge candidates: %w", err)
	}
	return out, nil
}

func scanJudgeCandidate(r pgx.CollectableRow) (JudgeCandidate, error) {
	var c JudgeCandidate
	var seq int64
	if err := r.Scan(&c.ID, &seq, &c.Statement, &c.Kind, &c.Section, &c.Lifecycle, &c.Trust, &c.Area, &c.DecisionStatus, &c.UpdatedAt); err != nil {
		return JudgeCandidate{}, err
	}
	c.Ref = FormatRef(PrefixMemory, seq)
	c.Area = textsig.NormalizeKey(c.Area)
	c.InForce = c.Kind == KindDecision && c.Lifecycle == lifecycle.Kept &&
		(c.DecisionStatus == "" || c.DecisionStatus == DecisionInForce)
	return c, nil
}

// decisionsInForce lists a space's decisions in force (except one memory).
func decisionsInForce(ctx context.Context, tx pgx.Tx, spaceID, except uuid.UUID) ([]JudgeCandidate, error) {
	return judgeCandidates(ctx, tx, `
		 WHERE m.space_id = $1 AND m.id <> $2 AND m.kind = 'decision' AND m.lifecycle = 'kept'
		   AND COALESCE(m.decision ->> 'status', '') IN ('', 'in_force')
		 ORDER BY m.seq DESC LIMIT `+fmt.Sprint(judgeMaxDecisions), spaceID, except)
}

// ftsQuery is an OR query of a statement's content words, for the
// full-text lane: any shared word is a reason to look, and ts_rank_cd
// orders by how many and how close.
func ftsQuery(statement string) string {
	var terms []string
	seen := map[string]bool{}
	for w := range textsig.ContentWords(statement) {
		for _, part := range strings.FieldsFunc(w, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r < 0x80
		}) {
			if len(part) < 2 || seen[part] || strings.ContainsAny(part, "'\\:&|!()<>*") {
				continue
			}
			seen[part] = true
			// A prefix match, so the stem "deploy" finds "deploys".
			terms = append(terms, "'"+part+"':*")
		}
	}
	slices.Sort(terms)
	if len(terms) > judgeMaxQueryTerms {
		terms = terms[:judgeMaxQueryTerms]
	}
	return strings.Join(terms, " | ")
}

// DecisionTouch is how a statement touches a decision in force.
type DecisionTouch struct {
	Decision JudgeCandidate
	// Why is "area" (the same area key), "mentions" (the statement names
	// the decision's area) or "overlap" (they share enough words).
	Why string
}

// The overlap a write must have with a decision in force to be held for
// the judge: three shared content words, or two that cover at least half
// of the shorter statement. "Deploy the v2 API to Fly.io in iad and ams"
// against "Deploy the v2 API to Railway for its preview environments"
// shares three (deploy, v2, api) of seven. A needless hold costs a Review
// card, never a false conflict; eval/judge reports both rates.
const (
	touchMinShared = 2
	touchMinCoeff  = 0.5
	touchShared    = 3
)

// Touches reports which decisions in force a statement touches: the same
// area key as area, the decision's area named in the statement, or enough
// shared words. It is the Write-level pre-check and the judge's keyed
// candidate set; it only reads, and costs one indexed query per space.
func Touches(statement, area string, decisions []JudgeCandidate) []DecisionTouch {
	area = textsig.NormalizeKey(area)
	var out []DecisionTouch
	for _, d := range decisions {
		if !d.InForce {
			continue
		}
		switch shared, coeff := textsig.Overlap(statement, d.Statement); {
		case area != "" && d.Area == area:
			out = append(out, DecisionTouch{Decision: d, Why: "area"})
		case d.Area != "" && textsig.Mentions(statement, d.Area):
			out = append(out, DecisionTouch{Decision: d, Why: "mentions"})
		case shared >= touchShared || (shared >= touchMinShared && coeff >= touchMinCoeff):
			out = append(out, DecisionTouch{Decision: d, Why: "overlap"})
		}
	}
	return out
}

// JudgeGrace is how long Keep waits for the judge on a proposal that
// touches a decision in force: within it, a Keep of words the judge hasn't
// looked at yet is refused as busy (try again in a moment), so a conflict
// is flagged before anyone keeps it (rule 11). After it, Keep goes ahead:
// a judge that is down must never block Review.
const JudgeGrace = 30 * time.Second

// JudgePendingError: Keep came before the judge, on a proposal that
// touches a decision in force. Retry in a moment.
type JudgePendingError struct{ Ref string }

func (e *JudgePendingError) Error() string {
	return fmt.Sprintf("Memax is still checking %s against the decisions in force. Try again in a moment.", e.Ref)
}

// Is makes errors.Is(err, ErrBusy) match.
func (e *JudgePendingError) Is(target error) bool { return target == ErrBusy }

// awaitJudge refuses a Keep that came before the judge (JudgeGrace).
func (w *writer) awaitJudge(ctx context.Context, mem *Memory) error {
	if mem.Lifecycle != lifecycle.Proposed {
		return nil
	}
	var pending bool
	if err := w.tx.QueryRow(ctx, `
		SELECT v.created_at > now() - make_interval(secs => $3)
		       AND NOT EXISTS (SELECT 1 FROM v2.judge_verdicts j WHERE j.memory_id = $1 AND j.version = $2)
		  FROM v2.memory_versions v WHERE v.memory_id = $1 AND v.version = $2`,
		mem.ID, mem.Version, JudgeGrace.Seconds()).Scan(&pending); err != nil {
		return fmt.Errorf("ledger: read verdicts: %w", err)
	}
	if !pending {
		return nil
	}
	area := ""
	if mem.Decision != nil {
		area = mem.Decision.Area
	}
	touches, err := w.touchesDecision(ctx, mem.SpaceID, mem.ID, mem.Statement, area)
	if err != nil || !touches {
		return err
	}
	return &JudgePendingError{Ref: mem.Ref}
}

// touchesDecision is the inline pre-check for a Write-level agent's
// write: does it touch a decision in force (Touches)? It runs inside the
// command's transaction.
func (w *writer) touchesDecision(ctx context.Context, spaceID, except uuid.UUID, statement, area string) (bool, error) {
	ds, err := decisionsInForce(ctx, w.tx, spaceID, except)
	if err != nil {
		return false, err
	}
	return len(Touches(statement, area, ds)) > 0, nil
}

// signature is a statement's stage-0 fingerprints, as stored on memories.
func signature(statement string) (string, []int64) {
	return textsig.ContentSHA256(statement), textsig.BandsOf(statement)
}
