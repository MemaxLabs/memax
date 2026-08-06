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

	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
)

// Phase 6 — Lane B board synthesis (plan 25 P2). One agent session per
// dream cycle writes the 梦记 (dreamlog) slot and one rotating wow
// card. Gated the same two-layer way as the other agentic phases:
// AgentRuntime configured at the app layer AND the hub's
// dreams_use_agent_runtime setting. Cost rides the cycle's shared
// budget governor; the whole phase is skipped when the governor is
// exhausted so board polish never crowds out organize/contradiction
// work.

// boardSynthesisMinInterval dedupes synthesis across rapid re-runs: a
// manually re-triggered dream within the window keeps last night's
// cards instead of burning agent calls on near-identical content.
const boardSynthesisMinInterval = 18 * time.Hour

const boardSynthesisSystemPrompt = `You are memax — the user's memory, speaking in first person about what you noticed while organizing their hub overnight. You write cards for the hub's pulse board.

Non-negotiable rules:
1. ANTI-BARNUM: never write a claim that could apply to anyone ("you've been busy lately", "you care about quality"). Every claim must be specific enough that the quoted memories PROVE it. If you cannot find a genuinely specific insight, omit the card entirely — an empty result is a good result; a hollow card destroys trust.
2. RECEIPTS: every card cites the memories behind it via their exact memory ids from recall results. Quote excerpts verbatim — do not paraphrase inside quotes.
3. VOICE: first person, warm, concise, zero corporate filler. Chinese memories get Chinese cards; English memories get English cards; mixed hubs follow the dominant language of the cited memories.
4. PLAIN TEXT: no markdown, no HTML — card text renders literally.

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

type synthesisResponse struct {
	Dreamlog *struct {
		Body string `json:"body"`
	} `json:"dreamlog"`
	Wow *struct {
		Kind   string             `json:"kind"`
		Title  string             `json:"title"`
		Body   string             `json:"body"`
		Quotes []synthesizedQuote `json:"quotes"`
		Then   *synthesizedQuote  `json:"then"`
		Now    *synthesizedQuote  `json:"now"`
	} `json:"wow"`
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
	if enabled, _ := settings["dreams_use_agent_runtime"].(bool); !enabled {
		return false
	}
	return runBudget.shouldRoute(runID)
}

// phaseBoardSynthesis runs the Lane B agent and writes the slots.
// Returns the number of cards written.
func (e *Engine) phaseBoardSynthesis(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	runBudget *dreamRunBudget,
) (int, model.DreamPhaseMetrics) {
	var metrics model.DreamPhaseMetrics
	start := time.Now()
	defer func() { metrics.DurationMs = time.Since(start).Milliseconds() }()

	// The system board always exists; custom boards (P4) are authored
	// by the user and carry their own instruction. Each board gets its
	// own agent session so one board's brief never bleeds into another.
	if _, err := e.store.GetOrCreateSystemBoard(hub.ID, hub.OwnerID); err != nil {
		slog.WarnContext(ctx, "dream: board synthesis skipped", "hub_id", hub.ID, "error", err)
		metrics.Errors++
		return 0, metrics
	}
	boards, err := e.store.ListBoardsByHub(hub.ID)
	if err != nil {
		metrics.Errors++
		return 0, metrics
	}

	written := 0
	for i := range boards {
		board := boards[i]
		// Paused boards are excluded; cooking boards are INCLUDED —
		// this run is exactly what they've been waiting for.
		if board.Status == model.BoardStatusPaused {
			continue
		}
		if !runBudget.shouldRoute(run.ID) {
			// The cycle's governor is spent; remaining boards wait for
			// tomorrow rather than starving the organizational phases.
			break
		}
		n, boardMetrics := e.synthesizeBoard(ctx, hub, run, runBudget, &board)
		written += n
		mergeSynthesisMetrics(&metrics, boardMetrics)
	}
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

// synthesizeBoard runs one agent session for one board.
func (e *Engine) synthesizeBoard(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	runBudget *dreamRunBudget,
	board *model.Board,
) (int, model.DreamPhaseMetrics) {
	var metrics model.DreamPhaseMetrics

	// Cadence dedupe: keep last night's cards if they're still fresh.
	// A cooking board has no slots yet, so it always proceeds.
	if existing, err := e.store.GetBoardSlot(board.ID, model.BoardSlotKeyDreamlog); err == nil {
		if time.Since(existing.UpdatedAt) < boardSynthesisMinInterval {
			return 0, metrics
		}
	}

	// Rotation keys on the board, not the hub, so two boards on the
	// same hub don't chase the same lens on the same night.
	wowKind := pickWowKind(board.ID, time.Now().UTC())
	prompt := buildSynthesisPrompt(run, wowKind, board)

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
		return 0, metrics
	}

	res, err := agent.Run(ctx, agent.RunInput{
		Prompt:       prompt,
		Profile:      agent.ProfileDreamActive,
		Tools:        []sdktool.Tool{recallTool},
		Model:        e.agentRuntime.Model,
		Session:      session,
		SystemPrompt: boardSystemPrompt(board),
	})
	if err != nil {
		metrics.Errors++
		metrics.LLMErrors++
		return 0, metrics
	}
	runBudget.consume(&res)
	metrics.LLMCalls += res.ModelCalls
	metrics.TokensIn += int64(res.Usage.InputTokens)
	metrics.TokensOut += int64(res.Usage.OutputTokens)
	if res.Status != agent.RunStatusSuccess {
		metrics.LLMErrors++
		slog.WarnContext(ctx, "dream: board synthesis agent terminated",
			"hub_id", hub.ID, "status", res.Status, "err", res.Err)
		return 0, metrics
	}

	var parsed synthesisResponse
	if err := json.Unmarshal([]byte(anthropic.ExtractJSONObject(res.Final)), &parsed); err != nil {
		metrics.Errors++
		slog.WarnContext(ctx, "dream: board synthesis parse", "hub_id", hub.ID, "error", err)
		return 0, metrics
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

	// 酝酿中 → 活跃: a custom board leaves the cooking state the moment
	// it has its first real card. If synthesis produced nothing, it
	// keeps cooking and the UI keeps promising tomorrow rather than
	// showing an empty board that looks broken.
	if written > 0 && board.Status == model.BoardStatusCooking {
		board.Status = model.BoardStatusActive
		if err := e.store.UpdateBoard(board); err != nil {
			slog.WarnContext(ctx, "dream: could not activate cooking board",
				"board_id", board.ID, "error", err)
		}
	}
	return written, metrics
}

// buildWowSlot validates the agent's wow card against the citation
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
		// if it's still a known wow kind — an unknown kind would hit
		// the fallback renderer with generated text.
		if _, ok := model.LaneBCitationFloor(kind); !ok {
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

// verifyQuotes keeps only quotes whose memory ids exist AND live in
// this hub. Excerpts stay as the agent wrote them (they were told to
// quote verbatim; enforcing that would require content fetch + fuzzy
// match, tracked for a later hardening pass).
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
	for _, q := range quotes {
		mem, ok := accessible[q.MemoryID]
		if !ok || mem.HubID != hub.ID {
			continue
		}
		if strings.TrimSpace(q.Excerpt) == "" {
			continue
		}
		out = append(out, q)
	}
	return out
}

// boardSystemPrompt appends the board's own brief for custom boards.
// The base rules (anti-Barnum, receipts, voice) always win — a user
// instruction shapes WHAT to look for, never whether to cite.
func boardSystemPrompt(board *model.Board) string {
	instruction := strings.TrimSpace(board.Instruction)
	if instruction == "" {
		return boardSynthesisSystemPrompt
	}
	return boardSynthesisSystemPrompt + fmt.Sprintf(
		"\n\nThis board is user-authored. Their standing brief for it:\n\n%s\n\nHonor the brief when choosing what to surface. It does NOT relax any rule above: no receipts, no card.",
		truncateForTitle2(instruction))
}

// truncateForTitle2 bounds a user instruction inside the prompt.
func truncateForTitle2(s string) string {
	const maxInstructionChars = 2000
	if len(s) <= maxInstructionChars {
		return s
	}
	cut := maxInstructionChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func buildSynthesisPrompt(run *model.DreamRun, wowKind string, board *model.Board) string {
	var b strings.Builder
	b.WriteString("Tonight's dream cycle just finished. Stats: ")
	fmt.Fprintf(&b, "%d memories scanned, %d duplicates merged, %d contradictions found, %d memories organized, %d topics restructured.\n\n",
		run.MemoriesScanned, run.DuplicatesMerged, run.ContradictionsFound, run.MemoriesOrganized, run.TopicsRestructured)
	b.WriteString("Write the dreamlog card from these stats plus anything specific you noticed via recall.\n\n")
	b.WriteString("Tonight's wow lens: ")
	b.WriteString(wowKindHints[wowKind])
	b.WriteString("\nUse recall (multiple searches, different angles) to hunt for it. If the hub genuinely doesn't contain one, return null for wow.")
	if title := strings.TrimSpace(board.Title); title != "" {
		fmt.Fprintf(&b, "\n\nYou are writing for the board titled %q — keep both cards on that subject.", title)
	}
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
