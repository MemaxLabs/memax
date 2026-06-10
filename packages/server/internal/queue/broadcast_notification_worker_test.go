package queue

import (
	"context"
	"testing"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// SendBroadcastNotificationWorker fans notifications out to all users.
// Empty store → no users → zero-work exit. Unknown kind → drop with
// slog.Error and nil (no retry).

func TestBroadcastNotificationWorker_EmptyStore(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &SendBroadcastNotificationWorker{Store: s}
	err := w.Work(context.Background(), &river.Job[SendBroadcastNotificationArgs]{
		Args: SendBroadcastNotificationArgs{
			NotifKind: model.NotificationKindSystemNotice,
			BatchID:   "batch-1",
			AdminID:   "admin-1",
		},
	})
	if err != nil {
		t.Errorf("Work on empty DB: %v", err)
	}
}

func TestBroadcastNotificationWorker_UnknownKindReturnsNilNoRetry(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &SendBroadcastNotificationWorker{Store: s}
	err := w.Work(context.Background(), &river.Job[SendBroadcastNotificationArgs]{
		Args: SendBroadcastNotificationArgs{
			NotifKind: "unknown_kind",
			BatchID:   "batch-1",
			AdminID:   "admin-1",
		},
	})
	if err != nil {
		t.Errorf("unknown kind should not error (no retry), got %v", err)
	}
}

func TestBroadcastNotificationWorker_GiftKindIsRecognized(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &SendBroadcastNotificationWorker{Store: s}
	// Gift kind just maps to a different SourceKind — still exits
	// cleanly with no users seeded.
	err := w.Work(context.Background(), &river.Job[SendBroadcastNotificationArgs]{
		Args: SendBroadcastNotificationArgs{
			NotifKind: model.NotificationKindGiftInviteLink,
			BatchID:   "batch-1",
			AdminID:   "admin-1",
		},
	})
	if err != nil {
		t.Errorf("gift kind Work: %v", err)
	}
}
