-- 010: polish_onboarding_seed_copy
--
-- Second copy pass on the four-card onboarding curriculum that 009
-- introduced. The cards (slots e001..e004) keep their pedagogical
-- order — ⌘K, Connect agent, Dreams + Topics, Team hubs — but the
-- bodies, summaries, and hints are revised:
--
--   1. ⌘K card now mirrors the in-product hint exactly (4 numbered
--      capabilities), notes drop/paste support, and calls out Esc.
--   2. Connect-agent card leads with the *why* (one shared brain across
--      agents), shows the green "Connected" tell, and lists what the
--      user can actually say to a connected agent.
--   3. Dreams + Topics card adds the drag-to-refile flow the user
--      asked about and names the Grid / Dense / List view modes the
--      hub home actually surfaces.
--   4. Team-hubs card spells out the privacy line, the hub picker,
--      author attribution, and a rule-of-thumb for personal-vs-team.
--
-- Keep deterministic UUIDs (and `created_at` order from 009) so the
-- worker still surfaces them in pedagogical order. Bump only updated_at.
-- content_hash uses a `…-v2` slug suffix to stay readable in admin tools
-- and to differ from 009's hash without colliding with the original 005
-- slugs (which the 009 down migration restores).
--
-- Plan 23 principle 4 (future-only edits) holds: existing per-user
-- copies stay untouched. New signups after this migration get the
-- revised copy; admins who want it on their own account run the
-- "Sync to my account" path on /admin/seed-memories.

UPDATE public.memories
SET
  title = '⌘K — capture, recall, ask, jump',
  content = $seed$Memax has one shortcut you'll use everywhere: **⌘K**.

Press it from any page and a single bar opens. From that bar you can:

1. **Type, then ⌘↵** to dump a new memory. No form, no title required — just thought → memory.
2. **Type a question, then ↵** for AI search across everything you've remembered. The Brain answers using your own memories as context.
3. **Type `#topic`** to scope search to a single topic.
4. **Type `/hub`** to jump between your personal hub and any team hub.

You can also **drop, paste, or drag** images, links, files, and screenshots into the bar — they become memories with the source preserved. **Esc** closes the bar without losing your draft.

> One bar. Capture, recall, ask, jump.$seed$,
  content_hash = encode(sha256('seed:cmd-k-everything-key-v2'::bytea), 'hex'),
  summary = 'One shortcut, four powers — dump memories, AI search, scope by #topic, hop hubs with /hub. Drop or paste anything in.',
  hint = 'How the universal ⌘K bar works — recall this when asked how to add a memory, how to search, how to switch hubs, what cmd+K does, or how to paste a screenshot in.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e001'::uuid;

UPDATE public.memories
SET
  title = 'Connect an agent — every AI you use shares one memory',
  content = $seed$Connecting an agent is the moment Memax stops being a notebook and starts being a brain your AI tools share.

**One command:**

```bash
npx memax-cli@latest login && npx memax-cli@latest setup --all
```

It auto-detects every agent installed on your machine — Claude Code, Cursor, Codex, Windsurf, GitHub Copilot, Gemini CLI — mints per-agent API keys, and wires up MCP + hooks. Each will show up under **Agents** with a green **Connected** dot.

**What changes once an agent is connected:**

- Mid-conversation, you can say *"save this to memax"* — it pushes the memory for you, attributed to that agent.
- Your agent can call `recall` on its own to pull relevant memories before answering, so you stop re-pasting context at the start of every session.
- Every connected agent reads from the same hub — what one teaches, the others know.

> Tomorrow, your AI will remember today.$seed$,
  content_hash = encode(sha256('seed:connect-your-first-agent-v2'::bytea), 'hex'),
  summary = 'One CLI command wires Claude Code, Cursor, Codex, and the rest into your hub. They push and recall as you work.',
  hint = 'How to connect AI agents — recall this when asked how to set up Claude Code, MCP setup, connect Cursor, how to make my agent remember, or what setup --all does.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;

UPDATE public.memories
SET
  title = 'Memax dreams — topics emerge, you steer them',
  content = $seed$You dump memories. Memax dreams.

In the background, Memax runs a periodic **dream pass** over your unorganized memories — clustering related thoughts, surfacing patterns, and proposing **topics** so you never file anything by hand.

**Where you'll see it:**

- **Topics** appear on your hub home as folders — *Memax Architecture*, *Personal & Life*, *Side Projects*… auto-named, auto-grouped.
- **Pinned topics** float to the top. Pin the ones you live in.
- **N to review** is the inbox for the dream's proposals — confirm, rename, merge, or split.

**Steering it:**

- **Drag any memory** (card or row) onto a topic to refile it. Drop on the rail, on a topic card, or inside a topic detail view — all work. The next dream pass respects your move.
- Switch between **Grid**, **Dense**, and **List** views in the topic header to find the rhythm that fits your dataset.
- Don't like a topic? Rename it, merge it into another, or archive it from the topic menu.

> You don't organize memories. You let them organize — then you steer.$seed$,
  content_hash = encode(sha256('seed:memax-dreams-topics-v2'::bytea), 'hex'),
  summary = 'Memax clusters your memories into topics in the background. Drag any memory between topics to refile.',
  hint = 'How dreams + topics work — recall this when asked how memax organizes memories, what are topics, how to move a memory to a topic, how to refile, drag to topic, or how auto-organize works.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e003'::uuid;

UPDATE public.memories
SET
  title = 'Team hubs — shared memory for you and your team''s AI',
  content = $seed$A **hub** is a memory space. Your personal hub is private to you. Team hubs are shared with whoever you invite — a project, a company, a friend group.

**Privacy line:** team hubs are visible only to invited members at the role you give them — **Viewer**, **Contributor**, or **Admin**. Personal stays personal.

**Create one:** click the hub picker (top-left of the rail) → **New hub**, name it, give it an emoji. Then hit **Invite** in the top-right and send an email or share-link.

**Use it from your agent:** once an agent is connected, it can push and recall against any hub you have access to. *"Save that to the memax team hub"* mid-chat does it. Each memory shows up with **your avatar and name** so teammates see who pushed what.

**Switch hubs** from the rail — your home, topics, and ⌘K all rebind to the hub you pick.

**Rule of thumb:** if someone else might need it, push to a team hub. If it's just for you, personal.

> One brain. Many minds. Same context.$seed$,
  content_hash = encode(sha256('seed:team-hubs-shared-brain-v2'::bytea), 'hex'),
  summary = 'Invite teammates and every member''s AI agents read and write the same hub. Author identity travels with each memory.',
  hint = 'How team hubs work — recall this when asked how to share memories, invite team, create a hub, switch hubs, team vs personal, or who sees a memory.',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e004'::uuid;
