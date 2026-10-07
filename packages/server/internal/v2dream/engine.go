// Package v2dream is Dream on the V2 record (plan 25 §5.10, Phase 2 epic
// 2.2): a space's overnight upkeep, done in the open. Each run reads what
// is new since the last edition, decides a few small actions, and hands
// them to the ledger as one PublishEdition command, applied as Dream with
// a receipt per action that cites the edition. Every action is undoable
// (ledger.UndoDreamAction) and nothing is rewritten silently.
//
// The phases, in V2's terms (V1's internal/dreams merged and archived;
// topics, restructure and boards are retired):
//
//   - fold: notes become lineage of the kept memory they support; what is
//     new in them becomes a proposal citing them (a model call per batch).
//     A memory's words change only through a proposal a person keeps.
//   - dedupe: a proposal that repeats one already waiting is folded into
//     it before Review: exact and near-verbatim repeats with no model, the
//     rest with the judge's classifier.
//   - conflict: what was kept or changed since the last edition (and what
//     the judge never got to) is checked against the decisions in force and
//     the nearest kept facts, with the judge's classifier; a contradiction
//     is flagged and linked for a person. Conflicts the judge flagged since
//     are listed too.
//   - stale: kept memories past their stale_after date are flagged.
//   - fade: kept memories nobody read or touched in 60 days fade
//     (Ledger.FadeCandidates: never one in a file whose loads Memax can't
//     observe, a decision in force, a flagged memory, or one the Brief
//     places). Restorable, never deleted.
//   - brief: a few small, cited changes to the Brief in force (place, move,
//     reword, add a line), never a rewrite; an uncited line fails.
//
// Cost: a run happens only when the space has new input since its last
// edition (notes, record changes, a stale_after date that came due), its
// model calls are capped (DREAM_MAX_CALLS), and the plan's cadence and
// run-now caps bound how often (D9). Scheduling is the catch-up sweep in
// jobs.go: no River Pro (§5.17).
package v2dream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// actor is Dream as receipts name it.
var actor = ledger.Actor{Kind: policy.ActorDream, Name: "Dream", Credential: policy.CredentialSession}

// Searcher finds the kept memories nearest a text (v2recall.Searcher).
type Searcher interface {
	Search(ctx context.Context, scope ledger.Scope, q v2recall.Query) (v2recall.Result, error)
}

// Engine runs Dream.
type Engine struct {
	ledger   *ledger.Ledger
	model    Model
	searcher Searcher
	cfg      Config
	now      func() time.Time
	log      *slog.Logger
}

// Option configures an Engine.
type Option func(*Engine)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(e *Engine) { e.now = now } }

// WithSearcher sets how candidates are found (nil: no fold candidates, and
// conflicts are checked against decisions in force only).
func WithSearcher(s Searcher) Option { return func(e *Engine) { e.searcher = s } }

