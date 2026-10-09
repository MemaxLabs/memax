package compile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// The generation-counter recipe (plan 25 §5.7):
//
//  1. A command that changes what compiles bumps targets.dirty_gen and
//     InsertTx-es compile_target in the same transaction (internal/ledger).
//     The job is unique by args over every unfinished state.
//  2. The job waits in-process for a quiet window (Quiet since the last
//     bump, capped at QuietCap after the job started) and compiles the
//     generation g it then reads, from one snapshot.
//  3. Recording the run locks the target. If dirty_gen moved past g, the
//     run is not recorded and the job snoozes, so it compiles again; after
//     SnoozeCap snoozes it records g anyway and leaves the rest to the
//     sweeper. Otherwise compiled_gen = g.
//  4. A 30 s sweeper re-enqueues every target where dirty_gen > compiled_gen.
//
// Unchanged output is skipped: when the input hash equals the latest
// run's, or compiling under the latest run's ID and time gives that run's
// exact bytes, no new run is recorded and the target simply settles.

// Defaults for the recipe.
const (
	DefaultQuiet     = 1500 * time.Millisecond
	DefaultQuietCap  = 4 * time.Second
	DefaultSnoozeCap = 5
	// MaxObservedBytes bounds a reported file: Codex reads at most 32 KiB
	// of AGENTS.md, and a hand-edited file far beyond that is not a file
	// Memax compiled.
	MaxObservedBytes = 1 << 20
)

// Config tunes a Service.
type Config struct {
	// AppBaseURL is the web app's origin; headers point at
	// <AppBaseURL>/<space>/brief. Defaults to https://memax.app.
	AppBaseURL string
	Quiet      time.Duration
	QuietCap   time.Duration
	SnoozeCap  int
	Now        func() time.Time
	Log        *slog.Logger
}

// Service is the compile coordinator: it builds compile inputs from the
// record, calls the compile service, stores artifacts and records runs,
// all record reads and writes through the ledger. It also serves what
// sits around a compile: the preview, observations of hand edits, and the
// drift view.
type Service struct {
	ledger   *ledger.Ledger
	compiler Compiler
	store    objectstore.Store
	cfg      Config
}

// New returns the coordinator, or nil when any dependency is missing (no
// database, no compile service, no object store): nil means compiling is
// disabled, and dirty targets wait for it.
func New(l *ledger.Ledger, c Compiler, store objectstore.Store, cfg Config) *Service {
	if l == nil || isNil(c) || isNil(store) {
		return nil
	}
	if cfg.Quiet == 0 {
		cfg.Quiet = DefaultQuiet
	}
	if cfg.QuietCap == 0 {
		cfg.QuietCap = DefaultQuietCap
	}
	if cfg.QuietCap < cfg.Quiet {
		cfg.QuietCap = cfg.Quiet
	}
	if cfg.SnoozeCap == 0 {
		cfg.SnoozeCap = DefaultSnoozeCap
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Service{ledger: l, compiler: c, store: store, cfg: cfg}
}

// isNil reports whether an interface holds nothing or a nil pointer.
func isNil(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case *Client:
		return x == nil
	}
	return false
}

// Outcome is what one compile job attempt ended with.
type Outcome int

// The outcomes.
const (
	// Done: the target is compiled (or there was nothing to do).
	Done Outcome = iota
	// Snooze: the target changed while compiling; run the job again.
	Snooze
)

// RunOptions describe the job attempt.
type RunOptions struct {
	// Snoozes is how often the job already snoozed.
	Snoozes int
	// NoWait skips the quiet window (tests, "compile everything now").
	NoWait bool
	// Finish completes the job in the transaction that records or settles
	// its generation (see ledger.Finisher). The River worker passes
	// river.JobCompleteTx; without it the job completes after Run returns.
	Finish ledger.Finisher
}

