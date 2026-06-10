package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- Redirect allowlist ---

func redirectAllowlistFromAppBaseURL(appBaseURL string) []string {
	origin, ok := redirectOrigin(appBaseURL)
	if !ok {
		return nil
	}
	return []string{origin}
}

func redirectOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

func (h *AuthHandler) isAllowedRedirect(raw string) bool {
	if raw == "" {
		return true // no redirect is always allowed
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	// Reject URLs with userinfo (user:pass@host)
	if u.User != nil {
		return false
	}
	// Loopback: allow http with any port for localhost, 127.0.0.1, ::1
	if u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
		return true
	}
	// Non-loopback must use https
	if u.Scheme != "https" {
		return false
	}
	// Exact scheme://host match
	origin := u.Scheme + "://" + u.Host
	for _, allowed := range h.redirectAllowlist {
		if origin == allowed {
			return true
		}
	}
	return false
}

// --- OAuth state management ---

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (h *AuthHandler) createOAuthState(ctx context.Context, provider, flow, clientRedirect, userID string) (string, error) {
	if !h.isAllowedRedirect(clientRedirect) {
		return "", fmt.Errorf("redirect URI not in allowlist: %s", clientRedirect)
	}
	token := generateToken()
	hash := sha256Hex(token)
	expiresAt := time.Now().Add(10 * time.Minute)

	var uid *string
	if userID != "" {
		uid = &userID
	}
	err := h.store.CreateOAuthState(ctx, model.OAuthState{
		StateHash:      hash,
		Provider:       provider,
		Flow:           flow,
		UserID:         uid,
		ClientRedirect: clientRedirect,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return "", fmt.Errorf("create OAuth state: %w", err)
	}
	return token, nil
}

func (h *AuthHandler) consumeOAuthState(ctx context.Context, stateToken, expectedProvider string) (*model.OAuthState, error) {
	hash := sha256Hex(stateToken)
	state, err := h.store.ConsumeOAuthState(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired OAuth state")
	}
	// Provider binding: prevent cross-provider state replay
	if state.Provider != expectedProvider {
		return nil, fmt.Errorf("OAuth state provider mismatch: expected %s, got %s", expectedProvider, state.Provider)
	}
	return state, nil
}

// --- Provider user abstraction ---

// Registration gating errors — handled in OAuth callbacks.
var (
	ErrRegistrationRequired = fmt.Errorf("registration_required")
	ErrInvalidInvite        = fmt.Errorf("invalid_invite")
	ErrInviteEmailMismatch  = fmt.Errorf("invite_email_mismatch")
)

// loginOpts carries optional context through the login flow.
type loginOpts struct {
	InviteToken    string // waitlist invite token from client_redirect
	ClientRedirect string // original redirect URL (for post-login invite consumption)
	// ProviderOrgMember is a provider-side signal that the user
	// belongs to an org we've pre-authorized (e.g., GitHub users in
	// OAUTH_GITHUB_REQUIRED_ORG). Treated as an implicit
	// registration invite — in invite_only mode, an org member can
	// register without carrying an explicit invite token. Keeps the
	// org gate as a convenience, not a hard block, so a non-member
	// invitee with a valid hub/waitlist invite still gets through.
	ProviderOrgMember bool
}

// checkRegistrationGate decides whether a NEW user (no existing
// auth identity, no matching email) is allowed to register given
// the server's registrationMode and the signals in opts.
//
// invite_only: accept either an explicit invite token OR a
// provider-side org membership signal. The org signal is an
// implicit invite — members of the pre-authorized org don't need
// to carry a token. This is what lets a MemaxLabs employee sign
// up without waitlist paperwork while still requiring an explicit
// invite for outside users. Previously the org check was a hard
// gate in the callback and invitees outside the org were rejected
// even with a valid token.
//
// org_gated: org/allowlist is a HARD gate. The provider must have
// set ProviderOrgMember (GitHub: checkOrgMembership; Google: caller
// validates via isGoogleEmailAllowed BEFORE calling us, which is
// why the Google callback is free to pass ProviderOrgMember=false
// and still reach here — the gate below would reject a Google user
// in org_gated, so Google MUST be gated upstream via the allowlist
// and never reach this path for a non-allowed email). Invite tokens
// are not a substitute for org membership in this mode.
//
// open: anyone can register.
//
// Pure function — no I/O, no side effects. Unit-testable in
// isolation; loginOrCreateUser wraps it so we don't reach the
// tx path when the gate rejects.
func checkRegistrationGate(registrationMode string, opts loginOpts) error {
	switch registrationMode {
	case "invite_only":
		if opts.InviteToken == "" && !opts.ProviderOrgMember {
			return ErrRegistrationRequired
		}
	case "org_gated":
		if !opts.ProviderOrgMember {
			return ErrRegistrationRequired
		}
	}
	return nil
}

// extractInviteToken parses ?invite=TOKEN from a redirect URL.
func extractInviteToken(clientRedirect string) string {
	if clientRedirect == "" {
		return ""
	}
	u, err := url.Parse(clientRedirect)
	if err != nil {
		return ""
	}
	return u.Query().Get("invite")
}

type providerUser struct {
	Provider      string
	ProviderID    string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// loginOrCreateUser finds or creates a user from a provider identity.
// Uses a single transaction with unique-violation retry for concurrent races.
// opts carries optional invite token context for registration gating.
func (h *AuthHandler) loginOrCreateUser(ctx context.Context, pu providerUser, opts loginOpts) (*model.User, error) {
	if !pu.EmailVerified {
		return nil, fmt.Errorf("email not verified by %s", pu.Provider)
	}
	email := strings.ToLower(strings.TrimSpace(pu.Email))

	// Try once, retry on unique violation (concurrent race)
	user, err := h.tryLoginOrCreate(ctx, pu, email, opts)
	if err != nil && isUniqueViolation(err) {
		slog.Info("login race detected, retrying", "provider", pu.Provider, "email", email)
		user, err = h.tryLoginOrCreate(ctx, pu, email, opts)
	}

	// Post-login: consume invite if present (works for both new and existing users).
	// An invite represents admin approval — consume it and upgrade permissions.
	if err == nil && user != nil && opts.InviteToken != "" && h.store != nil {
		h.ConsumeInviteForUser(ctx, opts.InviteToken, user)
	}

	return user, err
}

func (h *AuthHandler) tryLoginOrCreate(ctx context.Context, pu providerUser, email string, opts loginOpts) (*model.User, error) {
	// 1. Check if this provider identity already exists (read-only, outside tx)
	user, err := h.store.GetUserByAuthIdentity(pu.Provider, pu.ProviderID)
	if err == nil && user != nil {
		// Backfill profile fields that may have been empty at account creation
		// (e.g., email was missing because GitHub profile was private).
		h.backfillUserProfile(ctx, user, pu, email)
		return user, nil
	}

	// 2. Check if email matches an existing user (auto-link)
	if email != "" {
		user, err = h.store.GetUserByCanonicalEmail(email)
		if err == nil && user != nil {
			// Auto-link: add this provider identity to the existing user.
			// CreateAuthIdentity returns ErrIdentityConflict if another user owns it.
			if linkErr := h.store.CreateAuthIdentity(user.ID, pu.Provider, pu.ProviderID, email, pu.Name, pu.AvatarURL); linkErr != nil {
				return nil, fmt.Errorf("auto-link identity: %w", linkErr)
			}
			// Backfill users.github_id for transition compat when a Google-created
			// user later signs in with GitHub. Existing github_id is preserved.
			if pu.Provider == "github" {
				h.backfillGitHubID(ctx, user.ID, pu.ProviderID)
			}
			slog.Info("auto-linked provider to existing user",
				"provider", pu.Provider, "user_id", user.ID, "email", email)
			return user, nil
		}
	}

	// 3. No match — create new user.
	//
	// Before gating, check if the token is a valid hub invite. Hub
	// invites are a SEPARATE authorization signal from waitlist
	// invites: they're claimed post-login by the /v1/invites/{token}/
	// accept flow, not in this tx. In invite_only mode an outstanding
	// hub invite counts as "authorized to register" — someone already
	// approved this person into a hub. Registering them here as a
	// free personal plan user lets them land on the hub-accept
	// screen.
	//
	// NOT honored in org_gated mode: invites do not substitute for
	// org membership when the deployment has chosen org-gated
	// registration. This is the same rule applied to waitlist tokens
	// in checkRegistrationGate and to Google's isGoogleEmailAllowed.
	//
	// When the invite carries an invitee_email (admin addressed it to
	// a specific person), require the registering user's verified
	// email to match — otherwise anyone with the link could use it as
	// a registration bypass. Legacy link-only invites (no
	// invitee_email) authorize whoever clicks through.
	isValidHubInvite := false
	if h.registrationMode == "invite_only" && opts.InviteToken != "" && h.store != nil {
		if hi, hubErr := h.store.GetHubInviteByToken(opts.InviteToken); hubErr == nil && hi != nil &&
			hi.AcceptedBy == nil && hi.ExpiresAt.After(time.Now()) {
			if hi.InviteeEmail == nil || strings.EqualFold(strings.TrimSpace(*hi.InviteeEmail), strings.TrimSpace(email)) {
				isValidHubInvite = true
			}
		}
	}

	// Gate by registration mode. ProviderOrgMember OR (in invite_only)
	// a valid hub invite OR a valid waitlist-ish token (presence-only
	// here; waitlist-token validity is proven atomically in the tx
	// below).
	gateOpts := opts
	if isValidHubInvite {
		// Only reachable in invite_only (guarded above). Promotes the
		// hub invite into the gate's authorization signal without
		// affecting the waitlist tx block, which uses the untouched
		// `opts` for its own decisions.
		gateOpts.ProviderOrgMember = true
	}
	if err := checkRegistrationGate(h.registrationMode, gateOpts); err != nil {
		return nil, err
	}

	// Create new user + identity (+ atomic invite claim) in a single transaction.
	// Everything succeeds or nothing does — no dangling rows, no race conditions.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after commit

	// In invite_only mode: atomically claim the WAITLIST invite inside the tx.
	// UPDATE ... WHERE status='active' AND expires_at > now() RETURNING email
	// If 0 rows affected, another concurrent request already claimed it → fail.
	//
	// Skip entirely when:
	//   - no token provided (org-member signup)
	//   - token is a hub invite (claimed later by /v1/invites/{token}/accept;
	//     running the waitlist UPDATE would match no row and spuriously
	//     reject, and we don't want the waitlist early_access upgrade
	//     either — hub invitees join on the free personal plan)
	//
	// When the token is present but not a waitlist match, org members
	// degrade gracefully (log + proceed without claim); for non-org,
	// non-hub-invite users the waitlist token is the only authorizing
	// signal so a lookup miss stays fatal. Email mismatch remains
	// fatal either way — it's a data-integrity signal.
	inviteClaimed := false
	if h.registrationMode == "invite_only" && opts.InviteToken != "" && !isValidHubInvite {
		var inviteEmail string
		claimErr := tx.QueryRow(ctx,
			`UPDATE waitlist_invites
			 SET status = 'used', used_at = now()
			 WHERE token = $1 AND status = 'active' AND expires_at > now()
			 RETURNING email`,
			opts.InviteToken).Scan(&inviteEmail)
		if claimErr != nil {
			if !opts.ProviderOrgMember {
				return nil, ErrInvalidInvite
			}
			slog.Warn("invite claim failed for org member; proceeding without invite",
				"error", claimErr, "email", email)
			// Fall through: org membership authorizes. UPDATE affected 0
			// rows so nothing to undo, and inviteClaimed stays false so
			// downstream invite metadata writes are skipped.
		} else if !strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(inviteEmail)) {
			// Rollback will un-claim the invite (tx not committed)
			return nil, ErrInviteEmailMismatch
		} else {
			inviteClaimed = true
		}
	}

	now := time.Now()
	var newUser model.User

	// For GitHub, also write github_id for backward-compat queries
	var githubID int64
	if pu.Provider == "github" {
		if id, parseErr := fmt.Sscanf(pu.ProviderID, "%d", &githubID); id != 1 || parseErr != nil {
			githubID = 0
		}
	}

	canCreateHub := true

	err = tx.QueryRow(ctx,
		`INSERT INTO users (github_id, email, name, display_name, avatar_url, can_create_hub, personal_plan_id, created_at, updated_at)
		VALUES (NULLIF($1, 0), $2, $3, $3, $4, $5, $6, $7, $7)
		RETURNING id, COALESCE(github_id, 0), email, name, COALESCE(display_name, name, ''),
		          avatar_url, personal_plan_id, personal_plan_id, can_create_hub, created_at, updated_at`,
		githubID, email, pu.Name, pu.AvatarURL, canCreateHub, model.PersonalFreePlanID, now,
	).Scan(&newUser.ID, &newUser.GitHubID, &newUser.Email, &newUser.Name,
		&newUser.DisplayName, &newUser.AvatarURL, &newUser.Plan,
		&newUser.PersonalPlanID, &newUser.CanCreateHub, &newUser.CreatedAt, &newUser.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Create auth_identity in the same transaction
	_, err = tx.Exec(ctx,
		`INSERT INTO auth_identities (user_id, provider, provider_id, provider_email, provider_name, provider_avatar)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		newUser.ID, pu.Provider, pu.ProviderID, email, pu.Name, pu.AvatarURL)
	if err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}

	// In invite_only mode: backfill used_by, set users.invite_id, update waitlist entry.
	// Errors abort the transaction — partial metadata is a data integrity problem.
	// Gated on inviteClaimed (not just token != "") because org-member
	// signups with a stale token skip the claim and must also skip the
	// metadata writes — otherwise we'd stamp used_by onto the previous
	// holder's invite row.
	if inviteClaimed {
		if _, err = tx.Exec(ctx,
			`UPDATE waitlist_invites SET used_by = $1::uuid WHERE token = $2`,
			newUser.ID, opts.InviteToken); err != nil {
			return nil, fmt.Errorf("set invite used_by: %w", err)
		}
		// Link user back to the invite that created them
		if _, err = tx.Exec(ctx,
			`UPDATE users SET invite_id = (SELECT id FROM waitlist_invites WHERE token = $1) WHERE id = $2::uuid`,
			opts.InviteToken, newUser.ID); err != nil {
			return nil, fmt.Errorf("set user invite_id: %w", err)
		}
		if _, err = tx.Exec(ctx,
			`UPDATE waitlist SET status = 'registered', updated_at = now()
			 WHERE id = (SELECT waitlist_id FROM waitlist_invites WHERE token = $1)`,
			opts.InviteToken); err != nil {
			return nil, fmt.Errorf("mark waitlist entry registered: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	slog.Info("created new user via provider",
		"provider", pu.Provider, "user_id", newUser.ID, "email", email)

	if h.store != nil {
		h.ensurePersonalHub(&newUser)

		// Plan 23 §5.2 — enqueue the seed-memory copy job after the
		// personal hub exists. Errors are logged but don't fail signup;
		// the user can still use the app, they just won't get the
		// onboarding seeds. River unique-arg + DB partial unique index
		// make the job idempotent on retry.
		if h.jobInserter != nil {
			if err := h.jobInserter.Insert(ctx, queue.CopySeedMemoriesArgs{
				UserID: newUser.ID,
			}, nil); err != nil {
				slog.Warn("failed to enqueue seed-memory copy job",
					"error", err, "user_id", newUser.ID)
			}
		}

		// Plan 18 — emit the founder note + first-week checklist
		// inline. The two rows are upserted via
		// notifications_source_unique so a signup retry that lands
		// twice collapses cleanly. Errors are logged but don't fail
		// signup — the user can still use the app, they just won't
		// see the pinned onboarding region.
		if h.onboardingEmitter != nil {
			if err := h.onboardingEmitter.EmitWelcome(ctx, newUser.ID); err != nil {
				slog.Warn("failed to emit onboarding welcome",
					"error", err, "user_id", newUser.ID)
			}
		}

		// Auto-grant admin role if email matches ADMIN_EMAILS
		if h.isAdminEmail(newUser.Email) {
			if _, seedErr := h.store.EnsureAdminRolesByEmail(ctx, []string{newUser.Email}, "super_admin"); seedErr != nil {
				slog.Warn("failed to auto-grant admin role", "error", seedErr, "email", newUser.Email)
			} else {
				slog.Info("auto-granted admin role to new user", "email", newUser.Email)
			}
		}

		// Upgrade invite-only new users to early_access plan.
		// The invite was already claimed atomically in the tx above, so
		// ConsumeInviteForUser (called later by loginOrCreateUser) will
		// skip because status='used'. We handle the plan upgrade here
		// for the new-user path. Gated on inviteClaimed so org-member
		// signups without a real invite don't get the early_access bump.
		if inviteClaimed && h.planChanger != nil {
			if err := h.planChanger.ChangePlan(ctx, newUser.ID, model.PersonalEarlyAccessPlanID, "waitlist_invite"); err != nil {
				slog.Warn("failed to upgrade new user to early_access plan",
					"error", err, "user_id", newUser.ID)
			} else {
				newUser.PersonalPlanID = model.PersonalEarlyAccessPlanID
				newUser.Plan = model.PersonalEarlyAccessPlanID // keep Plan in sync for response
				slog.Info("new user upgraded to early_access plan",
					"user_id", newUser.ID)
			}
		}
	}
	return &newUser, nil
}

// ConsumeInviteForUser validates and consumes an invite token for a user (new or existing).
// Checks email match to prevent cross-user invite theft.
//
// Exported so the admin waitlist reconcile endpoint can reuse the same
// logic when repairing orphan invites — calling a parallel helper
// would risk drift on a critical state machine (invite status,
// waitlist status, can_create_hub, plan upgrade, users.invite_id
// link). The function is intentionally idempotent: early-returns on
// expired / used / email-mismatch invites and uses `WHERE invite_id
// IS NULL` on the final link, so re-invocation is safe.
func (h *AuthHandler) ConsumeInviteForUser(ctx context.Context, token string, user *model.User) {
	invite, err := h.store.GetWaitlistInviteByToken(ctx, token)
	if err != nil || invite.Status != "active" || invite.ExpiresAt.Before(time.Now()) {
		return // invalid/expired/used — nothing to consume
	}

	// Email match: prevent user A from consuming user B's invite
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(user.Email))
	normalizedInviteEmail := strings.ToLower(strings.TrimSpace(invite.Email))
	if normalizedUserEmail != normalizedInviteEmail {
		slog.Warn("invite email mismatch — not consuming",
			"user_id", user.ID, "user_email", normalizedUserEmail)
		return
	}

	if consumeErr := h.store.ConsumeWaitlistInvite(ctx, token, user.ID); consumeErr != nil {
		tokenPrefix := token
		if len(tokenPrefix) > 8 {
			tokenPrefix = tokenPrefix[:8]
		}
		slog.Warn("failed to consume invite", "error", consumeErr, "token_prefix", tokenPrefix)
		return
	}

	// Update waitlist entry status
	h.store.UpdateWaitlistEntry(ctx, invite.WaitlistID, "registered", "", nil, "")

	// Upgrade permissions if not already set.
	// Deprecated: ownership is now controlled by personal_plan_id's
	// max_owned_free_team_hubs field. This boolean remains as a legacy
	// fallback for admin queries until it's removed in a future migration.
	if !user.CanCreateHub {
		h.store.SetCanCreateHub(ctx, user.ID, true)
	}

	// Upgrade waitlist-approved users to early_access plan via billing service.
	// Uses ChangePlan for validation, subscription tracking, and audit logging.
	if h.planChanger != nil && (user.PersonalPlanID == "" || user.PersonalPlanID == model.PersonalFreePlanID) {
		if err := h.planChanger.ChangePlan(ctx, user.ID, model.PersonalEarlyAccessPlanID, "waitlist_invite"); err != nil {
			slog.Warn("failed to upgrade to early_access plan",
				"error", err, "user_id", user.ID)
		} else {
			user.PersonalPlanID = model.PersonalEarlyAccessPlanID
			user.Plan = model.PersonalEarlyAccessPlanID // keep Plan in sync for response
			slog.Info("user upgraded to early_access plan",
				"user_id", user.ID)
		}
	}

	// Link user to invite (only if not already linked to a different invite)
	if _, linkErr := h.pool.Exec(ctx,
		`UPDATE users SET invite_id = $1::uuid WHERE id = $2::uuid AND invite_id IS NULL`,
		invite.ID, user.ID); linkErr != nil {
		slog.Warn("failed to set invite_id on existing user",
			"error", linkErr, "user_id", user.ID, "invite_id", invite.ID)
	}

	slog.Info("invite consumed", "user_id", user.ID, "invite_id", invite.ID)
}

// isAdminEmail checks if an email is in the ADMIN_EMAILS list.
func (h *AuthHandler) isAdminEmail(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	for _, e := range h.adminEmails {
		if e == normalized {
			return true
		}
	}
	return false
}

// --- Google OAuth handlers ---

// GoogleLogin initiates the Google OAuth flow.
func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.googleClientID == "" {
		writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
			Error: &model.Error{Code: "auth_disabled", Message: "Google OAuth not configured."},
		})
		return
	}

	clientRedirect := r.URL.Query().Get("redirect_uri")
	state, err := h.createOAuthState(r.Context(), "google", "login", clientRedirect, "")
	if err != nil {
		slog.Error("failed to create OAuth state", "error", err)
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_redirect", Message: err.Error()},
		})
		return
	}

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&scope=openid+email+profile&response_type=code&state=%s",
		h.googleClientID, url.QueryEscape(h.googleRedirectURL), state,
	)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// GoogleCallback handles the OAuth callback from Google.
