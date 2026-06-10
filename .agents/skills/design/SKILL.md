---
name: design
description: Use when designing or building any UI — components, layouts, interactions, styles, visual decisions. ALWAYS use this skill when the task involves frontend code in packages/web/, even for 'small' CSS changes or 'quick' component tweaks. Design thinking applies to every visual change, not just new features.
---

You are designing and building frontend for memax. Before writing any code, think through the design. Before implementing, check the rules.

## Workflow

**Bug fix:**

1. Read this skill (especially checklist item 0)
2. Locate root cause — is this a surface bug or a structural problem?
3. If structural: fix the structure, not the symptom
4. Fix → `pnpm format` → commit

**New feature:**

1. Read `docs/design/uiux-strategy.md` — find the feature's spec
2. Read this skill — **start with checklist item 0**: where does this feature belong? Does the current structure support it? Am I using framework patterns or inventing my own?
3. If the structure isn't right: restructure first, then build the feature
4. Read `.agents/skills/i18n/SKILL.md` — add i18n keys BEFORE writing JSX
5. Implement → `pnpm format` → commit
6. Update roadmap if applicable

## Reference docs

- **`/dev/kitchen` (source of truth)** — **the design system**: 28 sections organized as Foundations → Components → Patterns. Section 0 has an LLM-optimized rules block for code generation. Section 26 has copy-paste composition recipes. Section 27 has WCAG accessibility data.
- `docs/design/memax-design-system.md` — **design philosophy**: explains the _why_ behind decisions. Does NOT contain token values (those are in the kitchen).
- `references/implementation.md` — **codebase patterns**: portal architecture, routing, state tiers, component tiers. Read when writing code, not during design thinking.
- This file — **design thinking**: checklist, interaction rules, anti-patterns
- `docs/plans/08-web-experience.md` — **implementation status**: what's built, what's remaining

**i18n: every user-facing string must go through `t.*`.** See `.agents/skills/i18n/SKILL.md` for the full i18n guide. When writing new UI text, add keys to `en.ts` + `zh.ts` FIRST, then use `t.section.key` in JSX. Never hardcode English strings.

## Kitchen Workflow (design iteration)

The kitchen at `/dev/kitchen` is a workbench, not a spec doc. Use it to iterate visually.

- **To iterate a pattern:** edit the section file in `_sections/NN-name.tsx`, hot-reload, compare with production
- **To add a section:** create `_sections/NN-name.tsx`, export a named component, import in `page.tsx`
- **To propagate to production:** each section header has a `// Maps to:` comment — update those files
- **Palette switching:** `_kitchen-context.tsx` provides theme presets via `useKitchen()`. Add presets there.
- **`packages/ui/` migration:** when a `ui/` component passes migration criteria (zero web imports, CSS vars only), move it to `packages/ui/` and update imports across packages

## Core Principle: Container Morphing

**The same container adapts to different states and information densities. Never spawn a new overlay when the existing container can transform.**

This is the defining interaction pattern of memax. Containers are persistent surfaces that reshape themselves:

- **Bar:** One element → brain view (centered 46vh), memory view (bottom), idle/typing/recall/remember states. Position, content, and behavior all change. The container stays.
- **Memory detail:** Tapping any memory row (from any surface — brain view recall, memory grid, topic detail) navigates to `/memories/[id]`. One borderless detail page, no modals.

**The test:** Before creating a new surface (modal, overlay, panel), ask: "Is there already a container on screen that could show this content?" If yes, morph that container. If no, then use a route.

## Two Interaction Paradigms

memax has two fundamentally different views:

**Brain view = intent-driven.** User types what they want, system acts. The bar is the only interface — centered at 46vh, clean background, nothing else. This is memax's differentiation.

**Memory view = browse-driven.** User scans, filters, clicks. Memory rows, topic cards — traditional UI patterns (Linear, Apple Notes). The bar moves to the bottom.

|                           | Brain             | Memory                |
| ------------------------- | ----------------- | --------------------- |
| User knows what they want | ✓ just ask        | slow — has to find    |
| User is exploring         | ✗ ask what?       | ✓ browse and discover |
| Few memories              | overkill          | ✓ scan all            |
| Many memories             | ✓ only viable way | slow                  |

