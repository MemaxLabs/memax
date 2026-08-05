// Package boards holds the pulse-board producers (plan 25). Lane A is
// the deterministic lane: pure queries over existing data, zero LLM
// calls, safe to run for every hub every night. Lane B (agentic
// synthesis) lands in P2 behind the dream runtime.
package boards

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

const (
	traceWindowHours = 24
	pulseWindowDays  = 7
	pulseTopicLimit  = 5
	capsuleTolerance = 7 * 24 * time.Hour
	weekWindow       = 7 * 24 * time.Hour

	// Slot keys carry an ordering prefix: ListBoardSlots sorts by
	// slot_key, so these literals fix the board layout top-to-bottom.
	slotKeyTrace   = "a-trace"
	slotKeyPulse   = "b-pulse"
	slotKeyCapsule = "c-capsule"
	slotKeyWeek    = "d-week"
)

// Producer computes Lane A cards for one hub's system board.
type Producer struct {
	store store.Store
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
}

func NewProducer(s store.Store) *Producer {
	return &Producer{store: s, now: time.Now}
}

// RefreshHubBoard recomputes every Lane A slot for the hub. Kinds with
// no data remove their slot (an empty card is worse than no card);
// kinds whose content is unchanged since the last run are skipped so a
// member's resolved receipt isn't reset by a no-op refresh.
func (p *Producer) RefreshHubBoard(ctx context.Context, hubID string) error {
	hub, err := p.store.GetHub(hubID)
	if err != nil {
		return fmt.Errorf("boards: get hub %s: %w", hubID, err)
	}
	board, err := p.store.GetOrCreateSystemBoard(hubID, hub.OwnerID)
	if err != nil {
		return fmt.Errorf("boards: get system board for hub %s: %w", hubID, err)
	}
	if board.Status != model.BoardStatusActive {
		return nil
	}
	now := p.now().UTC()

	if err := p.refreshTrace(board.ID, hubID, now); err != nil {
		return err
	}
	if err := p.refreshPulse(board.ID, hubID, now); err != nil {
		return err
	}
	if err := p.refreshCapsule(board.ID, hubID, now); err != nil {
		return err
	}
	return p.refreshWeek(board.ID, hubID, now)
}

func (p *Producer) refreshTrace(boardID, hubID string, now time.Time) error {
	activity, err := p.store.ListRecentAgentActivityByHub(hubID, now.Add(-traceWindowHours*time.Hour))
	if err != nil {
		return fmt.Errorf("boards: trace query: %w", err)
	}
	if len(activity) == 0 {
		return p.store.DeleteBoardSlot(boardID, slotKeyTrace)
	}
	total := 0
	for _, a := range activity {
		total += a.Count
	}
	return p.writeSlotIfChanged(&model.BoardSlot{
		BoardID: boardID,
		SlotKey: slotKeyTrace,
		Kind:    model.BoardKindTrace,
		Title:   fmt.Sprintf("Agent activity: %d memories in the last %dh", total, traceWindowHours),
		Payload: mustJSON(model.BoardTracePayload{
			WindowHours: traceWindowHours,
			Agents:      activity,
		}),
	})
}

func (p *Producer) refreshPulse(boardID, hubID string, now time.Time) error {
	topics, err := p.store.ListTopicActivityByHub(hubID, now.Add(-pulseWindowDays*24*time.Hour), pulseTopicLimit)
	if err != nil {
		return fmt.Errorf("boards: pulse query: %w", err)
	}
	if len(topics) == 0 {
		return p.store.DeleteBoardSlot(boardID, slotKeyPulse)
	}
	return p.writeSlotIfChanged(&model.BoardSlot{
		BoardID: boardID,
		SlotKey: slotKeyPulse,
		Kind:    model.BoardKindPulse,
		Title:   fmt.Sprintf("Topic pulse: %d active topics this week", len(topics)),
		Payload: mustJSON(model.BoardPulsePayload{
			WindowDays: pulseWindowDays,
			Topics:     topics,
		}),
	})
}

func (p *Producer) refreshCapsule(boardID, hubID string, now time.Time) error {
	mem, err := p.store.GetMemoryNear(hubID, now.AddDate(-1, 0, 0), capsuleTolerance)
	if err != nil {
		return fmt.Errorf("boards: capsule query: %w", err)
	}
	if mem == nil {
		return p.store.DeleteBoardSlot(boardID, slotKeyCapsule)
	}
	quote := mem.Title
	if quote == "" {
		quote = mem.Summary
	}
	if quote == "" {
		return p.store.DeleteBoardSlot(boardID, slotKeyCapsule)
	}
	return p.writeSlotIfChanged(&model.BoardSlot{
		BoardID:       boardID,
		SlotKey:       slotKeyCapsule,
		Kind:          model.BoardKindCapsule,
		Title:         "One year ago: " + truncateRunes(quote, 120),
		CiteMemoryIDs: []string{mem.ID},
		Payload: mustJSON(model.BoardCapsulePayload{
			MemoryID: mem.ID,
			When:     mem.CreatedAt.UTC().Format(time.RFC3339),
			Quote:    quote,
		}),
	})
}

func (p *Producer) refreshWeek(boardID, hubID string, now time.Time) error {
	thisWeek, err := p.store.CountMemoriesInHubSince(hubID, now.Add(-weekWindow))
	if err != nil {
		return fmt.Errorf("boards: week count: %w", err)
	}
	twoWeeks, err := p.store.CountMemoriesInHubSince(hubID, now.Add(-2*weekWindow))
	if err != nil {
		return fmt.Errorf("boards: two-week count: %w", err)
	}
	lastWeek := twoWeeks - thisWeek
	if thisWeek == 0 && lastWeek == 0 {
		return p.store.DeleteBoardSlot(boardID, slotKeyWeek)
	}
	return p.writeSlotIfChanged(&model.BoardSlot{
		BoardID: boardID,
		SlotKey: slotKeyWeek,
		Kind:    model.BoardKindWeek,
		Title:   fmt.Sprintf("This week: %d memories (last week %d)", thisWeek, lastWeek),
		Payload: mustJSON(model.BoardWeekPayload{
			ThisWeek: thisWeek,
			LastWeek: lastWeek,
		}),
	})
}

// writeSlotIfChanged upserts only when kind, title, payload, or
// citations differ from the stored slot. A byte-identical refresh is a
// no-op so it doesn't reset a resolved card back to fresh.
func (p *Producer) writeSlotIfChanged(slot *model.BoardSlot) error {
	existing, err := p.store.GetBoardSlot(slot.BoardID, slot.SlotKey)
	if err == nil && existing.Kind == slot.Kind && existing.Title == slot.Title &&
		bytes.Equal(existing.Payload, slot.Payload) &&
		equalStringSlices(existing.CiteMemoryIDs, slot.CiteMemoryIDs) {
		return nil
	}
	return p.store.UpsertBoardSlot(slot)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		// Payload structs are plain data — a marshal failure is a
		// programming error, not a runtime condition.
		panic(fmt.Sprintf("boards: marshal payload: %v", err))
	}
	return data
}
