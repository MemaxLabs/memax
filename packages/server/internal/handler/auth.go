package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type AuthHandler struct {
	pool            *pgxpool.Pool
	jwtSecret       []byte
	clientID        string           // GitHub OAuth
	clientSecret    string           // GitHub OAuth
	redirectURL     string           // GitHub OAuth callback URL
	requiredOrg     string           // if set, only members of this GitHub org can log in
	devOrg          string           // if set, members of this GitHub org get dev_access=true
	mcpOAuth        *MCPOAuthHandler // set after init to handle MCP OAuth callbacks
	store           store.Store      // for hub/usage operations (set via SetStore)
	eventsPublisher events.Publisher // user-axis SSE events (set via SetEventsPublisher)

	// Google OAuth (optional — disabled if empty)
	googleClientID       string
	googleClientSecret   string
	googleRedirectURL    string
	googleAllowedEmails  map[string]struct{}
	googleAllowedDomains map[string]struct{}

	// API base URL for computing provider callback URIs.
	apiBaseURL string

	// Redirect allowlist for OAuth state client_redirect
	redirectAllowlist []string

	// Registration gating — controls whether new accounts can be created.
	// "open" (default), "invite_only" (waitlist), "org_gated" (staging)
	registrationMode string

	// Comma-separated admin emails for auto-granting on registration
	adminEmails []string

	// planChanger handles plan changes for waitlist invite flow.
	// Uses a minimal interface to avoid importing the billing package.
	planChanger planChanger

	// jobInserter enqueues post-signup async work (currently the
	// onboarding seed-memory copy job, plan 23). Optional — nil
	// disables the enqueue. Minimal interface so test handlers can
	// pass a stub without depending on the queue package.
	jobInserter jobInserter

	// onboardingEmitter inserts the plan 18 super-notif welcome rows
	// (founder note + first-week checklist) on new-user signup. Nil
	// = disabled — signup still works, the user just doesn't see the
	// pinned onboarding region.
	onboardingEmitter onboardingEmitter

	// enqueueEmail delivers transactional emails (currently the email
	// OTP sign-in code). Nil disables the email sign-in surface — the
	// handler returns 503 rather than storing a row whose email will
	// never arrive.
	enqueueEmail func(template, to string, vars map[string]string) error
}

// onboardingEmitter is the plan-18 producer surface. Minimal
// interface so AuthHandler doesn't import the onboarding package and
// test handlers can pass a no-op stub.
type onboardingEmitter interface {
	EmitWelcome(ctx context.Context, userID string) error
}

// SetOnboardingEmitter wires the plan-18 welcome emitter. Called
// once at app boot. Fire-and-forget — a failure inside EmitWelcome
// is logged at the call site, never fails signup.
func (h *AuthHandler) SetOnboardingEmitter(e onboardingEmitter) {
	h.onboardingEmitter = e
}

// jobInserter enqueues River jobs from the auth handler. Satisfied by
// *queue.Client.
type jobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) error
}

// planChanger changes a user's plan. Satisfied by billing.Service.
type planChanger interface {
	ChangePlan(ctx context.Context, userID, newPlanID, source string) error
}

// SetPlanChanger wires the billing service for early access plan assignment.
func (h *AuthHandler) SetPlanChanger(pc planChanger) {
	h.planChanger = pc
}

const defaultAPIBaseURL = "http://localhost:8080"

// SetMCPOAuth wires the MCP OAuth handler for callback delegation.
func (h *AuthHandler) SetMCPOAuth(mcp *MCPOAuthHandler) {
	h.mcpOAuth = mcp
}

// SetStore wires the store for hub and usage operations.
func (h *AuthHandler) SetStore(s store.Store) {
	h.store = s
}

// SetEventsPublisher wires the user-axis SSE publisher. Called once
// at app boot from serverapp/routes.go. Fire-and-forget — if unset,
// publishers are no-ops (helpers guard nil) and the reconcile safety
// net in the web client recovers freshness on reconnect.
func (h *AuthHandler) SetEventsPublisher(p events.Publisher) {
	h.eventsPublisher = p
}

// SetJobInserter wires the River insert client used to enqueue
// post-signup jobs (currently the onboarding seed-memory copy job per
// plan 23). Optional — nil disables the enqueue without breaking
// signup, so test handlers and CLI-only configurations stay clean.
func (h *AuthHandler) SetJobInserter(j jobInserter) {
	h.jobInserter = j
}

// SetEnqueueEmail wires the transactional email enqueue callback used
// by the email OTP sign-in path. Same shape as HubsHandler /
// WaitlistHandler so the wiring in serverapp/app.go can pass the
// shared closure. Nil keeps email sign-in disabled.
func (h *AuthHandler) SetEnqueueEmail(fn func(template, to string, vars map[string]string) error) {
	h.enqueueEmail = fn
}

func RequiredJWTSecret() ([]byte, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required when auth is enabled")
	}
	return []byte(secret), nil
}

func resolveAPIBaseURL() string {
	baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
	if baseURL == "" {
		return defaultAPIBaseURL
	}
	return baseURL
}

func providerCallbackURL(apiBaseURL, provider string) string {
	baseURL := strings.TrimRight(apiBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	return baseURL + "/v1/auth/" + provider + "/callback"
}

func resolveGitHubRedirectURL(apiBaseURL string) string {
	if redirectURL := strings.TrimSpace(os.Getenv("OAUTH_GITHUB_REDIRECT_URL")); redirectURL != "" {
		return redirectURL
	}
	return providerCallbackURL(apiBaseURL, "github")
}

func parseOAuthAllowlist(raw string) map[string]struct{} {
	entries := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		entries[value] = struct{}{}
	}
	return entries
}

func NewAuthHandler(pool *pgxpool.Pool) (*AuthHandler, error) {
	jwtSecret, err := RequiredJWTSecret()
	if err != nil {
		return nil, err
	}
	apiBaseURL := resolveAPIBaseURL()
	redirectURL := resolveGitHubRedirectURL(apiBaseURL)
	requiredOrg := os.Getenv("OAUTH_GITHUB_REQUIRED_ORG")
	if requiredOrg != "" {
		slog.Info("org restriction enabled", "org", requiredOrg)
	}
	devOrg := os.Getenv("DEV_GITHUB_ORG")
	if devOrg != "" {
		slog.Info("dev org enabled", "org", devOrg)
	}
	clientID := os.Getenv("OAUTH_GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_GITHUB_CLIENT_SECRET")

	// Google OAuth config (optional)
	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	googleClientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	googleRedirectURL := providerCallbackURL(apiBaseURL, "google")
	googleAllowedEmails := parseOAuthAllowlist(os.Getenv("OAUTH_GOOGLE_ALLOWED_EMAILS"))
	googleAllowedDomains := parseOAuthAllowlist(os.Getenv("OAUTH_GOOGLE_ALLOWED_DOMAINS"))
	if googleClientID != "" {
		slog.Info("Google OAuth enabled",
			"redirect_url", googleRedirectURL,
			"allowed_email_count", len(googleAllowedEmails),
			"allowed_domain_count", len(googleAllowedDomains),
		)
	}

	// Redirect allowlist for OAuth state client_redirect. Web redirects are
	// constrained to APP_BASE_URL; CLI loopback redirects are allowed separately
	// by isAllowedRedirect.
	allowlist := redirectAllowlistFromAppBaseURL(os.Getenv("APP_BASE_URL"))

	regMode := os.Getenv("REGISTRATION_MODE")
	if regMode == "" {
		regMode = "open"
	}
	slog.Info("registration mode", "mode", regMode)

	var adminEmailList []string
	if raw := os.Getenv("ADMIN_EMAILS"); raw != "" {
		for _, e := range strings.Split(raw, ",") {
			e = strings.ToLower(strings.TrimSpace(e))
			if e != "" {
				adminEmailList = append(adminEmailList, e)
			}
		}
	}

	return &AuthHandler{
		pool:                 pool,
		jwtSecret:            jwtSecret,
		clientID:             clientID,
		clientSecret:         clientSecret,
		redirectURL:          redirectURL,
		requiredOrg:          requiredOrg,
		devOrg:               devOrg,
		googleClientID:       googleClientID,
		googleClientSecret:   googleClientSecret,
		googleRedirectURL:    googleRedirectURL,
		googleAllowedEmails:  googleAllowedEmails,
		googleAllowedDomains: googleAllowedDomains,
		apiBaseURL:           apiBaseURL,
		redirectAllowlist:    allowlist,
		registrationMode:     regMode,
		adminEmails:          adminEmailList,
	}, nil
}

