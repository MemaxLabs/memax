# Changelog

All notable changes to `memax-cli` are documented here.

## 0.4.0

Your Personal space on every machine, memories that say when they may no
longer be true, and cloud agents. It needs `memax-sdk` 0.9.

### New

- **Your Personal space on every machine.** `memax link --home` writes
  it into this machine's agents' own files: `~/.claude/rules/memax-personal.md`
  (a file Memax owns), one managed block in `~/.codex/AGENTS.md` (or
  `AGENTS.override.md` when that's what Codex reads),
  `~/.copilot/copilot-instructions.md` and `~/.memax/personal/AGENTS.md`.
  It names every file and asks first. Your own lines stay byte for byte,
  a hand edit comes back as a proposal, and the daemon keeps the files
  current on every machine you link. `memax unlink --home` takes them
  off. It refuses CI, a shell without a terminal (unless `--yes
  --home`), files outside your home, symlinks and folders inside a git
  repository (such as dotfiles).
- `memax connect` adds the agents whose home file waits to be confirmed:
  `memax connect gemini` for `~/.gemini/AGENTS.md` (Antigravity, and
  Gemini CLI once its settings list `AGENTS.md`: `--agents-md`),
  `memax connect windsurf` for one block in Windsurf's global rules
  (within its 6,000-character limit, your rules included), and
  `memax connect opencode`, which adds the canonical copy to OpenCode's
  `instructions` rather than writing a file of OpenCode's. Cursor reads
  no file of the Personal space, so its session-start hook prints your
  Personal space's kept lines at every start, within its budget.
- `memax init` and `memax connect` offer the home link, in a repository
  or not (`--home`, `--no-home`). After the first delivery, two tidy-ups
  ask first: taking out lines that now load twice, and V1's old
  instruction block.
- **Stale memories.** The daemon checks what the space's memories rely
  on (a path exists, a file contains a name, a manifest declares a
  dependency, a file is unchanged) against each linked repository's
  committed tree at `HEAD`, on the default branch as pushed, and only
  results leave the machine: outcomes, the commit, the paths and
  declared versions that changed, never a file's contents. A memory
  whose condition stops holding is marked stale, and every compiled
  file says "(Being verified)" beside it.
- `memax check` runs those checks now and says what each condition
  found. `memax verify <M-id>` says why a memory is stale, shows the
  change from your clone (the commit and its diff, read from git and
  never sent), and takes your answer: still true, new words (a rename's
  new path suggested), or Forget on the web.
- **Cloud agents.** `memax connect --cloud claude-code|copilot|codex`
  connects a cloud agent to the repository's space at Read, prints its
  start key once (never to a file) and commits the session-start hook,
  `memax hook cloud-start`, which tells the agent what changed since the
  compile its files hold, in one request.
- **The session-start hook** also says what changed in your Personal
  space's own file, which memories are being verified and why, and what
  this machine's check found on the branch you're on, each once. In a
  linked repository it asks the daemon to check it soon, so a memory's
  page can say it was checked when that agent started.
- `memax status`, `memax compile` and `memax daemon status` include home
  files.
- `memax export --okf` writes a space as an Open Knowledge Format (OKF
  v0.2) bundle, for any tool that reads OKF.

### Changed

- Every scoped rules format (Cursor's `.mdc`, Claude Code's rules,
  Copilot's instructions, Windsurf's rules) and the `GEMINI.md` shim are
  opt-in for new spaces, since nearly every agent reads `AGENTS.md` and
  would load each scoped fact twice.
- A space switched back to V1 is left to V1's sync: the daemon writes
  nothing for it, reports no hand edit and checks nothing until it is on
  V2 again, and `memax status`, `memax compile` and `memax daemon status`
  say so.
- Signing out (`memax logout`), a session the API refuses, or signing
  in as a different person takes the home files off the machine; the
  same person signing in again puts them back. A Personal space that no
  longer exists takes them off too.
- `memax init` ties this repository's `CLAUDE.local.md` and Claude Code
  auto memory to the repository (`scope.repos`), so they never load in
  other projects; with no remote and no space repository it leaves
  those two files on the machine.
- Agents' JSON settings keep their comments and formatting when Memax
  adds to them.
- Removing Memax's managed block now restores your file byte for byte;
  the block adds no blank line when your file already ends with one.
- The MCP SDK is 1.32.1.

## 0.3.0

The first release for the V2 record: a space compiles into the files
your agents already read (`AGENTS.md`, a `CLAUDE.md` that imports it,
scoped Cursor rules), and the CLI sets a repository up, keeps those
files current and checks what Memax holds. It needs `memax-sdk` 0.8.
Upgrading from 0.2.1, see "Changed" and "Removed".

### New

- `memax init` (`npx memax-cli init`) sets a repository up: it finds
  your agents and the files they read, signs you in, connects the
  agents (their MCP settings, once you agree, and their session-start
  hooks; connections start at Propose, Cursor and Gemini CLI at Read),
  splits the files into statements on your machine (secrets and hidden
  characters never leave it), imports them as proposals, shows where
  they disagree, keeps what you agree with and compiles the space back
  into your files, never over one you wrote. `--dry-run` uploads
  nothing; `--yes --format json` is for CI; `--space`, `--no-connect`,
  `--no-personal`, `--no-daemon`, `--wait` and `--timing` too.
- `memax connect <agent>` connects one agent here: its MCP settings,
  its session-start hook (Claude Code, Codex, Gemini CLI, Cursor and
  Copilot CLI), its connection to this repository's space (never
  raised from the CLI) and a first compile. Agents: `claude-code`,
  `codex`, `cursor`, `gemini`, `copilot`, `opencode`, `windsurf`,
  `chatgpt`. `--gemini-md` adds a `GEMINI.md` for Gemini CLI. Running
  it again changes nothing.
