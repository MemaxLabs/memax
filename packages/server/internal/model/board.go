package model

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

// Pulse boards (plan 25). A board is a per-hub surface of named slots;
// producers upsert card content into slots with replace semantics, so
// the board is a fixed-size window that refreshes rather than a feed
// that grows. Cards reuse the plan-18 item contract: every user-facing
// text field is a plain string (no HTML/markdown/placeholders) because
// the client renders `title` + payload text literally through the
// unknown-kind fallback when a producer ships ahead of its renderer.

const (
	BoardKindSystem = "system"
	BoardKindCustom = "custom"

	// BoardStatusActive — normal operation, included in producer runs.
	// BoardStatusCooking — configured but awaiting its first dream run
	// (the 酝酿中 state custom boards show after creation).
	// BoardStatusPaused — excluded from producer runs, still viewable.
	BoardStatusActive  = "active"
	BoardStatusCooking = "cooking"
	BoardStatusPaused  = "paused"
)

// Slot lifecycle. fresh → seen is a passive view transition (not yet
// wired in P0); fresh/seen → resolved|dismissed happens through the
// resolve endpoint and is terminal for that card — the slot only
// leaves a terminal state by being replaced with new content, which
// resets it to fresh.
const (
	BoardSlotStateFresh     = "fresh"
	BoardSlotStateSeen      = "seen"
	BoardSlotStateResolved  = "resolved"
	BoardSlotStateDismissed = "dismissed"
)

// Resolve actions accepted by the slot resolve endpoint, and the
// feedback verdicts the `feedback` action carries.
const (
	BoardSlotActionAck      = "ack"
	BoardSlotActionDismiss  = "dismiss"
	BoardSlotActionFeedback = "feedback"

	BoardFeedbackAccurate   = "accurate"
	BoardFeedbackInaccurate = "inaccurate"
)

