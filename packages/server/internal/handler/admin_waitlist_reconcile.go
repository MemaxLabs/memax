package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminWaitlistReconcileHandler serves the orphan-invite repair
// endpoints under /v1/admin/waitlist/.
//
// Background: between 2026-04-14 (Phase F waitlist/register) and
// 2026-04-20 (fix 8d87c591) the web client dropped the invite token
// on the OAuth redirect, so every invited user who signed in during
// that window registered without having their invite consumed.
// Symptoms:
//   - waitlist_invites.status stuck on 'active'
//   - users.invite_id NULL
//   - users.personal_plan_id stayed on ” or personal_free instead
//     of upgrading to personal_early_access
//   - (when registration_mode flipped to invite_only later) those
//     users got bounced to /waitlist on every login attempt
//
// The repair reuses AuthHandler.ConsumeInviteForUser — calling a
// parallel helper would risk drifting from the invite state machine
// that production login flows depend on.
type AdminWaitlistReconcileHandler struct {
	store    store.Store
	consumer inviteConsumer
}

// inviteConsumer is the narrow slice of AuthHandler this handler needs.
// Kept as an interface so unit tests can substitute a fake without
// constructing the full OAuth stack.
type inviteConsumer interface {
	ConsumeInviteForUser(ctx context.Context, token string, user *model.User)
}

// NewAdminWaitlistReconcileHandler wires the reconcile endpoints.
// consumer is typically *AuthHandler; the interface exists purely for
// testability.
func NewAdminWaitlistReconcileHandler(s store.Store, c inviteConsumer) *AdminWaitlistReconcileHandler {
	return &AdminWaitlistReconcileHandler{store: s, consumer: c}
}

// AdminOrphanCandidatesResponse is the envelope for
// GET /v1/admin/waitlist/orphans. The operator inspects the list
// before firing the reconcile POST.
type AdminOrphanCandidatesResponse struct {
	Candidates []model.OrphanWaitlistCandidate `json:"candidates"`
	Scanned    int                             `json:"scanned"`
	Since      time.Time                       `json:"since"`
	Limit      int                             `json:"limit"`
}

// AdminUserOrphanStatusResponse is the GET
// /v1/admin/users/{id}/orphan-status envelope. Backs the
// "Waitlist status" card on the admin user-detail page: a quick
// "can this one user be reconciled?" check without hitting the
// bulk list endpoint (which is scan-window-gated).
//
// IsOrphan is the boolean the UI actually gates on. Candidate
// carries the invite_id + expires_at for display; it's omitted
// entirely when IsOrphan is false so the client doesn't confuse
// "no candidate" with "candidate exists but empty."
type AdminUserOrphanStatusResponse struct {
	IsOrphan  bool                           `json:"is_orphan"`
	Candidate *model.OrphanWaitlistCandidate `json:"candidate,omitempty"`
}

// GetUserOrphanStatus — GET /v1/admin/users/{id}/orphan-status
//
// Single-user check used by the admin user-detail page to
// render the reconcile affordance conditionally. Mirrors the
// same predicate set as the bulk lister via the shared
// Store.GetOrphanCandidateForUser method.
func (h *AdminWaitlistReconcileHandler) GetUserOrphanStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}
	candidate, err := h.store.GetOrphanCandidateForUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: AdminUserOrphanStatusResponse{
			IsOrphan:  candidate != nil,
			Candidate: candidate,
		},
	})
}

