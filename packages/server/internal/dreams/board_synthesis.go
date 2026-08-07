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
    "now": {"memory_id": "...", "excerpt": "...", "when": "..."},
    "holder": "member name — kind=who_knows only"
  } | null,
  "nextup": {"items": [{"title": "...", "why": "...", "quotes": [{"memory_id": "...", "excerpt": "verbatim quote", "when": "ISO date if known"}]}]} | null
}
"then"/"now" only for kind=echo and kind=team_echo (the old question and the new answer, oldest first). kind=consensus_gap uses exactly two "quotes", one per side. Other kinds use "quotes". Return null for a card you cannot honestly fill.

nextup: infer the 1-3 concrete actions the user most plausibly wants to take next — open loops, stated intentions, unfinished threads found via recall. Each item: imperative title (≤80 chars), one-line why, ≥1 verbatim quote from a real memory proving the loop is open. Anti-Barnum applies fully: no generic productivity advice; if no genuine open loops, null.`

// teamSynthesisContext is appended to the system prompt on a TEAM hub
// and nowhere else. Without it every card comes out addressed to one
// reader, which is how a shared board ends up being the personal board
// with more rows in it. The last sentence is not a threat for its own
// sake — buildWowSlot really does discard team cards whose receipts
// don't match the claim, so telling the model the check exists is the
// cheapest way to stop it guessing.
const teamSynthesisContext = `

TEAM HUB CONTEXT: this hub is shared by several members, and its memories were written by DIFFERENT PEOPLE. Write to the team, not to one person. Never merge two members' notes into one imagined head — attribute a claim to whoever actually wrote the memory. Every claim about the team is verified against the authorship of the memories you cite: a card that says two members disagree while citing two memories by the same person will be discarded, and so will a "who to ask" card whose citations are spread across several people.`

// memberRosterMax bounds the roster line in the prompt — enough for
// the model to name a holder, small enough that a 200-person hub
// doesn't push the actual instructions out of attention.
const memberRosterMax = 12

// boardSynthesisSystemPromptFor picks the system prompt for one hub:
// the shared one, plus the team paragraph and (when the roster is
// available) the member names, on a team hub.
func boardSynthesisSystemPromptFor(hub *model.Hub, members []model.HubMember) string {
	if hub.HubType != model.HubTypeTeam {
		return boardSynthesisSystemPrompt
	}
	prompt := boardSynthesisSystemPrompt + teamSynthesisContext
	if roster := memberRosterLine(members); roster != "" {
		prompt += "\nMembers of this hub: " + roster + "."
	}
	return prompt
}

// memberRosterLine renders the names the model may use. Members with
// no display name are skipped rather than falling back to their email
// — a card should never address someone by their login.
func memberRosterLine(members []model.HubMember) string {
	names := make([]string, 0, len(members))
	for _, m := range members {
		name := strings.TrimSpace(m.UserName)
		if name == "" {
			continue
		}
		names = append(names, name)
		if len(names) >= memberRosterMax {
			break
		}
	}
	return strings.Join(names, ", ")
}

// memberNamesByID is the owner-id → display-name lookup used to fill
// the who_knows holder from the AUTHOR of the cited memories instead
// of trusting the model's own attribution.
func memberNamesByID(members []model.HubMember) map[string]string {
	if len(members) == 0 {
		return nil
	}
	names := make(map[string]string, len(members))
	for _, m := range members {
		if name := strings.TrimSpace(m.UserName); name != "" {
			names[m.UserID] = name
		}
	}
	return names
}

// customBoardCardRequest closes the custom board's user prompt. It
// lives with the material (not the system prompt) so the output
// contract sits right next to the data it applies to.
const customBoardCardRequest = `Based on the night material above, write 0-2 cards for this board as a JSON array:
[{"kind": "pattern"|"thread", "title": "one plain-text line, the hook", "body": "2-4 sentences making the specific claim", "quotes": [{"memory_id": "...", "excerpt": "verbatim quote"}]}]
Only write a card if the material is genuinely relevant to this board's brief — an empty array [] is a good answer. Cite only memory ids that appear in the material.`

