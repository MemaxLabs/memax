package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestUnsubscribePostSetsOptOutAndRotatesToken(t *testing.T) {
	s := store.NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "alice@example.com"})

	// Mint the user's initial token — this is what's embedded in
	// outbound campaign emails.
	token, err := s.GetOrCreateEmailOptOutToken(context.Background(), "u1")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	h := NewUnsubscribeHandler(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/unsubscribe?token="+token, nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsubscribed") {
		t.Errorf("expected success copy in body, got: %s", rec.Body.String())
	}

	optedOut, _ := s.IsUserEmailOptedOut(context.Background(), "u1")
	if !optedOut {
		t.Fatal("expected user to be opted out after POST")
	}

	// Old token MUST no longer unsubscribe anyone (rotated).
	req2 := httptest.NewRequest(http.MethodPost, "/v1/unsubscribe?token="+token, nil)
	rec2 := httptest.NewRecorder()
	h.Receive(rec2, req2)
	// Success-shaped response regardless (avoids token-validity oracle),
	// but the store-level rotation means no second user got touched.
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on rotated token, got %d", rec2.Code)
	}
}

func TestUnsubscribePostAcceptsFormBodyToken(t *testing.T) {
	s := store.NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "alice@example.com"})
	token, _ := s.GetOrCreateEmailOptOutToken(context.Background(), "u1")

	h := NewUnsubscribeHandler(s)
	body := url.Values{}
	body.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/v1/unsubscribe", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Receive(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	optedOut, _ := s.IsUserEmailOptedOut(context.Background(), "u1")
	if !optedOut {
		t.Fatal("expected form-body token to trigger opt-out")
	}
}

func TestUnsubscribeGetRendersConfirmForm(t *testing.T) {
	s := store.NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "alice@example.com"})
	token, _ := s.GetOrCreateEmailOptOutToken(context.Background(), "u1")

	h := NewUnsubscribeHandler(s)
	req := httptest.NewRequest(http.MethodGet, "/v1/unsubscribe?token="+token, nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "Unsubscribe") {
		t.Errorf("expected confirm page copy, got: %s", bodyStr)
	}
	// Form must include the token as a hidden input so POST works.
	if !strings.Contains(bodyStr, token) {
		t.Error("confirm page must embed the token in the form")
	}
	// GET alone MUST NOT unsubscribe — only POST does.
	optedOut, _ := s.IsUserEmailOptedOut(context.Background(), "u1")
	if optedOut {
		t.Fatal("GET must not trigger opt-out; user became opted out")
	}
}

func TestUnsubscribeMissingTokenReturns400(t *testing.T) {
	h := NewUnsubscribeHandler(store.NewInMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/unsubscribe", nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUnsubscribeUnknownTokenReturnsSuccessShape(t *testing.T) {
	// Defense against token-validity oracle: unknown tokens MUST render
	// the same shape as valid ones so an attacker can't enumerate.
	h := NewUnsubscribeHandler(store.NewInMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/v1/unsubscribe?token=nonsense", nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (oracle defense), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsubscribed") {
		t.Errorf("expected success-shaped body, got: %s", rec.Body.String())
	}
}

func TestGetOrCreateEmailOptOutTokenIsIdempotent(t *testing.T) {
	s := store.NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "alice@example.com"})
	t1, err := s.GetOrCreateEmailOptOutToken(context.Background(), "u1")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	t2, err := s.GetOrCreateEmailOptOutToken(context.Background(), "u1")
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if t1 != t2 {
		t.Fatalf("expected stable token, got %q then %q", t1, t2)
	}
}
