package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"

	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
)

// Phase 6 — Lane B board synthesis (plan 25 P2).
//
// 一场梦一张梦记，共享勘探分开成稿 — one dream writes ONE dreamlog, on
// the system board only; custom boards share that night's exploration
// and draft their own cards from it cheaply.
//
// The system board keeps the full agent session (recall tool, wow
// rotation, dreamlog + wow slots). Custom boards each get ONE plain
// Complete call fed a compact "night material" pack (run stats, the
// system session's final notes, recent memory titles) — no agent
// budget, no recall fan-out, and never a dreamlog slot. Gated the
// same two-layer way as the other agentic phases: AgentRuntime
// configured at the app layer AND the hub's dreams settings. Cost
// rides the cycle's shared budget governor; the phase is skipped when
// the governor is exhausted so board polish never crowds out
// organize/contradiction work.

// boardSynthesisMinInterval dedupes synthesis across rapid re-runs: a
// manually re-triggered dream within the window keeps last night's
// cards instead of burning LLM calls on near-identical content.
const boardSynthesisMinInterval = 18 * time.Hour

// Custom-board cheap-call envelope: single Complete on the organize
// model (Sonnet — the cards are user-facing prose that must clear the
// anti-Barnum bar), bounded so N custom boards stay cheap.
const (
	customBoardLLMTimeout = 30 * time.Second
	customBoardMaxTokens  = 1200
)

// Night material pack bounds: enough context to write specific cards,
// small enough that N custom boards cost pennies.
const (
	nightMaterialMaxBytes    = 4000
	nightMaterialMemoryLimit = 15
)

// boardSynthesisCoreRules is the shared anti-Barnum contract. Both the
// system board's agent session and every custom board's cheap call
// embed it verbatim — a user instruction shapes WHAT to look for,
// never whether to cite.
const boardSynthesisCoreRules = `Non-negotiable rules:
1. ANTI-BARNUM: never write a claim that could apply to anyone ("you've been busy lately", "you care about quality"). Every claim must be specific enough that the quoted memories PROVE it. If you cannot find a genuinely specific insight, omit the card entirely — an empty result is a good result; a hollow card destroys trust.
2. RECEIPTS: every card cites the memories behind it via their exact memory ids. Quote excerpts verbatim — do not paraphrase inside quotes.
3. VOICE: first person, warm, concise, zero corporate filler. Chinese memories get Chinese cards; English memories get English cards; mixed hubs follow the dominant language of the cited memories.
4. PLAIN TEXT: no markdown, no HTML — card text renders literally.`

const boardSynthesisSystemPrompt = `You are memax — the user's memory, speaking in first person about what you noticed while organizing their hub overnight. You write cards for the hub's pulse board.

` + boardSynthesisCoreRules + `

You have a recall tool. Use it to explore the hub before writing — search for old questions, recurring themes, and connections. Then respond with ONLY a JSON object:
{
  "dreamlog": {"body": "2-4 sentences: what you did tonight and the single most interesting thing you noticed"} | null,
  "wow": {
    "kind": "<the kind you were asked to attempt>",
    "title": "one plain-text line, the hook",
    "body": "2-4 sentences making the specific claim",
    "quotes": [{"memory_id": "...", "excerpt": "verbatim quote", "when": "ISO date if known"}],
    "then": {"memory_id": "...", "excerpt": "...", "when": "..."},
    "now": {"memory_id": "...", "excerpt": "...", "when": "..."}
  } | null
}
"then"/"now" only for kind=echo (the old question and the new answer). Other kinds use "quotes". Return null for a card you cannot honestly fill.`

// customBoardCardRequest closes the custom board's user prompt. It
// lives with the material (not the system prompt) so the output
// contract sits right next to the data it applies to.
const customBoardCardRequest = `Based on the night material above, write 0-2 cards for this board as a JSON array:
[{"kind": "musing"|"pattern"|"openq", "title": "one plain-text line, the hook", "body": "2-4 sentences making the specific claim", "quotes": [{"memory_id": "...", "excerpt": "verbatim quote"}]}]
Only write a card if the material is genuinely relevant to this board's brief — an empty array [] is a good answer. Cite only memory ids that appear in the material.`

