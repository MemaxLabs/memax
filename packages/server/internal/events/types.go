package events

import (
	"time"
)

const (
	EventTypeHubMemoriesChanged = "hub.memories.changed"
	EventTypeHubTopicsChanged   = "hub.topics.changed"
	EventTypeHubDreamsChanged   = "hub.dreams.changed"
	EventTypeHubMembersChanged  = "hub.members.changed"

	// Notification event types — Phase 3b of the inbox notification
	// framework (docs/plans/17-inbox-notification-framework.md §4.5).
	// Clients subscribe to these to keep the inbox, bar, and badge
	// summary converged without polling.
	EventTypeNotificationCreated  = "notification.created"
	EventTypeNotificationResolved = "notification.resolved"
	EventTypeNotificationUpdated  = "notification.updated"

	// User-axis settings events. Delivered only to the RecipientUserID
	// subscription (Audience="user"), not fanned out across hub
	// members. Close the freshness gap on Settings → Your Agents and
	// the API-keys surface, where cross-device mutations were
	// otherwise invisible until refresh. `State` carries the change
	// shape:
	//   agent.changed → "upserted" (new or touched) | "disconnected" | "activity"
	//   key.changed   → "created" | "updated" | "revoked"
	//   hub.list.changed → "created" | "deleted" | "metadata_updated"
	//                       | "membership_joined" | "membership_left"
	// `EntityID` carries the agent slug, api-key id, or hub id.
	EventTypeAgentChanged = "agent.changed"
	EventTypeKeyChanged   = "key.changed"
	// EventTypeApprovalDecided fires when a user approves or
	// denies a chat-tool approval (`POST /v1/chat/sessions/{id}/approvals/{approval_id}`).
	// Plan 24 §"Approval flow" — the SDK-host approver
	// (Phase 3.6b2) subscribes per-approval for these and
	// unblocks the agent goroutine when the decision arrives.
	// Routed user-audience: Audience="user",
	// RecipientUserID=session.OwnerID, EntityID=approval_id,
	// State="approved"|"denied"|"timed_out".
	EventTypeApprovalDecided = "approval.decided"
	// hub.list.changed — user-axis event for hub lifecycle transitions
	// that the subscriber's frozen HubRoles map can't route (create, new
	// membership, delete). Paired with hub.members.changed for role
	// transitions inside hubs the subscriber is already subscribed to.
	EventTypeHubListChanged = "hub.list.changed"
)

// NotificationChange discriminates the kind of mutation a
// notification.updated event describes. Clients use this to decide
// whether a cached row should drop out of the local bar-push queue,
// update its seen_at, or tick into an expired tombstone.
type NotificationChange string

const (
	NotificationChangeSeen      NotificationChange = "seen"
	NotificationChangeDismissed NotificationChange = "dismissed"
	NotificationChangeExpired   NotificationChange = "expired"
	// NotificationChangeItemUpdated fires when a super-notif sub-item
	// transitions (viewed_at or completed_at stamped). The event
	// envelope carries the item snapshot inline (ItemID, ItemViewedAt,
	// ItemCompletedAt, ItemProgress) so clients update the cached
	// notification payload in place without a follow-up GET.
	//
	// Per plan 18 §4.5: item_updated does NOT affect bar-push eviction
	// (super-notifs are not bar-push eligible — there's nothing to
	// evict). The broker treats item_updated like any other updated
	// event for fan-out but the client bar-push consumer skips it.
	NotificationChangeItemUpdated NotificationChange = "item_updated"
)

type Event struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	HubID       string `json:"hub_id"`
	ActorID     string `json:"actor_id"`
	EntityID    string `json:"entity_id,omitempty"`
	State       string `json:"state,omitempty"`
	TopicID     string `json:"topic_id,omitempty"`
	PrivateOnly bool   `json:"private_only,omitempty"`
	// Audience is the routing discriminator for notification events.
	// Mirrors model.NotificationAudience: "hub", "hub_member", "user".
	// Empty for legacy non-notification events (hub.memories.changed
	// etc.), which fall through to the pre-Phase-3a hub fan-out path.
	//
	// Audience is LOAD-BEARING: the broker routes on this field
	// explicitly rather than inferring from HubID/RecipientUserID
	// emptiness. User-audience notifications carry HubID as a
	// display-context decoration (so the client can show the hub
	// name without a follow-up fetch), and the previous
	// field-emptiness heuristic (`HubID == ""` → user-direct) caused
	// user-addressed rows to fan out to every hub member via the
	// hub-axis path. Routing on Audience closes that leak at the
	// SSE layer, in parallel with the audience-gated visibility
	// predicate in store/postgres_notifications.go.
	Audience string `json:"audience,omitempty"`
	// RecipientUserID is the routing field for user-audience events
	// (notifications addressed to a specific user regardless of hub
	// membership). The broker delivers such an event to the single
	// subscription whose UserID matches, and to no one else —
	// specifically, NOT to every member of HubID the way hub-audience
	// events fan out. Used when Audience = "user".
	// See docs/plans/17-inbox-notification-framework.md §4.0 for the
	// three-tier audience model this supports.
	RecipientUserID string `json:"recipient_user_id,omitempty"`

	// Notification-specific envelope fields (§4.5). Populated by
	// PublishNotificationCreated / Resolved / Updated so a single SSE
	// event carries everything the client needs to (a) route it to
	// the right inbox tab, (b) play the right exit animation without
	// re-fetching, (c) drop a stale push from the local bar queue.
	// Unset for every non-notification event type so the existing
	// hub.memories.changed / hub.topics.changed etc. wire shapes are
	// unchanged.
	Kind          string             `json:"kind,omitempty"`
	SourceKind    string             `json:"source_kind,omitempty"`
	SourceID      string             `json:"source_id,omitempty"`
	HubMemberRole string             `json:"hub_member_role,omitempty"`
	Resolution    string             `json:"resolution,omitempty"`
	Change        NotificationChange `json:"change,omitempty"`
	SeenAt        *time.Time         `json:"seen_at,omitempty"`

	// Super-notif item snapshot fields (plan 18 §4.5). Populated only
	// when Change == NotificationChangeItemUpdated so clients can
	// patch payload.items[item_id] in place without a follow-up GET.
	// All fields are omitempty — the renderer side reads ItemID first,
	// then applies whichever of the remaining three were stamped.
	//
	// Carrying the snapshot inline avoids a serialization roundtrip
	// where every checklist tick fetches the whole notification just
	// to learn that one item flipped its viewed_at. Item snapshots are
	// small (≤ 4 timestamps + 2 ints).
	ItemID          string             `json:"item_id,omitempty"`
	ItemViewedAt    *time.Time         `json:"item_viewed_at,omitempty"`
	ItemCompletedAt *time.Time         `json:"item_completed_at,omitempty"`
	ItemProgress    *NotifItemProgress `json:"item_progress,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// NotifItemProgress is the SSE-side mirror of model.ItemProgress.
// Defined here (rather than imported from the model package) so the
// events package remains free of upstream-model imports — events is
// a leaf package other layers depend on.
type NotifItemProgress struct {
	Current int `json:"current"`
	Target  int `json:"target"`
}