**Key rule:** Every memory tap navigates to `/memories/[id]` — one unified detail experience.

## Visual Identity

**Quiet, alive, confident.** See kitchen Section 0 for full principles.

- **Opacity architecture** — all colors derive from `--foreground` via oklch opacity. 4 text levels (`text-fg-1` through `text-fg-4`), 3 surface levels (`bg-surface-1` through `bg-surface-3`). Never use raw `text-foreground/XX` or `bg-foreground/X`.
- **Signature color (Dream Violet)** `var(--signature)` — appears ONLY where memax AI works: ✦ star, loading pulse, recall send button, active nav dot. Never decorative.
- **Spring easing always** — `var(--ease-spring)` for all transitions. Never linear, never ease-in-out.
- **Category dots** — small colored dots identify content categories. Independent of signature color. Source: `lib/category.ts`.
- **Functional glass only** — `backdrop-filter` communicates layer hierarchy. Never decorative.
- **No decorative elements** — no ghost cards, no grid lines, no gradient blobs, no colored borders.

**For specific token values, px sizes, contrast ratios:** read the kitchen, not this file.

## The Checklist

### Always Check (every change)

#### 0. Am I working with the framework or against it?

Before writing any code, before thinking about UI — ask these three questions:

- **Am I using the framework's solution or inventing my own?** Next.js has routing, layouts, parallel routes, server components. Go has interfaces, context, stdlib patterns. If you're building something the framework already provides (custom events for navigation, hand-rolled state machines for routing, manual data fetching that a framework hook handles), stop. Use the framework's version.
- **If this feature gets 3 more variants, does the structure still work?** One phase in a switch statement is fine. Ten phases in a 3000-line file is not. Think forward: will the next person adding a similar feature follow this same pattern? If that leads somewhere ugly, the pattern is wrong now.
- **Am I adding to a file or adding to the system?** Adding lines to a file is easy. Adding a capability to a well-structured system is harder but right. If your change only makes sense by reading the entire file top-to-bottom, it's coupled wrong. Each piece should be understandable in isolation.

If any answer is "I'm working around the framework" or "this won't scale" — fix the structure first, then add the feature. Never accumulate structural debt to ship faster.

#### 1. What is the user's intent?

- Name the primary intent. Everything on screen serves it.
- Name secondary intents. Reachable but visually quieter.
- Everything else is noise — remove it.
- Intent changes after each action. Design the transition.

#### 2. Is this the simplest version?

- Explained on the phone, would it sound obvious?
- Could a less clever solution work? Prefer boring and clear.
- Count states this component tracks. More than 3 → probably overcomplicating.

#### 3. What's the hierarchy?

- Squint test. What stands out? Is that the right thing?
- Max 3 levels of visual importance per screen.
- Hierarchy through size/weight/opacity, not decoration.

### Check When Relevant

#### 4. Does this belong here?

- Most natural place, or where there's space?
- What happened 1s before? What happens 1s after?
- If removed, would anyone notice?

#### 5. How does this affect its neighbors?

- Visual weight match surrounding elements?
- Works with different content? (Empty, 1 item, 100 items, long/short text)
- Spacing intentional or leftover?

#### 6. Is the motion necessary?

**Productivity app = instant feel.** Animation serves loading, not completed operations.

- Only animate waits (loading spinners, processing states)
- Don't animate results — data ready = show immediately
- **Loading → loaded transition (MANDATORY):** EVERY skeleton/loader → content swap MUST use `animate-content-ready` on the content wrapper (0.15s opacity fade). No exceptions — pop-in without animation looks broken. Pattern: `{isLoading ? <Skeleton /> : <div className="animate-content-ready">...content...</div>}`.
- Modal open/close: 0.1s opacity only, no scale
- Intent toggle: instant swap, no blur/fade
- Use motion tokens from `lib/motion.ts` — never hardcode durations

#### 7. Are elements consistent?

- Same radius, spacing, opacity, font size as similar elements?
- All floating panels: `rounded-2xl`, `border border-border`, halo shadow
- All interactive elements: cursor-pointer, hover state, focus ring

