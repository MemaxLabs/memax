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

// onHubInviteRevoked resolves any pending hub_invite notification
// keyed on the given invite id with resolution=dismissed so a
// revoked-but-still-pending row disappears from the invitee's
// inbox immediately instead of rotting there as a phantom
// accept/decline target.
//
// Called from:
//   - HubsHandler.RevokeInvite  (admin tombstones the invite)
//   - HubsHandler.RegenerateInvite (revokes the old invite before
//     minting a replacement; without this the invitee would see
//     two pending rows, the first pointing at a dead row)
//
// Best-effort: a ResolveNotificationsBySource failure is logged
// and swallowed — the durable fact (the revoked invite row) is
// already written.
func onHubInviteRevoked(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	inviteID string,
) {
	if inviteID == "" {
		return
	}
	resolved, err := s.ResolveNotificationsBySource(
		ctx,
		model.NotificationSourceHubInvite,
		inviteID,
		model.ResolutionDismissed,
	)
	if err != nil {
		slog.Warn("hub_invite revoke: resolve notifications by source",
			"invite_id", inviteID, "error", err)
		return
	}
	for i := range resolved {
		events.PublishNotificationResolved(ctx, publisher, &resolved[i])
	}
}

// onHubInviteDeclined runs the notification side effects for a
// successful decline. Fires two outcome receipts so both sides of
// the lifecycle get a durable, self-oriented record:
//
//  1. hub_invite_declined to the inviter (invite.InvitedBy, not
//     always the hub owner — team admins can invite, and they
//     should get their own close signal on the invite they created).
//  2. hub_invite_declined_by_you to the invitee themself so their
//     inbox has a self-oriented confirmation instead of just a
//     disappearing decision row.
//
// Both receipts are keyed on (source_kind, invite.ID) so
// notifications_source_unique dedupes retries. The source_kinds
// are distinct from each other and from the original hub_invite
// decision row (source_kind=hub_invite), so there is no collision
// across the three rows that can exist for a single invite id.
//
// Best-effort: failures are logged and swallowed. The durable fact
// (the revoked invite row) is already written by the time this
// runs.
func onHubInviteDeclined(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	invite *model.HubInvite,
) {
	if invite == nil {
		return
	}
	hub, err := s.GetHub(invite.HubID)
	if err != nil {
		slog.Warn("hub_invite decline: load hub",
			"invite_id", invite.ID, "error", err)
		return
	}
	hubRef := model.HubInviteHubRef{
		ID:     hub.ID,
		Name:   hub.Name,
		Icon:   hub.Icon,
		Accent: hub.Accent,
	}

	// Inviter-side receipt: "{invitee} declined your invite to {hub}".
	if invite.InvitedBy != "" {
		var invitee *model.User
		if invite.InviteeUserID != nil && *invite.InviteeUserID != "" {
			invitee, _ = s.GetUser(*invite.InviteeUserID)
		}
		inviteeRef := model.HubMemberRef{}
		if invitee != nil {
			display := invitee.DisplayName
			if display == "" {
				display = invitee.Name
			}
			inviteeRef = model.HubMemberRef{
				ID:          invitee.ID,
				DisplayName: display,
				AvatarURL:   invitee.AvatarURL,
			}
		} else if invite.InviteeUserID != nil {
			inviteeRef.ID = *invite.InviteeUserID
		}

		payload := model.HubInviteDeclinedPayload{
			Hub:     hubRef,
			Invitee: inviteeRef,
		}
		writeOutcomeReceipt(ctx, s, publisher, &outcomeReceiptSpec{
			recipientID: invite.InvitedBy,
			hubID:       hub.ID,
			kind:        model.NotificationKindHubInviteDeclined,
			sourceKind:  model.NotificationSourceHubInviteDeclined,
			sourceID:    invite.ID,
			payload:     payload,
			logKey:      "hub_invite_declined notification",
		})
	}

	// Invitee-side self-receipt: "You declined {hub}".
	if invite.InviteeUserID != nil && *invite.InviteeUserID != "" {
		payload := model.HubInviteDeclinedByYouPayload{Hub: hubRef}
		writeOutcomeReceipt(ctx, s, publisher, &outcomeReceiptSpec{
			recipientID: *invite.InviteeUserID,
			hubID:       hub.ID,
			kind:        model.NotificationKindHubInviteDeclinedByYou,
			sourceKind:  model.NotificationSourceHubInviteDeclinedByYou,
			sourceID:    invite.ID,
			payload:     payload,
			logKey:      "hub_invite_declined_by_you notification",
		})
	}
}