// GitHubLogin redirects to GitHub OAuth authorization page.
// GET /v1/auth/github?redirect_uri=http://localhost:3000/auth/callback
func (h *AuthHandler) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	if h.clientID == "" {
		writeJSON(w, http.StatusServiceUnavailable, model.ApiResponse{
			Error: &model.Error{Code: "auth_disabled", Message: "GitHub OAuth not configured. Set OAUTH_GITHUB_CLIENT_ID and OAUTH_GITHUB_CLIENT_SECRET."},
		})
		return
	}

	clientRedirect := r.URL.Query().Get("redirect_uri")

	// Use oauth_states for CSRF-safe state management
	if h.store != nil {
		state, err := h.createOAuthState(r.Context(), "github", "login", clientRedirect, "")
		if err != nil {
			slog.Error("failed to create OAuth state", "error", err)
			writeJSON(w, http.StatusBadRequest, model.ApiResponse{
				Error: &model.Error{Code: "invalid_redirect", Message: err.Error()},
			})
			return
		}
		authURL := fmt.Sprintf(
			"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user,user:email,read:org&state=%s",
			h.clientID, h.redirectURL, state,
		)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
		return
	}

	// Fallback: legacy state encoding (no store available)
	state := generateState(clientRedirect)
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user,user:email,read:org&state=%s",
		h.clientID, h.redirectURL, state,
	)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// GitHubCallback handles the OAuth callback from GitHub.
// GET /v1/auth/github/callback?code=xxx&state=yyy
func (h *AuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "missing_code", Message: "No authorization code provided."},
		})
		return
	}

	// Exchange code for GitHub access token
	ghToken, err := h.exchangeGitHubCode(code)
	if err != nil {
		slog.Error("github token exchange failed", "error", err)
		writeJSON(w, http.StatusBadGateway, model.ApiResponse{
			Error: &model.Error{Code: "github_error", Message: "Failed to authenticate with GitHub."},
		})
		return
	}

	// Get GitHub user info (always fetches verified email from /user/emails)
	ghUser, err := h.getGitHubUser(ghToken)
	if err != nil {
		slog.Error("github user fetch failed", "error", err)
		writeJSON(w, http.StatusBadGateway, model.ApiResponse{
			Error: &model.Error{Code: "github_error", Message: "Failed to get GitHub user info."},
		})
		return
	}

	// Org membership semantics depend on registration mode:
	//
	// - invite_only: CONVENIENCE. A member of the required org can
	//   register without a waitlist/hub invite; a non-member with a
	//   valid invite is still allowed through. Flows into
	//   loginOrCreateUser via loginOpts.ProviderOrgMember.
	// - org_gated: HARD GATE. New registrations require org
	//   membership; no invite override. Existing users (matching
	//   auth_identity) still log in — the gate fires later, inside
	//   loginOrCreateUser, only on the new-user path.
	// - open: ignored.
	//
	// An errored check degrades to "not a member" — transient GitHub
	// API blips shouldn't lock out invitees in invite_only. In
	// org_gated this degradation becomes a user-visible rejection on
	// transient failures, which is the acceptable tradeoff (retry is
	// cheap; letting a non-member through is not).
	isRequiredOrgMember := false
	if h.requiredOrg != "" {
		m, err := h.checkOrgMembership(ghToken, h.requiredOrg)
		if err != nil {
			slog.Warn("github org membership check failed; treating as non-member",
				"error", err, "user", ghUser.Login, "org", h.requiredOrg)
		} else if m {
			isRequiredOrgMember = true
			slog.Info("org membership verified", "user", ghUser.Login, "org", h.requiredOrg)
		}
	}

	// Check dev org membership (non-blocking — doesn't prevent login)
	devAccess := false
	if h.devOrg != "" {
		if isMember, err := h.checkOrgMembership(ghToken, h.devOrg); err == nil && isMember {
			devAccess = true
			slog.Info("dev org membership verified", "user", ghUser.Login, "org", h.devOrg)
		}
	}

	// Build a verified providerUser from the GitHub response.
	// Email and EmailVerified come from /user/emails (not the profile field).
	pu := providerUser{
		Provider:      "github",
		ProviderID:    fmt.Sprintf("%d", ghUser.ID),
		Email:         ghUser.Email,
		EmailVerified: ghUser.EmailVerified,
		Name:          ghUser.Name,
		AvatarURL:     ghUser.AvatarURL,
	}

	// --- MCP OAuth flow ---
	// state starts with "mcp:" — resolve user, then delegate to MCP handler.
	// MCP flow does not carry invite tokens; org membership is the
	// only signal that lets a new user register here. If requiredOrg
	// is set and the user isn't a member AND doesn't already have an
	// account, loginOrCreateUser returns ErrRegistrationRequired —
	// surfaced below as a clean 403.
	if strings.HasPrefix(state, "mcp:") && h.mcpOAuth != nil {
		user, loginErr := h.loginOrCreateUser(r.Context(), pu, loginOpts{
			ProviderOrgMember: isRequiredOrgMember,
		})
		if loginErr != nil {
			if errors.Is(loginErr, ErrRegistrationRequired) {
				writeJSON(w, http.StatusForbidden, model.ApiResponse{
					Error: &model.Error{Code: "not_authorized", Message: fmt.Sprintf("Access restricted to members of the %s GitHub organization or invited users.", h.requiredOrg)},
				})
				return
			}
			slog.Error("github mcp login failed", "error", loginErr)
			writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
				Error: &model.Error{Code: "login_failed", Message: "Failed to log in via GitHub."},
			})
			return
		}
		h.persistDevAccess(user.ID, devAccess)
		mcpSessionID := strings.TrimPrefix(state, "mcp:")
		h.mcpOAuth.HandleMCPCallback(w, r, user.ID, mcpSessionID)
		return
	}

	// --- New OAuth state flow (production with store) ---
	if h.store != nil {
		oauthState, stateErr := h.consumeOAuthState(r.Context(), state, "github")
		if stateErr != nil {
			// Fail closed: invalid, expired, consumed, or provider-mismatched state → 400
			slog.Warn("invalid GitHub OAuth state", "error", stateErr)
			writeJSON(w, http.StatusBadRequest, model.ApiResponse{
				Error: &model.Error{Code: "invalid_state", Message: "Invalid or expired login state. Please try again."},
			})
			return
		}

		switch oauthState.Flow {
		case "link":
			if oauthState.UserID == nil {
				writeJSON(w, http.StatusBadRequest, model.ApiResponse{
					Error: &model.Error{Code: "invalid_state", Message: "Link flow missing user context."},
				})
				return
			}
			if linkErr := h.linkProviderToUser(*oauthState.UserID, pu); linkErr != nil {
				h.redirectOrHandleLinkError(w, r, oauthState.ClientRedirect, linkErr)
				return
			}
			if oauthState.ClientRedirect != "" {
				h.redirectLinkResult(w, r, oauthState.ClientRedirect, "github")
				return
			}
			writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "linked"}})
			return

		default: // "login"
			inviteToken := extractInviteToken(oauthState.ClientRedirect)
			user, loginErr := h.loginOrCreateUser(r.Context(), pu, loginOpts{
				InviteToken:       inviteToken,
				ClientRedirect:    oauthState.ClientRedirect,
				ProviderOrgMember: isRequiredOrgMember,
			})
			if loginErr != nil {
				if errors.Is(loginErr, ErrRegistrationRequired) {
					h.redirectOrHandleLoginError(w, r, oauthState.ClientRedirect,
						"registration_required", "Account registration requires an invite.")
					return
				}
				if errors.Is(loginErr, ErrInvalidInvite) {
					h.redirectOrHandleLoginError(w, r, oauthState.ClientRedirect,
						"invalid_invite", "This invite is invalid or expired.")
					return
				}
				if errors.Is(loginErr, ErrInviteEmailMismatch) {
					h.redirectOrHandleLoginError(w, r, oauthState.ClientRedirect,
						"invite_email_mismatch", "This invite was sent to a different email. Please sign in with the email that received the invite.")
					return
				}
				slog.Error("github login failed", "error", loginErr, "email", pu.Email)
				writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
					Error: &model.Error{Code: "login_failed", Message: "Failed to log in. " + loginErr.Error()},
				})
				return
			}
			h.persistDevAccess(user.ID, devAccess)
			h.completeLogin(w, r, user, oauthState.ClientRedirect)
			return
		}
	}

	// --- Legacy fallback (dev mode, no store/database) ---
	// Only reachable when h.store == nil (local dev without Postgres).
	user, err := h.upsertUser(ghUser)
	if err != nil {
		slog.Error("user upsert failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to create user."},
		})
		return
	}
	clientRedirect := parseState(state)
	h.completeLogin(w, r, user, clientRedirect)
}

