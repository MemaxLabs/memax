package v2dream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Scheduling (plan 25 §5.17, revised Oct 7: no River Pro). A periodic
// dream_sweep, every DREAM_SWEEP_INTERVAL, runs ledger.DreamSweep: in one
// transaction it moves every due space's due_at past now and queues one
// dream_space job per (space, slot). It is idempotent and catches up:
//
//   - A missed night (the worker down, a leader change) is still due on
//     the next sweep, and runs once, for the latest slot; a space never
//     runs twice for one slot (the job is unique by args over every state,
//     and an edition is unique per slot).
//   - Two sweeps at once skip each other's rows (FOR UPDATE SKIP LOCKED).
//   - The slot is the owner's local night, in their own zone (schedule.go).

// MaxWorkers is the dream queue's concurrency in the worker.
const MaxWorkers = 2

// SweepBatch is how many spaces one sweep looks at.
const SweepBatch = 500

// SweepArgs is the periodic catch-up sweep.
type SweepArgs struct{}

// Kind implements river.JobArgs.
func (SweepArgs) Kind() string { return "dream_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueDream, MaxAttempts: 1}
}

var errNotConfigured = errors.New("dream: not configured (the worker has no database)")

// Sweep runs the catch-up sweep once.
func (e *Engine) Sweep(ctx context.Context, jobs ledger.Jobs) ([]ledger.DreamSpaceArgs, error) {
	if e == nil {
		return nil, errNotConfigured
	}
	now := e.now()
	return e.ledger.DreamSweep(ctx, now, SweepBatch, func(s ledger.SweepSpace) ledger.SweepDecision {
		return e.cfg.Decide(now, s)
	}, jobs)
}

// SweepWorker runs dream_sweep.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Engine *Engine
}

// Work sweeps.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	if w.Engine == nil {
		return river.JobCancel(errNotConfigured)
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("dream sweep: %w", err)
	}
	queued, err := w.Engine.Sweep(ctx, client)
	if err != nil {
		return err
	}
	if len(queued) > 0 {
		w.Engine.log.InfoContext(ctx, "dream: queued spaces", "spaces", len(queued))
	}
	return nil
}

// SpaceWorker runs dream_space.
type SpaceWorker struct {
	river.WorkerDefaults[ledger.DreamSpaceArgs]
	Engine *Engine
}

// Timeout bounds one run: its model calls are capped, each call is bounded.
func (w *SpaceWorker) Timeout(*river.Job[ledger.DreamSpaceArgs]) time.Duration {
	return 15 * time.Minute
}

// Work runs Dream on the space for the slot.
func (w *SpaceWorker) Work(ctx context.Context, job *river.Job[ledger.DreamSpaceArgs]) error {
	if w.Engine == nil {
		return river.JobCancel(errNotConfigured)
	}
	_, err := w.Engine.Run(ctx, job.Args)
	return err
}

// EmailWorker runs dream_email.
type EmailWorker struct {
	river.WorkerDefaults[ledger.DreamEmailArgs]
	Mailer *Mailer
}

// Work sends the edition's morning email to whoever hasn't had it. Someone
// in their quiet hours gets it when they end: the job snoozes until then
// (a snooze uses no attempt), and the people it already reached are
// skipped (v2.dream_email_sends).
func (w *EmailWorker) Work(ctx context.Context, job *river.Job[ledger.DreamEmailArgs]) error {
	if w.Mailer == nil {
		return nil // no email configured: nothing to send
	}
	wait, err := w.Mailer.Send(ctx, job.Args.SpaceID, job.Args.EditionID)
	if err != nil {
		return err
	}
	if wait > 0 {
		return river.JobSnooze(wait)
	}
	return nil
}

// PeriodicJobs is the sweep, every interval, from the start.
func PeriodicJobs(e *Engine) []*river.PeriodicJob {
	interval := DefaultSweepInterval
	if e != nil {
		interval = e.cfg.SweepInterval
	}
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(interval),
			func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}),
	}
}

// AddWorkers registers Dream's workers. e and m may be nil: runs then
// cancel with the reason, and editions send no email.
func AddWorkers(workers *river.Workers, e *Engine, m *Mailer) {
	river.AddWorker(workers, &SweepWorker{Engine: e})
	river.AddWorker(workers, &SpaceWorker{Engine: e})
	river.AddWorker(workers, &EmailWorker{Mailer: m})
}
