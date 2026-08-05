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

	role, err := h.store.GetHubMemberRole(hubID, userID)
	if err != nil {
		// A store failure must not masquerade as a membership denial —
		// 403 would mislead real members and defeat client retries.
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return "", "", false
	}
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
//	feedback — 准/不准 verdict; records a per-member board_feedback row
//	           (latest verdict wins, survives slot replacement) then
//	           resolves the card
//
// Resolution is idempotent: a slot another member (or a retry) already
// settled returns 200 with the current slot instead of an error — a
// benign race on a shared board must not surface as a failure. Feedback
// verdicts are still recorded on already-terminal slots so every hub
// member can weigh in, not just whoever resolved the card first.
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
		// Idempotent path: someone (or a retry) settled the card first.
		// Return the current slot; feedback below still records.
		slot, err = h.store.GetBoardSlot(board.ID, slotKey)
		if errors.Is(err, store.ErrBoardSlotNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No card in that slot")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Feedback is recorded after the transition (or the idempotent
	// read) succeeds. The store upserts per (board, slot, member), so a
	// retry after a failed sequence — including one where the resolve
	// already committed — still lands the verdict instead of losing it.
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
