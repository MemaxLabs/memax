package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func boardsListRequest(hubID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hubID+"/boards", nil)
	req.SetPathValue("id", hubID)
	return withTestIdentity(req, userID)
}

func boardCreateRequest(hubID, userID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/boards", strings.NewReader(body))
	req.SetPathValue("id", hubID)
	return withTestIdentity(req, userID)
}

func boardMutateRequest(method, hubID, boardID, userID, body string) *http.Request {
	req := httptest.NewRequest(method, "/v1/hubs/"+hubID+"/boards/"+boardID, strings.NewReader(body))
	req.SetPathValue("id", hubID)
	req.SetPathValue("board_id", boardID)
	return withTestIdentity(req, userID)
}

func createTestBoard(t *testing.T, h *BoardsHandler, hubID, userID, body string) model.Board {
	t.Helper()
	rec := httptest.NewRecorder()
	h.CreateBoard(rec, boardCreateRequest(hubID, userID, body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create board: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Board model.Board `json:"board"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.Board
}

func TestCustomBoardCreateStartsCooking(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)

	board := createTestBoard(t, h, boardsTestHubID, "u1",
		`{"title":"健身 & 睡眠","instruction":"追踪我的训练和睡眠，两者背离时告诉我"}`)
	// The promise is "tomorrow morning", so a fresh board must not
	// claim to be active with nothing on it.
	if board.Status != model.BoardStatusCooking {
		t.Fatalf("new custom board must start cooking, got %q", board.Status)
	}
	if board.Kind != model.BoardKindCustom || board.Instruction == "" {
		t.Fatalf("unexpected board: %#v", board)
	}

	// Listing returns system board first, then the custom one.
	rec := httptest.NewRecorder()
	h.ListBoards(rec, boardsListRequest(boardsTestHubID, "u1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data struct {
			Boards []model.Board `json:"boards"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data.Boards) != 2 || list.Data.Boards[0].Kind != model.BoardKindSystem {
		t.Fatalf("expected [system, custom], got %#v", list.Data.Boards)
	}
}

func TestCustomBoardValidationAndLimit(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)

	for _, tc := range []struct{ name, body string }{
		{"empty title", `{"title":"","instruction":"x"}`},
		{"empty instruction", `{"title":"t","instruction":""}`},
		{"long title", `{"title":"` + strings.Repeat("x", 81) + `","instruction":"x"}`},
		{"long instruction", `{"title":"t","instruction":"` + strings.Repeat("x", 2001) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.CreateBoard(rec, boardCreateRequest(boardsTestHubID, "u1", tc.body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	for i := 0; i < maxCustomBoardsPerHub; i++ {
		createTestBoard(t, h, boardsTestHubID, "u1", `{"title":"b","instruction":"i"}`)
	}
	rec := httptest.NewRecorder()
	h.CreateBoard(rec, boardCreateRequest(boardsTestHubID, "u1", `{"title":"one more","instruction":"i"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("over limit: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCustomBoardUpdateRewriteReturnsToCooking(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)
	board := createTestBoard(t, h, boardsTestHubID, "u1", `{"title":"t","instruction":"original brief"}`)

	// Simulate the dream run activating it.
	board.Status = model.BoardStatusActive
	if err := s.UpdateBoard(&board); err != nil {
		t.Fatal(err)
	}

	// Title-only edit keeps it active.
	rec := httptest.NewRecorder()
	h.UpdateBoard(rec, boardMutateRequest(http.MethodPatch, boardsTestHubID, board.ID, "u1", `{"title":"renamed"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.GetBoard(board.ID)
	if after.Status != model.BoardStatusActive || after.Title != "renamed" {
		t.Fatalf("rename should not disturb status: %#v", after)
	}

	// Rewriting the brief invalidates existing cards → back to cooking.
	rec = httptest.NewRecorder()
	h.UpdateBoard(rec, boardMutateRequest(http.MethodPatch, boardsTestHubID, board.ID, "u1", `{"instruction":"a totally different brief"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body.String())
	}
	after, _ = s.GetBoard(board.ID)
	if after.Status != model.BoardStatusCooking {
		t.Fatalf("rewritten brief must return the board to cooking, got %q", after.Status)
	}
}

func TestCustomBoardGuards(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	otherHub := "44444444-4444-4444-4444-444444444444"
	if err := s.CreateHub(&model.Hub{ID: otherHub, OwnerID: "u2", Slug: "other", HubType: "personal"}); err != nil {
		t.Fatal(err)
	}
	s.roles[otherHub+":u2"] = "owner"
	h := NewBoardsHandler(s)
	board := createTestBoard(t, h, boardsTestHubID, "u1", `{"title":"t","instruction":"i"}`)

	// Non-member of the hub in the path.
	rec := httptest.NewRecorder()
	h.UpdateBoard(rec, boardMutateRequest(http.MethodPatch, boardsTestHubID, board.ID, "outsider", `{"title":"x"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member: expected 403, got %d", rec.Code)
	}

	// Member of another hub cannot reach this board by id.
	rec = httptest.NewRecorder()
	h.UpdateBoard(rec, boardMutateRequest(http.MethodPatch, otherHub, board.ID, "u2", `{"title":"x"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-hub board id: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// The system board is immutable.
	system, _ := s.GetOrCreateSystemBoard(boardsTestHubID, "u1")
	rec = httptest.NewRecorder()
	h.UpdateBoard(rec, boardMutateRequest(http.MethodPatch, boardsTestHubID, system.ID, "u1", `{"title":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("system board edit: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.DeleteBoard(rec, boardMutateRequest(http.MethodDelete, boardsTestHubID, system.ID, "u1", ``))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("system board delete: expected 400, got %d", rec.Code)
	}

	// Deleting a custom board works.
	rec = httptest.NewRecorder()
	h.DeleteBoard(rec, boardMutateRequest(http.MethodDelete, boardsTestHubID, board.ID, "u1", ``))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetBoardByIDReturnsSlots(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	otherHub := "55555555-5555-5555-5555-555555555555"
	if err := s.CreateHub(&model.Hub{ID: otherHub, OwnerID: "u2", Slug: "other2", HubType: "personal"}); err != nil {
		t.Fatal(err)
	}
	s.roles[otherHub+":u2"] = "owner"
	h := NewBoardsHandler(s)
	board := createTestBoard(t, h, boardsTestHubID, "u1", `{"title":"健身","instruction":"追踪训练"}`)

	// A card written by synthesis onto the custom board.
	if err := s.UpsertBoardSlot(&model.BoardSlot{
		BoardID: board.ID, SlotKey: "1-wow", Kind: "pattern", Title: "训练在周末塌方",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+boardsTestHubID+"/boards/"+board.ID, nil)
	req.SetPathValue("id", boardsTestHubID)
	req.SetPathValue("board_id", board.ID)
	rec := httptest.NewRecorder()
	h.GetBoardByID(rec, withTestIdentity(req, "u1"))
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
	if resp.Data.Board.ID != board.ID || len(resp.Data.Slots) != 1 ||
		resp.Data.Slots[0].Title != "训练在周末塌方" {
		t.Fatalf("unexpected payload: %#v %#v", resp.Data.Board, resp.Data.Slots)
	}

	// Reading through a hub the board doesn't belong to must 404.
	req = httptest.NewRequest(http.MethodGet, "/v1/hubs/"+otherHub+"/boards/"+board.ID, nil)
	req.SetPathValue("id", otherHub)
	req.SetPathValue("board_id", board.ID)
	rec = httptest.NewRecorder()
	h.GetBoardByID(rec, withTestIdentity(req, "u2"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-hub read: expected 404, got %d", rec.Code)
	}
}
