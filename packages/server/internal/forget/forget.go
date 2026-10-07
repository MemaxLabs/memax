// Package forget carries a Forget to every copy Memax holds (plan 25
// §5.13 step 2, rule 7). The ledger's Forget command purges the words from
// Postgres in one transaction and queues forget_propagate (River) in it;
// this package runs that job:
//
//  1. the forget ledger: the op's ids and refs (never words) are copied to
//     object storage, which a database restore can't take back, so
//     cmd/v2-reapply-forgets can re-apply them after a point-in-time
//     restore;
//  2. targets: every target that held a forgotten memory is compiled again
//     now (compile.Service.Run, without the quiet window), and its step
//     records the new run: done for MCP and copy-out targets, waiting for
//     delivery for files (the daemon writes them; the Tombstone page reads
//     delivery live), held when the file has a hand edit Memax won't write
//     over (rule 6), stopped when compiling was stopped;
//  3. artifacts: every stored compile output that held it, and every drift
//     observation of the space, is re-rendered without the lines citing it
//     (a retired space's are deleted);
//  4. caches: every process's in-memory copies (the compiled digest, the
//     embeddings cache) are dropped, by a Redis signal and locally.
//
// The agents' notices were queued in the Forget's own transaction. The
// job is idempotent (each step is skipped once done) and completes the
// tombstone when every step has an answer: the SLO is a minute from the
// Forget's commit.
package forget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Compiler is what propagation needs from the compile coordinator.
type Compiler interface {
	Run(ctx context.Context, args ledger.CompileTargetArgs, opts compile.RunOptions) (compile.Outcome, error)
	RedactArtifacts(ctx context.Context, set ledger.ArtifactSet, refs []string) (int, error)
	DeleteArtifacts(ctx context.Context, keys []string) (int, error)
	PutJSON(ctx context.Context, key string, v any) error
}

// Propagator runs forget_propagate.
type Propagator struct {
	ledger  *ledger.Ledger
	compile Compiler
	purge   Purger
	log     *slog.Logger
	now     func() time.Time
}

// New returns a Propagator, or nil without a ledger. compile may be nil
// (compiling isn't configured): the targets then wait for the compile
// sweeper and the artifacts step can't run, so the job retries.
func New(l *ledger.Ledger, c Compiler, p Purger, log *slog.Logger) *Propagator {
	if l == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	if p == nil {
		p = NopPurger{}
	}
	return &Propagator{ledger: l, compile: c, purge: p, log: log.With("component", "forget"), now: time.Now}
}

// ErrNoCompiler: a target or artifact step needs the compile coordinator.
var ErrNoCompiler = errors.New("forget: compiling isn't configured (COMPILE_SERVICE_URL and object storage)")

// Report is what one run did.
type Report struct {
	Done     bool
	Targets  int
	Rewrote  int
	Held     int
	Stopped  int
	Redacted int
	Deleted  int
	Duration time.Duration
}

// Run carries one Forget as far as it can, and completes its tombstone
// when every step has an answer.
func (p *Propagator) Run(ctx context.Context, args ledger.ForgetPropagateArgs) (Report, error) {
	var rep Report
	scope, err := p.ledger.SpaceScope(ctx, args.SpaceID)
	if errors.Is(err, ledger.ErrNotFound) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	op, err := p.ledger.ForgetOp(ctx, scope, args.OpID)
	if errors.Is(err, ledger.ErrNotFound) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	pending := 0
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, s := range op.Steps {
		if s.Status != ledger.PropagationPending {
			if s.Kind == "target" {
				rep.Targets++
				switch s.Status {
				case ledger.PropagationDone:
					rep.Rewrote++
				case ledger.PropagationHeld:
					rep.Held++
				case ledger.PropagationStopped:
					rep.Stopped++
				}
			}
			continue
		}
		var err error
		switch s.Kind {
		case "ledger":
			err = p.copyLedger(ctx, scope, op, s)
		case "target":
			rep.Targets++
			var status string
			status, err = p.recompile(ctx, scope, op, s)
			switch status {
			case ledger.PropagationDone:
				rep.Rewrote++
			case ledger.PropagationHeld:
				rep.Held++
			case ledger.PropagationStopped:
				rep.Stopped++
			}
		case "artifacts", "attachments":
			// attachments: a forgotten note's V1 attached files, deleted by
			// key (their detail lists them, as a retired space's artifacts).
			err = p.artifacts(ctx, scope, op, s, &rep)
		case "caches":
			p.purge.Purge(ctx, op.SpaceID)
			err = p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationDone, nil)
		}
		if err != nil {
			pending++
			keep(fmt.Errorf("forget: %s step: %w", s.Kind, err))
		}
	}
	if pending > 0 {
		return rep, firstErr
	}
	rep.Duration = p.now().Sub(op.ForgottenAt)
	if op.Status != ledger.TombstoneDone {
		summary := map[string]any{
			"targets": rep.Targets, "rewritten": rep.Rewrote, "held": rep.Held, "stopped": rep.Stopped,
			"artifacts": rep.Redacted + rep.Deleted, "duration_ms": rep.Duration.Milliseconds(),
		}
		if err := p.ledger.CompleteForget(ctx, scope, op.OpID, summary); err != nil {
			return rep, err
		}
		p.log.InfoContext(ctx, "forget: propagated", "op", op.OpID.String(), "space_id", op.SpaceID.String(),
			"refs", len(op.Refs), "targets", rep.Targets, "held", rep.Held, "artifacts", rep.Redacted+rep.Deleted,
			"duration_ms", rep.Duration.Milliseconds(), "metric", "forget_propagation_ms")
	}
	rep.Done = true
	return rep, nil
}

