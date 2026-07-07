package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// hubBillingService is the billing service interface for hub seat tracking.
type hubBillingService interface {
	UpdateHubSeatCount(ctx context.Context, hubID string) error
	TransferHubBilling(ctx context.Context, hubID, newOwnerID string) error
}

// hubOwnershipResolver checks whether a user can create/own free team hubs.
type hubOwnershipResolver interface {
	ResolveOwnershipEntitlements(ctx context.Context, userID string) (model.OwnershipEntitlements, error)
}

type HubsHandler struct {
	store              store.Store
	events             events.Publisher
	invalidateUserPlan func(ctx context.Context, userID string) error          // nil = no-op
	billing            hubBillingService                                       // nil = no seat tracking
	ownership          hubOwnershipResolver                                    // nil = legacy can_create_hub fallback
	enqueueEmail       func(template, to string, vars map[string]string) error // nil = no email
	appBaseURL         string                                                  // for invite links in emails
}

func NewHubsHandler(s store.Store) *HubsHandler {
	return &HubsHandler{store: s}
}

// SetInvalidateUserPlan wires the plan resolver's cache invalidation.
func (h *HubsHandler) SetInvalidateUserPlan(fn func(ctx context.Context, userID string) error) {
	h.invalidateUserPlan = fn
}

// SetBilling wires the billing service for seat count tracking.
func (h *HubsHandler) SetBilling(b hubBillingService) {
	h.billing = b
}

// SetOwnershipResolver wires the plan resolver for ownership cap checks.
func (h *HubsHandler) SetOwnershipResolver(r hubOwnershipResolver) {
	h.ownership = r
}

// SetEnqueueEmail wires the email enqueue callback. Called after queue client is ready.
func (h *HubsHandler) SetEnqueueEmail(fn func(template, to string, vars map[string]string) error) {
	h.enqueueEmail = fn
}

// SetAppBaseURL sets the base URL for building invite links in emails.
func (h *HubsHandler) SetAppBaseURL(url string) {
	h.appBaseURL = url
}

// maxActiveInvitesPerHub is the quota for outstanding invites per hub.
// Both link and email invites share this pool. Counted at creation time.
// Product interpretation: concurrent active invites, not total issuable.
const maxActiveInvitesPerHub = 20

// updateSeatCountIfNeeded syncs the hub's subscription seat count.
func (h *HubsHandler) updateSeatCountIfNeeded(ctx context.Context, hubID string) {
	if h.billing != nil {
		if err := h.billing.UpdateHubSeatCount(ctx, hubID); err != nil {
			slog.Warn("seat count update failed after membership change",
				"error", err, "hub_id", hubID)
		}
	}
}

// invalidateUserPlanIfNeeded calls the invalidation callback if set.
// Returns error if invalidation fails — callers log it via slog.Warn.
// Membership mutations still succeed (the DB change is committed).
// Staleness is operator-visible via logs and the weekly reconciliation
// job, not surfaced in the HTTP response.
func (h *HubsHandler) invalidateUserPlanIfNeeded(ctx context.Context, userID string) error {
	if h.invalidateUserPlan == nil {
		return nil
	}
	return h.invalidateUserPlan(ctx, userID)
}

// WithEvents installs an events.Publisher on the handler so
// hub_invite notifications can fan out via SSE. Optional — the
// handler remains fully functional without one (invite row still
// gets written, clients just don't see a real-time push and rely
// on the next reconnect / poll). Test callers don't need to wire
// this; the Publish helpers no-op on nil.
func (h *HubsHandler) WithEvents(p events.Publisher) *HubsHandler {
	h.events = p
	return h
}