// persistDevAccess stores the dev_access preference for the user.
// Records the grant timestamp so impersonation can expire stale grants.
// Clears dev_access_granted_at when access is revoked.
func (h *AuthHandler) persistDevAccess(userID string, devAccess bool) {
	if h.store != nil {
		prefs := map[string]any{"dev_access": devAccess}
		if devAccess {
			prefs["dev_access_granted_at"] = time.Now().UTC().Format(time.RFC3339)
		} else {
			prefs["dev_access_granted_at"] = nil
		}
		h.store.UpsertUserPreferences(userID, prefs)
	}
}

// Me returns the full user profile with plan, usage, and hubs.
// GET /v1/auth/me  (Authorization: Bearer <token>)
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromRequest(r, h.jwtSecret)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Invalid or missing access token."},
		})
		return
	}

	var user model.User
	// github_id is NULL for Google-only accounts (see auth_providers.go's
	// NULLIF($1, 0) insert), so COALESCE to 0 before scanning into
	// int64 — otherwise /v1/auth/me returns not_found for a valid
	// session. Mirrors the fix applied to GetUser / GetUsersByIDs.
	err := h.pool.QueryRow(context.Background(),
		`SELECT id, COALESCE(github_id, 0), email, name, COALESCE(display_name, ''), avatar_url,
			COALESCE(plan, 'free'), personal_plan_id, can_create_hub, created_at, updated_at
		FROM users WHERE id = $1`, userID).Scan(
		&user.ID, &user.GitHubID, &user.Email, &user.Name,
		&user.DisplayName, &user.AvatarURL, &user.Plan,
		&user.PersonalPlanID, &user.CanCreateHub, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		writeJSON(w, http.StatusNotFound, model.ApiResponse{
			Error: &model.Error{Code: "not_found", Message: "User not found."},
		})
		return
	}

	resp := model.MeResponse{User: user}

	// Connected providers (from auth_identities)
	if h.store != nil {
		identities, _ := h.store.ListAuthIdentities(userID)
		providers := make([]string, 0, len(identities))
		for _, id := range identities {
			providers = append(providers, id.Provider)
		}
		if len(providers) == 0 && user.GitHubID > 0 {
			providers = []string{"github"} // fallback for users not yet backfilled
		}
		resp.ConnectedProviders = providers
	}

	if h.store != nil {
		// Hubs for membership/context selection — includes memory counts via
		// a single JOIN so the hub switcher renders immediately without a
		// second round-trip to /v1/hubs.
		hubs, hubErr := h.store.ListUserHubs(userID)
		if hubErr != nil {
			slog.Warn("failed to list user hubs", "error", hubErr, "user_id", userID)
		}
		if hubs == nil {
			hubs = []model.HubWithRole{}
		}
		resp.Hubs = hubs

		// Usage
		usage, _ := h.store.GetCurrentUsage(userID)
		resp.Usage = usage

		// Dev access
		prefs, _ := h.store.GetUserPreferences(userID)
		if prefs != nil {
			merged := prefs.MergedSettings()
			if da, ok := merged["dev_access"].(bool); ok {
				resp.DevAccess = da
			}
		}
		// dev_access is set during login based on actual GitHub org membership check.
		// No override needed here — trust the stored preference.

		// Admin role
		adminRole, _ := h.store.GetAdminRole(context.Background(), userID)
		if adminRole != "" {
			resp.AdminRole = adminRole
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// UpdateMe updates the user's display name.
// PATCH /v1/auth/me
func (h *AuthHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromRequest(r, h.jwtSecret)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Invalid or missing access token."},
		})
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if h.store != nil {
		if err := h.store.UpdateUserProfile(userID, req.DisplayName); err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "updated", "display_name": req.DisplayName},
	})
}

