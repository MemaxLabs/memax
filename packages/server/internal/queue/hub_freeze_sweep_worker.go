package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// hubFreezeSweepTimeout caps the sweep. The dispatched work is one
// SELECT + a bounded fan-out of INSERTs (hub count × recipients ≤ a
// small number in practice). 90 seconds is well beyond the hot-path
// expectation and exists only to keep a pathological run from
// monopolising a worker slot.
const hubFreezeSweepTimeout = 90 * time.Second

// HubFreezeSweepWorker fires hub_frozen notifications to the hub
// owner + admins of any hub whose grace window has expired. Uses
// CreateNotificationIfAbsent + an over_limit_since cycle id in the
// source_id so a repeat sweep over an already-notified hub is a
// no-op rather than a republish: the durable row is untouched and
// no realtime "created" event is fired. A later cycle (restore →
// over again) gets a fresh over_limit_since and therefore a fresh
// notification.
//
// Wired as an hourly periodic job in workerapp/app.go. The hub is
// already frozen at enforcement time the moment grace crosses 7d;
// the sweep only drives the user-facing notification.
type HubFreezeSweepWorker struct {
	river.WorkerDefaults[HubFreezeSweepArgs]
	Store  store.Store
	Events events.Publisher
}

func (w *HubFreezeSweepWorker) Timeout(_ *river.Job[HubFreezeSweepArgs]) time.Duration {
	return hubFreezeSweepTimeout
}

func (w *HubFreezeSweepWorker) Work(ctx context.Context, _ *river.Job[HubFreezeSweepArgs]) error {
	if w.Store == nil {
		return nil
	}
	now := time.Now().UTC()
	cutoff := now.Add(-model.HubGracePeriod)
	hubIDs, err := w.Store.ListHubsPastGrace(ctx, cutoff)
	if err != nil {
		return err
	}
	if len(hubIDs) == 0 {
		return nil
	}
	slog.InfoContext(ctx, "hub freeze sweep: processing hubs", "hub_count", len(hubIDs))
	for _, hubID := range hubIDs {
		hub, err := w.Store.GetHub(hubID)
		if err != nil || hub == nil {
			continue
		}
		sub, _ := w.Store.GetHubSubscriptionAnyStatus(ctx, hubID)
		if sub == nil || sub.OverLimitSince == nil {
			// Transitioned back while the sweep was in flight. Skip.
			continue
		}

		// Resolve the hub's plan to fill MemoryLimit in the payload —
		// otherwise the inbox renderer gets a zero denominator.
		var memoryLimit int
		if plan, pErr := w.Store.GetPlan(ctx, sub.PlanID); pErr == nil && plan != nil {
			memoryLimit = plan.MemoryLimit
		}

		payload := model.HubQuotaPayload{
			Hub: model.HubInviteHubRef{
				ID:     hub.ID,
				Name:   hub.Name,
				Icon:   hub.Icon,
				Accent: hub.Accent,
			},
			PlanID:         sub.PlanID,
			MemoryLimit:    memoryLimit,
			OverLimitSince: sub.OverLimitSince,
			FrozenAt:       &now,
		}
		if count, cErr := w.Store.CountMemoriesByHub(ctx, hubID); cErr == nil {
			payload.MemoryCount = count
		}
		dispatchHubFrozenNotifications(ctx, w.Store, w.Events, hub, *sub.OverLimitSince, payload)
	}
	return nil
}

// dispatchHubFrozenNotifications mirrors handler.dispatchHubQuotaNotification
// for the worker context (which can't import handler). Recipients are
// hub owner + members with role=admin; retries dedupe via
// (source_kind, source_id) with the cycle timestamp baked in so a
// later freeze cycle gets its own fresh notification.
//
// Uses CreateNotificationIfAbsent + a gated publish so the hourly
// sweep over an already-notified hub is a no-op.
func dispatchHubFrozenNotifications(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	hub *model.Hub,
	cycle time.Time,
	payload model.HubQuotaPayload,
) {
	recipients := map[string]struct{}{}
	if hub.OwnerID != "" {
		recipients[hub.OwnerID] = struct{}{}
	}
	members, err := s.ListHubMembers(hub.ID)
	if err == nil {
		for _, m := range members {
			if m.Role == "admin" {
				recipients[m.UserID] = struct{}{}
			}
		}
	}
	if len(recipients) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "hub freeze sweep: marshal payload",
			"hub_id", hub.ID, "error", err)
		return
	}
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)
	for recipientID := range recipients {
		sourceID := fmt.Sprintf("%s:frozen:%d:%s", hub.ID, cycle.UTC().UnixNano(), recipientID)
		notif := &model.Notification{
			ID:              generateID(),
			Audience:        model.AudienceUser,
			RecipientUserID: recipientID,
			HubID:           hub.ID,
			Kind:            model.NotificationKindHubFrozen,
			Status:          model.NotificationStatusPending,
			Priority:        0,
			SourceKind:      model.NotificationSourceHubQuota,
			SourceID:        sourceID,
			Payload:         body,
			CreatedAt:       now,
			ExpiresAt:       &expiresAt,
		}
		inserted, err := s.CreateNotificationIfAbsent(notif)
		if err != nil {
			slog.WarnContext(ctx, "hub freeze sweep: create notification",
				"hub_id", hub.ID, "recipient", recipientID, "error", err)
			continue
		}
		if inserted {
			events.PublishNotificationCreated(ctx, publisher, notif)
		}
	}
}
