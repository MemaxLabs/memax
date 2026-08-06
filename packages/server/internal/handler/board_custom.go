package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Custom boards (plan 25 P4). A user writes a standing instruction —
// "track my sleep and training, tell me when they diverge" — and gets
// a board whose nightly synthesis honors that brief. The board is
// created in the 酝酿中 (cooking) state and flips to active the first
// time a dream run puts a real card on it: the promise is "tomorrow
// morning", not "instantly", and the UI says so.

const (
	maxBoardTitleRunes       = 80
	maxBoardInstructionRunes = 2000
	maxCustomBoardsPerHub    = 8
)

// ListBoards returns every board on the hub (system first).
func (h *BoardsHandler) ListBoards(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	if _, err := h.store.GetOrCreateSystemBoard(hubID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	boards, err := h.store.ListBoardsByHub(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if boards == nil {
		boards = []model.Board{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"boards": boards}})
}

// CreateBoard adds a custom board to the hub.
func (h *BoardsHandler) CreateBoard(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	var req struct {
		Title       string `json:"title"`
		Instruction string `json:"instruction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	instruction := strings.TrimSpace(req.Instruction)
	if title == "" || utf8.RuneCountInString(title) > maxBoardTitleRunes {
		writeError(w, http.StatusBadRequest, "invalid_title",
			fmt.Sprintf("Board title must be 1..%d characters", maxBoardTitleRunes))
		return
	}
	if instruction == "" || utf8.RuneCountInString(instruction) > maxBoardInstructionRunes {
		writeError(w, http.StatusBadRequest, "invalid_instruction",
			fmt.Sprintf("Board instruction must be 1..%d characters", maxBoardInstructionRunes))
		return
	}

	boards, err := h.store.ListBoardsByHub(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	custom := 0
	for _, b := range boards {
		if b.Kind == model.BoardKindCustom {
			custom++
		}
	}
	if custom >= maxCustomBoardsPerHub {
		writeError(w, http.StatusConflict, "board_limit",
			fmt.Sprintf("This hub already has %d custom boards", maxCustomBoardsPerHub))
		return
	}

	board := &model.Board{
		HubID:       hubID,
		CreatedBy:   userID,
		Kind:        model.BoardKindCustom,
		Title:       title,
		Instruction: instruction,
		// Cooking until the next dream run fills it — the UI shows
		// "xxx 正在酝酿，明早见" instead of an empty board.
		Status: model.BoardStatusCooking,
	}
	if err := h.store.CreateBoard(board); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: map[string]any{"board": board}})
}

// UpdateBoard edits a custom board's title/instruction/status. The
// system board is not editable — its behavior is code-defined.
func (h *BoardsHandler) UpdateBoard(w http.ResponseWriter, r *http.Request) {
	board, ok := h.requireCustomBoard(w, r)
	if !ok {
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Instruction *string `json:"instruction"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || utf8.RuneCountInString(title) > maxBoardTitleRunes {
			writeError(w, http.StatusBadRequest, "invalid_title",
				fmt.Sprintf("Board title must be 1..%d characters", maxBoardTitleRunes))
			return
		}
		board.Title = title
	}
	if req.Instruction != nil {
		instruction := strings.TrimSpace(*req.Instruction)
		if instruction == "" || utf8.RuneCountInString(instruction) > maxBoardInstructionRunes {
			writeError(w, http.StatusBadRequest, "invalid_instruction",
				fmt.Sprintf("Board instruction must be 1..%d characters", maxBoardInstructionRunes))
			return
		}
		// A rewritten brief means the existing cards answer the old
		// question — put the board back in cooking so the UI promises
		// fresh output instead of presenting stale cards as current.
		if instruction != board.Instruction {
			board.Status = model.BoardStatusCooking
		}
		board.Instruction = instruction
	}
	if req.Status != nil {
		switch *req.Status {
		case model.BoardStatusActive, model.BoardStatusPaused:
			board.Status = *req.Status
		default:
			writeError(w, http.StatusBadRequest, "invalid_status",
				"Status must be 'active' or 'paused'")
			return
		}
	}
	if err := h.store.UpdateBoard(board); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"board": board}})
}

// DeleteBoard removes a custom board and (via cascade) its slots.
func (h *BoardsHandler) DeleteBoard(w http.ResponseWriter, r *http.Request) {
	board, ok := h.requireCustomBoard(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteBoard(board.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"deleted": true}})
}

// requireCustomBoard resolves {board_id} under the hub membership
// guard and rejects both cross-hub ids and the system board.
func (h *BoardsHandler) requireCustomBoard(w http.ResponseWriter, r *http.Request) (*model.Board, bool) {
	hubID, _, ok := h.requireHubMember(w, r)
	if !ok {
		return nil, false
	}
	board, err := h.store.GetBoard(r.PathValue("board_id"))
	if errors.Is(err, store.ErrBoardNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Board not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return nil, false
	}
	// The path's hub is authoritative: a member of hub A must not be
	// able to reach hub B's board by id.
	if board.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Board not found")
		return nil, false
	}
	if board.Kind == model.BoardKindSystem {
		writeError(w, http.StatusBadRequest, "system_board_immutable",
			"The system board cannot be edited or deleted")
		return nil, false
	}
	return board, true
}
