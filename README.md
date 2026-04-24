# Memax

Public TypeScript packages for Memax, the persistent memory and context layer for AI agents.

This repository contains:

- `packages/sdk` — `memax-sdk`, the TypeScript client for the Memax API.
- `packages/cli` — `memax-cli`, the `memax` command-line interface and local MCP server.

The Memax hosted app, API server, worker, and internal product code live in private repositories.

## Install

```bash
npm install memax-sdk
npm install -g memax-cli
```

## Use The CLI

```bash
memax login
memax push "Important project context"
memax recall "What did we decide?"
memax mcp serve
```

## Use The SDK

```typescript
import { MemaxClient } from "memax-sdk";

const client = new MemaxClient({
  apiKey: process.env.MEMAX_API_KEY,
});

const result = await client.memories.recall({
  query: "What did we decide about auth?",
});

console.log(result.results);
```

## Development

```bash
pnpm install
pnpm build
pnpm lint
pnpm test
```

## Packages

### `memax-sdk`

The SDK is the canonical TypeScript client for Memax `/v1/*` API routes.

```bash
pnpm --filter memax-sdk build
pnpm --filter memax-sdk test
```

### `memax-cli`

The CLI supports authentication, push, recall, ask, hub management, agent config/session sync, and local MCP serving.

```bash
pnpm --filter memax-cli build
pnpm --filter memax-cli test
```

## License

MIT. See [LICENSE](LICENSE).
