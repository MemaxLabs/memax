package model

import (
	"encoding/json"
	"time"
)

// NotificationAudience is the closed routing enum for notifications.
// Phase 3a of the inbox notification framework — see
// docs/plans/17-inbox-notification-framework.md §4.0 for the routing
// semantics of each value.
type NotificationAudience string

const (
	// AudienceHub is reserved for future hub-wide announcements
	// delivered to every current member of hub_id. No producer
	// writes this audience in Phase 3a.
	AudienceHub NotificationAudience = "hub"

	// AudienceHubMember is the default audience for today's
	// hub-scoped review kinds. Visible to every current member of
	// hub_id; optionally narrowed to a specific role via
	// HubMemberRole.
	AudienceHubMember NotificationAudience = "hub_member"

	// AudienceUser is addressed to a specific user regardless of
	// hub membership. Used for receipts (dream_run_completed),
	// invites (hub_invite), and any cross-hub delivery. Requires
	// RecipientUserID to be set.
	AudienceUser NotificationAudience = "user"
)

// NotificationStatus is the lifecycle state machine for a notification
// row. Read state lives in SeenAt (timestamp or nil), NOT in status:
// a row can be status="pending" with SeenAt set, meaning "surfaced but
// not yet acted on."
//
//	pending   → resolved  : every /resolve action on a decision kind,
//	                        including action="dismiss" (see §6.4).
//	pending   → dismissed : /dismiss endpoint on a receipt kind only.
//	                        Decision kinds never reach this state.
//	pending   → expired   : nightly sweep on receipts past expires_at.
type NotificationStatus string

const (
	NotificationStatusPending   NotificationStatus = "pending"
	NotificationStatusResolved  NotificationStatus = "resolved"
	NotificationStatusDismissed NotificationStatus = "dismissed"
	NotificationStatusExpired   NotificationStatus = "expired"
)

// NotificationResolution records which /resolve action actually ran on
// a decision row. Non-empty only when status=resolved. Receipt rows
// that leave pending via /dismiss or /expire never carry a resolution.
//
// The closed set mirrors the notification_resolution Postgres enum
// from migration 062 and the server's reviewKindAllowList. Adding a
// new value requires a coordinated server/API client change in the
// same PR.
type NotificationResolution string

const (
	ResolutionKeptA        NotificationResolution = "kept_a"
	ResolutionKeptB        NotificationResolution = "kept_b"
	ResolutionKeptBoth     NotificationResolution = "kept_both"
	ResolutionMerged       NotificationResolution = "merged"
	ResolutionKeptSeparate NotificationResolution = "kept_separate"
	ResolutionApplied      NotificationResolution = "applied"
	ResolutionKept         NotificationResolution = "kept"
	ResolutionDismissed    NotificationResolution = "dismissed"
	ResolutionAccepted     NotificationResolution = "accepted"
	ResolutionDeclined     NotificationResolution = "declined"
	// ResolutionAppliedAuto is set by the server itself when every
	// required item on a `checklist` super-notif has completed — the
	// handler calls its own /resolve with action=complete_all and
	// stamps `applied_auto` so the funnel slice can distinguish
	// server-driven auto-completion from any future user-driven
	// `applied` action. Plan 18 §3.2.
	ResolutionAppliedAuto NotificationResolution = "applied_auto"
)

// Notification is the durable record behind every row in the inbox.
// Payload shape varies by Kind — producers encode a per-kind struct at
// write time via json.Marshal and consumers decode via a type switch
// on Kind. See docs/plans/17-inbox-notification-framework.md §4.2 for
// the wire contract.
type Notification struct {
	ID              string                 `json:"id"`
	Audience        NotificationAudience   `json:"audience"`
	HubID           string                 `json:"hub_id,omitempty"`            // required when audience in (hub, hub_member)
	RecipientUserID string                 `json:"recipient_user_id,omitempty"` // required when audience = user
	HubMemberRole   string                 `json:"hub_member_role,omitempty"`   // optional narrowing for hub_member
	Kind            string                 `json:"kind"`
	Status          NotificationStatus     `json:"status"`
	Resolution      NotificationResolution `json:"resolution,omitempty"` // set only on resolved decision rows
	Priority        int                    `json:"priority"`
	SourceKind      string                 `json:"source_kind"`
	SourceID        string                 `json:"source_id,omitempty"`
	// DreamRunID tags notifications produced by a dream cycle with
	// their originating run. Nil for non-dream notifications (hub
	// invites, hub_frozen, etc.). Populated by the engine for
	// dream_run_completed, review_contradiction, review_topic_merge,
	// and review_topic_restructure kinds. Enables admin queries
	// like "show me every row this run produced" without parsing
	// kind + source_id.
	DreamRunID *string         `json:"dream_run_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	ExpiresAt  *time.Time      `json:"expires_at,omitempty"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	SeenAt     *time.Time      `json:"seen_at,omitempty"`
}