// New returns the engine, or nil without a ledger. A nil model runs the
// phases that need none (exact repeats, stale, fade).
func New(l *ledger.Ledger, m Model, cfg Config, opts ...Option) *Engine {
	if l == nil {
		return nil
	}
	cfg = cfg.withDefaults()
	e := &Engine{ledger: l, model: m, cfg: cfg, now: time.Now, log: cfg.Log}
	if !cfg.Primary.Enabled() {
		e.model = nil
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Config is the engine's configuration.
func (e *Engine) Config() Config { return e.cfg }

// Modeled reports whether the phases that need a model run.
func (e *Engine) Modeled() bool { return e != nil && e.model != nil }

// Outcome is what one run did.
type Outcome struct {
	// Ran is false when nothing was new (no input, no run) or a cap held it.
	Ran     bool
	Reason  string
	Edition *ledger.DreamEdition
	// Plan is what the run decided, before the ledger re-checked it (dry
	// runs and the eval read it).
	Plan  []ledger.PlannedAction
	Stats ledger.DreamStats
}

// The reasons a run didn't publish.
const (
	ReasonNoInput = "no_input"
	ReasonCap     = "cap"
	ReasonDryRun  = "dry_run"
	ReasonGone    = "gone"
)

// Run runs Dream on one space for one slot and publishes its edition.
func (e *Engine) Run(ctx context.Context, args ledger.DreamSpaceArgs) (Outcome, error) {
	return e.run(ctx, args, e.cfg.DryRun)
}

// Plan runs the phases and returns the plan without publishing anything.
func (e *Engine) Plan(ctx context.Context, args ledger.DreamSpaceArgs) (Outcome, error) {
	return e.run(ctx, args, true)
}

func (e *Engine) run(ctx context.Context, args ledger.DreamSpaceArgs, dry bool) (Outcome, error) {
	if e == nil {
		return Outcome{}, ledger.ErrDisabled
	}
	started := e.now()
	scope, err := e.ledger.SpaceScope(ctx, args.SpaceID)
	if errors.Is(err, ledger.ErrNotFound) {
		return Outcome{Reason: ReasonGone}, nil
	}
	if err != nil {
		return Outcome{}, err
	}
	if args.Trigger == ledger.DreamManual {
		if ok, err := e.manualAllowed(ctx, scope, args.SpaceID); err != nil || !ok {
			return Outcome{Reason: ReasonCap}, err
		}
	}
	snap, err := e.ledger.DreamSnapshot(ctx, scope, args.SpaceID, ledger.DreamSnapshotOptions{
		MaxNotes: e.cfg.MaxNotes, FadeAfter: e.cfg.FadeAfter})
	if err != nil {
		return Outcome{}, err
	}
	if !snap.HasInput() {
		e.log.InfoContext(ctx, "dream: nothing new since the last edition, so no run",
			"space_id", args.SpaceID.String(), "slot", args.Slot, "metric", "dream_skipped_no_input")
		return Outcome{Reason: ReasonNoInput}, nil
	}

	u := &usage{}
	ctx = withUsage(ctx, u)
	r := &runState{e: e, snap: snap, scope: scope, timings: map[string]int64{}, considered: map[string]int{}}
	if e.model != nil {
		r.model = &countingModel{inner: e.model, budget: e.cfg.MaxCalls}
	}
	for _, ph := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"fold", r.fold}, {"dedupe", r.dedupe}, {"conflict", r.conflicts}, {"stale", r.stale},
		{"fade", r.fade}, {"brief", r.brief},
	} {
		t := time.Now()
		if err := ph.fn(ctx); err != nil {
			if ctx.Err() != nil {
				return Outcome{}, ctx.Err()
			}
			// A phase that fails (a model that doesn't answer) leaves the
			// rest to run: Dream degrades, it doesn't block.
			e.log.WarnContext(ctx, "dream: a phase failed; the edition goes ahead without it",
				"phase", ph.name, "space_id", args.SpaceID.String(), "error", err.Error(), "metric", "dream_phase_failed")
			r.considered["failed_"+ph.name]++
		}
		r.timings[ph.name] = time.Since(t).Milliseconds()
	}
	stats := ledger.DreamStats{Timings: r.timings, Plan: string(e.cfg.Plan), Considered: r.considered, Changes: snap.Changes}
	if e.model != nil {
		mu := u.snapshot()
		mu.ZDR = e.cfg.ZeroDataRetention
		stats.Model = &mu
	}
	out := Outcome{Plan: r.actions, Stats: stats}
	notes := make([]ledger.NoteRead, len(snap.Notes))
	for i, n := range snap.Notes {
		notes[i] = n.NoteRead
	}
	var cursor *ledger.NoteCursor
	if len(snap.Notes) > 0 {
		last := snap.Notes[len(snap.Notes)-1]
		cursor = &ledger.NoteCursor{At: last.CreatedAt, ID: last.ID}
	}
	if dry {
		counts := map[string]int{}
		for _, a := range r.actions {
			counts[string(a.Kind)]++
		}
		e.log.InfoContext(ctx, "dream: dry run planned an edition", "space_id", args.SpaceID.String(),
			"notes", len(notes), "changes", snap.Changes, "actions", counts, "surfaced", len(snap.Surfaced))
		out.Reason = ReasonDryRun
		return out, nil
	}
	pub := &ledger.PublishEdition{
		Meta: ledger.Meta{Actor: actor, Scope: scope, Via: policy.ViaSystem,
			IdempotencyKey: fmt.Sprintf("dream:%s:%s", args.SpaceID, args.Slot.UTC().Format(time.RFC3339Nano))},
		SpaceID: args.SpaceID, Slot: args.Slot, Trigger: args.Trigger, RequestedBy: args.RequestedBy,
		Since: snap.Since, Until: snap.Now, Cursor: cursor, StartedAt: started,
		Notes: notes, Actions: r.actions, Surfaced: snap.Surfaced, Stats: stats,
	}
	if pub.Trigger == "" {
		pub.Trigger = ledger.DreamScheduled
	}
	if pub.Trigger == ledger.DreamScheduled {
		pub.RequestedBy = uuid.Nil
	}
	res, err := e.ledger.Apply(ctx, pub)
	if err != nil {
		return Outcome{}, err
	}
	if res.Outcome == ledger.OutcomeRefused {
		return Outcome{}, fmt.Errorf("dream: the ledger refused the edition: %s", res.Policy.Message)
	}
	out.Ran, out.Edition = true, res.Edition
	if res.Edition != nil {
		e.log.InfoContext(ctx, "dream: published an edition", "space_id", args.SpaceID.String(), "edition", res.Edition.Ref,
			"notes", res.Edition.NotesRead, "actions", len(r.actions), "applied", res.Edition.Counts,
			"skipped", res.Edition.Stats.Skipped, "model_calls", callsOf(stats), "metric", "dream_published")
	}
	return out, nil
}

func callsOf(s ledger.DreamStats) int {
	if s.Model == nil {
		return 0
	}
	return s.Model.Calls
}

// manualAllowed applies the plan's run-now cap.
func (e *Engine) manualAllowed(ctx context.Context, scope ledger.Scope, space uuid.UUID) (bool, error) {
	ok, _, err := e.ManualAllowed(ctx, scope, space)
	return ok, err
}

// ManualAllowed reports whether the space may run now, under its plan's
// cap (DREAM_MANUAL_PER_DAY on Pro, DREAM_MANUAL_PER_WEEK on Free), and
// when the next run-now is allowed if not.
func (e *Engine) ManualAllowed(ctx context.Context, scope ledger.Scope, space uuid.UUID) (bool, time.Duration, error) {
	window, limit := 24*time.Hour, e.cfg.ManualPerDay
	if e.cfg.Plan == PlanFree {
		window, limit = 7*24*time.Hour, e.cfg.ManualPerWeek
	}
	_, manual, err := e.ledger.RecentEditions(ctx, scope, space, e.now().Add(-window))
	if err != nil {
		return false, 0, err
	}
	if manual >= limit {
		return false, window, nil
	}
	return true, 0, nil
}
