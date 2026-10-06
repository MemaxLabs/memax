package judge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// actor is the judge as receipts name it: Memax, through the system. The
// judge is Memax's own check, with no person or agent behind it, the same
// actor the compile coordinator records compiles as. Its receipts say what
// it did (merged, linked, flagged, judged) and which memory it compared
// with (source), so a separate actor kind would carry nothing more.
var actor = ledger.Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}

// Judge runs the pipeline for one memory version.
type Judge struct {
	ledger     *ledger.Ledger
	classifier *Classifier
	vectors    Vectors
	cfg        Config
	now        func() time.Time

	// Metrics counts what the judge did, for logs and tests.
	Metrics Metrics
}

// Metrics are the judge's counters.
type Metrics struct {
	Judged, Folded, Suppressed, Linked, Superseding, Flagged atomic.Int64
	// LLMFailed counts stage-1 failures (the proposal stays unflagged);
	// EscalationFailed counts verdicts on decisions the strong tier didn't
	// confirm.
	LLMFailed, EscalationFailed atomic.Int64
}

// Option configures a Judge.
type Option func(*Judge)

// WithVectors adds vector candidates (nil: lexical only).
func WithVectors(v Vectors) Option { return func(j *Judge) { j.vectors = v } }

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(j *Judge) { j.now = now } }

// New returns a judge on the ledger, or nil when the ledger is nil. A nil
// model runs stage 0 only.
func New(l *ledger.Ledger, m Model, cfg Config, opts ...Option) *Judge {
	if l == nil {
		return nil
	}
	cfg = cfg.withDefaults()
	j := &Judge{ledger: l, classifier: NewClassifier(m, cfg), cfg: cfg, now: time.Now}
	for _, o := range opts {
		o(j)
	}
	return j
}

// Stage1 reports whether the model stage is on.
func (j *Judge) Stage1() bool { return j != nil && j.classifier != nil }

// RunOptions are what the worker knows about the job.
type RunOptions struct {
	// EnqueuedAt is when the job was created, for the queue time.
	EnqueuedAt time.Time
}

// Run is what one judgement did.
type Run struct {
	// Skipped: the memory moved on before the judge got to it.
	Skipped    bool
	Stage      ledger.JudgeStage
	Outcome    ledger.VerdictOutcome
	Relation   ledger.Relation
	Target     string
	Candidates int
	Calls      map[string]int
	Timings    map[string]int64
	Receipt    *ledger.Receipt
}

// Run judges one memory version and records the verdict.
func (j *Judge) Run(ctx context.Context, args ledger.JudgeArgs, opts RunOptions) (*Run, error) {
	start := j.now()
	timings := map[string]int64{}
	if !opts.EnqueuedAt.IsZero() {
		timings["queue_ms"] = max(0, start.Sub(opts.EnqueuedAt).Milliseconds())
	}
	scope, err := j.ledger.SpaceScope(ctx, args.SpaceID)
	if err != nil {
		return nil, err
	}
	snap, err := j.ledger.JudgeSnapshot(ctx, scope, args, j.cfg.RejectedWithin)
	if errors.Is(err, ledger.ErrNotFound) {
		return &Run{Skipped: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if !snap.Eligible {
		return &Run{Skipped: true}, nil
	}
	m := snap.Memory
	area := ""
	if m.Decision != nil {
		area = m.Decision.Area
	}
	cmd := &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: actor, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: args.IdempotencyKey()},
		Memory: m.ID, Version: args.Version, Mode: args.Mode, Round: args.Round, Force: args.Force,
		Outcome: ledger.OutcomeNone,
	}
	run := &Run{Timings: timings}

	// Stage 0: repeats, with no model.
	t0 := j.now()
	if args.Mode == ledger.JudgeProposal && !args.SkipFold {
		if hit, ok := Stage0(m.Statement, snap.Exact, snap.Near); ok {
			one := 1.0
			cmd.Outcome, cmd.Target = hit.Outcome, hit.Candidate.ID
			cmd.Verdict = ledger.Verdict{Stage: hit.Stage, Relation: ledger.RelationDuplicate, Related: hit.Candidate.ID,
				Confidence: &one, Candidates: []ledger.VerdictCandidate{{MemoryID: hit.Candidate.ID, Ref: hit.Candidate.Ref,
					Sets: []string{string(hit.Stage)}, Relation: ledger.RelationDuplicate, Confidence: &one}}}
		}
	}
	timings["stage0_ms"] = j.now().Sub(t0).Milliseconds()

	if cmd.Outcome == ledger.OutcomeNone {
		t1 := j.now()
		var vecs []ledger.JudgeCandidate
		if j.vectors != nil {
			if vecs, err = j.vectors.Similar(ctx, scope, m.SpaceID, m.ID, m.Statement, j.cfg.Candidates); err != nil {
				// Vectors are an extra lane: lexical candidates still stand.
				j.cfg.Log.WarnContext(ctx, "judge: vector candidates failed", "memory", m.Ref, "error", err)
				vecs = nil
			}
		}
		cands := gather(m.Statement, area, snap, vecs, j.cfg.Candidates)
		timings["candidates_ms"] = j.now().Sub(t1).Milliseconds()
		run.Candidates = len(cands)
		j.stage1(ctx, cmd, args.Mode, m, area, cands, run)
	}

	timings["total_ms"] = j.now().Sub(start).Milliseconds()
	cmd.Verdict.Timings = timings
	res, err := j.ledger.Apply(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("judge: record the verdict on %s: %w", m.Ref, err)
	}
	if res.Unchanged {
		return &Run{Skipped: true}, nil
	}
	if len(res.Receipts) > 0 {
		rc := res.Receipts[0]
		run.Receipt = &rc
	}
	run.Stage, run.Relation, run.Outcome = cmd.Verdict.Stage, cmd.Verdict.Relation, cmd.Outcome
	if res.Memory != nil && res.Memory.Judge != nil {
		run.Outcome = res.Memory.Judge.Outcome
		if res.Memory.Judge.Related != nil {
			run.Target = res.Memory.Judge.Related.Ref
		}
	}
	j.count(run.Outcome)
	j.cfg.Log.InfoContext(ctx, "judge: verdict", "metric", "judge_verdict", "memory", m.Ref, "space_id", m.SpaceID.String(),
		"mode", string(args.Mode), "stage", string(run.Stage), "relation", string(run.Relation), "outcome", string(run.Outcome),
		"target", run.Target, "candidates", run.Candidates, "tier", cmd.Verdict.Tier, "error", cmd.Verdict.Error,
		"queue_ms", timings["queue_ms"], "llm_ms", timings["llm_ms"], "strong_ms", timings["strong_ms"],
		"total_ms", timings["total_ms"])
	return run, nil
}