- `memax link` and `memax unlink` tie a repository to a space
  (`.memax.yml`, and `~/.memax/daemon/repos.json` on this machine).
- `memax daemon start | stop | status | run | install | uninstall`: a
  background process that writes each compile into the linked
  repositories within seconds and never writes over a hand edit (it
  reports it for Review instead). `install` starts it at login with
  launchd or systemd. macOS and Linux only for now.
- `memax status` shows the space, its compiled files and agents, and
  what is waiting on you. `memax compile` compiles every target now and
  waits for the files (`--via pr` isn't available yet).
- `memax hook session-start --agent <id>`: run by the agents at session
  start, it prints a `<memax-context>` block with only what changed
  since that agent's last session in this repository, from local files
  only, in under 100 ms. `memax connect` and `memax init` install it,
  and so does the Claude Code plugin.
- `memax export` writes each V2 space's whole record as Markdown with
  its receipts and signed checkpoints (`--space`, `--out`, `--force`);
  `memax verify-export <folder or zip>` checks one (`--key`,
  `--offline`).
- `memax switch` moves a space you own from V1 to the V2 record:
  `--dry-run` shows what moves, `--as project` switches a V1 team hub
  as a project space, and `--back` switches back. Nothing in V1 changes.
- `memax forget <M-id>` forgets a memory on the V2 record everywhere:
  Memax, every compiled file and every agent. It says what goes first,
  and you type the ID (or pass `--confirm <id>`).
- `memax gate [G-id]` lists the decisions agents are waiting on you for,
  and answers one (`--option`). Where a space's decisions need a person
  on the web, it links there instead.
- `memax sessions` lists where you are signed in.
- `memax ask` in a space on the V2 record (from `--space`, `.memax.yml`
  or the link) streams an answer cited from what the space has kept.
  Without one it asks V1 as before.
- `memax mcp serve` serves the same tools as the remote MCP server,
  adding `memax_search`, output schemas and structured results. In
  spaces on V2 it proposes through `/v2`, asks decision gates
  (`memax_request_decision` takes `space_id`), sends Forget to a person
  instead of forgetting, and passes each agent's notices on once.

### Changed

- When Memax limits how fast an account sends requests, the CLI waits a
  short limit out; `memax init` and `memax compile` say to wait a minute
  and run the command again when it doesn't pass, and both carry on
  from where they stopped.
- `memax login` and `memax init` sign in with a code you confirm on the
  web (memax.app/device, opened when a browser can), so the CLI is the
  account the browser is signed in to, however you sign in there
  (GitHub, Google, an email code or a passkey). `--provider
  github|google` opens that provider's own page instead, which was the
  default before; a server without the device grant falls back to it.
- `memax logout` also signs the session out on the server, and the CLI
  keeps the new refresh token each refresh returns (they now rotate).
- V1 commands (`push`, `recall`, `list`, `show`, `delete`, `topic`,
  `dreams`, `hub`, `import`, `capture-session`, `setup`) work as before
  against spaces on V1. `memax push` into a space on V2 is kept there
  as a note, which Dream proposes from for Review, and says so.
- `memax agents sync` reports the agent files of spaces on V2: two-way
  sync is off for them, since Memax compiles the space into those files
  instead (`memax status` in the repository shows them). Sync comes back
  for a space switched back to V1.
- `memax forget` is its own command. A V1 memory ID still deletes it as
  `memax delete` does (`-y` skips the question).
- `memax auth create-key` needs `--agent <slug>` (an agent's key, whose
  writes are credited to the agent) or `--personal` (your own key).
  `memax auth list-keys` marks personal keys `[personal]`.
  `memax setup --print --api-key` needs `--agent <slug>`.
- `memax push` and `memax capture-session` no longer send an empty or
  placeholder (`unknown`) agent name.
- `memax hook install|uninstall` stays deprecated: `memax connect
  claude-code` installs the session-start hook.

### Removed

- `memax push --ttl`, which was never sent to the server.
- `memax import --watch` (it was "coming soon"); `memax init` and the
  daemon replace it.
- The `forget` alias of `memax delete`: `memax forget` is the command
  above.

## 0.2.1 - 2026-08-12

- Added `memax topic archive <id>`, `memax topic restore <id>` and
  `memax topic archived`.
- `memax agents sync` classifies config files by role (identity,
  memory, rules, settings) and never syncs settings files, which may
  hold secrets. It covers OpenClaw's root and workspace files, memory
  notes and skills, and Hermes's files, memory and profiles (scope
  `profile:<name>`); it skips files over 512 KB and refuses cloud paths
  with `..`.