func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateToken := r.URL.Query().Get("state")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "missing_code", Message: "No authorization code provided."},
		})
		return
	}

	// Consume and validate state
	state, err := h.consumeOAuthState(r.Context(), stateToken, "google")
	if err != nil {
		slog.Warn("invalid Google OAuth state", "error", err)
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_state", Message: "Invalid or expired login state. Please try again."},
		})
		return
	}

	// Exchange code for access token
	token, err := h.exchangeGoogleCode(r.Context(), code)
	if err != nil {
		slog.Error("Google token exchange failed", "error", err)
		writeJSON(w, http.StatusBadGateway, model.ApiResponse{
			Error: &model.Error{Code: "google_error", Message: "Failed to authenticate with Google."},
		})
		return
	}

	// Get user info
	gu, err := h.getGoogleUser(r.Context(), token)
	if err != nil {
		slog.Error("Google user info failed", "error", err)
		writeJSON(w, http.StatusBadGateway, model.ApiResponse{
			Error: &model.Error{Code: "google_error", Message: "Failed to get Google user info."},
		})
		return
	}

	pu := providerUser{
		Provider:      "google",
		ProviderID:    gu.ID,
		Email:         gu.Email,
		EmailVerified: gu.VerifiedEmail,
		Name:          gu.Name,
		AvatarURL:     gu.Picture,
	}

	if !h.isGoogleEmailAllowed(pu.Email) {
		slog.Warn("Google OAuth account rejected by allowlist", "email", pu.Email, "flow", state.Flow)
		if state.Flow == "link" {
			h.redirectOrHandleLinkError(w, r, state.ClientRedirect, ErrProviderAccountNotAllowed)
			return
		}
		h.redirectOrHandleLoginError(w, r, state.ClientRedirect, "account_not_allowed", "This Google account is not allowed to sign in to memax.")
		return
	}

	// Route by flow
	switch state.Flow {
	case "link":
		if state.UserID == nil {
			writeJSON(w, http.StatusBadRequest, model.ApiResponse{
				Error: &model.Error{Code: "invalid_state", Message: "Link flow missing user context."},
			})
			return
		}
		if err := h.linkProviderToUser(*state.UserID, pu); err != nil {
			h.redirectOrHandleLinkError(w, r, state.ClientRedirect, err)
			return
		}
		// Redirect back to settings
		if state.ClientRedirect != "" {
			h.redirectLinkResult(w, r, state.ClientRedirect, "google")
			return
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "linked"}})
		return

	default: // "login"
		inviteToken := extractInviteToken(state.ClientRedirect)
		user, err := h.loginOrCreateUser(r.Context(), pu, loginOpts{
			InviteToken:    inviteToken,
			ClientRedirect: state.ClientRedirect,
		})
		if err != nil {
			if errors.Is(err, ErrRegistrationRequired) {
				h.redirectOrHandleLoginError(w, r, state.ClientRedirect,
					"registration_required", "Account registration requires an invite.")
				return
			}
			if errors.Is(err, ErrInvalidInvite) {
				h.redirectOrHandleLoginError(w, r, state.ClientRedirect,
					"invalid_invite", "This invite is invalid or expired.")
				return
			}
			if errors.Is(err, ErrInviteEmailMismatch) {
				h.redirectOrHandleLoginError(w, r, state.ClientRedirect,
					"invite_email_mismatch", "This invite was sent to a different email. Please sign in with the email that received the invite.")
				return
			}
			slog.Error("Google login failed", "error", err, "email", pu.Email)
			writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
				Error: &model.Error{Code: "login_failed", Message: "Failed to log in. " + err.Error()},
			})
			return
		}
		h.completeLogin(w, r, user, state.ClientRedirect)
	}
}

