package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func testBoardSlot(boardID, slotKey string) *model.BoardSlot {
	return &model.BoardSlot{
		BoardID: boardID,
		SlotKey: slotKey,
		Kind:    "trace",
		Title:   "test card",
		Payload: json.RawMessage(`{"description":"v1"}`),
	}
}

func TestInMemoryBoardsSystemBoardIsPerHubSingleton(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	a1, err := s.GetOrCreateSystemBoard("hub-a", "u1")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.GetOrCreateSystemBoard("hub-a", "u2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.GetOrCreateSystemBoard("hub-b", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("same hub must reuse the system board: %s vs %s", a1.ID, a2.ID)
	}
	if a1.ID == b.ID {
		t.Fatal("different hubs must not share a board")
	}
	if a1.Kind != model.BoardKindSystem || a1.Status != model.BoardStatusActive {
		t.Fatalf("unexpected board defaults: %#v", a1)
	}
}

func TestInMemoryBoardsUpsertReplaceSemantics(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	board, _ := s.GetOrCreateSystemBoard("hub-a", "u1")

	slot := testBoardSlot(board.ID, "hero")
	if err := s.UpsertBoardSlot(slot); err != nil {
		t.Fatal(err)
	}
	firstID := slot.ID

	// Resolve it, then replace the content: state must reset to fresh
	// and the resolution receipt must be cleared, but the slot row
	// identity survives.
	if _, err := s.ResolveBoardSlot(board.ID, "hero", model.BoardSlotStateResolved,
		model.BoardSlotResolution{Action: "ack", ResolvedBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	replacement := testBoardSlot(board.ID, "hero")
	replacement.Title = "v2 card"
	if err := s.UpsertBoardSlot(replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.ID != firstID {
		t.Fatalf("replace must keep slot identity: %s vs %s", replacement.ID, firstID)
	}

	slots, err := s.ListBoardSlots(board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	got := slots[0]
	if got.Title != "v2 card" || got.State != model.BoardSlotStateFresh || got.Resolution != nil {
		t.Fatalf("replace must reset lifecycle: %#v", got)
	}
}

func TestInMemoryBoardsResolveTransitions(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	board, _ := s.GetOrCreateSystemBoard("hub-a", "u1")
	if err := s.UpsertBoardSlot(testBoardSlot(board.ID, "hero")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ResolveBoardSlot(board.ID, "missing", model.BoardSlotStateResolved,
		model.BoardSlotResolution{Action: "ack"}); !errors.Is(err, ErrBoardSlotNotFound) {
		t.Fatalf("expected ErrBoardSlotNotFound, got %v", err)
	}

	slot, err := s.ResolveBoardSlot(board.ID, "hero", model.BoardSlotStateDismissed,
		model.BoardSlotResolution{Action: "dismiss", ResolvedBy: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if slot.State != model.BoardSlotStateDismissed || slot.Resolution == nil {
		t.Fatalf("unexpected resolved slot: %#v", slot)
	}

	if _, err := s.ResolveBoardSlot(board.ID, "hero", model.BoardSlotStateResolved,
		model.BoardSlotResolution{Action: "ack"}); !errors.Is(err, ErrBoardSlotAlreadyResolved) {
		t.Fatalf("expected ErrBoardSlotAlreadyResolved, got %v", err)
	}
}

func TestInMemoryBoardsGetSlotAndFeedbackUpsert(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	board, _ := s.GetOrCreateSystemBoard("hub-a", "u1")
	if err := s.UpsertBoardSlot(testBoardSlot(board.ID, "hero")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetBoardSlot(board.ID, "missing"); !errors.Is(err, ErrBoardSlotNotFound) {
		t.Fatalf("expected ErrBoardSlotNotFound, got %v", err)
	}
	slot, err := s.GetBoardSlot(board.ID, "hero")
	if err != nil || slot.SlotKey != "hero" {
		t.Fatalf("GetBoardSlot: %v, %#v", err, slot)
	}

	// Feedback upserts per (board, slot, member): same member's repeat
	// verdict replaces, another member's verdict adds a row.
	mk := func(user, verdict string) *model.BoardFeedback {
		return &model.BoardFeedback{
			BoardID: board.ID, SlotKey: "hero", CardKind: "trace",
			CardTitle: "test card", Verdict: verdict, UserID: user,
		}
	}
	if err := s.CreateBoardFeedback(mk("u1", "accurate")); err != nil {
		t.Fatal(err)
	}
	f2 := mk("u1", "inaccurate")
	if err := s.CreateBoardFeedback(f2); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBoardFeedback(mk("u2", "accurate")); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.boardFeedback) != 2 {
		t.Fatalf("expected 2 rows (u1 upserted, u2 added), got %d", len(s.boardFeedback))
	}
	for _, row := range s.boardFeedback {
		if row.UserID == "u1" && row.Verdict != "inaccurate" {
			t.Fatalf("u1 repeat verdict must win: %#v", row)
		}
	}
}

func TestInMemoryBoardsSlotValidation(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	board, _ := s.GetOrCreateSystemBoard("hub-a", "u1")

	cases := []struct {
		name    string
		mutate  func(*model.BoardSlot)
		wantSub string
	}{
		{"empty title", func(sl *model.BoardSlot) { sl.Title = "" }, "title"},
		{"empty kind", func(sl *model.BoardSlot) { sl.Kind = "" }, "kind"},
		{"empty slot key", func(sl *model.BoardSlot) { sl.SlotKey = "" }, "slot_key"},
		{"invalid payload", func(sl *model.BoardSlot) { sl.Payload = json.RawMessage(`{nope`) }, "JSON"},
		{"oversized payload", func(sl *model.BoardSlot) {
			sl.Payload = json.RawMessage(`"` + strings.Repeat("x", 33*1024) + `"`)
		}, "payload"},
		{"too many citations", func(sl *model.BoardSlot) {
			sl.CiteMemoryIDs = make([]string, 21)
		}, "cite_memory_ids"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slot := testBoardSlot(board.ID, "hero")
			tc.mutate(slot)
			err := s.UpsertBoardSlot(slot)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}
