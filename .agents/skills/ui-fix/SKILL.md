---
name: ui-fix
description: "Use when fixing a frontend bug in the memax web app. Diagnoses root cause vs symptom, checks structural health, then fixes. ALWAYS use this skill when the user reports a bug, broken behavior, visual glitch, or 'something doesn't work' in the web app. Never jump straight to code — diagnose first."
user-invocable: true
---

You are fixing a frontend bug in memax. Follow this workflow. The goal is to fix the right thing, not the easy thing.

## Step 1 — Reproduce and locate

Find the bug in the code. Check the browser console for errors. Read the component and its parent. Understand the data flow — where does the data come from (hook, prop, server component), how does it transform, and where does the rendering break?

## Step 2 — Root cause vs symptom

Ask yourself out loud:

- **Is this a surface bug or a structural problem?** A wrong color is surface. A state that gets out of sync because the component manages too many concerns is structural.
- **Why does this bug exist?** Trace back through decisions. Was it a conscious choice gone wrong, or an unconsidered default?
- **Is it isolated or systemic?** Check all places the same pattern is used. If the bug exists in one instance, it likely exists in all of them.

**If structural:** Fix the structure, not the symptom. Tell the user what you're restructuring and why.

## Step 3 — Structural check (checklist item 0)

Even for bug fixes, check:

1. **Framework fit:** Is the buggy code fighting the framework? Would this bug not exist if we'd used Next.js routes / layouts / built-in patterns?
2. **Scale test:** Will this fix hold when more features are added, or is it a patch on a shaky foundation?
3. **File health:** Is this file too large (>300 lines)? Is the bug hard to find because of poor organization?

If the structure is the real problem, fix it. A bug fix that doesn't address the root cause will just resurface.

## Step 4 — Fix

Read `.agents/skills/design/SKILL.md`. Compare with `/dev/kitchen` — the fix should match the kitchen's visual treatment. Verify your fix follows:

- Visual identity rules
- Interaction principles (especially cross-cutting consistency — check ALL instances of the pattern)
- Anti-patterns list

Then fix → `pnpm format && pnpm lint` (must exit 0, **no warnings** — `packages/web` is `--max-warnings 0`; see AGENTS.md "Warnings are regressions" for the six hooks-exhaustive-deps fix patterns) → commit.

## Step 5 — Prevention

After fixing, ask: **What rule would have caught this earlier?** If the answer is a rule that should exist in the design skill or strategy doc but doesn't, add it.

## Rules

- Never fix one instance of a cross-cutting pattern without grepping for all instances.
- Never add a workaround comment like `// HACK` or `// TODO: fix later`. Fix it now or document why it can't be fixed yet.
- If the fix touches the bar state machine, update the Bar State Machine section in `docs/design/uiux-strategy.md`.
