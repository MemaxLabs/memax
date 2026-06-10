package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func nowUTC() time.Time { return time.Now().UTC() }

func firstOfMonthUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// adminPlanChanger changes a user's plan. Satisfied by billing.Service.
type adminPlanChanger interface {
	ChangePlan(ctx context.Context, userID, newPlanID, source string) error
	ChangeHubPlan(ctx context.Context, hubID, planID, source string) error
	GetHubSubscription(ctx context.Context, hubID string) (*model.HubSubscription, error)
	ListBillingHubs(ctx context.Context, userID string) ([]model.HubSubscription, error)
}

// adminPlanResolver resolves plan limits. Satisfied by plans.Registry.
type adminPlanResolver interface {
	GetUserLimits(ctx context.Context, userID, planID string) model.UserLimits
	Reload(ctx context.Context) error
}

// adminUsageReader exposes the meter's committed counters for admin
// views. Narrow interface so tests can stub and so main.go doesn't
// have to hand AdminUsersHandler the full *meter.Meter with its
// Reserve/Commit/Rollback surface.
type adminUsageReader interface {
	GetCommittedUsage(ctx context.Context, userID string) (push, recall, ask int)
	GetCommittedStorageBytes(ctx context.Context, ownerID string) int64
}

// AdminUsersHandler serves /v1/admin/users/* and /v1/admin/plans/* endpoints.
type AdminUsersHandler struct {
	store        store.Store
	planChanger  adminPlanChanger
	planResolver adminPlanResolver
	usage        adminUsageReader
}

// NewAdminUsersHandler creates an AdminUsersHandler.
func NewAdminUsersHandler(s store.Store, pc adminPlanChanger, pr adminPlanResolver) *AdminUsersHandler {
	return &AdminUsersHandler{store: s, planChanger: pc, planResolver: pr}
}

// SetUsageReader wires the meter's read-only counter accessors. Optional —
// when unset the usage endpoint returns 0s across the board, matching the
// degraded-mode contract of every other quota code path.
func (h *AdminUsersHandler) SetUsageReader(r adminUsageReader) { h.usage = r }

// ListUsers returns a paginated list of users with search and plan filter.
// GET /v1/admin/users
func (h *AdminUsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	opts := model.AdminUserListOpts{
		Plan:   q.Get("plan"),
		Query:  q.Get("q"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
	users, nextCursor, total, err := h.store.ListUsers(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"users":       users,
		"next_cursor": nextCursor,
		"total":       total,
	}})
}

