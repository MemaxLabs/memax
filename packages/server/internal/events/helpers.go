package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func PublishMemoryChanged(ctx context.Context, p Publisher, memory *model.Memory, actorID string) {
	if p == nil || memory == nil {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:        EventTypeHubMemoriesChanged,
		HubID:       memory.HubID,
		ActorID:     actorID,
		EntityID:    memory.ID,
		State:       memory.State,
		TopicID:     memory.TopicID,
		PrivateOnly: memory.Boundary == "private",
	})
}

func PublishMemoryChangedWithPrivacy(ctx context.Context, p Publisher, memory *model.Memory, actorID string, privateOnly bool) {
	if p == nil || memory == nil {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:        EventTypeHubMemoriesChanged,
		HubID:       memory.HubID,
		ActorID:     actorID,
		EntityID:    memory.ID,
		State:       memory.State,
		TopicID:     memory.TopicID,
		PrivateOnly: privateOnly,
	})
}

func PublishHubMemoriesChanged(ctx context.Context, p Publisher, hubID string, actorID string) {
	if p == nil || hubID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:    EventTypeHubMemoriesChanged,
		HubID:   hubID,
		ActorID: actorID,
	})
}

func PublishTopicsChanged(ctx context.Context, p Publisher, hubID string, actorID string, topicID string) {
	if p == nil || hubID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:     EventTypeHubTopicsChanged,
		HubID:    hubID,
		ActorID:  actorID,
		EntityID: topicID,
		TopicID:  topicID,
	})
}

func PublishDreamsChanged(ctx context.Context, p Publisher, hubID string, actorID string) {
	if p == nil || hubID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:    EventTypeHubDreamsChanged,
		HubID:   hubID,
		ActorID: actorID,
	})
}

// PublishHubMembersChanged emits a hub-axis `hub.members.changed`
// event so in-hub member-list surfaces (Settings → Members tab)
// converge across tabs without a reload. The broker delivers to
// every subscriber with a role on this hub — Members tab is
// visible to all roles, not just owner/admin, per
// settings-nav.tsx's `visible: isTeam` gate.
//
// This event is paired with `hub.list.changed` (user-axis) for
// events that also affect the subscriber's hub-list membership,
// like accept-invite / leave / add-member / remove-member. Role
// changes and ownership transfers emit both too so the management
// UI reflects the new role state while the list surface catches up.
func PublishHubMembersChanged(ctx context.Context, p Publisher, hubID string, actorID string) {
	if p == nil || hubID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:    EventTypeHubMembersChanged,
		HubID:   hubID,
		ActorID: actorID,
	})
}

// PublishApprovalDecided fires when a chat-tool approval row
// transitions out of `pending` (approved / denied / timed_out).
// Routed user-audience: the SDK-host approver subscribes per
// approval-id and unblocks the agent goroutine when the
// decision arrives.
//
// **Why user-audience.** The blocked goroutine lives on
// whichever worker picked up the chat run; the decide-approval
// HTTP request can land on ANY API server. User-audience
// routing in the existing broker (delivers to subscriptions
// where UserID matches RecipientUserID, regardless of hub
// membership) is exactly the cross-process fan-out we need
// without inventing a new routing primitive.
//
// EntityID carries the approval id so subscribers can filter
// to their specific approval (one approver per turn typically).
// State carries the new status string so subscribers don't
// need a follow-up DB read just to learn the verdict.
//
// Publish failures are logged at warn (not swallowed silently)
// — a missed approval.decided event leaves the SDK-host
// approver waiting on its 250ms poll fallback, so it's
// recoverable but worth diagnosing. Codex's Phase 3.6b1 v1
// review flagged this; every other helper in this file uses the
// same `_ = p.Publish(...)` pattern, but for chat-approval the
// poll fallback exists precisely so a publish miss doesn't
// stick the user — recording the miss tells operators when the
// poll path is carrying load it shouldn't have to.
func PublishApprovalDecided(ctx context.Context, p Publisher, ownerUserID, approvalID, status string) {
	if p == nil || ownerUserID == "" || approvalID == "" {
		return
	}
	if err := p.Publish(ctx, Event{
		Type:            EventTypeApprovalDecided,
		Audience:        "user",
		RecipientUserID: ownerUserID,
		EntityID:        approvalID,
		State:           status,
	}); err != nil {
		slog.WarnContext(ctx, "publish approval.decided failed",
			"approval_id", approvalID,
			"recipient", ownerUserID,
			"state", status,
			"err", err)
	}
}

