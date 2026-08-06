package dreams

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
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
		return &synthesisResponse{Wow: &synthesizedWow{
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

func TestBuildWowSlotRejectsDreamlogAndDuplicateCitations(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "hub-1", Title: "a"},
		{ID: "m2", OwnerID: "u1", HubID: "hub-1", Title: "b"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{store: s}
	ctx := context.Background()

	mk := func(kind string, quotes []synthesizedQuote) *synthesisResponse {
		return &synthesisResponse{Wow: &synthesizedWow{Kind: kind, Title: "t", Body: "b", Quotes: quotes}}
	}

	// dreamlog has a zero citation floor — accepting it as a wow kind
	// would ship an uncited first-person claim, the exact thing the
	// validator exists to stop.
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindPattern,
		mk(model.BoardKindDreamlog, nil)); got != nil {
		t.Fatalf("dreamlog must never fill the wow slot, got %#v", got)
	}

	// One memory quoted three times is one memory, not a pattern.
	dupes := []synthesizedQuote{
		{MemoryID: "m1", Excerpt: "x"},
		{MemoryID: "m1", Excerpt: "y"},
		{MemoryID: "m1", Excerpt: "z"},
	}
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindPattern,
		mk(model.BoardKindPattern, dupes)); got != nil {
		t.Fatalf("duplicate citations must not satisfy the floor, got %#v", got)
	}
}

func TestBuildNextUpSlotPerItemCitationGate(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "hub-1", Title: "开放问题：备份策略"},
		{ID: "m2", OwnerID: "u1", HubID: "hub-1", Title: "说好要写的迁移脚本"},
		{ID: "other", OwnerID: "u1", HubID: "hub-2", Title: "foreign"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{store: s}
	ctx := context.Background()

	// Valid 2-item card: both items keep their verified quotes.
	valid := &synthesisResponse{NextUp: &synthesizedNextUp{Items: []synthesizedNextUpItem{
		{Title: "定下备份策略", Why: "你问过但一直没回答。",
			Quotes: []synthesizedQuote{{MemoryID: "m1", Excerpt: "备份到底放哪？"}}},
		{Title: "写完迁移脚本", Why: "你说好要写，之后再没提。",
			Quotes: []synthesizedQuote{{MemoryID: "m2", Excerpt: "明天写迁移脚本"}}},
	}}}
	slot := e.buildNextUpSlot(ctx, hub, "b1", "run1", valid)
	if slot == nil {
		t.Fatal("valid 2-item nextup should build")
	}
	if slot.Kind != model.BoardKindNextUp || slot.SlotKey != model.BoardSlotKeyNextUp {
		t.Fatalf("wrong kind/slot key: %#v", slot)
	}
	if slot.DreamRunID != "run1" {
		t.Fatalf("nextup slot must link its dream run: %#v", slot)
	}
	if slot.Title != "定下备份策略" {
		t.Fatalf("slot title must be the first item's title, got %q", slot.Title)
	}
	if len(slot.CiteMemoryIDs) != 2 {
		t.Fatalf("cite ids must union both items' quotes: %v", slot.CiteMemoryIDs)
	}
	var payload model.BoardNextUpPayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	for i, item := range payload.Items {
		if len(item.Quotes) != 1 {
			t.Fatalf("item %d must keep its verified quote: %#v", i, item)
		}
	}

	// Invented quote drops ITS item; the sibling with real receipts
	// survives and the card still ships.
	invented := &synthesisResponse{NextUp: &synthesizedNextUp{Items: []synthesizedNextUpItem{
		{Title: "幻觉任务", Why: "编造的。",
			Quotes: []synthesizedQuote{{MemoryID: "does-not-exist", Excerpt: "made up"}}},
		{Title: "写完迁移脚本", Why: "真的开放。",
			Quotes: []synthesizedQuote{{MemoryID: "m2", Excerpt: "明天写迁移脚本"}}},
	}}}
	slot = e.buildNextUpSlot(ctx, hub, "b1", "run1", invented)
	if slot == nil {
		t.Fatal("sibling with verified quotes must survive an invented sibling")
	}
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Title != "写完迁移脚本" {
		t.Fatalf("only the verified item should survive: %#v", payload.Items)
	}
	if slot.Title != "写完迁移脚本" {
		t.Fatalf("title must fall to the first SURVIVING item, got %q", slot.Title)
	}
	if len(slot.CiteMemoryIDs) != 1 || slot.CiteMemoryIDs[0] != "m2" {
		t.Fatalf("cite ids must only carry surviving quotes: %v", slot.CiteMemoryIDs)
	}

	// All items invalid → no slot at all.
	allInvalid := &synthesisResponse{NextUp: &synthesizedNextUp{Items: []synthesizedNextUpItem{
		{Title: "没有出处", Quotes: []synthesizedQuote{{MemoryID: "does-not-exist", Excerpt: "x"}}},
		{Title: "也没有出处", Quotes: nil},
	}}}
	if got := e.buildNextUpSlot(ctx, hub, "b1", "run1", allInvalid); got != nil {
		t.Fatalf("all-invalid items must drop the card, got %#v", got)
	}

	// Cross-hub quote kills its item (isolation), sparing siblings.
	leaked := &synthesisResponse{NextUp: &synthesizedNextUp{Items: []synthesizedNextUpItem{
		{Title: "泄漏的任务", Why: "引了别的 hub。",
			Quotes: []synthesizedQuote{{MemoryID: "other", Excerpt: "foreign"}}},
		{Title: "定下备份策略", Why: "真的开放。",
			Quotes: []synthesizedQuote{{MemoryID: "m1", Excerpt: "备份到底放哪？"}}},
	}}}
	slot = e.buildNextUpSlot(ctx, hub, "b1", "run1", leaked)
	if slot == nil {
		t.Fatal("in-hub sibling must survive a cross-hub sibling")
	}
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Title != "定下备份策略" {
		t.Fatalf("cross-hub item must be dropped: %#v", payload.Items)
	}

	// nil / empty nextup → no slot.
	if got := e.buildNextUpSlot(ctx, hub, "b1", "run1", &synthesisResponse{}); got != nil {
		t.Fatalf("nil nextup must not build a slot, got %#v", got)
	}
}