// slugRegex enforces the character-set rule: start + end alphanumeric,
// internal hyphens allowed. The LENGTH bound inside the regex is
// [4, 50] (chars 0 and last are one alnum each, the middle is
// `[a-z0-9-]{2,48}`). Raising the floor from 3 to 4 rules out
// ambiguous short handles ("ai", "io", "eng") that are too generic
// to carry real meaning for a multi-member hub, and mirrors the
// length rules most public services converge on (GitHub min 1,
// Notion min 2, Linear min 4, Slack min 3 but recommends 4+).
var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,48}[a-z0-9]$`)

const minHubSlugLength = 4

// reservedHubSlugs are slugs the hub creation flow forbids. Some
// are ambiguous-meaning words that would confuse team members
// ("team", "shared", "personal"); some would collide with first-
// party URL paths or subdomains we reserve for product use
// ("admin", "api", "docs", "memax"); and some are brand-sensitive
// words we don't want handed to customers. The backend masks
// reserved-name rejections as "taken" in the CheckSlug response so
// the frontend can't enumerate the full list by probing.
var reservedHubSlugs = map[string]struct{}{
	// Ambiguous / misleading handles for a multi-member hub
	"personal": {}, "shared": {}, "team": {}, "teams": {},
	"public": {}, "private": {}, "internal": {}, "external": {},
	"all": {}, "everyone": {}, "anyone": {}, "new": {},
	"me": {}, "my": {}, "user": {}, "users": {},
	// First-party product paths / subdomain collisions
	"admin": {}, "api": {}, "auth": {}, "dashboard": {},
	"dev": {}, "docs": {}, "help": {}, "home": {},
	"hub": {}, "hubs": {}, "inbox": {}, "login": {},
	"logout": {}, "memories": {}, "memory": {}, "recall": {},
	"register": {}, "settings": {}, "signup": {}, "support": {},
	"topic": {}, "topics": {}, "waitlist": {},
	// Brand / legal
	"memax": {}, "memaxlabs": {},
	// Technical handles operators don't want customers grabbing
	"root": {}, "test": {}, "demo": {}, "staging": {}, "prod": {},
	"production": {}, "null": {}, "undefined": {}, "true": {}, "false": {},
}

// isReservedHubSlug reports whether the (trimmed, lowercased) slug
// is in the reservedHubSlugs set.
func isReservedHubSlug(slug string) bool {
	key := strings.ToLower(strings.TrimSpace(slug))
	_, reserved := reservedHubSlugs[key]
	return reserved
}

func (h *HubsHandler) requireHubAdminRole(hubID, userID string) (string, bool) {
	role, _ := h.store.GetHubMemberRole(hubID, userID)
	return role, isHubAdmin(role)
}

func normalizeInviteRole(raw string) (string, bool) {
	role := strings.TrimSpace(raw)
	if role == "" {
		role = "contributor"
	}
	switch role {
	case "member":
		role = "contributor"
	case "reader":
		role = "viewer"
	}
	switch role {
	case "admin", "contributor", "viewer":
		return role, true
	default:
		return "", false
	}
}

func writeHubTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrHubTransferNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Ownership transfer not found")
	case errors.Is(err, store.ErrHubTransferExpired):
		writeError(w, http.StatusGone, "expired", "This ownership transfer has expired")
	case errors.Is(err, store.ErrHubTransferAccepted):
		writeError(w, http.StatusConflict, "accepted", "This ownership transfer has already been accepted")
	case errors.Is(err, store.ErrHubTransferCanceled):
		writeError(w, http.StatusGone, "canceled", "This ownership transfer has been canceled")
	case errors.Is(err, store.ErrHubTransferExists):
		writeError(w, http.StatusConflict, "transfer_exists", "A pending ownership transfer already exists")
	case errors.Is(err, store.ErrHubTransferInvalid):
		writeError(w, http.StatusConflict, "transfer_invalid", "This ownership transfer is no longer valid")
	default:
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}

func (h *HubsHandler) newHubInvite(hubID, userID, role string) (*model.HubInvite, error) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	return &model.HubInvite{
		ID:        generateHubID(),
		HubID:     hubID,
		Token:     fmt.Sprintf("%x", tokenBytes),
		InvitedBy: userID,
		Role:      role,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
	}, nil
}

func inviteURLForToken(token string) string {
	base := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	parsed.Path = "/invite/" + token
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// Create creates a new team hub.
// POST /v1/hubs
func (h *HubsHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	// Resolve ownership entitlements — checks personal plan's free team hub cap.
	// Phase 6: ownership resolver is the sole authority (can_create_hub fallback removed).
	if h.ownership == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable",
			"Plan resolver not available. Hub creation is temporarily disabled.")
		return
	}
	ent, err := h.ownership.ResolveOwnershipEntitlements(r.Context(), ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to check permissions.")
		return
	}
	if !ent.CanCreateFreeTeamHub {
		WriteErrorWithDetails(w, http.StatusForbidden, "ownership_cap_exceeded",
			"You have reached the maximum number of free team hubs for your plan.",
			map[string]any{
				"current_count": ent.CurrentOwnedCount,
				"limit":         ent.MaxOwnedFreeTeamHubs,
				"plan":          ent.PersonalPlanID,
			})
		return
	}
	maxFreeTeamHubs := ent.MaxOwnedFreeTeamHubs

	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "Hub name is required")
		return
	}

	// Generate slug from name if not provided
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}
	if !slugRegex.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "invalid_slug", "Slug must be 4–50 lowercase alphanumeric characters with hyphens")
		return
	}

	// Uniqueness + reserved check share the same 409 slug_taken
	// response — both go through GetHubBySlug first so a probing
	// caller cannot distinguish reserved vs real-collision via
	// response-time deltas (mirrors CheckSlug). An op error on the
	// lookup is surfaced; anything else either finds the slug
	// (taken) or doesn't (reserved wins).
	_, slugErr := h.store.GetHubBySlug(req.Slug)
	if slugErr != nil && !errors.Is(slugErr, store.ErrHubNotFound) {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not check slug availability")
		return
	}
	if slugErr == nil || isReservedHubSlug(req.Slug) {
		writeError(w, http.StatusConflict, "slug_taken", "A hub with this slug already exists")
		return
	}

	now := time.Now()
	hub := &model.Hub{
		ID:                      generateHubID(),
		Name:                    req.Name,
		Icon:                    "",
		Accent:                  model.DefaultHubAccent("team"),
		Slug:                    req.Slug,
		HubType:                 "team",
		Plan:                    model.HubFreeTeamPlanID, // new team hubs start as hub_free_team
		OwnerID:                 ownerID,
		AllowContributorTopics:  true,
		AllowContributorDreams:  true,
		ContributorDeletePolicy: "own",
		CreatedAt:               now,
		UpdatedAt:               now,
	}

	// Atomic team hub creation with ownership cap enforcement:
	// advisory lock → count check → hub + member + subscription.
	// If any step fails, nothing is created.
	if err := h.store.CreateTeamHub(r.Context(), hub, ownerID, maxFreeTeamHubs); err != nil {
		if errors.Is(err, store.ErrOwnershipCapExceeded) {
			writeError(w, http.StatusForbidden, "ownership_cap_exceeded",
				"You have reached the maximum number of free team hubs for your plan.")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// User-axis emit so the creator's other tabs / devices see the new
	// hub in the list without refresh. Only the creator is affected at
	// this moment — no one else can see a hub they don't belong to.
	events.PublishHubListChanged(r.Context(), h.events, ownerID, hub.ID, "created")

	trackRequest(r, "api.hubs.create", map[string]any{"hub_id": hub.ID, "hub_type": "team"})
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: hub})

	// Plan 18 onboarding tick used to fire here via the recorder.
	// Now the materializer computes first_hub_invite completion
	// lazily from ListUserHubs filtered to hub_type=team on
	// notification read; the hub.list.changed event above invalidates
	// ["notifications"] so the next read picks up the new hub
	// without any explicit tick here.
}

// CheckSlug validates a slug and checks availability.
// GET /v1/hubs/check-slug?slug=acme-engineering
// Requires authentication to prevent handle enumeration.
func (h *HubsHandler) CheckSlug(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if slug == "" {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"available": false,
			"reason":    "invalid",
		}})
		return
	}

	if !slugRegex.MatchString(slug) {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"available": false,
			"reason":    "invalid",
		}})
		return
	}

	// Always hit the DB even for reserved names, so a caller can't
	// distinguish reserved vs real-collision by response-time deltas.
	// The reserved-list check is applied AFTER the lookup and simply
	// overrides the outcome for reserved-but-free slugs. Both paths
	// now do the same work and return the same shape.
	_, err := h.store.GetHubBySlug(slug)
	if err != nil && !errors.Is(err, store.ErrHubNotFound) {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not check slug availability")
		return
	}
	taken := err == nil
	reserved := isReservedHubSlug(slug)

	if taken || reserved {
		// Reserved slugs are reported as "taken" — same reason a real
		// collision returns — so the reserved list cannot be
		// enumerated by probing common words.
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"available": false,
			"reason":    "taken",
		}})
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"available": true,
	}})
}

// List returns all hubs the user is a member of.
// GET /v1/hubs
func (h *HubsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)

	hubs, err := h.store.ListUserHubs(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if hubs == nil {
		hubs = []model.HubWithRole{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: hubs})
}

// Get returns hub details and members.
// GET /v1/hubs/{id}
func (h *HubsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	// Check membership
	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}

	members, _ := h.store.ListHubMembers(hubID)
	if members == nil {
		members = []model.HubMember{}
	}
	pendingTransfer, err := h.store.GetActiveHubOwnershipTransfer(hubID)
	if err != nil {
		pendingTransfer = nil
	}

	// is_frozen is a derived public flag: the caller only needs to
	// know "is this hub gated on billing right now?" to disable
	// actions like manual dream trigger. The full subscription row
	// is intentionally admin-only (status, over-limit timestamps,
	// provider ids) so we compute the boolean here instead of
	// shipping the row.
	//
	// GetHubSubscriptionAnyStatus returns the subscription even
	// when status != 'active' (cancelled, past_due) — exactly the
	// rows IsHubFrozen is designed to catch. The active-only query
	// would mask them as nil and look "never frozen" on a
	// cancelled hub.
	sub, _ := h.store.GetHubSubscriptionAnyStatus(r.Context(), hubID)
	isFrozen := model.IsHubFrozen(sub, time.Now())

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{
			"hub":              hub,
			"members":          members,
			"role":             role,
			"pending_transfer": pendingTransfer,
			"is_frozen":        isFrozen,
		},
	})
}

// GetSummary returns the canonical page-level summary model for a hub.
// GET /v1/hubs/{id}/summary
func (h *HubsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return
	}

	summary, err := h.buildHubSummary(r, userID, hubID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: summary})
}

// RecordVisit persists the current user's visit to a hub.
// POST /v1/hubs/{id}/visit
func (h *HubsHandler) RecordVisit(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return
	}

	if err := h.store.UpsertHubVisit(userID, hubID, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "ok", "hub_id": hubID},
	})
}

func (h *HubsHandler) buildHubSummary(r *http.Request, userID, hubID, role string) (*model.HubSummary, error) {
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		return nil, err
	}

	now := resolveHubSummaryTime(r, userID, h)
	scope := store.VisibilityScope{OwnerID: userID, HubIDs: GetAccessibleHubIDs(r)}

	topics, err := h.store.ListTopics(hubID)
	if err != nil {
		return nil, err
	}
	perTopic, unassigned, err := h.store.CountTopicMemories(scope, hubID)
	if err != nil {
		return nil, err
	}

	totalMemories := unassigned
	for _, count := range perTopic {
		totalMemories += count
	}

	// Pending-review count now reads from the notifications summary
	// instead of the retired reviews table. Hub-scoped narrow: only
	// hub-audience rows in this hub that belong to the decision
	// (Needs-action) bucket count.
	pendingReview := 0
	if notifSummary, err := h.store.GetNotificationSummary(r.Context(), store.NotificationSummaryOpts{
		UserID: userID,
		HubIDs: GetAccessibleHubIDs(r),
		HubID:  hubID,
	}); err == nil && notifSummary != nil {
		pendingReview = notifSummary.NeedsActionPending
	}

	dreamTopics := 0
	// Include partial_failed too — phases that DID commit are still
	// useful history. Excluding it would drop topic-restructure data
	// from the hub summary whenever an unrelated phase (e.g. organize)
	// hit an LLM timeout.
	if run, err := h.store.GetLatestDreamRunByHub(hubID); err == nil && run != nil &&
		(run.Status == model.DreamRunStatusCompleted || run.Status == model.DreamRunStatusPartialFailed) {
		dreamTopics = run.TopicsRestructured
	}

	teamActivity := 0
	if hub.HubType == "team" {
		teamActivity, err = h.store.CountRecentMemoriesByHub(scope, hubID, now.Add(-24*time.Hour))
		if err != nil {
			return nil, err
		}
	}

	membersPreview := []model.HubSummaryMember{}
	memberCount := 0
	if hub.HubType == "team" {
		memberCount, err = h.store.CountHubMembers(hubID)
		if err != nil {
			return nil, err
		}
		members, err := h.store.ListHubMembers(hubID)
		if err != nil {
			return nil, err
		}
		limit := min(len(members), hubMembersPreviewLimit)
		for _, member := range members[:limit] {
			membersPreview = append(membersPreview, model.HubSummaryMember{
				UserID:        member.UserID,
				UserName:      member.UserName,
				UserAvatarURL: member.UserAvatarURL,
			})
		}
	}

	var lastVisit *model.HubVisit
	if visit, err := h.store.GetHubVisit(userID, hubID); err == nil {
		lastVisit = visit
	}

	// No hardcoded fallback here: greeting_params.name stays empty when
	// the account has no name, and the web client substitutes a
	// locale-appropriate fallback. The old server-side "there" leaked
	// English into non-English greeting templates.
	userName := ""
	if user, err := h.store.GetUser(userID); err == nil && user != nil {
		if display := strings.TrimSpace(user.DisplayName); display != "" {
			userName = display
		} else if name := strings.TrimSpace(user.Name); name != "" {
			userName = name
		}
	}
	if idx := strings.Index(userName, " "); idx > 0 {
		userName = userName[:idx]
	}

	stats := model.HubSummaryStats{
		Memories:      totalMemories,
		Topics:        len(topics),
		Inbox:         unassigned,
		PendingReview: pendingReview,
		DreamTopics:   dreamTopics,
	}
	if hub.HubType == "team" {
		stats.Members = memberCount
		stats.TeamActivity = teamActivity
	}

	state := resolveHubHeaderState(stats, lastVisit, now)
	seed := fmt.Sprintf("%s:%s:%s", hubID, userID, now.Format("2006-01-02"))
	header := model.HubSummaryHeader{
		State:          state,
		GreetingKey:    resolveHubGreetingKey(hub.HubType, state, getHubTimeBucket(now), seed),
		GreetingParams: hubGreetingParams(userName, stats),
		TimeBucket:     getHubTimeBucket(now),
	}

	return &model.HubSummary{
		Hub:            *hub,
		Role:           role,
		Stats:          stats,
		MembersPreview: membersPreview,
		Header:         header,
	}, nil
}

// Update updates hub name/slug. Requires owner or admin.
// PATCH /v1/hubs/{id}
func (h *HubsHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "Only hub owners and admins can update the hub")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}

	var req struct {
		Name                    string          `json:"name"`
		Icon                    *string         `json:"icon"`
		Accent                  *string         `json:"accent"`
		Slug                    string          `json:"slug"`
		AllowContributorTopics  *bool           `json:"allow_contributor_topics"`
		AllowContributorDreams  *bool           `json:"allow_contributor_dreams"`
		ContributorDeletePolicy *string         `json:"contributor_delete_policy"`
		HeaderAuroraMode        *string         `json:"header_aurora_mode"`
		Settings                *map[string]any `json:"settings"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	// Slug is immutable after creation.
	if req.Slug != "" {
		writeError(w, http.StatusBadRequest, "slug_immutable", "Hub handle cannot be changed after creation")
		return
	}

	if req.Name != "" {
		hub.Name = req.Name
	}
	if req.Icon != nil {
		trimmed := strings.TrimSpace(*req.Icon)
		if utf8.RuneCountInString(trimmed) > 8 {
			writeError(w, http.StatusBadRequest, "invalid_icon", "Hub icon must be 8 characters or fewer")
			return
		}
		hub.Icon = trimmed
	}
	if req.Accent != nil {
		trimmed := strings.TrimSpace(*req.Accent)
		if trimmed == "" {
			hub.Accent = model.DefaultHubAccent(hub.HubType)
		} else {
			if !model.IsValidHubAccent(trimmed) {
				writeError(w, http.StatusBadRequest, "invalid_accent", "Hub accent must be one of: violet, blue, green, amber, rose, slate")
				return
			}
			hub.Accent = trimmed
		}
	}
	if req.AllowContributorTopics != nil {
		hub.AllowContributorTopics = *req.AllowContributorTopics
	}
	if req.AllowContributorDreams != nil {
		hub.AllowContributorDreams = *req.AllowContributorDreams
	}
	if req.ContributorDeletePolicy != nil {
		switch *req.ContributorDeletePolicy {
		case "none", "own", "any":
			hub.ContributorDeletePolicy = *req.ContributorDeletePolicy
		default:
			writeError(w, http.StatusBadRequest, "invalid_delete_policy", "contributor_delete_policy must be: none, own, or any")
			return
		}
	}
	if req.HeaderAuroraMode != nil {
		// Personal hubs take their header aurora mode from the user's
		// settings (PATCH /v1/settings), not from per-hub state —
		// refuse the ambiguous update rather than silently ignore it.
		if hub.HubType == "personal" {
			writeError(w, http.StatusBadRequest, "personal_hub_header_mode", "Personal hub header aurora mode is managed through user settings")
			return
		}
		mode := strings.TrimSpace(*req.HeaderAuroraMode)
		if !model.IsValidHubHeaderAuroraMode(mode) {
			writeError(w, http.StatusBadRequest, "invalid_header_aurora_mode", "header_aurora_mode must be one of: none, signature, time")
			return
		}
		hub.HeaderAuroraMode = mode
	}

	// Settings patch is validated up-front so an invalid key or
	// value can't partially apply (non-settings fields already
	// applied by the time we reach here). We delegate the merge to
	// store.PatchHubSettings, which composes the patch into
	// hubs.settings atomically in SQL — a read-merge-write round
	// trip here would let two concurrent toggles drop each other's
	// changes.
	//
	// Allow-list stays narrow (model.HubSettingsAllowedKeys) so new
	// user prefs don't implicitly become hub-level knobs.
	if req.Settings != nil {
		for k, v := range *req.Settings {
			if !model.IsHubSettingsKey(k) {
				writeError(w, http.StatusBadRequest, "unknown_setting", "Unknown hub setting: "+k)
				return
			}
			if v != nil {
				if _, ok := v.(bool); !ok {
					writeError(w, http.StatusBadRequest, "invalid_setting_value", "Hub setting "+k+" must be boolean or null")
					return
				}
			}
		}
	}

	// UpdateHub writes the full non-settings column set, so calling
	// it for a settings-only PATCH would snapshot name/icon/accent
	// etc. from the pre-PATCH GetHub and race a concurrent
	// non-settings PATCH on the same hub. Gate it on at least one
	// non-settings field being present — settings has its own
	// atomic path below.
	hasNonSettingsField := req.Name != "" ||
		req.Icon != nil ||
		req.Accent != nil ||
		req.AllowContributorTopics != nil ||
		req.AllowContributorDreams != nil ||
		req.ContributorDeletePolicy != nil ||
		req.HeaderAuroraMode != nil
	if hasNonSettingsField {
		if err := h.store.UpdateHub(hub); err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	}

	if req.Settings != nil {
		if err := h.store.PatchHubSettings(r.Context(), hub.ID, *req.Settings); err != nil {
			if errors.Is(err, store.ErrHubNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Hub not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		// Re-read so the response reflects the merged settings.
		// Failing to refetch is itself a store error — the caller
		// relies on the response being the authoritative
		// post-patch state, not a pre-patch snapshot. Swallowing
		// the error and returning the stale `hub` would make the
		// response contract quietly lie.
		refreshed, err := h.store.GetHub(hub.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", "hub patch succeeded but refetch failed: "+err.Error())
			return
		}
		hub = refreshed
	}
	// Fan out hub.list.changed to every current member so their hub
	// list / hub header / picker converges to the new metadata
	// (name, icon, accent, settings). Read-only fan-out — if
	// ListHubMembers fails we log-and-continue: the per-role
	// hub.members.changed route will still invalidate hub-detail,
	// and a refresh picks up any gap. Also emit hub.members.changed
	// (hub-axis) so already-subscribed member-management UI re-reads.
	if members, err := h.store.ListHubMembers(hub.ID); err == nil {
		for _, m := range members {
			events.PublishHubListChanged(r.Context(), h.events, m.UserID, hub.ID, "metadata_updated")
		}
	} else {
		slog.Warn("hub.update: list members failed; skipping hub.list.changed fan-out",
			"hub_id", hub.ID, "error", err)
	}
	events.PublishHubMembersChanged(r.Context(), h.events, hub.ID, userID)

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: hub})
}

