// Package stalesweep flags memories whose date to check again has passed
// (the stale-detection design §2.3, migration 068): an hourly stale_sweep
// finds the V2 spaces with a kept memory due, and a stale_space job per
// space applies ledger.FlagDue there as Memax, via system. Within the hour
// on every plan, where Dream's weekly editions on Free flagged them up to
// six days late; and one actor flags stale, so Dream only reports it.
package stalesweep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Interval is how often the sweep runs: a date that passed is flagged
// within it.
const Interval = time.Hour

// MaxWorkers is the stale queue's concurrency in the worker.
const MaxWorkers = 2

// sweepBatch bounds the spaces one sweep queues; the next hour takes the
// rest.
const sweepBatch = 1000

// maxRounds bounds one space's FlagDue rounds in one job (each flags up to
// ledger.MaxStaleFlags); the next hour's job takes the rest.
const maxRounds = 20

var errNotConfigured = errors.New("stale sweep: not configured (the worker has no database)")

// actor is Memax, as the checker flags.
var actor = ledger.Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}

// SweepArgs is the hourly date sweep.
type SweepArgs struct{}

// Kind implements river.JobArgs.
func (SweepArgs) Kind() string { return "stale_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueStale, MaxAttempts: 1}
}

// SweepWorker runs stale_sweep: one read across spaces, as the sweeper's
// role, then a stale_space job per space with a memory due.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Ledger *ledger.Ledger
	// Now is the clock (tests set it); nil is time.Now.
	Now func() time.Time
}

// Work runs one sweep.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	if w.Ledger == nil {
		return river.JobCancel(errNotConfigured)
	}
	now := clock(w.Now)
	spaces, err := w.Ledger.StaleDueSpaces(ctx, now, sweepBatch)
	if err != nil || len(spaces) == 0 {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("stale sweep: %w", err)
	}
	slot := now.UTC().Truncate(Interval)
	params := make([]river.InsertManyParams, 0, len(spaces))
	for _, id := range spaces {
		params = append(params, river.InsertManyParams{Args: ledger.StaleSpaceArgs{SpaceID: id, Slot: slot}})
	}
	if _, err := client.InsertMany(ctx, params); err != nil {
		return fmt.Errorf("stale sweep: %w", err)
	}
	slog.DebugContext(ctx, "stale sweep: queued spaces", "spaces", len(spaces))
	return nil
}

// SpaceWorker runs stale_space (ledger.StaleSpaceArgs).
type SpaceWorker struct {
	river.WorkerDefaults[ledger.StaleSpaceArgs]
	Ledger *ledger.Ledger
}

// Timeout bounds one attempt: a few short transactions.
func (w *SpaceWorker) Timeout(*river.Job[ledger.StaleSpaceArgs]) time.Duration { return time.Minute }

// Work flags the space's memories whose date passed, as Memax.
func (w *SpaceWorker) Work(ctx context.Context, job *river.Job[ledger.StaleSpaceArgs]) error {
	if w.Ledger == nil {
		return river.JobCancel(errNotConfigured)
	}
	flagged, err := FlagSpace(ctx, w.Ledger, job.Args)
	if errors.Is(err, ledger.ErrNotFound) {
		return nil // the space went meanwhile
	}
	if err != nil {
		return err
	}
	if flagged > 0 {
		slog.InfoContext(ctx, "stale sweep: flagged memories whose date passed",
			"space_id", job.Args.SpaceID, "flagged", flagged)
	}
	return nil
}

// FlagSpace applies FlagDue to one space until nothing more is due (at
// most maxRounds), and returns how many memories it flagged. Each round's
// idempotency key is the space, the slot and the round, so a retried job
// replays what it flagged before and goes on from there.
func FlagSpace(ctx context.Context, l *ledger.Ledger, args ledger.StaleSpaceArgs) (int, error) {
	scope, err := l.SpaceScope(ctx, args.SpaceID)
	if err != nil {
		return 0, err
	}
	flagged := 0
	for round := range maxRounds {
		res, err := l.Apply(ctx, &ledger.FlagDue{SpaceID: args.SpaceID, Meta: ledger.Meta{
			Actor: actor, Scope: scope, Via: policy.ViaSystem,
			IdempotencyKey: fmt.Sprintf("stale-dates:%s:%s:%d", args.SpaceID, args.Slot.UTC().Format(time.RFC3339), round),
		}})
		if err != nil {
			return flagged, fmt.Errorf("stale sweep: flag %s: %w", args.SpaceID, err)
		}
		if res.Outcome == ledger.OutcomeRefused {
			// A space on V1 again since the sweep read it, say: nothing to do.
			return flagged, nil
		}
		flagged += len(res.Receipts)
		if len(res.Receipts) < ledger.MaxStaleFlags {
			return flagged, nil
		}
	}
	return flagged, nil
}

func clock(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}

// PeriodicJobs are the hourly sweep, run once at start too.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(Interval),
		func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// AddWorkers registers the sweep's workers. l may be nil: the jobs then
// cancel with the reason.
func AddWorkers(workers *river.Workers, l *ledger.Ledger) {
	river.AddWorker(workers, &SweepWorker{Ledger: l})
	river.AddWorker(workers, &SpaceWorker{Ledger: l})
}
