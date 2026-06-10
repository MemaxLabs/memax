package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// emitHubOwnershipTransferNotification fans out a
// hub_ownership_transfer decision notification to the target user
// when an owner initiates a transfer. Addressed audience=user,
// source_id=transfer.ID so the notifications_source_unique
// constraint dedupes a replayed producer to a single inbox row.
//
// Best-effort: a failure to write the notification or publish the
// SSE event does not fail the transfer creation itself. The
// underlying hub_ownership_transfers row is already durable; the
// notification is a delivery nicety on top.
func emitHubOwnershipTransferNotification(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	transfer *model.HubOwnershipTransfer,
) {
	if transfer == nil || transfer.TargetUserID == "" {
		return
	}
	hub, err := s.GetHub(transfer.HubID)
	if err != nil {
		slog.Warn("hub_ownership_transfer notification: load hub",
			"transfer_id", transfer.ID, "error", err)
		return
	}
	initiator, _ := s.GetUser(transfer.InitiatedBy)
	payload := model.HubOwnershipTransferPayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Icon:   hub.Icon,
			Accent: hub.Accent,
		},
		ExpiresAt: transfer.ExpiresAt,
	}
	if initiator != nil {
		display := initiator.DisplayName
		if display == "" {
			display = initiator.Name
		}
		payload.Initiator = model.HubMemberRef{
			ID:          initiator.ID,
			DisplayName: display,
			AvatarURL:   initiator.AvatarURL,
		}
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("hub_ownership_transfer notification: marshal payload",
			"transfer_id", transfer.ID, "error", err)
		return
	}
	expiresAt := transfer.ExpiresAt
	notif := &model.Notification{
		ID:              generateID(),
		Audience:        model.AudienceUser,
		RecipientUserID: transfer.TargetUserID,
		HubID:           hub.ID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		// Ownership transfer is a rarer, higher-stakes decision than
		// a hub invite so it ranks just above invites in the inbox.
		Priority:   2,
		SourceKind: model.NotificationSourceHubOwnershipTransfer,
		SourceID:   transfer.ID,
		Payload:    payloadBytes,
		CreatedAt:  time.Now(),
		ExpiresAt:  &expiresAt,
	}
	if err := s.CreateNotification(notif); err != nil {
		slog.Warn("hub_ownership_transfer notification: create",
			"transfer_id", transfer.ID, "error", err)
		return
	}
	events.PublishNotificationCreated(ctx, publisher, notif)
}

// onHubOwnershipTransferAccepted runs the notification side-effects
// for a successful ownership transfer accept, regardless of which
// entry path produced it (the legacy /v1/hubs/.../accept handler or
// the notification-native /v1/notifications/{id}/resolve path).
//
// Side effects:
//
//  1. The pending hub_ownership_transfer decision notification is
//     resolved with resolution=accepted. SSE notification.resolved
//     events are published for each affected row so the target's
//     other tabs converge. Skipped when resolveExistingNotification
//     is false — the by-id path's handler Resolve flow already
//     writes the specific row itself, and calling
//     ResolveNotificationsBySource there would produce a duplicate
//     notification.resolved SSE event.
//
//  2. hub_ownership_transferred receipts are emitted to both the old
//     owner and the new owner so the handoff resolves into a durable
//     receipt on both sides instead of one side seeing a silent
//     disappearance.
//
// Best-effort: all steps log-and-continue on failure. The durable
// fact (roles + owner_id swap) is already written by the time this
// runs.
func onHubOwnershipTransferAccepted(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	transfer *model.HubOwnershipTransfer,
	oldOwnerID string,
	newOwnerID string,
	resolveExistingNotification bool,
) {
	if transfer == nil {
		return
	}

	if resolveExistingNotification {
		resolved, err := s.ResolveNotificationsBySource(
			ctx,
			model.NotificationSourceHubOwnershipTransfer,
			transfer.ID,
			model.ResolutionAccepted,
		)
		if err != nil {
			slog.Warn("hub_ownership_transfer accept: resolve notifications by source",
				"transfer_id", transfer.ID, "error", err)
		} else {
			for i := range resolved {
				events.PublishNotificationResolved(ctx, publisher, &resolved[i])
			}
		}
	}

	emitHubOwnershipTransferredReceipt(ctx, s, publisher, transfer, oldOwnerID, newOwnerID)
}