// cannedLLM is a minimal llmClient fake for the custom-board cheap
// path: FIFO responses, then empty arrays. Same shape as the fakeLLM
// in integration_test.go, kept local so this file stands alone.
type cannedLLM struct {
	responses []string
	calls     int
}

func (f *cannedLLM) Complete(_ context.Context, _ anthropic.CompleteRequest) (*anthropic.CompleteResponse, error) {
	i := f.calls
	f.calls++
	if i >= len(f.responses) {
		return &anthropic.CompleteResponse{Text: "[]", InputTokens: 10, OutputTokens: 2}, nil
	}
	return &anthropic.CompleteResponse{Text: f.responses[i], InputTokens: 10, OutputTokens: 20}, nil
}

func seedBoardSynthHub(t *testing.T) (*store.InMemoryStore, *model.Hub) {
	t.Helper()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "hub-1", Title: "训练日志：周六没去"},
		{ID: "m2", OwnerID: "u1", HubID: "hub-1", Title: "训练日志：周日又没去"},
		{ID: "m3", OwnerID: "u1", HubID: "hub-1", Title: "训练日志：周末补不回来"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	return s, hub
}

func TestSynthesizeCustomBoardWritesWowSlotsNeverDreamlog(t *testing.T) {
	t.Parallel()
	s, hub := seedBoardSynthHub(t)
	board := &model.Board{ID: "b1", HubID: hub.ID, Kind: model.BoardKindCustom,
		Instruction: "盯我的训练频率", Status: model.BoardStatusCooking}
	if err := s.CreateBoard(board); err != nil {
		t.Fatal(err)
	}
	fake := &cannedLLM{responses: []string{
		`[{"kind":"pattern","title":"周末训练在塌方","body":"三条记录都指向同一件事。","quotes":[{"memory_id":"m1","excerpt":"周六没去"},{"memory_id":"m2","excerpt":"周日又没去"},{"memory_id":"m3","excerpt":"补不回来"}]},
		  {"kind":"openq","title":"补不回来之后呢","body":"你记录了缺席，但从没写下打算怎么办。","quotes":[{"memory_id":"m3","excerpt":"补不回来"}]}]`,
	}}
	e := &Engine{store: s, client: fake, organizeModel: "test-model"}
	run := &model.DreamRun{ID: "run1"}

	n, metrics := e.synthesizeCustomBoard(context.Background(), hub, run, board, "night material")
	if n != 2 {
		t.Fatalf("expected 2 cards written, got %d (metrics %+v)", n, metrics)
	}
	// Custom boards NEVER write a dreamlog slot — 一场梦一张梦记.
	if slot, err := s.GetBoardSlot(board.ID, model.BoardSlotKeyDreamlog); err == nil {
		t.Fatalf("custom board must not have a dreamlog slot, got %#v", slot)
	}
	first, err := s.GetBoardSlot(board.ID, model.BoardSlotKeyWow)
	if err != nil || first.Kind != model.BoardKindPattern || len(first.CiteMemoryIDs) != 3 {
		t.Fatalf("first card should land in 1-wow as pattern: %#v (err %v)", first, err)
	}
	second, err := s.GetBoardSlot(board.ID, model.BoardSlotKeyWow2)
	if err != nil || second.Kind != model.BoardKindOpenQuestion {
		t.Fatalf("second card should land in 2-wow as openq: %#v (err %v)", second, err)
	}
	if metrics.LLMCalls != 1 {
		t.Fatalf("custom board must cost exactly one LLM call, got %d", metrics.LLMCalls)
	}
	// First real card flips 酝酿中 → 活跃.
	fresh, err := s.GetBoard(board.ID)
	if err != nil || fresh.Status != model.BoardStatusActive {
		t.Fatalf("cooking board with cards must activate: %#v (err %v)", fresh, err)
	}
}