// Run compiles one target: steps 2 and 3 of the recipe.
func (s *Service) Run(ctx context.Context, args ledger.CompileTargetArgs, opts RunOptions) (Outcome, error) {
	start := s.cfg.Now()
	scope, err := s.ledger.SpaceScope(ctx, args.SpaceID)
	if errors.Is(err, ledger.ErrNotFound) {
		return Done, nil
	}
	if err != nil {
		return Done, err
	}
	if !opts.NoWait {
		if err := s.quiet(ctx, scope, args.TargetID, start); err != nil {
			return Done, err
		}
	}

	snap, err := s.ledger.CompileSnapshot(ctx, scope, args.TargetID)
	if errors.Is(err, ledger.ErrNotFound) {
		return Done, nil
	}
	if err != nil {
		return Done, err
	}
	t := snap.View.Target
	gen := t.DirtyGen
	// Nothing to compile (already compiled, stopped, or no Brief): settle,
	// which finishes the job under the target's lock, so a change that
	// lands meanwhile snoozes it instead of being lost.
	if t.SyncState == ledger.SyncOff || t.CompiledGen >= gen || snap.Brief == nil {
		return s.settle(ctx, scope, t, gen, opts)
	}
	now := s.cfg.Now().UTC().Truncate(time.Microsecond) // what Postgres keeps, so a rerun can reproduce the header
	b, err := buildInput(snap, s.cfg.AppBaseURL, now)
	if err != nil {
		return Done, err
	}
	if err := s.fillCurrent(ctx, snap.View, b.input); err != nil {
		return Done, err
	}
	hash := inputHash(b.input, b.stale)

	// Unchanged output: same input, or the same bytes under the latest
	// run's own ID and time.
	if last := snap.View.LastGood; last != nil {
		if last.InputSHA256 == hash {
			return s.settle(ctx, scope, t, gen, opts)
		}
		sameStale := true
		if lb, err := buildInput(snap, s.cfg.AppBaseURL, last.CompiledAt); err == nil {
			sameStale = slices.Equal(lb.stale, b.stale)
		}
		if sameStale {
			b.input.Compile = Run{ID: last.Ref, At: last.CompiledAt.UTC().Format(time.RFC3339Nano)}
			res, err := s.compiler.Compile(ctx, b.input)
			var ie *InputError
			if err != nil && !errors.As(err, &ie) {
				return Done, err
			}
			if err == nil {
				if out, _ := ledger.OutputManifests(outputs(res)); out == last.OutputSHA256 {
					return s.settle(ctx, scope, t, gen, opts)
				}
			}
		}
	}

	ref, err := s.ledger.ReserveCompileRef(ctx, scope, t.SpaceID)
	if err != nil {
		return Done, err
	}
	b.input.Compile = Run{ID: ref, At: now.Format(time.RFC3339Nano)}
	res, err := s.compiler.Compile(ctx, b.input)
	cmd := &ledger.RecordCompile{
		Meta:   ledger.Meta{Actor: actor, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: "compile:" + uuid.NewString()},
		Target: t.ID, Ref: ref, Generation: gen, BriefID: snap.Brief.ID, BriefVersion: snap.Brief.Version,
		InputSHA256: hash, EnqueuedAt: t.DirtyAt, StartedAt: start, CompiledAt: now,
		AllowBehind: opts.Snoozes >= s.cfg.SnoozeCap, Finish: opts.Finish,
	}
	if cmd.CompiledAt.Before(cmd.StartedAt) {
		cmd.CompiledAt = cmd.StartedAt
	}
	var ie *InputError
	switch {
	case errors.As(err, &ie):
		// The compiler refused the input: a permanent failure, recorded so
		// people see it. The message names fields, never memory text.
		cmd.Error = truncate(ie.Error(), ledger.MaxCompileError)
		s.cfg.Log.ErrorContext(ctx, "compile: input refused", "target_id", t.ID.String(), "compile", ref, "error", ie.Error())
	case err != nil:
		return Done, err
	default:
		outs := outputs(res)
		cmd.Files = outs
		cmd.OutputSHA256, cmd.DriftSHA256 = ledger.OutputManifests(outs)
		cmd.Bytes, cmd.Lines, cmd.Refs, cmd.DroppedForBudget = totals(outs)
		cmd.Warnings = warnings(res, b.warnings)
		cmd.ArtifactKey = compileKey(t.SpaceID, t.ID, ref)
		if err := putJSON(ctx, s.store, cmd.ArtifactKey, newArtifact(ref, res, cmd.Warnings)); err != nil {
			return Done, fmt.Errorf("compile: store %s: %w", ref, err)
		}
	}
	rec, err := s.ledger.Apply(ctx, cmd)
	if err != nil {
		// No run records this artifact, so nothing would ever reach it:
		// Forget re-renders only the artifacts a run records, and the
		// ledger refuses a run citing a memory forgotten while it compiled
		// (MXF02). Left behind, it would keep the forgotten words.
		s.drop(ctx, cmd.ArtifactKey)
		if errors.Is(err, ledger.ErrBehind) {
			return Snooze, nil
		}
		return Done, err
	}
	if rec.Unchanged {
		// Recorded by an earlier attempt, or stopped meanwhile.
		s.drop(ctx, cmd.ArtifactKey)
		return s.settle(ctx, scope, t, gen, opts)
	}
	if rec.Compile != nil {
		s.cfg.Log.InfoContext(ctx, "compile: recorded", "target_id", t.ID.String(), "kind", string(t.Kind),
			"compile", rec.Compile.Ref, "status", string(rec.Compile.Status), "generation", gen,
			"bytes", rec.Compile.Bytes, "since_dirty_ms", s.cfg.Now().Sub(t.DirtyAt).Milliseconds())
	}
	return Done, nil
}

