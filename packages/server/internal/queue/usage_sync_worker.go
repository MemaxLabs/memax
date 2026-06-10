package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// usageReader reads committed usage counters from the meter (Redis).
type usageReader interface {
	GetCommittedUsage(ctx context.Context, userID string) (push, recall, ask int)
}

// UsageSyncWorker periodically syncs committed Redis usage counters to the
// Postgres usage table. This keeps the durable store roughly current for
// billing analytics and the /v1/usage endpoint fallback path.
//
// The sync uses SetUsageCounters with GREATEST() semantics — values can
// only increase. This is idempotent and safe to run multiple times.
type UsageSyncWorker struct {
	river.WorkerDefaults[UsageSyncArgs]
	Store store.Store
	Meter usageReader // nil = skip sync (no Redis)
}

func (w *UsageSyncWorker) Timeout(_ *river.Job[UsageSyncArgs]) time.Duration {
	return 60 * time.Second
}

func (w *UsageSyncWorker) Work(ctx context.Context, _ *river.Job[UsageSyncArgs]) error {
	if w.Meter == nil {
		return nil // no meter — nothing to sync
	}

	// Get all users who have usage in the current period.
	// We read from the Postgres usage table to find active users,
	// then sync their Redis committed counters back.
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	// List users with any usage this period
	users, err := w.Store.ListUsageUserIDs(ctx, periodStart)
	if err != nil {
		slog.WarnContext(ctx, "usage sync: failed to list active users", "error", err)
		// Fall through — still try to sync users we know about
		return nil
	}

	synced := 0
	for _, userID := range users {
		push, recall, ask := w.Meter.GetCommittedUsage(ctx, userID)
		if push == 0 && recall == 0 && ask == 0 {
			continue // no Redis counters — skip
		}
		if err := w.Store.SetUsageCounters(ctx, userID, periodStart, periodEnd, push, recall, ask); err != nil {
			slog.WarnContext(ctx, "usage sync: failed to sync user",
				"error", err, "user_id", userID)
			continue
		}
		synced++
	}
	if synced > 0 {
		slog.InfoContext(ctx, "usage sync complete", "synced", synced, "total_users", len(users))
	}
	return nil
}
