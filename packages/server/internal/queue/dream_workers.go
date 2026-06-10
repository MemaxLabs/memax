package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/dreams"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- Dream Cycle Worker ---

type DreamCycleWorker struct {
	river.WorkerDefaults[DreamCycleArgs]
	Engine *dreams.Engine
}

func (w *DreamCycleWorker) Timeout(*river.Job[DreamCycleArgs]) time.Duration {
	return 10 * time.Minute
}

func (w *DreamCycleWorker) Work(ctx context.Context, job *river.Job[DreamCycleArgs]) error {
	if w.Engine == nil {
		return river.JobCancel(fmt.Errorf("dream engine not configured"))
	}
	slog.InfoContext(ctx, "dream cycle started", "hub_id", job.Args.HubID)
	_, err := w.Engine.RunForActor(ctx, job.Args.HubID, job.Args.TriggeredBy)
	return err
}

// --- Nightly Dream Sweep Worker ---
// Enqueues DreamCycleArgs for every dreamable hub. Runs on a cron schedule.

type NightlyDreamSweepWorker struct {
	river.WorkerDefaults[NightlyDreamSweepArgs]
	Store       store.Store
	RiverClient *Client // insert-only client for re-enqueueing
}

func (w *NightlyDreamSweepWorker) Timeout(*river.Job[NightlyDreamSweepArgs]) time.Duration {
	return 2 * time.Minute
}

func (w *NightlyDreamSweepWorker) Work(ctx context.Context, job *river.Job[NightlyDreamSweepArgs]) error {
	hubs, err := w.Store.ListDreamableHubs()
	if err != nil {
		return fmt.Errorf("list dreamable hubs: %w", err)
	}

	enqueued := 0
	for _, hub := range hubs {
		if err := w.RiverClient.Insert(ctx, DreamCycleArgs{HubID: hub.ID}, nil); err != nil {
			slog.WarnContext(ctx, "failed to enqueue dream cycle", "hub_id", hub.ID, "hub_type", hub.HubType, "error", err)
			continue
		}
		enqueued++
	}

	slog.InfoContext(ctx, "nightly dream sweep complete", "hubs", len(hubs), "enqueued", enqueued)
	return nil
}
