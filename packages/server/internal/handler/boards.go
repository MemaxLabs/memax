package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// BoardsHandler serves the pulse board surface (plan 25). Boards are
// hub-scoped: every route lives under /v1/hubs/{id}/board and starts
// with the same membership guard as the other hub routes.
type BoardsHandler struct {
	store store.Store
}

func NewBoardsHandler(s store.Store) *BoardsHandler {
	return &BoardsHandler{store: s}
}

// boardSlotActionTargetState mirrors the notifications resolve
// allow-list pattern: the posted action must be a key here, anything
// else is a 400. Keep in sync with the SDK's board action map.
var boardSlotActionTargetState = map[string]string{
	model.BoardSlotActionAck:      model.BoardSlotStateResolved,
	model.BoardSlotActionDismiss:  model.BoardSlotStateDismissed,
	model.BoardSlotActionFeedback: model.BoardSlotStateResolved,
}

// requireHubMember runs the standard hub membership guard. Returns the
// hub id and true when the requester may proceed; writes the error
// response and returns false otherwise.
func (h *BoardsHandler) requireHubMember(w http.ResponseWriter, r *http.Request) (hubID, userID string, ok bool) {
	userID = GetUserID(r)
	hubID = r.PathValue("id")

	role, _ := h.store.GetHubMemberRole(hubID, userID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return "", "", false
	}
	return hubID, userID, true
}

// Get returns the hub's system board and its occupied slots, creating
// the board row on first access. An empty board (no slots yet — dreams
// haven't run) is a normal response, not an error.
func (h *BoardsHandler) Get(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}

	board, err := h.store.GetOrCreateSystemBoard(hubID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	slots, err := h.store.ListBoardSlots(board.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if slots == nil {
		slots = []model.BoardSlot{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"board": board,
		"slots": slots,
	}})
}

// ResolveSlot transitions a card out of its live state. Actions:
//
//	ack      — 收下了, card becomes a receipt (resolved)
//	dismiss  — not useful, card greys out (dismissed)
//	feedback — 准/不准 verdict; records an append-only board_feedback
//	           row (survives slot replacement) then resolves the card
func (h *BoardsHandler) ResolveSlot(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	slotKey := r.PathValue("slot_key")

	var req struct {
		Action  string `json:"action"`
		Verdict string `json:"verdict"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	newState, allowed := boardSlotActionTargetState[req.Action]
	if !allowed {
		writeError(w, http.StatusBadRequest, "invalid_action", "Action must be one of: ack, dismiss, feedback")
		return
	}
	if req.Action == model.BoardSlotActionFeedback &&
		req.Verdict != model.BoardFeedbackAccurate && req.Verdict != model.BoardFeedbackInaccurate {
		writeError(w, http.StatusBadRequest, "invalid_verdict", "Feedback verdict must be 'accurate' or 'inaccurate'")
		return
	}
	if req.Action != model.BoardSlotActionFeedback {
		req.Verdict = ""
	}

	board, err := h.store.GetOrCreateSystemBoard(hubID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	slot, err := h.store.ResolveBoardSlot(board.ID, slotKey, newState, model.BoardSlotResolution{
		Action:     req.Action,
		Verdict:    req.Verdict,
		ResolvedBy: userID,
		ResolvedAt: time.Now().UTC(),
	})
	if errors.Is(err, store.ErrBoardSlotNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "No card in that slot")
		return
	}
	if errors.Is(err, store.ErrBoardSlotAlreadyResolved) {
		writeError(w, http.StatusConflict, "already_resolved", "This card was already resolved")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Feedback is recorded after the transition succeeds so a replayed
	// or already-resolved request can't double-count a verdict.
	if req.Action == model.BoardSlotActionFeedback {
		if err := h.store.CreateBoardFeedback(&model.BoardFeedback{
			BoardID:       board.ID,
			SlotKey:       slot.SlotKey,
			CardKind:      slot.Kind,
			CardTitle:     slot.Title,
			Verdict:       req.Verdict,
			UserID:        userID,
			CiteMemoryIDs: slot.CiteMemoryIDs,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"slot": slot,
	}})
}
