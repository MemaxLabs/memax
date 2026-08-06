package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Decision gates (plan 25 P2, 等你 cards). An agent hits a fork it
// shouldn't decide alone and calls memax_request_decision; the gate
// lands as BOTH a board slot (the decision surface) and a needs-action
// notification (the ping). Choosing an option on the board resolves
// the slot, resolves the companion notification, and writes the
// decision back into hub memory — so the requesting agent's next
// recall sees the answer. That write-back is the whole point: the
// user's decision becomes durable, recallable context instead of a
// chat reply that evaporates.

const (
	maxDecisionGateOptions       = 4
	minDecisionGateOptions       = 2
	maxDecisionGateQuestionRunes = 300
	maxDecisionGateContextRunes  = 2000
	maxLiveDecisionGates         = 3
)

var (
	errGateLimit   = errors.New("too many open decision gates")
	errGateInvalid = errors.New("invalid decision gate")
)

// createDecisionGate is the shared write path for the REST endpoint
// and the MCP tool. Returns the created slot.
func createDecisionGate(
	s store.Store,
	publisher events.Publisher,
	hub *model.Hub,
	question, contextText, sourceAgent string,
	optionLabels []string,
) (*model.BoardSlot, error) {
	question = strings.TrimSpace(question)
	if question == "" || utf8.RuneCountInString(question) > maxDecisionGateQuestionRunes {
		return nil, fmt.Errorf("%w: question must be 1..%d chars", errGateInvalid, maxDecisionGateQuestionRunes)
	}
	if utf8.RuneCountInString(contextText) > maxDecisionGateContextRunes {
		return nil, fmt.Errorf("%w: context exceeds %d chars", errGateInvalid, maxDecisionGateContextRunes)
	}
	var options []model.BoardDecisionOption
	for _, label := range optionLabels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		options = append(options, model.BoardDecisionOption{
			ID:    fmt.Sprintf("opt-%d", len(options)+1),
			Label: label,
		})
	}
	if len(options) < minDecisionGateOptions || len(options) > maxDecisionGateOptions {
		return nil, fmt.Errorf("%w: need %d-%d non-empty options", errGateInvalid, minDecisionGateOptions, maxDecisionGateOptions)
	}

	board, err := s.GetOrCreateSystemBoard(hub.ID, hub.OwnerID)
	if err != nil {
		return nil, err
	}
	slots, err := s.ListBoardSlots(board.ID)
	if err != nil {
		return nil, err
	}
	live := 0
	for _, slot := range slots {
		if slot.Kind == model.BoardKindDecisionGate &&
			(slot.State == model.BoardSlotStateFresh || slot.State == model.BoardSlotStateSeen) {
			live++
		}
	}
	if live >= maxLiveDecisionGates {
		return nil, errGateLimit
	}

	// Full uuid, not a truncation: this key doubles as the
	// notification's globally-unique source_id, so 32 bits of entropy
	// would collide across hubs (birthday bound: ~1% at 9k gates) —
	// and a collision silently drops the ping AND lets a later resolve
	// settle a different hub's decision.
	slotKey := model.BoardSlotKeyGatePrefix + generateID()
	payload, err := json.Marshal(model.BoardDecisionGatePayload{
		Description: question,
		Question:    question,
		Context:     strings.TrimSpace(contextText),
		Options:     options,
		SourceAgent: sourceAgent,
	})
	if err != nil {
		return nil, err
	}
	slot := &model.BoardSlot{
		BoardID: board.ID,
		SlotKey: slotKey,
		Kind:    model.BoardKindDecisionGate,
		Title:   question,
		Payload: payload,
	}
	if err := s.UpsertBoardSlot(slot); err != nil {
		return nil, err
	}

	// The ping: one hub_member row is visible to every current member
	// without fan-out. Idempotent on (source_kind, source_id) so a
	// retried tool call can't double-ping.
	notifPayload, err := json.Marshal(map[string]any{
		"title":        question,
		"description":  strings.TrimSpace(contextText),
		"hub_id":       hub.ID,
		"hub_slug":     hub.Slug,
		"slot_key":     slotKey,
		"source_agent": sourceAgent,
	})
	if err != nil {
		return nil, err
	}
	notif := &model.Notification{
		ID:         generateID(),
		Audience:   model.AudienceHubMember,
		HubID:      hub.ID,
		Kind:       model.NotificationKindDecisionGate,
		Status:     model.NotificationStatusPending,
		Priority:   1,
		SourceKind: model.NotificationSourceDecisionGate,
		// Hub-qualified: even if two hubs ever produced the same slot
		// key, their notification rows stay distinct and a resolve can
		// never reach across hubs.
		SourceID: decisionGateSourceID(hub.ID, slotKey),
		Payload:    notifPayload,
		CreatedAt:  time.Now().UTC(),
	}
	if inserted, err := s.CreateNotificationIfAbsent(notif); err == nil && inserted {
		events.PublishNotificationCreated(context.Background(), publisher, notif)
	}
	return slot, nil
}