// outcomeReceiptSpec captures the shape shared by every invite
// outcome receipt: hub_member_joined, hub_invite_accepted,
// hub_invite_declined, hub_invite_declined_by_you. Centralizing
// the write path keeps recipient targeting and source dedup
// consistent across the four kinds.
type outcomeReceiptSpec struct {
	recipientID string
	hubID       string
	kind        string
	sourceKind  string
	sourceID    string
	payload     any
	logKey      string
}

func writeOutcomeReceipt(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	spec *outcomeReceiptSpec,
) {
	if spec.recipientID == "" || spec.sourceID == "" {
		return
	}
	bytes, err := json.Marshal(spec.payload)
	if err != nil {
		slog.Warn(spec.logKey+": marshal payload",
			"source_id", spec.sourceID, "error", err)
		return
	}
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	notif := &model.Notification{
		ID:              generateID(),
		Audience:        model.AudienceUser,
		RecipientUserID: spec.recipientID,
		HubID:           spec.hubID,
		Kind:            spec.kind,
		Status:          model.NotificationStatusPending,
		Priority:        0,
		SourceKind:      spec.sourceKind,
		SourceID:        spec.sourceID,
		Payload:         bytes,
		CreatedAt:       time.Now(),
		ExpiresAt:       &expiresAt,
	}
	if err := s.CreateNotification(notif); err != nil {
		slog.Warn(spec.logKey+": create",
			"source_id", spec.sourceID, "error", err)
		return
	}
	events.PublishNotificationCreated(ctx, publisher, notif)
}

// onHubInviteAccepted runs the notification side effects for a
// successful hub invite accept, regardless of which entry path
// (legacy token-based accept in hubs.go, notification-resolver
// by-id accept in notifications.go) produced the join.
//
// Side effects, in order:
//
//  1. Any pending hub_invite notification keyed on this invite's id
//     is resolved with resolution=accepted. SSE notification.resolved
//     events are published for each affected row so the invitee's
//     other tabs converge. The by-id path does NOT need this step
//     (its handler's Resolve flow already writes the row) and should
//     set resolveExistingNotification=false to skip it.
//
//  2. A hub_member_joined receipt is emitted to the hub owner,
//     unless the owner is the one who accepted (self-join noise).
//
//  3. A hub_invite_accepted self-receipt is emitted to the invitee
//     themself ("You joined X"). Without this the invitee's only
//     positive feedback is the decision row disappearing, and they
//     get no durable self-oriented record of the join. The owner's
//     hub_member_joined receipt is NEVER routed to the invitee —
//     audience=user + recipient=hub.OwnerID makes that impossible,
//     but the two receipts exist precisely so each side reads
//     their own view of the same event.
//
// Every step is best-effort: a notification write failure does not
// roll back the join, because the durable fact (the hub_members row)
// is already written by the time this helper runs. Errors are
// logged and swallowed.
func onHubInviteAccepted(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	invite *model.HubInvite,
	newMemberID string,
	resolveExistingNotification bool,
) {
	if invite == nil {
		return
	}

	if resolveExistingNotification {
		resolved, err := s.ResolveNotificationsBySource(
			ctx,
			model.NotificationSourceHubInvite,
			invite.ID,
			model.ResolutionAccepted,
		)
		if err != nil {
			slog.Warn("hub_invite accept: resolve notifications by source",
				"invite_id", invite.ID, "error", err)
		} else {
			for i := range resolved {
				events.PublishNotificationResolved(ctx, publisher, &resolved[i])
			}
		}
	}

	emitHubMemberJoinedNotification(ctx, s, publisher, invite, newMemberID)
	emitHubInviteAcceptedSelfReceipt(ctx, s, publisher, invite, newMemberID)
}