#### 8. Is the color justified?

- What does this color tell the user they wouldn't know without it?
- Hierarchy works without color? (Test in grayscale)
- No blue/purple tint anywhere — check oklch chroma is 0

#### 9. Mobile + Accessibility

- 320px first, touch targets 44px minimum
- Text contrast 4.5:1 (WCAG AA)
- `prefers-reduced-motion` for animations
- Keyboard navigable, aria labels on icon buttons

## Before Making a Decision

**Think in interaction flow, not visual layout.**

Every UI decision has two frames: **visual** (how it looks in a screenshot) and **physical** (how the cursor/finger moves through it in real time). When they conflict, physical wins.

- **Where is the cursor NOW?** After clicking a button, the cursor is at that button's position. Whatever appears there next should be the SAFE default — not the destructive continuation.
- **What's the user's momentum?** Fast double-tap, slow deliberate click, or scanning without clicking? Design for the most dangerous realistic accident.
- **What does the user see 100ms after their action?** Not 500ms (animation done). The instant feedback determines whether they feel in control or panicked.

**Common traps:**

- Optimizing for reading order when the interaction has its own spatial flow. Reading order is for scanning. Interaction order is for the cursor path.
- Centering an element at 50% when it has content extending in one direction. Center the CLUSTER (element + content), not the element alone. Bar at 46vh + content below = cluster centered at ~50vh.
- Giving mobile the same content as desktop but smaller. Mobile has different capabilities — no drag-drop, no terminal, no keyboard shortcuts. Show different content, not scaled-down desktop.

## When a Problem is Raised

**Do not jump to fixing the symptom.** Diagnose root cause first.

1. **Why does this problem exist?** Trace back through decisions.
2. **Isolated bug or systemic?** Check all places the same pattern is used.
3. **What decision led here?** Conscious choice gone wrong, or unconsidered default?
4. **Prevention?** What rule would have caught this earlier?

**Think globally:** If you change a card's border, check every card variant. If you change spacing, check every context. A design fix is about the system, not the element.

## Interaction Principles

These govern all stateful UI — the bar, collection container, modals, any morphing surface.

**1. State persistence — results are context, not ephemeral.**
Typing refines intent, it doesn't destroy visible results. Only explicit actions (Enter, Esc, ✕) transition state. The user builds mental context from what's on screen — removing it on every keystroke forces them to rebuild.

**2. Layered dismiss — every Esc peels back one layer.**
`new text → original query → dismiss results → clear input → blur → back to brain view`. Never skip levels. The user's expectation after Esc is "undo the last thing I did," not "nuke everything."

**3. Async cancellation — UI state and network state are independent.**
Dismissing the UI must invalidate in-flight requests. Use request ID refs for async functions (`aiRequestId`), query guards for TanStack Query effects (`recallQuery` check). A stale API response arriving after dismiss should be silently discarded.

**4. CTA hierarchy — one primary per view.**
When actions compete, only the most important gets the dark pill. Secondary actions use ghost outline (`border border-foreground/15`). Hide CTAs that don't apply to the current content (no "Ask memax" inside a doc view).

**5. State restoration on navigation.**
Back buttons must restore the state the user came from. Leaving memory detail → restore `value` to `recallQuery`, clear `noteSearch`. Leaving AI answer → keep `askAnswer` in state so it shows again. Navigation is reversible.

**6. Cross-cutting consistency — change the pattern, change ALL instances.**
When a pattern appears in multiple contexts (forget confirmation in memory detail + bar detail + card grid + list view), they must all use the same language, same layout, same interaction. Before implementing, grep for all instances. Before shipping, verify every instance matches. One stale instance = user confusion.

**7. Destructive actions — container morphing for confirmation.**
Never use same-button-twice to delete (accidental double-click). Never spawn a new row/modal for confirmation (violates morphing). Instead: the existing container morphs its state.

