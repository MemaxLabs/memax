package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminSeedMemoriesHandler backs /v1/admin/seed-memories — admin CRUD
// over the onboarding seed templates that live in the system tutorial
// hub (plan 23 §5.7).
//
// Why admin-only: editing a template affects every FUTURE signup's
// onboarding experience. Existing user copies stay untouched (per
// plan 23 principle 4 — future-only edits). Wrong copy here would
// corrupt the first impression for every new user without our
// noticing.
//
// Surface scope: list, get, update title/content/tags/state. Toggling
// `state` between "active" and "archived" is the enable/disable
// switch — archived templates skip the copy worker on signup.
//
// Not an MCP surface; not exposed via the public SDK (admin endpoints
// stay under packages/web/src/lib/admin-client/, never in memax-sdk —
// see AGENTS.md "Admin Surface Boundary").
type AdminSeedMemoriesHandler struct {
	store store.Store
	// riverClient enqueues per-memory MemoryProcess jobs for the
	// "sync to my account" path so the synced seed copies get chunked
	// + embedded + classified the same way signup-time copies do.
	// Optional — when nil, sync still inserts the memories but recall
	// won't surface them until processing fires by some other path.
	// In production both paths use the same queue client wired in
	// serverapp/app.go, so this is only ever nil in tests.
	riverClient *queue.Client
}

func NewAdminSeedMemoriesHandler(s store.Store, riverClient *queue.Client) *AdminSeedMemoriesHandler {
	return &AdminSeedMemoriesHandler{store: s, riverClient: riverClient}
}

type adminSeedMemoryRow struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Summary     string         `json:"summary,omitempty"`
	Hint        string         `json:"hint,omitempty"`
	Tags        []string       `json:"tags"`
	State       string         `json:"state"`
	Kind        string         `json:"kind"`
	Stability   string         `json:"stability"`
	ContentType string         `json:"content_type"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func toAdminSeedRow(m *model.Memory) adminSeedMemoryRow {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return adminSeedMemoryRow{
		ID:          m.ID,
		Title:       m.Title,
		Content:     m.Content,
		Summary:     m.Summary,
		Hint:        m.Hint,
		Tags:        tags,
		State:       m.State,
		Kind:        m.Kind,
		Stability:   m.Stability,
		ContentType: m.ContentType,
		Metadata:    m.Metadata,
		CreatedAt:   m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   m.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// List returns every seed-template row in the system tutorial hub —
// active AND archived. Unlike `Store.ListSeedMemoryTemplates` (which
// the worker uses) this admin path returns archived rows too so the
// admin UI can present the full toggle surface.
func (h *AdminSeedMemoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	memos, err := h.store.ListSeedMemoryTemplatesAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", "failed to list seed templates")
		return
	}
	rows := make([]adminSeedMemoryRow, 0, len(memos))
	for i := range memos {
		rows = append(rows, toAdminSeedRow(&memos[i]))
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"seeds": rows}})
}

// Get returns one seed by id. Wraps GetMemoryForAdmin (which bypasses
// owner_id filtering) but enforces source_kind so an admin can't
// accidentally fetch arbitrary memories through this endpoint.
func (h *AdminSeedMemoriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id required")
		return
	}
	m, err := h.store.GetMemoryForAdmin(id)
	if err != nil || m == nil {
		writeError(w, http.StatusNotFound, "not_found", "seed not found")
		return
	}
	if !isSeedTemplate(m) {
		writeError(w, http.StatusNotFound, "not_found", "not a seed memory")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"seed": toAdminSeedRow(m)}})
}

// isSeedTemplate gates every read/write path through this handler on
// the same invariant: the row must live in the system tutorial hub,
// be owned by the system user, and be marked as an onboarding seed.
// Codex review (gpt-5.5) flagged that earlier paths checked hub +
// source_kind but not owner — a path-confusion bug or row drift could
// have let an admin Get/Update an arbitrary memory through these
// endpoints. Owner check closes that gap.
func isSeedTemplate(m *model.Memory) bool {
	return m != nil &&
		m.OwnerID == model.SystemUserID &&
		m.HubID == model.TutorialHubID &&
		m.SourceKind == model.MemorySourceKindOnboard
}

type adminSeedCreateRequest struct {
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Summary   string   `json:"summary,omitempty"`
	Hint      string   `json:"hint,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Kind      string   `json:"kind,omitempty"`      // defaults to "semantic"
	Stability string   `json:"stability,omitempty"` // defaults to "stable"
	State     string   `json:"state,omitempty"`     // "active" (default) or "archived"
}

