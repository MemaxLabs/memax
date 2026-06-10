package onboarding

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestParseQueryableTrigger(t *testing.T) {
	t.Parallel()
	cases := []struct {
		trigger   string
		bucket    string
		threshold int
		ok        bool
	}{
		{"memory_count_gte:5", "memories", 5, true},
		{"memory_count_gte:1", "memories", 1, true},
		{"configs_sync", "agents", 1, true},
		{"hub_event", "team_hubs", 1, true},
		// non-queryable: ask is stateless, user_click is UI gesture,
		// empty triggers welcome which has no underlying signal.
		{"ask_count_gte:1", "", 0, false},
		{"user_click", "", 0, false},
		{"", "", 0, false},
		// invalid forms — ok=false
		{"memory_count_gte:", "", 0, false},
		{"memory_count_gte:abc", "", 0, false},
		{"memory_count_gte:0", "", 0, false},
		{"memory_count_gte:-3", "", 0, false},
		{"unknown_trigger", "", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.trigger, func(t *testing.T) {
			t.Parallel()
			b, n, ok := parseQueryableTrigger(tc.trigger)
			if ok != tc.ok {
				t.Errorf("ok mismatch: got %v want %v", ok, tc.ok)
			}
			if b != tc.bucket {
				t.Errorf("bucket mismatch: got %q want %q", b, tc.bucket)
			}
			if n != tc.threshold {
				t.Errorf("threshold mismatch: got %d want %d", n, tc.threshold)
			}
		})
	}
}

// TestMaterialize_NilSafe — nil store / empty user → no ticks, no
// panic. The handler hot path calls MaterializeChecklist on every
// notifications GET; defensive nil handling is non-negotiable.
func TestMaterialize_NilSafe(t *testing.T) {
	t.Parallel()
	p := model.ChecklistPayload{Items: []model.ChecklistItem{
		{Item: model.Item{ID: "first_memory"}, Trigger: "memory_count_gte:1"},
	}}
	if ticks := MaterializeChecklist(context.Background(), nil, "user-1", p); ticks != nil {
		t.Errorf("nil store should return nil ticks, got %v", ticks)
	}
	if ticks := MaterializeChecklist(context.Background(), store.NewInMemoryStore(), "", p); ticks != nil {
		t.Errorf("empty userID should return nil ticks, got %v", ticks)
	}
	if ticks := MaterializeChecklist(context.Background(), store.NewInMemoryStore(), "   ", p); ticks != nil {
		t.Errorf("whitespace userID should return nil ticks, got %v", ticks)
	}
}

// TestMaterialize_FastPath — when every queryable item is already
// complete or there are no queryable items, the materializer must
// NOT issue count queries. Past-onboarding users hit this path on
// every notifications read.
func TestMaterialize_FastPath(t *testing.T) {
	t.Parallel()
	// No queryable triggers — welcome / first_dream are non-queryable.
	p := model.ChecklistPayload{Items: []model.ChecklistItem{
		{Item: model.Item{ID: "welcome"}, Trigger: ""},
		{Item: model.Item{ID: "first_dream"}, Trigger: "user_click"},
	}}
	st := store.NewInMemoryStore()
	if ticks := MaterializeChecklist(context.Background(), st, "user-1", p); len(ticks) != 0 {
		t.Errorf("expected zero ticks for non-queryable payload, got %d", len(ticks))
	}
}

// TestMaterialize_ProgressAndComplete — exercises both branches of
// the materializer: items that should flip to completed (count ≥
// threshold) AND items that should report partial progress (count
// < threshold but > stored). Validates the codex Medium #2 fix —
// progress.current must reflect live count, not stay at 0/N until
// the threshold trips.
func TestMaterialize_ProgressAndComplete(t *testing.T) {
	t.Parallel()
	// Seed the in-memory store with 3 memories for the user. Below
	// five_memories threshold (5) but above first_memory threshold (1).
	st := store.NewInMemoryStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		m := &model.Memory{
			ID:      "mem-" + string(rune('a'+i)),
			OwnerID: "user-1",
			HubID:   "hub-1",
			Title:   "test",
			Content: "test",
		}
		if err := st.CreateMemory(m); err != nil {
			t.Fatalf("create memory %d: %v", i, err)
		}
	}
	p := model.ChecklistPayload{Items: []model.ChecklistItem{
		{
			Item:    model.Item{ID: "first_memory"},
			Trigger: "memory_count_gte:1",
		},
		{
			Item: model.Item{
				ID:       "five_memories",
				Progress: &model.ItemProgress{Current: 0, Target: 5},
			},
			Trigger: "memory_count_gte:5",
		},
	}}
	ticks := MaterializeChecklist(ctx, st, "user-1", p)
	if len(ticks) != 2 {
		t.Fatalf("expected 2 ticks (1 complete + 1 progress), got %d: %+v", len(ticks), ticks)
	}
	var firstMem, fiveMem *ChecklistTick
	for i := range ticks {
		switch ticks[i].ItemID {
		case "first_memory":
			firstMem = &ticks[i]
		case "five_memories":
			fiveMem = &ticks[i]
		}
	}
	if firstMem == nil || !firstMem.ShouldComplete {
		t.Errorf("first_memory should be marked complete: %+v", firstMem)
	}
	if fiveMem == nil || fiveMem.ShouldComplete {
		t.Errorf("five_memories should NOT be marked complete (3 < 5): %+v", fiveMem)
	}
	if fiveMem == nil || fiveMem.ProgressCurrent == nil || *fiveMem.ProgressCurrent != 3 {
		t.Errorf("five_memories progress should be 3, got %+v", fiveMem)
	}
	if fiveMem == nil || fiveMem.ProgressTarget == nil || *fiveMem.ProgressTarget != 5 {
		t.Errorf("five_memories target should be 5, got %+v", fiveMem)
	}
}

// TestMaterialize_SkipAlreadyComplete — an item with completed_at
// already set is never re-ticked, even if the underlying count
// continues to grow. Avoids spurious writes + SSE on every GET.
func TestMaterialize_SkipAlreadyComplete(t *testing.T) {
	t.Parallel()
	st := store.NewInMemoryStore()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := st.CreateMemory(&model.Memory{
			ID:      "mem-" + string(rune('a'+i)),
			OwnerID: "user-1",
			HubID:   "hub-1",
			Title:   "t",
			Content: "t",
		}); err != nil {
			t.Fatalf("create memory: %v", err)
		}
	}
	// Both items completed_at already set — count crossed threshold
	// in some prior pass + persisted. Materializer must be a no-op.
	now := time.Now().UTC()
	p := model.ChecklistPayload{Items: []model.ChecklistItem{
		{
			Item:        model.Item{ID: "first_memory"},
			CompletedAt: &now,
			Trigger:     "memory_count_gte:1",
		},
		{
			Item:        model.Item{ID: "five_memories"},
			CompletedAt: &now,
			Trigger:     "memory_count_gte:5",
		},
	}}
	if ticks := MaterializeChecklist(ctx, st, "user-1", p); len(ticks) != 0 {
		t.Errorf("expected zero ticks for already-complete items, got %d", len(ticks))
	}
}