- **Header/card bg changes** to subtle red (`oklch(from var(--destructive) l c h / 0.08)`) with `transition-colors`
- **Forget button** becomes `[Forget] [Keep]` — two distinct click targets
- **Button order: destructive LEFT, safe RIGHT.** User clicked Forget on the right → cursor is there. Keep (safe) appears where cursor already is. Forget (destructive) moves to the left, requiring deliberate movement. Prevents accidental double-tap deletion.
- **Navigation cancels** — Esc, back button, closing note all `undoForget()` pending state
- **Brand language everywhere**: Keep (cancel) / Forget (confirm). Never Undo/Confirm/Now.
- **Card grid**: same [Forget] [Keep] pattern inline, bg tints red. Content stays visible, no overlay, no opacity dim.

**8. Universal async action pattern — every mutation uses the same lifecycle.**
`Idle → Trigger → Pending → Success/Error toast → Idle`. Never wire per-callsite toast handling. The pattern:

- **MutationCache** on `QueryClient` (`lib/query-client.ts`) has global `onSuccess`/`onError` handlers that read `mutation.meta`.
- **meta.errorMessage** on EVERY mutation hook — even optimistic ones. Error toast is the safety net.
- **meta.errorAction** on EVERY mutation hook — short noun phrase from `t.errors.action.*` ("move that memory", "delete that topic"). The status-class classifier in `lib/error-copy.ts` interpolates it into "Going a little fast. Try to {action} again in {seconds}s" for 429s, "memax had a hiccup. Try to {action} again in a moment" for 5xx, etc. WITHOUT this field, rate-limit errors show the generic `errorMessage` fallback and users lose the retry-after countdown. Use the function form (`(err) => string | undefined`) of `errorMessage` when a specific business code needs bespoke copy that wins OVER the classifier (e.g., `hub_frozen` for dream triggers). Return undefined to fall through to the classifier.
- **meta.successMessage** only for irreversible or batch actions (disconnect, batch delete). Most mutations are optimistic — success is already visible.
- **meta.skipGlobalToast** for mutations with custom UX (push flow with undo handles its own feedback in bar-context).
- **Bridge**: `lib/mutation-toast.ts` — module-level callback ref. BarProvider registers the handler on mount. MutationCache calls it.
- **Callsite callbacks** are for UI side-effects only (`selection.exit()`, navigation) — never for toast messages.
- **i18n**: all messages through `t.*`. Dynamic messages use `(data, vars) => interpolate(t.key, { n: data.count })` in meta.
- **`useBarToast()`** is ONLY for non-mutation feedback (clipboard copy, DnD drop success). Never inside mutation callbacks.
- **Custom catch blocks**: hooks that use `useBarToast` directly in a `mutateAsync().catch` (e.g., `useMemoryMove`, `useMemoryForget`, `useTopicMove`, `useUpdateApiKey`) must layer `classifyMutationError(err, { action })` into the ladder AFTER business codes and BEFORE the generic fallback. Business codes stay authoritative (non-retryable); rate-limit / offline / 5xx flow through the classifier so users get the same retry-after countdown they'd see from the global cache handler.
- **Kitchen reference**: section 28 — live state machine demo + mutation inventory + pattern rules.

## Settings Dialog Pattern

The settings dialog (`settings-dialog.tsx`) uses a consistent structure across ALL tabs. Any new tab or section must follow this exactly.

**Structure:**

```
Tab content
├── Section (title="SECTION NAME")     ← optional title, uppercase, /45, tracking-wider
│   └── Surface (subtle, rounded-2xl, px-5 py-4)
│       └── Content inside Surface padding
├── Section (title="ANOTHER SECTION")
│   └── Surface (subtle, rounded-2xl, px-5 py-4)
│       └── ...
```

**Section component** (defined in settings-dialog.tsx):

- Title: `text-[12px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1`
- Body: `Surface variant="subtle" rounded="2xl" className="px-5 py-4"`
- Spacing: `mb-8` between sections

**Content inside Surface:**

- Rows: `px-3.5 py-2.5 rounded-lg hover:bg-surface-2` (interactive) or `px-3.5 py-3` (static)
- Row spacing: `space-y-0.5` (not `divide-y` — rows have their own rounded hover state)
- Badges: `text-[11px] px-1.5 py-0.5 rounded bg-surface-2 text-fg-3`
- Destructive zone: `oklch(from var(--destructive) l c h / 0.08)` bg, rounded-lg
- Toggle rows: label (14px) + sublabel (12px text-fg-3) + toggle aligned right