// Delete removes a team hub and all hub-scoped data. Requires owner.
// DELETE /v1/hubs/{id}
func (h *HubsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "Only the hub owner can delete the hub")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Personal hubs cannot be deleted here")
		return
	}

	// List members BEFORE deletion — the CASCADE will remove hub_members rows.
	// We need the member IDs to invalidate their effective plan caches after.
	// Fail the delete if we can't list members — otherwise cached boosts persist.
	members, err := h.store.ListHubMembers(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to list hub members before deletion")
		return
	}

	if err := h.store.DeleteHub(hubID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Invalidate all former members' effective plan caches.
	// The hub subscription is gone (CASCADE), so their personal boost
	// may need to downgrade. Same loop also fans out hub.list.changed
	// to each snapshotted recipient so their hub list / picker drops
	// the deleted hub immediately — the SSE subscription's frozen
	// HubRoles map still contains the old role for a moment, but the
	// user-axis event bypasses that lookup entirely. Publish is
	// post-cascade-success: DeleteHub returned nil above, so we won't
	// announce a delete that didn't commit.
	for _, member := range members {
		if err := h.invalidateUserPlanIfNeeded(r.Context(), member.UserID); err != nil {
			slog.Warn("plan cache invalidation failed after hub deletion",
				"error", err, "user_id", member.UserID, "hub_id", hubID)
		}
		events.PublishHubListChanged(r.Context(), h.events, member.UserID, hubID, "deleted")
	}

	trackRequest(r, "api.hubs.delete", map[string]any{"hub_id": hubID})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"deleted": true, "hub_id": hubID}})
}

