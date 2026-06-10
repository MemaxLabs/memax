-- 005: initial_onboarding_seed_memories
--
-- Plan 23 §5.8 — six launch seed templates inserted into the system
-- tutorial hub. The CopySeedMemoriesWorker (plan 23 §5.3) iterates
-- these on every new-user signup and copies each into the user's
-- personal hub.
--
-- Deterministic UUIDs let admin edits / future migrations identify
-- specific seeds without round-tripping the DB. Each seed's UUID
-- encodes its slug at the tail (`...e0...` = entry 1, etc.) so
-- log lines + admin queries stay readable.
--
-- ON CONFLICT (id) DO NOTHING makes this migration replayable: if
-- an environment already has a seed with the same id (e.g. from a
-- partial migration or hand-loaded fixture), we don't clobber it.
-- Admin edits flow through the live row, not the migration.

INSERT INTO public.memories (
  id, hub_id, owner_id,
  title, content, content_type, content_hash,
  summary, hint,
  kind, stability, retrieval_weight,
  tags, boundary, state, source, source_kind,
  created_at, updated_at, accessed_at
)
VALUES
  (
    '00000000-0000-0000-0000-00000000e001'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'Memory as Mirror',
    E'> "Memory is the diary that we all carry about with us." — Oscar Wilde\n\nYour memax hub is your second mind. Drop a thought here and it stays — searchable, askable, yours forever. The agents you work with read from it, and what you teach them carries across sessions.',
    'markdown',
    encode(sha256('seed:memory-as-mirror'::bytea), 'hex'),
    'A reflection on memory as the diary we carry with us, and how memax holds it for you across sessions.',
    'A short reflection that sets the tone — memories are personal and persistent.',
    'semantic', 'stable', 1.0,
    ARRAY['welcome', 'reflection']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  ),
  (
    '00000000-0000-0000-0000-00000000e002'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'What is memax',
    E'memax is a shared memory hub for you and your AI agents. Anything you remember here — a decision, a preference, a piece of context — your AI agents (Claude Code, Cursor, Codex, …) can recall in any future session.\n\nIt''s the layer that keeps your tools from forgetting you every conversation.',
    'markdown',
    encode(sha256('seed:what-is-memax'::bytea), 'hex'),
    'memax is a shared memory hub for you and your AI agents. It keeps context across sessions.',
    'Definition of memax — recall this if asked "what is memax".',
    'semantic', 'stable', 1.0,
    ARRAY['concepts', 'tutorial']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  ),
  (
    '00000000-0000-0000-0000-00000000e003'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'How recall works',
    E'When an AI agent calls `recall`, memax returns memories relevant to the question. Three signals fuse into the result:\n\n1. **Vector similarity** — semantic match against your memory contents.\n2. **Full-text search** — exact and stem matches on words you used.\n3. **Recency + access** — what you''ve looked at recently weighs more.\n\nThe output is a few short, citation-backed snippets — not a wall of text.',
    'markdown',
    encode(sha256('seed:how-recall-works'::bytea), 'hex'),
    'recall fuses vector similarity, full-text search, and recency to surface the most relevant memories.',
    'How retrieval works — useful when explaining memax to teammates.',
    'semantic', 'stable', 1.0,
    ARRAY['concepts', 'recall', 'tutorial']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  ),
  (
    '00000000-0000-0000-0000-00000000e004'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'What dreams do',
    E'Memax runs a periodic background pass called **Dreams**. While you''re away, it groups related memories into topics, suggests merges of near-duplicates, and writes short summaries that surface in recall.\n\nNothing is destroyed without your okay — Dreams proposes; you decide.',
    'markdown',
    encode(sha256('seed:what-dreams-do'::bytea), 'hex'),
    'Dreams is a background pass that organizes memories into topics, suggests merges, and writes summaries.',
    'How the Dreams background pass shapes your hub over time.',
    'semantic', 'stable', 1.0,
    ARRAY['concepts', 'dreams', 'tutorial']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  ),
  (
    '00000000-0000-0000-0000-00000000e005'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'Connect Claude Code in 30 seconds',
    E'```bash\nnpx memax-cli@latest login\nnpx memax-cli@latest setup --all\n```\n\nAfter setup, Claude Code (and any other configured agent) can `recall` and `push` to your memax hub directly. You''ll see them appear in your Settings → Agents tab.',
    'markdown',
    encode(sha256('seed:connect-claude-code'::bytea), 'hex'),
    'Two CLI commands to connect Claude Code to your memax hub.',
    'Quickstart — copy/paste to connect your first agent.',
    'procedural', 'stable', 1.0,
    ARRAY['agents', 'setup', 'tutorial']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  ),
  (
    '00000000-0000-0000-0000-00000000e006'::uuid,
    '00000000-0000-0000-0000-00000000babe'::uuid,
    '00000000-0000-0000-0000-00000000ada7'::uuid,
    'Why team hubs',
    E'Personal hubs are private to you. Team hubs are shared with people you invite — a project, a company, a friend group.\n\nWhen an agent works inside a team context, it can read both your personal memories AND the team hub''s memories. Push to the right hub on purpose so private context stays private and shared context stays useful to everyone.',
    'markdown',
    encode(sha256('seed:why-team-hubs'::bytea), 'hex'),
    'Personal hubs are private; team hubs are shared. Agents read across both based on context.',
    'When to use a team hub vs your personal hub.',
    'semantic', 'stable', 1.0,
    ARRAY['concepts', 'team-hubs', 'tutorial']::text[], 'private', 'active',
    'system', 'onboarding-seed',
    now(), now(), now()
  )
ON CONFLICT (id) DO NOTHING;