func (h *AuthHandler) isGoogleEmailAllowed(email string) bool {
	// In org_gated mode, the allowlist IS the gate for Google (GitHub
	// uses checkOrgMembership which fails closed by construction).
	// An empty allowlist with registrationMode=org_gated must mean
	// "nobody allowed" — fail closed. The previous fail-open default
	// let any Google account sign in whenever the OAUTH_GOOGLE_ALLOWED_*
	// env vars were unset, bypassing the gate entirely.
	//
	// In open / invite_only modes an empty allowlist remains a no-op:
	// open = anyone can register; invite_only = the invite token check
	// inside loginOrCreateUser is the real gate.
	if len(h.googleAllowedEmails) == 0 && len(h.googleAllowedDomains) == 0 {
		return h.registrationMode != "org_gated"
	}

	canonical := strings.ToLower(strings.TrimSpace(email))
	if canonical == "" {
		return false
	}
	if _, ok := h.googleAllowedEmails[canonical]; ok {
		return true
	}

	at := strings.LastIndex(canonical, "@")
	if at < 0 || at == len(canonical)-1 {
		return false
	}
	domain := canonical[at+1:]
	_, ok := h.googleAllowedDomains[domain]
	return ok
}

// --- Google API helpers ---

type googleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// oauthHTTPClient returns an HTTP client with a 10-second timeout bound to the
// request context. Auth endpoints must not hang indefinitely on upstream calls.
func oauthHTTPClient(ctx context.Context) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: http.DefaultTransport}
}

