-- 022: update_seed_connect_command_global_install
--
-- The "connect your first agent" seed card (slot e002) still hands the
-- user an npx one-liner:
--
--   npx memax-cli@latest login && npx memax-cli@latest setup --all
--
-- That command leaves nothing installed, which breaks the flow the card
-- is teaching:
--
--   * `setup --hooks` bakes the resolved binary into each agent's hook
--     config. With no global install, resolution falls through to
--     `npx -y memax-cli`, so every prompt re-resolves the package before
--     the hook can answer — against the <500ms hook budget.
--   * `memax` never lands on PATH, so the follow-up commands the rest of
--     the curriculum assumes (`memax agents sync`, `memax recall`) fail
--     with `command not found`.
--
-- The card's `hint` is explicitly "how to connect AI agents", so recall
-- and Ask surface this copy whenever someone asks how to set up Claude
-- Code or Cursor — persisted data, not just UI, has to carry the fix.
-- Matches the web copy in packages/web/src/lib/cli.ts (CLI_SETUP_CMD).
--
-- Only slot e002 changes; e001/e003/e004 keep their 011 copy. Slot UUID
-- and `created_at` order stay intact so the worker still surfaces the
-- curriculum in sequence — bump only `updated_at`. The hash slug takes a
-- `-v4` suffix so it differs from the 011 hashes without colliding.
--
-- Plan 23 principle 4 (future-only edits) holds: existing per-user copies
-- stay untouched. New signups after this migration get the revised copy;
-- admins refresh their own account via /admin/seed-memories →
-- "Sync to my account".

UPDATE public.memories
SET
  content = $seed$The magic of Memax shows up the moment your AI tools can read and write to it.

Once they're connected, the AI you already use — Claude Code, Cursor, and the rest — can save context AS you work, and recall it without you re-pasting anything. Tomorrow's session opens, the AI already knows the project.

```bash
npm i -g memax-cli && memax login && memax setup --all
```

That's it. The script finds the AI tools you already have and wires them in. Each one shows up under **Agents** with a green dot once it's working.

> Tomorrow, your AI will remember today.$seed$,
  content_hash = encode(sha256('seed:connect-your-first-agent-v4'::bytea), 'hex'),
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;
