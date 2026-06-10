-- 009: revamp_onboarding_seed_memories
--
-- Refreshes the launch curriculum (was migration 005) to match the
-- four-card onboarding sequence the new product surfaces:
--
--   1. ⌘K everything key      (was: Memory as Mirror)
--   2. Connect your first agent (was: What is memax)
--   3. Memax dreams + topics    (was: How recall works)
--   4. Team hubs                (was: What dreams do)
--
-- The remaining two original templates ("Connect Claude Code in 30s"
-- and "Why team hubs") collapse into the new e002 and e004 and are
-- archived rather than deleted so existing per-user copies still
-- resolve their seed_origin_id back to a live template.
--
-- UPDATE (not DELETE + INSERT) keeps the deterministic seed UUIDs in
-- 005 stable. Existing user copies of the OLD content stay untouched —
-- plan 23 principle 4 (future-only edits). Only signups AFTER this
-- migration get the new copy; admins can opt-in to refresh their own
-- account via /admin/seed-memories → "Sync to my account".
--
-- created_at is bumped to a fixed migration-day timestamp with
-- per-seed second offsets so the worker (which orders by created_at
-- ASC) and admin list both surface the cards in pedagogical order.

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
  kind = 'procedural',
  stability = 'stable',
  tags = ARRAY['welcome', 'tutorial', 'shortcuts', 'search']::text[],
  state = 'active',
  created_at = '2026-05-10 00:00:00+00'::timestamptz,
  updated_at = now(),
  accessed_at = now()
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
  kind = 'procedural',
  stability = 'stable',
  tags = ARRAY['agents', 'setup', 'tutorial', 'mcp']::text[],
  state = 'active',
  created_at = '2026-05-10 00:00:01+00'::timestamptz,
  updated_at = now(),
  accessed_at = now()
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
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['dreams', 'topics', 'tutorial', 'concepts']::text[],
  state = 'active',
  created_at = '2026-05-10 00:00:02+00'::timestamptz,
  updated_at = now(),
  accessed_at = now()
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
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['hubs', 'team', 'tutorial', 'collaboration']::text[],
  state = 'active',
  created_at = '2026-05-10 00:00:03+00'::timestamptz,
  updated_at = now(),
  accessed_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e004'::uuid;

-- e005 ("Connect Claude Code in 30 seconds") and e006 ("Why team hubs")
-- collapse into the new e002 and e004. Archive the rows so future
-- signups skip them (ListSeedMemoryTemplates filters state='active'),
-- but keep them on disk so existing per-user copies still resolve their
-- seed_origin_id back to a live template for analytics + admin review.
UPDATE public.memories
SET
  state = 'archived',
  updated_at = now()
WHERE id IN (
  '00000000-0000-0000-0000-00000000e005'::uuid,
  '00000000-0000-0000-0000-00000000e006'::uuid
);
