package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// SendBroadcastNotificationWorker pages through all users and creates
// a per-user notification row for each. Used by admin "send to all"
// targeting mode.
type SendBroadcastNotificationWorker struct {
	river.WorkerDefaults[SendBroadcastNotificationArgs]
	Store  store.Store
	Events events.Publisher
}

func (w *SendBroadcastNotificationWorker) Work(ctx context.Context, job *river.Job[SendBroadcastNotificationArgs]) error {
	args := job.Args

	sourceKind := ""
	switch args.NotifKind {
	case model.NotificationKindSystemNotice:
		sourceKind = model.NotificationSourceSystemNotice
	case model.NotificationKindGiftInviteLink:
		sourceKind = model.NotificationSourceGift
	default:
		slog.ErrorContext(ctx, "broadcast: unknown notification kind", "kind", args.NotifKind, "batch_id", args.BatchID)
		return nil // don't retry
	}

	var cursor string
	total := 0
	pageSize := 100

	for {
		users, nextCursor, _, err := w.Store.ListUsers(ctx, model.AdminUserListOpts{
			Limit:  pageSize,
			Cursor: cursor,
		})
		if err != nil {
			return err
		}

		for _, u := range users {
			n := &model.Notification{
				ID:              uuid.New().String(),
				Audience:        model.AudienceUser,
				RecipientUserID: u.ID,
				Kind:            args.NotifKind,
				Status:          model.NotificationStatusPending,
				SourceKind:      sourceKind,
				SourceID:        args.BatchID + ":" + u.ID,
				Payload:         args.Payload,
				CreatedAt:       time.Now(),
			}
			if err := w.Store.CreateNotification(n); err != nil {
				slog.WarnContext(ctx, "broadcast: failed to create notification",
					"error", err, "user_id", u.ID, "batch_id", args.BatchID)
				continue
			}
			events.PublishNotificationCreated(ctx, w.Events, n)
			total++
		}

		if nextCursor == "" || len(users) < pageSize {
			break
		}
		cursor = nextCursor
	}

	slog.InfoContext(ctx, "broadcast notification complete",
		"batch_id", args.BatchID, "kind", args.NotifKind, "total", total, "admin_id", args.AdminID)
	return nil
}
