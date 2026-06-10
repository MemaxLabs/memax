# Design Skill — Implementation Reference

Codebase structure and patterns for `packages/web/`. For design tokens, component specs, and visual rules, read the kitchen (`/dev/kitchen`). This file covers architecture and code patterns only.

## Architecture

- **Next.js 16** App Router, TypeScript strict
- **No sidebar, no top bar.** Floating bottom bar, peek-on-demand tree panel.
- **Bar is the only input.** No separate inputs for different contexts.
- **Navigation = `router.push()`.** No custom events (one legacy exception: `memax-open-settings`).

## Codebase Structure

```
app/(app)/
  layout.tsx              <- BarProvider + auth + bar DOM (portal slots)
  page.tsx                <- brain view (/)
  memories/page.tsx       <- memory view (/memories)
  memories/[id]/page.tsx  <- memory detail (/memories/:id)
  settings/page.tsx       <- redirects, opens settings dialog
  share/page.tsx          <- PWA share target

components/
  bar/
    bar-input-portal.tsx      <- textarea + animated placeholder
    bar-right-portal.tsx      <- send button + photo picker
    bar-expand-portal.tsx     <- routes to ExpandSearchResults + InlineFileChips
    bar-logo-portal.tsx       <- back chevron (memory view)
    bar-notification-card.tsx <- notification above bar
    expand-search-results.tsx <- unified 4-layer progressive search + AI synthesis
    inline-file-chips.tsx     <- file/URL chips above input
  features/
    knowledge-grid.tsx        <- brain view: memory rows + topic cards
    memory-row.tsx            <- unified row component (surface-driven presets)
    topic-card.tsx            <- topic grid card
    ai-summary.tsx            <- markdown AI answer with [N] citations
    recalling-text.tsx        <- crossfade cycling messages during AI loading
    batch-toolbar.tsx         <- multi-select toolbar
  ui/                         <- design system primitives (no business logic)

contexts/
  bar-context.tsx         <- single source of truth for bar state
  selection-context.tsx   <- batch select state
  settings-dialog-context.tsx <- settings modal
```

## Portal Pattern

Bar DOM lives in `layout.tsx` with 4 named slots (`#bar-input-slot`, `#bar-right-slot`, `#bar-expand-slot`, `#bar-logo-slot`). Portal components inject into these slots. Layout renders all portals — pages don't touch bar rendering.

## State Tiers

- **Tier A (BarProvider context):** phase, value, barMode, recallQuery, selectedMemory, stagedFiles — shared across all pages
- **Tier B (view-local):** topic filters, sort, view mode — local to knowledge-grid.tsx
- **Tier C (component-local):** placeholderIdx, mounted flags

## Routing

Every distinct view is a route. Never toggle views via state.

- `/` = brain, `/memories` = library, `/memories/[id]` = detail
- Layout derives bar behavior from `usePathname()`
- Browser back/forward works natively

## Component Tiers

- `components/ui/` — Design system primitives. No business logic, no hooks, no API calls. Pure props → JSX. Will migrate to `packages/ui/`.
- `components/features/` — Feature components. Compose `ui/` primitives with app-specific logic.

**The test:** Remove all imports from `@/hooks`, `@/contexts`, `@/lib/auth`, `next/navigation` — still renders? → `ui/`. Otherwise → `features/`.

## Canonical Data Sources

| Data             | Source                 | Never duplicate                    |
| ---------------- | ---------------------- | ---------------------------------- |
| Category colors  | `lib/category.ts`      | `CategoryDot`, `CategoryBadge`     |
| Agent identities | `lib/agents.ts`        | Memory row attribution, Settings   |
| Motion tokens    | `lib/motion.ts`        | All animated components            |
| Layout constants | `lib/layout.ts`        | `CONTENT_TOP`, `BAR_HEIGHT`, etc.  |
| i18n strings     | `i18n/en.ts` + `zh.ts` | All user-facing text               |
| Design tokens    | `globals.css` CSS vars | All components                     |
| Mutation toast   | `lib/query-client.ts`  | MutationCache meta, never callsite |

## Mutation Feedback Pattern

Every `useMutation` hook uses `meta: { errorMessage, successMessage? }` for toast feedback.
The MutationCache global handlers read meta and fire bar notifications via the bridge
(`lib/mutation-toast.ts` → BarProvider). Callsite callbacks (`mutate(data, { onSuccess })`)
are for UI side-effects only (selection.exit, navigation) — never for toast messages.

## Bar Expand Slot Structure

```
#bar-expand-slot (flex col, max-h-[70vh] desktop / 100dvh-160px mobile)
  +-- motion.div (flex-1, min-h-0, overflow-y-auto) <- sources/AI scroll
  |     +-- content
  |     +-- div (sticky bottom-0, h-8, -mt-8) <- dissolve gradient
  +-- div (shrink-0, bg: var(--card)) <- pinned CTA
```

## Pinned Sections + Dissolve Edges

| Context                          | Technique                                                                                       |
| -------------------------------- | ----------------------------------------------------------------------------------------------- |
| Viewport-level (tabs, bar edges) | `position: fixed` + `backdrop-filter: blur(24px)` + `mask-image: linear-gradient`               |
| Scroll container (expand slot)   | `position: sticky; bottom: 0` + `background: linear-gradient(to top, var(--card), transparent)` |

## For Visual Tokens

**All visual specs (colors, typography, surfaces, spacing, motion, controls, accessibility) live in the kitchen.** Read `/dev/kitchen` Section 0 (Design Principles) for the structured rules block. This file does not duplicate those values.