// Impersonate issues tokens for another user, allowing a dev to debug as them.
// POST /v1/auth/impersonate  { "user_id": "..." } or { "email": "..." }
//
// Requires dev_access=true on the calling user (set via DEV_GITHUB_ORG membership).
// Only available when the store is configured (not in local no-DB mode).
func (h *AuthHandler) Impersonate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromRequest(r, h.jwtSecret)
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}
	callerID := claims.Sub

	// Block chained impersonation: an impersonation token cannot mint another.
	if claims.ImpersonatorID != "" {
		writeError(w, http.StatusForbidden, "forbidden", "Cannot impersonate while already impersonating.")
		return
	}

	// Gate: caller must have dev_access
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Impersonation requires a database.")
		return
	}
	prefs, _ := h.store.GetUserPreferences(callerID)
	devAccess := false
	devAccessStale := true
	if prefs != nil {
		merged := prefs.MergedSettings()
		if da, ok := merged["dev_access"].(bool); ok {
			devAccess = da
		}
		// Check freshness: dev_access must have been granted within the last 24 hours
		// (refreshed on each GitHub login). This prevents stale grants from persisting
		// after a user leaves the dev org.
		if ts, ok := merged["dev_access_granted_at"].(string); ok {
			if grantedAt, err := time.Parse(time.RFC3339, ts); err == nil {
				devAccessStale = time.Since(grantedAt) > 24*time.Hour
			}
		}
	}
	if !devAccess || devAccessStale {
		msg := "Impersonation requires dev access."
		if devAccess && devAccessStale {
			msg = "Dev access expired. Log in again via GitHub to refresh."
		}
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	if req.UserID == "" && req.Email == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide user_id or email.")
		return
	}

	// Resolve target user
	var targetID string
	if req.UserID != "" {
		// Verify user exists
		var exists bool
		err := h.pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1::uuid)`, req.UserID).Scan(&exists)
		if err != nil || !exists {
			writeError(w, http.StatusNotFound, "not_found", "User not found.")
			return
		}
		targetID = req.UserID
	} else {
		user, err := h.store.GetUserByCanonicalEmail(req.Email)
		if err != nil || user == nil {
			writeError(w, http.StatusNotFound, "not_found", "No user with that email.")
			return
		}
		targetID = user.ID
	}

	// Issue a short-lived, access-only impersonation token.
	// No refresh token — forces re-impersonation after 1 hour.
	// The JWT carries impersonator_id so downstream audit can attribute the actor.
	accessToken, err := auth.SignImpersonationToken(targetID, callerID, h.jwtSecret, time.Hour)
	if err != nil {
		slog.Error("impersonation token issuance failed", "error", err, "caller", callerID, "target", targetID)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to issue tokens.")
		return
	}

	slog.Warn("user impersonated",
		"caller_id", callerID,
		"target_id", targetID,
		"target_email", req.Email,
	)

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{
			"access_token": accessToken,
			"expires_in":   3600,
			"target_id":    targetID,
			"impersonated": true,
		},
	})
}

// Refresh exchanges a refresh token for a new access token.
// POST /v1/auth/refresh  { "refresh_token": "..." }
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Missing refresh_token."},
		})
		return
	}

	var session model.Session
	var agentName string
	var grantID string
	err := h.pool.QueryRow(context.Background(),
		`SELECT id, user_id, expires_at, COALESCE(agent_name, ''), COALESCE(grant_id::text, '')
		FROM sessions WHERE refresh_token = $1`,
		req.RefreshToken).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &agentName, &grantID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "invalid_token", Message: "Invalid refresh token."},
		})
		return
	}

	if time.Now().After(session.ExpiresAt) {
		// Clean up expired session
		h.pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, session.ID)
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "expired_token", Message: "Refresh token expired. Please log in again."},
		})
		return
	}

	var accessToken string
	if grantID != "" {
		grant := h.ResolveOAuthGrant(session.UserID, grantID)
		if grant.UserID == "" {
			h.pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, session.ID)
			writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
				Error: &model.Error{Code: "invalid_token", Message: "Authorization grant is no longer valid."},
			})
			return
		}
		accessToken, err = auth.SignGrantAccessToken(session.UserID, grant.AgentName, grantID, h.jwtSecret, time.Hour)
	} else if agentName != "" {
		accessToken, err = auth.SignAgentAccessToken(session.UserID, agentName, h.jwtSecret, time.Hour)
	} else {
		accessToken, err = auth.SignAccessToken(session.UserID, h.jwtSecret, time.Hour)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to issue access token."},
		})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: model.TokenPair{
			AccessToken:  accessToken,
			RefreshToken: req.RefreshToken, // same refresh token
			ExpiresIn:    3600,
		},
	})
}

// ExchangeCode exchanges a one-time auth code for tokens.
// POST /v1/auth/exchange  { "code": "..." }
func (h *AuthHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Missing code."},
		})
		return
	}

	var userID string
	var expiresAt time.Time
	var used bool
	var grantID string
	err := h.pool.QueryRow(context.Background(),
		`SELECT user_id, expires_at, used, COALESCE(grant_id::text, '') FROM auth_codes WHERE code = $1`, req.Code,
	).Scan(&userID, &expiresAt, &used, &grantID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "invalid_code", Message: "Invalid authorization code."},
		})
		return
	}

	if used || time.Now().After(expiresAt) {
		// Clean up
		h.pool.Exec(context.Background(), `DELETE FROM auth_codes WHERE code = $1`, req.Code)
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "expired_code", Message: "Authorization code expired or already used."},
		})
		return
	}
	if grantID != "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "invalid_code", Message: "This authorization code must be exchanged through the OAuth token endpoint."},
		})
		return
	}

	// Mark as used
	h.pool.Exec(context.Background(), `UPDATE auth_codes SET used = true WHERE code = $1`, req.Code)

	tokens, err := h.issueTokens(userID)
	if err != nil {
		slog.Error("token issuance failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to issue tokens."},
		})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tokens})
}

// CreateAPIKey generates a new API key for CI/CD and non-interactive use.
// POST /v1/auth/api-keys  { "name": "ci-deploy", "hub_id": "optional", "expires_in_days": 90 }
//
// If hub_id is provided, the key is scoped to that hub only.
// If hub_id is omitted, the key has access to all the user's hubs.
func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	var req struct {
		Name          string   `json:"name"`
		HubID         string   `json:"hub_id,omitempty"`
		HubIDs        []string `json:"hub_ids,omitempty"`
		AgentName     string   `json:"agent_name,omitempty"` // agent identity: "claude-code", "cursor", etc.
		ExpiresInDays int      `json:"expires_in_days,omitempty"`
		Scopes        []string `json:"scopes,omitempty"`
		Permissions   []string `json:"permissions,omitempty"`
		TrustLevel    string   `json:"trust_level,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Missing name."},
		})
		return
	}

	scopeBundles := req.Scopes
	if len(scopeBundles) == 0 && len(req.Permissions) == 0 {
		scopeBundles = []string{"read", "write"}
	}
	permissions, invalid := ExpandPermissionBundles(scopeBundles)
	if len(req.Permissions) > 0 {
		explicit, bad := PermissionSetFromStrings(req.Permissions)
		permissions = permissions.Union(explicit)
		invalid = append(invalid, bad...)
	}
	if len(invalid) > 0 {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_permissions", Message: "Unknown permission or grant: " + strings.Join(invalid, ", ")},
		})
		return
	}
	legacyScopes := scopeBundles
	if len(legacyScopes) == 0 && len(req.Permissions) == 0 {
		legacyScopes = []string{"read", "write"}
	}

	hubIDs := append([]string{}, req.HubIDs...)
	if req.HubID != "" {
		hubIDs = append([]string{req.HubID}, hubIDs...)
	}
	hubIDs = uniqueNonEmptyStrings(hubIDs)

	trustLevel := strings.TrimSpace(req.TrustLevel)
	if trustLevel == "" {
		if len(hubIDs) > 0 {
			trustLevel = TrustStandard
		} else {
			trustLevel = TrustElevated
		}
	}
	if !knownTrustLevel(trustLevel) {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_trust_level", Message: "Unknown trust level."},
		})
		return
	}

	hubScopeMode := HubScopeAllAccessible
	var hubID *string
	if len(hubIDs) > 0 {
		hubScopeMode = HubScopeAllowlist
		if h.store != nil {
			for _, id := range hubIDs {
				role, _ := h.store.GetHubMemberRole(id, userID)
				if role == "" {
					writeJSON(w, http.StatusForbidden, model.ApiResponse{
						Error: &model.Error{Code: "forbidden", Message: "You can only scope API keys to hubs you can access."},
					})
					return
				}
			}
		}
		hubID = &hubIDs[0]
	}

	// Generate key: mxk_ + 32 random hex bytes
	rawBytes := make([]byte, 32)
	rand.Read(rawBytes)
	plainKey := "mxk_" + hex.EncodeToString(rawBytes)

	// Store hash only
	hash := sha256.Sum256([]byte(plainKey))
	keyHash := hex.EncodeToString(hash[:])
	prefix := plainKey[:8] // "mxk_" + first 4 hex chars

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	var id string
	err := h.pool.QueryRow(context.Background(),
		`INSERT INTO api_keys (
			user_id, name, key_hash, prefix, scopes, expires_at, hub_id, agent_name,
			hub_scope_mode, hub_ids, default_permissions, trust_level
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8, $9, $10::text[]::uuid[], $11::text[], $12) RETURNING id`,
		userID, req.Name, keyHash, prefix, legacyScopes, expiresAt, hubID, req.AgentName,
		hubScopeMode, hubIDs, permissions.Strings(), trustLevel,
	).Scan(&id)
	if err != nil {
		slog.Error("failed to create API key", "error", err)
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to create API key."},
		})
		return
	}

	scope := "all hubs"
	if hubID != nil {
		scope = *hubID
		if len(hubIDs) > 1 {
			scope = fmt.Sprintf("%d hubs", len(hubIDs))
		}
	}

	writeJSON(w, http.StatusCreated, model.ApiResponse{
		Data: map[string]any{
			"id":                  id,
			"name":                req.Name,
			"key":                 plainKey, // only returned once
			"prefix":              prefix,
			"scope":               scope,
			"hub_id":              hubID,
			"hub_ids":             hubIDs,
			"hub_scope_mode":      hubScopeMode,
			"default_permissions": permissions.Strings(),
			"trust_level":         trustLevel,
			"expires_at":          expiresAt,
			"created_at":          time.Now(),
			"agent_name":          req.AgentName,
			"standalone":          false,
		},
	})
	track(userID, "api.auth.create_key", map[string]any{"key_name": req.Name, "hub_scoped": hubID != nil})

	// Auto-populate connected_agents registry
	EnsureConnectedAgent(h.store, userID, req.AgentName)

	// Emit user-axis SSE so the creator's other tabs/devices see the
	// new key + (if first-touch) the new agent without a reload.
	events.PublishKeyChanged(r.Context(), h.eventsPublisher, userID, id, "created")
	if req.AgentName != "" {
		events.PublishAgentChanged(r.Context(), h.eventsPublisher, userID, req.AgentName, "upserted")
	}
}