// AddMember invites a user to the hub. Requires owner or admin.
// POST /v1/hubs/{id}/members
func (h *HubsHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "Only hub owners and admins can add members")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Cannot add members to a personal hub")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "missing_user_id", "user_id is required")
		return
	}

	role, ok := normalizeInviteRole(req.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_role", "Role must be: admin, contributor, or viewer")
		return
	}
	req.Role = role

	// Enforce member cap (advisory lock + count check in one transaction)
	memberCap, capErr := h.store.GetHubMemberCap(r.Context(), hubID)
	if capErr != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to check hub member cap.")
		return
	}
	if err := h.store.AddHubMemberWithCapCheck(r.Context(), hubID, req.UserID, req.Role, memberCap); err != nil {
		if errors.Is(err, store.ErrMemberCapExceeded) {
			WriteErrorWithDetails(w, http.StatusForbidden, "member_cap_exceeded",
				"This hub has reached its member limit. Upgrade the hub plan for more seats.",
				map[string]any{"hub_id": hubID, "limit": memberCap})
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := h.invalidateUserPlanIfNeeded(r.Context(), req.UserID); err != nil {
		slog.Warn("plan cache invalidation failed after membership change", "error", err, "user_id", req.UserID)
	}
	h.updateSeatCountIfNeeded(r.Context(), hubID)
	events.PublishHubMembersChanged(r.Context(), h.events, hubID, userID)
	// User-axis emit for the added user — their SSE subscription's
	// frozen HubRoles won't route a hub-axis event to them for this
	// brand-new membership, so hub.list.changed is the only way their
	// hub list / picker sees the addition live.
	events.PublishHubListChanged(r.Context(), h.events, req.UserID, hubID, "membership_joined")

	trackRequest(r, "api.hubs.add_member", map[string]any{"hub_id": hubID, "member_id": req.UserID, "role": req.Role})
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "added", "user_id": req.UserID, "role": req.Role},
	})
}

// UpdateMemberRole changes a member's role. Requires owner or admin.
// PATCH /v1/hubs/{id}/members/{user_id}
func (h *HubsHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	targetUserID := r.PathValue("user_id")

	requesterRole, _ := h.store.GetHubMemberRole(hubID, userID)
	if requesterRole != "owner" && requesterRole != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "Only hub owners and admins can change member roles")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Cannot edit members in a personal hub")
		return
	}
	if hub.OwnerID == targetUserID {
		writeError(w, http.StatusBadRequest, "cannot_change_owner", "Use ownership transfer to change the owner")
		return
	}
	if targetRole, _ := h.store.GetHubMemberRole(hubID, targetUserID); targetRole == "" {
		writeError(w, http.StatusNotFound, "not_found", "Member not found")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	role, ok := normalizeInviteRole(req.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_role", "Role must be: admin, contributor, or viewer")
		return
	}

	if err := h.store.UpdateHubMemberRole(hubID, targetUserID, role); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	events.PublishHubMembersChanged(r.Context(), h.events, hubID, userID)

	trackRequest(r, "api.hubs.update_member_role", map[string]any{"hub_id": hubID, "member_id": targetUserID, "role": role})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "updated", "user_id": targetUserID, "role": role}})
}

// RemoveMember removes a user from the hub. Requires owner or admin.
// DELETE /v1/hubs/{id}/members/{user_id}
func (h *HubsHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	targetUserID := r.PathValue("user_id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "Only hub owners and admins can remove members")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.OwnerID == targetUserID {
		writeError(w, http.StatusBadRequest, "cannot_remove_owner", "Cannot remove the hub owner")
		return
	}

	if err := h.store.RemoveHubMember(hubID, targetUserID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := h.invalidateUserPlanIfNeeded(r.Context(), targetUserID); err != nil {
		slog.Warn("plan cache invalidation failed after membership change", "error", err, "user_id", targetUserID)
	}
	h.updateSeatCountIfNeeded(r.Context(), hubID)
	if transfer, err := h.store.GetActiveHubOwnershipTransfer(hubID); err == nil && transfer.TargetUserID == targetUserID {
		_ = h.store.CancelHubOwnershipTransfer(hubID, transfer.ID)
	}
	events.PublishHubMembersChanged(r.Context(), h.events, hubID, userID)
	// User-axis emit for the removed user — their frozen HubRoles
	// still carries the hub until reconnect, but the user-axis path
	// bypasses the role lookup. Without this they'd see the hub in
	// their list until they refresh even though the event arrived.
	events.PublishHubListChanged(r.Context(), h.events, targetUserID, hubID, "membership_left")

	trackRequest(r, "api.hubs.remove_member", map[string]any{"hub_id": hubID, "member_id": targetUserID})
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "removed", "user_id": targetUserID},
	})
}