**Tab-level rules:**

- Tab name provides context — don't add a section title that repeats the tab name
- Single-content tabs (Teams list, Agent configs) use one untitled Section
- Multi-section tabs (Account: profile + danger + dev) use titled Sections
- Extracted tab components: `TeamsSection`, `AgentConfigsSection`, `IntelligenceSection`
- Settings-dialog.tsx is the shell only (~500 lines) — tab content lives in separate files

**What NOT to do:**

- Custom `border border-border/60 rounded-xl` containers (use Surface)
- Inline `<div className="mt-6">` wrappers (use Section component)
- Different title styles per tab (always uppercase /45 tracking-wider)
- Edge-to-edge rows with negative margins inside Surface (content stays within Surface padding)

## Memory Row Pattern (unified)

The MemoryRow is the most repeated element in the app. Kitchen section 21 is the visual reference. **ONE component definition, surface-driven presets.** Never create a second memory row implementation.

**Universal structure (every row, every surface):**

```
Line 1: [Indicator] [Context] · Title                    [Meta]
Line 2: Summary (optional, surface-dependent)               pl-5
Line 3: Tags (optional, surface-dependent)                   pl-5
```

**Attribution tiers (indicator slot, in priority order):**

1. **Agent capture** (`source_agent` present) → Per-agent icon + accent color (h-3.5) + agent name. Icon/color from `lib/agents.ts` `AGENT_IDENTITIES` (single source of truth). Examples: Terminal/coral for Claude Code, MousePointer2/teal for Cursor. Fallback for unknown: Bot/soft indigo.
2. **Team human** (`author_name` present, team hub) → Avatar initial (h-4 w-4) + name + "via agent"
3. **Personal default** (fallback) → Content-type icon (PDF/image/link) or colored dot (h-1.5)

**Surface presets (what to show/hide):**

| Feature      | recent | inbox | topic | list   | recall   |
| ------------ | ------ | ----- | ----- | ------ | -------- |
| Summary      | ✓      | ✗     | ✓     | ✓      | ✓        |
| Tags         | ✗      | ✗     | ✗     | ✗      | ✗        |
| Hub badge    | scope† | ✗     | ✗     | scope† | ✓ always |
| Per-row copy | ✓      | ✗     | ✗     | ✗      | ✗        |
| Processing ✦ | ✓      | ✓     | ✗     | ✗      | ✗        |

†**Hub badge is scope-conditional:** shown only in "All" scope (user needs to know which hub a memory belongs to). Hidden in Personal/Team scope (redundant — all memories are from the same hub). Recall results always show badges (cross-hub by design). See kitchen 20o for the scope model.

**Tags:** NOT shown at row level on any surface. Tags live in memory detail page, search matching (backend), and copy-for-AI export.

**Visual rules:**

- Edge-to-edge within card: `px-4 py-2.5`
- No `rounded-lg` on rows — flat
- Dividers: `border-t border-border/30`
- Hover: `hover:bg-surface-1`
- Title: `text-[14px] font-medium text-foreground` (14px for list items, industry standard)
- Age: always present, `text-[13px] text-fg-3 tabular-nums`

## Empty State Centering (industry standard)

Empty states are centered in the **available viewport space** between header and bar. Not viewport center (too low), not fixed padding (breaks on different screens).

**Pattern:** `minHeight` from layout constants + `paddingTop: 30vh` for perceptual center (~35-40% from top, matching brain view).

```tsx
import { CENTERED_HEIGHT, MOBILE_CENTERED_HEIGHT } from "@/lib/layout";

<div
  className="flex flex-col items-center text-center"
  style={{
    minHeight: isMobile ? MOBILE_CENTERED_HEIGHT : CENTERED_HEIGHT,
    paddingTop: "30vh",
  }}
>
  <span className="state-slow-breathe" style={{ color: "var(--signature)" }}>
    ✦
  </span>
  <h2>{title}</h2>
  <p>{subtitle}</p>
</div>;
```

**Layout constants** (`lib/layout.ts`):

