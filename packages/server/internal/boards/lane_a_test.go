package boards

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

var testNow = time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)

func newTestProducer(t *testing.T) (*Producer, *store.InMemoryStore, string) {
	t.Helper()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1", HubType: "personal", Name: "Personal"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	p := NewProducer(s)
	p.now = func() time.Time { return testNow }
	return p, s, hub.ID
}

func seedMemory(t *testing.T, s *store.InMemoryStore, id, hubID, agent, title string, createdAt time.Time) {
	t.Helper()
	if err := s.CreateMemory(&model.Memory{
		ID: id, HubID: hubID, OwnerID: "u1", Title: title,
		SourceAgent: agent, CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
}

func slotByKey(t *testing.T, s *store.InMemoryStore, boardID, key string) *model.BoardSlot {
	t.Helper()
	slots, err := s.ListBoardSlots(boardID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range slots {
		if slots[i].SlotKey == key {
			return &slots[i]
		}
	}
	return nil
}

func TestRefreshHubBoardProducesLaneACards(t *testing.T) {
	t.Parallel()
	p, s, hubID := newTestProducer(t)

	// Trace + week window: two agents in the last 24h.
	seedMemory(t, s, "m1", hubID, "claude-code", "Fixed the deploy config", testNow.Add(-2*time.Hour))
	seedMemory(t, s, "m2", hubID, "claude-code", "Chose River over Redis queue", testNow.Add(-1*time.Hour))
	seedMemory(t, s, "m3", hubID, "codex", "Refactored auth middleware", testNow.Add(-3*time.Hour))
	// Last-week-only memory (week diff), outside trace window.
	seedMemory(t, s, "m4", hubID, "codex", "Old note", testNow.Add(-10*24*time.Hour))
	// Capsule hit ~1 year ago.
	seedMemory(t, s, "m5", hubID, "", "如果 AI 能记住我说过的每句话", testNow.AddDate(-1, 0, 2))
	// Onboarding seed must be invisible to every card.
	if err := s.CreateMemory(&model.Memory{
		ID: "seed1", HubID: hubID, OwnerID: "u1", Title: "Seed",
		SourceKind: "onboarding-seed", CreatedAt: testNow.Add(-1 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Topic activity for pulse.
	if err := s.CreateTopic(&model.Topic{ID: "t1", HubID: hubID, Name: "部署"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignMemoryToTopic("m1", "t1", hubID, 1.0); err != nil {
		t.Fatal(err)
	}

	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	board, _ := s.GetOrCreateSystemBoard(hubID, "u1")
	slots, err := s.ListBoardSlots(board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 4 {
		t.Fatalf("expected 4 slots, got %d: %#v", len(slots), slots)
	}

	trace := slotByKey(t, s, board.ID, "a-trace")
	if trace == nil || trace.Kind != model.BoardKindTrace {
		t.Fatalf("missing trace slot: %#v", trace)
	}
	var tp model.BoardTracePayload
	if err := json.Unmarshal(trace.Payload, &tp); err != nil {
		t.Fatal(err)
	}
	if len(tp.Agents) != 2 || tp.Agents[0].Slug != "claude-code" || tp.Agents[0].Count != 2 {
		t.Fatalf("trace grouping wrong (seed leaked or order wrong): %#v", tp.Agents)
	}
	if tp.Agents[0].LatestTitle != "Chose River over Redis queue" {
		t.Fatalf("latest title wrong: %q", tp.Agents[0].LatestTitle)
	}

	capsule := slotByKey(t, s, board.ID, "c-capsule")
	if capsule == nil || len(capsule.CiteMemoryIDs) != 1 || capsule.CiteMemoryIDs[0] != "m5" {
		t.Fatalf("capsule must cite the year-old memory: %#v", capsule)
	}

	week := slotByKey(t, s, board.ID, "d-week")
	var wp model.BoardWeekPayload
	if err := json.Unmarshal(week.Payload, &wp); err != nil {
		t.Fatal(err)
	}
	if wp.ThisWeek != 3 || wp.LastWeek != 1 {
		t.Fatalf("week diff wrong: %#v", wp)
	}

	pulse := slotByKey(t, s, board.ID, "b-pulse")
	var pp model.BoardPulsePayload
	if err := json.Unmarshal(pulse.Payload, &pp); err != nil {
		t.Fatal(err)
	}
	if len(pp.Topics) != 1 || pp.Topics[0].Name != "部署" || pp.Topics[0].RecentCount != 1 {
		t.Fatalf("pulse topics wrong: %#v", pp.Topics)
	}
}

func TestRefreshHubBoardEmptyHubProducesNothing(t *testing.T) {
	t.Parallel()
	p, s, hubID := newTestProducer(t)
	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	board, _ := s.GetOrCreateSystemBoard(hubID, "u1")
	slots, _ := s.ListBoardSlots(board.ID)
	if len(slots) != 0 {
		t.Fatalf("empty hub must produce no cards, got %#v", slots)
	}
}

func TestRefreshHubBoardUnchangedContentKeepsReceipt(t *testing.T) {
	t.Parallel()
	p, s, hubID := newTestProducer(t)
	// Only a capsule memory: its content is stable across runs (same
	// quote, same cite), so a second refresh must be a no-op.
	seedMemory(t, s, "m5", hubID, "", "one year ago thought", testNow.AddDate(-1, 0, 0))

	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	board, _ := s.GetOrCreateSystemBoard(hubID, "u1")
	if _, err := s.ResolveBoardSlot(board.ID, "c-capsule", model.BoardSlotStateResolved,
		model.BoardSlotResolution{Action: "ack", ResolvedBy: "u1"}); err != nil {
		t.Fatal(err)
	}

	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	capsule := slotByKey(t, s, board.ID, "c-capsule")
	if capsule.State != model.BoardSlotStateResolved {
		t.Fatalf("no-op refresh must not reset a resolved card, got state %s", capsule.State)
	}
}

func TestJSONEqualSurvivesJSONBNormalization(t *testing.T) {
	t.Parallel()
	// Postgres jsonb reorders object keys and inserts whitespace, so
	// the no-op guard must compare structurally — a byte comparison
	// would reset every resolved card on every nightly refresh.
	marshaled := json.RawMessage(`{"window_hours":24,"agents":[{"slug":"claude-code","count":2}]}`)
	normalized := json.RawMessage(`{"agents": [{"slug": "claude-code", "count": 2}], "window_hours": 24}`)
	if !jsonEqual(marshaled, normalized) {
		t.Fatal("jsonb-normalized payload must compare equal to marshal output")
	}
	changed := json.RawMessage(`{"agents": [{"slug": "claude-code", "count": 3}], "window_hours": 24}`)
	if jsonEqual(marshaled, changed) {
		t.Fatal("different content must not compare equal")
	}
	if jsonEqual(json.RawMessage(`{invalid`), marshaled) {
		t.Fatal("invalid JSON must not compare equal")
	}
}

func TestRefreshHubBoardRemovesStaleSlots(t *testing.T) {
	t.Parallel()
	p, s, hubID := newTestProducer(t)
	seedMemory(t, s, "m1", hubID, "claude-code", "Recent work", testNow.Add(-1*time.Hour))
	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	board, _ := s.GetOrCreateSystemBoard(hubID, "u1")
	if slotByKey(t, s, board.ID, "a-trace") == nil {
		t.Fatal("trace slot should exist")
	}

	// A month later the activity aged out — trace and week cards must
	// disappear rather than showing stale content.
	p.now = func() time.Time { return testNow.Add(30 * 24 * time.Hour) }
	if err := p.RefreshHubBoard(context.Background(), hubID); err != nil {
		t.Fatal(err)
	}
	slots, _ := s.ListBoardSlots(board.ID)
	if len(slots) != 0 {
		t.Fatalf("aged-out data must remove slots, got %#v", slots)
	}
}