// NotificationKindCount is the per-kind slice of the summary endpoint.
// One entry per kind the current user can receive, including zeroes,
// so the badge and per-kind counters can read a stable object shape.
type NotificationKindCount struct {
	Pending int `json:"pending"`
	Unseen  int `json:"unseen"`
}

// NotificationSummary is the canonical shape returned by
// GET /v1/notifications/summary. Single source of truth for the badge
// dot (NeedsActionPending), the Updates header count (UpdatesPending),
// the unseen-emphasis count (UpdatesUnseen), and the settings >
// dreams contradictions counter (ByKind["review_contradiction"]).
// Plan §4.4 has the canonical shape.
type NotificationSummary struct {
	NeedsActionPending int                              `json:"needs_action_pending"`
	UpdatesPending     int                              `json:"updates_pending"`
	UpdatesUnseen      int                              `json:"updates_unseen"`
	ByKind             map[string]NotificationKindCount `json:"by_kind"`
}

// --- Notification kind constants (Phase 3b) ---
//
// Notification.Kind values consumed by Phase 3b producers + the
// handler layer. Strings, not a closed Go type, because the set will
// grow over Phase 4 / 5 and a typed union costs more than it saves.

const (
	NotificationKindReviewContradiction     = "review_contradiction"
	NotificationKindReviewTopicMerge        = "review_topic_merge"
	NotificationKindReviewTopicRestructure  = "review_topic_restructure"
	NotificationKindReviewLowConfidence     = "review_low_confidence"
	NotificationKindReviewStale             = "review_stale"
	NotificationKindDreamRunCompleted       = "dream_run_completed"
	NotificationKindHubInvite               = "hub_invite"
	NotificationKindHubInviteAccepted       = "hub_invite_accepted"
	NotificationKindHubInviteDeclined       = "hub_invite_declined"
	NotificationKindHubInviteDeclinedByYou  = "hub_invite_declined_by_you"
	NotificationKindHubMemberJoined         = "hub_member_joined"
	NotificationKindHubOwnershipTransfer    = "hub_ownership_transfer"
	NotificationKindHubOwnershipTransferred = "hub_ownership_transferred"
	// Hub quota lifecycle. Fired to the hub owner and any hub admins.
	//   hub_over_limit: grace window started (first push rejected by
	//     the hub memory cap).
	//   hub_frozen:     grace window expired — hub is now frozen, push
	//     blocked, memories excluded from member recall/ask.
	//   hub_restored:   hub count dropped back under the cap; frozen
	//     state (if any) is lifted.
	NotificationKindHubOverLimit   = "hub_over_limit"
	NotificationKindHubFrozen      = "hub_frozen"
	NotificationKindHubRestored    = "hub_restored"
	NotificationKindSystemNotice   = "system_notice"
	NotificationKindGiftInviteLink = "gift_invite_link"
	// Super-notif kinds (plan 18). Both consume the shared Item shape
	// (see ChecklistItem / DigestItem below). `checklist` is a decision
	// kind — exits pending via /resolve {dismiss|complete_all}, with
	// complete_all reserved for the server-driven auto-resolve when
	// every required item completes. `digest` is a receipt kind — exits
	// pending via /dismiss or /expire and is scaffolded here for the
	// first digest producer (weekly dream digest, release notes, etc.).
	NotificationKindChecklist = "checklist"

	// NotificationKindDecisionGate — an agent called
	// memax_request_decision and is waiting on the user. Decision
	// kind; the ping companion of the board's 等你 card. Resolving
	// either surface resolves both (linked via source_kind +
	// source_id = the board slot key).
	NotificationKindDecisionGate = "decision_gate"
	NotificationKindDigest       = "digest"
)

