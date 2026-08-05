package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// boardsTestStore overrides the InMemoryStore's permissive
// GetHubMemberRole (always "owner") with a real roles map so the
// membership guard is actually exercised, and captures feedback rows
// for assertions.
type boardsTestStore struct {
	*store.InMemoryStore
	roles    map[string]string
	feedback []*model.BoardFeedback
}

func newBoardsTestStore() *boardsTestStore {
	return &boardsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		roles:         make(map[string]string),
	}
}

func (s *boardsTestStore) GetHubMemberRole(hubID, userID string) (string, error) {
	return s.roles[hubID+":"+userID], nil
}

func (s *boardsTestStore) CreateBoardFeedback(f *model.BoardFeedback) error {
	if err := s.InMemoryStore.CreateBoardFeedback(f); err != nil {
		return err
	}
	copy := *f
	s.feedback = append(s.feedback, &copy)
	return nil
}

const boardsTestHubID = "22222222-2222-2222-2222-222222222222"

func boardsGetRequest(hubID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hubID+"/board", nil)
	req.SetPathValue("id", hubID)
	return withTestIdentity(req, userID)
}

func boardsResolveRequest(hubID, slotKey, userID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/board/slots/"+slotKey+"/resolve", bytes.NewBufferString(body))
	req.SetPathValue("id", hubID)
	req.SetPathValue("slot_key", slotKey)
	return withTestIdentity(req, userID)
}

func TestBoardsGetRejectsNonMember(t *testing.T) {
	s := newBoardsTestStore()
	h := NewBoardsHandler(s)

	rec := httptest.NewRecorder()
	h.Get(rec, boardsGetRequest(boardsTestHubID, "outsider"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_member") {
		t.Fatalf("expected not_member error, got %s", rec.Body.String())
	}
}

func TestBoardsGetCreatesBoardOnceAndReturnsEmptySlots(t *testing.T) {
	s := newBoardsTestStore()
	s.roles[boardsTestHubID+":u1"] = "member"
	h := NewBoardsHandler(s)

	var boardIDs []string
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.Get(rec, boardsGetRequest(boardsTestHubID, "u1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				Board model.Board       `json:"board"`
				Slots []model.BoardSlot `json:"slots"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Data.Board.HubID != boardsTestHubID {
			t.Fatalf("board hub mismatch: %s", resp.Data.Board.HubID)
		}
		if resp.Data.Slots == nil || len(resp.Data.Slots) != 0 {
			t.Fatalf("expected empty (non-null) slots, got %#v", resp.Data.Slots)
		}
		boardIDs = append(boardIDs, resp.Data.Board.ID)
	}
	if boardIDs[0] != boardIDs[1] {
		t.Fatalf("system board must be created once, got %s then %s", boardIDs[0], boardIDs[1])
	}
}

func seedBoardSlot(t *testing.T, s *boardsTestStore, hubID, slotKey string) *model.Board {
	t.Helper()
	board, err := s.GetOrCreateSystemBoard(hubID, "producer")
	if err != nil {
		t.Fatal(err)
	}
	slot := &model.BoardSlot{
		BoardID:       board.ID,
		SlotKey:       slotKey,
		Kind:          "trace",
		Title:         "Claude Code 这周在 3 个仓库里找过部署配置",
		Payload:       json.RawMessage(`{"description":"seeded"}`),
		CiteMemoryIDs: []string{"33333333-3333-3333-3333-333333333333"},
	}
	if err := s.UpsertBoardSlot(slot); err != nil {
		t.Fatal(err)
	}
	return board
}

func TestBoardsResolveSlotLifecycle(t *testing.T) {
	s := newBoardsTestStore()
	s.roles[boardsTestHubID+":u1"] = "member"
	h := NewBoardsHandler(s)
	seedBoardSlot(t, s, boardsTestHubID, "hero")

	// Invalid action.
	rec := httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "hero", "u1", `{"action":"explode"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid action: expected 400, got %d", rec.Code)
	}

	// Feedback without a valid verdict.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "hero", "u1", `{"action":"feedback","verdict":"meh"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid verdict: expected 400, got %d", rec.Code)
	}

	// Unknown slot.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "nope", "u1", `{"action":"ack"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slot: expected 404, got %d", rec.Code)
	}

	// Happy path: ack.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "hero", "u1", `{"action":"ack"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("ack: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Slot model.BoardSlot `json:"slot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Slot.State != model.BoardSlotStateResolved {
		t.Fatalf("expected resolved state, got %s", resp.Data.Slot.State)
	}
	if resp.Data.Slot.Resolution == nil || resp.Data.Slot.Resolution.ResolvedBy != "u1" {
		t.Fatalf("expected resolution receipt by u1, got %#v", resp.Data.Slot.Resolution)
	}

	// Terminal slots are not re-resolvable.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "hero", "u1", `{"action":"dismiss"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("double resolve: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBoardsFeedbackRecordsVerdictSnapshot(t *testing.T) {
	s := newBoardsTestStore()
	s.roles[boardsTestHubID+":u1"] = "member"
	h := NewBoardsHandler(s)
	seedBoardSlot(t, s, boardsTestHubID, "wow")

	rec := httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "wow", "u1", `{"action":"feedback","verdict":"accurate"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(s.feedback) != 1 {
		t.Fatalf("expected 1 feedback row, got %d", len(s.feedback))
	}
	f := s.feedback[0]
	if f.Verdict != model.BoardFeedbackAccurate || f.UserID != "u1" || f.CardKind != "trace" {
		t.Fatalf("feedback snapshot mismatch: %#v", f)
	}
	if len(f.CiteMemoryIDs) != 1 {
		t.Fatalf("feedback must snapshot citations, got %#v", f.CiteMemoryIDs)
	}
}

func TestBoardsSlotIsolationBetweenHubs(t *testing.T) {
	s := newBoardsTestStore()
	otherHub := "44444444-4444-4444-4444-444444444444"
	s.roles[boardsTestHubID+":u1"] = "member"
	s.roles[otherHub+":u1"] = "member"
	h := NewBoardsHandler(s)
	seedBoardSlot(t, s, boardsTestHubID, "hero")

	// u1 is a member of both hubs, but hub B's board has no "hero"
	// slot — resolving through hub B must not reach hub A's card.
	rec := httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(otherHub, "hero", "u1", `{"action":"ack"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-hub resolve: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// Non-member of hub A cannot resolve hub A's card at all.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "hero", "u2", `{"action":"ack"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member resolve: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
