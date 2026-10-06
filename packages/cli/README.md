<br />

<p align="center">
  <a href="https://memax.app">
    <img src="https://memax.app/images/memax-wordmark.svg" alt="Memax" width="200" />
  </a>
</p>

<p align="center">
  <strong>Persistent memory and context for AI agents — from the terminal.</strong>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/memax-cli"><img src="https://img.shields.io/npm/v/memax-cli.svg" alt="npm version" /></a>
  <a href="https://www.npmjs.com/package/memax-cli"><img src="https://img.shields.io/npm/dm/memax-cli.svg" alt="npm downloads" /></a>
  <a href="https://memax.app"><img src="https://img.shields.io/badge/memax-app-7c3aed" alt="memax.app" /></a>
  <a href="https://docs.memax.app"><img src="https://img.shields.io/badge/docs-memax.app-7c3aed" alt="docs.memax.app" /></a>
</p>

---

Memax is the shared memory layer for you and your AI agents. Push knowledge once — notes, files, URLs, chat transcripts — and recall it from any agent, any session, any device. Ask grounded questions and get answers with citations from your own memory base.

`memax-cli` is the command-line entry point. It ships the `memax` binary for terminal workflows and a local MCP server so Claude Code, Cursor, Codex, and any other MCP-aware agent can read and write your memory directly.

## Install

```bash
npm install -g memax-cli
```

Or run once without installing:

```bash
npx memax-cli recall "jwt session rotation policy"
```

## Quick start

```bash
# One-time: log in via browser
memax login

# Remember something
memax push "Never block on a live migration — always do online + backfill."

# Recall with natural language
memax recall "migration guidelines"

# Ask a grounded question — answer includes citations
memax ask "How do we handle breaking schema changes?"

# Wire up your IDE agent (writes an MCP entry to the right config file)
memax setup
```

## Compiled files: the record on your disk

A Memax space compiles to the files your agents already read: `AGENTS.md`, a `CLAUDE.md` that imports it, and path-scoped Cursor rules. Link a repository once, and a small daemon writes each compile into it within seconds of a Keep. It never writes over a hand edit: it reports the edit, and you pull it in, overwrite it or stop compiling the file in the app.

```bash
memax link --space memax-v2   # writes space: memax-v2 to .memax.yml, links this repository here
memax daemon start            # writes the compiled files, and reports hand edits
memax status                  # the space, its files and agents, and what's waiting on you
memax compile                 # compile every target now and wait for the files
```

```text
› memax status

  memax-v2 · Project · 214 kept · 5 waiting on you

  ● AGENTS.md        in sync          14:31
  ● CLAUDE.md        in sync          14:31
  ○ .cursor/rules    drifted          1 local edit
  ● ChatGPT project  in sync          copy it from the app

  CC write · CX propose · CU read · GPT propose · CL propose · GM paused
```

- **`memax link` / `unlink`**: tie a repository to a space (`.memax.yml` at the git root, and `~/.memax/daemon/repos.json` on this machine). A `CLAUDE.md` you already have can stay yours, with one Memax block in it.
- **`memax daemon start | stop | status | run`**: one daemon per user. `run` is the foreground loop for service managers; `install` / `uninstall` start it at login with launchd or systemd, after showing what they write. Logs are in `~/.memax/daemon/daemon.log` and never hold file contents. macOS and Linux.
- **`memax status [--format json]`**: the marks are the app's: ● in sync, ○ waiting on you, ◌ in flight, - stopped.
- **`memax compile [--via pr]`**: without a running daemon it writes the files itself, in a linked repository. Pull requests (`--via pr`) aren't available yet.

The daemon writes a file only when it is absent, already holds the run, or holds something Memax wrote (an earlier compile after a `git pull` is fine). Writes are atomic and the file is read again right before the rename, so a save in between is never lost. It never follows a symlink out of the repository and never touches `.git`. Idle, it polls each space with a 600-byte probe every few seconds and uses well under 1% CPU.

## What it does

- **`memax push`** — save a thought, file, URL, or piped stdin
- **`memax recall`** — natural-language search across personal + team knowledge
- **`memax ask`** — AI-synthesized answer grounded in your memory, with citations
- **`memax list` / `show` / `delete`** — browse and manage entries
- **`memax hub`** — create, invite, and switch between team hubs
- **`memax topic`** — inspect auto-generated topic clusters
- **`memax dreams`** — view the ingestion/organization pipeline status
- **`memax agents sync`** — device-aware sync of agent configs and session artifacts
- **`memax import <dir>`** — one-way ingest of a directory into memory
- **`memax mcp serve`** — start a local MCP server for agent integration
- **`memax setup`** — detect installed agents and wire up MCP + hooks
- **`memax hook`** — Claude Code hook for automatic context injection

Run `memax --help` or `memax <command> --help` for the full surface.

## Agent integration

Memax is built agent-first. Three integration paths:

1. **MCP (recommended for IDE agents)** — `memax setup` writes the right MCP server entry for Claude Code, Cursor, Codex, or Windsurf. The agent can then call `memax_recall`, `memax_push`, `memax_ask`, and friends directly.
2. **Claude Code hooks** — automatic context injection before each prompt (`memax hook`). Latency budget is under 500ms; context is injected as `<memax-context>` blocks.
3. **Direct CLI piping** — works with any agent and in CI. `memax recall … | your-agent`.

## Configuration

The CLI reads from `~/.memax/config.json` after first login. For CI and non-interactive use:

```bash
export MEMAX_API_KEY="mk_live_..."   # from memax.app → Settings → API Keys
export MEMAX_API_URL="https://api.memax.app"   # default
```

## Links

- **Product** — [memax.app](https://memax.app)
- **Docs** — [docs.memax.app](https://docs.memax.app)
- **SDK** — [`memax-sdk`](https://www.npmjs.com/package/memax-sdk)

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
