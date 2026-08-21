-- Revert 022: update_seed_connect_command_global_install
--
-- Restores slot e002's content from migration 011 verbatim, including the
-- npx command. Per the contract every seed migration shares, this stomps
-- any admin edits made to e002 between 022 landing and the revert.

UPDATE public.memories
SET
  content = $seed$The magic of Memax shows up the moment your AI tools can read and write to it.

Once they're connected, the AI you already use — Claude Code, Cursor, and the rest — can save context AS you work, and recall it without you re-pasting anything. Tomorrow's session opens, the AI already knows the project.

```bash
npx memax-cli@latest login && npx memax-cli@latest setup --all
```

That's it. The script finds the AI tools you already have and wires them in. Each one shows up under **Agents** with a green dot once it's working.

> Tomorrow, your AI will remember today.$seed$,
  content_hash = encode(sha256('seed:connect-your-first-agent-v3'::bytea), 'hex'),
  updated_at = now()
WHERE id = '00000000-0000-0000-0000-00000000e002'::uuid;
