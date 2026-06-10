---
name: ui-polish
description: "Use when modifying, polishing, or changing existing frontend flows — interaction tweaks, visual adjustments, flow changes, UX improvements. ALWAYS use this skill when the user asks to 'tweak', 'adjust', 'improve', 'change', or 'polish' any existing UI behavior in the web app. Polish changes are deceptively dangerous — the impact check is critical."
user-invocable: true
---

You are modifying an existing frontend flow in memax. This could be: changing interaction behavior, adjusting visual details, tweaking animation, reordering steps, improving an existing experience. Follow this workflow.

## Step 1 — Understand the current state

Read the relevant code. Before changing anything, describe out loud:

- What the current behavior is
- What the user wants it to become
- What other flows/components are affected by this change

## Step 2 — Impact check

This is the most important step. Polish changes are deceptively dangerous because they touch existing, working code.

- **Cross-cutting consistency:** Grep for every instance of the pattern you're changing. A spacing change on one card must apply to all card variants. An interaction change in brain view may need to match memory view.
- **State machine impact:** If this changes bar behavior, check the Bar State Machine in `docs/design/uiux-strategy.md`. Does the transition map still hold? Does the esc chain still work? Does the visual state table need updating?
- **Mobile + Desktop:** Does this change affect both? Check both paths explicitly.

## Step 3 — Structural check (checklist item 0)

Even for polish:

1. **Framework fit:** Am I changing code that's fighting the framework? If yes, consider fixing the structure instead of polishing a hack.
2. **File health:** Is the code I'm touching in a 300+ line file? Could this polish be easier if the component was properly split?

If the answer to either is yes, tell the user and propose restructuring first.

## Step 4 — Design rules

Read `.agents/skills/design/SKILL.md`. Check `/dev/kitchen` for the current visual treatment of the component you're changing. Verify your change follows:

- Visual identity (pure neutral, borders, no decorative elements)
- Motion rules (animate waits only, instant feel)
- Interaction principles (state persistence, layered dismiss)
- Typography and spacing scale

## Step 5 — Implement

- Make the change across ALL instances (not just the one the user pointed at)
- Run `pnpm format && pnpm lint` — must exit 0 with **no warnings** (`packages/web` is `--max-warnings 0`). See AGENTS.md "Warnings are regressions" for the six common `react-hooks/exhaustive-deps` fix patterns.
- Commit with descriptive message

## Step 6 — Update docs

If the change affects:

- Bar phases or transitions → update Bar State Machine in `uiux-strategy.md`
- Design patterns or anti-patterns → update `design/SKILL.md`
- User-facing text → update i18n keys

## Rules

- Never change one instance of a pattern without checking all others. `grep` before committing.
- Never change interaction behavior without verifying the esc chain still works end-to-end.
- If a polish reveals a deeper structural issue, flag it to the user. Don't paper over it.
