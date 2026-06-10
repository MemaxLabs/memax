package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func newAdminEmailTemplatesHandler(t *testing.T) (*AdminHandler, *email.TemplateManager) {
	t.Helper()

	s := store.NewInMemoryStore()
	manager, err := email.NewTemplateManager(s, s)
	if err != nil {
		t.Fatalf("new template manager: %v", err)
	}

	h := NewAdminHandler(s)
	h.SetTemplateManager(manager)
	return h, manager
}

func TestAdminEmailTemplateUpdatePersistsVisualEditorState(t *testing.T) {
	h, _ := newAdminEmailTemplatesHandler(t)

	body := `{
		"subject": "{{.InviterName}} invited you",
		"html": "<p>Hello {{.InviterName}}</p>",
		"text": "Hello {{.InviterName}}",
		"notes": "Visual version",
		"editor_kind": "react_email_editor",
		"editor_state": {
			"document": {
				"type": "doc",
				"content": [
					{"type": "paragraph", "content": [{"type": "text", "text": "Hello"}]}
				]
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/templates/hub_invite", bytes.NewBufferString(body))
	req = withAdminIdentity(req, "admin-1")
	req.SetPathValue("name", "hub_invite")
	rec := httptest.NewRecorder()

	h.UpdateEmailTemplate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, _ := json.Marshal(resp.Data)

	var detail model.AdminEmailTemplateDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	if detail.Override == nil {
		t.Fatal("expected override in response")
	}
	if detail.Override.EditorKind != "react_email_editor" {
		t.Fatalf("expected editor kind react_email_editor, got %q", detail.Override.EditorKind)
	}
	document, ok := detail.Override.EditorState["document"].(map[string]any)
	if !ok || document["type"] != "doc" {
		t.Fatalf("expected saved editor document, got %#v", detail.Override.EditorState)
	}
}

func TestAdminEmailTemplateResetTracksResetActor(t *testing.T) {
	h, manager := newAdminEmailTemplatesHandler(t)

	updateBody := `{
		"subject": "{{.InviterName}} invited you",
		"html": "<p>Hello {{.InviterName}}</p>",
		"text": "Hello {{.InviterName}}",
		"notes": "Saved before reset",
		"editor_kind": "source"
	}`

	updateReq := httptest.NewRequest(http.MethodPut, "/v1/admin/email/templates/hub_invite", bytes.NewBufferString(updateBody))
	updateReq = withAdminIdentity(updateReq, "admin-save")
	updateReq.SetPathValue("name", "hub_invite")
	updateRec := httptest.NewRecorder()
	h.UpdateEmailTemplate(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}

	resetReq := httptest.NewRequest(http.MethodDelete, "/v1/admin/email/templates/hub_invite", nil)
	resetReq = withAdminIdentity(resetReq, "admin-reset")
	resetReq.SetPathValue("name", "hub_invite")
	resetRec := httptest.NewRecorder()
	h.ResetEmailTemplate(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset expected 200, got %d: %s", resetRec.Code, resetRec.Body.String())
	}

	detail, err := manager.Get(context.Background(), "hub_invite")
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.Revisions) < 2 {
		t.Fatalf("expected at least 2 revisions, got %d", len(detail.Revisions))
	}

	resetRevision := detail.Revisions[0]
	if resetRevision.Action != "reset" {
		t.Fatalf("expected latest revision to be reset, got %q", resetRevision.Action)
	}
	if resetRevision.CreatedBy == nil || *resetRevision.CreatedBy != "admin-reset" {
		t.Fatalf("expected reset revision actor admin-reset, got %#v", resetRevision.CreatedBy)
	}
}

func TestAdminEmailTemplateSendQueuesRenderedEmail(t *testing.T) {
	h, _ := newAdminEmailTemplatesHandler(t)

	var queued struct {
		to      string
		subject string
		html    string
		text    string
		tags    map[string]string
	}
	h.SetEnqueueRenderedEmail(func(to, subject, html, text string, tags map[string]string) error {
		queued.to = to
		queued.subject = subject
		queued.html = html
		queued.text = text
		queued.tags = tags
		return nil
	})

	body := `{
		"to": "tester@example.com",
		"subject": "{{.InviterName}} invited you to {{.HubName}}",
		"html": "<html><body><p>Hello {{.InviterName}}</p><a href=\"{{.InviteURL}}\">Join</a></body></html>",
		"text": "Hello {{.InviterName}}\n{{.InviteURL}}",
		"sample_data": {
			"InviterName": "Avery",
			"HubName": "Design ops",
			"InviteURL": "https://memax.app/invites/invite_123",
			"Role": "viewer",
			"ExpiresAt": "May 1, 2026"
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/email/templates/hub_invite/send", bytes.NewBufferString(body))
	req = withAdminIdentity(req, "admin-send")
	req.SetPathValue("name", "hub_invite")
	rec := httptest.NewRecorder()

	h.SendEmailTemplate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if queued.to != "tester@example.com" {
		t.Fatalf("expected recipient tester@example.com, got %q", queued.to)
	}
	if !strings.Contains(queued.subject, "Avery invited you") {
		t.Fatalf("expected rendered subject, got %q", queued.subject)
	}
	if !strings.Contains(queued.html, "https://memax.app/invites/invite_123") {
		t.Fatalf("expected rendered html to contain invite url, got %q", queued.html)
	}
	if queued.tags["template"] != "hub_invite" {
		t.Fatalf("expected template tag hub_invite, got %#v", queued.tags)
	}
}
