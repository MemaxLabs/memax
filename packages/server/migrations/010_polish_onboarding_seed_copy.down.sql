-- Revert 010: polish_onboarding_seed_copy
--
-- Restores the four-card content from migration 009 verbatim. Per the
-- contract every seed migration shares, this stomps any admin edits
-- made to e001..e004 between 010 landing and the revert. e005/e006
-- remain archived (009 archived them; 010 didn't touch them).

UPDATE public.memories
SET
  title = 'Press ⌘K anywhere — your one shortcut for everything',
  content = $seed$Memax has one shortcut you'll use more than any other: **⌘K**.

Press it from anywhere — homepage, topic, agent page — and a single bar opens. From that one bar you can:

1. **Type, then ⌘+enter** to dump a new memory. No file, no form. Just thought → memory.
2. **Type a question** for quick search. Press enter to escalate to semantic AI search across everything you've remembered.
3. **Type `#topic`** to scope the search to a single topic.
4. **Type `/hub`** to jump between your personal hub and any team hub.

> Learn ⌘K first. Everything else flows from it.$seed$,
  content_hash = encode(sha256('seed:cmd-k-everything-key'::bytea), 'hex'),
  summary = 'Press ⌘K anywhere to dump a memory, search, scope by #topic, or jump hubs with /hub.',
  hint = 'How to use the universal ⌘K bar — recall this when asked how to add memories or how to search memax.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e001'::uuid;

UPDATE public.memories
SET
  title = 'Connect your first agent — one command, every AI remembers',
  content = $seed$Memax becomes useful the second your AI agents can read and write to it. Once connected, Claude Code, Cursor, Codex, Windsurf, GitHub Copilot, and Gemini CLI all share the same memory — no more re-explaining your project every session.

**Setup:**

1. Open **Agents** in the left rail
2. Click **Connect agent**
3. Run the one command — it auto-detects every AI tool on your machine, mints per-agent API keys, and wires up MCP + hooks.

```bash
npx memax-cli@latest login && npx memax-cli@latest setup --all
```

**Then try this:**

- Mid-conversation, tell your agent: *"save this decision to memax."* It pushes the memory for you.
- Tomorrow, in a fresh session, ask *"what did we decide about X?"* — it recalls without prompting.

> Tomorrow, your AI will remember today.$seed$,
  content_hash = encode(sha256('seed:connect-your-first-agent'::bytea), 'hex'),
  summary = 'One CLI command connects every AI agent on your machine to memax via MCP + hooks.',
  hint = 'Quickstart for connecting agents — recall this when asked how to connect Claude Code, Cursor, or set up MCP.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;

UPDATE public.memories
SET
  title = 'Memax dreams — your memories become topics on their own',
  content = $seed$You dump memories. Memax dreams.

In the background, Memax periodically runs a **dream pass** over your unorganized memories — clustering related thoughts, surfacing patterns, and proposing **topics** so you never file anything by hand.

What you'll see on your home:

- **Topics** — auto-generated folders that group related memories (e.g. *Memax Architecture*, *Personal & Life*, *Side Projects & Tools*)
- **Pinned topics** — pin the ones you live in; they float to the top
- **N to review** — when a dream surfaces something new, it lands here for you to confirm, rename, or merge

Topics aren't a filing system you maintain. They're a **map** Memax draws of what's in your head. Drag any memory between topics to refit it.

> You don't organize memories. You let them organize.$seed$,
  content_hash = encode(sha256('seed:memax-dreams-topics'::bytea), 'hex'),
  summary = 'Dreams clusters memories into topics, surfaces patterns, and proposes merges for you to review.',
  hint = 'How Dreams and Topics work — recall this when asked what topics are or how memax organizes memories.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e003'::uuid;

UPDATE public.memories
SET
  title = 'Team hubs — give your team (and their AI) a shared brain',
  content = $seed$A **hub** is a memory space. You always have a personal hub. Create or join more for your team, your project, or your company — and every member's AI agents share the same context.

**Create a team hub:**

1. Click your hub picker (top-left of the rail)
2. **New hub** → name it, pick an emoji, invite your team

**Invite members:**

Click **Invite** in the top-right of any hub page. Send an email or a link, and choose Viewer, Contributor, or Admin.

**Use it from your agent:**

```
"push this to the memax team hub — API rate limit
raised to 500 rpm on 2026-05-10"
```

Every member's identity travels with the memories they push. Your teammate recalls yours, you recall theirs — and nobody copy-pastes context between Slack, Notion, and chat threads ever again.

> One brain. Many minds. Same context.$seed$,
  content_hash = encode(sha256('seed:team-hubs-shared-brain'::bytea), 'hex'),
  summary = 'Team hubs are shared memory spaces. Every member''s agent reads and writes the same context with author identity preserved.',
  hint = 'How team hubs work — recall this when asked how to share memories with a team or invite teammates.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e004'::uuid;