// GetUser returns detailed information about a specific user.
// GET /v1/admin/users/{id}
func (h *AdminUsersHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}

	user, err := h.store.GetUser(userID)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "not_found", "User not found")
		return
	}

	usage, _ := h.store.GetCurrentUsage(userID)
	memoryCount, _ := h.store.CountMemoriesByOwner(r.Context(), userID)
	override, _ := h.store.GetPlanOverride(r.Context(), userID)

	var limits *model.UserLimits
	if h.planResolver != nil {
		// Prefer scoped personal_plan_id for limit resolution
		planID := user.PersonalPlanID
		if planID == "" {
			planID = user.Plan
		}
		l := h.planResolver.GetUserLimits(r.Context(), userID, planID)
		limits = &l
	}

	hubs, _ := h.store.ListUserHubs(userID)
	// Load plan definition — try scoped ID first, then legacy
	planID := user.PersonalPlanID
	if planID == "" {
		planID = user.Plan
	}
	plan, _ := h.store.GetPlan(r.Context(), planID)
	if plan == nil {
		plan, _ = h.store.GetPlan(r.Context(), user.Plan)
	}

	// Effective plan (from cache). When it names a different plan than
	// the user's personal plan, we also resolve the full plan
	// definition + limits so the admin UI can render a second usage
	// block against the effective-plan denominator (i.e. what the user
	// actually gets when operating in their own hub).
	//
	// Comparison normalizes personal planID through LegacyToScopedPlanID
	// so legacy "free" vs scoped "personal_free" don't falsely register
	// as different, and so empty-personal users don't trip the branch
	// just because the resolver defaults to personal_free. Only a real
	// hub-driven elevation should surface the second card.
	effectivePlan, _ := h.store.GetEffectivePlan(r.Context(), userID)
	var effectivePlanDef *model.Plan
	var effectiveLimits *model.UserLimits
	normalizedPersonal := model.LegacyToScopedPlanID(planID)
	if normalizedPersonal == "" {
		normalizedPersonal = model.PersonalFreePlanID
	}
	if effectivePlan != nil && effectivePlan.PlanID != normalizedPersonal && h.planResolver != nil {
		effectivePlanDef, _ = h.store.GetPlan(r.Context(), effectivePlan.PlanID)
		el := h.planResolver.GetUserLimits(r.Context(), userID, effectivePlan.PlanID)
		effectiveLimits = &el
	}

	// Hub subscriptions the user pays for. Denormalize hub name + slug
	// inline so the admin UI can render "Subscribed: memax (memax)" with
	// a link to /admin/hubs/{id} without a second round-trip per row.
	// A GetHub failure per row degrades silently to raw id — a missing
	// hub should be a very rare state and isn't worth failing the page.
	var hubSubs []model.HubSubscription
	if h.planChanger != nil {
		hubSubs, _ = h.planChanger.ListBillingHubs(r.Context(), userID)
		for i := range hubSubs {
			hub, err := h.store.GetHub(hubSubs[i].HubID)
			if err != nil || hub == nil {
				continue
			}
			name := hub.Name
			slug := hub.Slug
			hubSubs[i].HubName = &name
			hubSubs[i].HubSlug = &slug
		}
	}

	detail := model.AdminUserDetail{
		User:             *user,
		Plan:             plan,
		EffectivePlan:    effectivePlan,
		EffectivePlanDef: effectivePlanDef,
		Usage:            usage,
		Limits:           limits,
		EffectiveLimits:  effectiveLimits,
		Override:         override,
		Hubs:             hubs,
		HubSubscriptions: hubSubs,
		MemoryCount:      memoryCount,
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// SetPlan changes a user's plan via the billing service.
// POST /v1/admin/users/{id}/set-plan
func (h *AdminUsersHandler) SetPlan(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}

	var req struct {
		PlanID string `json:"plan_id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil || req.PlanID == "" {
		writeError(w, http.StatusBadRequest, "invalid_json", "plan_id is required")
		return
	}

	if h.planChanger == nil {
		writeError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing service not configured")
		return
	}
	if err := h.planChanger.ChangePlan(r.Context(), userID, req.PlanID, "admin"); err != nil {
		// Distinguish not-found from other errors
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "plan_change_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status":  "updated",
		"plan_id": req.PlanID,
	}})
}

// SetOverrides sets per-user limit overrides.
// PUT /v1/admin/users/{id}/overrides
func (h *AdminUsersHandler) SetOverrides(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	adminID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}

	var req struct {
		Overrides map[string]any `json:"overrides"`
		Reason    string         `json:"reason"`
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

	override := &model.PlanOverride{
		UserID:    userID,
		Overrides: req.Overrides,
		Reason:    req.Reason,
		SetBy:     adminID,
	}
	if err := h.store.UpsertPlanOverride(r.Context(), override); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "updated"}})
}

// DeleteOverrides removes per-user limit overrides.
// DELETE /v1/admin/users/{id}/overrides
func (h *AdminUsersHandler) DeleteOverrides(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}
	if err := h.store.DeletePlanOverride(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "deleted"}})
}

// ListPlans returns all plans.
// GET /v1/admin/plans
func (h *AdminUsersHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.store.ListPlans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: plans})
}

// UpdatePlan updates a plan's limits.
// PATCH /v1/admin/plans/{id}
func (h *AdminUsersHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	if planID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Plan ID is required")
		return
	}

	existing, err := h.store.GetPlan(r.Context(), planID)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "not_found", "Plan not found")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	// Unmarshal patch onto existing plan (only provided fields update)
	if err := json.Unmarshal(body, existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	existing.ID = planID // prevent ID change via patch

	if err := h.store.UpdatePlan(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Synchronously reload the plan registry so the updated limits
	// take effect immediately (not after the 60s background interval).
	if h.planResolver != nil {
		if err := h.planResolver.Reload(r.Context()); err != nil {
			// Non-fatal: the plan was updated in Postgres, registry will
			// catch up on next background reload cycle.
			slog.Warn("plan registry reload failed after plan update",
				"error", err, "plan_id", planID)
		}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: existing})
}

// GetStats returns system-wide statistics.
// GET /v1/admin/stats
func (h *AdminUsersHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetSystemStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: stats})
}

// --- Hub Management ---

// ListTeamHubs returns a paginated slice of team hubs with
// subscription + member-count enrichment.
// GET /v1/admin/hubs?limit=&cursor=&q=
func (h *AdminUsersHandler) ListTeamHubs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	opts := model.AdminHubListOpts{
		Query:  q.Get("q"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
	hubs, nextCursor, total, err := h.store.ListTeamHubs(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Enrich with subscription + member count. One small round trip
	// per hub (2 queries × page size). Acceptable at page size 50;
	// if a future page size grows, consider batching via IN clauses.
	type hubWithSub struct {
		model.Hub
		Subscription *model.HubSubscription `json:"subscription,omitempty"`
		MemberCount  int                    `json:"member_count"`
	}
	items := make([]hubWithSub, 0, len(hubs))
	for _, hub := range hubs {
		item := hubWithSub{Hub: hub}
		// Admin view wants to see cancelled / past_due / trialing
		// subscriptions too — their presence is what flips the
		// frozen-badge UI. GetActiveHubSubscription filters those
		// out, so we use AnyStatus here.
		sub, err := h.store.GetHubSubscriptionAnyStatus(r.Context(), hub.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "enrichment_error",
				"Failed to load subscription for hub "+hub.ID)
			return
		}
		item.Subscription = sub
		count, err := h.store.CountHubMembers(hub.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "enrichment_error",
				"Failed to count members for hub "+hub.ID)
			return
		}
		item.MemberCount = count
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"hubs":        items,
		"next_cursor": nextCursor,
		"total":       total,
	}})
}

// GetHub returns the full admin view of a single team hub: meta +
// owner (for user cross-link) + subscription + resolved plan + full
// member roster + aggregate stats.
// GET /v1/admin/hubs/{id}
func (h *AdminUsersHandler) GetHub(w http.ResponseWriter, r *http.Request) {
	hubID := r.PathValue("id")
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Hub ID is required")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil || hub == nil {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}

	// Admin hub detail is a team-hub surface — the endpoint renders a
	// subscription/plan selector and drives SetHubPlan, which has no
	// meaning for personal hubs. Reject personal hubs at the handler
	// rather than letting the admin UI surface a misleading form that
	// could upsert a billable subscription on a personal hub.
	if hub.HubType != "team" {
		writeError(w, http.StatusNotFound, "not_found", "Hub not found")
		return
	}

	owner, _ := h.store.GetUser(hub.OwnerID)

	// Use the any-status read so inactive subscriptions
	// (cancelled, past_due) reach the admin UI and light up the
	// frozen banner. Active-only callers (billing mutation paths)
	// continue to go through planChanger.GetHubSubscription.
	sub, _ := h.store.GetHubSubscriptionAnyStatus(r.Context(), hubID)

	var plan *model.Plan
	planID := ""
	if sub != nil {
		planID = sub.PlanID
	} else if hub.Plan != "" {
		planID = hub.Plan
	}
	if planID != "" {
		plan, _ = h.store.GetPlan(r.Context(), planID)
	}

	// Members are fetched separately via
	// GET /v1/admin/hubs/{id}/members so large teams don't balloon
	// the detail response. MemberCount still ships inline so the
	// stat tile renders without a second fetch.
	memberCount, err := h.store.CountHubMembers(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	memoryCount, _ := h.store.CountMemoriesByHub(r.Context(), hubID)

	detail := model.AdminHubDetail{
		Hub:          *hub,
		Owner:        owner,
		Subscription: sub,
		Plan:         plan,
		MemberCount:  memberCount,
		MemoryCount:  memoryCount,
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// ListHubMembers returns a paginated page of a hub's members for the
// admin roster table. Keyset pagination on (joined_at, user_id) ASC.
// GET /v1/admin/hubs/{id}/members?cursor=&q=&limit=
func (h *AdminUsersHandler) ListHubMembers(w http.ResponseWriter, r *http.Request) {
	hubID := r.PathValue("id")
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Hub ID is required")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	opts := model.AdminHubMemberListOpts{
		Query:  q.Get("q"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
	members, nextCursor, total, err := h.store.ListHubMembersPaginated(r.Context(), hubID, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if members == nil {
		members = []model.HubMember{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"members":     members,
		"next_cursor": nextCursor,
		"total":       total,
	}})
}

// --- Hub Subscription Management ---

// GetHubSubscription returns the active subscription for a hub.
// GET /v1/admin/hubs/{id}/subscription
func (h *AdminUsersHandler) GetHubSubscription(w http.ResponseWriter, r *http.Request) {
	hubID := r.PathValue("id")
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Hub ID is required")
		return
	}
	if h.planChanger == nil {
		writeError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing service not configured")
		return
	}
	sub, err := h.planChanger.GetHubSubscription(r.Context(), hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if sub == nil {
		// Return null directly so SDK's HubSubscription | null contract works.
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: nil})
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: sub})
}

// SetHubPlan changes a hub's subscription plan.
// POST /v1/admin/hubs/{id}/set-plan
func (h *AdminUsersHandler) SetHubPlan(w http.ResponseWriter, r *http.Request) {
	hubID := r.PathValue("id")
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Hub ID is required")
		return
	}
	var req struct {
		PlanID string `json:"plan_id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil || req.PlanID == "" {
		writeError(w, http.StatusBadRequest, "invalid_json", "plan_id is required")
		return
	}
	if h.planChanger == nil {
		writeError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing service not configured")
		return
	}
	if err := h.planChanger.ChangeHubPlan(r.Context(), hubID, req.PlanID, "admin"); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "hub_plan_change_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status":  "updated",
		"hub_id":  hubID,
		"plan_id": req.PlanID,
	}})
}

