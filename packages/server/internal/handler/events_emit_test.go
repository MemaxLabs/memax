package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// eventsCapture is a test-only events.Publisher implementation that
// records every event it sees. Handler-level regression tests use it
// to assert that emit sites fire on their success branch and stay
// silent on their error branch. Matches the contract of the
// capturePublisher in events/helpers_test.go but is duplicated here
// because test helpers don't cross package boundaries in Go.
type eventsCapture struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *eventsCapture) Publish(_ context.Context, evt events.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *eventsCapture) Close() error { return nil }

func (p *eventsCapture) snapshot() []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]events.Event, len(p.events))
	copy(cp, p.events)
	return cp
}

func (p *eventsCapture) byType(t string) []events.Event {
	var out []events.Event
	for _, e := range p.snapshot() {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// ─── Configs emit sites (PR H.4) ─────────────────────────────────

func TestConfigsUpsertEmitsAgentChanged(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	pub := &eventsCapture{}
	h := NewConfigsHandler(s, nil).WithEvents(pub)

	body := `{"agent":"claude-code","file_path":"/cfg.md","scope":"global","content":"hello"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/configs", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Upsert(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	matches := pub.byType(events.EventTypeAgentChanged)
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 agent.changed event, got %d", len(matches))
	}
	evt := matches[0]
	if evt.Audience != "user" {
		t.Errorf("want user-axis routing, got audience=%q", evt.Audience)
	}
	if evt.RecipientUserID != "u1" {
		t.Errorf("want recipient u1, got %q", evt.RecipientUserID)
	}
	if evt.EntityID != "claude-code" {
		t.Errorf("want entity_id=claude-code (agent slug), got %q", evt.EntityID)
	}
	if evt.State != "upserted" {
		t.Errorf("want state=upserted, got %q", evt.State)
	}
}

func TestConfigsBatchDeleteDedupsByAgent(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	pub := &eventsCapture{}
	h := NewConfigsHandler(s, nil).WithEvents(pub)

	// Seed 5 configs split across 2 agents. Delete all 5.
	// Expected: 2 agent.changed events — one per unique agent.
	makeAgentConfig(t, s, "c1", "u1", "claude-code", "/a.md", 1)
	makeAgentConfig(t, s, "c2", "u1", "claude-code", "/b.md", 1)
	makeAgentConfig(t, s, "c3", "u1", "claude-code", "/c.md", 1)
	makeAgentConfig(t, s, "c4", "u1", "codex", "/x.md", 1)
	makeAgentConfig(t, s, "c5", "u1", "codex", "/y.md", 1)

	result := callBatchDelete(t, h, "u1", []string{"c1", "c2", "c3", "c4", "c5"})
	if result.Deleted != 5 {
		t.Fatalf("expected 5 deletes, got %d", result.Deleted)
	}

	matches := pub.byType(events.EventTypeAgentChanged)
	if len(matches) != 2 {
		t.Fatalf("expected 2 agent.changed (one per unique agent), got %d", len(matches))
	}
	agents := map[string]bool{}
	for _, e := range matches {
		agents[e.EntityID] = true
	}
	if !agents["claude-code"] || !agents["codex"] {
		t.Errorf("expected both claude-code and codex, got %v", agents)
	}
}

func TestConfigsBatchDeleteEmitsNothingWhenAllSkipped(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	pub := &eventsCapture{}
	h := NewConfigsHandler(s, nil).WithEvents(pub)

	// Delete non-existent ids — all should be skipped as not_found.
	result := callBatchDelete(t, h, "u1", []string{"missing-1", "missing-2"})
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deletes, got %d", result.Deleted)
	}
	if len(pub.snapshot()) != 0 {
		t.Errorf("expected no events on all-skipped batch, got %d", len(pub.snapshot()))
	}
}

// ─── Hub lifecycle emit sites (PR H.5) ────────────────────────────

func TestHubsCreateEmitsListChangedForOwner(t *testing.T) {
	t.Parallel()
	s := newHubsTestStore()
	pub := &eventsCapture{}
	h := NewHubsHandler(s).WithEvents(pub)
	h.SetOwnershipResolver(&allowAllOwnership{})

	body := `{"name":"Acme Engineering","slug":"acme-eng"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	list := pub.byType(events.EventTypeHubListChanged)
	if len(list) != 1 {
		t.Fatalf("expected 1 hub.list.changed, got %d", len(list))
	}
	if list[0].RecipientUserID != "u1" {
		t.Errorf("creator should receive the event, got recipient %q", list[0].RecipientUserID)
	}
	if list[0].EntityID == "" {
		t.Errorf("expected entity_id=hubID to be populated")
	}
	if list[0].State != "created" {
		t.Errorf("want state=created, got %q", list[0].State)
	}
}

// ─── Publish helper nil-guards (defense against missing WithEvents) ─

func TestHandlersWithNoPublisherDoNotPanic(t *testing.T) {
	// Each handler must remain fully functional without .WithEvents()
	// wired — the publish helpers all nil-guard. This test is a
	// contract reminder: if someone adds an emit site but forgets the
	// nil guard in a helper, a nil publisher deref would show up here.
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil) // note: NO .WithEvents()

	body := `{"agent":"claude-code","file_path":"/cfg.md","scope":"global","content":"hello"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/configs", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Upsert(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 without publisher wired, got %d: %s", rec.Code, rec.Body.String())
	}
}
