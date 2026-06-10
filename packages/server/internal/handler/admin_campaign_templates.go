package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminCampaignTemplatesHandler serves /v1/admin/campaign-templates*.
// Custom campaign templates are admin-authored, reusable email bodies
// picked at campaign authoring time. They are pure scaffolds — copied
// into a campaign's inline content on pick and never referenced at
// send time. See model.CampaignTemplate and migration 010.
type AdminCampaignTemplatesHandler struct {
	store store.Store
}

func NewAdminCampaignTemplatesHandler(s store.Store) *AdminCampaignTemplatesHandler {
	return &AdminCampaignTemplatesHandler{store: s}
}

// slugPattern MUST stay in sync with the campaign_templates_slug_chk
// CHECK constraint in migration 010. Keeping two sources of truth is
// unfortunate but unavoidable — the handler needs to reject invalid
// slugs with a useful error before hitting the DB, and the DB CHECK
// is the backstop against direct-SQL inserts.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

// bodyLimits: these aren't hard security boundaries — admins are
// authenticated and the bodies get copied into campaigns, which have
// their own send-time validation. But capping here protects against
// a paste-accident that would blow up the admin-client JSON payload
// and/or surprise us with a multi-MB row. Matches the spirit of the
// email_template_overrides guard in UpsertOverride.
const (
	maxCampaignTemplateNameLen        = 200
	maxCampaignTemplateDescriptionLen = 1000
	maxCampaignTemplateSubjectLen     = 500
	maxCampaignTemplateBodyLen        = 200_000 // ~200 KB per body
)

type adminCampaignTemplateCreateRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	BodyHTML    string `json:"body_html"`
	BodyText    string `json:"body_text"`
}

type adminCampaignTemplateUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	BodyHTML    string `json:"body_html"`
	BodyText    string `json:"body_text"`
}

type adminCampaignTemplateArchiveRequest struct {
	Archived bool `json:"archived"`
}

// List returns campaign templates. `?include_archived=true` surfaces
// archived rows for the management view; by default archived rows are
// hidden so pickers stay clean.
// GET /v1/admin/campaign-templates
func (h *AdminCampaignTemplatesHandler) List(w http.ResponseWriter, r *http.Request) {
	opts := model.CampaignTemplateListOpts{
		IncludeArchived: r.URL.Query().Get("include_archived") == "true",
	}
	templates, err := h.store.ListCampaignTemplates(r.Context(), opts)
	if err != nil {
		slog.Error("list campaign templates failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not list campaign templates.")
		return
	}
	if templates == nil {
		templates = []model.CampaignTemplate{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"templates": templates,
	}})
}

// Get returns a single campaign template by id.
// GET /v1/admin/campaign-templates/{id}
func (h *AdminCampaignTemplatesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tmpl, err := h.store.GetCampaignTemplate(r.Context(), id)
	if errors.Is(err, store.ErrCampaignTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign template not found.")
		return
	}
	if err != nil {
		slog.Error("get campaign template failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign template.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tmpl})
}

// Create inserts a new campaign template.
// POST /v1/admin/campaign-templates
func (h *AdminCampaignTemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req adminCampaignTemplateCreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	req.Slug = strings.TrimSpace(strings.ToLower(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	if req.Slug == "" || !slugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "invalid_slug",
			"Slug must be 2-64 chars, lowercase alphanumeric with _ or -.")
		return
	}
	if msg := h.validateContentFields(req.Name, req.Description, req.Subject, req.BodyHTML, req.BodyText); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_content", msg)
		return
	}

	tmpl := &model.CampaignTemplate{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Subject:     req.Subject,
		BodyHTML:    req.BodyHTML,
		BodyText:    req.BodyText,
	}
	if actorID != "" {
		tmpl.CreatedBy = &actorID
		tmpl.UpdatedBy = &actorID
	}
	if err := h.store.CreateCampaignTemplate(r.Context(), tmpl); err != nil {
		if errors.Is(err, store.ErrCampaignTemplateSlugTaken) {
			writeError(w, http.StatusConflict, "slug_taken", "A template with this slug already exists.")
			return
		}
		slog.Error("create campaign template failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not create campaign template.")
		return
	}
	trackRequest(r, "api.admin.campaign_templates.create", map[string]any{"slug": tmpl.Slug})
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: tmpl})
}

