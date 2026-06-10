package model

import "time"

// PlanScope distinguishes personal plans (assigned to users) from hub plans
// (assigned to team hubs). This is a product contract, not just implementation.
type PlanScope string

const (
	PlanScopePersonal PlanScope = "personal"
	PlanScopeHub      PlanScope = "hub"
)

// Canonical plan ID constants. All code must reference these instead of
// string literals so that plan ID changes become compile-time errors.
const (
	// Personal plans
	PersonalFreePlanID        = "personal_free"
	PersonalEarlyAccessPlanID = "personal_early_access"
	PersonalProPlanID         = "personal_pro"
	PersonalProPlusPlanID     = "personal_pro_plus"

	// Hub plans
	HubFreeTeamPlanID   = "hub_free_team"
	HubTeamPlanID       = "hub_team"
	HubEnterprisePlanID = "hub_enterprise"

	// Legacy plan IDs — used only for backward compatibility during
	// the migration transition. New code must use scoped IDs above.
	LegacyFreePlanID        = "free"
	LegacyEarlyAccessPlanID = "early_access"
	LegacyProPlanID         = "pro"
	LegacyProPlusPlanID     = "pro_plus"
	LegacyTeamPlanID        = "team"
	LegacyEnterprisePlanID  = "enterprise"
)

// LegacyToScopedPlanID maps legacy plan IDs to their scoped equivalents.
// Returns the input unchanged if it's already scoped or unknown.
func LegacyToScopedPlanID(planID string) string {
	switch planID {
	case LegacyFreePlanID:
		return PersonalFreePlanID
	case LegacyEarlyAccessPlanID:
		return PersonalEarlyAccessPlanID
	case LegacyProPlanID:
		return PersonalProPlanID
	case LegacyProPlusPlanID:
		return PersonalProPlusPlanID
	case LegacyTeamPlanID:
		return HubTeamPlanID
	case LegacyEnterprisePlanID:
		return HubEnterprisePlanID
	default:
		return planID
	}
}