// wowKindHints steers the agent's exploration per rotated kind.
var wowKindHints = map[string]string{
	model.BoardKindEcho:         "echo (回声): find a question or uncertainty the user recorded 30+ days ago that a RECENT memory now answers or settles. The payoff is the time gap.",
	model.BoardKindThread:       "thread (暗线): find two memories from different times or contexts that are plausibly the same underlying idea the user never connected. Be conservative — a false connection is worse than none.",
	model.BoardKindPattern:      "pattern (未观察模式): find a recurring behavior visible across 3+ memories that the user likely hasn't noticed about themselves. Must be provable from the citations.",

	// Team-only lenses. Each one must cite memories written by the
	// right MIX of people or it is dropped after the fact (see
	// buildWowSlot) — say nothing rather than fake a team claim.
	model.BoardKindConsensusGap: "consensus_gap (共识缺口): find one thing TWO DIFFERENT members of this hub understand differently — not a contradiction inside one person's notes, but two people who each sound sure and who disagree with each other. Cite exactly two memories, one per person, and they MUST be written by different members. If everyone in the hub agrees, return null.",
	model.BoardKindTeamEcho:     "team_echo (团队回声): find a question or uncertainty ONE member recorded, which a DIFFERENT member later answered without ever connecting the two. Put the older question in \"then\" and the newer answer in \"now\" — they must be written by different members, oldest first. The payoff is that the hub already held the answer.",
	model.BoardKindWhoKnows:     "who_knows (谁知道这个): pick a topic with recent activity in this hub and identify the ONE member whose memories dominate it, so everyone else knows who to ask. Cite 2+ memories that all belong to that SAME member, and put their name in \"holder\". If the topic's memories are spread evenly across members, there is no holder — return null.",
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
	// Holder is who_knows only: the member to ask. Treated as a
	// fallback — the producer prefers the roster name of the owner who
	// actually wrote the cited memories over whatever the model typed.
	Holder string `json:"holder"`
}

// synthesizedNextUpItem is one predicted action from the system
// session's nextup field: title + why + the quotes claiming the loop
// is open. Validated per item by buildNextUpSlot.
type synthesizedNextUpItem struct {
	Title  string             `json:"title"`
	Why    string             `json:"why"`
	Quotes []synthesizedQuote `json:"quotes"`
}

type synthesizedNextUp struct {
	Items []synthesizedNextUpItem `json:"items"`
}

