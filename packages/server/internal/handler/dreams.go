package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// DreamsHandler exposes dream reports to users.
// Dreams run automatically via the worker (nightly). Users only see results.
// Dev users can trigger a dream cycle manually via POST /v1/dreams/trigger.
type DreamsHandler struct {
	store    store.Store
	events   events.Publisher
	enqueue  func(ctx context.Context, hubID string, triggeredBy string) error // set via SetEnqueue
	resolver quota.DreamResolver                                                // set via SetQuotaResolver; nil = quota gate skipped (legacy)
	mode     quota.Mode                                                         // soft (Phase 1 default) or hard (Phase 3)
}

func NewDreamsHandler(s store.Store, publisher events.Publisher) *DreamsHandler {
	return &DreamsHandler{
		store:  s,
		events: publisher,
		// Default mode is soft so behavior is "allow + record signal" until
		// operators explicitly flip to hard via env. Soft is also the
		// correct default for dev/test environments where we don't want
		// quota gates blocking iteration.
		mode: quota.ModeSoft,
	}
}

// SetEnqueue wires the queue client for manual dream triggers.
func (h *DreamsHandler) SetEnqueue(fn func(ctx context.Context, hubID string, triggeredBy string) error) {
	h.enqueue = fn
}

// SetQuotaResolver wires the plan resolver used for the dream-quota
// gate. When unset, the gate is skipped (manual triggers proceed
// regardless of quota state) — appropriate for environments without
// a fully-configured plan stack. Production wiring should always
// call this after constructing the resolver.
func (h *DreamsHandler) SetQuotaResolver(r quota.DreamResolver) {
	h.resolver = r
}

// SetQuotaMode overrides the default soft enforcement mode. Phase 1
// rollout uses soft; Phase 3 flips to hard. Defaults to soft if the
// caller never invokes this — matches the plan's "ship metering with
// soft caps" rollout.
func (h *DreamsHandler) SetQuotaMode(m quota.Mode) {
	if m.Valid() {
		h.mode = m
	}
}

