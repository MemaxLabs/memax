-- Revert 009: revamp_onboarding_seed_memories
--
-- Restores the original 005 launch curriculum verbatim — title, body,
-- summary, hint, content_hash, kind, stability, tags, state. Down
-- migrations are best-effort: an admin who edited a seed via
-- /admin/seed-memories after 008 landed will see those edits stomped
-- here. That matches the contract for every seed migration — the
-- migration file is the source of truth for fresh environments, and
-- live edits are subject to overwrite by any subsequent migration.

UPDATE public.memories
SET
  title = 'Memory as Mirror',
  content = E'> "Memory is the diary that we all carry about with us." — Oscar Wilde\n\nYour memax hub is your second mind. Drop a thought here and it stays — searchable, askable, yours forever. The agents you work with read from it, and what you teach them carries across sessions.',
  content_hash = encode(sha256('seed:memory-as-mirror'::bytea), 'hex'),
  summary = 'A reflection on memory as the diary we carry with us, and how memax holds it for you across sessions.',
  hint = 'A short reflection that sets the tone — memories are personal and persistent.',
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['welcome', 'reflection']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e001'::uuid;

UPDATE public.memories
SET
  title = 'What is memax',
  content = E'memax is a shared memory hub for you and your AI agents. Anything you remember here — a decision, a preference, a piece of context — your AI agents (Claude Code, Cursor, Codex, …) can recall in any future session.\n\nIt''s the layer that keeps your tools from forgetting you every conversation.',
  content_hash = encode(sha256('seed:what-is-memax'::bytea), 'hex'),
  summary = 'memax is a shared memory hub for you and your AI agents. It keeps context across sessions.',
  hint = 'Definition of memax — recall this if asked "what is memax".',
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['concepts', 'tutorial']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;

UPDATE public.memories
SET
  title = 'How recall works',
  content = E'When an AI agent calls `recall`, memax returns memories relevant to the question. Three signals fuse into the result:\n\n1. **Vector similarity** — semantic match against your memory contents.\n2. **Full-text search** — exact and stem matches on words you used.\n3. **Recency + access** — what you''ve looked at recently weighs more.\n\nThe output is a few short, citation-backed snippets — not a wall of text.',
  content_hash = encode(sha256('seed:how-recall-works'::bytea), 'hex'),
  summary = 'recall fuses vector similarity, full-text search, and recency to surface the most relevant memories.',
  hint = 'How retrieval works — useful when explaining memax to teammates.',
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['concepts', 'recall', 'tutorial']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e003'::uuid;

UPDATE public.memories
SET
  title = 'What dreams do',
  content = E'Memax runs a periodic background pass called **Dreams**. While you''re away, it groups related memories into topics, suggests merges of near-duplicates, and writes short summaries that surface in recall.\n\nNothing is destroyed without your okay — Dreams proposes; you decide.',
  content_hash = encode(sha256('seed:what-dreams-do'::bytea), 'hex'),
  summary = 'Dreams is a background pass that organizes memories into topics, suggests merges, and writes summaries.',
  hint = 'How the Dreams background pass shapes your hub over time.',
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['concepts', 'dreams', 'tutorial']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e004'::uuid;

UPDATE public.memories
SET
  title = 'Connect Claude Code in 30 seconds',
  content = E'```bash\nnpx memax-cli@latest login\nnpx memax-cli@latest setup --all\n```\n\nAfter setup, Claude Code (and any other configured agent) can `recall` and `push` to your memax hub directly. You''ll see them appear in your Settings → Agents tab.',
  content_hash = encode(sha256('seed:connect-claude-code'::bytea), 'hex'),
  summary = 'Two CLI commands to connect Claude Code to your memax hub.',
  hint = 'Quickstart — copy/paste to connect your first agent.',
  kind = 'procedural',
  stability = 'stable',
  tags = ARRAY['agents', 'setup', 'tutorial']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e005'::uuid;

UPDATE public.memories
SET
  title = 'Why team hubs',
  content = E'Personal hubs are private to you. Team hubs are shared with people you invite — a project, a company, a friend group.\n\nWhen an agent works inside a team context, it can read both your personal memories AND the team hub''s memories. Push to the right hub on purpose so private context stays private and shared context stays useful to everyone.',
  content_hash = encode(sha256('seed:why-team-hubs'::bytea), 'hex'),
  summary = 'Personal hubs are private; team hubs are shared. Agents read across both based on context.',
  hint = 'When to use a team hub vs your personal hub.',
  kind = 'semantic',
  stability = 'stable',
  tags = ARRAY['concepts', 'team-hubs', 'tutorial']::text[],
  state = 'active',
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e006'::uuid;
