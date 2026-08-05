package dreams

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestPickWowKindDeterministicAndRotating(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC)
	a := pickWowKind("hub-1", day)
	if b := pickWowKind("hub-1", day); a != b {
		t.Fatalf("same hub+day must pick the same kind: %s vs %s", a, b)
	}
	// Across 5 consecutive days a hub must cycle through all 5 kinds
	// (rotation, not a coin flip).
	seen := map[string]bool{}
	for i := 0; i < len(model.WowKinds); i++ {
		seen[pickWowKind("hub-1", day.AddDate(0, 0, i))] = true
	}
	if len(seen) != len(model.WowKinds) {
		t.Fatalf("expected full rotation over %d days, saw %d kinds", len(model.WowKinds), len(seen))
	}
}

func TestBuildWowSlotCitationValidator(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	// Two real memories in-hub, one in another hub.
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "hub-1", Title: "old question"},
		{ID: "m2", OwnerID: "u1", HubID: "hub-1", Title: "new answer"},
		{ID: "other", OwnerID: "u1", HubID: "hub-2", Title: "foreign"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{store: s}
	ctx := context.Background()

	base := func() *synthesisResponse {
		return &synthesisResponse{Wow: &struct {
			Kind   string             `json:"kind"`
			Title  string             `json:"title"`
			Body   string             `json:"body"`
			Quotes []synthesizedQuote `json:"quotes"`
			Then   *synthesizedQuote  `json:"then"`
			Now    *synthesizedQuote  `json:"now"`
		}{
			Kind:  model.BoardKindEcho,
			Title: "118 天前的问题，有答案了",
			Body:  "你四月问过的问题，八月的决策回答了它。",
			Then:  &synthesizedQuote{MemoryID: "m1", Excerpt: "persona 跟设备走还是跟云走？"},
			Now:   &synthesizedQuote{MemoryID: "m2", Excerpt: "persona 绑定 memax agent。"},
		}}
	}

	// Valid echo passes.
	slot := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, base())
	if slot == nil || slot.Kind != model.BoardKindEcho || len(slot.CiteMemoryIDs) != 2 {
		t.Fatalf("valid echo should build: %#v", slot)
	}
	if slot.DreamRunID != "run1" {
		t.Fatalf("wow slot must link its dream run: %#v", slot)
	}

	// Invented citation kills the whole card.
	invented := base()
	invented.Wow.Now.MemoryID = "does-not-exist"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, invented); got != nil {
		t.Fatalf("invented citation must drop the card, got %#v", got)
	}

	// Cross-hub citation kills the card (isolation).
	leaked := base()
	leaked.Wow.Now.MemoryID = "other"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, leaked); got != nil {
		t.Fatalf("cross-hub citation must drop the card, got %#v", got)
	}

	// Pattern kind below its 3-citation floor is dropped.
	pattern := base()
	pattern.Wow.Kind = model.BoardKindPattern
	pattern.Wow.Then, pattern.Wow.Now = nil, nil
	pattern.Wow.Quotes = []synthesizedQuote{
		{MemoryID: "m1", Excerpt: "a"},
		{MemoryID: "m2", Excerpt: "b"},
	}
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindPattern, pattern); got != nil {
		t.Fatalf("pattern below citation floor must drop, got %#v", got)
	}

	// Unknown kind from the agent is rejected.
	unknown := base()
	unknown.Wow.Kind = "horoscope"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, unknown); got != nil {
		t.Fatalf("unknown kind must drop, got %#v", got)
	}
}