func (j *Judge) count(o ledger.VerdictOutcome) {
	j.Metrics.Judged.Add(1)
	switch o {
	case ledger.OutcomeFolded:
		j.Metrics.Folded.Add(1)
	case ledger.OutcomeSuppressed:
		j.Metrics.Suppressed.Add(1)
	case ledger.OutcomeLinked:
		j.Metrics.Linked.Add(1)
	case ledger.OutcomeSuperseding:
		j.Metrics.Superseding.Add(1)
	case ledger.OutcomeFlagged:
		j.Metrics.Flagged.Add(1)
	}
}

// stage1 asks the model about the candidates and decides the outcome.
func (j *Judge) stage1(ctx context.Context, cmd *ledger.RecordVerdict, mode ledger.JudgeMode, m *ledger.Memory, area string, cands []candidate, run *Run) {
	v := &cmd.Verdict
	v.Stage, v.Relation = ledger.StageNone, ledger.RelationNone
	for _, c := range cands {
		v.Candidates = append(v.Candidates, ledger.VerdictCandidate{MemoryID: c.ID, Ref: c.Ref, Sets: c.sets, Relation: ledger.RelationNone})
	}
	if len(cands) == 0 || j.classifier == nil {
		return
	}
	p := Proposal{Ref: m.Ref, Statement: m.Statement, Kind: string(m.Kind), Section: string(m.Section), Area: textsig.NormalizeKey(area)}
	for _, s := range m.Sources {
		p.Sources = append(p.Sources, SourceSummary{Kind: string(s.Kind), Ref: s.Ref, URI: s.URI, Quote: s.Quote})
	}
	in := make([]Candidate, len(cands))
	for i, c := range cands {
		in[i] = Candidate{Ref: c.Ref, Statement: c.Statement, Kind: string(c.Kind), Section: string(c.Section),
			Area: c.Area, InForce: c.InForce, Keyed: c.keyed}
	}
	cls, err := j.classifier.Classify(ctx, p, in)
	run.Calls = cls.Calls
	run.Timings["llm_ms"], run.Timings["strong_ms"] = cls.LLM, cls.Strong
	if err != nil {
		// Never block Review: the proposal stays unflagged, the failure is
		// on the verdict and in the logs.
		j.Metrics.LLMFailed.Add(1)
		j.cfg.Log.WarnContext(ctx, "judge: stage 1 failed", "metric", "judge_llm_failed", "memory", m.Ref,
			"calls", cls.Calls, "error", err)
		v.Stage, v.Error, cmd.Outcome = ledger.StageLLM, "llm_failed", ledger.OutcomeFailed
		return
	}
	if cls.Error != "" {
		j.Metrics.EscalationFailed.Add(1)
		j.cfg.Log.WarnContext(ctx, "judge: the strong tier didn't confirm a verdict on a decision", "metric",
			"judge_escalation_failed", "memory", m.Ref)
	}
	v.Stage, v.Error, v.Tier, v.Model = ledger.StageLLM, cls.Error, cls.Tier, cls.Model
	for i, pr := range cls.Pairs {
		conf := pr.Confidence
		v.Candidates[i].Relation, v.Candidates[i].Confidence, v.Candidates[i].Tier = pr.Relation, &conf, pr.Tier
	}
	d := Decide(mode, m.Kind == ledger.KindDecision, m.Statement, in, cls.Pairs, j.cfg.Thresholds)
	cmd.Outcome = d.Outcome
	if d.Pair >= 0 {
		pr := cls.Pairs[d.Pair]
		conf := pr.Confidence
		v.Relation, v.Related, v.Confidence = pr.Relation, cands[d.Pair].ID, &conf
		v.Rationale, v.MergedStatement, v.Tier = pr.Rationale, pr.MergedStatement, pr.Tier
		if pr.Tier == ledger.TierStrong {
			v.Model = cls.StrongModel
		}
		if d.Outcome != ledger.OutcomeNone {
			cmd.Target = cands[d.Pair].ID
		}
		for _, i := range d.Also {
			cmd.AlsoConflicts = append(cmd.AlsoConflicts, cands[i].ID)
		}
	} else {
		v.Relation = ledger.RelationUnrelated
	}
	if j.cfg.Conditions && mode == ledger.JudgeProposal {
		cmd.Conditions = validConditions(cls.Conditions, m.Statement, m.Sources, j.now())
	}
}