// --- Notification events (Phase 3b) ---
//
// The three helpers below build the routing envelope for a
// notification row. Every event carries the row's audience routing
// fields (hub_id / recipient_user_id / hub_member_role) so the broker
// can fan out correctly via Phase 3a's user-direct routing path for
// user-audience rows. Publish AFTER the tx commits, never inside it.

// PublishNotificationCreated announces a newly-written notification
// row. Used by producers after a review / receipt / invite fan-out
// commits so clients can show the bar push or pull the inbox row.
func PublishNotificationCreated(ctx context.Context, p Publisher, n *model.Notification) {
	publishNotificationEnvelope(ctx, p, EventTypeNotificationCreated, n, "", nil)
}

// PublishNotificationResolved announces a /resolve write. Carries the
// row's resolution discriminator so clients can play the right exit
// animation (merged vs dismissed vs accepted) without re-fetching.
func PublishNotificationResolved(ctx context.Context, p Publisher, n *model.Notification) {
	publishNotificationEnvelope(ctx, p, EventTypeNotificationResolved, n, "", nil)
}

// PublishNotificationUpdated announces a non-resolve mutation to a
// notification row: /seen, /dismiss (on a receipt), or an expiry
// sweep flipping status to expired. Clients drop the row from any
// local bar-push queue regardless of change because all three states
// mean the row is no longer pending.
func PublishNotificationUpdated(ctx context.Context, p Publisher, n *model.Notification, change NotificationChange) {
	publishNotificationEnvelope(ctx, p, EventTypeNotificationUpdated, n, change, nil)
}

// NotificationItemSnapshot is the inline item payload carried by
// item_updated events. Plan 18 §4.5: clients receive viewed_at /
// completed_at / progress for one item ID and patch their cached
// notification payload in place — no follow-up fetch.
type NotificationItemSnapshot struct {
	ItemID      string
	ViewedAt    *time.Time
	CompletedAt *time.Time
	Progress    *NotifItemProgress
}

// PublishNotificationItemUpdated emits a notification.updated event
// with change=item_updated and the inline item snapshot. Used by the
// /items/{id}/view + /items/{id}/complete handlers (plan 18) so the
// other tabs / devices update their cached payload without refetching.
//
// Bar-push eviction is unaffected: item_updated does NOT drop the
// parent row from any client-side push queue (super-notifs aren't
// bar-push eligible — nothing to evict).
func PublishNotificationItemUpdated(ctx context.Context, p Publisher, n *model.Notification, snap NotificationItemSnapshot) {
	publishNotificationEnvelope(ctx, p, EventTypeNotificationUpdated, n, NotificationChangeItemUpdated, &snap)
}

func publishNotificationEnvelope(ctx context.Context, p Publisher, eventType string, n *model.Notification, change NotificationChange, item *NotificationItemSnapshot) {
	if p == nil || n == nil {
		return
	}
	// Build the full §4.5 envelope. Clients need every one of these
	// fields to drive the Phase 4 inbox + bar behavior from the
	// event stream alone, without a follow-up /v1/notifications/{id}
	// GET:
	//   - Kind          → renderer dispatch + bucket decision
	//   - HubID         → hub-axis routing for hub-audience rows
	//   - RecipientUser → user-axis routing for user-audience rows
	//   - HubMemberRole → optional hub_member narrowing
	//   - SourceKind +  → legacy /v1/reviews ↔ notifications
	//     SourceID         correlation during the Phase 4 rollout
	//   - Resolution    → exit-animation discriminator on .resolved
	//   - Change +      → drop-from-bar-queue / update seen state
	//     SeenAt           on .updated
	// ActorID is intentionally NOT populated from RecipientUserID:
	// those are different concepts. ActorID is used by the broker's
	// PrivateOnly filter to restrict a change event to the user who
	// caused it, while RecipientUserID is the addressee of a
	// user-audience notification — the invitee, the dream-run owner,
	// etc. Conflating them sent "the recipient is the actor" on the
	// wire, which confused the PrivateOnly semantics. Notifications
	// aren't PrivateOnly (producers don't set it), so leaving ActorID
	// empty is correct: the broker routes via HubID / RecipientUserID
	// exclusively for these event types.
	evt := Event{
		Type:            eventType,
		Audience:        string(n.Audience),
		HubID:           n.HubID,
		EntityID:        n.ID,
		RecipientUserID: n.RecipientUserID,
		HubMemberRole:   n.HubMemberRole,
		Kind:            n.Kind,
		SourceKind:      n.SourceKind,
		SourceID:        n.SourceID,
	}
	if eventType == EventTypeNotificationResolved && n.Resolution != "" {
		evt.Resolution = string(n.Resolution)
	}
	if eventType == EventTypeNotificationUpdated {
		evt.Change = change
		if change == NotificationChangeSeen && n.SeenAt != nil {
			seenAt := *n.SeenAt
			evt.SeenAt = &seenAt
		}
		if change == NotificationChangeItemUpdated && item != nil {
			evt.ItemID = item.ItemID
			if item.ViewedAt != nil {
				v := *item.ViewedAt
				evt.ItemViewedAt = &v
			}
			if item.CompletedAt != nil {
				c := *item.CompletedAt
				evt.ItemCompletedAt = &c
			}
			if item.Progress != nil {
				p := *item.Progress
				evt.ItemProgress = &p
			}
		}
	}
	_ = p.Publish(ctx, evt)
}

