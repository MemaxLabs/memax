---
name: ui-feature
description: "Use when implementing a new frontend feature for the memax web app. Runs the full workflow — strategy spec lookup, structural check, design rules, i18n, implementation. ALWAYS trigger this skill when the user asks to 'add', 'build', 'create', or 'implement' any UI feature in packages/web/, even if it seems simple. The structural check prevents architectural debt."
user-invocable: true
---

You are implementing a new frontend feature for memax. Follow this workflow exactly. Do not skip steps.

## Step 1 — Understand what to build

Read `docs/design/uiux-strategy.md`. Find the item that matches this feature. Extract:

- Data source (which API endpoint, which fields)
- Desktop vs mobile differences
- Visual spec (typography, spacing, colors)
- Interaction logic (phase transitions, esc chain, back behavior)

If the feature is not in the strategy doc, ask the user for a spec before proceeding.

## Step 2 — Verify backend API

Before building any UI, confirm what the backend actually returns:

1. **Find the handler:** grep `packages/server/` for the endpoint path (e.g. `GET /v1/memories`)
2. **Read the response shape:** check the handler's `writeJSON` call and the model struct it returns
3. **Check the frontend hook:** read the corresponding `use-*.ts` hook in `packages/web/src/hooks/` — does it match the server response?
4. **If no endpoint exists:** flag to the user. Don't build frontend against an imagined API.

Never assume a response format from the strategy doc alone — the doc describes intent, the code is the source of truth.

## Step 3 — Structural check (checklist item 0)

Before touching any code, answer these three questions out loud:

1. **Framework fit:** Am I using Next.js's solution (routes, layouts, parallel routes, server components) or am I inventing my own (custom events, state machine routing, conditional renders)? If inventing → stop and use the framework.
2. **Scale test:** If this feature gets 3 more variants, does the current file/structure still work? If not → restructure first.
3. **File vs system:** Am I adding lines to a file or adding a capability to the system? If the change only makes sense by reading the entire file → it's coupled wrong.

**If any answer is wrong:** Fix the structure first. Tell the user what you're restructuring and why. Then proceed to the feature.

## Step 4 — Design rules

Read `.agents/skills/design/SKILL.md`. Check `/dev/kitchen` for existing patterns — match them, don't invent new ones. Check:

- Container morphing: can an existing surface show this content?
- Visual identity: pure neutral, borders define surfaces, category dots only color
- Interaction principles: state persistence, layered dismiss, async cancellation
- Anti-patterns: review "What NOT to generate" section

## Step 5 — i18n

Read `.agents/skills/i18n/SKILL.md`. Add all new user-facing strings to `en.ts` + `zh.ts` BEFORE writing JSX. Use `t.section.key` in components.

## Step 6 — Implement

- Write the code
- No file > 300 lines. If approaching, split into components immediately. Do not wait until the file is "done" — split as you go.
- Run `pnpm format && pnpm lint` — **lint must exit 0 with no warnings**. `packages/web` is configured with `--max-warnings 0`, so any `react-hooks/exhaustive-deps` or related warning fails the build. See AGENTS.md "Warnings are regressions" for the six common fix patterns (useMemo-wrap `?? []` fallbacks, add stable-setter deps, useCallback + reorder decls for TDZ, capture refs at effect-open, extract complex dep exprs, align dep with the variable actually read).
- Commit with descriptive message

## Step 7 — Update roadmap

If this feature maps to a `uiux-strategy.md` item, check it off in `docs/plans/11-roadmap.md`.

## Rules

- Never let a single file exceed 300 lines. Split into components or route segments.
- Navigation = `router.push()`. No custom events for view switching.
- Every new bar phase must be added to the Bar State Machine section in `uiux-strategy.md`.
- Test both desktop and mobile behavior mentally before committing.
- **Admin pages use `@/lib/admin-client`, never `memax-sdk`.** Admin is an internal operator surface — types and methods live in `packages/web/src/lib/admin-client/`. The public SDK must stay product-only. See AGENTS.md "Admin Surface Boundary (CRITICAL)" for the full rule and the CI check that enforces it.