func (h *AuthHandler) exchangeGoogleCode(ctx context.Context, code string) (string, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {h.googleClientID},
		"client_secret": {h.googleClientSecret},
		"redirect_uri":  {h.googleRedirectURL},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oauthHTTPClient(ctx).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Bound body read to 64 KB — token responses are tiny
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read google token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse google token response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("google token error: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("google returned empty access token")
	}
	return result.AccessToken, nil
}

func (h *AuthHandler) getGoogleUser(ctx context.Context, accessToken string) (*googleUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := oauthHTTPClient(ctx).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read google userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo endpoint returned %d: %s", resp.StatusCode, body)
	}

	var user googleUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("parse google userinfo response: %w", err)
	}
	return &user, nil
}

// --- Account linking ---

var (
	ErrIdentityTaken             = fmt.Errorf("identity_taken")
	ErrEmailBelongsToAnotherUser = fmt.Errorf("email_belongs_to_another_account")
	ErrLastIdentity              = fmt.Errorf("last_identity")
	ErrEmailNotVerified          = fmt.Errorf("email_not_verified")
	ErrProviderAccountNotAllowed = fmt.Errorf("provider_account_not_allowed")
)

func (h *AuthHandler) linkProviderToUser(currentUserID string, pu providerUser) error {
	if !pu.EmailVerified {
		return ErrEmailNotVerified
	}

	// Check 1: is this provider identity already linked to any user?
	existingUser, err := h.store.GetUserByAuthIdentity(pu.Provider, pu.ProviderID)
	if err == nil && existingUser != nil {
		if existingUser.ID == currentUserID {
			return nil // already linked — no-op
		}
		return ErrIdentityTaken
	}

	// Check 2: does this provider's verified email belong to a different user?
	if pu.Email != "" {
		email := strings.ToLower(strings.TrimSpace(pu.Email))
		emailOwner, err := h.store.GetUserByCanonicalEmail(email)
		if err == nil && emailOwner != nil && emailOwner.ID != currentUserID {
			return ErrEmailBelongsToAnotherUser
		}
	}

	if err := h.store.CreateAuthIdentity(currentUserID, pu.Provider, pu.ProviderID, pu.Email, pu.Name, pu.AvatarURL); err != nil {
		if errors.Is(err, store.ErrIdentityConflict) {
			return ErrIdentityTaken
		}
		return err
	}
	// Keep users.github_id in sync for backward-compat transition
	if pu.Provider == "github" {
		h.backfillGitHubID(context.Background(), currentUserID, pu.ProviderID)
	}
	return nil
}

