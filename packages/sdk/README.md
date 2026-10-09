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
await memax.push(
  "Our staging DB is pooled through PgBouncer in transaction mode.",
  { tags: ["ops", "db"] },
);

// Recall with natural language
const { memories } = await memax.recall("pooling strategy", { limit: 10 });

// Ask — grounded answer with citations
const { answer, citations } = await memax.ask(
  "What mode does PgBouncer run in for staging?",
);

console.log(answer);
for (const c of citations) {
  console.log(`  ↳ [${c.index}] ${c.memory_id} — ${c.title}`);
}
```

## Features

- **`push`** — save content, files, URLs, structured data; idempotent on content hash
- **`recall`** — vector + lexical hybrid search with optional reranking
- **`ask`** — AI-synthesized answer from your memory, with citations
- **`memories.*`** — list, get, update, delete; filter by hub, kind, topic, date
- **`hubs.*`** — create, list, transfer, update members, manage roles and invites
- **`invites.*`** — look up and accept a hub invite
- **`topics.*`** — inspect auto-generated topic clusters over your memory base
- **`events.subscribe()`** — live server-sent events: new memories, hub changes, dream completion
- **`v2.*`** — the `/v2` API: the V2 record, Review, receipts, compiled files, Ask, export (below)
- **Typed errors** — `MemaxError` with `code`, `status` and getters such as `isRateLimited` for clean client handling
- **Runtime-portable** — works wherever `fetch` works

## V2 API

`memax.v2` talks to the `/v2` API, the V2 record: every change is a
command with a receipt, agents propose and people keep. Its types are
generated from the API contract and live under the `V2` namespace.

```ts
import { Memax, refusalOf, type V2 } from "memax-sdk";

const memax = new Memax({ apiKey: process.env.MEMAX_API_KEY });

// Commands need an Idempotency-Key: choose one per intent, reuse it on retry.
const res: V2.CommandResult = await memax.v2.memories.remember(
  "memax-v2", // space id or slug
  { statement: "River is our queue, not Kafka.", section: "decisions" },
  { idempotencyKey: crypto.randomUUID() },
);
res.outcome; // "applied" for a member or owner, "proposed" for an API key

// Display IDs repeat across tenants, so they travel with their space.
const { memory, versions, receipts } = await memax.v2.memories.get("M-0219", {
  space: "memax-v2",
});

// Edits send the version they started from; a newer one is 412 edit_clash.
await memax.v2.memories.edit(
  memory.id,
  { statement: "River is our only queue." },
  { idempotencyKey: crypto.randomUUID(), ifMatch: memory.version },
);

try {
  await memax.v2.memories.keep(
    "M-0220",
    {},
    { space: "memax-v2", idempotencyKey: crypto.randomUUID() },
  );
} catch (err) {
  refusalOf(err)?.code; // "person_must_review": API keys never keep
}

const queue = await memax.v2.review.list("memax-v2"); // queue.total waiting
const activity = await memax.v2.receipts.list("memax-v2", { limit: 50 });
```

`memax.v2.ask` streams a cited answer as server-sent events, and
`verifyExport` and `verifyReceiptChain` check an export and a space's
receipt chain against its signed checkpoints, with no dependencies.

The types are regenerated from `packages/server/openapi/v2.yaml` with
`pnpm --filter memax-sdk gen:v2`; `pnpm lint` fails when they are stale.

## Authentication

Create an API key in Settings on [memax.app](https://memax.app). For browser / user-session auth (OAuth flow), pass the session's token through `auth` instead:

```ts
const memax = new Memax({
  auth: async () => ({ Authorization: `Bearer ${session.accessToken}` }),
});
```

To target a non-default Memax API endpoint:

```ts
const memax = new Memax({
  apiKey: "...",
  apiUrl: "https://api.memax.app",
});
```

## Error handling

All failures throw `MemaxError`. The instance carries enough to drive retry and user-facing messaging:

```ts
import { Memax, MemaxError } from "memax-sdk";

try {
  await memax.recall("...");
} catch (err) {
  if (err instanceof MemaxError) {
    if (err.isRateLimited || err.isServerError || err.isNetwork) {
      // transient — back off (err.retryAfterSeconds when the server says) and retry
    } else if (err.isUnauthorized) {
      // auth issue — prompt re-login
    }
    console.error(err.code, err.message);
  }
  throw err;
}
```

## Live events

```ts
const stream = memax.events.subscribe({
  onEvent: (event, data) => {
    console.log(event, data);
  },
});

// later
stream.abort();
```

## Links

- **Product** — [memax.app](https://memax.app)
- **Docs** — [docs.memax.app](https://docs.memax.app)
- **CLI** — [`memax-cli`](https://www.npmjs.com/package/memax-cli)

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