// Create inserts a new onboarding-seed template into the system
// tutorial hub. The handler fills in every system-owned field
// (owner=SystemUserID, hub=TutorialHubID, source=system,
// source_kind=onboarding-seed, content_type=markdown, version=1,
// retrieval_weight=1.0) and computes content_hash from the body so
// the row obeys the same shape CreateMemory expects.
//
// Title + content are required (an empty seed isn't useful onboarding
// material); summary/hint/tags/kind/stability default to safe values.
// Invalid `kind` / `stability` values are rejected with 400 — silent
// coercion via the model normalizers would otherwise return a
// response showing the caller's typo while the DB stored the
// normalized value (codex review (gpt-5.5)).
//
// Plan 23 principle 4 (future-only edits) still applies: a newly
// created template only lands in users who sign up AFTER its
// creation. Existing users get it via the admin "Sync to my account"
// path (which deletes prior copies and re-runs the worker).
func (h *AdminSeedMemoriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	var req adminSeedCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse JSON")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "title is required")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "content is required")
		return
	}

	state := strings.TrimSpace(req.State)
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "archived" {
		writeError(w, http.StatusBadRequest, "invalid_state", "state must be active or archived")
		return
	}

	// kind / stability default when omitted; reject explicit invalid
	// values rather than silently coercing via model.NormalizeMemoryKind
	// (which would return the normalized value while the response
	// echoes the caller's typo — codex review).
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = model.MemoryKindSemantic
	} else if model.NormalizeMemoryKind(kind) != kind {
		writeError(w, http.StatusBadRequest, "invalid_kind",
			"kind must be one of episodic, semantic, procedural, rationale")
		return
	}
	stability := strings.TrimSpace(req.Stability)
	if stability == "" {
		stability = model.MemoryStabilityStable
	} else if model.NormalizeMemoryStability(stability) != stability {
		writeError(w, http.StatusBadRequest, "invalid_stability",
			"stability must be one of volatile, evolving, stable")
		return
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	now := time.Now().UTC()
	hashBytes := sha256.Sum256([]byte(req.Content))
	m := &model.Memory{
		ID:                       uuid.NewString(),
		HubID:                    model.TutorialHubID,
		OwnerID:                  model.SystemUserID,
		Title:                    title,
		Content:                  req.Content,
		ContentType:              "markdown",
		ContentHash:              hex.EncodeToString(hashBytes[:]),
		Summary:                  strings.TrimSpace(req.Summary),
		Hint:                     strings.TrimSpace(req.Hint),
		Kind:                     kind,
		Stability:                stability,
		RetrievalWeight:          1.0,
		Tags:                     tags,
		Boundary:                 "private",
		State:                    state,
		Source:                   model.MemorySourceSystem,
		SourceKind:               model.MemorySourceKindOnboard,
		Version:                  1,
		CreatedAt:                now,
		UpdatedAt:                now,
		AccessedAt:               now,
		ProvenanceCreatedByType:  model.MemoryCreatedByHuman,
		ProvenanceInitiationType: model.MemoryInitiationUnknown,
	}
	if err := h.store.CreateMemory(m); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"seed": toAdminSeedRow(m)}})
}