// onHubOwnershipTransferCancelled resolves any pending
// hub_ownership_transfer notification addressed to the target when
// the transfer is retired out-of-band from the notification
// /resolve flow — either because the initiating owner cancelled
// unilaterally or because the target clicked "Decline" in the
// legacy settings UI.
//
// The caller passes the resolution it wants persisted so the two
// entry points converge on the same outcome as the inbox path:
//
//   - Target declines  → model.ResolutionDeclined (matches the
//     notification-native decline action)
//   - Initiator cancels → model.ResolutionDismissed (system-side
//     retraction, target never made a choice)
//
// Skipped when resolveExistingNotification is false (by-id decline
// path — the notifications handler's Resolve flow already writes
// the specific row so calling ResolveNotificationsBySource here
// would produce a duplicate SSE event).
func onHubOwnershipTransferCancelled(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	transferID string,
	resolution model.NotificationResolution,
	resolveExistingNotification bool,
) {
	if transferID == "" {
		return
	}
	if !resolveExistingNotification {
		return
	}
	if resolution == "" {
		resolution = model.ResolutionDismissed
	}
	resolved, err := s.ResolveNotificationsBySource(
		ctx,
		model.NotificationSourceHubOwnershipTransfer,
		transferID,
		resolution,
	)
	if err != nil {
		slog.Warn("hub_ownership_transfer cancel: resolve notifications by source",
			"transfer_id", transferID, "error", err)
		return
	}
	for i := range resolved {
		events.PublishNotificationResolved(ctx, publisher, &resolved[i])
	}
}

// emitHubOwnershipTransferredReceipt fires ownership-transferred
// receipts to both sides of the accepted handoff. Each recipient gets
// a distinct source_id suffix so retries stay idempotent without the
// two receipts colliding on notifications_source_unique.
func emitHubOwnershipTransferredReceipt(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	transfer *model.HubOwnershipTransfer,
	oldOwnerID string,
	newOwnerID string,
) {
	if transfer == nil || oldOwnerID == "" || newOwnerID == "" {
		return
	}
	if oldOwnerID == newOwnerID {
		// Self-transfer is a no-op; don't emit a receipt to the
		// user who just "accepted their own transfer."
		return
	}
	hub, err := s.GetHub(transfer.HubID)
	if err != nil {
		slog.Warn("hub_ownership_transferred notification: load hub",
			"transfer_id", transfer.ID, "error", err)
		return
	}
	oldOwner, _ := s.GetUser(oldOwnerID)
	newOwner, _ := s.GetUser(newOwnerID)

	payload := model.HubOwnershipTransferredPayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Icon:   hub.Icon,
			Accent: hub.Accent,
		},
	}
	payload.OldOwner = memberRefFromUser(oldOwner, oldOwnerID)
	payload.NewOwner = memberRefFromUser(newOwner, newOwnerID)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("hub_ownership_transferred notification: marshal payload",
			"transfer_id", transfer.ID, "error", err)
		return
	}
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	emitReceipt := func(recipientUserID string, sourceSuffix string) {
		notif := &model.Notification{
			ID:              generateID(),
			Audience:        model.AudienceUser,
			RecipientUserID: recipientUserID,
			HubID:           hub.ID,
			Kind:            model.NotificationKindHubOwnershipTransferred,
			Status:          model.NotificationStatusPending,
			Priority:        1,
			SourceKind:      model.NotificationSourceHubOwnershipTransferred,
			SourceID:        transfer.ID + ":" + sourceSuffix,
			Payload:         payloadBytes,
			CreatedAt:       time.Now(),
			ExpiresAt:       &expiresAt,
		}
		if err := s.CreateNotification(notif); err != nil {
			slog.Warn("hub_ownership_transferred notification: create",
				"transfer_id", transfer.ID, "recipient_user_id", recipientUserID, "error", err)
			return
		}
		events.PublishNotificationCreated(ctx, publisher, notif)
	}

	emitReceipt(oldOwnerID, "old_owner")
	emitReceipt(newOwnerID, "new_owner")
}

func memberRefFromUser(u *model.User, fallbackID string) model.HubMemberRef {
	ref := model.HubMemberRef{ID: fallbackID}
	if u == nil {
		return ref
	}
	display := u.DisplayName
	if display == "" {
		display = u.Name
	}
	ref.ID = u.ID
	ref.DisplayName = display
	ref.AvatarURL = u.AvatarURL
	return ref
}