// emitHubInviteAcceptedSelfReceipt fans out the "You joined {hub}"
// self-receipt to the new member. Keyed on source_kind=
// hub_invite_accepted + source_id=invite.ID so it dedupes cleanly
// against retries and does not collide with the decision row
// (source_kind=hub_invite) or the owner-side hub_member_joined
// row (source_kind=hub_member).
func emitHubInviteAcceptedSelfReceipt(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	invite *model.HubInvite,
	newMemberID string,
) {
	if invite == nil || newMemberID == "" {
		return
	}
	hub, err := s.GetHub(invite.HubID)
	if err != nil {
		slog.Warn("hub_invite_accepted notification: load hub",
			"invite_id", invite.ID, "error", err)
		return
	}
	payload := model.HubInviteAcceptedPayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Icon:   hub.Icon,
			Accent: hub.Accent,
		},
		Role: invite.Role,
	}
	writeOutcomeReceipt(ctx, s, publisher, &outcomeReceiptSpec{
		recipientID: newMemberID,
		hubID:       hub.ID,
		kind:        model.NotificationKindHubInviteAccepted,
		sourceKind:  model.NotificationSourceHubInviteAccepted,
		sourceID:    invite.ID,
		payload:     payload,
		logKey:      "hub_invite_accepted notification",
	})
}

// emitHubMemberJoinedNotification fans out a hub_member_joined
// receipt to the hub owner when a user accepts their invite. Shared
// by the token-based AcceptInvite handler and the notification
// by-id dispatchHubInviteAction path.
//
// Best-effort: a failure to write the notification or publish the
// SSE event does not fail the accept itself.
func emitHubMemberJoinedNotification(
	ctx context.Context,
	s store.Store,
	publisher events.Publisher,
	invite *model.HubInvite,
	newMemberID string,
) {
	if invite == nil {
		return
	}
	hub, err := s.GetHub(invite.HubID)
	if err != nil {
		slog.Warn("hub_member_joined notification: load hub",
			"invite_id", invite.ID, "error", err)
		return
	}
	// Don't notify the owner if the owner somehow accepted their own
	// invite — self-notifications are noise.
	if hub.OwnerID == "" || hub.OwnerID == newMemberID {
		return
	}
	member, _ := s.GetUser(newMemberID)
	payload := model.HubMemberJoinedPayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Icon:   hub.Icon,
			Accent: hub.Accent,
		},
		Role: invite.Role,
	}
	if member != nil {
		display := member.DisplayName
		if display == "" {
			display = member.Name
		}
		payload.Member = model.HubMemberRef{
			ID:          member.ID,
			DisplayName: display,
			AvatarURL:   member.AvatarURL,
		}
	}
	// source_id MUST be keyed on invite.ID, not on the new member's
	// user id. notifications_source_unique is (source_kind, source_id)
	// only — it is not hub-scoped. Using newMemberID collided across
	// hubs: once user U produced one hub_member_joined row in ANY
	// hub, every subsequent join by the same user in any other hub
	// would conflict and the ON CONFLICT DO UPDATE path would mutate
	// the original row instead of creating a new one, silently
	// suppressing the receipt for every other hub owner. Keying on
	// invite.ID makes each join→receipt mapping one-to-one: different
	// invites (including cross-hub invites and re-invites after a
	// remove) write distinct rows, and a retried producer for the
	// same invite deterministically upserts the same row.
	writeOutcomeReceipt(ctx, s, publisher, &outcomeReceiptSpec{
		recipientID: hub.OwnerID,
		hubID:       hub.ID,
		kind:        model.NotificationKindHubMemberJoined,
		sourceKind:  model.NotificationSourceHubMember,
		sourceID:    invite.ID,
		payload:     payload,
		logKey:      "hub_member_joined notification",
	})
}
