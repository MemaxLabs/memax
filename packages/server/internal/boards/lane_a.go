// Package boards holds the pulse-board producers (plan 25). Lane A is
// the deterministic lane: pure queries over existing data, zero LLM
// calls, safe to run for every hub every night. Lane B (agentic
// synthesis) lands in P2 behind the dream runtime.
package boards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	// Counts collapse into ONE strip ("z-" sorts it to the bottom —
	// it's the least interesting thing on the board); the capsule
	// stays a real card because it surfaces actual content, not a
	// number.
	slotKeyCapsule  = "c-capsule"
	slotKeyActivity = "z-activity"

	// Retired keys from the four-card era; the producer deletes them
	// so upgraded boards don't keep stale cards forever.
	slotKeyTrace = "a-trace"
	slotKeyPulse = "b-pulse"
	slotKeyWeek  = "d-week"
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

	// One-time cleanup of the retired per-metric cards.
	for _, stale := range []string{slotKeyTrace, slotKeyPulse, slotKeyWeek} {
		if err := p.store.DeleteBoardSlot(board.ID, stale); err != nil {
			return fmt.Errorf("boards: drop retired slot %s: %w", stale, err)
		}
	}

	if err := p.refreshCapsule(board.ID, hubID, now); err != nil {
		return err
	}
	return p.refreshActivity(board.ID, hubID, now)
}

// refreshActivity folds agent traces, topic movement and the week
// diff into a single line. Each was its own card once; three cards of
// counters pushed the cards that actually say something off the
// screen, which is the opposite of what the board is for.
func (p *Producer) refreshActivity(boardID, hubID string, now time.Time) error {
	agents, err := p.store.ListRecentAgentActivityByHub(hubID, now.Add(-traceWindowHours*time.Hour))
	if err != nil {
		return fmt.Errorf("boards: activity agents: %w", err)
	}
	topics, err := p.store.ListTopicActivityByHub(hubID, now.Add(-pulseWindowDays*24*time.Hour), pulseTopicLimit)
	if err != nil {
		return fmt.Errorf("boards: activity topics: %w", err)
	}
	thisWeek, err := p.store.CountMemoriesInHubRange(hubID, now.Add(-weekWindow), now)
	if err != nil {
		return fmt.Errorf("boards: week count: %w", err)
	}
	lastWeek, err := p.store.CountMemoriesInHubRange(hubID, now.Add(-2*weekWindow), now.Add(-weekWindow))
	if err != nil {
		return fmt.Errorf("boards: last-week count: %w", err)
	}

	recent := 0
	for _, a := range agents {
		recent += a.Count
	}
	if recent == 0 && len(topics) == 0 && thisWeek == 0 && lastWeek == 0 {
		return p.store.DeleteBoardSlot(boardID, slotKeyActivity)
	}

	return p.writeSlotIfChanged(&model.BoardSlot{
		BoardID: boardID,
		SlotKey: slotKeyActivity,
		Kind:    model.BoardKindActivity,
		Title: fmt.Sprintf("%d memories in the last %dh · %d this week",
			recent, traceWindowHours, thisWeek),
		Payload: mustJSON(model.BoardActivityPayload{
			WindowHours: traceWindowHours,
			WindowDays:  pulseWindowDays,
			Agents:      agents,
			Topics:      topics,
			ThisWeek:    thisWeek,
			LastWeek:    lastWeek,
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

// writeSlotIfChanged upserts only when kind, title, payload, or
// citations differ from the stored slot. An unchanged refresh is a
// no-op so it doesn't reset a resolved card back to fresh. Payloads
// compare semantically, not byte-wise: Postgres jsonb normalizes key
// order and whitespace, so bytes read back never equal json.Marshal
// output even for identical content.
func (p *Producer) writeSlotIfChanged(slot *model.BoardSlot) error {
	existing, err := p.store.GetBoardSlot(slot.BoardID, slot.SlotKey)
	switch {
	case err == nil:
		if existing.Kind == slot.Kind && existing.Title == slot.Title &&
			jsonEqual(existing.Payload, slot.Payload) &&
			equalStringSlices(existing.CiteMemoryIDs, slot.CiteMemoryIDs) {
			return nil
		}
	case errors.Is(err, store.ErrBoardSlotNotFound):
		// First write for this slot — proceed to insert.
	default:
		// Fail closed: a transient read error must not fall through to
		// an upsert that would wipe a member's resolution receipt.
		return fmt.Errorf("boards: read slot %s/%s: %w", slot.BoardID, slot.SlotKey, err)
	}
	return p.store.UpsertBoardSlot(slot)
}

// jsonEqual compares two JSON documents structurally.
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
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
