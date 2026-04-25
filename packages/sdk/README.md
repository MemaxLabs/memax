<br />

<p align="center">
  <a href="https://memax.app">
    <img src="https://memax.app/images/memax-wordmark.svg" alt="Memax" width="200" />
  </a>
</p>

<p align="center">
  <strong>TypeScript SDK for Memax — shared memory and context for AI agents.</strong>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/memax-sdk"><img src="https://img.shields.io/npm/v/memax-sdk.svg" alt="npm version" /></a>
  <a href="https://www.npmjs.com/package/memax-sdk"><img src="https://img.shields.io/npm/dm/memax-sdk.svg" alt="npm downloads" /></a>
  <a href="https://memax.app"><img src="https://img.shields.io/badge/memax-app-7c3aed" alt="memax.app" /></a>
  <a href="https://docs.memax.app"><img src="https://img.shields.io/badge/docs-memax.app-7c3aed" alt="docs.memax.app" /></a>
</p>

---

`memax-sdk` is the official TypeScript client for the [Memax](https://memax.app) API. It runs in Node.js, Deno, and edge runtimes — uses standard `fetch`, no heavy dependencies.

Use it to push knowledge, recall with natural language, ask grounded questions with citations, manage hubs and invites, subscribe to live events, and drive the same memory surface your team sees in the web app.

## Install

```bash
npm install memax-sdk
# or
pnpm add memax-sdk
# or
yarn add memax-sdk
```

## Quick start

```ts
import { Memax } from "memax-sdk";

const memax = new Memax({
  apiKey: process.env.MEMAX_API_KEY,
});

// Push a memory
await memax.push({
  content: "Our staging DB is pooled through PgBouncer in transaction mode.",
  tags: ["ops", "db"],
});

// Recall with natural language
const { memories } = await memax.recall({
  query: "pooling strategy",
  limit: 10,
});

// Ask — grounded answer with citations
const { answer, citations } = await memax.ask({
  query: "What mode does PgBouncer run in for staging?",
});

console.log(answer);
for (const c of citations) {
  console.log(`  ↳ ${c.memoryId} — ${c.snippet}`);
}
```

## Features

- **`push`** — save content, files, URLs, structured data; idempotent on content hash
- **`recall`** — vector + lexical hybrid search with optional reranking
- **`ask`** — AI-synthesized answer from your memory, with citations
- **`memories.*`** — list, get, update, delete; filter by hub, tags, date, source
- **`hubs.*`** — create, list, transfer, update members, manage roles
- **`invites.*`** — create hub invites, list outstanding, revoke
- **`topics.*`** — inspect auto-generated topic clusters over your memory base
- **`events.subscribe()`** — live server-sent events: new memories, hub changes, dream completion
- **`capture.*`** — start and append to a capture session for streaming ingest
- **Typed errors** — `MemaxError` with `code`, `status`, `retryable` for clean client handling
- **Runtime-portable** — works wherever `fetch` works

## Authentication

Get an API key at [memax.app → Settings → API Keys](https://memax.app/settings/api-keys). For browser / user-session auth (OAuth flow), pass a bearer token instead:

```ts
const memax = new Memax({ bearerToken: session.accessToken });
```

To target a non-default Memax API endpoint:

```ts
const memax = new Memax({
  apiKey: "...",
  baseUrl: "https://api.memax.app",
});
```

## Error handling

All failures throw `MemaxError`. The instance carries enough to drive retry and user-facing messaging:

```ts
import { Memax, MemaxError } from "memax-sdk";

try {
  await memax.recall({ query: "..." });
} catch (err) {
  if (err instanceof MemaxError) {
    if (err.retryable) {
      // transient — back off and retry
    } else if (err.status === 401) {
      // auth issue — prompt re-login
    }
    console.error(err.code, err.message);
  }
  throw err;
}
```

## Live events

```ts
const unsubscribe = memax.events.subscribe(
  { types: ["memory.created", "hub.updated"] },
  (event) => {
    console.log(event.type, event.data);
  },
);

// later
unsubscribe();
```

## Links

- **Product** — [memax.app](https://memax.app)
- **Docs** — [docs.memax.app](https://docs.memax.app)
- **CLI** — [`memax-cli`](https://www.npmjs.com/package/memax-cli)

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
