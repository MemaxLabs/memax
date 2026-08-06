package model

import "time"

type HubHeaderState string

const (
	DefaultPersonalHubName                          = "Memory Hub"
	HubAccentViolet                                 = "violet"
	HubAccentBlue                                   = "blue"
	HubAccentGreen                                  = "green"
	HubAccentAmber                                  = "amber"
	HubAccentRose                                   = "rose"
	HubAccentSlate                                  = "slate"
	HubHeaderStateFirstTime          HubHeaderState = "first_time"
	HubHeaderStateReviewNeeded       HubHeaderState = "review_needed"
	HubHeaderStateDreamDeltas        HubHeaderState = "dream_deltas"
	HubHeaderStateInboxOverflow      HubHeaderState = "inbox_overflow"
	HubHeaderStateReturnAfterAbsence HubHeaderState = "return_after_absence"
	HubHeaderStateTeamActivity       HubHeaderState = "team_activity"
	HubHeaderStateClean              HubHeaderState = "clean"
)

type HubTimeBucket string

const (
	HubTimeBucketDeepNight HubTimeBucket = "deep-night"
	HubTimeBucketMorning   HubTimeBucket = "morning"
	HubTimeBucketNoon      HubTimeBucket = "noon"
	HubTimeBucketAfternoon HubTimeBucket = "afternoon"
	HubTimeBucketEvening   HubTimeBucket = "evening"
	HubTimeBucketLateNight HubTimeBucket = "late-night"
)

type HubGreetingKey string

const (
	HubGreetingKeyFirstTimeA          HubGreetingKey = "firstTimeA"
	HubGreetingKeyFirstTimeB          HubGreetingKey = "firstTimeB"
	HubGreetingKeyReviewNeededA       HubGreetingKey = "reviewNeededA"
	HubGreetingKeyReviewNeededB       HubGreetingKey = "reviewNeededB"
	HubGreetingKeyTeamReviewA         HubGreetingKey = "teamReviewA"
	HubGreetingKeyTeamReviewB         HubGreetingKey = "teamReviewB"
	HubGreetingKeyDreamDeltasA        HubGreetingKey = "dreamDeltasA"
	HubGreetingKeyDreamDeltasB        HubGreetingKey = "dreamDeltasB"
	HubGreetingKeyInboxOverflowA      HubGreetingKey = "inboxOverflowA"
	HubGreetingKeyInboxOverflowB      HubGreetingKey = "inboxOverflowB"
	HubGreetingKeyReturnAfterAbsenceA HubGreetingKey = "returnAfterAbsenceA"
	HubGreetingKeyReturnAfterAbsenceB HubGreetingKey = "returnAfterAbsenceB"
	HubGreetingKeyMorningDreamA       HubGreetingKey = "morningDreamA"
	HubGreetingKeyMorningDreamB       HubGreetingKey = "morningDreamB"
	HubGreetingKeyTeamMorningA        HubGreetingKey = "teamMorningA"
	HubGreetingKeyTeamMorningB        HubGreetingKey = "teamMorningB"
	HubGreetingKeyMorningCleanA       HubGreetingKey = "morningCleanA"
	HubGreetingKeyMorningCleanB       HubGreetingKey = "morningCleanB"
	HubGreetingKeyAfternoonCleanA     HubGreetingKey = "afternoonCleanA"
	HubGreetingKeyAfternoonCleanB     HubGreetingKey = "afternoonCleanB"
	HubGreetingKeyDeepNightCleanA     HubGreetingKey = "deepNightCleanA"
	HubGreetingKeyDeepNightCleanB     HubGreetingKey = "deepNightCleanB"
	HubGreetingKeyTimeEveningA        HubGreetingKey = "timeEveningA"
	HubGreetingKeyTimeEveningB        HubGreetingKey = "timeEveningB"
	HubGreetingKeyTimeNoonA           HubGreetingKey = "timeNoonA"
	HubGreetingKeyTimeNoonB           HubGreetingKey = "timeNoonB"
	HubGreetingKeyTimeAfternoonA      HubGreetingKey = "timeAfternoonA"
	HubGreetingKeyTimeAfternoonB      HubGreetingKey = "timeAfternoonB"
)

func DefaultHubAccent(hubType string) string {
	if hubType == "team" {
		return HubAccentAmber
	}
	return HubAccentViolet
}

func IsValidHubAccent(accent string) bool {
	switch accent {
	case HubAccentViolet, HubAccentBlue, HubAccentGreen, HubAccentAmber, HubAccentRose, HubAccentSlate:
		return true
	default:
		return false
	}
}