// CreateDecisionGate is POST /v1/hubs/{id}/board/decision-gate — the
// REST surface behind the SDK/CLI (and thereby the local MCP server).
func (h *BoardsHandler) CreateDecisionGate(w http.ResponseWriter, r *http.Request) {
	hubID, userID, ok := h.requireHubMember(w, r)
	if !ok {
		return
	}
	var req struct {
		Question    string   `json:"question"`
		Context     string   `json:"context"`
		Options     []string `json:"options"`
		SourceAgent string   `json:"source_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	sourceAgent := req.SourceAgent
	if sourceAgent == "" {
		sourceAgent = resolveAuthSourceAgent(r)
	}
	slot, err := createDecisionGate(h.store, h.events, hub, req.Question, req.Context, sourceAgent, req.Options)
	if errors.Is(err, errGateLimit) {
		writeError(w, http.StatusConflict, "gate_limit",
			fmt.Sprintf("This board already has %d open decisions. Wait for the user to resolve one.", maxLiveDecisionGates))
		return
	}
	if errors.Is(err, errGateInvalid) {
		writeError(w, http.StatusBadRequest, "invalid_gate", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	_ = userID
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: map[string]any{"slot": slot}})
}

// resolveDecisionGateSideEffects runs after a gate slot resolves with
// a choice: writes the decision memory and settles the companion
// notification. Failures are logged, not fatal — the slot resolution
// (the user's intent) already committed.
func (h *BoardsHandler) resolveDecisionGateSideEffects(
	r *http.Request,
	hub *model.Hub,
	slot *model.BoardSlot,
	userID, choiceID string,
) {
	var payload model.BoardDecisionGatePayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		return
	}
	choiceLabel := choiceID
	for _, opt := range payload.Options {
		if opt.ID == choiceID {
			choiceLabel = opt.Label
			break
		}
	}

	// The write-back: decision becomes a recallable hub memory.
	content := fmt.Sprintf("Decision: %s\n\nChosen: %s", payload.Question, choiceLabel)
	if payload.Context != "" {
		content += "\n\nContext from " + orUnknownAgent(payload.SourceAgent) + ": " + payload.Context
	}
	memory := &model.Memory{
		ID:          generateID(),
		OwnerID:     userID,
		HubID:       hub.ID,
		Title:       "Decision: " + payload.Question,
		Content:     content,
		ContentType: "text",
		Kind:        model.MemoryKindRationale,
		Source:      "board/decision-gate",
		SourceAgent: payload.SourceAgent,
		State:       "active",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := h.store.CreateMemory(memory); err == nil && h.enqueue != nil {
		h.enqueue(memory.ID, userID, model.PushRequest{
			Content:     content,
			Title:       memory.Title,
			Kind:        model.MemoryKindRationale,
			ContentType: "text",
			Source:      memory.Source,
			SourceAgent: payload.SourceAgent,
		})
	}

	// Settle the ping so the inbox row doesn't outlive the decision.
	hubIDs := GetAccessibleHubIDs(r)
	if n, err := h.store.GetNotificationBySource(r.Context(), model.NotificationSourceDecisionGate,
		decisionGateSourceID(hub.ID, slot.SlotKey)); err == nil && n != nil {
		_ = h.store.ResolveNotification(r.Context(), n.ID, userID, hubIDs, model.NotificationResolution("applied"))
		if updated, err := h.store.GetNotification(r.Context(), n.ID, userID, hubIDs); err == nil {
			events.PublishNotificationResolved(r.Context(), h.events, updated)
		}
	}
}

// decisionGateSourceID keys a gate's notification by (hub, slot) so
// the row is unique per hub regardless of slot-key entropy.
func decisionGateSourceID(hubID, slotKey string) string {
	return hubID + ":" + slotKey
}

func orUnknownAgent(agent string) string {
	if agent == "" {
		return "agent"
	}
	return agent
}
