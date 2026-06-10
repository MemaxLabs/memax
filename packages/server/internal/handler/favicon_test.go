package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFaviconHandler_ServesSVG(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	FaviconHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("expected image/svg+xml, got %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "<svg") {
		t.Errorf("body did not start with <svg: %q", rec.Body.String()[:min(60, len(rec.Body.String()))])
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("expected immutable cache, got %q", cc)
	}
}

func TestFaviconHandler_NonEmpty(t *testing.T) {
	// Defensive: //go:embed silently embeds an empty byte slice when
	// the asset is missing. If someone deletes assets/favicon.svg,
	// this catches it before we ship a broken deploy.
	if len(faviconSVG) == 0 {
		t.Fatal("embedded favicon is empty — assets/favicon.svg may be missing")
	}
	if len(faviconSVG) > 64*1024 {
		t.Errorf("favicon size %d exceeds 64KiB — probably wrong file embedded", len(faviconSVG))
	}
}
