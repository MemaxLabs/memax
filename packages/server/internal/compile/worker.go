package compile

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// SweepInterval is how often the sweeper re-enqueues dirty targets (§5.7
// step 4).
const SweepInterval = 30 * time.Second

// sweepBatch bounds one sweep.
const sweepBatch = 500

// MaxWorkers is the compile queue's concurrency in the worker.
const MaxWorkers = 8

// TargetWorker runs compile_target jobs (ledger.CompileTargetArgs).
type TargetWorker struct {
	river.WorkerDefaults[ledger.CompileTargetArgs]
	Service *Service
}

// Timeout bounds one attempt: the quiet window, a compile or two and the
// artifact upload take seconds.
func (w *TargetWorker) Timeout(*river.Job[ledger.CompileTargetArgs]) time.Duration {
	return 2 * time.Minute
}

// Work compiles the target, snoozing when it changed meanwhile. The job
// completes itself in the transaction that records (or settles) its
// generation: compile_target is unique over running jobs, so a Keep that
// committed after the record but before River marked the job completed
// would otherwise be deduplicated into a job that is about to end.
func (w *TargetWorker) Work(ctx context.Context, job *river.Job[ledger.CompileTargetArgs]) error {
	if w.Service == nil {
		return river.JobCancel(fmt.Errorf("compile: not configured (set COMPILE_SERVICE_URL and object storage)"))
	}
	finish := func(ctx context.Context, tx pgx.Tx) error {
		_, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job)
		return err
	}
	out, err := w.Service.Run(ctx, job.Args, RunOptions{Snoozes: snoozes(job.Metadata), Finish: finish})
	if err != nil {
		return err
	}
	if out == Snooze {
		return river.JobSnooze(0)
	}
	return nil
}

// snoozes reads River's snooze count from a job's metadata.
func snoozes(metadata []byte) int {
	var m struct {
		Snoozes int `json:"snoozes"`
	}
	_ = json.Unmarshal(metadata, &m)
	return m.Snoozes
}

// SweepArgs is the periodic compile sweep.
type SweepArgs struct{}

// Kind implements river.JobArgs.
func (SweepArgs) Kind() string { return "compile_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueCompile, MaxAttempts: 1}
}

// SweepWorker re-enqueues every target whose latest compile is behind.
// compile_target's uniqueness keeps a target that already has a job from
// getting a second one.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Ledger *ledger.Ledger
}

// Work runs one sweep.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	refs, err := w.Ledger.DirtyTargets(ctx, sweepBatch)
	if err != nil || len(refs) == 0 {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("compile sweep: %w", err)
	}
	params := make([]river.InsertManyParams, 0, len(refs))
	for _, r := range refs {
		params = append(params, river.InsertManyParams{Args: ledger.CompileTargetArgs{TargetID: r.TargetID, SpaceID: r.SpaceID}})
	}
	_, err = client.InsertMany(ctx, params)
	return err
}

// PeriodicJobs are the compile path's periodic jobs: the sweeper.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(SweepInterval),
		func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// AddWorkers registers the compile workers. svc may be nil (compiling is
// disabled): compile jobs then cancel with the reason, and the sweeper
// still runs, re-enqueueing them once compiling is configured.
func AddWorkers(workers *river.Workers, l *ledger.Ledger, svc *Service) {
	river.AddWorker(workers, &TargetWorker{Service: svc})
	river.AddWorker(workers, &SweepWorker{Ledger: l})
}