// Trigger manually enqueues a dream cycle for the current user.
// POST /v1/dreams/trigger
func (h *DreamsHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Dreams require an explicit or resolved hub.")
		return
	}

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub_not_found", "Hub not found.")
		return
	}
	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if !canTriggerDreams(role, hub) && hub.OwnerID != userID {
		writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to trigger dreams for this hub.")
		return
	}

	// Mirror the engine's freeze gate at the API edge so the caller
	// gets a typed 409 instead of silently enqueueing a job that the
	// engine will skip. Also avoids paying the River scheduling cost
	// for a work unit we already know will no-op.
	//
	// Use the any-status lookup: GetActiveHubSubscription filters
	// WHERE status='active', which masks cancelled and past_due rows
	// as nil — exactly the states IsHubFrozen is designed to catch.
	sub, _ := h.store.GetHubSubscriptionAnyStatus(r.Context(), hubID)
	if model.IsHubFrozen(sub, time.Now()) {
		writeError(w, http.StatusConflict, "hub_frozen", "This hub is frozen. Resolve the over-limit or subscription state before triggering dreams.")
		return
	}

	// Dream quota gate. Selects the right scope (personal vs hub)
	// based on hub.HubType and runs CheckDreamQuota. Soft mode
	// (Phase 1 default) always allows through but populates the
	// telemetry banner shape; hard mode (Phase 3) rejects past cap
	// with 402.
	//
	// If no resolver is wired, the gate is skipped — appropriate
	// for dev/test environments without a fully-configured plan
	// stack. Production always wires a resolver.
	if h.resolver != nil {
		// h.store satisfies quota.HubLookup (it has GetHub(id) →
		// (*model.Hub, error) on the Store interface).
		scope, err := quota.SelectScope(h.store, hubID, userID)
		if err != nil {
			slog.Error("dream quota: scope selection failed", "hub_id", hubID, "error", err)
			writeError(w, http.StatusInternalServerError, "quota_error", "Could not resolve quota scope.")
			return
		}
		decision, err := quota.CheckDreamQuota(r.Context(), h.resolver, h.store, scope, h.mode)
		if err != nil {
			slog.Error("dream quota: check failed", "hub_id", hubID, "scope", scope.Type, "error", err)
			writeError(w, http.StatusInternalServerError, "quota_error", "Could not check dream quota.")
			return
		}
		if !decision.Allowed {
			// 402 with the standard envelope. decision.Details
			// matches the shape spec'd in docs/plans/23-dream-tiers.md
			// (code, scope, hub_id, limit, used, resets_at,
			// upgrade_url, docs_url, quota_source) and is JSON-
			// marshalable directly via Details any.
			slog.Info("dream quota: trigger denied",
				"hub_id", hubID, "scope", scope.Type,
				"tier", decision.Tier, "limit", decision.Limit, "used", decision.Used)
			code := "lucid_quota_exhausted"
			if decision.Details != nil {
				code = decision.Details.Code
			}
			WriteErrorWithDetails(w, http.StatusPaymentRequired, code,
				"Dream quota exhausted for this billing period.",
				decision.Details)
			return
		}
		// Soft-mode-would-have-blocked telemetry: emit a structured
		// log so dashboards can count what fraction of paid users
		// would be blocked under hard enforcement during the Phase 1
		// rollout window. The completion path (next commit) writes
		// the persistent dream_quota_signals row.
		if h.mode == quota.ModeSoft && decision.Details != nil {
			slog.Info("dream quota: would_have_blocked under hard mode",
				"hub_id", hubID, "scope", scope.Type,
				"tier", decision.Tier, "limit", decision.Limit, "used", decision.Used,
				"quota_source", decision.QuotaSource)
		}
	}

	if h.enqueue == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Dream trigger not configured.")
		return
	}

	if err := h.enqueue(r.Context(), hubID, userID); err != nil {
		slog.Error("failed to enqueue dream cycle", "hub_id", hubID, "error", err)
		writeError(w, http.StatusInternalServerError, "enqueue_failed", "Could not trigger dream cycle.")
		return
	}

	slog.Info("dream cycle triggered manually", "hub_id", hubID)
	events.PublishDreamsChanged(r.Context(), h.events, hubID, userID)
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "queued"},
	})
}