- `memax setup` detects Hermes and writes its MCP entry into
  `~/.hermes/config.yaml`.
- Codex: `memax setup --api-key` writes the key under `http_headers`
  (Codex ignored `headers`), and `memax setup` checks that Codex is
  signed in to Memax and runs or prints `codex mcp login memax`.
- `memax mcp serve` adds `memax_request_decision`.

## 0.2.0 - 2026-05-22

- Removed agent session sync: the `memax agents sessions` commands
  (sync, list, deleted, restore, delete, cleanup, doctor) and the
  session part of `memax agents sync`, `list` and `doctor`. `memax
  agents sync` syncs configs only. Needs `memax-sdk` 0.6.

## 0.1.3 - 2026-04-25

- Relicensed from MIT to Apache 2.0. Apache 2.0 adds an explicit
  patent grant and a defensive termination clause; existing
  installs of older versions remain under MIT.
- Added `memax dreams quota [--hub <slug>] [--format text|json]` —
  shows the caller's current dream quota for the billing period.
  Renders tier, used / limit, remaining, and reset date; specialised
  rendering for the disabled-tier and exhausted states. Mirrors
  the per-hub Dream Intelligence indicator on memax.app.

## 0.1.2 - 2026-04-24

- Added public npm README assets and MIT license packaging.
- Linked package metadata to the public `@memaxlabs` presence.

## 0.1.1 - 2026-04-22

- Updated the default API URL to production (`https://api.memax.app`).

## 0.1.0 - 2026-04-21

- First stable CLI package line after the alpha series.
- Includes memory push/recall/list/import commands, agent setup helpers, MCP serving, API-key auth, and agent config/session sync surfaces.

## Earlier alpha releases

- Iterated on MCP schemas, setup flows, session capture, upload handling, topic flags, and config/session sync recovery while the product was still private.
