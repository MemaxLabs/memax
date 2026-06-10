package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// ExpireWaitlistInvitesWorker marks past-due waitlist invites as expired.
// Runs periodically (every hour) via River periodic jobs.
type ExpireWaitlistInvitesWorker struct {
	river.WorkerDefaults[ExpireWaitlistInvitesArgs]
	Store store.Store
}

func (w *ExpireWaitlistInvitesWorker) Timeout(_ *river.Job[ExpireWaitlistInvitesArgs]) time.Duration {
	return 30 * time.Second
}

func (w *ExpireWaitlistInvitesWorker) Work(ctx context.Context, _ *river.Job[ExpireWaitlistInvitesArgs]) error {
	count, err := w.Store.ExpireWaitlistInvites(ctx)
	if err != nil {
		return fmt.Errorf("expire waitlist invites: %w", err)
	}
	if count > 0 {
		slog.InfoContext(ctx, "expired waitlist invites", "count", count)
	}
	return nil
}
