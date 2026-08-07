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
	a := pickWowKind("hub-1", "personal", day)
	if b := pickWowKind("hub-1", "personal", day); a != b {
		t.Fatalf("same hub+day must pick the same kind: %s vs %s", a, b)
	}
	// Across N consecutive days a hub must cycle through all N kinds
	// (rotation, not a coin flip).
	seen := map[string]bool{}
	for i := 0; i < len(model.WowKinds); i++ {
		seen[pickWowKind("hub-1", "personal", day.AddDate(0, 0, i))] = true
	}
	if len(seen) != len(model.WowKinds) {
		t.Fatalf("expected full rotation over %d days, saw %d kinds", len(model.WowKinds), len(seen))
	}
}

// A team hub rotates over six lenses, a personal hub over three, and
// the team-only kinds must NEVER be offered to a personal hub — on a
// hub with one writer they are unfillable, so offering one would just
// invite the model to fabricate a colleague.
func TestPickWowKindPoolFollowsHubType(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		hubType   string
		wantPool  []string
		wantTeam  bool
		wantCount int
	}{
		{"personal hub keeps the three personal lenses", "personal", model.WowKinds, false, len(model.WowKinds)},
		{"empty hub type is treated as personal", "", model.WowKinds, false, len(model.WowKinds)},
		{"team hub rotates over all six", model.HubTypeTeam, model.TeamWowKinds, true, len(model.TeamWowKinds)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allowed := make(map[string]bool, len(tc.wantPool))
			for _, k := range tc.wantPool {
				allowed[k] = true
			}
			seen := map[string]bool{}
			sawTeamKind := false
			for i := 0; i < tc.wantCount; i++ {
				kind := pickWowKind("hub-1", tc.hubType, day.AddDate(0, 0, i))
				if !allowed[kind] {
					t.Fatalf("kind %q is not in the %s pool", kind, tc.hubType)
				}
				seen[kind] = true
				if model.IsTeamWowKind(kind) {
					sawTeamKind = true
				}
			}
			if len(seen) != tc.wantCount {
				t.Fatalf("expected full rotation over %d days, saw %d kinds", tc.wantCount, len(seen))
			}
			if sawTeamKind != tc.wantTeam {
				t.Fatalf("team kinds present = %v, want %v", sawTeamKind, tc.wantTeam)
			}
		})
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
	slot := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, base(), nil)
	if slot == nil || slot.Kind != model.BoardKindEcho || len(slot.CiteMemoryIDs) != 2 {
		t.Fatalf("valid echo should build: %#v", slot)
	}
	if slot.DreamRunID != "run1" {
		t.Fatalf("wow slot must link its dream run: %#v", slot)
	}

	// Invented citation kills the whole card.
	invented := base()
	invented.Wow.Now.MemoryID = "does-not-exist"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, invented, nil); got != nil {
		t.Fatalf("invented citation must drop the card, got %#v", got)
	}

	// Cross-hub citation kills the card (isolation).
	leaked := base()
	leaked.Wow.Now.MemoryID = "other"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, leaked, nil); got != nil {
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
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindPattern, pattern, nil); got != nil {
		t.Fatalf("pattern below citation floor must drop, got %#v", got)
	}

	// Unknown kind from the agent is rejected.
	unknown := base()
	unknown.Wow.Kind = "horoscope"
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindEcho, unknown, nil); got != nil {
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
		mk(model.BoardKindDreamlog, nil), nil); got != nil {
		t.Fatalf("dreamlog must never fill the wow slot, got %#v", got)
	}

	// One memory quoted three times is one memory, not a pattern.
	dupes := []synthesizedQuote{
		{MemoryID: "m1", Excerpt: "x"},
		{MemoryID: "m1", Excerpt: "y"},
		{MemoryID: "m1", Excerpt: "z"},
	}
	if got := e.buildWowSlot(ctx, hub, "b1", "run1", model.BoardKindPattern,
		mk(model.BoardKindPattern, dupes), nil); got != nil {
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
		  {"kind":"thread","title":"补不回来之后呢","body":"你记录了缺席，但从没写下打算怎么办。","quotes":[{"memory_id":"m3","excerpt":"补不回来"},{"memory_id":"m2","excerpt":"周日又没去"}]}]`,
	}}
	e := &Engine{store: s, client: fake, organizeModel: "test-model"}
	run := &model.DreamRun{ID: "run1"}

	n, metrics := e.synthesizeCustomBoard(context.Background(), hub, run, board, "night material", newDreamRunBudget())
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
	if err != nil || second.Kind != model.BoardKindThread {
		t.Fatalf("second card should land in 2-wow as thread: %#v (err %v)", second, err)
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
		`[{"kind":"pattern","title":"没答的问题","body":"你问过但没回。","quotes":[{"memory_id":"m1","excerpt":"周六没去"},{"memory_id":"m2","excerpt":"周日又没去"},{"memory_id":"m3","excerpt":"补不回来"}]}]`,
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
	if slot, err := s.GetBoardSlot(b2.ID, model.BoardSlotKeyWow); err != nil || slot.Kind != model.BoardKindPattern {
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

// A nextup item whose evidence is one real memory plus fabricated ones
// is a half-lie: the user can't tell which half is real, so the item
// must die like a wow card would.
func TestBuildNextUpSlotDropsPartiallyInventedItem(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-1", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "hub-1", Title: "real"},
		{ID: "m2", OwnerID: "u1", HubID: "hub-1", Title: "also real"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{store: s}

	parsed := &synthesisResponse{NextUp: &synthesizedNextUp{Items: []synthesizedNextUpItem{
		{
			Title: "半真半假的一条",
			Why:   "one real quote, one invented",
			Quotes: []synthesizedQuote{
				{MemoryID: "m1", Excerpt: "real evidence"},
				{MemoryID: "does-not-exist", Excerpt: "fabricated evidence"},
			},
		},
		{
			Title:  "完全有据的一条",
			Why:    "fully grounded",
			Quotes: []synthesizedQuote{{MemoryID: "m2", Excerpt: "solid"}},
		},
	}}}

	slot := e.buildNextUpSlot(context.Background(), hub, "b1", "run1", parsed)
	if slot == nil {
		t.Fatal("the fully-grounded item should still ship the card")
	}
	var payload model.BoardNextUpPayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Title != "完全有据的一条" {
		t.Fatalf("partially-invented item must be dropped, got %#v", payload.Items)
	}
	if slot.Title != "完全有据的一条" {
		t.Fatalf("title must come from the surviving item, got %q", slot.Title)
	}
}

// --- Team-native kinds: owner-diversity enforcement ---

// seedTeamHub builds a two-member team hub whose memories have real
// authors and real timestamps: u1 (Wei) wrote ta and tc, u2 (Lin)
// wrote tb, and tb sits between them in time.
func seedTeamHub(t *testing.T) (*store.InMemoryStore, *model.Hub) {
	t.Helper()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-t", OwnerID: "u1", HubType: model.HubTypeTeam}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "ta", OwnerID: "u1", HubID: "hub-t", Title: "wei jan",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "tb", OwnerID: "u2", HubID: "hub-t", Title: "lin mar",
			CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "tc", OwnerID: "u1", HubID: "hub-t", Title: "wei may",
			CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	return s, hub
}

func quoteRefs(ids ...string) []synthesizedQuote {
	out := make([]synthesizedQuote, 0, len(ids))
	for _, id := range ids {
		out = append(out, synthesizedQuote{MemoryID: id, Excerpt: "excerpt " + id})
	}
	return out
}

// The team kinds live or die on WHO wrote the receipts. A 共识缺口
// citing one person twice is a fabricated disagreement; a 谁知道这个
// citing three people names the wrong person to ask. Both are worse
// than an empty board, so both must be dropped.
func TestBuildWowSlotEnforcesOwnerDiversity(t *testing.T) {
	t.Parallel()
	s, hub := seedTeamHub(t)
	e := &Engine{store: s}
	ctx := context.Background()
	roster := map[string]string{"u1": "Wei", "u2": "Lin"}

	tests := []struct {
		name    string
		kind    string
		wow     *synthesizedWow
		members map[string]string
		want    bool
	}{
		{
			name: "consensus_gap across two members ships",
			kind: model.BoardKindConsensusGap,
			wow:  &synthesizedWow{Title: "两种理解", Body: "两个人对同一件事的理解不同。", Quotes: quoteRefs("ta", "tb")},
			want: true,
		},
		{
			name: "consensus_gap from one member is a fabricated disagreement",
			kind: model.BoardKindConsensusGap,
			wow:  &synthesizedWow{Title: "两种理解", Body: "同一个人的两条记忆。", Quotes: quoteRefs("ta", "tc")},
			want: false,
		},
		{
			name: "consensus_gap with three sides is not a two-sided claim",
			kind: model.BoardKindConsensusGap,
			wow:  &synthesizedWow{Title: "三种理解", Body: "三条。", Quotes: quoteRefs("ta", "tb", "tc")},
			want: false,
		},
		{
			name: "team_echo from question to a different member's answer ships",
			kind: model.BoardKindTeamEcho,
			wow: &synthesizedWow{Title: "有人早就答过", Body: "Wei 的问题，Lin 后来答了。",
				Then: &synthesizedQuote{MemoryID: "ta", Excerpt: "部署要不要手动？"},
				Now:  &synthesizedQuote{MemoryID: "tb", Excerpt: "已经接到 CI 上了。"}},
			want: true,
		},
		{
			name: "team_echo answering yourself is just an echo",
			kind: model.BoardKindTeamEcho,
			wow: &synthesizedWow{Title: "自问自答", Body: "同一个人。",
				Then: &synthesizedQuote{MemoryID: "ta", Excerpt: "问题"},
				Now:  &synthesizedQuote{MemoryID: "tc", Excerpt: "答案"}},
			want: false,
		},
		{
			name: "team_echo told backwards is dropped, not silently swapped",
			kind: model.BoardKindTeamEcho,
			wow: &synthesizedWow{Title: "顺序反了", Body: "答案比问题还早。",
				Then: &synthesizedQuote{MemoryID: "tc", Excerpt: "五月"},
				Now:  &synthesizedQuote{MemoryID: "tb", Excerpt: "三月"}},
			want: false,
		},
		{
			name:    "who_knows backed by one member's memories ships",
			kind:    model.BoardKindWhoKnows,
			wow:     &synthesizedWow{Title: "问 Wei", Body: "部署的事都是 Wei 写的。", Quotes: quoteRefs("ta", "tc")},
			members: roster,
			want:    true,
		},
		{
			name:    "who_knows spread across members names nobody",
			kind:    model.BoardKindWhoKnows,
			wow:     &synthesizedWow{Title: "问谁？", Body: "两个人都写过。", Quotes: quoteRefs("ta", "tb")},
			members: roster,
			want:    false,
		},
		{
			name: "who_knows with no roster and no model name has nobody to ask",
			kind: model.BoardKindWhoKnows,
			wow:  &synthesizedWow{Title: "问谁？", Body: "没人名。", Quotes: quoteRefs("ta", "tc")},
			want: false,
		},
		{
			name: "who_knows falls back to the model's name when the roster is empty",
			kind: model.BoardKindWhoKnows,
			wow:  &synthesizedWow{Title: "问 Wei", Body: "有名字。", Quotes: quoteRefs("ta", "tc"), Holder: "Wei"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wow := *tc.wow
			wow.Kind = tc.kind
			slot := e.buildWowSlot(ctx, hub, "b1", "run1", tc.kind,
				&synthesisResponse{Wow: &wow}, tc.members)
			if (slot != nil) != tc.want {
				t.Fatalf("card shipped = %v, want %v (%#v)", slot != nil, tc.want, slot)
			}
		})
	}
}

// The holder on a 谁知道这个 card is the person the reader is told to
// go ask. It must come from the AUTHOR of the receipts, not from the
// model's own attribution — the model is guessing, the roster is a
// fact.
func TestWhoKnowsHolderComesFromCitationAuthor(t *testing.T) {
	t.Parallel()
	s, hub := seedTeamHub(t)
	e := &Engine{store: s}

	slot := e.buildWowSlot(context.Background(), hub, "b1", "run1", model.BoardKindWhoKnows,
		&synthesisResponse{Wow: &synthesizedWow{
			Kind:   model.BoardKindWhoKnows,
			Title:  "部署的事问 Wei",
			Body:   "最近的部署记忆都是同一个人写的。",
			Quotes: quoteRefs("ta", "tc"),
			// The model misattributes; the roster must win.
			Holder: "Lin",
		}}, map[string]string{"u1": "Wei", "u2": "Lin"})
	if slot == nil {
		t.Fatal("who_knows with two same-owner receipts should ship")
	}
	var payload model.BoardWhoKnowsPayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Holder != "Wei" {
		t.Fatalf("holder must come from the citation author, got %q", payload.Holder)
	}
	if len(payload.Quotes) != 2 {
		t.Fatalf("who_knows must carry its receipts, got %#v", payload.Quotes)
	}
}

// consensus_gap renders as a quote pair, so its payload must expose
// exactly two sides in the order the model presented them.
func TestConsensusGapPayloadHasTwoSides(t *testing.T) {
	t.Parallel()
	s, hub := seedTeamHub(t)
	e := &Engine{store: s}

	slot := e.buildWowSlot(context.Background(), hub, "b1", "run1", model.BoardKindConsensusGap,
		&synthesisResponse{Wow: &synthesizedWow{
			Kind:   model.BoardKindConsensusGap,
			Title:  "同一件事，两种理解",
			Body:   "一个人以为已经定了，另一个人还在等结论。",
			Quotes: quoteRefs("ta", "tb"),
		}}, map[string]string{"u1": "Wei", "u2": "Lin"})
	if slot == nil {
		t.Fatal("consensus_gap across two members should ship")
	}
	var payload model.BoardConsensusPayload
	if err := json.Unmarshal(slot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sides) != 2 {
		t.Fatalf("consensus_gap must carry exactly two sides, got %d", len(payload.Sides))
	}
	if payload.Sides[0].MemoryID != "ta" || payload.Sides[1].MemoryID != "tb" {
		t.Fatalf("sides must keep their order, got %#v", payload.Sides)
	}
	if len(slot.CiteMemoryIDs) != 2 {
		t.Fatalf("both sides must be cited, got %#v", slot.CiteMemoryIDs)
	}
	// Attribution comes from the roster keyed on the stored owner — on
	// a two-sided team card, "who said this" is half the content.
	if payload.Sides[0].Author != "Wei" || payload.Sides[1].Author != "Lin" {
		t.Fatalf("sides must be attributed to their authors, got %#v", payload.Sides)
	}
}

// A team kind on a personal hub is unfillable by construction — one
// writer cannot disagree with a colleague. Neither the rotation nor a
// stray kind in the model's output may put one on a personal board.
func TestTeamKindsNeverShipOnPersonalHub(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	hub := &model.Hub{ID: "hub-p", OwnerID: "u1", HubType: "personal"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*model.Memory{
		{ID: "p1", OwnerID: "u1", HubID: "hub-p", Title: "a"},
		{ID: "p2", OwnerID: "u1", HubID: "hub-p", Title: "b"},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{store: s}

	for _, kind := range []string{model.BoardKindConsensusGap, model.BoardKindTeamEcho, model.BoardKindWhoKnows} {
		got := e.buildWowSlot(context.Background(), hub, "b1", "run1", kind,
			&synthesisResponse{Wow: &synthesizedWow{
				Kind:   kind,
				Title:  "t",
				Body:   "b",
				Quotes: quoteRefs("p1", "p2"),
				Then:   &synthesizedQuote{MemoryID: "p1", Excerpt: "x"},
				Now:    &synthesizedQuote{MemoryID: "p2", Excerpt: "y"},
				Holder: "Someone",
			}}, map[string]string{"u1": "Someone"})
		if got != nil {
			t.Fatalf("%s must never ship on a personal hub, got %#v", kind, got)
		}
	}
}

// The team system prompt is additive and team-only: a personal hub
// must never be told its memories were written by several people.
func TestBoardSynthesisSystemPromptTeamContext(t *testing.T) {
	t.Parallel()
	members := []model.HubMember{
		{UserID: "u1", UserName: "Wei"},
		{UserID: "u2", UserName: "Lin"},
		{UserID: "u3", UserEmail: "ghost@example.com"},
	}

	personal := boardSynthesisSystemPromptFor(&model.Hub{HubType: "personal"}, members)
	if personal != boardSynthesisSystemPrompt {
		t.Fatal("personal hubs must get the unmodified system prompt")
	}

	team := boardSynthesisSystemPromptFor(&model.Hub{HubType: model.HubTypeTeam}, members)
	if !strings.Contains(team, "TEAM HUB CONTEXT") {
		t.Fatal("team hubs must get the team context paragraph")
	}
	if !strings.Contains(team, "Wei, Lin") {
		t.Fatalf("team prompt must name the roster, got:\n%s", team)
	}
	if strings.Contains(team, "ghost@example.com") {
		t.Fatal("members without a display name must not be addressed by email")
	}

	// No roster (store failure, or an in-memory store that doesn't
	// track members) still gets the team framing — owner-id validation
	// is what actually holds the line.
	bare := boardSynthesisSystemPromptFor(&model.Hub{HubType: model.HubTypeTeam}, nil)
	if !strings.Contains(bare, "TEAM HUB CONTEXT") || strings.Contains(bare, "Members of this hub") {
		t.Fatal("a missing roster must degrade to generic team framing")
	}
}
