<!-- Compiled by Memax from memax-v2 at 2026-10-06 10:15 UTC (C-0910). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->
# Memax V2 engineering brief

Every line cites a memory. Ask the memax MCP server for anything else.

## Decisions
- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]

### In `packages/server/**`
- Handlers return the model.ApiResponse envelope, never raw JSON. [M-0450]

## Conventions
- pnpm workspaces only. Never run `npm install` at the root. [M-0071]

### In `**/*.test.ts`, `**/*.test.tsx`
- Test files sit next to the code they test. [M-0451]

### In `packages/web/**`
- Prefer named exports in React components. [M-0441]
- Run tests with `pnpm test -- --run`. [M-0442]

## Live context
Use the memax MCP server for anything not here:
memax_recall, memax_search, memax_get. Propose with memax_push.
