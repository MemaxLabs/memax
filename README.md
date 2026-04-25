<br />

<p align="center">
  <a href="https://memax.app">
    <img src="https://memax.app/images/memax-wordmark.svg" alt="Memax" width="220" />
  </a>
</p>

<p align="center">
  <strong>Persistent memory and context for AI agents.</strong>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/memax-sdk"><img src="https://img.shields.io/npm/v/memax-sdk.svg?label=memax-sdk" alt="memax-sdk npm version" /></a>
  <a href="https://www.npmjs.com/package/memax-cli"><img src="https://img.shields.io/npm/v/memax-cli.svg?label=memax-cli" alt="memax-cli npm version" /></a>
  <a href="https://github.com/MemaxLabs/memax/actions/workflows/ci.yml"><img src="https://github.com/MemaxLabs/memax/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-Apache_2.0-blue.svg" alt="Apache 2.0 License" /></a>
</p>

<p align="center">
  <a href="https://memax.app">Product</a>
  ·
  <a href="https://docs.memax.app">Docs</a>
  ·
  <a href="https://www.npmjs.com/package/memax-sdk">SDK</a>
  ·
  <a href="https://www.npmjs.com/package/memax-cli">CLI</a>
</p>

---

Memax is a shared memory layer for developers and AI agents. Push context once, then recall it from Claude Code, Codex, Cursor, CI jobs, local scripts, or your own application code.

This repository contains the public TypeScript SDK and CLI packages for Memax. The hosted app, API server, worker, and internal product code are maintained separately.

## Packages

| Package                          | npm                                                    | Purpose                                        |
| -------------------------------- | ------------------------------------------------------ | ---------------------------------------------- |
| [`packages/sdk`](./packages/sdk) | [`memax-sdk`](https://www.npmjs.com/package/memax-sdk) | TypeScript client for the Memax API.           |
| [`packages/cli`](./packages/cli) | [`memax-cli`](https://www.npmjs.com/package/memax-cli) | `memax` terminal command and local MCP server. |

## Install

```bash
npm install memax-sdk
npm install -g memax-cli
```

## CLI Quick Start

```bash
memax login
memax push "Our staging database uses PgBouncer in transaction mode."
memax recall "database pooling"
memax ask "How is staging database pooling configured?"
memax setup
```

`memax setup` detects supported local agents and can configure MCP so agents can call Memax tools directly.

## SDK Quick Start

```ts
import { Memax } from "memax-sdk";

const memax = new Memax({
  apiKey: process.env.MEMAX_API_KEY,
});

await memax.push({
  content: "Release branches are cut from main after CI is green.",
  tags: ["release", "process"],
});

const { memories } = await memax.recall({
  query: "release branch process",
  limit: 10,
});

console.log(memories);
```

## What You Can Build

- Agent context recall through MCP, CLI piping, or direct SDK calls.
- Team knowledge workflows with hubs and invites.
- Memory-backed applications that need search, retrieval, and grounded answers.
- CI/CD helpers that push deployment facts and recall operational context.
- Local agent setup flows for Claude Code, Cursor, Codex, Windsurf, and other MCP-aware clients.

## Development

This is a small pnpm + Turborepo workspace.

```bash
pnpm install
pnpm format:check
pnpm lint
pnpm build
pnpm test
```

Package-specific commands:

```bash
pnpm --filter memax-sdk build
pnpm --filter memax-sdk test

pnpm --filter memax-cli build
pnpm --filter memax-cli test
```

## Release Model

Alpha packages are published automatically from `main` after CI passes:

```text
memax-sdk@<version>-alpha.<run>
memax-cli@<version>-alpha.<run>
```

Stable npm releases are published manually through the `Release npm packages` GitHub Actions workflow. Stable releases also create package-specific GitHub releases and tags:

```text
memax-sdk-v0.4.2
memax-cli-v0.1.2
```

## License

Apache 2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
