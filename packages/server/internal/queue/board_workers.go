package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/boards"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- Board Refresh Worker (plan 25, Lane A) ---

type BoardRefreshWorker struct {
	river.WorkerDefaults[BoardRefreshArgs]
	Store store.Store
}

func (w *BoardRefreshWorker) Timeout(*river.Job[BoardRefreshArgs]) time.Duration {
	return 2 * time.Minute
}

func (w *BoardRefreshWorker) Work(ctx context.Context, job *river.Job[BoardRefreshArgs]) error {
	if err := boards.NewProducer(w.Store).RefreshHubBoard(ctx, job.Args.HubID); err != nil {
		return err
	}
	slog.InfoContext(ctx, "board refreshed", "hub_id", job.Args.HubID)
	return nil
}

// --- Board Sweep Worker ---
// Same fan-out shape as the nightly dream sweep, reusing the dreamable
// hub set: a hub whose plan allows dreams gets a Lane A board too.

type BoardSweepWorker struct {
	river.WorkerDefaults[BoardSweepArgs]
	Store       store.Store
	RiverClient *Client
}

func (w *BoardSweepWorker) Timeout(*river.Job[BoardSweepArgs]) time.Duration {
	return 2 * time.Minute
}

func (w *BoardSweepWorker) Work(ctx context.Context, job *river.Job[BoardSweepArgs]) error {
	hubs, err := w.Store.ListDreamableHubs()
	if err != nil {
		return fmt.Errorf("list dreamable hubs: %w", err)
	}
	enqueued := 0
	for _, hub := range hubs {
		if err := w.RiverClient.Insert(ctx, BoardRefreshArgs{HubID: hub.ID}, nil); err != nil {
			slog.WarnContext(ctx, "failed to enqueue board refresh", "hub_id", hub.ID, "error", err)
			continue
		}
		enqueued++
	}
	slog.InfoContext(ctx, "board sweep complete", "hubs", len(hubs), "enqueued", enqueued)
	return nil
}
