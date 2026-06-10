package model

import "time"

// WaitlistEntry represents a single waitlist signup.
type WaitlistEntry struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	UseCase    string     `json:"use_case"`
	AITools    []string   `json:"ai_tools"`
	Role       string     `json:"role"`
	Status     string     `json:"status"` // pending, approved, rejected, registered
	Wave       *int       `json:"wave,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	ApprovedBy *string    `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// Denormalized from latest invite (populated at query time for admin list)
	InviteStatus *string    `json:"invite_status,omitempty"` // active, used, expired
	InviteUsedAt *time.Time `json:"invite_used_at,omitempty"`
	InviteUsedBy *string    `json:"invite_used_by,omitempty"`
	// Friendly display fields populated by joining waitlist_invites.used_by
	// to users.id in the admin list query. Let the admin UI show
	// "used by <name>" with a link to /admin/users/{id} without a
	// per-row follow-up fetch.
	InviteUsedByEmail *string `json:"invite_used_by_email,omitempty"`
	InviteUsedByName  *string `json:"invite_used_by_name,omitempty"`
}

// WaitlistInvite represents a tokenized registration invite linked to a waitlist entry.
type WaitlistInvite struct {
	ID         string     `json:"id"`
	WaitlistID string     `json:"waitlist_id"`
	Email      string     `json:"email"`
	Token      string     `json:"-"`      // never expose raw token in API responses
	Status     string     `json:"status"` // active, used, expired
	ExpiresAt  time.Time  `json:"expires_at"`
	UsedAt     *time.Time `json:"used_at,omitempty"`
	UsedBy     *string    `json:"used_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// WaitlistInvitePublic is the response for public invite validation.
// Only exposes what the invite recipient needs to see.
type WaitlistInvitePublic struct {
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WaitlistStats aggregates waitlist metrics for the admin dashboard.
type WaitlistStats struct {
	Total        int         `json:"total"`
	Pending      int         `json:"pending"`
	Approved     int         `json:"approved"`
	Registered   int         `json:"registered"`
	Rejected     int         `json:"rejected"`
	CapacityCap  int         `json:"capacity_cap"`
	CapacityUsed int         `json:"capacity_used"`
	Waves        []WaveStats `json:"waves"`
}

// WaveStats tracks per-wave metrics.
type WaveStats struct {
	Wave       int        `json:"wave"`
	Approved   int        `json:"approved"`
	Registered int        `json:"registered"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
}

// WaitlistListOpts controls filtering and pagination for admin waitlist listing.
type WaitlistListOpts struct {
	Status  string // filter by status
	UseCase string // filter by use_case
	Query   string // ILIKE substring search on email
	Sort    string // "created_at" or "email"
	Order   string // "asc" or "desc"
	Limit   int
	Cursor  string
}

// Waitlist status constants.
const (
	WaitlistStatusPending    = "pending"
	WaitlistStatusApproved   = "approved"
	WaitlistStatusRejected   = "rejected"
	WaitlistStatusRegistered = "registered"
)

// Waitlist invite status constants.
const (
	WaitlistInviteStatusActive  = "active"
	WaitlistInviteStatusUsed    = "used"
	WaitlistInviteStatusExpired = "expired"
)

// OrphanWaitlistCandidate is a user who has a matching active waitlist
// invite but never had it consumed — a stuck state from the 2026-04
// regression where the client dropped the invite token during OAuth.
// Populated by Store.ListOrphanWaitlistCandidates for the admin
// reconcile preview (GET /v1/admin/waitlist/orphans). Contains the
// minimum needed to render the preview UI and to feed the per-user
// POST /v1/admin/waitlist/reconcile payload.
//
// Token is intentionally NOT exposed in JSON — operators shouldn't
// see raw waitlist tokens, and the reconcile path doesn't need
// it client-side (it's looked up server-side from the invite row).
type OrphanWaitlistCandidate struct {
	UserID          string    `json:"user_id"`
	UserEmail       string    `json:"user_email"`
	UserCreatedAt   time.Time `json:"user_created_at"`
	UserPlanID      string    `json:"user_plan_id"` // "" or "personal_free" today
	InviteID        string    `json:"invite_id"`
	InviteToken     string    `json:"-"`
	InviteExpiresAt time.Time `json:"invite_expires_at"`
	WaitlistID      string    `json:"waitlist_id"`
}

// OrphanWaitlistCandidateOpts controls ListOrphanWaitlistCandidates.
// Since is required so the operator must narrow the scan — prevents
// an unbounded full-users scan from the admin UI.
type OrphanWaitlistCandidateOpts struct {
	Since time.Time
	Limit int // capped at 500 by the store; 0 uses default 100
}