- `CENTERED_HEIGHT` = `calc(100dvh - CONTENT_TOP - BAR_HEIGHT)` — desktop
- `MOBILE_CENTERED_HEIGHT` = `calc(100dvh - MOBILE_CONTENT_TOP - MOBILE_BAR_HEIGHT)` — mobile

**Rules:**

- Never use `justify-center` for full-page empty states (puts content at mathematical 50%, too low)
- Never hardcode pixel values for centering (use layout constants)
- `paddingTop: 30vh` gives perceptual center — content sits slightly elevated like brain view
- Same pattern for ALL empty states: 0 memories, 0 topics, error states, loading
- ✦ always breathes (`state-slow-breathe`) in signature color

**What NOT to do:**

- Create a new memory row component (use the unified one)
- Show subcategory labels (category is being deprecated from UI; dots remain as color signal)
- Use `rounded-lg` on individual rows
- Add padding wrappers around row groups (`px-1.5` etc.)

## Hub Model (Slack/Notion pattern)

You're always in a specific hub. The hub switcher (`settings-panel.tsx`) controls both what you see AND where you push. No "All" scope — recall crosses hubs, browse doesn't.

| Context  | Icon    | Read              | Push        | Title                |
| -------- | ------- | ----------------- | ----------- | -------------------- |
| Personal | —       | personal hub only | → personal  | "Your Knowledge"     |
| Team     | `Users` | that team only    | → that team | "{name}'s Knowledge" |

**State model (`auth.tsx`):**

- `activeHubId` = the hub you're in. Controls both read and write.
- `switchHub(hubId)` → sets activeHubId, persisted to localStorage
- `useActiveHub()` → `{ activeHub, isTeamHub, hubFilter }`

**Push target indicator (bar):**

- Personal hub → no indicator (default, obvious)
- Team hub → `→ team-name` pill (bg-surface-2)
- Single-hub users → no hub switcher shown at all

**Recall always crosses all hubs** regardless of active hub. Hub badges on recall results show origin.

**Rules:**

- Default hub is personal (on first login, on logout reset)
- Switching hub persisted to localStorage (`memax_active_hub_id`)
- All queries (`useMemories`, `useTopics`) pass `hubFilter` (always a real hub ID)
- API writes auto-include `X-Hub-ID` header from localStorage (`writeHubHeaders` in api.ts)

## Tree Panel Pattern (Notion-style)

The knowledge tree sidebar (`topic-tree-panel.tsx`) has two desktop states and one mobile state. Kitchen section 22 is the visual reference.

**Desktop — Pinned (in layout flow):**

- Full-height `sticky top-0 h-screen`, `var(--card)` bg, `borderRight`, width 280px
- **Logo stays fixed** in its normal position (does NOT move or hide when pinned)
- Tree header ("知识树") aligned with `CONTENT_TOP` (88px) via `paddingTop: CONTENT_TOP`
- Header: title + `ChevronsLeft` to collapse
- Content push animated via `motion.div` width `0↔280` with spring easing (NOT `layout` prop)
- `isPinned` persisted to localStorage (`memax_tree_pinned`)

**Desktop — Peeking (hover-reveal overlay):**

- Triggered by hovering within 12px of left edge (150ms delay, `DesktopTreeHoverEdge` component)
- Fixed panel at `top: HEADER_TOP`, `borderTopRightRadius: 12px`, shadow
- NOT full height — doesn't start from top
- Backdrop `rgba(0,0,0,0.08)`, click outside dismisses
- Header: title + `ChevronsRight` (`»`) to pin it open
- Mouse-leave dismisses after 300ms delay
- Slide animation: `duration: FAST`, `ease: [0.16, 1, 0.3, 1]`

**Mobile — Bottom sheet (current):**

- Toggle button next to "你的知识" page header (right side, `Code` icon in `knowledge-grid.tsx`)
- NOT in the fixed header — no longer affects logo positioning
- Opens bottom sheet overlay via `TopicTreePanelProvider`

**Mobile — Master/detail push (future target, kitchen 22d):**

- Tree IS the page content. No toggle button, no overlay.
- Tap topic → detail slides in from right
- Not yet implemented in production

