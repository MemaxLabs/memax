package model

import "time"

type User struct {
	ID             string    `json:"id"`
	GitHubID       int64     `json:"github_id,omitempty"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	DisplayName    string    `json:"display_name,omitempty"`
	AvatarURL      string    `json:"avatar_url"`
	Plan           string    `json:"plan"`             // legacy — reads old users.plan column; use PersonalPlanID for scoped resolution
	PersonalPlanID string    `json:"personal_plan_id"` // scoped plan ID (personal_free, personal_early_access, etc.)
	CanCreateHub   bool      `json:"can_create_hub"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Note: `users.email_opt_out_marketing` column (added in migration 008)
// is read via Store.IsUserEmailOptedOut with a dedicated query. It is
// deliberately NOT hydrated onto this struct — doing so would require
// touching every user SELECT query in the codebase. If we ever need
// opt-out on read paths (admin list, profile serialization), add a
// targeted column to User and update all scans at the same time.

// MeResponse is the full profile returned by GET /v1/auth/me.
type MeResponse struct {
	User               User          `json:"user"`
	ConnectedProviders []string      `json:"connected_providers"` // ["github", "google"]
	Usage              *Usage        `json:"usage,omitempty"`
	Hubs               []HubWithRole `json:"hubs"`
	DevAccess          bool          `json:"dev_access"`
	AdminRole          string        `json:"admin_role,omitempty"` // "super_admin" when user is admin
}

// AuthIdentity represents one OAuth provider linked to a user account.
type AuthIdentity struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Provider      string    `json:"provider"` // "github", "google"
	ProviderID    string    `json:"provider_id"`
	ProviderEmail string    `json:"provider_email"`
	ProviderName  string    `json:"provider_name"`
	CreatedAt     time.Time `json:"created_at"`
}

// OAuthState is a single-use, expiring record for CSRF-safe OAuth flows.
type OAuthState struct {
	StateHash      string
	Provider       string  // "github", "google"
	Flow           string  // "login" or "link"
	UserID         *string // set for "link" flow
	ClientRedirect string
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}

// EmailOTPCode is one issued sign-in code, hashed at rest. The verify
// path scans the most recent unconsumed row for an email, increments
// Attempts on a miss, and writes ConsumedAt on a hit. ClientRedirect
// and InviteToken carry the OAuth-equivalent post-login context
// through the two-step request/verify dance.
//
// The plaintext code is NEVER stored — only sha256(code || ":" ||
// email || ":" || pepper) hex. Pepper is the server's JWT_SECRET so
// a leaked DB snapshot alone can't brute-force codes.
type EmailOTPCode struct {
	ID             string
	Email          string // canonicalized: lower(btrim(email))
	CodeHash       string
	Purpose        string // "login" today; future: "verify_email", etc.
	Attempts       int
	MaxAttempts    int
	ClientRedirect string
	InviteToken    string
	RequestIP      string
	UserAgent      string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}

type Session struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	RefreshToken string    `json:"-"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
}