// wowKindHints steers the agent's exploration per rotated kind.
var wowKindHints = map[string]string{
	model.BoardKindEcho:         "echo (回声): find a question or uncertainty the user recorded 30+ days ago that a RECENT memory now answers or settles. The payoff is the time gap.",
	model.BoardKindThread:       "thread (暗线): find two memories from different times or contexts that are plausibly the same underlying idea the user never connected. Be conservative — a false connection is worse than none.",
	model.BoardKindOpenQuestion: "openq (开放问题): find a question the user asked in their memories that was never answered or followed up. Surface it verbatim.",
	model.BoardKindPattern:      "pattern (未观察模式): find a recurring behavior visible across 3+ memories that the user likely hasn't noticed about themselves. Must be provable from the citations.",
	model.BoardKindMusing:       "musing (随想): an observation about the shape of this hub's knowledge — what's growing, what's abandoned, what's oddly missing — grounded in 3+ specific memories.",
}

type synthesizedQuote struct {
	MemoryID string `json:"memory_id"`
	Excerpt  string `json:"excerpt"`
	When     string `json:"when"`
}

// synthesizedWow is one candidate wow card, produced either by the
// system board's agent session or a custom board's cheap call. Both
// paths funnel through buildWowSlot so the citation validator is the
// single gate for everything user-facing.
type synthesizedWow struct {
	Kind   string             `json:"kind"`
	Title  string             `json:"title"`
	Body   string             `json:"body"`
	Quotes []synthesizedQuote `json:"quotes"`
	Then   *synthesizedQuote  `json:"then"`
	Now    *synthesizedQuote  `json:"now"`
}

type synthesisResponse struct {
	Dreamlog *struct {
		Body string `json:"body"`
	} `json:"dreamlog"`
	Wow *synthesizedWow `json:"wow"`
}

// customBoardCard is one element of the custom board's JSON-array
// response. No then/now — echo needs the recall tool's time-gap
// hunting, which the cheap path doesn't have.
type customBoardCard struct {
	Kind   string             `json:"kind"`
	Title  string             `json:"title"`
	Body   string             `json:"body"`
	Quotes []synthesizedQuote `json:"quotes"`
}

// pickWowKind rotates deterministically by hub + day so every night
// tries a different lens without storing rotation state.
func pickWowKind(hubID string, day time.Time) string {
	seed := 0
	for _, r := range hubID {
		seed += int(r)
	}
	seed += day.YearDay() + day.Year()*366
	return model.WowKinds[seed%len(model.WowKinds)]
}

// shouldRunBoardSynthesis is the phase gate.
func (e *Engine) shouldRunBoardSynthesis(settings map[string]any, runBudget *dreamRunBudget, runID string) bool {
	if !e.agentRuntime.IsConfigured() {
		return false
	}
	// Own gate, default true — see DefaultSettings. A hub can opt out;
	// an absent key means on.
	if enabled, ok := settings["dreams_board_synthesis_enabled"].(bool); ok && !enabled {
		return false
	}
	return runBudget.shouldRoute(runID)
}

// phaseBoardSynthesis runs the night's board work: one agent session
// for the system board, then one cheap call per custom board off the
// shared night material. Returns the number of cards written.
func (e *Engine) phaseBoardSynthesis(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	runBudget *dreamRunBudget,
) (int, model.DreamPhaseMetrics) {
	var metrics model.DreamPhaseMetrics
	start := time.Now()
	defer func() { metrics.DurationMs = time.Since(start).Milliseconds() }()

	sysBoard, err := e.store.GetOrCreateSystemBoard(hub.ID, hub.OwnerID)
	if err != nil {
		slog.WarnContext(ctx, "dream: board synthesis skipped", "hub_id", hub.ID, "error", err)
		metrics.Errors++
		return 0, metrics
	}
	boards, err := e.store.ListBoardsByHub(hub.ID)
	if err != nil {
		metrics.Errors++
		return 0, metrics
	}

	// System board first: the ONLY full agent session — and the only
	// dreamlog — of the night. Its exploration is then shared with
	// every custom board through the material pack.
	written := 0
	var sessionFinal string
	if sysBoard.Status != model.BoardStatusPaused && runBudget.shouldRoute(run.ID) {
		var sysMetrics model.DreamPhaseMetrics
		var n int
		n, sysMetrics, sessionFinal = e.synthesizeSystemBoard(ctx, hub, run, runBudget, sysBoard)
		written += n
		mergeSynthesisMetrics(&metrics, sysMetrics)
	}

	material := e.buildNightMaterial(ctx, hub, run, sessionFinal)
	n, customMetrics := e.synthesizeCustomBoards(ctx, hub, run, runBudget, boards, material)
	written += n
	mergeSynthesisMetrics(&metrics, customMetrics)
	return written, metrics
}

