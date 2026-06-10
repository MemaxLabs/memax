package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- isHTMLContentType --------------------------------------------------

func TestIsHTMLContentType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"html", true},
		{"HTML", true},
		{"text/html", true},
		{"TEXT/HTML", true},
		{"text/html; charset=utf-8", true},
		{"application/xhtml+xml", true},
		{"markdown", false},
		{"text/markdown", false},
		{"text", false},
		{"code", false},
		{"application/pdf", false},
		{"", false},
		{" ", false},
	}
	for _, tt := range cases {
		if got := isHTMLContentType(tt.in); got != tt.want {
			t.Errorf("isHTMLContentType(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// --- sanitizePushRequest: content sanitized when content_type is HTML --

func TestSanitizePushRequest_StripsXSSFromHTMLContent(t *testing.T) {
	req := &model.PushRequest{
		ContentType: "html",
		Content:     `<p>Hello</p><script>alert(1)</script><img src=x onerror="bad()">`,
		Title:       "a title",
	}
	sanitizePushRequest(req)

	if req.Content == "" {
		t.Fatal("sanitizer should preserve safe content, not wipe it")
	}
	for _, bad := range []string{"<script", "alert(1)", "onerror"} {
		if strings.Contains(req.Content, bad) {
			t.Errorf("sanitized content still contains %q: %s", bad, req.Content)
		}
	}
	if !strings.Contains(req.Content, "<p>Hello</p>") {
		t.Errorf("safe paragraph should be preserved, got %q", req.Content)
	}
}

func TestSanitizePushRequest_LeavesMarkdownContentAlone(t *testing.T) {
	// Markdown is rendered by ReactMarkdown (which escapes HTML), so
	// we must NOT run the HTML sanitizer on markdown — it would mangle
	// angle-bracket literals and break intentional code formatting.
	// Only content_type == html triggers sanitization.
	raw := "```go\nfmt.Println(\"<script>\")\n```\n\n<script>not a real script in markdown</script>"
	req := &model.PushRequest{ContentType: "markdown", Content: raw}
	sanitizePushRequest(req)
	if req.Content != raw {
		t.Errorf("markdown content must not be sanitized, got %q", req.Content)
	}
}

// --- sanitizePushRequest: title + hint always stripped -----------------

func TestSanitizePushRequest_StripsHTMLFromTitleAndHint(t *testing.T) {
	req := &model.PushRequest{
		ContentType: "text",
		Title:       `<b>bold</b><script>alert(1)</script>tail`,
		Hint:        `<img src=x onerror="alert(1)">hint`,
	}
	sanitizePushRequest(req)
	if req.Title != "boldtail" {
		t.Errorf("title should have tags stripped, got %q", req.Title)
	}
	if req.Hint != "hint" {
		t.Errorf("hint should have tags stripped, got %q", req.Hint)
	}
}

func TestSanitizePushRequest_Idempotent(t *testing.T) {
	// Running twice yields the same result — important for Update which
	// sanitizes a request whose content/title may already have been
	// sanitized at Create time and round-tripped through the API.
	req := &model.PushRequest{
		ContentType: "html",
		Title:       `<b>t</b>`,
		Content:     `<p>c</p><script>x</script>`,
	}
	sanitizePushRequest(req)
	first := *req
	sanitizePushRequest(req)
	if req.Title != first.Title || req.Content != first.Content {
		t.Errorf("sanitization not idempotent:\n  first=%+v\n  after=%+v", first, *req)
	}
}

func TestSanitizePushRequest_NilSafe(t *testing.T) {
	sanitizePushRequest(nil) // must not panic
}

// --- sanitizeMemory: MCP-shaped ingest ---------------------------------

func TestSanitizeMemory_StripsXSSFromHTMLContent(t *testing.T) {
	// Mirrors the REST test but through the MCP sanitization entry
	// point — both routes share sanitizeMemoryFields underneath, so
	// this pins that the MCP intake sees the same policy. Regression
	// for the Codex finding where mcp.go skipped sanitization entirely.
	m := &model.Memory{
		ContentType: "html",
		Title:       `<b>t</b><script>alert(1)</script>`,
		Hint:        `<img src=x onerror="x()">hint`,
		Content:     `<p>hi</p><script>alert(1)</script>`,
	}
	sanitizeMemory(m)
	if strings.Contains(m.Content, "<script") || strings.Contains(m.Content, "alert(") {
		t.Errorf("content XSS not stripped: %q", m.Content)
	}
	if m.Title != "t" {
		t.Errorf("title tags should be stripped, got %q", m.Title)
	}
	if m.Hint != "hint" {
		t.Errorf("hint tags should be stripped, got %q", m.Hint)
	}
}

func TestSanitizeMemory_NilSafe(t *testing.T) {
	sanitizeMemory(nil) // must not panic
}

func TestUpdate_RelabelToHTMLSanitizesExistingBody(t *testing.T) {
	// Regression for the Codex round-2 finding: a client could create
	// a memory labeled "text" with raw HTML in the body, then PATCH
	// only content_type=html. Before this fix, Update wrote the new
	// label without re-sanitizing, turning stored XSS into
	// HTML-rendered XSS. Now the relabel-to-html path sanitizes the
	// existing body in place.
	s := store.NewInMemoryStore()
	existing := &model.Memory{
		ID:          "mem-relabel",
		OwnerID:     "u1",
		Title:       "existing",
		Content:     `<p>hi</p><script>alert("pwn")</script>`,
		ContentType: "text",
		ContentHash: "old-hash",
		Version:     1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.CreateMemory(existing); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPatch, "/v1/memories/mem-relabel",
		bytes.NewBufferString(`{"content_type":"html"}`))
	req.SetPathValue("id", "mem-relabel")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := s.GetMemory("mem-relabel", "u1")
	if err != nil {
		t.Fatalf("reread memory: %v", err)
	}
	if after.ContentType != "html" {
		t.Errorf("expected ContentType=html after relabel, got %q", after.ContentType)
	}
	if strings.Contains(after.Content, "<script") || strings.Contains(after.Content, "alert(") {
		t.Errorf("existing body must be sanitized on relabel to html, got %q", after.Content)
	}
	if !strings.Contains(after.Content, "<p>hi</p>") {
		t.Errorf("safe content should still be present, got %q", after.Content)
	}
}

func TestSanitizeMemory_MarkdownContentLeftAlone(t *testing.T) {
	// Same markdown-preservation guarantee as the REST path. MCP
	// pushes are content_type=markdown by default, so this is the
	// hot path for that route.
	raw := "```\n<b>not actually bold</b>\n```"
	m := &model.Memory{ContentType: "markdown", Content: raw}
	sanitizeMemory(m)
	if m.Content != raw {
		t.Errorf("markdown content must not be modified, got %q", m.Content)
	}
}

func TestSanitizePushRequest_EmptyFieldsNoOp(t *testing.T) {
	req := &model.PushRequest{}
	sanitizePushRequest(req)
	if req.Title != "" || req.Hint != "" || req.Content != "" {
		t.Errorf("empty fields should stay empty, got %+v", *req)
	}
}