// Leave removes the current user from a team hub.
// POST /v1/hubs/{id}/leave
func (h *HubsHandler) Leave(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return
	}
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Personal hubs cannot be left")
		return
	}
	if role == "owner" {
		count, err := h.store.CountHubMembers(hubID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		if count <= 1 {
			writeError(w, http.StatusBadRequest, "last_owner_delete_required", "Delete the hub instead of leaving when you are the last member")
			return
		}
		writeError(w, http.StatusBadRequest, "ownership_transfer_required", "Transfer ownership before leaving this hub")
		return
	}

	if err := h.store.RemoveHubMember(hubID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := h.invalidateUserPlanIfNeeded(r.Context(), userID); err != nil {
		slog.Warn("plan cache invalidation failed after membership change", "error", err, "user_id", userID)
	}
	h.updateSeatCountIfNeeded(r.Context(), hubID)
	if transfer, err := h.store.GetActiveHubOwnershipTransfer(hubID); err == nil && transfer.TargetUserID == userID {
		_ = h.store.CancelHubOwnershipTransfer(hubID, transfer.ID)
	}
	events.PublishHubMembersChanged(r.Context(), h.events, hubID, userID)
	// User-axis emit — the leaving user's own tabs need to drop the
	// hub from their list. Symmetric with membership_joined in
	// AcceptInvite / AddMember.
	events.PublishHubListChanged(r.Context(), h.events, userID, hubID, "membership_left")

	trackRequest(r, "api.hubs.leave", map[string]any{"hub_id": hubID})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "left", "hub_id": hubID}})
}

