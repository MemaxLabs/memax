package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type adminEmailTemplateUpdateRequest struct {
	Subject     string         `json:"subject"`
	HTML        string         `json:"html"`
	Text        string         `json:"text"`
	Notes       string         `json:"notes"`
	EditorKind  string         `json:"editor_kind"`
	EditorState map[string]any `json:"editor_state"`
}

type adminEmailTemplatePreviewRequest struct {
	Subject    string            `json:"subject"`
	HTML       string            `json:"html"`
	Text       string            `json:"text"`
	SampleData map[string]string `json:"sample_data"`
}

type adminEmailTemplateSendRequest struct {
	To         string            `json:"to"`
	Subject    string            `json:"subject"`
	HTML       string            `json:"html"`
	Text       string            `json:"text"`
	SampleData map[string]string `json:"sample_data"`
}

// ListEmailTemplates returns the admin catalog of editable email templates.
// GET /v1/admin/email/templates
func (h *AdminHandler) ListEmailTemplates(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}

	templates, err := h.templateManager.List(r.Context())
	if err != nil {
		slog.Error("failed to list email templates", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load email templates.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"templates": templates,
	}})
}

// GetEmailTemplate returns one editable email template detail payload.
// GET /v1/admin/email/templates/{name}
func (h *AdminHandler) GetEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}

	name := r.PathValue("name")
	detail, err := h.templateManager.Get(r.Context(), name)
	if err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// PreviewEmailTemplate renders a draft template with sample data.
// POST /v1/admin/email/templates/{name}/preview
func (h *AdminHandler) PreviewEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}
	var req adminEmailTemplatePreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	name := r.PathValue("name")
	preview, err := h.templateManager.Preview(r.Context(), name, req.Subject, req.HTML, req.Text, req.SampleData)
	if err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: preview})
}

// UpdateEmailTemplate upserts an override for one template.
// PUT /v1/admin/email/templates/{name}
func (h *AdminHandler) UpdateEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}
	var req adminEmailTemplateUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.HTML) == "" || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "Subject, HTML, and plain text are required.")
		return
	}

	name := r.PathValue("name")
	adminID := GetUserID(r)
	if adminID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	if err := h.templateManager.UpsertOverride(r.Context(), name, req.Subject, req.HTML, req.Text, req.Notes, req.EditorKind, req.EditorState, &adminID); err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}

	detail, err := h.templateManager.Get(r.Context(), name)
	if err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}

	trackRequest(r, "api.admin.email_templates.update", map[string]any{"name": name})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// ResetEmailTemplate deletes the override and falls back to the built-in template.
// DELETE /v1/admin/email/templates/{name}
func (h *AdminHandler) ResetEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}

	name := r.PathValue("name")
	adminID := GetUserID(r)
	if adminID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	if err := h.templateManager.DeleteOverride(r.Context(), name, &adminID); err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	trackRequest(r, "api.admin.email_templates.reset", map[string]any{"name": name})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status": "reset",
		"name":   name,
	}})
}

// PublishEmailTemplate promotes the current draft to published.
// POST /v1/admin/email/templates/{name}/publish
//
// This is the test-vs-prod gate: until the admin calls this endpoint,
// real sends continue using the previously-published content (or the
// file default for never-published templates). `Save` writes to draft
// only — `Publish` is the explicit promotion.
func (h *AdminHandler) PublishEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email template management is unavailable.")
		return
	}
	name := r.PathValue("name")
	adminID := GetUserID(r)
	if adminID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	if _, err := h.templateManager.PublishOverride(r.Context(), name, &adminID); err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	detail, err := h.templateManager.Get(r.Context(), name)
	if err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	trackRequest(r, "api.admin.email_templates.publish", map[string]any{"name": name})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// SendEmailTemplate renders a draft template with variables and enqueues a real email send.
// POST /v1/admin/email/templates/{name}/send
func (h *AdminHandler) SendEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templateManager == nil || h.enqueueRenderedEmail == nil {
		writeError(w, http.StatusServiceUnavailable, "email_templates_unavailable", "Email sending is unavailable.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}
	var req adminEmailTemplateSendRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	if strings.TrimSpace(req.To) == "" || !strings.Contains(req.To, "@") {
		writeError(w, http.StatusBadRequest, "invalid_email", "A valid recipient email is required.")
		return
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.HTML) == "" || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "Subject, HTML, and plain text are required.")
		return
	}

	name := r.PathValue("name")
	preview, err := h.templateManager.Preview(r.Context(), name, req.Subject, req.HTML, req.Text, req.SampleData)
	if err != nil {
		h.writeEmailTemplateError(w, err)
		return
	}
	if err := h.enqueueRenderedEmail(
		req.To,
		preview.Subject,
		preview.HTML,
		preview.Text,
		map[string]string{"template": name, "source": "admin_email"},
	); err != nil {
		slog.Error("failed to enqueue admin email send", "template", name, "to", req.To, "error", err)
		writeError(w, http.StatusInternalServerError, "enqueue_failed", "Failed to enqueue email send.")
		return
	}

	trackRequest(r, "api.admin.email_templates.send", map[string]any{"name": name, "to": req.To})
	writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: model.AdminEmailTemplateSendResult{
		Status:   "queued",
		Name:     name,
		To:       req.To,
		Subject:  preview.Subject,
		QueuedAt: time.Now().UTC().Format(time.RFC3339),
	}})
}

func (h *AdminHandler) writeEmailTemplateError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, email.ErrOverrideLooksLikeFullDocument):
		writeError(w, http.StatusBadRequest, "invalid_template_body", msg)
	case errors.Is(err, store.ErrEmailTemplateNotFound):
		// Admin hit Publish on a template that has no draft row yet.
		// Frontend should render: "save a draft first, then publish."
		writeError(w, http.StatusNotFound, "no_draft", "Save a draft before publishing.")
	case strings.Contains(msg, "template not found"):
		writeError(w, http.StatusNotFound, "not_found", "Email template not found.")
	case strings.Contains(msg, "missingkey"):
		writeError(w, http.StatusBadRequest, "invalid_variables", "The preview data is missing a required variable.")
	case strings.Contains(msg, "parse ") || strings.Contains(msg, "render "):
		writeError(w, http.StatusBadRequest, "invalid_template", msg)
	default:
		slog.Error("email template operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Email template operation failed.")
	}
}
