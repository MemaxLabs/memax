package judge

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// MaxWorkers is the judge queue's concurrency in the worker. A job spends
// almost all its time waiting on the model.
const MaxWorkers = 10

// Worker runs judge_proposal jobs (ledger.JudgeArgs).
type Worker struct {
	river.WorkerDefaults[ledger.JudgeArgs]
	Judge *Judge
}

// Timeout bounds one attempt: up to four model calls (two per tier) and
// the strong tier's confirmation, each bounded by the call timeout.
func (w *Worker) Timeout(*river.Job[ledger.JudgeArgs]) time.Duration { return 2 * time.Minute }

// Work judges the memory version. A model failure is recorded on the
// verdict, not returned: River retries only database trouble.
func (w *Worker) Work(ctx context.Context, job *river.Job[ledger.JudgeArgs]) error {
	if w.Judge == nil {
		return river.JobCancel(errors.New("judge: not configured (the worker has no database)"))
	}
	_, err := w.Judge.Run(ctx, job.Args, RunOptions{EnqueuedAt: job.CreatedAt})
	return err
}

// AddWorkers registers the judge's worker. j may be nil in an insert-only
// client, which never works jobs.
func AddWorkers(workers *river.Workers, j *Judge) {
	river.AddWorker(workers, &Worker{Judge: j})
}
