---
name: memax-memory
description: Persistent memory layer using Memax. Gives Claude the ability to recall past context and store new learnings across sessions via the Memax MCP tools (memax_recall, memax_push, memax_get, memax_list, memax_forget, memax_capture, memax_topics, memax_hubs, memax_hub_members). Use this skill at the START of every conversation to do a light context check, and throughout the session whenever the user references past decisions, project context, architecture, or conventions — or whenever Claude discovers something worth remembering for future sessions. Also trigger when the user explicitly asks to remember or forget something, or when working on a project where prior context would prevent redundant questions. If Memax MCP tools are available, this skill applies.
---

# Memax Memory — Persistent Context for Claude

You have access to **Memax**, a persistent cloud knowledge hub shared across all your AI agents. It survives between sessions — anything you push now is available in every future conversation.

Your job is to use it proactively. Don't wait for the user to say "check Memax" or "remember this." Treat it like your own long-term memory.

## Tools

| Tool                | Purpose                                                                                                                                           |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `memax_recall`      | Semantic search — find memories relevant to a natural language query across all accessible hubs                                                   |
| `memax_push`        | Save a new memory — supports `hub_id` to target a specific hub, `hub_reason` (required for team hubs), and automatic classification for retrieval |
| `memax_get`         | Read the full content of a specific memory by ID                                                                                                  |
| `memax_list`        | Browse/list memories with pagination and sorting                                                                                                  |
| `memax_forget`      | Delete a memory by ID                                                                                                                             |
| `memax_capture`     | Extract and save key decisions, learnings from a session summary                                                                                  |
| `memax_topics`      | Browse the topic tree, or list memories within a specific topic                                                                                   |
| `memax_hubs`        | List hubs the user can access (IDs, slugs, roles, memory counts)                                                                                  |
| `memax_hub_members` | List members of a specific hub                                                                                                                    |

## When to recall (read)

### Every session — light check

At the very start of a conversation, before diving into the user's request, do a quick `memax_recall` with a query derived from the user's first message. This takes a second and often saves minutes of redundant back-and-forth.

**Example:** User says "Let's work on the auth flow." → `memax_recall("auth flow")` before responding.

If the first message is generic (e.g. "hey" or "what's up"), skip the recall — there's nothing meaningful to search for yet. Resume checking once the conversation has a topic.

### On cues — deeper recall

Go deeper when you notice:

- References to past decisions: "the approach we agreed on," "like last time," "what did we decide about..."
- Project names, codenames, or domain-specific terms you don't have full context for
- Architecture or convention questions: "how do we handle X," "what's our pattern for Y"
- The user correcting you on something that suggests prior context exists

For deeper recall, chain multiple calls: `memax_recall` to find relevant memories, then `memax_get` on specific IDs to read full details, `memax_list` to browse recent memories, or `memax_topics` to explore the user's knowledge tree and find memories organized by topic.

### Don't over-recall

One or two recall calls at session start is plenty for most conversations. Don't recall on every single message — that's noisy and slow. Use judgment: if you already have the context you need, just work.

## When to push (write)

Push memories **proactively during the conversation** whenever something important surfaces. Don't hoard insights until the end — if you discover something worth remembering, push it now. Sessions can end abruptly (browser closed, timeout, context limit), and anything not pushed is lost.

### What to push

Things that would save time or prevent mistakes in a future session:

- **Architecture decisions** — "We chose X over Y because Z"
- **API conventions** — naming patterns, error handling approaches, auth schemes
- **Debugging solutions** — especially non-obvious ones that took effort to find
- **Deployment processes** — steps, gotchas, environment-specific details
- **Team/project preferences** — coding style, tool choices, workflow preferences
- **Infrastructure details** — server setup, service configurations, access patterns
- **Key decisions with rationale** — not just what, but why

### What NOT to push

- Ephemeral task details ("fix the typo on line 42")
- File contents — they belong in git, not memory
- Obvious things that any Claude session would already know
- Verbatim code blocks — summarize the approach instead
- Anything the user explicitly says is temporary or experimental

### How to write good memories

Write memories as if explaining to a future Claude session that has zero context about the current conversation. Be specific and self-contained.

**Good:**

> Memax uses JWT auth with RS256 signing. Tokens are issued by the API gateway and verified by each microservice independently. We chose RS256 over HS256 so services don't need a shared secret. Decision made 2025-01.

**Bad:**

> We're using JWT now.