// ListAPIKeys returns all API keys for the authenticated user.
// GET /v1/auth/api-keys
func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	rows, err := h.pool.Query(context.Background(),
		`SELECT id, name, prefix, scopes, expires_at, last_used, created_at,
			hub_id, COALESCE(agent_name, ''),
			COALESCE(hub_scope_mode, 'all_accessible'),
			ARRAY(SELECT hub_id::text FROM unnest(COALESCE(hub_ids, ARRAY[]::uuid[])) AS hub_id),
			COALESCE(default_permissions, ARRAY[]::text[]),
			COALESCE(trust_level, 'elevated'),
			COALESCE(standalone, false)
		FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to list API keys."},
		})
		return
	}
	defer rows.Close()

	var keys []map[string]any
	for rows.Next() {
		var id, name, prefix, agentName string
		var scopes []string
		var hubIDs []string
		var defaultPermissions []string
		var expiresAt, lastUsed *time.Time
		var hubID *string
		var createdAt time.Time
		var hubScopeMode, trustLevel string
		var standalone bool
		if err := rows.Scan(&id, &name, &prefix, &scopes, &expiresAt, &lastUsed, &createdAt, &hubID, &agentName, &hubScopeMode, &hubIDs, &defaultPermissions, &trustLevel, &standalone); err != nil {
			continue
		}
		scope := "all hubs"
		if hubID != nil {
			scope = *hubID
			if len(hubIDs) > 1 {
				scope = fmt.Sprintf("%d hubs", len(hubIDs))
			}
		}
		entry := map[string]any{
			"id":                  id,
			"name":                name,
			"prefix":              prefix,
			"scopes":              scopes,
			"scope":               scope,
			"hub_id":              hubID,
			"hub_ids":             hubIDs,
			"hub_scope_mode":      hubScopeMode,
			"default_permissions": defaultPermissions,
			"trust_level":         trustLevel,
			"expires_at":          expiresAt,
			"last_used":           lastUsed,
			"created_at":          createdAt,
			"agent_name":          agentName,
			"standalone":          standalone,
		}
		keys = append(keys, entry)
	}

	if keys == nil {
		keys = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: keys})
}

// RevokeAPIKey deletes an API key.
//
// Returns a structured ApiKeyRevokeResult in the ApiResponse envelope.
// Always 200 — `not_found` and `revoke_failed` are reported as skip
// reasons in the result's `skipped` array, not as 4xx/5xx. This matches
// the memory batch-delete + config batch-delete pattern and makes
// scripted CLI revoke flows idempotent: re-running `memax auth
// revoke-key` against an already-revoked key exits 0.
//
// Skip reasons:
//   - not_found: the DELETE matched zero rows. Unknown id, already
//     revoked, or the key belongs to another user (the WHERE clause
//     makes these indistinguishable). Client treats as silent success.
//   - revoke_failed: the DELETE itself returned an error. Server still
//     has the key; client must roll back the optimistic removal and
//     surface retry copy.
//
// The 401 unauthorized branch is preserved as a hard error because
// there is no recoverable interpretation of a missing auth context.
//
// DELETE /v1/auth/api-keys/{id}
func (h *AuthHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}

	keyID := r.PathValue("id")

	// Capture the prior agent assignment before deleting so we can
	// fire agent.changed for the ex-owner's connected-agents refresh.
	// Non-fatal — if the lookup fails (e.g. the row is already gone),
	// we fall through to the normal DELETE path and the not_found
	// branch handles it. Empty string means either no assignment or
	// the row doesn't exist; we won't emit agent.changed in that case.
	var prevAgent string
	_ = h.pool.QueryRow(context.Background(),
		`SELECT COALESCE(agent_name, '') FROM api_keys WHERE id = $1 AND user_id = $2`,
		keyID, userID).Scan(&prevAgent)

	result, err := h.pool.Exec(context.Background(),
		`DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		slog.Error("revoke api key failed", "key_id", keyID, "user", userID, "err", err)
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.ApiKeyRevokeResult{
			Revoked: false,
			Skipped: []model.SkippedMemory{
				{ID: keyID, Reason: model.ApiKeyRevokeSkipRevokeFailed},
			},
		}})
		return
	}
	if result.RowsAffected() == 0 {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.ApiKeyRevokeResult{
			Revoked: false,
			Skipped: []model.SkippedMemory{
				{ID: keyID, Reason: model.ApiKeyRevokeSkipNotFound},
			},
		}})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.ApiKeyRevokeResult{
		Revoked: true,
		Skipped: []model.SkippedMemory{},
	}})

	// Emit user-axis SSE so other tabs/devices drop this key from
	// their api-keys list without a reload. If the revoked key had an
	// agent assignment, also fire agent.changed so connected-agents
	// key_count converges — otherwise the assigned agent's card would
	// show a stale count until next focus/reconnect.
	events.PublishKeyChanged(r.Context(), h.eventsPublisher, userID, keyID, "revoked")
	if prevAgent != "" {
		events.PublishAgentChanged(r.Context(), h.eventsPublisher, userID, prevAgent, "upserted")
	}
}

// UpdateAPIKey patches mutable API-key metadata (agent_name, standalone).
//
// PATCH /v1/auth/api-keys/{id}
//
// Auth gate: user-session principals only. API-key and OAuth-grant
// principals cannot modify key metadata (they would be modifying
// themselves or other agents, which is a privilege-escalation risk).
//
// Owner-scoped UPDATE — a key id from another user's account falls
// through the WHERE clause and returns 404.
//
// Side effects: when agent_name is assigned to a non-empty slug, the
// connected_agents registry is upserted synchronously so the agent
// card appears immediately in the UI. Clearing agent_name does not
// remove the row from connected_agents (history preserved).
func (h *AuthHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	// The /v1/auth/api-keys/* mount in serverapp/routes.go only runs
	// RequireAuth (not the full withAuth chain), so AuthorizeHTTP —
	// which is what populates AuthContext — is NOT in this path.
	// Pull the authenticated user + principal from the grant context
	// that RequireAuth does populate, matching what CreateAPIKey /
	// RevokeAPIKey already do. Using GetAuthContext here previously
	// returned nil and 401'd every request, which is what a user
	// reported in prod ("it shows Assigning and then logged me out"
	// — the 401 triggered the refresh+clear cascade).
	userID := GetUserID(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
			Error: &model.Error{Code: "unauthorized", Message: "Authentication required."},
		})
		return
	}
	grant := grantFromRequest(r)
	// Only user-session JWTs may modify key metadata. API-key and
	// OAuth-grant principals are explicitly rejected — the former
	// would let a key alter its own attribution, the latter would let
	// an agent change another key's settings. "local" is the
	// memory-mode principal used when JWT_SECRET is unset (dev
	// without auth); treat it like "user" for this endpoint so
	// local-dev still works.
	if grant.PrincipalType != "user" && grant.PrincipalType != "local" {
		writeJSON(w, http.StatusForbidden, model.ApiResponse{
			Error: &model.Error{Code: "forbidden", Message: "Only user sessions can modify API key metadata."},
		})
		return
	}
	if !grant.DefaultPermissions.Has(PermSettingsWrite) {
		writeJSON(w, http.StatusForbidden, model.ApiResponse{
			Error: &model.Error{Code: "insufficient_permissions", Message: "settings:write required."},
		})
		return
	}

	keyID := strings.TrimSpace(r.PathValue("id"))
	if keyID == "" {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Missing key id."},
		})
		return
	}

	var req struct {
		AgentName  *string `json:"agent_name,omitempty"`
		Standalone *bool   `json:"standalone,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Malformed JSON body."},
		})
		return
	}
	if req.AgentName == nil && req.Standalone == nil {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "No fields to update."},
		})
		return
	}

	// Invariant: a key cannot simultaneously be attributed to an agent
	// AND marked standalone — "standalone" means "no agent, by design."
	// Reject contradictory requests explicitly rather than silently
	// letting one field win. Assigning a non-empty agent is treated as
	// "this key now belongs to an agent," so we force standalone=false
	// to unblock the caller from having to send two fields.
	normalizedAgent := ""
	if req.AgentName != nil {
		normalizedAgent = model.NormalizeAgentSlug(*req.AgentName)
	}
	if normalizedAgent != "" && req.Standalone != nil && *req.Standalone {
		writeJSON(w, http.StatusBadRequest, model.ApiResponse{
			Error: &model.Error{Code: "invalid_request", Message: "Cannot set standalone=true while assigning an agent."},
		})
		return
	}
	autoClearStandalone := false
	if normalizedAgent != "" && req.Standalone == nil {
		// Assigning an agent implicitly clears any prior standalone
		// flag so the key doesn't end up in a contradictory state.
		autoClearStandalone = true
	}
	autoClearAgent := false
	if req.Standalone != nil && *req.Standalone && req.AgentName == nil {
		// Symmetric to autoClearStandalone: setting standalone=true
		// means "no agent, by design," so we force agent_name='' on
		// the row. Without this, a separate PATCH {standalone:true}
		// from a linked state leaves both fields populated and the
		// client classifier (first-branch-wins: linked) keeps
		// showing the agent badge, appearing to ignore the click.
		autoClearAgent = true
	}

	// Build a partial UPDATE — only the fields the caller passed, plus
	// the implicit fields needed to keep the two-field invariant.
	setClauses := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if req.AgentName != nil {
		setClauses = append(setClauses, fmt.Sprintf("agent_name = $%d", len(args)+1))
		args = append(args, normalizedAgent)
	} else if autoClearAgent {
		setClauses = append(setClauses, fmt.Sprintf("agent_name = $%d", len(args)+1))
		args = append(args, "")
	}
	if req.Standalone != nil {
		setClauses = append(setClauses, fmt.Sprintf("standalone = $%d", len(args)+1))
		args = append(args, *req.Standalone)
	} else if autoClearStandalone {
		setClauses = append(setClauses, fmt.Sprintf("standalone = $%d", len(args)+1))
		args = append(args, false)
	}
	args = append(args, keyID, userID)
	// CTE returns BOTH previous and new attribution in one atomic
	// query so the audit track() call below can log the full
	// before/after diff — Postgres doesn't support OLD.* in plain
	// UPDATE RETURNING, so we read the row via `prev` and join it to
	// the UPDATE's `upd` output. Both subqueries use identical
	// owner-scoped WHERE clauses; the UPDATE still respects the
	// revoked_at guard. If the row isn't there, the join yields zero
	// rows and we hit the ErrNoRows branch.
	query := fmt.Sprintf(
		`WITH prev AS (
			SELECT COALESCE(agent_name, '') AS agent_name,
			       COALESCE(standalone, false) AS standalone
			FROM api_keys
			WHERE id = $%d::uuid AND user_id = $%d::uuid AND revoked_at IS NULL
		), upd AS (
			UPDATE api_keys SET %s
			WHERE id = $%d::uuid AND user_id = $%d::uuid AND revoked_at IS NULL
			RETURNING id, COALESCE(agent_name, '') AS agent_name,
			          COALESCE(standalone, false) AS standalone
		)
		SELECT upd.id,
		       prev.agent_name, upd.agent_name,
		       prev.standalone, upd.standalone
		FROM upd, prev`,
		len(args)-1, len(args),
		strings.Join(setClauses, ", "),
		len(args)-1, len(args),
	)

	var id, prevAgentName, agentName string
	var prevStandalone, standalone bool
	err := h.pool.QueryRow(context.Background(), query, args...).Scan(
		&id,
		&prevAgentName, &agentName,
		&prevStandalone, &standalone,
	)
	if err != nil {
		// Distinguish "no match" (404) from real DB failures (500).
		// Previously both fell through to 404, which hid operational
		// failures and gave clients the wrong contract.
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, model.ApiResponse{
				Error: &model.Error{Code: "not_found", Message: "API key not found."},
			})
			return
		}
		slog.Error("update api key failed", "key_id", keyID, "user", userID, "err", err)
		writeJSON(w, http.StatusInternalServerError, model.ApiResponse{
			Error: &model.Error{Code: "internal", Message: "Failed to update API key."},
		})
		return
	}

	// Upsert connected_agents when assigning a non-empty slug — agent
	// surfaces driven from the registry (agent cards, dream attribution
	// fallback) otherwise stay stale.
	if req.AgentName != nil && normalizedAgent != "" {
		EnsureConnectedAgent(h.store, userID, normalizedAgent)
	}

	changed := make([]string, 0, 2)
	if req.AgentName != nil {
		changed = append(changed, "agent_name")
	}
	if req.Standalone != nil {
		changed = append(changed, "standalone")
	}
	// Include before/after attribution so support can reconstruct
	// what actually changed if the user ever disputes an assignment.
	// The `changed` array still reflects which fields the CLIENT sent
	// explicitly; `prev_*`/`new_*` capture the full row-level diff
	// including implicit clears (auto-clear standalone on assign, and
	// auto-clear agent_name on standalone=true).
	track(userID, "api.auth.update_key", map[string]any{
		"key_id":          keyID,
		"changed":         changed,
		"prev_agent_name": prevAgentName,
		"new_agent_name":  agentName,
		"prev_standalone": prevStandalone,
		"new_standalone":  standalone,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{
			"id":         id,
			"agent_name": agentName,
			"standalone": standalone,
		},
	})

	// Emit user-axis SSE so other tabs/devices see the row change
	// without a reload. Agent attribution changes must fire
	// agent.changed for BOTH the prior and new assignments when they
	// differ — `connected-agents` key_count is derived from api_keys
	// on the server, so losing the assignment (clear, reassign, or
	// standalone=true auto-clear) requires the previous agent's card
	// to refresh on other devices.
	events.PublishKeyChanged(r.Context(), h.eventsPublisher, userID, keyID, "updated")
	if prevAgentName != agentName {
		if prevAgentName != "" {
			events.PublishAgentChanged(r.Context(), h.eventsPublisher, userID, prevAgentName, "upserted")
		}
		if agentName != "" {
			events.PublishAgentChanged(r.Context(), h.eventsPublisher, userID, agentName, "upserted")
		}
	}
}