// mergeSynthesisMetrics folds one board's counters into the phase
// total so the cycle's cost accounting stays whole.
func mergeSynthesisMetrics(total *model.DreamPhaseMetrics, part model.DreamPhaseMetrics) {
	total.LLMCalls += part.LLMCalls
	total.LLMErrors += part.LLMErrors
	total.TokensIn += part.TokensIn
	total.TokensOut += part.TokensOut
	total.Errors += part.Errors
}

// synthesizeSystemBoard runs the night's single agent session and
// writes the system board's dreamlog + rotating wow slots. The third
// return is the agent's final text, recycled into the custom boards'
// night material so their cheap calls inherit tonight's exploration.
func (e *Engine) synthesizeSystemBoard(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	runBudget *dreamRunBudget,
	board *model.Board,
) (int, model.DreamPhaseMetrics, string) {
	var metrics model.DreamPhaseMetrics

	// Cadence dedupe: keep last night's cards if they're still fresh.
	// Keyed on content_updated_at, NOT updated_at —
	// acking or dismissing a card bumps updated_at, and using that
	// would mean the more a user engages with the board, the longer it
	// stays silent.
	if existing, err := e.store.GetBoardSlot(board.ID, model.BoardSlotKeyDreamlog); err == nil {
		if time.Since(existing.ContentUpdatedAt) < boardSynthesisMinInterval {
			return 0, metrics, ""
		}
	}

	wowKind := pickWowKind(board.ID, time.Now().UTC())
	prompt := buildSynthesisPrompt(run, wowKind)

	session := agent.SessionDescriptor{
		OwnerID: hub.OwnerID,
		Type:    agent.ScopeTypeSingleHub,
		HubIDs:  []string{hub.ID},
	}
	recallTool, err := tools.NewRecallTool(tools.RecallToolConfig{
		Service:  e.agentRuntime.RecallService,
		Resolver: e.agentRuntime.Resolver,
		Session:  session,
	})
	if err != nil {
		slog.WarnContext(ctx, "dream: board synthesis recall tool", "hub_id", hub.ID, "error", err)
		metrics.Errors++
		return 0, metrics, ""
	}

	res, err := agent.Run(ctx, agent.RunInput{
		Prompt:       prompt,
		Profile:      agent.ProfileDreamActive,
		Tools:        []sdktool.Tool{recallTool},
		Model:        e.agentRuntime.Model,
		Session:      session,
		SystemPrompt: boardSynthesisSystemPrompt,
	})
	if err != nil {
		metrics.Errors++
		metrics.LLMErrors++
		return 0, metrics, ""
	}
	runBudget.consume(&res)
	metrics.LLMCalls += res.ModelCalls
	metrics.TokensIn += int64(res.Usage.InputTokens)
	metrics.TokensOut += int64(res.Usage.OutputTokens)
	if res.Status != agent.RunStatusSuccess {
		metrics.LLMErrors++
		slog.WarnContext(ctx, "dream: board synthesis agent terminated",
			"hub_id", hub.ID, "status", res.Status, "err", res.Err)
		return 0, metrics, ""
	}

	var parsed synthesisResponse
	if err := json.Unmarshal([]byte(anthropic.ExtractJSONObject(res.Final)), &parsed); err != nil {
		metrics.Errors++
		slog.WarnContext(ctx, "dream: board synthesis parse", "hub_id", hub.ID, "error", err)
		return 0, metrics, res.Final
	}

	written := 0
	if parsed.Dreamlog != nil && strings.TrimSpace(parsed.Dreamlog.Body) != "" {
		slot := &model.BoardSlot{
			BoardID:    board.ID,
			SlotKey:    model.BoardSlotKeyDreamlog,
			Kind:       model.BoardKindDreamlog,
			Title:      truncateForTitle(parsed.Dreamlog.Body),
			DreamRunID: run.ID,
			Payload: mustMarshalPayload(model.BoardDreamlogPayload{
				Description: parsed.Dreamlog.Body,
				Body:        parsed.Dreamlog.Body,
			}),
		}
		if err := e.store.UpsertBoardSlot(slot); err != nil {
			metrics.Errors++
		} else {
			written++
		}
	}

	if wowSlot := e.buildWowSlot(ctx, hub, board.ID, run.ID, wowKind, &parsed); wowSlot != nil {
		if err := e.store.UpsertBoardSlot(wowSlot); err != nil {
			metrics.Errors++
		} else {
			written++
		}
	}
	return written, metrics, res.Final
}

