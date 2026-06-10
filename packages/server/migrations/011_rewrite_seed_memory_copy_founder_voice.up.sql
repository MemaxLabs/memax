-- 011: rewrite_seed_memory_copy_founder_voice
--
-- Third copy pass on the four-card onboarding curriculum (slots
-- e001..e004). The 010 pass leaned tech-heavy (MCP, "auto-detects
-- every AI tool", agent-name lists) and conflicted with the brand
-- voice "Your AI knows you. In every tool." (memax 01-vision-and-
-- strategy.md).
--
-- This pass:
--   * strips jargon (no MCP, no "auto-detects", no full agent roster)
--   * keeps the procedural ⌘K + setup steps users actually need
--   * aligns to the founder voice from the welcome message memo
--     (id 9d458e7c) — warm, two-person team, "memax does an awesome
--     job memorizing and organizing", "ships every week"
--   * leans on the strategy positioning: passive ingestion, dreams
--     run while you sleep, your AI knows you across tools
--   * mirrors the privacy-by-intent rule from 07-team-hubs.md
--     (writes are explicit, not LLM-routed)
--
-- Slot UUIDs and `created_at` order from 009 stay intact so the
-- worker still surfaces them in pedagogical sequence. Bump only
-- `updated_at`. Hash slugs get a `-v3` suffix to differ from the
-- 010 hashes without colliding with 005's originals.
--
-- Plan 23 principle 4 (future-only edits) holds: existing per-user
-- copies stay untouched. New signups after this migration get the
-- revised copy; admins refresh their own account via
-- /admin/seed-memories → "Sync to my account".

UPDATE public.memories
SET
  title = 'Press ⌘K to remember. Or to ask.',
  content = $seed$**⌘K** is your one shortcut.

Hit it from anywhere — a topic page, the agents tab, anywhere — and a single bar opens. From that bar:

- **Type something, press ⌘↵** to drop a memory. No title, no folder, no form. Just thought → stored.
- **Type a question, press ↵** to ask. Memax answers using only what you've remembered — no web, no hallucination.

Drop in screenshots, links, files, voice notes — they all become memories with the source kept.

> One bar. Capture, ask, done.$seed$,
  content_hash = encode(sha256('seed:cmd-k-everything-key-v3'::bytea), 'hex'),
  summary = 'Hit ⌘K anywhere. ⌘+Enter saves a memory. Enter asks a question grounded in what you''ve saved. Drop or paste anything in.',
  hint = 'How the ⌘K bar works — recall this when asked how to add a memory, how to search, what cmd+K does, or how to drop a screenshot in.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e001'::uuid;

UPDATE public.memories
SET
  title = 'One command, every AI tool you use shares one brain',
  content = $seed$The magic of Memax shows up the moment your AI tools can read and write to it.

Once they're connected, the AI you already use — Claude Code, Cursor, and the rest — can save context AS you work, and recall it without you re-pasting anything. Tomorrow's session opens, the AI already knows the project.

```bash
npx memax-cli@latest login && npx memax-cli@latest setup --all
```

That's it. The script finds the AI tools you already have and wires them in. Each one shows up under **Agents** with a green dot once it's working.

> Tomorrow, your AI will remember today.$seed$,
  content_hash = encode(sha256('seed:connect-your-first-agent-v3'::bytea), 'hex'),
  summary = 'One CLI command connects every AI tool you already use to your memax brain. They save and recall context for you, across sessions.',
  hint = 'How to connect AI agents — recall this when asked how to set up Claude Code, Cursor, or how to make an AI remember across sessions.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;

UPDATE public.memories
SET
  title = 'Memax dreams while you work',
  content = $seed$You dump memories. Memax dreams.

In the background, Memax reads what you've saved, finds patterns, merges duplicates, and groups related thoughts into **topics** — folders that organize themselves on your home page.

You can steer the result:

- **Drag any memory** (card or row) onto a topic in the left rail's topic tree to refile it. The next dream pass respects your move.
- Don't like a topic? Rename it, merge it into another, or archive it from the topic menu.
- The **N to review** badge on your home is the inbox for dream proposals — confirm, rename, or split with a tap.

Topics aren't a filing cabinet you maintain. They're the map Memax draws of what's in your head.

> You don't organize. You let memax dream.$seed$,
  content_hash = encode(sha256('seed:memax-dreams-topics-v3'::bytea), 'hex'),
  summary = 'Memax runs a background pass that organizes your memories into topics. Drag any memory between topics to steer the result.',
  hint = 'How dreams + topics work — recall when asked what topics are, how memax organizes memories, how to refile, or how to drag-to-topic.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e003'::uuid;

UPDATE public.memories
SET
  title = 'When your team''s AI should share one brain',
  content = $seed$A **hub** is a memory space.

You always have a personal hub (private to you). Create or join more for your team, your project, your group — and every member's AI tools share that hub's memory.

**Privacy by intent:** team hubs are visible only to people you invite, at the role you give them — Viewer, Contributor, or Admin. Personal stays personal. And pushing to a team hub is always an explicit choice — Memax never routes a write there guessing your intent.

Three things to try:

1. **Click the hub picker** (top-left of the rail) → **New hub**, name it, hit **Invite** in the top-right.
2. **Tell your AI** *"save this to the <name> hub"* — the agent pushes there with your name and avatar attached.
3. **Switch hubs from the rail** any time. Home, topics, ⌘K all rebind to the hub you pick.

> One brain. Many minds. Same context.$seed$,
  content_hash = encode(sha256('seed:team-hubs-shared-brain-v3'::bytea), 'hex'),
  summary = 'Team hubs are shared memory spaces. Members'' AI tools read and write the same context, with your identity attached. Invites and pushes are always explicit.',
  hint = 'How team hubs work — recall when asked how to share memories, invite teammates, switch hubs, or what''s the difference between personal and team.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e004'::uuid;