// Board is the container row. Instruction is the board-as-instruction
// contract: empty for system boards (behavior is code-defined), the
// user's natural-language brief for custom boards (P4) consumed by the
// dream synthesis phase (P2).
type Board struct {
	ID          string    `json:"id"`
	HubID       string    `json:"hub_id"`
	CreatedBy   string    `json:"created_by"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title,omitempty"`
	Instruction string    `json:"instruction,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BoardSlotResolution is the receipt stamped on a slot when it leaves
// fresh/seen through the resolve endpoint. Stored as jsonb so future
// actions can carry extra fields without a migration.
type BoardSlotResolution struct {
	Action     string    `json:"action"`
	Verdict    string    `json:"verdict,omitempty"`
	ResolvedBy string    `json:"resolved_by"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// BoardSlot is one occupied slot. Payload shape varies by Kind —
// producers encode a per-kind struct; unknown kinds MUST still carry
// enough plain text (Title + payload "description") for the fallback
// renderer. CiteMemoryIDs are the receipts that make the card
// auditable and power the CiteChip → memory-detail jump.
type BoardSlot struct {
	ID            string               `json:"id"`
	BoardID       string               `json:"board_id"`
	SlotKey       string               `json:"slot_key"`
	Kind          string               `json:"kind"`
	Title         string               `json:"title"`
	Payload       json.RawMessage      `json:"payload,omitempty"`
	CiteMemoryIDs []string             `json:"cite_memory_ids,omitempty"`
	State         string               `json:"state"`
	Resolution    *BoardSlotResolution `json:"resolution,omitempty"`
	DreamRunID    string               `json:"dream_run_id,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// BoardFeedback is the append-only 准/不准 snapshot. It outlives the
// slot content it judged (slots are replaced in place) and is read by
// later synthesis runs as a first-class quality signal.
type BoardFeedback struct {
	ID            string    `json:"id"`
	BoardID       string    `json:"board_id"`
	SlotKey       string    `json:"slot_key"`
	CardKind      string    `json:"card_kind"`
	CardTitle     string    `json:"card_title"`
	Verdict       string    `json:"verdict"`
	UserID        string    `json:"user_id"`
	CiteMemoryIDs []string  `json:"cite_memory_ids,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// --- Lane A payloads (P1) ---
//
// Deterministic producers write structured data, not prose: the web
// renderer composes localized copy from these fields, and slot.title
// carries a plain-English summary only for the unknown-kind fallback.
// Board kind constants double as slot-key prefixes ("a-trace" …) so
// ListBoardSlots' slot_key ordering fixes the board layout.

const (
	BoardKindTrace   = "trace"   // 行迹 — agent activity in the window
	BoardKindPulse   = "pulse"   // 项目脉搏 — most active topics
	BoardKindCapsule = "capsule" // 时间胶囊 — a memory from ~1 year ago
	BoardKindWeek    = "week"    // 周对比 — this week vs last week
)

// BoardAgentItem is one concrete memory reference inside a trace row —
// the drill-down that lets a member verify what an agent actually
// wrote instead of trusting a count.
type BoardAgentItem struct {
	MemoryID string `json:"memory_id"`
	Title    string `json:"title"`
}

// BoardAgentActivity is one agent's line in the trace card. LatestTitle
// is the newest memory title from that agent — plain user content,
// quoted verbatim by the renderer. Items carries the newest few
// memories (id + title) so each row expands to the receipts behind
// the number.
type BoardAgentActivity struct {
	Slug        string           `json:"slug"`
	DisplayName string           `json:"display_name,omitempty"`
	Icon        string           `json:"icon,omitempty"`
	Count       int              `json:"count"`
	LatestTitle string           `json:"latest_title,omitempty"`
	Items       []BoardAgentItem `json:"items,omitempty"`
}

type BoardTracePayload struct {
	Description string               `json:"description,omitempty"`
	WindowHours int                  `json:"window_hours"`
	Agents      []BoardAgentActivity `json:"agents"`
}

// BoardTopicActivity is one topic's line in the pulse card.
type BoardTopicActivity struct {
	TopicID      string `json:"topic_id"`
	Name         string `json:"name"`
	Icon         string `json:"icon,omitempty"`
	RecentCount  int    `json:"recent_count"`
	Contributors int    `json:"contributors,omitempty"`
}

type BoardPulsePayload struct {
	Description string               `json:"description,omitempty"`
	WindowDays  int                  `json:"window_days"`
	Topics      []BoardTopicActivity `json:"topics"`
}

// BoardCapsulePayload quotes one memory from roughly a year ago. When
// is the memory's created_at in RFC3339; the renderer formats it.
type BoardCapsulePayload struct {
	Description string `json:"description,omitempty"`
	MemoryID    string `json:"memory_id"`
	When        string `json:"when"`
	Quote       string `json:"quote"`
}

type BoardWeekPayload struct {
	Description string `json:"description,omitempty"`
	ThisWeek    int    `json:"this_week"`
	LastWeek    int    `json:"last_week"`
}

// Producer-side guardrails. Slot writes go through ValidateBoardSlot
// before hitting the store so a buggy producer can't ship an
// unrenderable or oversized card. The caps mirror the plan-18
// validator philosophy: cheap structural checks at the write site,
// semantic quality gates (citation floors, anti-Barnum) at the
// producer layer.
const (
	maxBoardSlotTitleRunes  = 200
	maxBoardSlotKeyRunes    = 64
	maxBoardSlotPayloadSize = 32 * 1024
	maxBoardSlotCitations   = 20
)

// ValidateBoardSlot enforces the structural card contract on every
// slot write. Returns a descriptive error naming the failing field.
func ValidateBoardSlot(s *BoardSlot) error {
	if s.BoardID == "" {
		return fmt.Errorf("board slot: board_id is required")
	}
	if s.SlotKey == "" || utf8.RuneCountInString(s.SlotKey) > maxBoardSlotKeyRunes {
		return fmt.Errorf("board slot: slot_key must be 1..%d chars", maxBoardSlotKeyRunes)
	}
	if s.Kind == "" {
		return fmt.Errorf("board slot: kind is required")
	}
	if s.Title == "" || utf8.RuneCountInString(s.Title) > maxBoardSlotTitleRunes {
		return fmt.Errorf("board slot: title must be 1..%d chars (fallback renderer prints it literally)", maxBoardSlotTitleRunes)
	}
	if len(s.Payload) > maxBoardSlotPayloadSize {
		return fmt.Errorf("board slot: payload exceeds %d bytes", maxBoardSlotPayloadSize)
	}
	if len(s.Payload) > 0 && !json.Valid(s.Payload) {
		return fmt.Errorf("board slot: payload is not valid JSON")
	}
	if len(s.CiteMemoryIDs) > maxBoardSlotCitations {
		return fmt.Errorf("board slot: cite_memory_ids exceeds %d entries", maxBoardSlotCitations)
	}
	return nil
}