func TestSynthesizeCustomBoardsIsolatesParseFailure(t *testing.T) {
	t.Parallel()
	s, hub := seedBoardSynthHub(t)
	b1 := &model.Board{ID: "b1", HubID: hub.ID, Kind: model.BoardKindCustom,
		Instruction: "brief one", Status: model.BoardStatusActive}
	b2 := &model.Board{ID: "b2", HubID: hub.ID, Kind: model.BoardKindCustom,
		Instruction: "brief two", Status: model.BoardStatusActive}
	for _, b := range []*model.Board{b1, b2} {
		if err := s.CreateBoard(b); err != nil {
			t.Fatal(err)
		}
	}
	fake := &cannedLLM{responses: []string{
		"tonight I dreamed of unparseable prose, not JSON",
		`[{"kind":"openq","title":"没答的问题","body":"你问过但没回。","quotes":[{"memory_id":"m1","excerpt":"周六没去"}]}]`,
	}}
	e := &Engine{store: s, client: fake, organizeModel: "test-model"}
	run := &model.DreamRun{ID: "run1"}

	n, metrics := e.synthesizeCustomBoards(context.Background(), hub, run, newDreamRunBudget(),
		[]model.Board{*b1, *b2}, "night material")
	if n != 1 {
		t.Fatalf("second board must still write despite first board's parse failure, got %d", n)
	}
	if metrics.Errors == 0 {
		t.Fatalf("parse failure must be counted, metrics %+v", metrics)
	}
	if _, err := s.GetBoardSlot(b1.ID, model.BoardSlotKeyWow); err == nil {
		t.Fatalf("failed board must not have a card")
	}
	if slot, err := s.GetBoardSlot(b2.ID, model.BoardSlotKeyWow); err != nil || slot.Kind != model.BoardKindOpenQuestion {
		t.Fatalf("second board should have its card: %#v (err %v)", slot, err)
	}
}

func TestNightMaterialTruncatesRuneSafe(t *testing.T) {
	t.Parallel()
	s, hub := seedBoardSynthHub(t)
	e := &Engine{store: s}
	run := &model.DreamRun{ID: "run1", MemoriesScanned: 3}

	// A session final far past the byte cap, all multi-byte runes, so
	// the cut almost certainly lands mid-rune without the helper.
	sessionFinal := strings.Repeat("夜航西飞，梦里都是记忆。", 300)
	material := e.buildNightMaterial(context.Background(), hub, run, sessionFinal)
	if len(material) > nightMaterialMaxBytes+len("…") {
		t.Fatalf("material exceeds byte cap: %d", len(material))
	}
	if !utf8.ValidString(material) {
		t.Fatalf("material truncation split a rune")
	}
	if !strings.Contains(material, "m1") {
		t.Fatalf("material should list recent memory ids:\n%s", material)
	}

	// Direct boundary check: cap not divisible by rune width.
	out := truncateBytesRuneSafe(strings.Repeat("梦", 2000), 4000)
	if !utf8.ValidString(out) || len(out) > 4000+len("…") {
		t.Fatalf("truncateBytesRuneSafe broke a rune or the cap: len=%d valid=%v", len(out), utf8.ValidString(out))
	}
	if short := truncateBytesRuneSafe("short", 4000); short != "short" {
		t.Fatalf("under-cap string must pass through unchanged, got %q", short)
	}
}