**What NOT to do:**

- Collapsed strip with chevron indicators (visual noise, no industry precedent)
- `motion.main layout` for content push (unreliable with conditional siblings — use `motion.div` width)
- Toggle button in fixed header on mobile (disconnects from content context)
- `Pin`/`PinOff` icons (use `ChevronsLeft`/`ChevronsRight` — matches Notion)

## Bar State Design (unified)

The bar uses a single, consistent visual treatment across ALL states. Kitchen section 11 is the reference.

**Rules:**

- **Always 1px border** — never change border-width between states (no 1.5px, 2px)
- **`--bar-border`**: `rgba(0,0,0,0.12)` light, `oklch(0.30)` dark — subtle, Notion-aligned
- **`--bar-shadow`**: 2-layer subtle (`0 1px 3px 0.04, 0 4px 16px 0.03`) — not heavy 3-layer
- **Focus (Cmd+K)**: adds `0 0 0 1px foreground/8` ring + `translateY(-2px)` lift
- **Push feedback**: optimistic clear + "Sending..." → "Sent to memax [Undo]" notification above bar
- **Recall feedback**: send button spinner, results appear progressively in expand slot. No notification banner.
- **No ring pulse animations** on the bar container — removed `animate-recall`, `animate-remember`

**Universal design tokens** — single source of truth chain:

```
--op-primary (0.9)  →  --fg-1  →  @theme --color-fg-1  →  text-fg-1
--op-secondary (0.65) → --fg-2 → @theme --color-fg-2   →  text-fg-2
--op-tertiary (0.4)  →  --fg-3  →  @theme --color-fg-3  →  text-fg-3
--op-muted (0.2)     →  --fg-4  →  @theme --color-fg-4  →  text-fg-4
--op-bg-subtle (0.03) → --surface-1 → @theme --color-surface-1 → bg-surface-1
--op-bg-hover (0.05)  → --surface-2 → @theme --color-surface-2 → bg-surface-2
--op-bg-active (0.08) → --surface-3 → @theme --color-surface-3 → bg-surface-3
```

**Use `text-fg-*` / `bg-surface-*` everywhere.** Never introduce raw `text-foreground/XX` or `bg-foreground/X`.

For specific values (pixel sizes, surface colors, border tokens, typography scale): **read `globals.css` `:root` tokens** — they are the single source of truth. Kitchen visualizes them. This skill defines rules, not values.

## What NOT to Generate

