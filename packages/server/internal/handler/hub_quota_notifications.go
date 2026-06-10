package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// hubQuotaTransition names the three lifecycle states as source_id
// suffixes so the (source_kind, source_id) unique constraint on
// notifications dedupes retries for the same transition / recipient
// without colliding across transitions. One notification row per
// (hub, transition, cycle, recipient).
type hubQuotaTransition string

const (
	hubQuotaOverLimit hubQuotaTransition = "over_limit"
	hubQuotaFrozen    hubQuotaTransition = "frozen"
	hubQuotaRestored  hubQuotaTransition = "restored"
)

// hubQuotaSourceID composes the (source_kind, source_id) value that
// dedupes retries while still letting a later lifecycle cycle fire
// fresh notifications. The cycle token is a monotonic time-based
// string so:
//
//   - Same cycle (a hub goes over limit and the push path retries):
//     token is identical, CreateNotification upserts the existing row.
//   - Second cycle (restored → over again): token differs (the new
//     over_limit_since is a different timestamp), fresh row.
//   - Restored uses now() as the cycle token — unique per clear.
func hubQuotaSourceID(hubID string, t hubQuotaTransition, cycle time.Time, recipientID string) string {
	return fmt.Sprintf("%s:%s:%d:%s", hubID, t, cycle.UTC().UnixNano(), recipientID)
}

// dispatchHubQuotaNotification fans a hub-quota lifecycle event out
// to the hub owner and all hub admins. Each recipient gets their own
// row so the unique (source_kind, source_id) constraint doesn't
// collapse the fan-out — source_id is scoped per recipient as
// <hub_id>:<transition>:<user_id>.
//
// Best-effort: individual failures log and continue rather than
// block. A dropped notification is strictly better than a failed
// push; the push-side enforcement is already done by the caller.
func dispatchHubQuotaNotification(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	hub *model.Hub,
	kind string,
	transition hubQuotaTransition,
	cycle time.Time,
	payload model.HubQuotaPayload,
) {
	if hub == nil {
		return
	}
	recipients := resolveHubQuotaRecipients(s, hub)
	if len(recipients) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("hub quota notification: marshal payload",
			"hub_id", hub.ID, "kind", kind, "error", err)
		return
	}
	now := time.Now()
	// Notifications expire after 30 days — long enough to be seen,
	// short enough that the inbox doesn't accumulate stale capacity
	// chatter forever.
	expiresAt := now.Add(30 * 24 * time.Hour)
	for _, recipientID := range recipients {
		notif := &model.Notification{
			ID:              generateID(),
			Audience:        model.AudienceUser,
			RecipientUserID: recipientID,
			HubID:           hub.ID,
			Kind:            kind,
			Status:          model.NotificationStatusPending,
			Priority:        0,
			SourceKind:      model.NotificationSourceHubQuota,
			SourceID:        hubQuotaSourceID(hub.ID, transition, cycle, recipientID),
			Payload:         body,
			CreatedAt:       now,
			ExpiresAt:       &expiresAt,
		}
		// CreateNotification is an upsert on (source_kind, source_id).
		// Only publish the realtime created event when we actually
		// inserted a fresh row — otherwise the upsert would re-fire
		// "notification.created" for every retry of the same cycle.
		inserted, err := s.CreateNotificationIfAbsent(notif)
		if err != nil {
			slog.Warn("hub quota notification: create",
				"hub_id", hub.ID, "recipient", recipientID, "kind", kind, "error", err)
			continue
		}
		if inserted {
			events.PublishNotificationCreated(ctx, publisher, notif)
		}
	}
}

// resolveHubQuotaRecipients returns the unique set of user IDs who
// should receive a hub-quota notification: the hub owner plus every
// member with role=admin. Order is stable (owner first, admins sorted
// by UserID) for deterministic retries.
func resolveHubQuotaRecipients(s store.Store, hub *model.Hub) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	if hub.OwnerID != "" {
		out = append(out, hub.OwnerID)
		seen[hub.OwnerID] = struct{}{}
	}
	members, err := s.ListHubMembers(hub.ID)
	if err != nil {
		// Fall back to owner-only dispatch if the members query fails.
		return out
	}
	for _, m := range members {
		if m.Role != "admin" {
			continue
		}
		if _, dup := seen[m.UserID]; dup {
			continue
		}
		seen[m.UserID] = struct{}{}
		out = append(out, m.UserID)
	}
	return out
}

// buildHubQuotaPayload fetches the plan + current count and packages
// them into the shared HubQuotaPayload shape. Kept as a helper so the
// three callsites (push-path over_limit trigger, River-job frozen
// trigger, delete-path restored trigger) don't duplicate the logic.
// Returns a zero-value payload on error; the caller is expected to
// best-effort through it.
func buildHubQuotaPayload(
	ctx context.Context,
	s store.Store,
	hub *model.Hub,
	planID string,
	memoryLimit int,
) model.HubQuotaPayload {
	payload := model.HubQuotaPayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Accent: hub.Accent,
			Icon:   hub.Icon,
		},
		PlanID:      planID,
		MemoryLimit: memoryLimit,
	}
	if count, err := s.CountMemoriesByHub(ctx, hub.ID); err == nil {
		payload.MemoryCount = count
	}
	return payload
}