type Hub struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon,omitempty"`
	Accent  string `json:"accent,omitempty"`
	Slug    string `json:"slug"`
	HubType string `json:"hub_type"` // "personal", "team"
	// HubType
	Plan                    string `json:"plan,omitempty"`
	OwnerID                 string `json:"owner_id"`
	AllowContributorTopics  bool   `json:"allow_contributor_topics"`
	AllowContributorDreams  bool   `json:"allow_contributor_dreams"`
	ContributorDeletePolicy string `json:"contributor_delete_policy"` // "none", "own", "any"
	// HeaderAuroraMode stores the team hub's header banner aurora treatment
	// ("none" | "signature" | "time"). Empty string means "inherit default"
	// ("signature") and is the expected value for personal hubs, which take
	// their aurora mode from the user's settings instead.
	HeaderAuroraMode string `json:"header_aurora_mode,omitempty"`
	// Settings is a JSONB bag of per-hub overrides. Currently used
	// by the dream engine for per-hub phase toggles
	// (dreams_enabled, dreams_merge_enabled, etc.). Readers merge in
	// order: hub.Settings → user.Settings → DefaultSettings(). New
	// keys land here without a schema migration.
	Settings  map[string]any `json:"settings,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// IsValidHubHeaderAuroraMode reports whether value is an accepted aurora mode.
// Empty string is valid and means "inherit default".
func IsValidHubHeaderAuroraMode(value string) bool {
	switch value {
	case "", "none", "signature", "time":
		return true
	default:
		return false
	}
}

// HubSettingsAllowedKeys lists the hub.Settings keys that the public
// PATCH /v1/hubs/{id} endpoint will accept. Keeping this narrow
// (versus the full user-settings allow-list in handler/settings.go)
// is deliberate: hub-level overrides are a power-user affordance
// and we don't want every new user-preference key to implicitly
// become a per-hub override the moment it ships. New keys are
// opted-in here only once there's a UI for them.
//
// All accepted values must be boolean (or null, which deletes the
// override). Non-boolean keys like dreams_similarity_threshold or
// dreams_excluded_kinds are intentionally not in this list until
// the UI supports them.
var HubSettingsAllowedKeys = map[string]bool{
	"dreams_enabled":             true,
	"dreams_merge_enabled":       true,
	"dreams_archive_enabled":     true,
	"dreams_organize_enabled":    true,
	"dreams_restructure_enabled": true,
	// Phase 2b agentic-dream opt-in. Default OFF (set in
	// DefaultSettings). Per-hub override is accepted so we can
	// flip a single internal team hub on for dogfooding before
	// rolling out broadly. Once a paid-tier rollout is calibrated
	// per plan 24, the default flips to plan-derived.
	"dreams_use_agent_runtime": true,
	// Per-hub opt-OUT for board synthesis (default on globally).
	"dreams_board_synthesis_enabled": true,
}

// IsHubSettingsKey reports whether key may appear in a PATCH
// /v1/hubs/{id} `settings` payload.
func IsHubSettingsKey(key string) bool {
	return HubSettingsAllowedKeys[key]
}

// MergedHubDreamSettings returns the effective settings for a dream
// cycle on this hub. hub.Settings layered on DefaultSettings —
// same resolution for every hub type, personal and team.
//
// Before migration 018 the personal-hub path also layered the
// owner's user_preferences.settings between defaults and hub
// settings. That two-layer model is gone: migration 018 backfilled
// the personal hub's dream-phase keys from user prefs into
// hub.Settings and stripped them from user_preferences, so the
// engine reads a single, hub-scoped source of truth. See
// docs/plans/10-dreams-and-knowledge.md for the collapse rationale.
func MergedHubDreamSettings(hub *Hub) map[string]any {
	out := DefaultSettings()
	if hub != nil {
		for k, v := range hub.Settings {
			out[k] = v
		}
	}
	return out
}

type HubMember struct {
	HubID    string    `json:"hub_id"`
	UserID   string    `json:"user_id"`
	Role     string    `json:"role"` // HubRole: "owner", "admin", "contributor", "viewer"
	JoinedAt time.Time `json:"joined_at"`
	// Joined user info (populated by queries that JOIN users)
	UserName      string `json:"user_name,omitempty"`
	UserEmail     string `json:"user_email,omitempty"`
	UserAvatarURL string `json:"user_avatar_url,omitempty"`
}

// HubMembership is the lean (hub_id, role) pair used by the
// agent runtime's ScopeResolver to compute SessionScope's
// HubIDs (read-eligible) AND WritableHubIDs (subset where the
// user has a non-viewer role). Distinct from HubMember, which
// carries user/joined/avatar fields the agent path doesn't
// need; this type is the minimum the resolver consumes.
//
// Returned by store.HubMembershipsForUser. Same indexed query
// as the previous IDs-only form, just with the role column.
type HubMembership struct {
	HubID string `json:"hub_id"`
	Role  string `json:"role"` // "owner" | "admin" | "contributor" | "viewer"
}

// IsWritableHubRole reports whether a hub-member role grants
// write capability at the COARSE level used by the
// ScopeResolver to derive SessionScope.WritableHubIDs (the
// hubs in scope where the user has a non-viewer role).
//
// Used by the chat tool wrappers' fast-fail path:
//   - push_memory: requires the hub to be in WritableHubIDs.
//   - propose_topic_merge: requires the hub to be in
//     WritableHubIDs.
//   - forget_memory: deliberately uses IsHubAllowed, NOT
//     IsHubWritable, because forget has an owner-bypass —
//     a viewer who AUTHORED the memory can still forget it.
//     The per-call role check inside the forget adapter is
//     the authoritative gate for the non-owner branch.
//
// Per-tool refinements (forget's ContributorDeletePolicy
// gate, propose's AllowContributorTopics gate, push's
// per-hub-quota / freeze checks) live in the per-tool
// service code — this predicate is the coarse prerequisite
// the resolver applies once.
//
//   - owner / admin / contributor → true
//   - viewer → false
//   - "" (non-member) → false
func IsWritableHubRole(role string) bool {
	switch role {
	case "owner", "admin", "contributor":
		return true
	}
	return false
}

type HubWithRole struct {
	Hub         Hub    `json:"hub"`
	Role        string `json:"role"`
	MemoryCount int    `json:"memory_count"`
}

type HubSummaryStats struct {
	Memories      int `json:"memories"`
	Topics        int `json:"topics"`
	Inbox         int `json:"inbox"`
	PendingReview int `json:"pending_review"`
	Members       int `json:"members,omitempty"`
	DreamTopics   int `json:"dream_topics,omitempty"`
	TeamActivity  int `json:"team_activity,omitempty"`
}

type HubSummaryHeader struct {
	State          HubHeaderState    `json:"state"`
	GreetingKey    HubGreetingKey    `json:"greeting_key"`
	GreetingParams map[string]string `json:"greeting_params"`
	TimeBucket     HubTimeBucket     `json:"time_bucket"`
}

type HubSummaryMember struct {
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	UserAvatarURL string `json:"user_avatar_url,omitempty"`
}

type HubSummary struct {
	Hub            Hub                `json:"hub"`
	Role           string             `json:"role"`
	Stats          HubSummaryStats    `json:"stats"`
	MembersPreview []HubSummaryMember `json:"members_preview,omitempty"`
	Header         HubSummaryHeader   `json:"header"`
}

type HubVisit struct {
	UserID         string    `json:"user_id"`
	HubID          string    `json:"hub_id"`
	FirstVisitedAt time.Time `json:"first_visited_at"`
	LastVisitedAt  time.Time `json:"last_visited_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type HubInvite struct {
	ID              string     `json:"id"`
	HubID           string     `json:"hub_id"`
	Token           string     `json:"token"`
	InvitedBy       string     `json:"invited_by"`
	Role            string     `json:"role"`
	InviteURL       string     `json:"invite_url,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	AcceptedBy      *string    `json:"accepted_by,omitempty"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	RevokedBy       *string    `json:"revoked_by,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	InviteeUserID   *string    `json:"invitee_user_id,omitempty"`   // populated when the invite is addressed to a known account; nil for email-only invites
	InviteeEmail    *string    `json:"invitee_email,omitempty"`     // the email the admin typed; persisted alongside invitee_user_id when the email resolves
	EmailEnqueuedAt *time.Time `json:"email_enqueued_at,omitempty"` // when the email job was last submitted to the queue (enqueue time, not confirmed delivery)
}

type HubOwnershipTransfer struct {
	ID              string     `json:"id"`
	HubID           string     `json:"hub_id"`
	InitiatedBy     string     `json:"initiated_by"`
	TargetUserID    string     `json:"target_user_id"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
	TargetUserName  string     `json:"target_user_name,omitempty"`
	TargetUserEmail string     `json:"target_user_email,omitempty"`
}

type Usage struct {
	UserID      string `json:"user_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	PushCount   int    `json:"push_count"`
	RecallCount int    `json:"recall_count"`
	AskCount    int    `json:"ask_count"`
}