// ResolveAPIKey looks up an API key by its plaintext value, returning the user ID and optional hub scope.
func (h *AuthHandler) ResolveAPIKey(key string) APIKeyResult {
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	var userID string
	var expiresAt *time.Time
	var hubID *string
	var agentName *string
	var id string
	var hubScopeMode string
	var hubIDs []string
	var defaultPermissions []string
	var trustLevel string
	var rateLimitTier *string
	err := h.pool.QueryRow(context.Background(),
		`SELECT id, user_id, expires_at, hub_id, agent_name,
			COALESCE(hub_scope_mode, 'all_accessible'),
			ARRAY(SELECT hub_id::text FROM unnest(COALESCE(hub_ids, ARRAY[]::uuid[])) AS hub_id),
			COALESCE(default_permissions, ARRAY[]::text[]),
			COALESCE(trust_level, 'elevated'),
			rate_limit_tier
		FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`, keyHash,
	).Scan(&id, &userID, &expiresAt, &hubID, &agentName, &hubScopeMode, &hubIDs, &defaultPermissions, &trustLevel, &rateLimitTier)
	if err != nil {
		return APIKeyResult{}
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		return APIKeyResult{}
	}

	perms, invalid := PermissionSetFromStrings(defaultPermissions)
	if len(invalid) > 0 {
		slog.Warn("api key contains unknown permissions", "key_id", id, "permissions", invalid)
	}
	if len(perms) == 0 {
		return APIKeyResult{}
	}
	result := APIKeyResult{
		UserID:             userID,
		GrantID:            id,
		HubScopeMode:       hubScopeMode,
		ScopedHubIDs:       hubIDs,
		DefaultPermissions: perms,
		TrustLevel:         trustLevel,
	}
	if hubID != nil {
		result.HubID = *hubID
		if len(result.ScopedHubIDs) == 0 {
			result.ScopedHubIDs = []string{*hubID}
		}
		if result.HubScopeMode == "" {
			result.HubScopeMode = HubScopeAllowlist
		}
	}
	if agentName != nil {
		result.AgentName = *agentName
	}
	if rateLimitTier != nil {
		result.RateLimitTier = *rateLimitTier
	}

	// Update last_used asynchronously
	go func() {
		h.pool.Exec(context.Background(),
			`UPDATE api_keys SET last_used = $1 WHERE key_hash = $2`, time.Now(), keyHash)
	}()

	return result
}

