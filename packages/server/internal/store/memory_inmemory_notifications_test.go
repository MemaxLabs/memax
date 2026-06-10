package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestInMemoryUserAdminStats(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_, _, _, _ = s.ListUsers(ctx, model.AdminUserListOpts{Limit: 10})
	_ = s.UpdateUserPlan(ctx, "u1", "personal_free")
	_, _ = s.CountMemoriesByOwner(ctx, "u1")
	_, _ = s.CountMemoriesByHub(ctx, "h1")
	_, _ = s.SumStorageBytesByOwner(ctx, "u1")
	_, _ = s.GetSystemStats(ctx)
	_ = s.ResetUsageCounters(ctx, "u1", time.Now(), time.Now().Add(time.Hour))
}

func TestInMemoryNotificationsStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_ = s.CreateNotification(&model.Notification{ID: "n1"})
	_, _ = s.CreateNotificationIfAbsent(&model.Notification{ID: "n2"})
	_, _, _ = s.ListNotificationsForUser(ctx, NotificationListOpts{UserID: "u1"})
	_, _ = s.GetNotification(ctx, "n1", "u1", nil)
	_, _ = s.GetNotificationSummary(ctx, NotificationSummaryOpts{UserID: "u1"})
	_ = s.MarkNotificationSeen(ctx, "n1", "u1", nil)
	_ = s.DismissNotification(ctx, "n1", "u1", nil)
	_ = s.ResolveNotification(ctx, "n1", "u1", nil, model.ResolutionMerged)
	_, _ = s.ResolveNotificationsBySource(ctx, "src", "hub", model.ResolutionMerged)
	_, _ = s.ExpireNotifications(ctx, time.Now())
	_, _ = s.BulkMarkNotificationsSeen(ctx, BulkNotificationMutationOpts{UserID: "u1"})
	_, _ = s.BulkDismissNotifications(ctx, BulkNotificationMutationOpts{UserID: "u1"})
	_, _ = s.QueryAdminNotificationBatches(ctx, AdminNotifBatchOpts{})
}

func TestInMemoryHubSubscriptionStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_, _, _, _ = s.ListTeamHubs(ctx, model.AdminHubListOpts{})
	_, _ = s.GetActiveHubSubscription(ctx, "h1")
	_, _ = s.GetHubSubscriptionAnyStatus(ctx, "h1")
	_ = s.UpdateHubSubscription(ctx, &model.HubSubscription{})
	_, _ = s.SetHubOverLimit(ctx, "h1", time.Now())
	_, _ = s.ClearHubOverLimit(ctx, "h1")
	_, _ = s.ListFrozenHubIDs(ctx, []string{"h1"}, time.Now())
	_, _ = s.ListHubsPastGrace(ctx, time.Now())
	_, _ = s.ListHubSubscriptionsByBillingUser(ctx, "u1")
}
