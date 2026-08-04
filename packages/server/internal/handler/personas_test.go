package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func upsertConfigAs(t *testing.T, h *ConfigsHandler, owner, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/configs", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, owner))
	rec := httptest.NewRecorder()
	h.Upsert(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("upsert failed: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIdentityConfigUpsertDerivesPersona(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw",
		"file_path":"SOUL.md",
		"scope":"global",
		"content":"# Soul\nWarm, direct, a little playful."
	}`)
	// Non-identity config must NOT create a persona.
	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw",
		"file_path":"AGENTS.md",
		"scope":"global",
		"content":"# Agents\nProject conventions."
	}`)

	personas, err := s.ListPersonas("owner-a")
	if err != nil {
		t.Fatalf("ListPersonas: %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}
	if personas[0].Name != "openclaw" || personas[0].SourceFilePath != "SOUL.md" {
		t.Fatalf("unexpected persona: %#v", personas[0])
	}

	// Profile-scoped identity file derives a persona named after the profile.
	upsertConfigAs(t, h, "owner-a", `{
		"agent":"hermes",
		"file_path":"SOUL.md",
		"scope":"profile:work",
		"content":"# Soul\nFocused and formal."
	}`)
	personas, _ = s.ListPersonas("owner-a")
	if len(personas) != 2 {
		t.Fatalf("expected 2 personas, got %d", len(personas))
	}
	var found bool
	for _, p := range personas {
		if p.Name == "work" && p.SourceScope == "profile:work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("profile persona not derived: %#v", personas)
	}
}

func TestApplyPersonaWritesTargetIdentityConfig(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw",
		"file_path":"SOUL.md",
		"scope":"global",
		"content":"# Soul\nWarm and playful."
	}`)
	personas, _ := s.ListPersonas("owner-a")
	if len(personas) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(personas))
	}

	apply := func(owner, personaID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/personas/"+personaID+"/apply",
			bytes.NewBufferString(body))
		req.SetPathValue("id", personaID)
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, owner))
		rec := httptest.NewRecorder()
		h.ApplyPersona(rec, req)
		return rec
	}

	// Owner isolation: another user cannot apply someone else's persona.
	if rec := apply("owner-b", personas[0].ID, `{"target_agent":"hermes"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign owner, got %d", rec.Code)
	}

	// Invalid scope rejected.
	if rec := apply("owner-a", personas[0].ID, `{"target_agent":"hermes","target_scope":"profile:../evil"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsafe scope, got %d", rec.Code)
	}
	if rec := apply("owner-a", personas[0].ID, `{"target_agent":"hermes","target_scope":"project:github.com/x/y"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for project scope, got %d", rec.Code)
	}

	// Happy path: apply to a hermes profile.
	rec := apply("owner-a", personas[0].ID, `{"target_agent":"hermes","target_scope":"profile:work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply failed: %d: %s", rec.Code, rec.Body.String())
	}

	cfg, err := s.GetAgentConfigByPath("hermes", "SOUL.md", "profile:work", "owner-a")
	if err != nil {
		t.Fatalf("target config not written: %v", err)
	}
	if cfg.Content != "# Soul\nWarm and playful." {
		t.Fatalf("unexpected target content: %q", cfg.Content)
	}

	// The applied slot is itself an identity config → persona derived for it.
	personas, _ = s.ListPersonas("owner-a")
	if len(personas) != 2 {
		t.Fatalf("expected 2 personas after apply, got %d", len(personas))
	}
}

func TestPersonaRevisionsLifecycle(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw","file_path":"SOUL.md","scope":"global",
		"content":"# Soul v1"
	}`)
	// Same content again — must NOT bump the persona or add a revision.
	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw","file_path":"SOUL.md","scope":"global",
		"content":"# Soul v1"
	}`)
	upsertConfigAs(t, h, "owner-a", `{
		"agent":"openclaw","file_path":"SOUL.md","scope":"global",
		"content":"# Soul v2"
	}`)

	personas, _ := s.ListPersonas("owner-a")
	if len(personas) != 1 || personas[0].Version != 2 {
		t.Fatalf("expected persona at v2, got %#v", personas)
	}
	revs, err := s.ListPersonaRevisions(personas[0].ID, "owner-a")
	if err != nil {
		t.Fatalf("ListPersonaRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revs))
	}
	if revs[0].Content != "" {
		t.Fatalf("list must omit content, got %q", revs[0].Content)
	}

	// Restore v1 → source config rewritten → persona re-derived at v3.
	req := httptest.NewRequest(http.MethodPost,
		"/v1/personas/"+personas[0].ID+"/revisions/1/restore", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", personas[0].ID)
	req.SetPathValue("version", "1")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()
	h.RestorePersonaRevision(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore failed: %d: %s", rec.Code, rec.Body.String())
	}

	cfg, err := s.GetAgentConfigByPath("openclaw", "SOUL.md", "global", "owner-a")
	if err != nil || cfg.Content != "# Soul v1" {
		t.Fatalf("source config not restored: %v %#v", err, cfg)
	}
	personas, _ = s.ListPersonas("owner-a")
	if personas[0].Version != 3 {
		t.Fatalf("restore must create a new head version, got v%d", personas[0].Version)
	}
	// Foreign owner cannot read revisions.
	if _, err := s.GetPersonaRevision(personas[0].ID, 1, "owner-b"); err == nil {
		t.Fatalf("expected owner isolation on revisions")
	}
}