// GetUsage returns the current dream-quota state for the caller's
// scope. Read-only — never charges quota, never enqueues a run.
//
// GET /v1/usage/dreams[?hub_id=...]
//
// Scope selection mirrors List's hub-resolution rules:
//
//   - Hub-scoped credentials (HubScopeMode=allowlist) are LOCKED to
//     their first scoped hub regardless of what the caller asks for.
//     Without this, a token bound to hub A could call /v1/usage/dreams
//     with no hub_id and learn the user's personal-scope quota — a
//     scope leak. Mirrors the same defense in List.
//   - Otherwise, ?hub_id query wins; absent → personal scope
//     (ScopeUser against the caller).
//
// Then per Trigger's scope rules:
//
//   - hubID resolved to a "team" hub → ScopeHub. Reports the hub's
//     fixed-per-hub Lucid counter.
//   - hubID resolved to a "personal" hub → ScopeUser (against the hub
//     owner). Reports the owner's effective tier.
//   - hubID empty → ScopeUser (against the caller). Reports the
//     caller's effective personal tier (max(personal, best_hub)
//     baked into the resolver).
//
// Authorization: when hubID is non-empty (whether from query or from
// scoped-credential default), the caller must have PermDreamRead on
// it. The route also passes through requiredHTTPPermission's
// PermDreamRead floor, so a credential with NO read permission
// anywhere is rejected before this handler runs.
//
// The Allowed field reflects whether a trigger right now would
// succeed under the configured mode (soft never blocks finite-cap
// exhaustion during Phase 1 rollout; only Limit=0 disables in soft
// mode). UI clients gate the "Dream now" button on this.
func (h *DreamsHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)

	// Without a resolver, we have no way to answer accurately. Return
	// 503 (matches Trigger's "not_available" when enqueue is unwired)
	// rather than make up a response. Production always wires both.
	if h.resolver == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Dream quota not configured.")
		return
	}

	// Hub resolution: hub-scoped credentials override the query
	// param; otherwise the query param wins. Header X-Hub-ID is NOT
	// consulted here — Usage is a deliberate per-call lookup, not a
	// "default to active hub" surface like the bar.
	var hubID string
	authCtx := GetAuthContext(r)
	if authCtx != nil &&
		authCtx.HubScopeMode == HubScopeAllowlist &&
		len(authCtx.ScopedHubIDs) > 0 {
		hubID = authCtx.ScopedHubIDs[0]
	} else {
		hubID = strings.TrimSpace(r.URL.Query().Get("hub_id"))
	}

	if hubID != "" {
		if !Can(r, PermDreamRead, hubID) {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to read dream quota for this hub.")
			return
		}
	}

	scope, err := quota.SelectScope(h.store, hubID, userID)
	if err != nil {
		slog.Error("dream usage: scope selection failed", "hub_id", hubID, "error", err)
		writeError(w, http.StatusInternalServerError, "quota_error", "Could not resolve quota scope.")
		return
	}
	decision, err := quota.CheckDreamQuota(r.Context(), h.resolver, h.store, scope, h.mode)
	if err != nil {
		slog.Error("dream usage: check failed", "hub_id", hubID, "scope", scope.Type, "error", err)
		writeError(w, http.StatusInternalServerError, "quota_error", "Could not check dream quota.")
		return
	}

	// Map internal ScopeType to the public API contract used by
	// DenialDetails ("personal" | "hub"). Clients see one vocabulary
	// across both surfaces.
	scopeStr := "personal"
	if scope.IsHub() {
		scopeStr = "hub"
	}
	// Remaining as *int: nil signals unlimited (omitted from JSON);
	// non-nil 0 signals exhausted finite cap. Without this, JSON
	// "exhausted" and "unlimited" both serialize as the field
	// missing — clients can't tell them apart.
	var remaining *int
	if decision.Limit != -1 {
		r := decision.Remaining
		remaining = &r
	}

	resp := model.DreamUsage{
		Scope:       scopeStr,
		Tier:        string(decision.Tier),
		Mode:        string(decision.Mode),
		Limit:       decision.Limit,
		Used:        decision.Used,
		Remaining:   remaining,
		Allowed:     decision.Allowed,
		PeriodStart: decision.PeriodStart,
		PeriodEnd:   decision.PeriodEnd,
		QuotaSource: decision.QuotaSource,
	}
	if scope.IsHub() {
		resp.HubID = scope.HubID
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// dreamListLimit parses the ?limit query param. Returning 0 when
// absent lets the store's default (20) fire — keeping the public
// contract honest with the SDK docstring rather than silently
// capping at a different value inside the handler.
func dreamListLimit(query url.Values) (int, error) {
	raw := query.Get("limit")
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, strconv.ErrSyntax
	}
	return limit, nil
}