type synthesisResponse struct {
	Dreamlog *struct {
		Body string `json:"body"`
	} `json:"dreamlog"`
	Wow    *synthesizedWow    `json:"wow"`
	NextUp *synthesizedNextUp `json:"nextup"`
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
// tries a different lens without storing rotation state. The pool
// depends on hub type: a personal hub rotates over the three personal
// lenses, a team hub over six (the personal three plus the team-native
// three), so a shared board says something about the group roughly
// half the nights.
func pickWowKind(boardID, hubType string, day time.Time) string {
	seed := 0
	for _, r := range boardID {
		seed += int(r)
	}
	seed += day.YearDay() + day.Year()*366
	pool := model.WowKindsForHub(hubType)
	return pool[seed%len(pool)]
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

	// Roster: team hubs only, one indexed read per night. It feeds the
	// prompt (so the model can name a member) AND the who_knows holder
	// resolution (so the NAME on the card comes from the authorship of
	// the receipts, not from the model). A failure here is not fatal —
	// the prompt falls back to generic "different members" wording and
	// owner-id validation still holds the line.
	var members []model.HubMember
	if hub.HubType == model.HubTypeTeam {
		var err error
		if members, err = e.store.ListHubMembers(hub.ID); err != nil {
			slog.WarnContext(ctx, "dream: board synthesis roster unavailable",
				"hub_id", hub.ID, "error", err)
		}
	}

	wowKind := pickWowKind(board.ID, hub.HubType, time.Now().UTC())
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
		SystemPrompt: boardSynthesisSystemPromptFor(hub, members),
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

	if wowSlot := e.buildWowSlot(ctx, hub, board.ID, run.ID, wowKind, &parsed, memberNamesByID(members)); wowSlot != nil {
		if err := e.store.UpsertBoardSlot(wowSlot); err != nil {
			metrics.Errors++
		} else {
			written++
		}
	}

	// 接下来 — system board only, like the dreamlog: the prediction
	// needs the recall tool's open-loop hunting, which custom boards'
	// cheap path doesn't have.
	if nextUpSlot := e.buildNextUpSlot(ctx, hub, board.ID, run.ID, &parsed); nextUpSlot != nil {
		if err := e.store.UpsertBoardSlot(nextUpSlot); err != nil {
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
		n, boardMetrics := e.synthesizeCustomBoard(ctx, hub, run, &board, material, runBudget)
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
	runBudget *dreamRunBudget,
) (int, model.DreamPhaseMetrics) {
	// One-time cleanup: boards synthesized before the one-dream-one-
	// dreamlog refactor each wrote their own 梦记. Nothing overwrites
	// those slots any more, so without this they'd sit on the board
	// forever showing a months-old first-person night report.
	if err := e.store.DeleteBoardSlot(board.ID, model.BoardSlotKeyDreamlog); err != nil {
		slog.WarnContext(ctx, "dream: could not drop legacy dreamlog slot",
			"board_id", board.ID, "error", err)
	}

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
	// Debit the cycle governor: these are real model calls, and
	// without counting them the loop's shouldRoute gate can never trip
	// and cycle-end budget telemetry undercounts what was spent.
	runBudget.consumeModelCalls(1)
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
		}, nil)
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

// isCustomCardKind is the kinds the cheap path may produce. echo
// needs the recall tool's cross-time hunting, so the material-only
// path doesn't offer it; pattern and thread are both provable from
// the material pack's citations.
func isCustomCardKind(kind string) bool {
	switch kind {
	case model.BoardKindPattern, model.BoardKindThread:
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

// consensusGapSides is the exact number of quotes a 共识缺口 card
// carries — it is a two-sided card by definition, so "one side and a
// half" or "three sides" is a different claim than the one the kind
// makes.
const consensusGapSides = 2

// buildWowSlot validates a synthesized wow card against the citation
// floor, the hub-membership of every quoted memory, and (for the team
// kinds) the AUTHORSHIP composition its claim implies. A card that
// fails any check is dropped silently — no card beats a wrong card.
//
// members is the hub's owner-id → display-name roster, used only to
// fill the who_knows holder from the real author of the receipts. Nil
// on personal hubs and on the custom-board path.
func (e *Engine) buildWowSlot(
	ctx context.Context,
	hub *model.Hub,
	boardID, runID, requestedKind string,
	parsed *synthesisResponse,
	members map[string]string,
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
		if !isWowKindForHub(hub, kind) {
			return nil
		}
	}
	// A team kind on a personal hub is unfillable by construction: one
	// writer cannot disagree with a colleague or be the person to ask.
	// Guarded here as well as in the rotation pool so a stray kind in
	// the model's output can't route around it.
	if model.IsTeamWowKind(kind) && hub.HubType != model.HubTypeTeam {
		return nil
	}

	var quotes []synthesizedQuote
	switch kind {
	case model.BoardKindEcho, model.BoardKindTeamEcho:
		if wow.Then == nil || wow.Now == nil {
			return nil
		}
		quotes = []synthesizedQuote{*wow.Then, *wow.Now}
	default:
		quotes = wow.Quotes
	}
	if kind == model.BoardKindConsensusGap && len(quotes) != consensusGapSides {
		return nil
	}

	floor, _ := model.LaneBCitationFloor(kind)
	verified := e.verifyQuotesWithOwners(ctx, hub, quotes)
	if len(verified) < floor || len(verified) < len(quotes) {
		// Any invented citation kills the card, not just the quote —
		// a card that half-lies about receipts is worse than none.
		slog.InfoContext(ctx, "dream: wow card dropped by citation validator",
			"hub_id", hub.ID, "kind", kind,
			"claimed", len(quotes), "verified", len(verified), "floor", floor)
		return nil
	}

	// Owner composition. This is the whole difference between a team
	// card and a personal card wearing plural pronouns: "two members
	// disagree" quoting one person twice is a fabricated collaboration,
	// and "ask Wei about deploys" quoting three people names the wrong
	// person. Enforced on the OWNER of the stored memory, never on
	// anything the model claimed.
	ownerIDs := make([]string, 0, len(verified))
	for _, q := range verified {
		ownerIDs = append(ownerIDs, q.OwnerID)
	}
	rule := model.LaneBOwnerRule(kind)
	if !model.SatisfiesOwnerRule(rule, ownerIDs) {
		slog.InfoContext(ctx, "dream: team card dropped by owner-diversity rule",
			"hub_id", hub.ID, "kind", kind, "rule", rule, "owners", len(ownerIDs))
		return nil
	}
	// 团队回声 is a claim about direction: A asked, then B answered. If
	// the stored timestamps say otherwise the card is telling the story
	// backwards, and swapping the pair would silently rewrite who
	// answered whom — so drop it instead.
	if kind == model.BoardKindTeamEcho && !orderedOldToNew(verified[0], verified[1]) {
		slog.InfoContext(ctx, "dream: team echo dropped, answer predates the question",
			"hub_id", hub.ID)
		return nil
	}

	citeIDs := make([]string, 0, len(verified))
	refs := make([]model.BoardQuoteRef, 0, len(verified))
	for _, q := range verified {
		citeIDs = append(citeIDs, q.MemoryID)
		refs = append(refs, model.BoardQuoteRef{
			MemoryID: q.MemoryID,
			When:     q.When,
			Excerpt:  q.Excerpt,
			// Attribution from the roster, keyed on the stored owner —
			// on a team card "who said this" is half the content, and it
			// is the half the model must not be trusted with. Empty when
			// the hub has no roster or the author has left.
			Author: members[q.OwnerID],
		})
	}

	var payload any
	switch kind {
	case model.BoardKindEcho, model.BoardKindTeamEcho:
		payload = model.BoardEchoPayload{
			Description: wow.Body,
			Body:        wow.Body,
			Then:        refs[0],
			Now:         refs[1],
		}
	case model.BoardKindConsensusGap:
		payload = model.BoardConsensusPayload{
			Description: wow.Body,
			Body:        wow.Body,
			Sides:       refs,
		}
	case model.BoardKindWhoKnows:
		// Owner rule already proved every receipt has the same author,
		// so verified[0] IS the holder. Prefer the roster name over the
		// model's — the model is guessing, the roster is a fact. Without
		// either there is nobody to ask, and the card has no content.
		holder := members[verified[0].OwnerID]
		if holder == "" {
			holder = strings.TrimSpace(wow.Holder)
		}
		if holder == "" {
			return nil
		}
		payload = model.BoardWhoKnowsPayload{
			BoardWowPayload: model.BoardWowPayload{
				Description: wow.Body,
				Body:        wow.Body,
				Quotes:      refs,
			},
			Holder: holder,
		}
	default:
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

// boardNextUpMaxItems caps the 接下来 card — three predictions is a
// nudge, ten is a backlog.
const boardNextUpMaxItems = 3

// buildNextUpSlot validates the system session's nextup prediction.
// The citation gate is per ITEM, not per card: every item must keep
// ≥1 verified quote or it is dropped; the card ships only if at least
// one item survives. An invented or cross-hub quote kills its item
// (verifyQuotes filters it out), but not its siblings — unlike a wow
// card, each prediction stands or falls on its own receipts.
// countDistinctQuotedMemories counts unique non-empty memory ids in a
// quote list — verifyQuotes de-duplicates, so the comparison must be
// against distinct claims, not raw quote count.
func countDistinctQuotedMemories(quotes []synthesizedQuote) int {
	seen := make(map[string]bool, len(quotes))
	for _, q := range quotes {
		if q.MemoryID != "" {
			seen[q.MemoryID] = true
		}
	}
	return len(seen)
}

func (e *Engine) buildNextUpSlot(
	ctx context.Context,
	hub *model.Hub,
	boardID, runID string,
	parsed *synthesisResponse,
) *model.BoardSlot {
	if parsed.NextUp == nil || len(parsed.NextUp.Items) == 0 {
		return nil
	}

	items := make([]model.BoardNextUpItem, 0, boardNextUpMaxItems)
	citeIDs := make([]string, 0, boardNextUpMaxItems)
	seenCite := make(map[string]bool)
	for _, raw := range parsed.NextUp.Items {
		if len(items) >= boardNextUpMaxItems {
			break
		}
		title := strings.TrimSpace(raw.Title)
		if title == "" {
			continue
		}
		// Same bar as the wow card: ANY invented or cross-hub quote
		// kills the item, not just an item with zero survivors. An
		// item whose evidence is one real memory plus two fabricated
		// ones is a half-lie, and half-lying receipts are worse than
		// no card — the user can't tell which half is real.
		verified := e.verifyQuotes(ctx, hub, raw.Quotes)
		if len(verified) == 0 || len(verified) < countDistinctQuotedMemories(raw.Quotes) {
			slog.InfoContext(ctx, "dream: nextup item dropped by citation validator",
				"hub_id", hub.ID, "title", truncateForTitle(title),
				"claimed", len(raw.Quotes), "verified", len(verified))
			continue
		}
		refs := make([]model.BoardQuoteRef, 0, len(verified))
		for _, q := range verified {
			refs = append(refs, model.BoardQuoteRef{MemoryID: q.MemoryID, When: q.When, Excerpt: q.Excerpt})
			if !seenCite[q.MemoryID] {
				seenCite[q.MemoryID] = true
				citeIDs = append(citeIDs, q.MemoryID)
			}
		}
		items = append(items, model.BoardNextUpItem{
			Title:  truncateForTitle(title),
			Why:    strings.TrimSpace(raw.Why),
			Quotes: refs,
		})
	}
	if len(items) == 0 {
		return nil
	}

	// Description joins the surviving titles so the unknown-kind
	// fallback still shows the actual predictions as plain text.
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	return &model.BoardSlot{
		BoardID:       boardID,
		SlotKey:       model.BoardSlotKeyNextUp,
		Kind:          model.BoardKindNextUp,
		Title:         items[0].Title,
		CiteMemoryIDs: citeIDs,
		DreamRunID:    runID,
		Payload: mustMarshalPayload(model.BoardNextUpPayload{
			Description: strings.Join(titles, " · "),
			Items:       items,
		}),
	}
}

// isWowKindForHub reports whether a kind belongs to this hub's
// rotating wow pool — three lenses on a personal hub, six on a team.
func isWowKindForHub(hub *model.Hub, kind string) bool {
	for _, k := range model.WowKindsForHub(hub.HubType) {
		if k == kind {
			return true
		}
	}
	return false
}

// verifiedQuote is a surviving quote plus the facts about the memory
// behind it that the team kinds have to judge: who WROTE it and when
// it was stored. Both come from the store, never from the model — the
// whole point is that authorship is checked, not claimed.
type verifiedQuote struct {
	synthesizedQuote
	OwnerID   string
	CreatedAt time.Time
}

// orderedOldToNew reports whether a precedes b by stored creation
// time. Missing timestamps pass: an unknown order is not evidence of
// a wrong one, and the citation gates have already done the load-
// bearing work.
func orderedOldToNew(a, b verifiedQuote) bool {
	if a.CreatedAt.IsZero() || b.CreatedAt.IsZero() {
		return true
	}
	return !b.CreatedAt.Before(a.CreatedAt)
}

// verifyQuotes keeps only quotes whose memory ids exist, live in this
// hub, and are DISTINCT — citing one memory three times must not
// satisfy a floor of three, or "a pattern across 3+ memories" becomes
// one memory quoted thrice.
func (e *Engine) verifyQuotes(ctx context.Context, hub *model.Hub, quotes []synthesizedQuote) []synthesizedQuote {
	verified := e.verifyQuotesWithOwners(ctx, hub, quotes)
	out := make([]synthesizedQuote, 0, len(verified))
	for _, q := range verified {
		out = append(out, q.synthesizedQuote)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// verifyQuotesWithOwners is the single access-checked verification
// path; verifyQuotes is the projection of it for callers that don't
// care about authorship. Kept as one function so the hub-isolation
// logic can never drift between a "personal" and a "team" copy.
func (e *Engine) verifyQuotesWithOwners(_ context.Context, hub *model.Hub, quotes []synthesizedQuote) []verifiedQuote {
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
	var out []verifiedQuote
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
		out = append(out, verifiedQuote{
			synthesizedQuote: q,
			OwnerID:          mem.OwnerID,
			CreatedAt:        mem.CreatedAt,
		})
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