// drop deletes an artifact no run records.
func (s *Service) drop(ctx context.Context, key string) {
	if key == "" {
		return
	}
	if err := s.store.Delete(ctx, key); err != nil {
		s.cfg.Log.WarnContext(ctx, "compile: delete unrecorded artifact", "key", key, "error", err)
	}
}

// quiet waits until the target has been quiet for Quiet since its last
// change, or QuietCap has passed since start. It returns at once when
// there is nothing to compile.
func (s *Service) quiet(ctx context.Context, scope ledger.Scope, targetID uuid.UUID, start time.Time) error {
	for {
		t, err := s.ledger.GetTarget(ctx, scope, targetID)
		if errors.Is(err, ledger.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if t.SyncState == ledger.SyncOff || t.CompiledGen >= t.DirtyGen {
			return nil
		}
		now := s.cfg.Now()
		wait := min(t.DirtyAt.Add(s.cfg.Quiet).Sub(now), start.Add(s.cfg.QuietCap).Sub(now))
		if wait <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// settle records that generation gen needs no new run, and finishes the
// job in the same transaction.
func (s *Service) settle(ctx context.Context, scope ledger.Scope, t *ledger.Target, gen int64, opts RunOptions) (Outcome, error) {
	err := s.ledger.SettleUnchanged(ctx, scope, t.ID, gen, opts.Finish)
	if errors.Is(err, ledger.ErrBehind) {
		if opts.Snoozes >= s.cfg.SnoozeCap {
			return Done, nil // the sweeper comes back for it
		}
		return Snooze, nil
	}
	return Done, err
}

// fillCurrent gives a user-owned shim the file's content now: the hand
// edit Memax accepted as its baseline, or the content it last delivered.
func (s *Service) fillCurrent(ctx context.Context, v *ledger.TargetView, in *Input) error {
	for i := range in.Targets {
		ts := &in.Targets[i]
		if !ts.UserOwned {
			continue
		}
		content, _, _, err := s.baseline(ctx, v, ts.Path)
		if err != nil {
			return err
		}
		ts.Current = &content
	}
	return nil
}

// baseline is what Memax believes a file holds now: an accepted hand
// edit, the delivered run's file, or the latest run's file (never
// delivered). ok is false when Memax has no version of the path at all.
func (s *Service) baseline(ctx context.Context, v *ledger.TargetView, path string) (content, driftSHA string, base *ledger.CompileRun, err error) {
	if o, ok := v.Accepted[path]; ok {
		raw, err := get(ctx, s.store, o.ArtifactKey)
		if err != nil {
			return "", "", nil, err
		}
		return string(raw), o.ObservedSHA, nil, nil
	}
	for _, run := range []*ledger.CompileRun{v.Delivered, v.LastGood} {
		if run == nil || run.ArtifactKey == "" {
			continue
		}
		a, err := loadArtifact(ctx, s.store, run.ArtifactKey)
		if err != nil {
			return "", "", nil, err
		}
		if f, ok := a.file(path); ok {
			return f.Content, f.DriftSHA256, run, nil
		}
	}
	return "", "", nil, nil
}

// Preview is a target's latest good artifact and where it stands
// (TargetPreview).
type Preview struct {
	Target *ledger.Target
	// Compile is the latest good run whose content is shown; nil before
	// the first compile.
	Compile *ledger.CompileRun
	Reads   string
	Files   []ArtifactOutput
	Copies  []ArtifactOutput
}

// Preview reads a target's latest compiled content.
func (s *Service) Preview(ctx context.Context, scope ledger.Scope, targetID uuid.UUID) (*Preview, error) {
	v, err := s.ledger.GetTargetView(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	p := &Preview{Target: v.Target, Compile: v.LastGood, Files: []ArtifactOutput{}, Copies: []ArtifactOutput{}}
	if v.LastGood == nil {
		return p, nil
	}
	a, err := loadArtifact(ctx, s.store, v.LastGood.ArtifactKey)
	if err != nil {
		return nil, err
	}
	p.Reads, p.Files, p.Copies = a.Reads, a.Files, a.Copies
	return p, nil
}

// ObserveInput is a file a device (or GitHub) saw.
type ObserveInput struct {
	Path    string
	Content string
	// ObserverKind is ledger.ObserverDevice or ledger.ObserverGitHub.
	ObserverKind string
	ObserverID   string
	Commit       string
}

// Observe reports a target's file as seen outside Memax. When it matches
// what Memax believes is on disk, nothing is stored and the result is
// Unchanged; otherwise the content is stored, the compiler's parse-back
// says what the edit means, and the ledger records the observation (the
// target drifts).
func (s *Service) Observe(ctx context.Context, meta ledger.Meta, targetID uuid.UUID, in ObserveInput) (ledger.Result, error) {
	if len(in.Content) > MaxObservedBytes {
		return ledger.Result{}, &ledger.ValidationError{Field: "content",
			Message: fmt.Sprintf("is over %d bytes; Memax compiles files far smaller than that", MaxObservedBytes)}
	}
	if !utf8.ValidString(in.Content) || strings.ContainsRune(in.Content, 0) {
		return ledger.Result{}, &ledger.ValidationError{Field: "content", Message: "must be UTF-8 text"}
	}
	v, err := s.ledger.GetTargetView(ctx, meta.Scope, targetID)
	if err != nil {
		return ledger.Result{}, err
	}
	// Check the report before storing anything, so a malformed one leaves
	// nothing behind. The ledger checks again under the target's lock.
	if !ledger.ValidPath(in.Path) || !v.Target.Owns(in.Path) {
		return ledger.Result{}, &ledger.ValidationError{Field: "path",
			Message: fmt.Sprintf("%s isn't a file %s writes; use its path relative to the repository root", in.Path, v.Target.Label)}
	}
	content, driftSHA, base, err := s.baseline(ctx, v, in.Path)
	if err != nil {
		return ledger.Result{}, err
	}
	pb, err := s.compiler.ParseBack(ctx, &ParseBackRequest{Last: LastFile{Content: content, DriftSHA256: driftSHA}, Current: in.Content})
	if err != nil {
		return ledger.Result{}, err
	}
	unchanged := driftSHA != "" && !pb.Drifted
	// The latest output, written but not yet acknowledged, is no hand edit
	// either.
	if v.LastGood != nil && slices.ContainsFunc(v.LastGood.Files, func(f ledger.CompiledOutput) bool {
		return f.Path == in.Path && f.DriftSHA256 == pb.DriftSHA256
	}) {
		unchanged = true
	}
	if unchanged || v.Target.SyncState == ledger.SyncOff {
		return ledger.Result{Outcome: ledger.OutcomeApplied, Target: v.Target, Unchanged: true}, nil
	}
	// A file kept on a disk can still hold a forgotten memory's line (a
	// hand edit Memax won't write over): its lines never come back into
	// Memax, neither in the stored copy nor in what the edit means.
	forgotten, err := s.ledger.ForgottenRefs(ctx, meta.Scope, v.Target.SpaceID)
	if err != nil {
		return ledger.Result{}, err
	}
	stored, _ := StripCited(in.Content, forgotten)
	cmd := &ledger.RecordObservation{
		Meta: meta, Target: targetID, Path: in.Path, ObservedSHA256: pb.DriftSHA256,
		ObserverKind: in.ObserverKind, ObserverID: in.ObserverID, Commit: in.Commit,
		ArtifactKey: observedKey(v.Target.SpaceID, v.Target.ID, pb.DriftSHA256), Bytes: len(in.Content),
		BaseSHA256: driftSHA, Changes: stripChanges(pb.ChangeSet, forgotten),
	}
	if base != nil {
		cmd.BaseCompileID = &base.ID
	}
	if err := putText(ctx, s.store, cmd.ArtifactKey, stored); err != nil {
		return ledger.Result{}, fmt.Errorf("compile: store observation: %w", err)
	}
	return s.ledger.Apply(ctx, cmd)
}

// DriftItem is one file's hand edit, for DriftResolve: what Memax wrote,
// what is there now, and what the edit says.
type DriftItem struct {
	Observation ledger.Observation
	// Compiled is the baseline the edit departs from ("" if none).
	Compiled string
	// BaseCompile is the run that wrote the baseline, when Memax wrote it.
	BaseCompile *ledger.CompileRun
	Observed    string
}

// Drift is a target's open hand edits.
type Drift struct {
	Target *ledger.Target
	Items  []DriftItem
}

// Drift reads a target's open hand edits with both sides of each.
func (s *Service) Drift(ctx context.Context, scope ledger.Scope, targetID uuid.UUID) (*Drift, error) {
	v, err := s.ledger.GetTargetView(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	d := &Drift{Target: v.Target, Items: []DriftItem{}}
	for _, o := range v.Open {
		observed, err := get(ctx, s.store, o.ArtifactKey)
		if err != nil {
			return nil, err
		}
		compiled, _, base, err := s.baseline(ctx, v, o.Path)
		if err != nil {
			return nil, err
		}
		d.Items = append(d.Items, DriftItem{Observation: o, Compiled: compiled, BaseCompile: base, Observed: string(observed)})
	}
	return d, nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