func (h *AuthHandler) unlinkProvider(currentUserID, provider string) error {
	identities, _ := h.store.ListAuthIdentities(currentUserID)
	if len(identities) <= 1 {
		return ErrLastIdentity
	}
	if err := h.store.DeleteAuthIdentity(currentUserID, provider); err != nil {
		return err
	}
	// Clear users.github_id when unlinking GitHub for backward-compat transition
	if provider == "github" {
		h.clearGitHubID(context.Background(), currentUserID)
	}
	return nil
}

func (h *AuthHandler) handleLinkError(w http.ResponseWriter, err error) {
	status, code, message := linkErrorResponse(err)
	writeJSON(w, status, model.ApiResponse{
		Error: &model.Error{Code: code, Message: message},
	})
}

func linkErrorResponse(err error) (int, string, string) {
	switch err {
	case ErrIdentityTaken:
		return http.StatusConflict, "identity_taken", "This account is already linked to another memax account."
	case ErrEmailBelongsToAnotherUser:
		return http.StatusConflict, "email_conflict", "This email is already associated with another memax account."
	case ErrLastIdentity:
		return http.StatusBadRequest, "last_identity", "Cannot remove your last login method."
	case ErrEmailNotVerified:
		return http.StatusBadRequest, "email_not_verified", "Your email is not verified with this provider."
	case ErrProviderAccountNotAllowed:
		return http.StatusForbidden, "account_not_allowed", "This account is not allowed to sign in to memax."
	default:
		return http.StatusInternalServerError, "internal", "Failed to link account."
	}
}