func (h *AuthHandler) ResolveOAuthGrant(userID string, grantID string) APIKeyResult {
	if userID == "" || grantID == "" {
		return APIKeyResult{}
	}

	var resolvedUserID string
	var agentName string
	var hubScopeMode string
	var hubIDs []string
	var defaultPermissions []string
	var trustLevel string
	var rateLimitTier *string
	var expiresAt *time.Time
	err := h.pool.QueryRow(context.Background(),
		`SELECT user_id, COALESCE(agent_name, ''),
			COALESCE(hub_scope_mode, 'hub_allowlist'),
			ARRAY(SELECT hub_id::text FROM unnest(COALESCE(hub_ids, ARRAY[]::uuid[])) AS hub_id),
			COALESCE(default_permissions, ARRAY[]::text[]),
			COALESCE(trust_level, 'standard'),
			rate_limit_tier,
			expires_at
		FROM oauth_grants
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`,
		grantID, userID,
	).Scan(&resolvedUserID, &agentName, &hubScopeMode, &hubIDs, &defaultPermissions, &trustLevel, &rateLimitTier, &expiresAt)
	if err != nil {
		return APIKeyResult{}
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return APIKeyResult{}
	}

	perms, invalid := PermissionSetFromStrings(defaultPermissions)
	if len(invalid) > 0 {
		slog.Warn("oauth grant contains unknown permissions", "grant_id", grantID, "permissions", invalid)
	}
	if len(perms) == 0 {
		return APIKeyResult{}
	}

	result := APIKeyResult{
		UserID:             resolvedUserID,
		GrantID:            grantID,
		HubScopeMode:       hubScopeMode,
		ScopedHubIDs:       hubIDs,
		DefaultPermissions: perms,
		TrustLevel:         trustLevel,
		AgentName:          agentName,
	}
	if len(hubIDs) == 1 {
		result.HubID = hubIDs[0]
	}
	if rateLimitTier != nil {
		result.RateLimitTier = *rateLimitTier
	}

	go func() {
		_, _ = h.pool.Exec(context.Background(),
			`UPDATE oauth_grants SET last_used = $1 WHERE id = $2::uuid`, time.Now(), grantID)
	}()

	return result
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func knownTrustLevel(level string) bool {
	switch level {
	case TrustPublic, TrustStandard, TrustElevated, TrustAdmin:
		return true
	default:
		return false
	}
}

// UserIDFromRequest extracts the user ID from the Authorization header.
// Returns empty string if not authenticated.
func UserIDFromRequest(r *http.Request, jwtSecret []byte) string {
	claims := ClaimsFromRequest(r, jwtSecret)
	if claims == nil {
		return ""
	}
	return claims.Sub
}

// ClaimsFromRequest extracts full JWT claims (including agent_name for MCP OAuth tokens).
func ClaimsFromRequest(r *http.Request, jwtSecret []byte) *auth.Claims {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil
	}
	token := strings.TrimPrefix(header, "Bearer ")
	claims, err := auth.VerifyAccessToken(token, jwtSecret)
	if err != nil {
		return nil
	}
	return claims
}