// ListOrphans — GET /v1/admin/waitlist/orphans
//
// Query params:
//   - since: required, RFC3339 date. Users whose `created_at >=
//     since` are scanned. Forces the operator to narrow; prevents a
//     dashboard accident from triggering a full-users join.
//   - limit: optional; default 100, max 500.
//
// Read-only. Returns the cohort a reconcile POST would act on, so
// the operator can eyeball the list first.
func (h *AdminWaitlistReconcileHandler) ListOrphans(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawSince := q.Get("since")
	if rawSince == "" {
		writeError(w, http.StatusBadRequest, "missing_since",
			"`since` is required (RFC3339). Narrow the scan window to prevent full-users joins.")
		return
	}
	since, err := time.Parse(time.RFC3339, rawSince)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_since",
			"`since` must be RFC3339 (e.g. 2026-04-14T00:00:00Z).")
		return
	}

	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		limit = n
	}

	candidates, err := h.store.ListOrphanWaitlistCandidates(r.Context(),
		model.OrphanWaitlistCandidateOpts{Since: since, Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if candidates == nil {
		candidates = []model.OrphanWaitlistCandidate{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: AdminOrphanCandidatesResponse{
			Candidates: candidates,
			Scanned:    len(candidates),
			Since:      since,
			Limit:      limit,
		},
	})
}

// AdminReconcileRequest is the POST /v1/admin/waitlist/reconcile body.
// UserIDs is required and non-empty — no "reconcile everything"
// shorthand on purpose. Operators must run ListOrphans first, pick
// the ids they actually want to repair, and post those. Caps at 100
// per call so a typo can't mass-mutate plans.
type AdminReconcileRequest struct {
	UserIDs []string `json:"user_ids"`
}

// AdminReconcileItemResult is the per-user outcome.
// Action values:
//   - "reconciled": invite was consumed + plan upgraded (or user was
//     already in the right state and the consume was a no-op; see
//     Reason).
//   - "skipped":    user was not an eligible orphan (no matching
//     active invite, or already has an invite_id, or is already on
//     a non-free plan). Reason explains which.
//   - "error":      a store/consumer call failed mid-flight. Reason
//     carries the error string.
//
// BeforePlan/AfterPlan reflect users.personal_plan_id snapshotted
// at the top and bottom of the per-user block. Empty string means
// "no plan set" (legacy users pre-scoped-plans).
type AdminReconcileItemResult struct {
	UserID     string `json:"user_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
	BeforePlan string `json:"before_plan,omitempty"`
	AfterPlan  string `json:"after_plan,omitempty"`
	InviteID   string `json:"invite_id,omitempty"`
}

// AdminReconcileResponse aggregates per-user outcomes plus a roll-up
// count so the operator can eyeball success / skip / error totals
// without iterating the full Results array.
type AdminReconcileResponse struct {
	Results    []AdminReconcileItemResult `json:"results"`
	Reconciled int                        `json:"reconciled"`
	Skipped    int                        `json:"skipped"`
	Errored    int                        `json:"errored"`
}

const reconcileBatchLimit = 100

// Reconcile — POST /v1/admin/waitlist/reconcile
//
// Takes a list of user_ids and runs ConsumeInviteForUser against the
// (email, token) pair derived from waitlist_invites.email =
// users.email for each. The consumer is the exact function the login
// flow uses — all state transitions (invite status, waitlist status,
// can_create_hub, plan upgrade, users.invite_id) stay on one
// authoritative path, and the function's existing idempotency
// (early-return on consumed / expired / already-linked) makes
// re-runs safe.
//
// Per-user errors do not abort the batch; each user is processed
// independently and every outcome is reported in the response body.
// The HTTP status is 200 as long as the batch input parsed — the
// caller inspects Results to find individual failures.
func (h *AdminWaitlistReconcileHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	if h.consumer == nil {
		writeError(w, http.StatusServiceUnavailable, "consumer_unavailable",
			"Invite consumer not configured on this server.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}
	var req AdminReconcileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	if len(req.UserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "missing_user_ids",
			"`user_ids` is required and must be non-empty. Run GET /v1/admin/waitlist/orphans first, then POST the ids you want to repair.")
		return
	}
	if len(req.UserIDs) > reconcileBatchLimit {
		writeError(w, http.StatusBadRequest, "too_many_user_ids",
			fmt.Sprintf("At most %d user_ids per call.", reconcileBatchLimit))
		return
	}

	// Small batch; straightforward sequential processing. Parallel
	// would hit the planChanger billing path concurrently, which
	// isn't a tested load and buys us nothing — 100 rows × a few DB
	// round-trips each is a few seconds on the slow path.
	results := make([]AdminReconcileItemResult, 0, len(req.UserIDs))
	seen := make(map[string]struct{}, len(req.UserIDs))
	var reconciled, skipped, errored int

	for _, userID := range req.UserIDs {
		if userID == "" {
			results = append(results, AdminReconcileItemResult{
				UserID: userID, Action: "error", Reason: "empty_user_id",
			})
			errored++
			continue
		}
		if _, dup := seen[userID]; dup {
			results = append(results, AdminReconcileItemResult{
				UserID: userID, Action: "skipped", Reason: "duplicate_in_batch",
			})
			skipped++
			continue
		}
		seen[userID] = struct{}{}

		result := h.reconcileOne(r.Context(), userID)
		results = append(results, result)
		switch result.Action {
		case "reconciled":
			reconciled++
		case "skipped":
			skipped++
		default:
			errored++
		}
	}

	slog.Info("admin waitlist reconcile complete",
		"requested", len(req.UserIDs),
		"reconciled", reconciled,
		"skipped", skipped,
		"errored", errored)

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: AdminReconcileResponse{
			Results:    results,
			Reconciled: reconciled,
			Skipped:    skipped,
			Errored:    errored,
		},
	})
}

// reconcileOne does the lookup-and-consume dance for a single user.
// Extracted so the batch loop reads as a straight accumulator.
//
// Three possible outcomes (all carry before_plan; reconciled +
// already_repaired also carry after_plan):
//
//   - reconciled       — consumer ran and the postcondition holds:
//     personal_plan_id flipped to
//     personal_early_access.
//   - skipped          — user is not an orphan, or an earlier
//     writer already upgraded them.
//     reason disambiguates.
//   - error            — the consumer ran but the state machine
//     didn't complete (e.g. planChanger failed
//     mid-flight; ConsumeInviteForUser logs
//     but doesn't surface errors). Distinct
//     from skipped because the operator
//     should investigate, not move on.
func (h *AdminWaitlistReconcileHandler) reconcileOne(ctx context.Context, userID string) AdminReconcileItemResult {
	user, err := h.store.GetUser(userID)
	if err != nil || user == nil {
		return AdminReconcileItemResult{
			UserID: userID, Action: "error", Reason: "user_not_found",
		}
	}

	beforePlan := user.PersonalPlanID

	// User-scoped lookup — the bulk lister's (since, limit) pair is
	// a guard for the dashboard preview, not a contract the repair
	// path can rely on. GetOrphanCandidateForUser returns (nil, nil)
	// when the user isn't currently an orphan (not in the cohort
	// anymore; concurrent repair; never was).
	match, err := h.store.GetOrphanCandidateForUser(ctx, userID)
	if err != nil {
		return AdminReconcileItemResult{
			UserID: userID, Action: "error", Reason: err.Error(), BeforePlan: beforePlan,
		}
	}
	if match == nil {
		// If the user is already on early_access, distinguish
		// "someone else repaired this first" from the generic
		// "not an orphan" so the operator knows the state is
		// final, not stale.
		reason := "not_an_orphan"
		if beforePlan == model.PersonalEarlyAccessPlanID {
			reason = "already_upgraded"
		}
		return AdminReconcileItemResult{
			UserID: userID, Action: "skipped",
			Reason:     reason,
			BeforePlan: beforePlan,
		}
	}

	// ConsumeInviteForUser is idempotent and internally logs every
	// state change. It does not return errors — mirrors the login
	// path contract. We therefore re-fetch the user afterwards and
	// verify the postcondition ourselves, so the response can't lie
	// about partial failure (e.g. invite marked used but planChanger
	// failed).
	h.consumer.ConsumeInviteForUser(ctx, match.InviteToken, user)

	after, err := h.store.GetUser(userID)
	if err != nil || after == nil {
		return AdminReconcileItemResult{
			UserID: userID, Action: "error",
			Reason:     "post_consume_user_lookup_failed",
			BeforePlan: beforePlan, InviteID: match.InviteID,
		}
	}
	afterPlan := after.PersonalPlanID

	// Happy path: plan upgraded. Anything else means the consumer's
	// internal state machine stalled and the operator should look at
	// the server logs for the root cause (ConsumeInviteForUser emits
	// slog.Warn on each failure mode).
	if afterPlan != model.PersonalEarlyAccessPlanID {
		return AdminReconcileItemResult{
			UserID: userID, Action: "error",
			Reason:     "plan_upgrade_did_not_apply",
			BeforePlan: beforePlan, AfterPlan: afterPlan,
			InviteID: match.InviteID,
		}
	}

	return AdminReconcileItemResult{
		UserID:     userID,
		Action:     "reconciled",
		BeforePlan: beforePlan,
		AfterPlan:  afterPlan,
		InviteID:   match.InviteID,
	}
}