// copyLedger writes the op's forget-ledger entry to object storage.
func (p *Propagator) copyLedger(ctx context.Context, scope ledger.Scope, op *ledger.ForgetOpView, s ledger.ForgetStep) error {
	if p.compile == nil {
		return ErrNoCompiler
	}
	entry, err := p.ledger.ForgetLedger(ctx, scope, op.SpaceID, op.OpID)
	if err != nil {
		return err
	}
	if op.Retired {
		entry.Retired = true
	}
	key := ledger.ForgetLedgerKey(op.SpaceID, op.OpID)
	if err := p.compile.PutJSON(ctx, key, entry); err != nil {
		return err
	}
	return p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationDone, map[string]any{"key": key})
}

// recompile compiles one target that held a forgotten memory, now, and
// records the run its step waits on.
func (p *Propagator) recompile(ctx context.Context, scope ledger.Scope, op *ledger.ForgetOpView, s ledger.ForgetStep) (string, error) {
	if s.Target == nil {
		return "", p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationDone, nil)
	}
	t, err := p.ledger.GetTarget(ctx, scope, *s.Target)
	if errors.Is(err, ledger.ErrNotFound) {
		return ledger.PropagationStopped, p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationStopped, nil)
	}
	if err != nil {
		return "", err
	}
	if t.SyncState == ledger.SyncOff {
		return ledger.PropagationStopped, p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationStopped, nil)
	}
	gen, _ := s.Detail["generation"].(float64)
	if t.CompiledGen < int64(gen) {
		if p.compile == nil {
			return "", ErrNoCompiler
		}
		out, err := p.compile.Run(ctx, ledger.CompileTargetArgs{TargetID: t.ID, SpaceID: t.SpaceID}, compile.RunOptions{NoWait: true})
		if err != nil {
			return "", err
		}
		if out == compile.Snooze {
			return "", fmt.Errorf("target %s changed while compiling; again", t.Label)
		}
		if t, err = p.ledger.GetTarget(ctx, scope, *s.Target); err != nil {
			return "", err
		}
		if t.CompiledGen < int64(gen) {
			return "", fmt.Errorf("target %s isn't compiled past the forget yet", t.Label)
		}
	}
	detail := map[string]any{}
	if t.LastCompile != nil && t.LastCompile.Status != ledger.CompileFailed {
		detail["compile"] = t.LastCompile.Ref
	} else if t.LastCompile != nil && t.LastCompile.Status == ledger.CompileFailed {
		return ledger.PropagationFailed, p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationFailed,
			map[string]any{"compile": t.LastCompile.Ref})
	}
	status := ledger.PropagationDone
	if t.SyncState == ledger.SyncDrifted || t.OpenDrift > 0 {
		status = ledger.PropagationHeld
	}
	return status, p.ledger.MarkForgetStep(ctx, scope, s.ID, status, detail)
}

// artifacts re-renders (or, for a retired space, deletes) the stored
// artifacts that held a forgotten memory.
func (p *Propagator) artifacts(ctx context.Context, scope ledger.Scope, op *ledger.ForgetOpView, s ledger.ForgetStep, rep *Report) error {
	if p.compile == nil {
		return ErrNoCompiler
	}
	if raw, ok := s.Detail["delete"].([]any); ok {
		keys := make([]string, 0, len(raw))
		for _, k := range raw {
			if key, ok := k.(string); ok && key != "" {
				keys = append(keys, key)
			}
		}
		n, err := p.compile.DeleteArtifacts(ctx, keys)
		rep.Deleted += n
		if err != nil {
			return err
		}
		return p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationDone, map[string]any{"count": n})
	}
	set, err := p.ledger.ForgetArtifacts(ctx, scope, op.SpaceID, op.Refs)
	if err != nil {
		return err
	}
	n, err := p.compile.RedactArtifacts(ctx, set, op.Refs)
	rep.Redacted += n
	if err != nil {
		return err
	}
	return p.ledger.MarkForgetStep(ctx, scope, s.ID, ledger.PropagationDone, map[string]any{"count": n})
}

// Worker runs forget_propagate jobs.
type Worker struct {
	river.WorkerDefaults[ledger.ForgetPropagateArgs]
	Propagator *Propagator
}

// Timeout bounds one attempt.
func (w *Worker) Timeout(*river.Job[ledger.ForgetPropagateArgs]) time.Duration {
	return 2 * time.Minute
}

// NextRetry retries quickly: the SLO is a minute.
func (w *Worker) NextRetry(job *river.Job[ledger.ForgetPropagateArgs]) time.Time {
	d := time.Duration(1<<min(job.Attempt, 8)) * time.Second
	return time.Now().Add(d)
}

// Work runs one attempt.
func (w *Worker) Work(ctx context.Context, job *river.Job[ledger.ForgetPropagateArgs]) error {
	if w.Propagator == nil {
		return river.JobCancel(errors.New("forget: no ledger"))
	}
	_, err := w.Propagator.Run(ctx, job.Args)
	return err
}

// MaxWorkers is the forget queue's concurrency in the worker.
const MaxWorkers = 4

// AddWorkers registers the forget worker.
func AddWorkers(workers *river.Workers, p *Propagator) {
	river.AddWorker(workers, &Worker{Propagator: p})
}