// ListBillingHubs returns all hub subscriptions for a billing user.
// GET /v1/admin/users/{id}/billing-hubs
func (h *AdminUsersHandler) ListBillingHubs(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}
	if h.planChanger == nil {
		writeError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing service not configured")
		return
	}
	subs, err := h.planChanger.ListBillingHubs(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: subs})
}

// GetUserUsage returns the current-period quota state for a user plus
// lifetime counters (memory count, storage bytes). Used by the admin
// UI to answer "is user X hitting their limits?" without dropping to
// psql + redis-cli.
//
// GET /v1/admin/users/{id}/usage
//
// Redis degrades gracefully — if the meter isn't wired or Redis is
// unreachable, monthly counters return 0 and the admin UI renders
// "usage unavailable" rather than erroring. Lifetime counters come
// from Postgres directly, so they stay accurate regardless.
func (h *AdminUsersHandler) GetUserUsage(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}

	user, err := h.store.GetUser(userID)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "not_found", "User not found")
		return
	}

	planID := user.PersonalPlanID
	if planID == "" {
		planID = user.Plan
	}
	limits := h.planResolver.GetUserLimits(r.Context(), userID, planID)

	// Monthly counters — Redis-backed. Zeros when the meter isn't wired
	// (dev without Redis) or when the user hasn't hit any quota op this
	// period.
	var pushCur, recallCur, askCur int
	var storageBytesCur int64
	if h.usage != nil {
		pushCur, recallCur, askCur = h.usage.GetCommittedUsage(r.Context(), userID)
		storageBytesCur = h.usage.GetCommittedStorageBytes(r.Context(), userID)
	}
	// Lifetime memory count — authoritative via Postgres so this is
	// always accurate, Redis or not. The meter caches the same number
	// in Redis for hot-path enforcement; the admin view prefers truth
	// over cache.
	memoryCount, _ := h.store.CountMemoriesByOwner(r.Context(), userID)
	// Fallback for storage_bytes when Redis is unpopulated: re-derive
	// from the authoritative Postgres SUM. The sum is expensive, but
	// admin requests are rare enough that the trade of ~tens of ms for
	// an accurate number is worth it.
	if storageBytesCur == 0 {
		if sum, err := h.store.SumStorageBytesByOwner(r.Context(), userID); err == nil {
			storageBytesCur = sum
		}
	}

	type limitPair struct {
		Current int `json:"current"`
		Limit   int `json:"limit"` // -1 means unlimited
	}
	type byteLimitPair struct {
		Current int64 `json:"current"`
		Limit   int64 `json:"limit"` // -1 means unlimited
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"plan_id":      limits.PlanID,
		"plan_display": limits.PlanDisplayName,
		"period":       currentBillingPeriod(),
		"monthly": map[string]any{
			"push":   limitPair{Current: pushCur, Limit: limits.PushLimit},
			"recall": limitPair{Current: recallCur, Limit: limits.RecallLimit},
			"ask":    limitPair{Current: askCur, Limit: limits.AskLimit},
		},
		"lifetime": map[string]any{
			"memory_count":  limitPair{Current: memoryCount, Limit: limits.MemoryLimit},
			"storage_bytes": byteLimitPair{Current: storageBytesCur, Limit: limits.StorageBytesLimit},
		},
	}})
}

// currentBillingPeriod returns a small struct describing the current
// monthly window so the admin UI can render "April 2026: 8/10 asks"
// without deriving dates client-side.
func currentBillingPeriod() map[string]string {
	now := nowUTC()
	start := firstOfMonthUTC(now)
	end := firstOfMonthUTC(start.AddDate(0, 1, 1)) // next month's first day
	return map[string]string{
		"start": start.Format("2006-01-02T15:04:05Z"),
		"end":   end.Format("2006-01-02T15:04:05Z"),
	}
}
