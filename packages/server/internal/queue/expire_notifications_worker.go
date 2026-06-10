package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// notificationExpirySweepTimeout caps the expiry sweep at a generous
// ceiling. The query is a single UPDATE ... RETURNING on an indexed
// partial index (notifications_expires_idx), so the sweep itself is
// sub-second at any reasonable row count. The timeout exists only to
// catch a pathological lock-contention scenario without burning a
// River worker slot indefinitely.
const notificationExpirySweepTimeout = 60 * time.Second

// ExpireNotificationsWorker is the periodic sweeper that closes the
// notification lifecycle per docs/plans/17-inbox-notification-framework.md
// §3.1. Receipts past expires_at flip from pending to expired, and
// each affected row emits a notification.updated event with
// change=expired so clients can reconverge without polling.
//
// Wired as an hourly periodic job (+RunOnStart) in workerapp/app.go.
// Runs on the worker process only; the API server never inserts this
// job directly (the periodic scheduler is worker-owned).
type ExpireNotificationsWorker struct {
	river.WorkerDefaults[ExpireNotificationsArgs]
	Store  store.Store
	Events events.Publisher
}

func (w *ExpireNotificationsWorker) Timeout(_ *river.Job[ExpireNotificationsArgs]) time.Duration {
	return notificationExpirySweepTimeout
}

func (w *ExpireNotificationsWorker) Work(ctx context.Context, _ *river.Job[ExpireNotificationsArgs]) error {
	if w.Store == nil {
		return nil
	}
	expired, err := w.Store.ExpireNotifications(ctx, time.Now())
	if err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}
	// One notification.updated event per affected row (plan §4.5).
	// Publish is best-effort: a publish failure after a successful
	// DB flip is logged but not returned so River does not retry the
	// same batch — the rows are already durable at status=expired.
	for i := range expired {
		events.PublishNotificationUpdated(ctx, w.Events, &expired[i], events.NotificationChangeExpired)
	}
	slog.InfoContext(ctx, "notifications expired", "count", len(expired))
	return nil
}