// CreateOwnershipTransfer starts a pending ownership transfer. Requires owner.
// POST /v1/hubs/{id}/ownership-transfer
func (h *HubsHandler) CreateOwnershipTransfer(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "Only the hub owner can transfer ownership")
		return
	}
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Personal hubs do not support ownership transfer")
		return
	}

	var req struct {
		TargetUserID string `json:"target_user_id"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if req.TargetUserID == "" {
		writeError(w, http.StatusBadRequest, "missing_target_user_id", "target_user_id is required")
		return
	}
	if req.TargetUserID == hub.OwnerID {
		writeError(w, http.StatusBadRequest, "invalid_target", "Choose a different member")
		return
	}
	if targetRole, _ := h.store.GetHubMemberRole(hubID, req.TargetUserID); targetRole == "" {
		writeError(w, http.StatusBadRequest, "invalid_target", "Ownership can only be transferred to an existing member")
		return
	}

	transfer := &model.HubOwnershipTransfer{
		ID:           generateHubID(),
		HubID:        hubID,
		InitiatedBy:  userID,
		TargetUserID: req.TargetUserID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}
	if err := h.store.CreateHubOwnershipTransfer(transfer); err != nil {
		writeHubTransferError(w, err)
		return
	}
	if user, err := h.store.GetUser(req.TargetUserID); err == nil && user != nil {
		transfer.TargetUserName = user.Name
		transfer.TargetUserEmail = user.Email
	}

	// Fan out a decision notification to the target's inbox so the
	// pending transfer is not trapped behind the settings UI. Target
	// can still accept / decline from settings — both paths converge
	// on the same store primitives via the shared side-effects helper.
	emitHubOwnershipTransferNotification(r.Context(), h.store, h.events, transfer)

	trackRequest(r, "api.hubs.transfer.create", map[string]any{"hub_id": hubID, "target_user_id": req.TargetUserID})
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: transfer})
}

// AcceptOwnershipTransfer completes a pending ownership transfer
// from the settings UI path. Converges with the notification
// framework: any pending hub_ownership_transfer notification for
// this transfer id is flipped to resolved=accepted and a
// hub_ownership_transferred receipt is fanned out to the old owner.
// The notification-native accept path
// (POST /v1/notifications/{id}/resolve) produces the same side
// effects via the shared helper.
// POST /v1/hubs/{id}/ownership-transfer/{transfer_id}/accept
func (h *HubsHandler) AcceptOwnershipTransfer(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	transferID := r.PathValue("transfer_id")

	if role, _ := h.store.GetHubMemberRole(hubID, userID); role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return
	}
	// Snapshot the old owner BEFORE the accept path rewrites owner_id
	// so the old-owner receipt fans out to the right user. If GetHub
	// fails the accept will also fail downstream; surfacing the error
	// early keeps the failure mode tidy.
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	oldOwnerID := hub.OwnerID

	// Resolve ownership cap for the target user. The cap is enforced
	// atomically inside AcceptHubOwnershipTransfer with advisory lock.
	maxFreeTeamHubs := -1 // -1 = skip cap check (for paid hubs or when resolver unavailable)
	if h.ownership != nil {
		ent, entErr := h.ownership.ResolveOwnershipEntitlements(r.Context(), userID)
		if entErr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Failed to check ownership cap.")
			return
		}
		maxFreeTeamHubs = ent.MaxOwnedFreeTeamHubs
	}

	transfer, err := h.store.AcceptHubOwnershipTransfer(hubID, transferID, userID, maxFreeTeamHubs)
	if err != nil {
		if errors.Is(err, store.ErrOwnershipCapExceeded) {
			writeError(w, http.StatusForbidden, "ownership_cap_exceeded",
				"You have reached the maximum number of free team hubs for your plan.")
			return
		}
		writeHubTransferError(w, err)
		return
	}

	// resolveExistingNotification=true: settings path is out-of-band
	// from the notification /resolve flow, so it's responsible for
	// converging any pending inbox row here. No-op if the target
	// never had a notification (shouldn't happen post-producer-wiring
	// but the helper is safe).
	// Transfer billing contact to the new owner
	if h.billing != nil {
		if err := h.billing.TransferHubBilling(r.Context(), hubID, userID); err != nil {
			slog.Warn("hub billing transfer failed after ownership transfer",
				"error", err, "hub_id", hubID, "new_owner", userID)
		}
	}

	onHubOwnershipTransferAccepted(r.Context(), h.store, h.events, transfer, oldOwnerID, userID, true)
	events.PublishHubMembersChanged(r.Context(), h.events, hubID, userID)
	// User-axis fan-out for both parties — role state flipped for
	// each, so both hub lists need to re-read to show the new role
	// badge / ownership indicator. metadata_updated (not
	// membership_joined/left) because neither user joined or left the
	// hub; their role within it changed.
	events.PublishHubListChanged(r.Context(), h.events, oldOwnerID, hubID, "metadata_updated")
	events.PublishHubListChanged(r.Context(), h.events, userID, hubID, "metadata_updated")

	trackRequest(r, "api.hubs.transfer.accept", map[string]any{"hub_id": hubID, "transfer_id": transferID})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: transfer})
}

// CancelOwnershipTransfer retires a pending ownership transfer.
// Either the initiating owner or the addressed target can hit this
// endpoint, but the two act on different user intents and resolve
// the target's inbox notification differently:
//
//   - Target hits it (settings shows this as "Decline") → the
//     pending notification is resolved with
//     resolution=declined, matching the notification-native
//     inbox decline path so the persisted outcome does not
//     diverge by entry point.
//   - Initiator hits it ("Cancel") → the pending notification is
//     resolved with resolution=dismissed, reflecting the
//     system-side cancel.
//
// Store sentinels are mapped through writeHubTransferError so an
// already-accepted or already-cancelled transfer returns a proper
// 409 / 410 instead of a misleading 200 "canceled".
// POST /v1/hubs/{id}/ownership-transfer/{transfer_id}/cancel
func (h *HubsHandler) CancelOwnershipTransfer(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	transferID := r.PathValue("transfer_id")

	transfer, err := h.store.GetHubOwnershipTransfer(transferID)
	if err != nil || transfer.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Ownership transfer not found")
		return
	}
	if transfer.InitiatedBy != userID && transfer.TargetUserID != userID {
		writeError(w, http.StatusForbidden, "forbidden", "You cannot cancel this ownership transfer")
		return
	}
	if err := h.store.CancelHubOwnershipTransfer(hubID, transferID); err != nil {
		writeHubTransferError(w, err)
		return
	}

	// Converge the target's inbox row. Declined when the target
	// themself cancels (matching the inbox decline path), dismissed
	// when the initiator retires the transfer unilaterally.
	// resolveExistingNotification=true: no other code path writes the
	// notification row in the cancel flow, so we own the resolve.
	resolution := model.ResolutionDismissed
	if userID == transfer.TargetUserID {
		resolution = model.ResolutionDeclined
	}
	onHubOwnershipTransferCancelled(r.Context(), h.store, h.events, transferID, resolution, true)

	trackRequest(r, "api.hubs.transfer.cancel", map[string]any{
		"hub_id":      hubID,
		"transfer_id": transferID,
		"by_target":   userID == transfer.TargetUserID,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "canceled", "transfer_id": transferID}})
}

// CreateInvite generates an invite token for a hub. Accepts an
// optional addressed recipient so the notification framework can
// deliver the invite to an existing user's inbox directly; when no
// recipient is given, the invite falls back to the legacy
// copy-link flow.
//
// POST /v1/hubs/{id}/invites
//
// Request body:
//
//	{
//	  "role": "contributor",
//	  // Optional — when present and the email/id matches an
//	  // existing user, the created invite carries invitee_user_id
//	  // and fires a hub_invite notification to that user's inbox.
//	  "invitee": { "user_id": "..." | "email": "foo@bar.com" }
//	}
//
// Non-resolving invitee inputs (unknown email, missing account)
// still create a link-only invite so the inviter can forward the
// URL manually.
func (h *HubsHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	if _, ok := h.requireHubAdminRole(hubID, userID); !ok {
		writeError(w, http.StatusForbidden, "insufficient_role", "Only owners and admins can create invites")
		return
	}

	// Personal hubs are single-user — invites make no sense
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}
	if hub.HubType == "personal" {
		writeError(w, http.StatusBadRequest, "personal_hub", "Cannot create invites for a personal hub")
		return
	}

	var req struct {
		Role    string `json:"role"`
		Invitee *struct {
			UserID string `json:"user_id,omitempty"`
			Email  string `json:"email,omitempty"`
		} `json:"invitee,omitempty"`
	}
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
			return
		}
	}
	role, ok := normalizeInviteRole(req.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_role", "Role must be: admin, contributor, or viewer")
		return
	}

	invite, err := h.newHubInvite(hubID, userID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error", "Failed to generate invite token")
		return
	}

	// Quota check: enforce active invite cap before INSERT.
	active, err := h.store.CountActiveHubInvites(hubID)
	if err != nil {
		slog.Error("failed to count active invites", "error", err, "hub_id", hubID)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to check invite quota.")
		return
	}
	if active >= maxActiveInvitesPerHub {
		writeError(w, http.StatusTooManyRequests, "invite_limit",
			"This hub has reached its invite limit. Revoke unused invites or wait for them to expire.")
		return
	}

	// Resolve addressed recipient. When the client passes an invitee
	// block, unknown emails are valid (email-only invite for non-Memax
	// users). The legacy link-only path (no invitee) is unchanged.
	if req.Invitee != nil {
		// Defense-in-depth email format check. Client validates too,
		// but we can't trust that — a malformed invitee_email stored
		// on the invite row would later fail in the email queue, long
		// after the inviter has dismissed the UI. Reject at the front
		// door so the inviter can correct the typo while still in the
		// popover.
		//
		// net/mail.ParseAddress accepts both bare addresses and the
		// mailbox form ("Name <addr@host>"), but the client and the
		// rest of the pipeline treat invitee_email as a bare address
		// — storing a display-name form would send mail to a parsed
		// address that doesn't round-trip through the UI. Reject
		// non-bare input so client and server validate the same
		// shape.
		if trimmed := strings.TrimSpace(req.Invitee.Email); trimmed != "" {
			addr, err := mail.ParseAddress(trimmed)
			if err != nil || addr.Address != trimmed {
				writeError(w, http.StatusBadRequest, "invalid_email", "That doesn't look like a valid email.")
				return
			}
			// Carry the parsed address forward so the resolver and
			// store see the canonical form, not whatever whitespace
			// variant the client typed.
			req.Invitee.Email = addr.Address
		}
		res := h.resolveInviteRecipient(hubID, req.Invitee.UserID, req.Invitee.Email)
		if res.Status == "invitee_already_member" {
			writeError(w, http.StatusConflict, "invitee_already_member", "That user is already a member of this hub.")
			return
		}
		if res.UserID != "" {
			id := res.UserID
			invite.InviteeUserID = &id
		}
		if res.Email != "" {
			email := res.Email
			invite.InviteeEmail = &email
		}
	}

	if err := h.store.CreateHubInvite(invite); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// In-app notification for addressed invites (existing Memax users).
	h.emitHubInviteNotificationIfAddressed(r.Context(), invite, userID)

	// Email delivery for invites with an email address.
	if invite.InviteeEmail != nil {
		inviterName := ""
		if u, uErr := h.store.GetUser(userID); uErr == nil && u != nil {
			inviterName = u.DisplayName
			if inviterName == "" {
				inviterName = u.Name
			}
		}
		h.sendHubInviteEmail(invite, inviterName, hub.Name)
	}

	invite.InviteURL = inviteURLForToken(invite.Token)
	slog.Info("invite created",
		"hub_id", hubID,
		"invite_id", invite.ID,
		"token", invite.Token[:8]+"...",
		"by", userID,
		"role", invite.Role,
		"addressed", invite.InviteeUserID != nil,
		"email", invite.InviteeEmail != nil)
	trackRequest(r, "api.hubs.invite.create", map[string]any{
		"hub_id":    hubID,
		"invite_id": invite.ID,
		"role":      invite.Role,
		"addressed": invite.InviteeUserID != nil,
		"email":     invite.InviteeEmail != nil,
	})

	writeJSON(w, http.StatusCreated, model.ApiResponse{
		Data: invite,
	})
}

// recipientResolution holds the result of resolving an invite recipient.
type recipientResolution struct {
	UserID string // non-empty if existing Memax user found
	Email  string // always set when invitee.email was provided
	Status string // "" = ok, "invitee_already_member" = hard error
}

// resolveInviteRecipient maps a { user_id?, email? } input to an
// existing user id if possible. Unknown emails are valid — they
// produce a resolution with Email set but UserID empty, enabling
// email-only invites for non-Memax users.
//
// Returns:
//   - {UserID, Email, ""}                  — matched an existing user
//   - {"", Email, ""}                      — email provided, no Memax account (email-only invite)
//   - {"", "", "invitee_already_member"}   — user is already a hub member
//   - {"", "", ""}                         — no input at all (link-only path)
func (h *HubsHandler) resolveInviteRecipient(hubID, inputUserID, inputEmail string) recipientResolution {
	trimmedEmail := strings.TrimSpace(inputEmail)

	var candidate *model.User
	if id := strings.TrimSpace(inputUserID); id != "" {
		if u, err := h.store.GetUser(id); err == nil && u != nil {
			candidate = u
		}
	}
	if candidate == nil && trimmedEmail != "" {
		if u, err := h.store.GetUserByCanonicalEmail(trimmedEmail); err == nil && u != nil {
			candidate = u
		}
	}

	if candidate == nil {
		// No existing user. Return the email if provided (email-only invite).
		return recipientResolution{Email: trimmedEmail}
	}

	if existingRole, err := h.store.GetHubMemberRole(hubID, candidate.ID); err == nil && existingRole != "" {
		return recipientResolution{Status: "invitee_already_member"}
	}

	email := trimmedEmail
	if email == "" {
		email = candidate.Email
	}
	return recipientResolution{UserID: candidate.ID, Email: email}
}

// ListInvites returns outstanding invites for a hub.
// GET /v1/hubs/{id}/invites
func (h *HubsHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")

	if _, ok := h.requireHubAdminRole(hubID, userID); !ok {
		writeError(w, http.StatusForbidden, "insufficient_role", "Only owners and admins can view invites")
		return
	}

	invites, err := h.store.ListOutstandingHubInvites(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if invites == nil {
		invites = []model.HubInvite{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: invites})
}

// sendHubInviteEmail enqueues a hub invite email for the invitee.
// email_enqueued_at is only set when the queue insert succeeds — if
// enqueue fails, the invite row stays without a timestamp so the UI
// does not claim "queued" for an email that never entered River.
func (h *HubsHandler) sendHubInviteEmail(invite *model.HubInvite, inviterName, hubName string) {
	if h.enqueueEmail == nil || h.appBaseURL == "" || invite.InviteeEmail == nil {
		if h.appBaseURL == "" && invite.InviteeEmail != nil {
			slog.Warn("skipping hub invite email — APP_BASE_URL not set", "invite_id", invite.ID)
		}
		return
	}
	if err := h.enqueueEmail("hub_invite", *invite.InviteeEmail, map[string]string{
		"InviterName": inviterName,
		"HubName":     hubName,
		"Role":        invite.Role,
		"InviteURL":   inviteURLForToken(invite.Token),
		"ExpiresAt":   invite.ExpiresAt.Format("January 2, 2006"),
	}); err != nil {
		slog.Warn("failed to enqueue hub invite email",
			"error", err, "invite_id", invite.ID, "email", *invite.InviteeEmail)
		return
	}
	now := time.Now()
	invite.EmailEnqueuedAt = &now
	if err := h.store.UpdateHubInviteEmailEnqueuedAt(invite.ID, now); err != nil {
		slog.Warn("failed to update email_enqueued_at", "error", err, "invite_id", invite.ID)
	}
}

// ResendInvite re-sends the email for an existing invite.
// Same token, same invite row — no quota hit.
// POST /v1/hubs/{id}/invites/{invite_id}/resend
func (h *HubsHandler) ResendInvite(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	inviteID := r.PathValue("invite_id")

	if _, ok := h.requireHubAdminRole(hubID, userID); !ok {
		writeError(w, http.StatusForbidden, "insufficient_role", "Only owners and admins can resend invites")
		return
	}

	invite, err := h.store.GetHubInvite(inviteID)
	if err != nil || invite.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Invite not found")
		return
	}
	if invite.AcceptedBy != nil {
		writeError(w, http.StatusConflict, "invite_used", "Cannot resend an invite that has already been accepted")
		return
	}
	if invite.RevokedAt != nil {
		writeError(w, http.StatusConflict, "invite_revoked", "Cannot resend a revoked invite")
		return
	}
	if invite.ExpiresAt.Before(time.Now()) {
		writeError(w, http.StatusGone, "expired", "Cannot resend an expired invite")
		return
	}
	if invite.InviteeEmail == nil {
		writeError(w, http.StatusBadRequest, "no_email", "This invite has no email address. Only email invites can be resent.")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load hub details")
		return
	}

	inviterName := ""
	if u, uErr := h.store.GetUser(userID); uErr == nil && u != nil {
		inviterName = u.DisplayName
		if inviterName == "" {
			inviterName = u.Name
		}
	}

	h.sendHubInviteEmail(invite, inviterName, hub.Name)

	slog.Info("invite resent", "hub_id", hubID, "invite_id", inviteID, "to", *invite.InviteeEmail, "by", userID)
	trackRequest(r, "api.hubs.invite.resend", map[string]any{
		"hub_id":    hubID,
		"invite_id": inviteID,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"status":            "resent",
		"email_enqueued_at": invite.EmailEnqueuedAt,
	}})
}

// RevokeInvite revokes an outstanding invite for a hub.
// DELETE /v1/hubs/{id}/invites/{invite_id}
func (h *HubsHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	inviteID := r.PathValue("invite_id")

	if _, ok := h.requireHubAdminRole(hubID, userID); !ok {
		writeError(w, http.StatusForbidden, "insufficient_role", "Only owners and admins can revoke invites")
		return
	}

	invite, err := h.store.GetHubInvite(inviteID)
	if err != nil || invite.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Invite not found")
		return
	}
	if invite.AcceptedBy != nil {
		writeError(w, http.StatusConflict, "invite_used", "Cannot revoke an invite that has already been used")
		return
	}
	if invite.RevokedAt != nil {
		writeError(w, http.StatusConflict, "invite_revoked", "This invite has already been revoked")
		return
	}

	if err := h.store.RevokeHubInvite(hubID, inviteID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Converge the invitee's inbox: any pending hub_invite
	// notification keyed on this invite id must be resolved so the
	// invitee does not see a phantom accept/decline row for a dead
	// invite. No-op for link-only invites (no notification was ever
	// written).
	onHubInviteRevoked(r.Context(), h.store, h.events, inviteID)

	slog.Info("invite revoked", "hub_id", hubID, "invite_id", inviteID, "by", userID)
	trackRequest(r, "api.hubs.invite.revoke", map[string]any{"hub_id": hubID, "invite_id": inviteID})

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "revoked", "invite_id": inviteID},
	})
}

// RegenerateInvite revokes an existing invite and replaces it with a fresh one.
// POST /v1/hubs/{id}/invites/{invite_id}/regenerate
func (h *HubsHandler) RegenerateInvite(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := r.PathValue("id")
	inviteID := r.PathValue("invite_id")

	if _, ok := h.requireHubAdminRole(hubID, userID); !ok {
		writeError(w, http.StatusForbidden, "insufficient_role", "Only owners and admins can regenerate invites")
		return
	}

	invite, err := h.store.GetHubInvite(inviteID)
	if err != nil || invite.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Invite not found")
		return
	}
	if invite.AcceptedBy != nil {
		writeError(w, http.StatusConflict, "invite_used", "Cannot regenerate an invite that has already been used")
		return
	}
	if invite.RevokedAt != nil {
		writeError(w, http.StatusConflict, "invite_revoked", "Cannot regenerate an invite that has already been revoked")
		return
	}

	if err := h.store.RevokeHubInvite(hubID, inviteID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Retire the old notification row before minting the replacement.
	// Without this the invitee would end up with two pending
	// hub_invite notifications — the first pointing at a now-revoked
	// invite id, which would error out with invite_revoked / not_found
	// the moment they clicked accept. The new notification fires
	// below via emitHubInviteNotificationIfAddressed and is keyed on
	// the replacement's id, so there is no collision against
	// notifications_source_unique.
	onHubInviteRevoked(r.Context(), h.store, h.events, inviteID)

	replacement, err := h.newHubInvite(hubID, userID, invite.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error", "Failed to generate invite token")
		return
	}
	// Preserve the addressed recipient so regenerate does not
	// silently downgrade a notification-native invite to a
	// link-only one. Without this copy, any second "generate new
	// token" click on an addressed invite would strip invitee_user_id
	// and the invitee would never see the replacement in their
	// inbox.
	if invite.InviteeUserID != nil && *invite.InviteeUserID != "" {
		addressed := *invite.InviteeUserID
		replacement.InviteeUserID = &addressed
	}
	// Preserve the email so the replacement can be re-sent.
	if invite.InviteeEmail != nil && *invite.InviteeEmail != "" {
		email := *invite.InviteeEmail
		replacement.InviteeEmail = &email
	}
	if err := h.store.CreateHubInvite(replacement); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	h.emitHubInviteNotificationIfAddressed(r.Context(), replacement, userID)

	// Re-send email for email invites with the new token URL.
	if replacement.InviteeEmail != nil {
		hub, _ := h.store.GetHub(hubID)
		hubName := hubID
		if hub != nil {
			hubName = hub.Name
		}
		inviterName := ""
		if u, uErr := h.store.GetUser(userID); uErr == nil && u != nil {
			inviterName = u.DisplayName
			if inviterName == "" {
				inviterName = u.Name
			}
		}
		h.sendHubInviteEmail(replacement, inviterName, hubName)
	}

	slog.Info("invite regenerated", "hub_id", hubID, "old_invite_id", inviteID, "new_invite_id", replacement.ID, "by", userID, "addressed", replacement.InviteeUserID != nil, "email", replacement.InviteeEmail != nil)
	trackRequest(r, "api.hubs.invite.regenerate", map[string]any{"hub_id": hubID, "old_invite_id": inviteID, "new_invite_id": replacement.ID})

	replacement.InviteURL = inviteURLForToken(replacement.Token)
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: replacement})
}

// GetInvite returns invite details by token. No auth required — anyone with the link can see it.
// GET /v1/invites/{token}
func (h *HubsHandler) GetInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	invite, err := h.store.GetHubInviteByToken(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Invite not found or expired")
		return
	}

	// Check expiry
	if time.Now().After(invite.ExpiresAt) {
		writeError(w, http.StatusGone, "expired", "This invite has expired")
		return
	}

	// Check if already accepted
	if invite.AcceptedBy != nil {
		writeError(w, http.StatusGone, "used", "This invite has already been used")
		return
	}

	// Get hub details for display
	hub, _ := h.store.GetHub(invite.HubID)
	members, _ := h.store.ListHubMembers(invite.HubID)
	inviter, _ := h.store.GetUser(invite.InvitedBy)

	inviterName := ""
	if inviter != nil {
		inviterName = inviter.Name
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{
			"invite":       invite,
			"hub":          hub,
			"member_count": len(members),
			"invited_by":   inviterName,
		},
	})
}

// AcceptInvite accepts an invite via its token and adds the user
// to the hub. Converges with the notification framework: a
// successful accept resolves any pending hub_invite notification
// that was addressed to this user (so the invitee's inbox stops
// showing it across every open tab) and emits a hub_member_joined
// receipt to the hub owner. Legacy token-only invites (no
// addressee) still work and still produce the hub_member_joined
// receipt — the resolve step is a no-op when no notification row
// exists for the invite id.
//
// Addressed invites should also go through the notification-native
// accept path (POST /v1/notifications/{id}/resolve with
// action=accept) when the invitee has the row in their inbox; this
// handler remains the entry point for users who followed a copy
// link from outside the app.
//
// POST /v1/invites/{token}/accept
func (h *HubsHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	token := r.PathValue("token")

	// Resolve member cap for the hub. The cap is enforced atomically inside
	// AcceptHubInvite with an advisory lock — no TOCTOU race.
	memberCap := -1 // unlimited default
	if inv, err := h.store.GetHubInviteByToken(token); err == nil && inv != nil {
		cap, capErr := h.store.GetHubMemberCap(r.Context(), inv.HubID)
		if capErr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Failed to check hub member cap.")
			return
		}
		memberCap = cap
	}

	invite, err := h.store.AcceptHubInvite(token, userID, memberCap)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrHubInviteNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Invite not found")
		case errors.Is(err, store.ErrHubInviteExpired):
			writeError(w, http.StatusGone, "expired", "This invite has expired")
		case errors.Is(err, store.ErrHubInviteUsed):
			writeError(w, http.StatusConflict, "used", "This invite has already been used")
		case errors.Is(err, store.ErrHubInviteRevoked):
			writeError(w, http.StatusGone, "revoked", "This invite has been revoked")
		case errors.Is(err, store.ErrHubInviteWrongInvitee):
			writeError(w, http.StatusForbidden, "wrong_invitee", "This invite is addressed to a different user")
		case errors.Is(err, store.ErrHubAlreadyMember):
			writeError(w, http.StatusConflict, "already_member", "You are already a member of this hub")
		case errors.Is(err, store.ErrMemberCapExceeded):
			WriteErrorWithDetails(w, http.StatusForbidden, "member_cap_exceeded",
				"This hub has reached its member limit.",
				map[string]any{"limit": memberCap})
		default:
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}

	hub, err := h.store.GetHub(invite.HubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load hub")
		return
	}

	// resolveExistingNotification=true: the token path does not go
	// through the notification /resolve handler, so it's responsible
	// for converging any matching notification row here. The helper
	// no-ops cleanly when there is no addressed notification (legacy
	// link-only invites).
	if err := h.invalidateUserPlanIfNeeded(r.Context(), userID); err != nil {
		slog.Warn("plan cache invalidation failed after membership change", "error", err, "user_id", userID)
	}
	h.updateSeatCountIfNeeded(r.Context(), invite.HubID)
	onHubInviteAccepted(r.Context(), h.store, h.events, invite, userID, true)
	events.PublishHubMembersChanged(r.Context(), h.events, invite.HubID, userID)
	// User-axis emit so the accepting user's hub list grows live —
	// their frozen HubRoles doesn't yet contain this hub.
	events.PublishHubListChanged(r.Context(), h.events, userID, invite.HubID, "membership_joined")

	slog.Info("invite accepted", "hub_id", invite.HubID, "user_id", userID, "addressed", invite.InviteeUserID != nil)
	trackRequest(r, "api.hubs.invite.accept", map[string]any{"hub_id": invite.HubID})

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{"hub": hub, "role": invite.Role},
	})

	// Plan 18 onboarding tick used to fire here via the recorder.
	// Materializer-on-read + hub.list.changed invalidation now
	// covers first_hub_invite for both create and accept-invite
	// paths.
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens
	var result []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		}
	}
	s = string(result)
	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")
	if len(s) < 3 {
		s = s + "-hub"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

func generateHubID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// emitHubInviteNotificationIfAddressed fans out a hub_invite
// notification to the invitee when the invite was created with a
// resolved invitee_user_id. No-op for link-only invites (the
// legacy copy-link / token-redeem flow), which still exist as a
// backward-compatible fallback when the inviter has no existing
// account to address.
//
// Best-effort: a failure to write the notification or publish the
// SSE event does not fail the invite creation itself. The
// underlying hub_invites row is already durable; the notification
// is a delivery nicety on top.
func (h *HubsHandler) emitHubInviteNotificationIfAddressed(ctx context.Context, invite *model.HubInvite, inviterID string) {
	if invite == nil || invite.InviteeUserID == nil || *invite.InviteeUserID == "" {
		return
	}
	hub, err := h.store.GetHub(invite.HubID)
	if err != nil {
		slog.Warn("hub_invite notification: failed to load hub", "invite_id", invite.ID, "error", err)
		return
	}
	inviter, err := h.store.GetUser(inviterID)
	if err != nil {
		slog.Warn("hub_invite notification: failed to load inviter", "invite_id", invite.ID, "inviter_id", inviterID, "error", err)
		// Continue without inviter details rather than dropping the
		// whole notification.
		inviter = nil
	}

	payloadStruct := model.HubInvitePayload{
		Hub: model.HubInviteHubRef{
			ID:     hub.ID,
			Name:   hub.Name,
			Icon:   hub.Icon,
			Accent: hub.Accent,
		},
		Role:      invite.Role,
		ExpiresAt: invite.ExpiresAt,
	}
	if inviter != nil {
		display := inviter.DisplayName
		if display == "" {
			display = inviter.Name
		}
		payloadStruct.Inviter = model.HubInviteInviterRef{
			ID:          inviter.ID,
			DisplayName: display,
			AvatarURL:   inviter.AvatarURL,
		}
	}
	payloadBytes, err := json.Marshal(payloadStruct)
	if err != nil {
		slog.Warn("hub_invite notification: marshal payload", "invite_id", invite.ID, "error", err)
		return
	}

	notif := &model.Notification{
		ID:              generateHubID(),
		Audience:        model.AudienceUser,
		RecipientUserID: *invite.InviteeUserID,
		HubID:           hub.ID, // optional hub context
		Kind:            model.NotificationKindHubInvite,
		Status:          model.NotificationStatusPending,
		Priority:        1, // invites should show up above dream receipts
		SourceKind:      model.NotificationSourceHubInvite,
		SourceID:        invite.ID,
		Payload:         payloadBytes,
		CreatedAt:       time.Now(),
	}
	if err := h.store.CreateNotification(notif); err != nil {
		slog.Warn("hub_invite notification: create", "invite_id", invite.ID, "error", err)
		return
	}
	events.PublishNotificationCreated(ctx, h.events, notif)
}