// Update mutates editable fields. Slug is NOT editable — renames are
// "create new + archive old" to preserve audit-trail stability.
// PATCH /v1/admin/campaign-templates/{id}
func (h *AdminCampaignTemplatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req adminCampaignTemplateUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if msg := h.validateContentFields(req.Name, req.Description, req.Subject, req.BodyHTML, req.BodyText); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_content", msg)
		return
	}
	patch := model.CampaignTemplateUpsert{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Subject:     req.Subject,
		BodyHTML:    req.BodyHTML,
		BodyText:    req.BodyText,
	}
	var updatedBy *string
	if actorID != "" {
		updatedBy = &actorID
	}
	tmpl, err := h.store.UpdateCampaignTemplate(r.Context(), id, patch, updatedBy)
	if errors.Is(err, store.ErrCampaignTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign template not found.")
		return
	}
	if err != nil {
		slog.Error("update campaign template failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not update campaign template.")
		return
	}
	trackRequest(r, "api.admin.campaign_templates.update", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tmpl})
}

// SetArchived toggles the archive flag. Both directions are idempotent
// (archiving an already-archived row is a no-op that returns 200).
// POST /v1/admin/campaign-templates/{id}/archive
func (h *AdminCampaignTemplatesHandler) SetArchived(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req adminCampaignTemplateArchiveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	var updatedBy *string
	if actorID != "" {
		updatedBy = &actorID
	}
	tmpl, err := h.store.SetCampaignTemplateArchived(r.Context(), id, req.Archived, updatedBy)
	if errors.Is(err, store.ErrCampaignTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign template not found.")
		return
	}
	if err != nil {
		slog.Error("set campaign template archived failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not archive campaign template.")
		return
	}
	trackRequest(r, "api.admin.campaign_templates.archive", map[string]any{"id": id, "archived": req.Archived})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: tmpl})
}

// Delete hard-deletes a template that's ALREADY archived. We force
// archive-first because custom campaign templates have no recovery
// path — an accidental click on delete would lose admin authoring
// work with no undo. The two-step flow (archive → delete) matches
// industry norms (Postmark, Customer.io) and means a fat-finger on
// an active row is a no-op toward data loss.
// DELETE /v1/admin/campaign-templates/{id}
func (h *AdminCampaignTemplatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := h.store.GetCampaignTemplate(r.Context(), id)
	if errors.Is(err, store.ErrCampaignTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign template not found.")
		return
	}
	if err != nil {
		slog.Error("load campaign template for delete failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign template.")
		return
	}
	if !existing.IsArchived {
		writeError(w, http.StatusConflict, "not_archived",
			"Archive the template before deleting. Deletion is permanent.")
		return
	}
	if err := h.store.DeleteCampaignTemplate(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrCampaignTemplateNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign template not found.")
			return
		}
		slog.Error("delete campaign template failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not delete campaign template.")
		return
	}
	trackRequest(r, "api.admin.campaign_templates.delete", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "deleted", "id": id}})
}

// validateContentFields enforces the shared name/length + full-document
// checks used by Create and Update. Returns an empty string on success,
// a human-readable error message otherwise.
func (h *AdminCampaignTemplatesHandler) validateContentFields(name, description, subject, bodyHTML, bodyText string) string {
	if name == "" {
		return "Name is required."
	}
	if len(name) > maxCampaignTemplateNameLen {
		return "Name is too long."
	}
	if len(description) > maxCampaignTemplateDescriptionLen {
		return "Description is too long."
	}
	if len(subject) > maxCampaignTemplateSubjectLen {
		return "Subject is too long."
	}
	if len(bodyHTML) > maxCampaignTemplateBodyLen {
		return "HTML body is too long."
	}
	if len(bodyText) > maxCampaignTemplateBodyLen {
		return "Plain-text body is too long."
	}
	// Reject full-document HTML — campaign send wraps the body in the
	// brand layout, same guard as UpsertOverride + Campaign create.
	if looksLikeEmailFullDocument(bodyHTML) {
		return "HTML body must be a body-only fragment — remove <!DOCTYPE>, <html>, <head>, and <body>."
	}
	return ""
}
