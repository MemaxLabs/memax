// Package v2switch runs a space's Switch to V2 in the worker (plan 25 §10,
// Phase 2 epic 2.8): the River job space_switch, which the API inserts in
// the transaction that starts a switch with something to import
// (ledger.StartSwitch). The work itself is the ledger's (ledger.RunSwitch):
// each step idempotent, through the ledger with its receipts, so a failed
// attempt resumes at the step it stopped at.
package v2switch

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// MaxWorkers is the switch queue's concurrency in the worker: switches are
// rare and long, one person's spaces at a time.
const MaxWorkers = 2

// Worker runs space_switch jobs.
type Worker struct {
	river.WorkerDefaults[ledger.SpaceSwitchArgs]
	Ledger *ledger.Ledger
}

// Timeout bounds one attempt: a large V1 space imports thousands of
// statements, one command each.
func (w *Worker) Timeout(*river.Job[ledger.SpaceSwitchArgs]) time.Duration {
	return 30 * time.Minute
}

// Work runs the switch from where it stands. A refusal (the person who
// asked no longer owns the space) is final; anything else retries, and the
// next attempt resumes at the failed step.
func (w *Worker) Work(ctx context.Context, job *river.Job[ledger.SpaceSwitchArgs]) error {
	if w.Ledger == nil {
		return river.JobCancel(errors.New("v2switch: no ledger"))
	}
	_, err := w.Ledger.RunSwitch(ctx, job.Args.SpaceID)
	var refused *ledger.SpaceRefusedError
	if errors.As(err, &refused) {
		return river.JobCancel(err)
	}
	return err
}

// AddWorkers registers the switch worker. A nil ledger registers the kind
// only (the API's insert-only client must know every kind it inserts).
func AddWorkers(workers *river.Workers, l *ledger.Ledger) {
	river.AddWorker(workers, &Worker{Ledger: l})
}