// Delete hard-removes one onboarding-seed template. Existing per-user
// copies (different owner_id) stay untouched (plan 23 principle 4) —
// the founder iterating on the curriculum doesn't disturb users who
// already received a previous version.
//
// 404 when the id doesn't match a seed template owned by the system
// user in the tutorial hub. The store-side WHERE clause enforces
// the same scoping as a defense-in-depth — admin paths must never
// be a vector for deleting arbitrary user memories even under
// path-confusion bugs.
func (h *AdminSeedMemoriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id required")
		return
	}
	deleted, err := h.store.DeleteSeedMemoryTemplate(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not_found", "seed not found")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"deleted": true}})
}

type adminSeedUpdateRequest struct {
	Title   *string  `json:"title,omitempty"`
	Content *string  `json:"content,omitempty"`
	Summary *string  `json:"summary,omitempty"`
	Hint    *string  `json:"hint,omitempty"`
	Tags    []string `json:"tags,omitempty"`  // nil = no change; empty array = clear tags
	State   *string  `json:"state,omitempty"` // "active" or "archived"
}

// Update mutates the seed template. Only future signups see the new
// content; existing user copies stay untouched (plan 23 principle 4).
//
// Pointer fields distinguish "field omitted from JSON" (no change)
// from "field present with value" (apply). Tags use a separate
// slice convention because empty array IS meaningful (clear all tags).
func (h *AdminSeedMemoriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id required")
		return
	}
	var req adminSeedUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse JSON")
		return
	}
	m, err := h.store.GetMemoryForAdmin(id)
	if err != nil || m == nil {
		writeError(w, http.StatusNotFound, "not_found", "seed not found")
		return
	}
	if !isSeedTemplate(m) {
		writeError(w, http.StatusNotFound, "not_found", "not a seed memory")
		return
	}

	if req.Title != nil {
		m.Title = strings.TrimSpace(*req.Title)
	}
	if req.Content != nil {
		m.Content = *req.Content
		// Recompute content_hash whenever content changes so the
		// template row stays internally consistent (chunks key off the
		// hash; codex review flagged that prior Update mutations would
		// drift hash from content).
		hashBytes := sha256.Sum256([]byte(*req.Content))
		m.ContentHash = hex.EncodeToString(hashBytes[:])
	}
	if req.Summary != nil {
		m.Summary = strings.TrimSpace(*req.Summary)
	}
	if req.Hint != nil {
		m.Hint = strings.TrimSpace(*req.Hint)
	}
	if req.Tags != nil {
		m.Tags = req.Tags
	}
	if req.State != nil {
		next := strings.TrimSpace(*req.State)
		if next != "active" && next != "archived" {
			writeError(w, http.StatusBadRequest, "invalid_state", "state must be active or archived")
			return
		}
		m.State = next
	}

	if err := h.store.UpdateMemory(m); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", "failed to update seed")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"seed": toAdminSeedRow(m)}})
}

// SyncToSelf wipes every onboarding-seed copy in the calling admin's
// own personal hub and re-runs the seed-copy logic synchronously so
// the admin can preview the live seed templates on their own account
// without making a fresh signup. Plan 23 principle 4 (future-only
// edits) still holds for OTHER users — only the caller's own copies
// are touched. Scoped to the calling admin only so an operator can't
// accidentally rewrite another user's onboarding.
//
// Bypasses the worker's 24h River unique-arg dedup window because the
// admin is invoking the copy explicitly (not via signup-side fanout).
//
// Returns `{deleted, copied}` so the UI can confirm both legs landed.
func (h *AdminSeedMemoriesHandler) SyncToSelf(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store not wired")
		return
	}
	userID := strings.TrimSpace(GetUserID(r))
	if userID == "" {
		// Defense-in-depth — AdminMiddleware chains after authMiddleware
		// which sets userIDKey. This branch should be unreachable in
		// production routing.
		writeError(w, http.StatusUnauthorized, "missing_user", "no authenticated user")
		return
	}

	deleted, err := h.store.DeleteSeedCopiesForOwner(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", "failed to clear existing seed copies")
		return
	}

	copied, err := queue.RunSeedCopy(r.Context(), h.store, h.riverClient, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "copy_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"deleted": deleted,
		"copied":  copied,
	}})
}