// Plan defines a pricing tier with its limits and features.
type Plan struct {
	ID                 string    `json:"id"`
	Scope              PlanScope `json:"scope"`
	DisplayName        string    `json:"display_name"`
	TierOrder          int       `json:"tier_order"`
	EntitlementRank    int       `json:"entitlement_rank"` // cross-scope comparison rank for max() resolution
	MonthlyPriceCents  int       `json:"monthly_price_cents"`
	MemoryLimit        int       `json:"memory_limit"`         // total stored memories, -1 = unlimited
	PushLimit          int       `json:"push_limit"`           // per month, -1 = unlimited
	RecallLimit        int       `json:"recall_limit"`         // per month, -1 = unlimited
	AskLimit           int       `json:"ask_limit"`            // per month, -1 = unlimited
	MaxAttachmentBytes int64     `json:"max_attachment_bytes"` // per-file cap for memory attachment uploads, -1 = unlimited
	StorageBytesLimit  int64     `json:"storage_bytes_limit"`  // total storage cap across attachments + sessions + snapshots, -1 = unlimited
	AskModel           string    `json:"ask_model"`            // "haiku" or "sonnet"
	DreamsEnabled      bool      `json:"dreams_enabled"`
	BasicDreamLimit    int       `json:"basic_dream_limit"` // monthly Basic Dream runs; -1 = unlimited, 0 = disabled
	LucidDreamLimit    int       `json:"lucid_dream_limit"` // monthly Lucid Dream runs; -1 = unlimited, 0 = disabled
	ReviewInbox        bool      `json:"review_inbox"`
	MaxTeamHubs        int       `json:"max_team_hubs"` // -1 = unlimited (legacy, use MaxOwnedFreeTeamHubs for scoped)

	// Scoped ownership and hub fields (populated from migration 066+)
	MaxOwnedFreeTeamHubs int  `json:"max_owned_free_team_hubs"` // personal plan: how many free team hubs the user can own
	MaxHubMembers        *int `json:"max_hub_members"`          // hub plan: member cap; nil = unlimited
	SeatMinimum          int  `json:"seat_minimum"`             // hub plan: billing floor for paid subscriptions
	SeatBilled           bool `json:"seat_billed"`              // hub plan: whether seat count is billed externally

	RateLimitRPM      int            `json:"rate_limit_rpm"`
	RateLimitHeavyRPM int            `json:"rate_limit_heavy_rpm"`
	RateLimitLightRPM int            `json:"rate_limit_light_rpm"`
	Features          map[string]any `json:"features"`
	Active            bool           `json:"active"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// IsPersonal returns true if this plan is assigned to users.
func (p *Plan) IsPersonal() bool { return p.Scope == PlanScopePersonal }

// IsHub returns true if this plan is assigned to team hubs.
func (p *Plan) IsHub() bool { return p.Scope == PlanScopeHub }

// IsUnlimited returns true if the given limit value represents unlimited.
func IsUnlimited(limit int) bool {
	return limit < 0
}

// ─── Entitlement Resolution Types ───────────────────────────────────────────
// These types are the output of the scoped resolver. They carry both the
// effective limits AND the source/context that produced them, so that
// enforcement, logging, and display layers know exactly why a limit was chosen.

// EntitlementContext identifies the operation type for entitlement resolution.
type EntitlementContext string

const (
	EntitlementRead          EntitlementContext = "read"           // recall, ask
	EntitlementPersonalWrite EntitlementContext = "personal_write" // push to personal hub
	EntitlementHubWrite      EntitlementContext = "hub_write"      // push to team hub
	EntitlementOwnership     EntitlementContext = "ownership"      // hub creation, ownership transfer
	EntitlementHubManage     EntitlementContext = "hub_manage"     // hub settings, member cap
)

// EntitlementSource describes where the effective limits came from.
type EntitlementSource struct {
	Scope  PlanScope `json:"scope"`            // "personal" or "hub"
	PlanID string    `json:"plan_id"`          // the plan ID that sourced the limits
	HubID  string    `json:"hub_id,omitempty"` // set when source is a hub subscription
	Reason string    `json:"reason"`           // "personal", "best_hub", "target_hub", "override"
}

// ResolvedEntitlements is the result of a scoped entitlement resolution.
// It carries both the limits to enforce and the metadata for logging/display.
type ResolvedEntitlements struct {
	Context EntitlementContext `json:"context"`
	Limits  UserLimits         `json:"limits"`
	Source  EntitlementSource  `json:"source"`
}

// OwnershipEntitlements is the result of ownership cap resolution.
// It provides both the effective limits and the current ownership counts
// so that enforcement can return actionable error details.
type OwnershipEntitlements struct {
	PersonalPlanID       string `json:"personal_plan_id"`
	MaxOwnedFreeTeamHubs int    `json:"max_owned_free_team_hubs"`
	CurrentOwnedCount    int    `json:"current_owned_count"`
	CanCreateFreeTeamHub bool   `json:"can_create_free_team_hub"`
}

// PlanOverride stores per-user admin overrides for plan limits.
type PlanOverride struct {
	UserID    string         `json:"user_id"`
	Overrides map[string]any `json:"overrides"` // only overridden fields
	Reason    string         `json:"reason"`
	SetBy     string         `json:"set_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// UserLimits are the effective limits for a user after resolving plan + overrides.
type UserLimits struct {
	PlanID             string `json:"plan_id"`
	PlanDisplayName    string `json:"plan_display_name"`
	MemoryLimit        int    `json:"memory_limit"`
	PushLimit          int    `json:"push_limit"`
	RecallLimit        int    `json:"recall_limit"`
	AskLimit           int    `json:"ask_limit"`
	MaxAttachmentBytes int64  `json:"max_attachment_bytes"`
	StorageBytesLimit  int64  `json:"storage_bytes_limit"`
	AskModel           string `json:"ask_model"`
	DreamsEnabled      bool   `json:"dreams_enabled"`
	// Note: BasicDreamLimit and LucidDreamLimit are intentionally
	// NOT on UserLimits. They use a different resolution rule
	// (field-wise max across all candidate plans) than the rest of
	// UserLimits (rank-wins), and putting them on this struct would
	// risk callers reading them from a rank-wins-resolved instance
	// and getting silently wrong values (e.g., a Free user in
	// hub_free_team seeing basic_dream_limit=0). Use
	// planresolver.ResolveDreamLimits / GetHubDreamLimits instead.
	ReviewInbox       bool `json:"review_inbox"`
	MaxTeamHubs       int  `json:"max_team_hubs"`
	RateLimitRPM      int  `json:"rate_limit_rpm"`
	RateLimitHeavyRPM int  `json:"rate_limit_heavy_rpm"`
	RateLimitLightRPM int  `json:"rate_limit_light_rpm"`
}

// UsageWithLimits combines current usage counters with the user's effective limits.
// Usage fields are embedded (not nested) so the flat fields (push_count, recall_count,
// ask_count) remain at the top level — backward-compatible with clients that read
// the old flat Usage shape.
type UsageWithLimits struct {
	Usage                      // embedded: user_id, period_start, period_end, push_count, recall_count, ask_count
	Limits          UserLimits `json:"limits"`
	Plan            string     `json:"plan"`
	PlanDisplayName string     `json:"plan_display_name"`
}

// UsageEvent is an append-only record for billing and analytics.
type UsageEvent struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	HubID     string         `json:"hub_id,omitempty"`
	Operation string         `json:"operation"` // "push", "recall", "ask", "plan_change"
	Source    string         `json:"source"`    // "cli", "web", "mcp", "api", "admin"
	AgentName string         `json:"agent_name,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// BillingSubscription tracks a user's subscription to a plan via a payment provider.
type BillingSubscription struct {
	ID                     string     `json:"id"`
	UserID                 string     `json:"user_id"`
	PlanID                 string     `json:"plan_id"`
	Provider               string     `json:"provider"` // "admin", "stripe", "polar"
	ProviderSubscriptionID string     `json:"provider_subscription_id,omitempty"`
	ProviderCustomerID     string     `json:"provider_customer_id,omitempty"`
	Status                 string     `json:"status"` // "active", "past_due", "cancelled", "trialing"
	CurrentPeriodStart     *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd       *time.Time `json:"current_period_end,omitempty"`
	CancelledAt            *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// HubSubscription tracks a hub's subscription to a plan. Hub owner pays per-seat.
type HubSubscription struct {
	ID                     string     `json:"id"`
	HubID                  string     `json:"hub_id"`
	PlanID                 string     `json:"plan_id"`
	SeatCount              int        `json:"seat_count"`
	Provider               string     `json:"provider"` // "admin", "stripe", "polar"
	ProviderSubscriptionID string     `json:"provider_subscription_id,omitempty"`
	ProviderCustomerID     string     `json:"provider_customer_id,omitempty"`
	Status                 string     `json:"status"`          // "active", "past_due", "cancelled", "trialing"
	BillingUserID          string     `json:"billing_user_id"` // hub owner who pays
	CurrentPeriodStart     *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd       *time.Time `json:"current_period_end,omitempty"`
	CancelledAt            *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	// OverLimitSince is set when the hub's aggregate memory count first
	// exceeds the subscription plan's memory_limit, and cleared when it
	// drops back under. When non-nil and older than the grace window
	// (7 days), the hub is frozen: new pushes blocked, memories
	// excluded from members' recall/ask results.
	OverLimitSince *time.Time `json:"over_limit_since,omitempty"`
	// Denormalized hub display fields. Empty in most code paths — only
	// populated by admin handlers that want to render a human-friendly
	// reference alongside the subscription (so the admin UI can crosslink
	// to /admin/hubs/{id} with the hub's name instead of a raw uuid).
	HubName *string `json:"hub_name,omitempty"`
	HubSlug *string `json:"hub_slug,omitempty"`
}

// HubGracePeriod is the window a hub gets to resolve an over-limit
// state (via plan upgrade, member deletion, or count drift) before
// it freezes. Operator-tunable but this const is the default.
const HubGracePeriod = 7 * 24 * time.Hour

// IsHubFrozen reports whether a hub should be treated as frozen —
// pushes blocked, memories excluded from recall/ask. Two conditions:
//  1. subscription status is not active (past_due, cancelled, etc.)
//  2. over_limit_since is set and older than HubGracePeriod
//
// A nil subscription returns false: hubs with no subscription run on
// the hub_free_team plan and are never "frozen" — they simply live at
// whatever their free-tier memory_limit is.
func IsHubFrozen(sub *HubSubscription, now time.Time) bool {
	if sub == nil {
		return false
	}
	if sub.Status != "active" {
		return true
	}
	if sub.OverLimitSince != nil && now.Sub(*sub.OverLimitSince) > HubGracePeriod {
		return true
	}
	return false
}

// EffectivePlan is a cached resolution of a user's max(personal, hub) plan for
// read and personal-write operations. Team writes resolve the target hub directly.
type EffectivePlan struct {
	PlanID     string    `json:"plan_id"`
	Source     string    `json:"source"` // "personal" or "hub:<hub_id>"
	ComputedAt time.Time `json:"computed_at"`
}

// PlanMigrationAnomaly records data that didn't map cleanly during the
// scoped plans migration. Used for admin review and auditing.
type PlanMigrationAnomaly struct {
	ID            string     `json:"id"`
	EntityType    string     `json:"entity_type"` // "user", "hub", "subscription"
	EntityID      string     `json:"entity_id"`
	OldValue      string     `json:"old_value"`
	ProposedValue string     `json:"proposed_value"`
	Reason        string     `json:"reason"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// HubPlanRef pairs a hub ID with its active subscription plan ID.
type HubPlanRef struct {
	HubID  string `json:"hub_id"`
	PlanID string `json:"plan_id"`
}

// AdminUserListOpts configures the admin user list query.
type AdminUserListOpts struct {
	Plan   string // filter by plan ID
	Query  string // ILIKE search on email/name
	Sort   string // "created_at", "email", "plan"
	Order  string // "asc", "desc"
	Limit  int
	Cursor string
}

// AdminHubListOpts configures the admin team-hub list query.
// Keyset pagination matches AdminUserListOpts: Cursor is the last
// seen hub ID from the previous page; the query returns the next
// page of rows strictly before it in (created_at, id) DESC order.
type AdminHubListOpts struct {
	Query  string // ILIKE search on hub name/slug
	Limit  int
	Cursor string
}

// AdminHubMemberListOpts configures the paginated admin hub-members
// query. Cursor is the last seen member UserID; rows come back in
// (joined_at, user_id) ASC order — oldest first — so an admin
// scanning a team sees the core contributors first and expansion is
// chronological. Query is an ILIKE on user name / email.
type AdminHubMemberListOpts struct {
	Query  string
	Limit  int
	Cursor string
}

// AdminUserDetail is the full admin view of a user.
//
// Two limit fields, different denominators:
//   - Limits           — resolved against the user's personal plan (what
//     they get outside any hub, with no boost).
//   - EffectiveLimits  — resolved against the effective plan
//     (max(personal, best_hub_plan)). Populated only when the effective
//     plan id differs from the personal plan id; otherwise nil because
//     it would be identical to Limits. Admin UI uses this to show a
//     second "in personal hub" usage block that reflects the boost the
//     user actually gets when working in their own hub.
type AdminUserDetail struct {
	User             User                 `json:"user"`
	Plan             *Plan                `json:"plan"`
	EffectivePlan    *EffectivePlan       `json:"effective_plan,omitempty"`
	EffectivePlanDef *Plan                `json:"effective_plan_def,omitempty"`
	Override         *PlanOverride        `json:"override,omitempty"`
	Usage            *Usage               `json:"usage"`
	Limits           *UserLimits          `json:"limits"`
	EffectiveLimits  *UserLimits          `json:"effective_limits,omitempty"`
	Hubs             []HubWithRole        `json:"hubs"`
	HubSubscriptions []HubSubscription    `json:"hub_subscriptions,omitempty"`
	MemoryCount      int                  `json:"memory_count"`
	Subscription     *BillingSubscription `json:"subscription,omitempty"`
}

// AdminHubDetail is the full admin view of a team hub — meta + owner
// (for cross-linking in the admin UI), subscription state, and
// aggregate stats. The member roster is NOT inlined here — it's
// fetched separately via GET /v1/admin/hubs/{id}/members so very
// large teams don't balloon the detail response. Use MemberCount
// for the stat tile; use the paginated endpoint for the table.
type AdminHubDetail struct {
	Hub          Hub              `json:"hub"`
	Owner        *User            `json:"owner,omitempty"`
	Subscription *HubSubscription `json:"subscription,omitempty"`
	Plan         *Plan            `json:"plan,omitempty"`
	MemberCount  int              `json:"member_count"`
	MemoryCount  int              `json:"memory_count"`
}

// SystemStats provides system-wide admin statistics.
type SystemStats struct {
	TotalUsers    int            `json:"total_users"`
	UsersByPlan   map[string]int `json:"users_by_plan"`
	TotalMemories int            `json:"total_memories"`
	ActiveToday   int            `json:"active_today"`
}