// List returns recent dream runs for the current user. GET /v1/dreams
//
// Query params:
//   - hub_id:  optional; narrow to one hub. Caller must have
//     PermDreamRead on it (covers both membership and
//     scoped-credential allowlist).
//   - limit:   optional; default 20, max 100 (store caps).
//   - cursor:  optional; composite "<rfc3339nano>|<uuid>" from the
//     previous page's next_cursor.
//
// Scope resolution:
//  1. Hub-scoped credentials are locked to their ScopedHubIDs; any
//     user-supplied hub_id is ignored in favor of the scoped hub
//     so a token for hub A can't exfiltrate hub B via query param.
//  2. Otherwise the explicit ?hub_id query wins.
//  3. Otherwise the raw X-Hub-ID request header wins (lets legacy
//     hub-scoped callers continue passing the header).
//  4. Otherwise the response spans every hub the caller has
//     PermDreamRead on — the default for the Dream history view.
//
// We deliberately skip GetHubID(r) here: HubContext middleware
// defaults it to the caller's personal hub when no hub was
// supplied, which would silently collapse the "all hubs" default
// into personal-only and make the history filter misleading.
//
// Response envelope: {runs, next_cursor}. Keyset pagination shared
// with the admin endpoint (commit 3.2 composite cursor) so the
// client pagination logic lives in one place.
func (h *DreamsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	q := r.URL.Query()

	var hubID string
	authCtx := GetAuthContext(r)
	if authCtx != nil &&
		authCtx.HubScopeMode == HubScopeAllowlist &&
		len(authCtx.ScopedHubIDs) > 0 {
		hubID = authCtx.ScopedHubIDs[0]
	} else {
		hubID = strings.TrimSpace(q.Get("hub_id"))
		if hubID == "" {
			hubID = strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		}
	}

	// Explicit hub → PermDreamRead gate. Can() reads
	// PermissionsByHub[hubID], which already combines membership
	// role AND scope allowlist, so this single check rejects both
	// non-members and cross-hub access through a scoped token.
	if hubID != "" {
		if !Can(r, PermDreamRead, hubID) {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to read dreams for this hub")
			return
		}
	}

	limit, err := dreamListLimit(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", "Limit must be a positive integer.")
		return
	}

	runs, nextCursor, err := h.store.ListDreamRunsForUser(r.Context(), store.DreamRunListForUserOpts{
		UserID: userID,
		HubID:  hubID,
		Limit:  limit,
		Cursor: q.Get("cursor"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if runs == nil {
		runs = []model.DreamRun{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: model.DreamRunListResponse{
			Runs:       runs,
			NextCursor: nextCursor,
		},
	})
}

// GetReport returns the latest dream run with its actions and pending review count.
// GET /v1/dreams/report
func (h *DreamsHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Dream report requires an explicit or resolved hub.")
		return
	}

	run, err := h.store.GetLatestDreamRunByHub(hubID)
	if err != nil {
		intelligence, summaryErr := h.buildDreamIntelligenceSummary(r, userID, hubID, nil)
		if summaryErr != nil {
			writeError(w, http.StatusInternalServerError, "store_error", summaryErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{
			Data: model.DreamReport{
				HasRun:       false,
				Message:      "No dream cycles have run yet for this hub. Dreams run automatically each night to consolidate knowledge.",
				Intelligence: intelligence,
			},
		})
		return
	}

	actions, _ := h.store.GetDreamActions(run.ID)
	if actions == nil {
		actions = []model.DreamAction{}
	}
	intelligence, err := h.buildDreamIntelligenceSummary(r, userID, hubID, run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: model.DreamReport{
			HasRun:       true,
			Run:          run,
			Actions:      actions,
			Intelligence: intelligence,
		},
	})
}

func (h *DreamsHandler) buildDreamIntelligenceSummary(
	r *http.Request,
	userID string,
	hubID string,
	run *model.DreamRun,
) (model.DreamIntelligenceSummary, error) {
	out := model.DreamIntelligenceSummary{}
	if run != nil {
		out.LatestRun = &model.DreamLatestRunSummary{
			Merged:              run.DuplicatesMerged,
			ContradictionsFound: run.ContradictionsFound,
			Archived:            run.MemoriesArchived,
			Organized:           run.MemoriesOrganized,
			Restructured:        run.TopicsRestructured,
		}
	}

	summary, err := h.store.GetNotificationSummary(r.Context(), store.NotificationSummaryOpts{
		UserID: userID,
		HubIDs: GetAccessibleHubIDs(r),
		HubID:  hubID,
	})
	if err != nil {
		return out, err
	}

	out.PendingReview = model.DreamPendingReviewSummary{
		Contradictions:    summary.ByKind[model.NotificationKindReviewContradiction].Pending,
		TopicMerges:       summary.ByKind[model.NotificationKindReviewTopicMerge].Pending,
		TopicRestructures: summary.ByKind[model.NotificationKindReviewTopicRestructure].Pending,
	}
	out.PendingReview.Total =
		out.PendingReview.Contradictions +
			out.PendingReview.TopicMerges +
			out.PendingReview.TopicRestructures
	return out, nil
}