// NotificationSourceKind is the producer identity a notification row
// is keyed on via the notifications_source_unique constraint. Each
// producer picks a stable source_kind so re-processing the same
// source row upserts its notification rather than duplicating.
const (
	// NotificationSourceContradiction dedupes review_contradiction
	// rows by the memory pair the dream engine surfaced. Replaces
	// the Phase 1 source_kind="review" that keyed on the legacy
	// reviews table's row id.
	NotificationSourceContradiction = "contradiction"
	// NotificationSourceTopicReview is the source_kind for
	// review_topic_merge and review_topic_restructure producers.
	// source_id is the dream engine's topic review key, same shape
	// the Phase 1 reviews table used for its partial unique index.
	NotificationSourceTopicReview = "topic_review"
	NotificationSourceDreamRun    = "dream_run"

	// NotificationSourceDecisionGate keys decision_gate rows on the
	// board slot they mirror (source_id = slot key), making creation
	// idempotent per gate and resolution linkable from the board side.
	NotificationSourceDecisionGate = "decision_gate"
	NotificationSourceHubInvite    = "hub_invite"
	// NotificationSourceHubInviteAccepted keys the invitee's
	// self-receipt for a successful accept. source_id is the
	// invite id, so retries are idempotent against
	// notifications_source_unique and do not clash with the
	// decision row keyed on source_kind=hub_invite for the same
	// invite id.
	NotificationSourceHubInviteAccepted = "hub_invite_accepted"
	// NotificationSourceHubInviteDeclined keys the inviter's
	// receipt that their invite was declined. source_id is the
	// invite id.
	NotificationSourceHubInviteDeclined = "hub_invite_declined"
	// NotificationSourceHubInviteDeclinedByYou keys the invitee's
	// self-receipt for a decline. source_id is the invite id.
	NotificationSourceHubInviteDeclinedByYou = "hub_invite_declined_by_you"
	// NotificationSourceHubMember dedupes hub_member_joined rows.
	// source_id is the invite.ID that triggered the join — NOT the
	// newly-joined user id. Keying on the user id collided across
	// hubs (one global row per user) and silently suppressed
	// hub_member_joined receipts for every hub after the first.
	// Keying on invite.ID gives a distinct row per invite accept,
	// which is one-to-one with each join event.
	NotificationSourceHubMember = "hub_member"
	// NotificationSourceHubOwnershipTransfer dedupes
	// hub_ownership_transfer decision rows by the transfer id —
	// exactly one pending notification per transfer at a time,
	// resolved when the target accepts / declines or the owner
	// cancels the transfer out from under them.
	NotificationSourceHubOwnershipTransfer = "hub_ownership_transfer"
	// NotificationSourceHubOwnershipTransferred dedupes the
	// receipt-side hub_ownership_transferred notification. source_id
	// is transfer_id plus a recipient suffix ("old_owner" /
	// "new_owner") so both sides get a durable receipt while retries
	// stay idempotent.
	NotificationSourceHubOwnershipTransferred = "hub_ownership_transferred"
	// NotificationSourceSystemNotice is used for ad-hoc system
	// announcements (no natural producer in Phase 5; producers will
	// supply their own deterministic source_id).
	NotificationSourceSystemNotice = "system_notice"
	// NotificationSourceGift is used for gift / referral invite-link
	// flows. Phase 5 ships the renderer only; the producer lands
	// alongside the gift feature.
	NotificationSourceGift = "gift"
	// NotificationSourceHubQuota dedupes hub quota lifecycle rows
	// (over_limit / frozen / restored). source_id is the hub_id plus
	// a transition suffix ("over_limit", "frozen", "restored") so the
	// three transitions for one hub don't collide but retries within
	// the same transition do. Recipient fan-out (owner + admins) is
	// handled by the caller; each recipient gets their own row.
	NotificationSourceHubQuota = "hub_quota"
	// NotificationSourceOnboardingWelcome dedupes the founder-voice
	// welcome row emitted once per new user on signup. source_id is
	// the recipient user_id. The row is intentionally one-shot —
	// `notifications_source_unique` blocks re-emission, and restart
	// (plan 18 §3.3) only resurfaces the checklist, never the note.
	NotificationSourceOnboardingWelcome = "onboarding_welcome"
	// NotificationSourceOnboarding dedupes the first-week activation
	// checklist. source_id is `{user_id}:v{n}` — restart bumps the
	// version so each restart produces a distinct row while retries
	// within the same version collapse on the partial unique index.
	NotificationSourceOnboarding = "onboarding"
)