func (h *AuthHandler) redirectOrHandleLinkError(w http.ResponseWriter, r *http.Request, clientRedirect string, err error) {
	if clientRedirect == "" {
		h.handleLinkError(w, err)
		return
	}
	_, code, _ := linkErrorResponse(err)
	redirectURL := appendQueryParam(clientRedirect, "account_link_error", code)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) redirectLinkResult(w http.ResponseWriter, r *http.Request, clientRedirect, provider string) {
	redirectURL := appendQueryParam(clientRedirect, "account_linked", provider)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

func appendQueryParam(raw, key, value string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func (h *AuthHandler) redirectOrHandleLoginError(w http.ResponseWriter, r *http.Request, clientRedirect, code, message string) {
	if clientRedirect == "" {
		writeJSON(w, http.StatusForbidden, model.ApiResponse{
			Error: &model.Error{Code: code, Message: message},
		})
		return
	}
	redirectURL := appendQueryParam(clientRedirect, "error", code)
	redirectURL = appendQueryParam(redirectURL, "error_description", message)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// backfillGitHubID sets users.github_id for an existing user when they auto-link
// a GitHub identity. This keeps backward-compat queries (ON CONFLICT github_id)
// working during the transition to auth_identities.
// backfillUserProfile updates empty user fields (email, name, avatar) from a
// fresh provider login. This handles accounts created before the verified-email
// fix — they may have empty emails that are now available from /user/emails.
func (h *AuthHandler) backfillUserProfile(ctx context.Context, user *model.User, pu providerUser, email string) {
	needsUpdate := false
	newEmail := user.Email
	newName := user.Name
	newAvatar := user.AvatarURL

	if user.Email == "" && email != "" {
		newEmail = email
		needsUpdate = true
	}
	if user.Name == "" && pu.Name != "" {
		newName = pu.Name
		needsUpdate = true
	}
	if user.AvatarURL == "" && pu.AvatarURL != "" {
		newAvatar = pu.AvatarURL
		needsUpdate = true
	}

	if !needsUpdate {
		return
	}

	_, err := h.pool.Exec(ctx,
		`UPDATE users SET email = $1, name = $2, avatar_url = $3, updated_at = now()
		 WHERE id = $4::uuid AND (email = '' OR email IS NULL OR name = '' OR name IS NULL OR avatar_url = '' OR avatar_url IS NULL)`,
		newEmail, newName, newAvatar, user.ID)
	if err != nil {
		slog.Warn("failed to backfill user profile", "error", err, "user_id", user.ID)
		return
	}

	// Update the in-memory user so callers see fresh data
	user.Email = newEmail
	user.Name = newName
	user.AvatarURL = newAvatar

	if pu.Provider == "github" {
		h.backfillGitHubID(ctx, user.ID, pu.ProviderID)
	}

	slog.Info("backfilled user profile from provider",
		"user_id", user.ID, "provider", pu.Provider, "email_filled", user.Email != newEmail)
}

func (h *AuthHandler) backfillGitHubID(ctx context.Context, userID, providerID string) {
	var githubID int64
	if _, err := fmt.Sscanf(providerID, "%d", &githubID); err != nil || githubID == 0 {
		return
	}
	_, err := h.pool.Exec(ctx,
		`UPDATE users SET github_id = $1 WHERE id = $2::uuid AND (github_id IS NULL OR github_id = 0)`,
		githubID, userID)
	if err != nil {
		slog.Warn("failed to backfill github_id", "error", err, "user_id", userID)
	}
}

// clearGitHubID nulls out users.github_id when the user unlinks GitHub.
// Keeps the legacy column in sync with auth_identities.
func (h *AuthHandler) clearGitHubID(ctx context.Context, userID string) {
	_, err := h.pool.Exec(ctx,
		`UPDATE users SET github_id = NULL WHERE id = $1::uuid`,
		userID)
	if err != nil {
		slog.Warn("failed to clear github_id", "error", err, "user_id", userID)
	}
}

// --- Shared login completion ---

// completeLogin issues tokens and redirects to the client, or returns JSON.
// Used by both GitHub and Google callbacks.
func (h *AuthHandler) completeLogin(w http.ResponseWriter, r *http.Request, user *model.User, clientRedirect string) {
	tokens, err := h.issueTokens(user.ID)
	if err != nil {
		slog.Error("token issuance failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to issue tokens."},
		})
		return
	}

	track(user.ID, "api.auth.login", map[string]any{"name": user.Name, "email": user.Email})

	if clientRedirect != "" {
		authCode := generateToken()
		_, err := h.pool.Exec(context.Background(),
			`INSERT INTO auth_codes (code, user_id, expires_at) VALUES ($1, $2, $3)`,
			authCode, user.ID, time.Now().Add(60*time.Second))
		if err != nil {
			slog.Error("failed to store auth code", "error", err)
			writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
				Error: &model.Error{Code: "internal", Message: "Failed to issue auth code."},
			})
			return
		}
		// Use appendQueryParam (not a raw `%s?code=%s` concat) because
		// clientRedirect may already carry an `?invite=TOKEN` suffix
		// from the register flow. Naively appending `?code=...`
		// produces `.../auth/callback?invite=TOKEN?code=CODE` — a
		// malformed URL where the frontend's URLSearchParams sees a
		// single `invite` param whose value contains the literal
		// `?code=...`, so it never reads the auth code and the user
		// lands on the "login not complete" error page even though
		// the server-side invite consumption + login already
		// succeeded.
		redirectURL := appendQueryParam(clientRedirect, "code", authCode)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tokens})
}

