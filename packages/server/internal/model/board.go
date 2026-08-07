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
	// BoardSlotActionChoose resolves a decision gate with one of its
	// option ids (carried in BoardSlotResolution.Verdict).
	BoardSlotActionChoose = "choose"

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
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// ContentUpdatedAt moves only when a producer writes content —
	// user actions bump UpdatedAt, so only this field can answer
	// "how long since this card was actually regenerated".
	ContentUpdatedAt time.Time `json:"content_updated_at"`
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
	// BoardKindActivity is the one-line activity summary: agent
	// traces, topic movement and the week diff folded together.
	// These are COUNTS — true but low-value-per-pixel — so they get a
	// single strip, not three cards. Cards are for things worth
	// stopping on.
	BoardKindActivity = "activity"

	// Retired kinds, kept only so the producer can clean up slots
	// written by earlier versions. `capsule` was calendar-driven — the
	// one card with no trigger, surfacing a memory from a year ago
	// whether or not anything happened. New users never saw it; old
	// users got a coin flip. The genuine time-gap payoff lives in
	// `echo`, where the span is causal instead of coincidental.
	BoardKindTrace   = "trace"
	BoardKindPulse   = "pulse"
	BoardKindWeek    = "week"
	BoardKindCapsule = "capsule"
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

// BoardActivityPayload is the folded activity line. Everything here is
// countable fact; the renderer shows one summary line and reveals the
// breakdown on demand.
type BoardActivityPayload struct {
	Description string               `json:"description,omitempty"`
	WindowHours int                  `json:"window_hours"`
	WindowDays  int                  `json:"window_days"`
	Agents      []BoardAgentActivity `json:"agents,omitempty"`
	Topics      []BoardTopicActivity `json:"topics,omitempty"`
	ThisWeek    int                  `json:"this_week"`
	LastWeek    int                  `json:"last_week"`
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

// --- Lane B kinds + payloads (P2) ---
//
// Lane B cards are agent-synthesized during the dream cycle. The
// structural contract is stricter than Lane A because the content is
// generated: every card must quote its receipts (CiteMemoryIDs) and
// the producer drops any card that fails the citation floor — a
// synthesized claim with no receipts is exactly the Barnum statement
// the design forbids.

const (
	BoardKindDreamlog     = "dreamlog"      // 梦记 — first-person account of last night's dream work
	BoardKindEcho         = "echo"          // 回声 — an old question meets a new answer
	BoardKindThread       = "thread"        // 暗线 — two memories that may be one idea
	BoardKindPattern      = "pattern"       // 未观察模式 — a habit visible in the data, invisible to the user
	BoardKindDecisionGate = "decision_gate" // 等你 — an agent needs the user to decide
	BoardKindNextUp       = "nextup"        // 接下来 — predicted next actions, grounded in open loops

	// Team-native kinds. These exist ONLY because several people share a
	// hub — each one is a claim about the group, not about one person,
	// and each is validated against the OWNERS of its cited memories
	// (see LaneBOwnerRule). A "team" card whose receipts all come from
	// one person is a lie about collaboration, so the producer drops it.
	BoardKindConsensusGap = "consensus_gap" // 共识缺口 — two members understand the same thing differently
	BoardKindTeamEcho     = "team_echo"     // 团队回声 — A's question, B's answer, never connected
	BoardKindWhoKnows     = "who_knows"     // 谁知道这个 — who to ask about a topic

	// Lane B slot keys. "0-" sorts the dream log first, "0n-" the
	// predictive next-up card right after it ("0-" < "0n-" because
	// '-' < 'n'), "1-" the rotating wow card, and decision gates prefix
	// "2g-" so they sit above the Lane A band ("a-"…"d-"). Custom
	// boards have no dreamlog and may carry a second synthesized card
	// in "2-wow" ("2-" still sorts before "2g-").
	BoardSlotKeyDreamlog   = "0-dream"
	BoardSlotKeyNextUp     = "0n-next"
	BoardSlotKeyWow        = "1-wow"
	BoardSlotKeyWow2       = "2-wow"
	BoardSlotKeyGatePrefix = "2g-"

	// Retired synthesized kinds. `openq` asked the same question
	// `nextup` answers — an open loop — but stopped at naming it;
	// `musing` was the loosest-defined kind and the likeliest source
	// of Barnum text, and what it aimed at (what's growing, what's
	// abandoned) is either provable by `pattern` or countable outright.
	BoardKindOpenQuestion = "openq"
	BoardKindMusing       = "musing"
)

// WowKinds is the nightly rotation pool for the single wow slot on a
// PERSONAL hub — three lenses, all of them about one person.
var WowKinds = []string{
	BoardKindEcho,
	BoardKindThread,
	BoardKindPattern,
}

// TeamWowKinds is the rotation pool for a TEAM hub: the personal three
// (a shared hub still holds one person's echoes) plus the three kinds
// that only mean anything when several people write into the same hub.
// Six lenses instead of three, so a team board says something about
// the team roughly half the time.
var TeamWowKinds = []string{
	BoardKindEcho,
	BoardKindThread,
	BoardKindPattern,
	BoardKindConsensusGap,
	BoardKindTeamEcho,
	BoardKindWhoKnows,
}

// HubTypeTeam is the Hub.HubType value that unlocks the team kinds.
const HubTypeTeam = "team"

// WowKindsForHub returns the rotation pool for a hub type. Team kinds
// are never offered to a personal hub — on a hub with one writer they
// would either be unfillable or, worse, fabricated.
func WowKindsForHub(hubType string) []string {
	if hubType == HubTypeTeam {
		return TeamWowKinds
	}
	return WowKinds
}

// IsTeamWowKind reports whether a kind is one of the team-only lenses.
func IsTeamWowKind(kind string) bool {
	switch kind {
	case BoardKindConsensusGap, BoardKindTeamEcho, BoardKindWhoKnows:
		return true
	}
	return false
}

// BoardQuoteRef is a quoted memory inside a Lane B card: the id makes
// it navigable, When/Excerpt make it renderable without a fetch.
//
// Author is the display name of the member who WROTE the memory,
// filled only on team-hub cards where "who said this" is the point.
// It comes from the hub roster keyed by the memory's owner_id, never
// from the model — attribution is the one thing a team card must not
// guess. Empty on personal hubs, where there is only one author.
type BoardQuoteRef struct {
	MemoryID string `json:"memory_id"`
	When     string `json:"when,omitempty"` // RFC3339
	Excerpt  string `json:"excerpt"`
	Author   string `json:"author,omitempty"`
}

// BoardDreamlogPayload — 梦记. Body is memax speaking in first person
// about what it found while organizing; plain text.
type BoardDreamlogPayload struct {
	Description string `json:"description,omitempty"`
	Body        string `json:"body"`
}

// BoardEchoPayload — 回声. Then (the old question) and Now (the new
// answer) render as a quote pair joined by the signature mark.
type BoardEchoPayload struct {
	Description string        `json:"description,omitempty"`
	Body        string        `json:"body,omitempty"`
	Then        BoardQuoteRef `json:"then"`
	Now         BoardQuoteRef `json:"now"`
}

// BoardWowPayload is the shared shape for thread/openq/pattern/musing:
// a first-person body plus the quoted receipts behind it.
type BoardWowPayload struct {
	Description string          `json:"description,omitempty"`
	Body        string          `json:"body"`
	Quotes      []BoardQuoteRef `json:"quotes,omitempty"`
}

// BoardConsensusPayload — 共识缺口. Two members recorded contradictory
// understandings of the same thing. Sides is exactly two quotes, one
// per member, and the producer guarantees they come from DIFFERENT
// owners — that difference IS the card. Distinct from the dream's
// contradiction detection, which compares memory CONTENT: here each
// side may be internally consistent, and the gap is between people.
type BoardConsensusPayload struct {
	Description string          `json:"description,omitempty"`
	Body        string          `json:"body"`
	Sides       []BoardQuoteRef `json:"sides"`
}

// BoardWhoKnowsPayload — 谁知道这个. Routing, not insight: Holder is the
// display name of the member whose memories dominate the topic, and
// the quotes are that member's own memories (all one owner — that is
// the evidence they're the holder). Embeds the wow shape because the
// body + receipts layout is identical; only the name is new.
type BoardWhoKnowsPayload struct {
	BoardWowPayload
	Holder string `json:"holder,omitempty"`
}

// BoardNextUpItem is one predicted action on the 接下来 card: an
// imperative title, a one-line why, and the verbatim quote(s) proving
// the loop is actually open. The producer drops any item that ends up
// with zero verified quotes — a prediction without receipts is exactly
// the horoscope the design forbids.
type BoardNextUpItem struct {
	Title  string          `json:"title"`
	Why    string          `json:"why,omitempty"`
	Quotes []BoardQuoteRef `json:"quotes"`
}

// BoardNextUpPayload — 接下来. 1-3 items, each individually grounded;
// the card ships only when at least one item survives validation.
type BoardNextUpPayload struct {
	Description string            `json:"description,omitempty"`
	Items       []BoardNextUpItem `json:"items"`
}

// BoardDecisionOption is one choice on a decision gate.
type BoardDecisionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// BoardDecisionGatePayload — 等你. Created by memax_request_decision;
// resolving with a choice writes the decision back as a hub memory so
// the requesting agent can recall it.
type BoardDecisionGatePayload struct {
	Description string                `json:"description,omitempty"`
	Question    string                `json:"question"`
	Context     string                `json:"context,omitempty"`
	Options     []BoardDecisionOption `json:"options"`
	SourceAgent string                `json:"source_agent,omitempty"`
}

// laneBCitationFloor is the minimum receipts per synthesized kind.
// Kinds not listed have no floor (decision gates cite nothing — the
// agent's question is the content).
var laneBCitationFloor = map[string]int{
	BoardKindEcho:     2,
	BoardKindThread:   2,
	BoardKindPattern:  3,
	BoardKindDreamlog: 0,
	// nextup's real gate is per-ITEM (every item needs ≥1 verified
	// quote — see buildNextUpSlot); the card-level floor of 1 follows
	// from "at least one item survives".
	BoardKindNextUp: 1,
	// Team kinds: two receipts minimum, and the OWNER rule below is the
	// half of the gate that makes them team claims rather than personal
	// ones dressed up in plural pronouns.
	BoardKindConsensusGap: 2,
	BoardKindTeamEcho:     2,
	BoardKindWhoKnows:     2,
}

// LaneBCitationFloor returns the citation minimum for a kind and
// whether the kind is a Lane B synthesized kind at all.
func LaneBCitationFloor(kind string) (int, bool) {
	floor, ok := laneBCitationFloor[kind]
	return floor, ok
}

// BoardOwnerRule is the owner composition a kind's verified citations
// must satisfy. Counting receipts is not enough for the team kinds: a
// "gap between two members" quoting one person twice, or a "who to
// ask" quoting three different people, is factually wrong about the
// only thing the card claims.
type BoardOwnerRule int

const (
	// BoardOwnersAny — no constraint (every personal kind).
	BoardOwnersAny BoardOwnerRule = iota
	// BoardOwnersDistinct — the quotes must span ≥2 distinct owners.
	BoardOwnersDistinct
	// BoardOwnersSame — every quote must come from ONE owner.
	BoardOwnersSame
)

var laneBOwnerRule = map[string]BoardOwnerRule{
	BoardKindConsensusGap: BoardOwnersDistinct,
	BoardKindTeamEcho:     BoardOwnersDistinct,
	BoardKindWhoKnows:     BoardOwnersSame,
}

// LaneBOwnerRule returns the owner-composition rule for a kind.
func LaneBOwnerRule(kind string) BoardOwnerRule {
	return laneBOwnerRule[kind]
}

// SatisfiesOwnerRule reports whether a set of citation owner ids meets
// the rule. Empty input never satisfies a team rule — a card with no
// attributable owners cannot be making a claim about people.
func SatisfiesOwnerRule(rule BoardOwnerRule, ownerIDs []string) bool {
	if rule == BoardOwnersAny {
		return true
	}
	distinct := make(map[string]bool, len(ownerIDs))
	for _, id := range ownerIDs {
		if id == "" {
			// An unattributable quote can't prove either rule.
			return false
		}
		distinct[id] = true
	}
	switch rule {
	case BoardOwnersDistinct:
		return len(distinct) >= 2
	case BoardOwnersSame:
		return len(distinct) == 1
	}
	return true
}