// --- Notification payload shapes (Phase 3b) ---
//
// Each payload is produced at the write site via json.Marshal and
// stored in notifications.payload as jsonb. The handler / client
// decode via a type switch on Notification.Kind. Shapes are
// intentionally self-contained — the notification row should render
// without a follow-up fetch for the common inbox case.

// ReviewContradictionPayload is the payload for kind =
// review_contradiction. Carries lightweight memory refs so the inbox
// can render the expanded row without a /v1/memories round-trip.
// Clients that need the full memory bodies still hit /v1/memories
// on demand.
type ReviewContradictionPayload struct {
	MemoryA    ReviewMemoryRef `json:"memory_a"`
	MemoryB    ReviewMemoryRef `json:"memory_b"`
	Similarity float64         `json:"similarity"`
	Reason     string          `json:"reason"`
}

// ReviewMemoryRef is the shared lightweight memory-pointer shape used
// by notification payloads. Only carries enough for the inbox row to
// render a title + jump link.
type ReviewMemoryRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DreamRunCompletedPayload is the payload for kind =
// dream_run_completed. Produced at the end of every successful dream run,
// including clean runs with zero counts. Clients show this as a bar push +
// durable inbox receipt per plan §5.5; Phase 4 wires the bar side.
//
// MemoriesScanned is embedded in the payload (not derived from the
// active hub's dream report) because dream_run_completed is a
// user-audience notification that can render while the user is
// browsing an unrelated hub. Reading the active hub's dreamReport
// at render time would leak cross-hub state — see the Phase 4
// bar-fix commit for the original bug.
type DreamRunCompletedPayload struct {
	RunID string `json:"run_id"`
	Mode  string `json:"mode"`
	// Status carries the terminal DreamRunStatus ("completed" or
	// "partial_failed"). Consumers use it to distinguish a clean
	// finish from one where some phase hit errors — otherwise a
	// partial_failed run with zero committed actions would be
	// indistinguishable from "everything held together". Empty
	// string for historical rows written before this field
	// existed — consumers should treat empty as "completed".
	Status          string         `json:"status,omitempty"`
	Counts          DreamRunCounts `json:"counts"`
	MemoriesScanned int            `json:"memories_scanned"`
	FinishedAt      time.Time      `json:"finished_at"`
	ReportTL        string         `json:"report,omitempty"`            // compact "last night" line, if the engine has one
	TouchedIDs      []string       `json:"touched_topic_ids,omitempty"` // topic ids surfaced by the run
}

// DreamRunCounts is the per-phase outcome counts embedded in a
// dream_run_completed payload. Mirrors the DreamRun row fields so the
// producer can fill it with a straight copy.
type DreamRunCounts struct {
	Merged         int `json:"merged"`
	Archived       int `json:"archived"`
	Organized      int `json:"organized"`
	Contradictions int `json:"contradictions"`
	Restructures   int `json:"restructures"`
}

// Nonzero returns true if any count is greater than zero.
func (c DreamRunCounts) Nonzero() bool {
	return c.Merged > 0 || c.Archived > 0 || c.Organized > 0 || c.Contradictions > 0 || c.Restructures > 0
}

// HubInvitePayload is the payload for kind = hub_invite. Carries
// enough context (hub name + inviter) that the inbox row is
// self-rendering without a /v1/hubs fetch.
type HubInvitePayload struct {
	Hub       HubInviteHubRef     `json:"hub"`
	Inviter   HubInviteInviterRef `json:"inviter"`
	Role      string              `json:"role"`
	ExpiresAt time.Time           `json:"expires_at"`
}

// HubInviteHubRef is the lightweight hub pointer in hub_invite payloads.
type HubInviteHubRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Icon   string `json:"icon,omitempty"`
	Accent string `json:"accent,omitempty"`
}

// HubInviteInviterRef is the lightweight inviter pointer in hub_invite payloads.
type HubInviteInviterRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// HubMemberJoinedPayload is the payload for kind = hub_member_joined.
// Fan-out target is the hub owner; the row says "X joined your hub Y"
// and is a receipt-only notification (no resolve actions).
type HubMemberJoinedPayload struct {
	Hub    HubInviteHubRef `json:"hub"`
	Member HubMemberRef    `json:"member"`
	Role   string          `json:"role"`
}