// PublishAgentChanged emits a user-axis `agent.changed` event so the
// initiating user's other tabs and devices see Settings → Your Agents
// converge without reload. state discriminates the change ("upserted"
// for new/touched agents, "disconnected" for cascade removals).
// agentSlug goes in EntityID so clients can target a specific row.
//
// User-axis routing: Audience="user" + RecipientUserID=userID keeps
// the event off the hub fan-out path. No HubID because agents are
// user-owned, not hub-scoped.
//
// Fire-and-forget: Publisher errors are swallowed (logging is the
// broker's job); a missed event is recovered by the client's
// reconnect/visibility reconcile safety net.
func PublishAgentChanged(ctx context.Context, p Publisher, userID, agentSlug, state string) {
	if p == nil || userID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:            EventTypeAgentChanged,
		Audience:        "user",
		RecipientUserID: userID,
		EntityID:        agentSlug,
		State:           state,
	})
}

// PublishHubListChanged emits a user-axis `hub.list.changed` event
// so the subscriber's hub list / hub header / hub picker converge
// without a reload, even when the hub isn't yet (or no longer) in
// their SSE subscription's frozen HubRoles snapshot.
//
// Routing: Audience="user" + RecipientUserID=userID (bypasses the
// hub fan-out path). Emit ONE call per affected user — Update and
// Delete enumerate members and call this helper in a loop so every
// member is reached regardless of their subscription state.
//
// state is the transition discriminator:
//   - "created"           — new hub; recipient is the creator/owner
//   - "deleted"           — hub gone; caller MUST snapshot recipients
//     BEFORE the cascade and publish on success only
//   - "metadata_updated"  — name/icon/accent changed OR ownership
//     transferred (fanned to every current member)
//   - "membership_joined" — this user just joined the hub
//   - "membership_left"   — this user just left / was removed
//
// The client route ignores state for invalidation purposes (always
// invalidates the hub list prefix); state is carried for debugger
// readability and future state-specific optimizations.
func PublishHubListChanged(ctx context.Context, p Publisher, userID, hubID, state string) {
	if p == nil || userID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:            EventTypeHubListChanged,
		Audience:        "user",
		RecipientUserID: userID,
		EntityID:        hubID,
		State:           state,
	})
}

// PublishKeyChanged emits a user-axis `key.changed` event so the
// initiating user's other tabs and devices see the API keys list
// converge without reload. state is "created" | "updated" | "revoked".
// keyID goes in EntityID so clients can patch a specific row.
//
// Coarse by design per the final-plan Codex review — the api-keys
// list is small and low-frequency, so broad invalidation on this
// event is simpler than per-change optimistic patches. Split into
// created/updated/revoked event types later if UX demands
// state-specific cache ops.
func PublishKeyChanged(ctx context.Context, p Publisher, userID, keyID, state string) {
	if p == nil || userID == "" {
		return
	}
	_ = p.Publish(ctx, Event{
		Type:            EventTypeKeyChanged,
		Audience:        "user",
		RecipientUserID: userID,
		EntityID:        keyID,
		State:           state,
	})
}
