package sealer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// MaxWorkers is the seal queue's concurrency in the worker.
const MaxWorkers = 4

// VerifyInterval is how often every space is verified.
const VerifyInterval = 24 * time.Hour

// SweepArgs is the periodic seal sweep.
type SweepArgs struct{}

// Kind implements river.JobArgs.
func (SweepArgs) Kind() string { return "seal_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueSeal, MaxAttempts: 1}
}

// VerifySweepArgs is the nightly verification sweep.
type VerifySweepArgs struct{}

// Kind implements river.JobArgs.
func (VerifySweepArgs) Kind() string { return "receipts_verify_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (VerifySweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueSeal, MaxAttempts: 3}
}

// VerifyArgs verifies one space.
type VerifyArgs struct {
	SpaceID uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (VerifyArgs) Kind() string { return "receipts_verify" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (VerifyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueSeal, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{
		ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending,
			rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStateRetryable},
	}}
}

var errNotConfigured = errors.New("sealer: not configured (the worker has no database)")

// SweepWorker runs seal_sweep.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Sealer *Sealer
}

// Work moves the cursor and queues the seal jobs, in one transaction.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	if w.Sealer == nil {
		return river.JobCancel(errNotConfigured)
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("seal sweep: %w", err)
	}
	spaces, err := w.Sealer.ledger.SealSweep(ctx, SweepBatch, client)
	if err != nil {
		return err
	}
	if len(spaces) > 0 {
		w.Sealer.log.DebugContext(ctx, "sealer: queued spaces", "spaces", len(spaces))
	}
	return nil
}

// SealWorker runs seal_space (ledger.SealSpaceArgs).
type SealWorker struct {
	river.WorkerDefaults[ledger.SealSpaceArgs]
	Sealer *Sealer
}

// Timeout bounds one attempt: at most maxBatchesPerJob checkpoints and
// their uploads.
func (w *SealWorker) Timeout(*river.Job[ledger.SealSpaceArgs]) time.Duration { return 2 * time.Minute }

// Work seals the space; a space with more receipts than a job takes
// snoozes and continues. The job completes itself in the transaction that
// checks the sweep hasn't passed a receipt it missed (ledger.FinishSeal):
// seal_space is unique over running jobs, so a receipt the sweep passed
// while this job ran is this job's to seal.
func (w *SealWorker) Work(ctx context.Context, job *river.Job[ledger.SealSpaceArgs]) error {
	if w.Sealer == nil {
		return river.JobCancel(errNotConfigured)
	}
	_, err := w.Sealer.SealSpace(ctx, job.Args.SpaceID)
	switch {
	case errors.Is(err, ErrMore):
		return river.JobSnooze(0)
	case errors.Is(err, ledger.ErrNotFound):
		return river.JobCancel(fmt.Errorf("seal: space %s is gone", job.Args.SpaceID))
	case err != nil:
		return err
	}
	pending, err := w.Sealer.ledger.FinishSeal(ctx, job.Args.SpaceID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job)
		return err
	})
	if err != nil {
		return err
	}
	if pending {
		return river.JobSnooze(0)
	}
	return nil
}

// VerifySweepWorker runs receipts_verify_sweep.
type VerifySweepWorker struct {
	river.WorkerDefaults[VerifySweepArgs]
	Sealer *Sealer
}

// Work queues a verification of every space with receipts.
func (w *VerifySweepWorker) Work(ctx context.Context, _ *river.Job[VerifySweepArgs]) error {
	if w.Sealer == nil {
		return river.JobCancel(errNotConfigured)
	}
	spaces, err := w.Sealer.ledger.ReceiptSpaces(ctx)
	if err != nil || len(spaces) == 0 {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("verify sweep: %w", err)
	}
	params := make([]river.InsertManyParams, len(spaces))
	for i, s := range spaces {
		params[i] = river.InsertManyParams{Args: VerifyArgs{SpaceID: s}}
	}
	_, err = client.InsertMany(ctx, params)
	return err
}

// VerifyWorker runs receipts_verify.
type VerifyWorker struct {
	river.WorkerDefaults[VerifyArgs]
	Sealer *Sealer
}

// Timeout bounds one space's verification.
func (w *VerifyWorker) Timeout(*river.Job[VerifyArgs]) time.Duration { return 10 * time.Minute }

// Work verifies the space. A mismatch is not a job failure (retrying
// can't fix it): it is logged and metered by Sealer.Verify.
func (w *VerifyWorker) Work(ctx context.Context, job *river.Job[VerifyArgs]) error {
	if w.Sealer == nil {
		return river.JobCancel(errNotConfigured)
	}
	_, err := w.Sealer.Verify(ctx, job.Args.SpaceID)
	return err
}

// PeriodicJobs are the sweep, every interval, and the nightly verification.
func PeriodicJobs(s *Sealer) []*river.PeriodicJob {
	interval := DefaultInterval
	if s != nil {
		interval = s.cfg.Interval
	}
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(interval),
			func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(VerifyInterval),
			func() (river.JobArgs, *river.InsertOpts) { return VerifySweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false}),
	}
}

// AddWorkers registers the sealer's workers. s may be nil: the jobs then
// cancel with the reason.
func AddWorkers(workers *river.Workers, s *Sealer) {
	river.AddWorker(workers, &SweepWorker{Sealer: s})
	river.AddWorker(workers, &SealWorker{Sealer: s})
	river.AddWorker(workers, &VerifySweepWorker{Sealer: s})
	river.AddWorker(workers, &VerifyWorker{Sealer: s})
}