Include the _why_ behind decisions — rationale is the most valuable thing to remember because it prevents future sessions from relitigating settled questions.

### Push cadence

- Push when you reach a durable decision, solve a non-obvious problem, or learn something worth keeping across sessions
- If a conversation is heavy on decisions (e.g., architecture planning), you might push 3-5 memories
- For a routine task, zero pushes is fine — not every session produces lasting knowledge
- Routine progress and mid-task notes don't need to be pushed — save the signal, skip the noise. Memax handles deduplication on the server side, so don't worry about overlap with existing memories.

## Handling outdated information

If you recall a memory that looks outdated or contradicts what you're learning in the current session, just push the new information as a fresh memory. Don't bother deleting the old one — Memax's cloud layer handles conflict resolution and merging automatically.

The exception: if the user explicitly asks you to delete something ("forget that we use Redis" or "remove the old deployment notes"), use `memax_forget` to honor the request.

## Classification

Memax classifies memories automatically using invisible retrieval axes. Do not pass taxonomy labels when pushing. Instead, write clear, self-contained content and use `hint` when extra context would help the server understand the memory.

Good hints are short and plain-language:

- "Architecture decision from today's auth review"
- "Runbook for staging deploy recovery"
- "Personal preference about code review style"

Use topics, tags, pins, and explicit forget/delete actions for user-facing organization and corrections. Retrieval searches across every hub the token can access, with active hub context used as a ranking boost rather than a hard boundary.

## Topics

Memax automatically organizes memories into a topic tree based on content. Use `memax_topics` to:

- **Browse the full tree** (no arguments) — returns all topics with memory counts, useful for understanding what the user's knowledge base covers
- **Drill into a topic** (pass `topic_id`) — returns the memories within that topic

Topics are great for structured exploration: "what do I know about deployment?" → browse topics → drill into the relevant one. This complements `memax_recall` (semantic search) with a more navigable, hierarchical view.

## Hubs

Users can have multiple hubs — a personal hub and shared team hubs. Use `memax_hubs` to list what's available. When pushing memories, you can optionally target a specific hub with `hub_id` and provide `hub_reason` to explain why the memory belongs there.

- `memax_recall` searches across **all accessible hubs** by default — no need to specify a hub for reading
- `memax_push` without `hub_id` saves to the user's personal hub
- `memax_hub_members` shows who has access to a given hub — useful when the user asks about team knowledge or shared context

## Behavior across environments

This skill works in both **Claude Code** and **Claude.ai**. The tools and behaviors are identical. The only difference is conversational style:

- In **Claude Code**, you're likely deep in a coding task — recall silently and weave context into your responses without narrating that you checked memory.
- In **Claude.ai**, conversations are more exploratory — it's fine to briefly mention what you found ("I see from past context that you're using pgvector for embeddings — want me to build on that?").

In both cases, push memories inline without ceremony. No need to announce "I'm saving this to Memax" unless the user would benefit from knowing (e.g., they asked you to remember something and you're confirming).

## Project context

When the Memax MCP server is connected, memories you push are automatically tagged with the current project context (git repo, project name, branch). This means:

- Memories pushed from Project A rank higher when recalled from Project A
- Cross-project knowledge still surfaces, just lower in the ranking
- You don't need to manually tag project context — it's handled by the transport layer

This is especially useful when the user works across multiple repos with different conventions (e.g., different package managers, different auth patterns). Your memories from each repo are contextually prioritized.

## At session end

If the conversation is ending (user says goodbye, context is running low, or the task is wrapping up) and you learned something significant during the session, push it before the session closes. Key candidates:

- Decisions made during this session
- Bugs found and their root causes
- Architectural changes or new patterns introduced
- User preferences you discovered (response style, tool choices, workflow)

Don't push a "session summary" — push the individual insights that would be useful standalone.

## Quick reference

```
Session start    →  memax_recall(topic from first message)
Cue detected     →  memax_recall(specific query) → memax_get(id) if needed
Browse structure →  memax_topics() → memax_topics(topic_id) to drill in
Browse recent     →  memax_list(limit, cursor) with pagination
Check hubs       →  memax_hubs() → memax_hub_members(hub_id)
Important info   →  memax_push(clear, self-contained summary with rationale)
Team knowledge   →  memax_push(content, hub_id, hub_reason) to target a shared hub
User says forget →  memax_forget(id)
Outdated memory  →  Just push the new version, Memax handles merging
```
