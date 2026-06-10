package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminDreamsHandler serves /v1/admin/users/{id}/dream-runs and
// /v1/admin/dream-runs/{id}. Read-only; mutations stay in the dream
// engine. Closes Tier C #2 of the remediation plan.
//
// The split between "list by user" and "detail by run id" mirrors
// the existing admin inventory pattern (users list → user detail,
// hubs list → hub detail). Listing by user walks both personal and
// team-hub membership so operators can answer "show me everything
// dreams did for this account" in one query.
type AdminDreamsHandler struct {
	store store.Store
}

// NewAdminDreamsHandler constructs the handler. Accepts only the
// store — the admin view is pure projection over dream_runs,
// dream_actions, and notifications(dream_run_id). No plan resolver
// or meter injected because the handler never writes.
func NewAdminDreamsHandler(s store.Store) *AdminDreamsHandler {
	return &AdminDreamsHandler{store: s}
}

// AdminDreamRunListResponse is the /v1/admin/users/{id}/dream-runs
// response envelope. Keyset pagination: NextCursor is the
// started_at of the last row on this page, in RFC3339Nano. Clients
// pass it back as ?cursor= to fetch the next page. Empty when no
// more rows remain.
type AdminDreamRunListResponse struct {
	Runs       []model.DreamRun `json:"runs"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// AdminDreamRunDetailResponse is /v1/admin/dream-runs/{id}. Groups
// the run row with its recorded actions and every notification the
// run produced (joined via dream_run_id). Admin UI renders this as
// a three-section view: summary, actions, audit trail.
type AdminDreamRunDetailResponse struct {
	Run           model.DreamRun       `json:"run"`
	Actions       []model.DreamAction  `json:"actions"`
	Notifications []model.Notification `json:"notifications"`
}

// ListDreamRunsForUser — GET /v1/admin/users/{id}/dream-runs
//
// Query params:
//   - hubId: optional; scope to a single hub.
//   - limit: optional; default 20, max 100.
//   - cursor: optional; RFC3339Nano started_at from previous page.
func (h *AdminDreamsHandler) ListDreamRunsForUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "User ID is required")
		return
	}

	q := r.URL.Query()
	opts := store.DreamRunListForUserOpts{
		UserID: userID,
		HubID:  q.Get("hubId"),
		Cursor: q.Get("cursor"),
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		opts.Limit = n
	}

	runs, nextCursor, err := h.store.ListDreamRunsForUser(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: AdminDreamRunListResponse{
			Runs:       runs,
			NextCursor: nextCursor,
		},
	})
}

// GetDreamRunDetail — GET /v1/admin/dream-runs/{id}
//
// Joins the run with its actions (via run_id) and with every
// notification tagged with this dream_run_id (migration 017).
// Notifications are ordered by created_at ASC so the admin UI can
// render them as a timeline.
func (h *AdminDreamsHandler) GetDreamRunDetail(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Run ID is required")
		return
	}

	run, err := h.store.GetDreamRunByID(r.Context(), runID)
	if errors.Is(err, store.ErrDreamRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Dream run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Actions and notifications are independent — no fk join at
	// the DB layer. Fetch them in parallel would save ~5ms but
	// adds complexity; sequential is fine for an admin endpoint.
	actions, err := h.store.GetDreamActions(runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if actions == nil {
		actions = []model.DreamAction{}
	}
	notifs, err := h.store.ListNotificationsByDreamRunID(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if notifs == nil {
		notifs = []model.Notification{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: AdminDreamRunDetailResponse{
			Run:           *run,
			Actions:       actions,
			Notifications: notifs,
		},
	})
}
