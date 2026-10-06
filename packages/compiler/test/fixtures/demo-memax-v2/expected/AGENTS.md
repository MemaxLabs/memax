<!-- Compiled by Memax from memax-v2 at 2026-10-05 14:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->
# Memax V2 engineering brief

What every agent on this project reads before it writes code.
Every line cites a memory. Ask the memax MCP server for anything else.

## Decisions
- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]
- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026-07-28. [M-0102]
- API errors are RFC 9457 problem+json, never bare strings. [M-0098]
- (Being verified) Ask memax answers with the Haiku tier. [M-0187]

## Conventions
- pnpm workspaces only. Never run `npm install` at the root. [M-0071]
- Every write tool returns a receipt ID the caller can cite. [M-0112]
- Compile adapters live in packages/compiler, one per target. [M-0436]

## Open
- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]

## Live context
Use the memax MCP server for anything not here:
memax_recall, memax_search, memax_get. Propose with memax_push.