// HubInviteAcceptedPayload is the payload for kind =
// hub_invite_accepted — the invitee's self-receipt after a
// successful accept. Renders as "You joined {hub}" with an
// optional role hint. Fan-out target is the invitee themself, not
// the hub owner (the owner's receipt is hub_member_joined).
type HubInviteAcceptedPayload struct {
	Hub  HubInviteHubRef `json:"hub"`
	Role string          `json:"role"`
}

// HubInviteDeclinedPayload is the payload for kind =
// hub_invite_declined — the inviter's receipt that the invitee
// declined. Fan-out target is invite.InvitedBy (the admin who
// created the invite), NOT always the hub owner, because on a team
// hub any admin can send invites and should get their own close
// signal. Carries the invitee's member ref so the row can render
// "{name} declined your invite to {hub}" without an extra user
// fetch.
type HubInviteDeclinedPayload struct {
	Hub     HubInviteHubRef `json:"hub"`
	Invitee HubMemberRef    `json:"invitee"`
}

// HubInviteDeclinedByYouPayload is the payload for kind =
// hub_invite_declined_by_you — the invitee's self-receipt after a
// decline. Renders as "You declined {hub}". Fan-out target is the
// invitee themself. Keeps the inbox honest for users who want a
// durable record of declines without relying on resolved-row
// history. See the commit message for the design rationale.
type HubInviteDeclinedByYouPayload struct {
	Hub HubInviteHubRef `json:"hub"`
}

// HubMemberRef is the lightweight user pointer used by
// hub_member_joined payloads.
type HubMemberRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// SystemNoticePayload is the payload for kind = system_notice. A
// receipt-only row used for product announcements and billing
// notices. Phase 5 ships the renderer + types only; producers land
// alongside the individual features that emit them.
type SystemNoticePayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Link     string `json:"link,omitempty"`
	LinkText string `json:"link_text,omitempty"`
}

// GiftInviteLinkPayload is the payload for kind = gift_invite_link.
// Phase 5 ships the renderer + types only; the gift / referral
// producer lands alongside the gift feature.
type GiftInviteLinkPayload struct {
	Sender    HubInviteInviterRef `json:"sender"`
	Hub       HubInviteHubRef     `json:"hub,omitempty"`
	Token     string              `json:"token"`
	URL       string              `json:"url,omitempty"`
	ExpiresAt time.Time           `json:"expires_at"`
}