// Decision is what to do with a classification.
type Decision struct {
	Outcome ledger.VerdictOutcome
	// Pair is the index of the pair the outcome (or, for none, the most
	// notable verdict) is about; -1 when every candidate is unrelated.
	Pair int
	// Also are further decisions in force the memory contradicts.
	Also []int
}

// Decide turns the pairs into one outcome, in precedence order:
//
//  1. a contradiction of a decision in force: flag (rule 11). An implicit
//     update of a decision in force counts too: a person settles it.
//  2. an explicit change of a keyed decision in force, by a decision
//     proposal: link as superseding it (TEPA). Keeping it supersedes the
//     decision.
//  3. a duplicate of a kept memory, with the same numbers: fold.
//  4. an update of a kept fact: link, so Review shows a diff.
//
// Each needs its threshold; a pair the strong tier didn't confirm never
// acts. A kept memory (mode kept) is only ever flagged.
func Decide(mode ledger.JudgeMode, proposalIsDecision bool, statement string, cands []Candidate, pairs []Pair, th Thresholds) Decision {
	best := func(ok func(i int, p Pair) bool) []int {
		var idx []int
		for i, p := range pairs {
			if !p.Unconfirmed && ok(i, p) {
				idx = append(idx, i)
			}
		}
		slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(pairs[b].Confidence, pairs[a].Confidence) })
		return idx
	}
	supersedes := func(i int, p Pair) bool {
		return proposalIsDecision && cands[i].InForce && cands[i].Keyed && p.ExplicitChange &&
			(p.Relation == ledger.RelationUpdates || p.Relation == ledger.RelationContradicts) && p.Confidence >= th.Supersede
	}
	conflicts := best(func(i int, p Pair) bool {
		return cands[i].InForce && !supersedes(i, p) && p.Confidence >= th.Contradicts &&
			(p.Relation == ledger.RelationContradicts || p.Relation == ledger.RelationUpdates)
	})
	if len(conflicts) > 0 {
		return Decision{Outcome: ledger.OutcomeFlagged, Pair: conflicts[0], Also: conflicts[1:]}
	}
	if mode == ledger.JudgeKept {
		return Decision{Outcome: ledger.OutcomeNone, Pair: notable(pairs)}
	}
	if s := best(supersedes); len(s) > 0 {
		return Decision{Outcome: ledger.OutcomeSuperseding, Pair: s[0]}
	}
	if d := best(func(i int, p Pair) bool {
		// The deterministic half of Graphiti's rule backs the model's: a
		// "duplicate" with different numbers is at most an update.
		return p.Relation == ledger.RelationDuplicate && p.Confidence >= th.Duplicate &&
			slices.Equal(sortedNumbers(statement), sortedNumbers(cands[i].Statement))
	}); len(d) > 0 {
		return Decision{Outcome: ledger.OutcomeFolded, Pair: d[0]}
	}
	if u := best(func(i int, p Pair) bool {
		return !cands[i].InForce && p.Confidence >= th.Updates &&
			(p.Relation == ledger.RelationUpdates || (p.Relation == ledger.RelationDuplicate && p.Confidence >= th.Duplicate))
	}); len(u) > 0 {
		return Decision{Outcome: ledger.OutcomeLinked, Pair: u[0]}
	}
	return Decision{Outcome: ledger.OutcomeNone, Pair: notable(pairs)}
}

// notable is the most confident pair that isn't unrelated, or -1.
func notable(pairs []Pair) int {
	at := -1
	for i, p := range pairs {
		if p.Relation != ledger.RelationUnrelated && p.Relation != ledger.RelationNone && (at < 0 || p.Confidence > pairs[at].Confidence) {
			at = i
		}
	}
	return at
}

func sortedNumbers(s string) []string {
	n := textsig.Numbers(s)
	slices.Sort(n)
	return n
}
