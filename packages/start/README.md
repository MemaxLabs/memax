# @memaxlabs/start

Memax's warm start for cloud agents. A cloud agent (Claude Code's cloud
sessions, GitHub Copilot's cloud agent) starts in a fresh sandbox with the
repository's committed `AGENTS.md` and no Memax daemon. Run from the
agent's committed session-start hook, `memax-start` tells the agent what
changed in the space's record since the compile those committed files
hold: new and changed lines, memories a person forgot, and decisions
waiting for a person, in a short `<memax-context>` block.

> **Not published yet.** Publishing to npm waits for a founder's decision
> (cloud warm-start design, decision 2). The package is built in this
> repository and marked `private`, so it can't be published by accident.

```sh
npx -y @memaxlabs/start@1.0.0 --agent copilot
```

- **What it sends**: one request to `POST /v2/spaces/{space}/session-starts`
  with each compiled file's path, the compile its header names (`C-0881`)
  and its drift hash, plus the commit and branch. Never the files' words,
  and never the agent's prompt.
- **The credential**: `MEMAX_START_KEY` from the environment (a start key
  that reads only this), and `MEMAX_API_URL` for another server. Without a
  key it asks only in a Claude Code cloud session (`CLAUDE_CODE_REMOTE`),
  whose proxy may attach a network secret.
- **When it prints nothing**: nothing changed; no file has Memax's header;
  no key; on a person's own machine (a `~/.memax` with the daemon or the
  CLI's credentials), where `memax hook session-start` speaks; and on any
  failure (no answer within 2.5 s, offline, a firewall, any 4xx or 5xx).
  It always exits 0.
- **Output**: plain text for Claude Code and Codex, and
  `{"additionalContext": …}` for Copilot, under 9,000 characters and 3,000
  tokens (Codex 2,400).
- **Proxies**: `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` are honoured, and
  `NODE_EXTRA_CA_CERTS` as Node reads it.
- `--debug` (or `MEMAX_DEBUG=1`) says on stderr why it printed nothing:
  a status and a reason, never a header or the key.

It is one file with no dependencies: the same code as `memax hook
cloud-start` in `memax-cli` (`packages/cli/src/cloud-start-main.ts` and
`src/lib/cloud-start/`), bundled by `scripts/build.mjs`, which fails if
anything but this repository's sources went in or the bundle imports
anything but Node's built-ins.

```sh
pnpm --filter @memaxlabs/start build    # dist/memax-start.js
```

Apache-2.0.