// --- internal helpers ---

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type githubUser struct {
	ID            int64  `json:"id"`
	Login         string `json:"login"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	AvatarURL     string `json:"avatar_url"`
	EmailVerified bool   `json:"-"` // set by getGitHubUser after /user/emails check
}

func (h *AuthHandler) exchangeGitHubCode(code string) (string, error) {
	body := fmt.Sprintf(`{"client_id":"%s","client_secret":"%s","code":"%s"}`,
		h.clientID, h.clientSecret, code)

	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp githubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	if tokenResp.AccessToken == "" {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("no access token in response: %s", respBody)
	}
	return tokenResp.AccessToken, nil
}

// checkOrgMembership checks if a GitHub user is a member of the given org.
// Uses GET /user/orgs which lists orgs the authenticated user belongs to.
func (h *AuthHandler) checkOrgMembership(ghToken string, requiredOrg string) (bool, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("github orgs API returned %d", resp.StatusCode)
	}

	var orgs []struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return false, err
	}

	for _, org := range orgs {
		if strings.EqualFold(org.Login, requiredOrg) {
			return true, nil
		}
	}
	return false, nil
}

func (h *AuthHandler) getGitHubUser(token string) (*githubUser, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	// Always source the verified email from /user/emails, not the profile.
	// The profile email field is user-editable and not necessarily verified.
	// The /user/emails endpoint returns the verified flag from GitHub.
	verifiedEmail, verified := h.getGitHubVerifiedEmail(token)
	if verified && verifiedEmail != "" {
		user.Email = verifiedEmail
		user.EmailVerified = true
	}
	// If /user/emails fails or returns no verified email, Email stays as-is
	// but EmailVerified remains false — loginOrCreateUser will reject it.

	return &user, nil
}

// getGitHubVerifiedEmail fetches the user's primary verified email via the
// /user/emails endpoint. Returns the email and whether it was verified.
// This works even when the profile email is private, as long as the OAuth
// scope includes user:email.
func (h *AuthHandler) getGitHubVerifiedEmail(token string) (string, bool) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("failed to fetch GitHub emails", "error", err)
		return "", false
	}
	defer resp.Body.Close()

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		slog.Warn("failed to parse GitHub emails", "error", err)
		return "", false
	}

	// Prefer primary+verified, fall back to any verified email
	var fallbackEmail string
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true
		}
		if e.Verified && fallbackEmail == "" {
			fallbackEmail = e.Email
		}
	}
	if fallbackEmail != "" {
		return fallbackEmail, true
	}
	return "", false
}

func (h *AuthHandler) upsertUser(gh *githubUser) (*model.User, error) {
	ctx := context.Background()
	now := time.Now()

	var user model.User
	err := h.pool.QueryRow(ctx,
		`INSERT INTO users (github_id, email, name, display_name, avatar_url, created_at, updated_at)
		VALUES ($1, $2, $3, $3, $4, $5, $5)
		ON CONFLICT (github_id) DO UPDATE SET
			email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url,
			display_name = CASE WHEN users.display_name = '' OR users.display_name IS NULL THEN EXCLUDED.name ELSE users.display_name END,
			updated_at = $5
		RETURNING id, github_id, email, name, COALESCE(display_name, name, ''), avatar_url, COALESCE(plan, 'free'), personal_plan_id, created_at, updated_at`,
		gh.ID, gh.Email, gh.Name, gh.AvatarURL, now,
	).Scan(&user.ID, &user.GitHubID, &user.Email, &user.Name,
		&user.DisplayName, &user.AvatarURL, &user.Plan, &user.PersonalPlanID, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Ensure personal hub exists (idempotent — creates on first login)
	if h.store != nil {
		h.ensurePersonalHub(&user)
	}

	return &user, nil
}

// ensurePersonalHub creates a personal hub for the user if one doesn't exist.
func (h *AuthHandler) ensurePersonalHub(user *model.User) {
	if _, err := h.store.GetPersonalHub(user.ID); err == nil {
		return // already has one
	}

	now := time.Now()
	hub := &model.Hub{
		ID:        fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", now.UnixNano()&0xFFFFFFFF, now.UnixNano()>>32&0xFFFF, 0x4000|(now.UnixNano()>>48&0x0FFF), 0x8000|(now.UnixNano()>>60&0x3FFF), now.UnixNano()&0xFFFFFFFFFFFF),
		Name:      model.DefaultPersonalHubName,
		Icon:      "",
		Accent:    model.DefaultHubAccent("personal"),
		Slug:      user.ID,
		HubType:   "personal",
		OwnerID:   user.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.store.CreateHub(hub); err != nil {
		slog.Warn("failed to create personal hub", "error", err, "user_id", user.ID)
		return
	}
	if err := h.store.AddHubMember(hub.ID, user.ID, "owner"); err != nil {
		slog.Warn("failed to add hub owner membership", "error", err)
	}
	slog.Info("personal hub created", "user_id", user.ID, "hub_id", hub.ID)
}

func (h *AuthHandler) issueTokens(userID string) (*model.TokenPair, error) {
	return h.issueAgentTokens(userID, "")
}

// issueAgentTokens issues a token pair with optional agent identity embedded in the JWT.
func (h *AuthHandler) issueAgentTokens(userID, agentName string) (*model.TokenPair, error) {
	return h.issueAgentGrantTokens(userID, agentName, "", 30*24*time.Hour)
}

func (h *AuthHandler) issueAgentGrantTokens(userID, agentName, grantID string, refreshTTL time.Duration) (*model.TokenPair, error) {
	var accessToken string
	var err error
	if grantID != "" {
		accessToken, err = auth.SignGrantAccessToken(userID, agentName, grantID, h.jwtSecret, time.Hour)
	} else if agentName != "" {
		accessToken, err = auth.SignAgentAccessToken(userID, agentName, h.jwtSecret, time.Hour)
	} else {
		accessToken, err = auth.SignAccessToken(userID, h.jwtSecret, time.Hour)
	}
	if err != nil {
		return nil, err
	}

	refreshToken := generateToken()
	expiresAt := time.Now().Add(refreshTTL)

	_, err = h.pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, refresh_token, expires_at, agent_name, grant_id)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid)`,
		userID, refreshToken, expiresAt, agentName, grantID)
	if err != nil {
		return nil, err
	}

	return &model.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
	}, nil
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateState(clientRedirect string) string {
	// Encode the client redirect in the state. In production, add CSRF protection.
	if clientRedirect == "" {
		return "none"
	}
	return base64Encode(clientRedirect)
}

func parseState(state string) string {
	if state == "none" || state == "" {
		return ""
	}
	decoded, err := base64Decode(state)
	if err != nil {
		return ""
	}
	return decoded
}

func base64Encode(s string) string {
	return hex.EncodeToString([]byte(s))
}

func base64Decode(s string) (string, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