// buildNightMaterial assembles the compact plain-text pack every
// custom board's cheap call reads: run stats, recent memory
// titles+ids (the citable universe), and the system session's final
// notes. Byte-capped with a rune-safe cut so a hub full of CJK
// content can't blow the pack past budget or split a character.
func (e *Engine) buildNightMaterial(ctx context.Context, hub *model.Hub, run *model.DreamRun, sessionFinal string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tonight's dream cycle stats: %d memories scanned, %d duplicates merged, %d contradictions found, %d memories organized, %d topics restructured.\n",
		run.MemoriesScanned, run.DuplicatesMerged, run.ContradictionsFound, run.MemoriesOrganized, run.TopicsRestructured)

	memories, _, _, err := e.store.ListMemoriesPaginated(store.ListOptions{
		Scope: store.VisibilityScope{OwnerID: hub.OwnerID, HubIDs: []string{hub.ID}},
		HubID: hub.ID,
		Limit: nightMaterialMemoryLimit,
	})
	if err != nil {
		slog.WarnContext(ctx, "dream: night material memory list", "hub_id", hub.ID, "error", err)
	}
	if len(memories) > 0 {
		b.WriteString("\nRecent memories in this hub (memory_id — title):\n")
		for _, m := range memories {
			fmt.Fprintf(&b, "- %s — %s\n", m.ID, truncateForTitle(m.Title))
		}
	}
	if s := strings.TrimSpace(sessionFinal); s != "" {
		b.WriteString("\nNotes from tonight's system-board synthesis session:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return truncateBytesRuneSafe(b.String(), nightMaterialMaxBytes)
}

// synthesizeCustomBoards drafts cards for every non-paused custom
// board off the shared material — one cheap call each. Per-board
// failures are isolated: a parse error on one board must not silence
// the rest of the night's boards.
func (e *Engine) synthesizeCustomBoards(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	runBudget *dreamRunBudget,
	boards []model.Board,
	material string,
) (int, model.DreamPhaseMetrics) {
	var metrics model.DreamPhaseMetrics
	written := 0
	for i := range boards {
		board := boards[i]
		// Paused boards are excluded; cooking boards are INCLUDED —
		// this run is exactly what they've been waiting for.
		if board.Kind == model.BoardKindSystem || board.Status == model.BoardStatusPaused {
			continue
		}
		if !runBudget.shouldRoute(run.ID) {
			// The cycle's governor is spent; remaining boards wait for
			// tomorrow rather than starving the organizational phases.
			break
		}
		n, boardMetrics := e.synthesizeCustomBoard(ctx, hub, run, &board, material)
		written += n
		mergeSynthesisMetrics(&metrics, boardMetrics)
	}
	return written, metrics
}

// synthesizeCustomBoard is one custom board's night: a single
// Complete call over the material, 0-2 cards out, each validated by
// the same citation gate as the system board's wow card. Custom
// boards never write a dreamlog — 一场梦一张梦记.
func (e *Engine) synthesizeCustomBoard(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	board *model.Board,
	material string,
) (int, model.DreamPhaseMetrics) {
	var metrics model.DreamPhaseMetrics

	// Cadence dedupe, keyed on this board's primary card slot (custom
	// boards have no dreamlog slot to key on).
	if existing, err := e.store.GetBoardSlot(board.ID, model.BoardSlotKeyWow); err == nil {
		if time.Since(existing.ContentUpdatedAt) < boardSynthesisMinInterval {
			return 0, metrics
		}
	}

	prompt := material + "\n\n" + customBoardCardRequest
	trackCtx := e.trackingContextForHub(ctx, hub.ID, hub.OwnerID, "", "dreams")
	resp, err := e.callLLMWithModelTimeout(trackCtx, e.organizeModel, customBoardSystemPrompt(board), prompt,
		customBoardMaxTokens, customBoardLLMTimeout, "dreams.board_synthesis")
	metrics.LLMCalls++
	if err != nil {
		metrics.LLMErrors++
		metrics.Errors++
		slog.WarnContext(ctx, "dream: custom board synthesis call",
			"hub_id", hub.ID, "board_id", board.ID, "error", err)
		return 0, metrics
	}
	addLLMUsage(&metrics, resp)

	var cards []customBoardCard
	if err := json.Unmarshal([]byte(anthropic.ExtractJSONArray(resp.Text)), &cards); err != nil {
		metrics.Errors++
		slog.WarnContext(ctx, "dream: custom board synthesis parse",
			"hub_id", hub.ID, "board_id", board.ID, "error", err)
		return 0, metrics
	}

	written := 0
	slotKeys := [...]string{model.BoardSlotKeyWow, model.BoardSlotKeyWow2}
	for _, card := range cards {
		if written >= len(slotKeys) {
			break
		}
		if !isCustomCardKind(card.Kind) {
			continue
		}
		// Same citation path as the system board: buildWowSlot runs
		// verifyQuotes + LaneBCitationFloor and drops invalid cards.
		slot := e.buildWowSlot(ctx, hub, board.ID, run.ID, card.Kind, &synthesisResponse{
			Wow: &synthesizedWow{Kind: card.Kind, Title: card.Title, Body: card.Body, Quotes: card.Quotes},
		})
		if slot == nil {
			continue
		}
		slot.SlotKey = slotKeys[written]
		if err := e.store.UpsertBoardSlot(slot); err != nil {
			metrics.Errors++
			continue
		}
		written++
	}

	if written > 0 {
		e.maybeActivateCookingBoard(ctx, board)
	}
	return written, metrics
}

// isCustomCardKind is the kinds the cheap path may produce. echo and
// thread need the recall tool's cross-time hunting, so the material-
// only path doesn't offer them.
func isCustomCardKind(kind string) bool {
	switch kind {
	case model.BoardKindMusing, model.BoardKindPattern, model.BoardKindOpenQuestion:
		return true
	}
	return false
}

// maybeActivateCookingBoard flips 酝酿中 → 活跃: a custom board leaves
// the cooking state the moment it has its first real card. If
// synthesis produced nothing, it keeps cooking and the UI keeps
// promising tomorrow rather than showing an empty board that looks
// broken.
func (e *Engine) maybeActivateCookingBoard(ctx context.Context, board *model.Board) {
	if board.Status != model.BoardStatusCooking {
		return
	}
	// Re-read before writing: this board snapshot was taken before the
	// LLM call, and the user may have rewritten the brief in the
	// meantime. Writing the stale copy back would silently revert
	// their edit — and that edit deliberately put the board BACK into
	// cooking, so we must not activate it.
	fresh, err := e.store.GetBoard(board.ID)
	if err != nil {
		slog.WarnContext(ctx, "dream: could not re-read board for activation",
			"board_id", board.ID, "error", err)
		return
	}
	if fresh.Status == model.BoardStatusCooking && fresh.Instruction == board.Instruction {
		fresh.Status = model.BoardStatusActive
		if err := e.store.UpdateBoard(fresh); err != nil {
			slog.WarnContext(ctx, "dream: could not activate cooking board",
				"board_id", board.ID, "error", err)
		}
	}
}

// buildWowSlot validates a synthesized wow card against the citation
// floor and hub-membership of every quoted memory. A card that fails
// any check is dropped silently — no card beats a wrong card.
func (e *Engine) buildWowSlot(
	ctx context.Context,
	hub *model.Hub,
	boardID, runID, requestedKind string,
	parsed *synthesisResponse,
) *model.BoardSlot {
	wow := parsed.Wow
	if wow == nil || strings.TrimSpace(wow.Title) == "" || strings.TrimSpace(wow.Body) == "" {
		return nil
	}
	kind := wow.Kind
	if kind != requestedKind {
		// The agent picked a different lens than asked. Accept it only
		// if it's a real WOW kind — LaneBCitationFloor also knows
		// dreamlog (floor 0), and letting that through here would ship
		// an uncited first-person claim in the wow slot, which is
		// exactly what the validator exists to stop.
		if !isWowKind(kind) {
			return nil
		}
	}

	var quotes []synthesizedQuote
	if kind == model.BoardKindEcho {
		if wow.Then == nil || wow.Now == nil {
			return nil
		}
		quotes = []synthesizedQuote{*wow.Then, *wow.Now}
	} else {
		quotes = wow.Quotes
	}

	floor, _ := model.LaneBCitationFloor(kind)
	verified := e.verifyQuotes(ctx, hub, quotes)
	if len(verified) < floor || len(verified) < len(quotes) {
		// Any invented citation kills the card, not just the quote —
		// a card that half-lies about receipts is worse than none.
		slog.InfoContext(ctx, "dream: wow card dropped by citation validator",
			"hub_id", hub.ID, "kind", kind,
			"claimed", len(quotes), "verified", len(verified), "floor", floor)
		return nil
	}

	citeIDs := make([]string, 0, len(verified))
	refs := make([]model.BoardQuoteRef, 0, len(verified))
	for _, q := range verified {
		citeIDs = append(citeIDs, q.MemoryID)
		refs = append(refs, model.BoardQuoteRef{MemoryID: q.MemoryID, When: q.When, Excerpt: q.Excerpt})
	}

	var payload any
	if kind == model.BoardKindEcho {
		payload = model.BoardEchoPayload{
			Description: wow.Body,
			Body:        wow.Body,
			Then:        refs[0],
			Now:         refs[1],
		}
	} else {
		payload = model.BoardWowPayload{
			Description: wow.Body,
			Body:        wow.Body,
			Quotes:      refs,
		}
	}
	return &model.BoardSlot{
		BoardID:       boardID,
		SlotKey:       model.BoardSlotKeyWow,
		Kind:          kind,
		Title:         wow.Title,
		CiteMemoryIDs: citeIDs,
		DreamRunID:    runID,
		Payload:       mustMarshalPayload(payload),
	}
}

// isWowKind reports whether a kind belongs to the rotating wow pool.
func isWowKind(kind string) bool {
	for _, k := range model.WowKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// verifyQuotes keeps only quotes whose memory ids exist, live in this
// hub, and are DISTINCT — citing one memory three times must not
// satisfy a floor of three, or "a pattern across 3+ memories" becomes
// one memory quoted thrice.
func (e *Engine) verifyQuotes(ctx context.Context, hub *model.Hub, quotes []synthesizedQuote) []synthesizedQuote {
	ids := make([]string, 0, len(quotes))
	for _, q := range quotes {
		if q.MemoryID != "" {
			ids = append(ids, q.MemoryID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	accessible, err := e.store.GetAccessibleMemories(ids, hub.OwnerID, []string{hub.ID})
	if err != nil {
		return nil
	}
	var out []synthesizedQuote
	seen := make(map[string]bool, len(quotes))
	for _, q := range quotes {
		mem, ok := accessible[q.MemoryID]
		if !ok || mem.HubID != hub.ID {
			continue
		}
		if strings.TrimSpace(q.Excerpt) == "" || seen[q.MemoryID] {
			continue
		}
		seen[q.MemoryID] = true
		out = append(out, q)
	}
	return out
}

// customBoardSystemPrompt is the cheap path's system prompt: the
// shared core rules plus the board's own brief. The base rules
// (anti-Barnum, receipts, voice) always win — a user instruction
// shapes WHAT to look for, never whether to cite.
func customBoardSystemPrompt(board *model.Board) string {
	base := `You are memax — the user's memory, writing cards for one user-authored pulse board from tonight's night material. You have no tools: work ONLY from the material you are given.

` + boardSynthesisCoreRules
	instruction := strings.TrimSpace(board.Instruction)
	if instruction == "" {
		return base
	}
	return base + fmt.Sprintf(
		"\n\nThis board is user-authored. Their standing brief for it:\n\n%s\n\nHonor the brief when choosing what to surface. It does NOT relax any rule above: no receipts, no card.",
		truncateInstruction(instruction))
}

// truncateInstruction bounds a user instruction inside the prompt.
func truncateInstruction(s string) string {
	const maxInstructionBytes = 2000
	return truncateBytesRuneSafe(s, maxInstructionBytes)
}

// truncateBytesRuneSafe caps s at maxBytes without splitting a rune;
// the cut is marked with an ellipsis.
func truncateBytesRuneSafe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func buildSynthesisPrompt(run *model.DreamRun, wowKind string) string {
	var b strings.Builder
	b.WriteString("Tonight's dream cycle just finished. Stats: ")
	fmt.Fprintf(&b, "%d memories scanned, %d duplicates merged, %d contradictions found, %d memories organized, %d topics restructured.\n\n",
		run.MemoriesScanned, run.DuplicatesMerged, run.ContradictionsFound, run.MemoriesOrganized, run.TopicsRestructured)
	b.WriteString("Write the dreamlog card from these stats plus anything specific you noticed via recall.\n\n")
	b.WriteString("Tonight's wow lens: ")
	b.WriteString(wowKindHints[wowKind])
	b.WriteString("\nUse recall (multiple searches, different angles) to hunt for it. If the hub genuinely doesn't contain one, return null for wow.")
	return b.String()
}

func truncateForTitle(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= 80 {
		return string(runes)
	}
	return string(runes[:79]) + "…"
}

func mustMarshalPayload(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("dreams: marshal board payload: %v", err))
	}
	return data
}