- Gradient backgrounds, floating blobs, glass morphism
- Card hover lift/scale (`hover:scale-105`)
- Oversized marketing-page elements in the app
- Blue/purple tinted shadows (check oklch hue!)
- Separate preview panels (use inline rendering)
- AnimatePresence with blur transitions for state changes
- `useCallback` for simple event handlers (causes stale closures)
- **Modal/dialog overlays for memory detail** — all memory taps navigate to `/memories/[id]` route. No modals for viewing memories.
- **`flushSync` for animations** — causes layout shifts, breaks tab positioning. Use normal React state updates.
- **Complex animation libraries for simple transitions** — View Transitions API / Framer layoutId across component boundaries are fragile. Prefer instant state swaps (design system: "don't animate results").
- **Framer `LayoutGroup`** — coordinates layout animations across siblings, but causes phantom layout shifts when unrelated state changes trigger re-measurement. Removed from memory view.
- **Framer `layout` transition on cards** — `layout: { duration: 0.2 }` makes cards animate position changes on ANY re-render, not just tab switches. Removed.
- **`overflow-hidden` on containers that grow** — clips rounded corners but creates a scroll context. Use `rounded-t-2xl` on header instead.
- **Two containers with same styling that swap** — border/shadow flashes during swap. Use ONE persistent container, swap content inside.
- **Tab pill via `layoutId`** — re-measures on every render, shifts on unrelated state changes. Use CSS `transition-colors` instead.
- **Collapsing bar input row during recall** — user loses their anchor point, no visible way to go back or type a new query. Input must stay visible with X dismiss.
- **Bar `flex-col` (input at top) with `bottom` positioning** — bar grows upward from its fixed bottom edge. Input at top flies off screen as bar expands. Must use `flex-col-reverse` (input at bottom = anchored to fixed point).
- **Brain view bar staying at 40vh during recall** — results grow upward, visual center ends up at top of screen. Bar must slide to `bottom: 32px` when expanded so content fills naturally from bottom.
- **Dismissing recall results on typing** — user types new query but old results disappear. Results must persist until explicit submit (Enter) or dismiss (Esc/X). Typing during recall-result only updates input value.
- **Modal overlays for recall sources** — clicking a recall source navigates to `/memories/[id]`. No modal. Route-based detail for all entry paths.
- **Icon-only buttons on mobile CTA** — dark circle with arrow icon is inconsistent with text pill CTA used everywhere else. Use text label pills for all CTAs. Mobile toggle shows alternative mode label, not "Tab".
- **Single-level dismiss during recall** — X/Esc should clear new typed text first (back to original query), then dismiss on second press. Layered dismiss prevents accidental loss of results.
- **Animating completed results (DeblurReveal on AI answer)** — replays on every re-mount (note detail -> back). Data ready = show immediately. Animation only for loading/waiting states.
- **Showing "Ask memax" CTA inside note detail** — user is reading a doc, not browsing search results. Hide CTA when `selectedNote` is set.
- **Not cancelling async on dismiss** — `triggerAI` await resolves after Esc and overwrites reset state. Use `aiRequestId` ref pattern. Also guard TanStack Query effects with `recallQuery` check.
- **Not restoring state on back navigation** — note search changes `value`, then "back to results" leaves wrong value. Back must restore `value` to `recallQuery`.
- **Custom events for navigation** — use `router.push()`, not `dispatchEvent()`. Only legacy exception: `memax-open-settings`.
- **Same-button-twice for destructive action** — Forget -> Forget? on same button = accidental double-click deletes. Use header bg morph + two distinct targets (Keep / Forget).
- **Separate confirmation row below header** — spawning a new row violates container morphing. The header IS the container — morph its bg color and swap the button area.
- **Forget button at bottom of note body** — hidden below scroll, not discoverable. Put in header, always visible.
- **Generic confirmation language (Undo/Confirm/Now)** — doesn't match brand. Use Keep/Forget everywhere. Grep before shipping to verify zero instances of Undo/Confirm/Now.
- **Fixing one instance of a cross-cutting pattern** — language, layout, behavior patterns appear in 3-5 places (note detail, bar detail, card grid, list view, expanded view). Changing one without grepping for all others = inconsistency. Always `grep` the pattern before committing.
- **Clipboard write with no visual feedback** — `navigator.clipboard.writeText()` without showing "copied" confirmation feels broken. Every copy action needs a state-based confirmation (2s timeout).
- **Multi-step setup** — splitting install/login/setup into separate rows adds cognitive load. Agent setup should be prompt-first (paste one line into your agent's chat), with CLI and manual MCP as collapsed fallbacks. Never show terminal commands in the empty state.
- **Mobile "drop a file"** — no drag-and-drop on mobile. Don't show capabilities that don't exist on the platform.
- **Mobile text referencing desktop keys** — "press Enter", "press Tab", "Cmd+K" in mobile instructional text. Mobile users tap buttons, not keys. Use `isMobile ? t.empty.hintMobile : t.empty.hint` with separate mobile copy.
- **Referencing UI locations by name** — "go to settings", "in the menu". Users don't know where "settings" is. Make the text a clickable button that opens the destination directly (dispatch custom event like `memax-open-settings`).
- **Technical content in empty state** — terminal commands, JSON configs, npm install in the first-time user's view. The empty state is the product's first impression. It should be warm and action-oriented, not a README. Technical setup belongs in settings dropdown.
- **Hardcoded internal URLs in user-facing UI** — env vars or staging URLs shown to users. Production URLs (like `https://api.memax.app/mcp`) should be hardcoded constants, not dynamically constructed from `NEXT_PUBLIC_API_URL`.
- **Centering element at 50vh when content extends below** — the bar at 50vh with 80px of content below = visual center at 54vh = bottom-heavy. Center the cluster, not the element. Bar at 46vh.
