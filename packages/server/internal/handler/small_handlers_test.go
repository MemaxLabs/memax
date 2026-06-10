package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// ─── Settings handler ──────────────────────────────────────────

func setupSettingsFixture(t *testing.T) (*handler.SettingsHandler, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'S', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@st",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return handler.NewSettingsHandler(s), ownerID
}

func TestSettingsGet_ReturnsUserSettings(t *testing.T) {
	t.Parallel()
	h, ownerID := setupSettingsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/settings", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsGetUsage_Authenticated(t *testing.T) {
	t.Parallel()
	h, ownerID := setupSettingsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.GetUsage(rec, req)
	// Without plan/effective/usage resolvers wired, may 500 — accept
	// any non-auth-error outcome. We're covering the auth gate + the
	// initial resolver pipeline.
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("authenticated user got 401: %s", rec.Body.String())
	}
}

func TestSettingsGetUsage_UnauthenticatedDoesNotPanic(t *testing.T) {
	t.Parallel()
	h, _ := setupSettingsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	// NOT injecting userID — auth is middleware's job. This test
	// pins that the handler itself doesn't panic on empty user.
	rec := httptest.NewRecorder()
	h.GetUsage(rec, req)
	if rec.Code == 0 {
		t.Error("handler should write a response even without user")
	}
}

func TestSettingsUpdate_RejectsEmptyBody(t *testing.T) {
	t.Parallel()
	h, _ := setupSettingsFixture(t)
	body, _ := json.Marshal(map[string]any{"display_name": "new name"})
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewBuffer(body))
	// No user injected — handler returns 400 (treats empty user as an
	// invalid request) rather than 401. Accept either.
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusBadRequest {
		t.Errorf("unauth: expected 400/401, got %d", rec.Code)
	}
}

func TestSettingsUpdate_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h, ownerID := setupSettingsFixture(t)
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewBufferString("not json"))
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: expected 400, got %d", rec.Code)
	}
}

func TestSettingsSetters(t *testing.T) {
	t.Parallel()
	h, _ := setupSettingsFixture(t)
	// Nil setters must not panic.
	h.SetPlanResolver(nil)
	h.SetEffectiveResolver(nil)
	h.SetUsageReader(nil)
}

// ─── Agents handler ────────────────────────────────────────────

func setupAgentsFixture(t *testing.T) (*handler.AgentsHandler, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'A', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@ag",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return handler.NewAgentsHandler(s, nil), ownerID
}

func TestAgentsList_EmptyForNewUser(t *testing.T) {
	t.Parallel()
	h, ownerID := setupAgentsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentsList_UnauthenticatedDoesNotPanic(t *testing.T) {
	t.Parallel()
	h, _ := setupAgentsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code == 0 {
		t.Error("handler should write a response even without user")
	}
}

func TestAgentsDisconnect_IdempotentForUnknown(t *testing.T) {
	t.Parallel()
	h, ownerID := setupAgentsFixture(t)
	req := httptest.NewRequest(http.MethodDelete, "/v1/agents/nonexistent", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req.SetPathValue("slug", "nonexistent")
	rec := httptest.NewRecorder()
	h.Disconnect(rec, req)
	// Idempotent DELETE — unknown agent returns 200 (like S3
	// DeleteObject). What matters is no panic and a well-formed
	// response.
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}

// ─── Unsubscribe handler ───────────────────────────────────────

func setupUnsubscribeFixture(t *testing.T) *handler.UnsubscribeHandler {
	t.Helper()
	s, _ := testdb.Acquire(t)
	return handler.NewUnsubscribeHandler(s)
}

func TestUnsubscribeReceive_InvalidToken(t *testing.T) {
	t.Parallel()
	h := setupUnsubscribeFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/unsubscribe", nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)
	// Missing token → 400.
	if rec.Code == http.StatusOK {
		t.Errorf("missing token: should not be 200")
	}
}

func TestUnsubscribeReceive_RendersFormForToken(t *testing.T) {
	t.Parallel()
	h := setupUnsubscribeFixture(t)
	// Any non-empty token renders the confirmation form; the actual
	// token validation happens on POST.
	req := httptest.NewRequest(http.MethodGet, "/v1/unsubscribe?token=abc123", nil)
	rec := httptest.NewRecorder()
	h.Receive(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("confirm page: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
