---
name: memax
description: How to work with Memax, the reviewed context a person keeps for their agents. Use at the start of work in a repository that has Memax (an AGENTS.md or CLAUDE.md "Compiled by Memax", a <memax-context> block, or the memax MCP tools), when the person mentions earlier decisions, conventions or "what we agreed", and before you finish work that settled something worth keeping.
---

# Working with Memax

Memax keeps what a person and their team have decided, across every agent they use. People keep memories; agents read them and propose new ones. A person reviews each proposal before it counts.

## What you already have

- **The compiled files.** `AGENTS.md` (and the `CLAUDE.md` that imports it) is compiled from the kept memories and loaded at session start. Each line cites its memory, like `[M-0219]`. Don't re-read it from a tool: it's already in your context.
- **The `<memax-context>` block.** When the Memax CLI is installed, a session-start hook prints a short block with only what changed since your last session in this repository: new or changed lines, memories a person forgot, decisions waiting for a person, and a compile that hasn't reached this machine yet. When nothing changed, there is no block.

## Before you start a task

1. Call `memax_recall` with a specific query about the task, such as `"webhook retry policy"` or `"auth token refresh"`, not `"project"`.
2. If the first recall is thin, try once more with other words, or `memax_search` for exact terms.
3. Prefer kept memories over your own assumptions. When you rely on one, cite its ID, like `[M-0219]`.
4. Something in the files or a recall that contradicts what the person tells you now: say so, and ask which holds.

## When you learn or settle something durable

- Call `memax_push` with one fact or decision per call: what was decided, why, and what was rejected. Name the files or paths it applies to.
- **It's a proposal, not a memory.** A person keeps it, in the agent if your client asks them, or later in Review. Say "proposed; it waits in Review" rather than "saved" or "remembered".
- Don't push again to override a kept decision. If you think a decision is wrong, propose the change and say why; the person settles it.
- Never push credentials, tokens, personal data, or whole transcripts. `memax_capture` is for an explicit session summary when the person asks for one.

## Decisions that need a person

- When a choice belongs to the person (a dependency, an architecture direction, anything a kept decision doesn't cover), call `memax_request_decision` with the question and 2 to 4 options, then continue with work that doesn't depend on it.
- A decision listed as waiting in `<memax-context>` isn't yours to make. Its answer arrives as a kept decision that later recalls return.

## Forgotten memories

- A memory listed as forgotten in `<memax-context>` is gone on purpose. Don't use, repeat or restore what it said, and drop any copy you keep in your own notes.
- `memax_forget` doesn't forget anything by itself: it asks a person, who forgets or keeps the memory on the web. Use it when the person asks you to forget something, or when a memory is plainly wrong.

## The compiled files

- Don't hand-edit the Memax lines in `AGENTS.md` or `CLAUDE.md` to change what everyone reads. An edit there comes back as proposals for Review and the file isn't rewritten until a person settles them. Propose the change with `memax_push` instead.