// HubOwnershipTransferPayload is the payload for kind =
// hub_ownership_transfer. Addressed to the transfer target so the
// target's inbox shows a decision row with accept / decline. The
// source_id on the notification row is the transfer id, so the
// inbox entry is always keyed on the specific pending transfer.
type HubOwnershipTransferPayload struct {
	Hub       HubInviteHubRef `json:"hub"`
	Initiator HubMemberRef    `json:"initiator"`
	Role      string          `json:"role,omitempty"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// HubOwnershipTransferredPayload is the payload for kind =
// hub_ownership_transferred — a receipt fired to both sides of an
// accepted handoff. The payload is symmetrical; clients derive
// recipient-specific copy from currentUserID vs payload.new_owner.id
// without requiring a hub refetch.
type HubOwnershipTransferredPayload struct {
	Hub      HubInviteHubRef `json:"hub"`
	NewOwner HubMemberRef    `json:"new_owner"`
	OldOwner HubMemberRef    `json:"old_owner"`
}

// HubQuotaPayload is the payload shared by the three hub quota
// lifecycle notifications (over_limit / frozen / restored). Carries
// the numbers the inbox needs to render "Hub X is at capacity
// (1030/1000). Frozen in 7 days unless you upgrade or delete" etc.
// without a follow-up fetch.
//
//   - PlanID / PlanDisplay: the hub's current subscription plan.
//   - MemoryCount / MemoryLimit: authoritative count vs the plan cap.
//   - OverLimitSince: UTC timestamp of the grace start (nil for
//     restored).
//   - FrozenAt: UTC timestamp the hub was declared frozen (only set
//     on kind = hub_frozen).
type HubQuotaPayload struct {
	Hub            HubInviteHubRef `json:"hub"`
	PlanID         string          `json:"plan_id"`
	PlanDisplay    string          `json:"plan_display,omitempty"`
	MemoryCount    int             `json:"memory_count"`
	MemoryLimit    int             `json:"memory_limit"`
	OverLimitSince *time.Time      `json:"over_limit_since,omitempty"`
	FrozenAt       *time.Time      `json:"frozen_at,omitempty"`
}

// --- Super-notif payloads (plan 18) ---
//
// Both `checklist` (decision) and `digest` (receipt) consume the same
// Item shape so future digest producers (weekly dream digest, release
// notes, hub welcomes) don't have to relitigate field names. The
// abstraction lives at the Item level, not at the parent kind — see
// plan 18 Appendix A.

// ItemProgress is the optional progress-bar payload on an item. Used
// by the `five_memories` step today; reusable by any item that tracks
// a count-toward-target signal.
type ItemProgress struct {
	Current int `json:"current"`
	Target  int `json:"target"`
}

// Item is the shared sub-item shape used by both super-notif kinds.
// Title + description MUST be plain user-facing strings (no HTML, no
// markdown, no interpolation placeholders) because the inbox unknown-
// kind fallback renders `payload.title + payload.description` literally
// when a producer ships ahead of its renderer (plan 18 §4.2).
type Item struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Icon        string        `json:"icon,omitempty"`    // lucide name OR emoji
	CTAURL      string        `json:"cta_url,omitempty"` // static route path or external URL
	CTALabel    string        `json:"cta_label,omitempty"`
	ViewedAt    *time.Time    `json:"viewed_at,omitempty"`
	Progress    *ItemProgress `json:"progress,omitempty"`
}

// ChecklistItem extends Item with completion state and dependency
// metadata. `LockedBy` lists other item ids that must complete first;
// the /complete endpoint refuses the call with 400 item_locked if any
// dependency is still pending. `Trigger` is a server-only hint
// (e.g. "memory_count_gte:5") consumed by the OnboardingRecorder — the
// client never reads it.
type ChecklistItem struct {
	Item
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	LockedBy    []string   `json:"locked_by,omitempty"`
	Trigger     string     `json:"trigger,omitempty"`
}

// DigestItem is the receipt-kind sub-item. Same shape as Item today;
// the named type exists so future digest-only fields (e.g. severity,
// kind-specific badges) can land without touching the shared Item.
type DigestItem struct {
	Item
}

// ChecklistPayload is the wire shape for kind=checklist. The validator
// in postgres_notifications.go Create path enforces:
//   - Title non-empty (renderer fallback safety net).
//   - len(Items) <= 20 (JSONB write atomicity).
//   - Every RequiredIDs entry exists in Items[].ID.
//   - Every LockedBy entry exists in Items[].ID.
//
// PinContext routes the row to a specific pinned region on the client
// ("memories_hero" today; "inbox_hero" reserved). Empty means inbox-
// only.
//
// PinScopeHubKind further narrows the pin region by current-view hub
// kind. Values:
//
//	""          — render in any hub view (default, back-compat).
//	"personal"  — render only when the viewer's active hub is personal.
//	"team"      — render only when the viewer's active hub is a team.
//
// Onboarding (a user-axis getting-started journey) sets "personal" so
// the checklist + founder note don't follow the user into team hub
// views, where they'd be confusing collaborative-surface clutter.
// Future team-hub onboarding sets "team". Cross-hub receipts
// (dream_run_completed, etc.) leave it empty.
type ChecklistPayload struct {
	Title           string          `json:"title"` // required
	Description     string          `json:"description,omitempty"`
	Items           []ChecklistItem `json:"items"`                   // cap 20
	RequiredIDs     []string        `json:"required_ids,omitempty"`  // subset of items[].id
	CollapseHint    string          `json:"collapse_hint,omitempty"` // strip label when compact
	PinContext      string          `json:"pin_context,omitempty"`   // "memories_hero" | "inbox_hero" | ""
	PinScopeHubKind string          `json:"pin_scope_hub_kind,omitempty"`
}

// DigestPayload is the wire shape for kind=digest. Same validation as
// checklist minus required_ids/locked_by (receipts don't auto-resolve).
type DigestPayload struct {
	Title           string       `json:"title"` // required
	Description     string       `json:"description,omitempty"`
	Items           []DigestItem `json:"items"` // cap 20
	PinContext      string       `json:"pin_context,omitempty"`
	PinScopeHubKind string       `json:"pin_scope_hub_kind,omitempty"`
}
