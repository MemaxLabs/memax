package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// BoardsHandler serves the pulse board surface (plan 25). Boards are
// hub-scoped: every route lives under /v1/hubs/{id}/board and starts
// with the same membership guard as the other hub routes.
type BoardsHandler struct {
	store  store.Store
	events events.Publisher
	// enqueue schedules ingest processing (chunk/embed/classify) for
	// memories the board writes — the decision write-back must be
	// recallable, not just stored. Same seam as MemoriesHandler.
	enqueue func(memoryID, ownerID string, req model.PushRequest)
}

func NewBoardsHandler(s store.Store) *BoardsHandler {
	return &BoardsHandler{store: s}
}

// WithEvents wires the realtime publisher (chained at construction).
func (h *BoardsHandler) WithEvents(p events.Publisher) *BoardsHandler {
	h.events = p
	return h
}

// SetEnqueue wires the ingest queue seam (set from serverapp).
func (h *BoardsHandler) SetEnqueue(fn func(memoryID, ownerID string, req model.PushRequest)) {
	h.enqueue = fn
}

// boardSlotActionTargetState mirrors the notifications resolve
// allow-list pattern: the posted action must be a key here, anything
// else is a 400. Keep in sync with the SDK's board action map.
var boardSlotActionTargetState = map[string]string{
	model.BoardSlotActionAck:      model.BoardSlotStateResolved,
	model.BoardSlotActionDismiss:  model.BoardSlotStateDismissed,
	model.BoardSlotActionFeedback: model.BoardSlotStateResolved,
	model.BoardSlotActionChoose:   model.BoardSlotStateResolved,
}

// requireHubMember runs the standard hub membership guard. Returns the
// hub id and true when the requester may proceed; writes the error
// response and returns false otherwise.
func (h *BoardsHandler) requireHubMember(w http.ResponseWriter, r *http.Request) (hubID, userID string, ok bool) {
	hubID, userID, _, ok = h.requireHubRole(w, r)
	return hubID, userID, ok
}

// requireHubRole is the membership guard that also surfaces the role,
// so mutation paths can additionally require admin.
func (h *BoardsHandler) requireHubRole(w http.ResponseWriter, r *http.Request) (hubID, userID, role string, ok bool) {
	userID = GetUserID(r)
	hubID = r.PathValue("id")

	role, err := h.store.GetHubMemberRole(hubID, userID)
	if err != nil {
		// A store failure must not masquerade as a membership denial —
		// 403 would mislead real members and defeat client retries.
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return "", "", "", false
	}
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of this hub")
		return "", "", "", false
	}
	return hubID, userID, role, true
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
// targetBoard resolves which board a slot route addresses: the hub's
// system board by default, or — when the route carries {board_id}
// (the board-scoped variants, issue #41) — that board, after
// verifying it belongs to the path's hub. Membership-level: resolving
// cards is a member action, unlike requireCustomBoard's admin gate
// for editing boards. The path's hub stays authoritative — a member
// of hub A must not reach hub B's board by id.
func (h *BoardsHandler) targetBoard(w http.ResponseWriter, r *http.Request, hubID, userID string) (*model.Board, bool) {
	boardID := r.PathValue("board_id")
	if boardID == "" {
		board, err := h.store.GetOrCreateSystemBoard(hubID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return nil, false
		}
		return board, true
	}
	board, err := h.store.GetBoard(boardID)
	if errors.Is(err, store.ErrBoardNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Board not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return nil, false
	}
	if board.HubID != hubID {
		writeError(w, http.StatusNotFound, "not_found", "Board not found")
		return nil, false
	}
	return board, true
}

func (h *BoardsHandler) ResolveSlot(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	slotKey := r.PathValue("slot_key")

	var req struct {
		Action  string `json:"action"`
		Verdict string `json:"verdict"`
		Choice  string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	// Reopen is the undo path — the inverse transition of everything
	// below, so it short-circuits before the target-state table.
	// Idempotent like resolve: undoing a card someone already reopened
	// returns 200 with the current (live) slot.
	if req.Action == model.BoardSlotActionReopen {
		board, ok := h.targetBoard(w, r, hubID, userID)
		if !ok {
			return
		}
		slot, err := h.store.ReopenBoardSlot(board.ID, slotKey)
		if errors.Is(err, store.ErrBoardSlotAlreadyResolved) {
			slot, err = h.store.GetBoardSlot(board.ID, slotKey)
		}
		if errors.Is(err, store.ErrBoardSlotNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No card in that slot")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"slot": slot,
		}})
		return
	}

	newState, allowed := boardSlotActionTargetState[req.Action]
	if !allowed {
		writeError(w, http.StatusBadRequest, "invalid_action", "Action must be one of: ack, dismiss, feedback, reopen")
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
	if req.Action == model.BoardSlotActionChoose && req.Choice == "" {
		writeError(w, http.StatusBadRequest, "invalid_choice", "Choose requires a 'choice' option id")
		return
	}

	board, boardOK := h.targetBoard(w, r, hubID, userID)
	if !boardOK {
		return
	}

	if req.Action == model.BoardSlotActionChoose {
		// Choose is decision-gate-only, and the choice must be one of
		// the gate's own options.
		gate, err := h.store.GetBoardSlot(board.ID, slotKey)
		if errors.Is(err, store.ErrBoardSlotNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "No card in that slot")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		if gate.Kind != model.BoardKindDecisionGate || !decisionGateHasOption(gate, req.Choice) {
			writeError(w, http.StatusBadRequest, "invalid_choice", "Choice is not an option on this card")
			return
		}
	}

	verdict := req.Verdict
	if req.Action == model.BoardSlotActionChoose {
		verdict = req.Choice
	}
	slot, err := h.store.ResolveBoardSlot(board.ID, slotKey, newState, model.BoardSlotResolution{
		Action:     req.Action,
		Verdict:    verdict,
		ResolvedBy: userID,
		ResolvedAt: time.Now().UTC(),
	})
	if errors.Is(err, store.ErrBoardSlotNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "No card in that slot")
		return
	}
	transitioned := err == nil
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

	// Side effects fire only when THIS call performed the transition.
	// A retry lands in the already-resolved branch (transitioned ==
	// false) and must not write a second decision memory — comparing
	// the stored resolution to the request can't tell the two apart
	// when the retry replays the same choice.
	if req.Action == model.BoardSlotActionChoose && transitioned {
		if hub, err := h.store.GetHub(hubID); err == nil {
			h.resolveDecisionGateSideEffects(r, hub, slot, userID, req.Choice)
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"slot": slot,
	}})
}

// SlotHistory returns a slot's archived content versions, newest
// first. Slots are replaced in place by producers; the history is what
// lets the pulse UI show a stateful card's timeline (新 version / 旧
// version) instead of pretending each night's card is the first.
// GET /v1/hubs/{id}/board/slots/{slot_key}/history
func (h *BoardsHandler) SlotHistory(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	slotKey := r.PathValue("slot_key")
	board, ok2 := h.targetBoard(w, r, hubID, userID)
	if !ok2 {
		return
	}
	versions, err := h.store.ListBoardSlotHistory(board.ID, slotKey, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"versions": versions,
	}})
}

// decisionGateHasOption reports whether the gate payload contains the
// option id.
func decisionGateHasOption(slot *model.BoardSlot, choice string) bool {
	var payload model.BoardDecisionGatePayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		return false
	}
	for _, opt := range payload.Options {
		if opt.ID == choice {
			return true
		}
	}
	return false
}
