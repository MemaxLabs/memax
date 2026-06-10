package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestAdminEmailBrandGetReturnsDefaults(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewAdminHandler(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/email/brand", nil)
	req = withAdminIdentity(req, "admin-1")
	rec := httptest.NewRecorder()

	h.GetEmailBrand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.EmailBrandSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.ProductName != "memax" {
		t.Fatalf("expected default product_name memax, got %q", resp.Data.ProductName)
	}
	if resp.Data.PrimaryColor != "#111111" {
		t.Fatalf("expected default primary_color, got %q", resp.Data.PrimaryColor)
	}
}

func TestAdminEmailBrandUpdatePersistsPatchAndKeepsUntouchedFields(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewAdminHandler(s)

	body := `{"logo_url": "https://memax.app/brand/logo-email.png", "footer_html": "<p>new footer</p>"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/brand", bytes.NewBufferString(body))
	req = withAdminIdentity(req, "admin-1")
	rec := httptest.NewRecorder()

	h.UpdateEmailBrand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.EmailBrandSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.LogoURL != "https://memax.app/brand/logo-email.png" {
		t.Fatalf("logo_url not persisted: %q", resp.Data.LogoURL)
	}
	if resp.Data.FooterHTML != "<p>new footer</p>" {
		t.Fatalf("footer_html not persisted: %q", resp.Data.FooterHTML)
	}
	// Untouched fields keep their defaults.
	if resp.Data.PrimaryColor != "#111111" {
		t.Fatalf("primary_color should be unchanged, got %q", resp.Data.PrimaryColor)
	}
}

func TestAdminEmailBrandUpdateEmptyStringClearsField(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewAdminHandler(s)

	// First seed a logo URL.
	seed := `{"logo_url": "https://memax.app/brand/old.png"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/brand", bytes.NewBufferString(seed))
	req = withAdminIdentity(req, "admin-1")
	rec := httptest.NewRecorder()
	h.UpdateEmailBrand(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed failed: %d %s", rec.Code, rec.Body.String())
	}

	// Now explicitly clear it with "".
	clear := `{"logo_url": ""}`
	req = httptest.NewRequest(http.MethodPut, "/v1/admin/email/brand", bytes.NewBufferString(clear))
	req = withAdminIdentity(req, "admin-1")
	rec = httptest.NewRecorder()
	h.UpdateEmailBrand(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.EmailBrandSettings `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.LogoURL != "" {
		t.Fatalf("expected logo_url cleared, got %q", resp.Data.LogoURL)
	}
}

func TestAdminEmailBrandUpdateRejectsUnauthenticated(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewAdminHandler(s)

	body := `{"logo_url": "https://evil.example/x.png"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/brand", bytes.NewBufferString(body))
	// Intentionally no withAdminIdentity.
	rec := httptest.NewRecorder()

	h.UpdateEmailBrand(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminEmailBrandUpdateRejectsInvalidColor(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewAdminHandler(s)

	body := `{"primary_color": "red; }"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/brand", bytes.NewBufferString(body))
	req = withAdminIdentity(req, "admin-1")
	rec := httptest.NewRecorder()

	h.UpdateEmailBrand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_color") {
		t.Fatalf("expected invalid_color error code, got %s", rec.Body.String())
	}
}

func TestUpsertOverrideRejectsFullDocumentHTML(t *testing.T) {
	s := store.NewInMemoryStore()
	manager, err := email.NewTemplateManager(s, s)
	if err != nil {
		t.Fatalf("new template manager: %v", err)
	}

	fullDoc := "<!DOCTYPE html>\n<html>\n<body><h1>Hi</h1></body>\n</html>"
	err = manager.UpsertOverride(
		t.Context(),
		"hub_invite",
		"Subject",
		fullDoc,
		"body",
		"",
		"source",
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected full-document HTML to be rejected")
	}
	if !strings.Contains(err.Error(), "body-only fragment") {
		t.Fatalf("expected error mentioning body-only fragment, got: %v", err)
	}
}

func TestAdminEmailTemplateUpdateRejectsFullDocumentHTMLWith400(t *testing.T) {
	h, _ := newAdminEmailTemplatesHandler(t)

	body := `{
		"subject": "Subject",
		"html": "<!DOCTYPE html>\n<html><body>hi</body></html>",
		"text": "hi"
	}`
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/email/templates/hub_invite", bytes.NewBufferString(body))
	req = withAdminIdentity(req, "admin-1")
	req.SetPathValue("name", "hub_invite")
	rec := httptest.NewRecorder()

	h.UpdateEmailTemplate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for full-document override, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_template_body") {
		t.Fatalf("expected invalid_template_body code, got %s", rec.Body.String())
	}
}

func TestTemplateManagerRendersBaseLayoutWithBrand(t *testing.T) {
	s := store.NewInMemoryStore()
	logo := "https://memax.app/brand/logo-email.png"
	footer := "<p>brand footer</p>"
	if _, err := s.UpdateEmailBrandSettings(t.Context(), model.EmailBrandSettingsPatch{
		LogoURL:    &logo,
		FooterHTML: &footer,
	}, nil); err != nil {
		t.Fatalf("seed brand: %v", err)
	}

	manager, err := email.NewTemplateManager(s, s)
	if err != nil {
		t.Fatalf("new template manager: %v", err)
	}

	rendered, err := manager.Render(t.Context(), "hub_invite", map[string]string{
		"InviterName": "Avery",
		"HubName":     "Design ops",
		"Role":        "contributor",
		"InviteURL":   "https://memax.app/invites/abc",
		"ExpiresAt":   "May 1, 2026",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Base layout chrome must be present.
	if !strings.Contains(rendered.HTML, "<!DOCTYPE html>") {
		t.Errorf("expected doctype from base layout, got:\n%s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, logo) {
		t.Errorf("expected logo URL in rendered HTML, got:\n%s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "brand footer") {
		t.Errorf("expected brand footer HTML, got:\n%s", rendered.HTML)
	}
	// Body fragment must be inlined.
	if !strings.Contains(rendered.HTML, "Avery") || !strings.Contains(rendered.HTML, "Design ops") {
		t.Errorf("expected body variables in rendered HTML, got:\n%s", rendered.HTML)
	}
	// Text footer is appended by base.txt.
	if !strings.Contains(rendered.Text, "memax — memory for your AI agents") {
		t.Errorf("expected default footer text in rendered text, got:\n%s", rendered.Text)
	}
}