// --- Linking HTTP endpoints ---

// LinkProvider initiates provider linking for the current user.
// GET /v1/auth/link/{provider}?redirect_uri=...
func (h *AuthHandler) LinkProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	// Validate provider and config before creating state — avoids throwaway rows
	switch provider {
	case "github":
		if h.clientID == "" {
			writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
				Error: &model.Error{Code: "auth_disabled", Message: "GitHub OAuth not configured."},
			})
			return
		}
	case "google":
		if h.googleClientID == "" {
			writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
				Error: &model.Error{Code: "auth_disabled", Message: "Google OAuth not configured."},
			})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_provider", Message: "Unsupported provider: " + provider},
		})
		return
	}

	clientRedirect := r.URL.Query().Get("redirect_uri")
	state, err := h.createOAuthState(r.Context(), provider, "link", clientRedirect, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_redirect", Message: err.Error()},
		})
		return
	}

	var authURL string
	switch provider {
	case "github":
		authURL = fmt.Sprintf(
			"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user,user:email,read:org&state=%s",
			h.clientID, h.redirectURL, state,
		)
	case "google":
		authURL = fmt.Sprintf(
			"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&scope=openid+email+profile&response_type=code&state=%s",
			h.googleClientID, url.QueryEscape(h.googleRedirectURL), state,
		)
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// UnlinkProvider disconnects a provider from the current user.
// DELETE /v1/auth/link/{provider}
func (h *AuthHandler) UnlinkProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	if err := h.unlinkProvider(userID, provider); err != nil {
		h.handleLinkError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "unlinked"}})
}

// ListIdentities returns the current user's connected providers.
// GET /v1/auth/identities
func (h *AuthHandler) ListIdentities(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	identities, err := h.store.ListAuthIdentities(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to list identities."},
		})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: identities})
}
