<!-- Compiled by Memax from memax-v2 at 2026-10-06 12:00 UTC (C-0930). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->
# Memax V2 engineering brief

What every agent on this project reads before it writes code.
Every line cites a memory. Ask the memax MCP server for anything else.

## Decisions
- These hold for the v2 API and worker. [M-0100, M-0105]
- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0100]
- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026-07-28. [M-0101]
- API errors are RFC 9457 problem+json, never bare strings. [M-0102]
- New endpoints go under /v2, spec-first in openapi/v2.yaml. [M-0103]
- Display IDs are per-tenant counters; internal keys are uuidv7. [M-0104]
- Every write to the record goes through Ledger.Apply. [M-0105]
- Receipts never hold memory text, so Forget can purge words. [M-0106]
- Row-level security is on for every space-scoped table. [M-0107]
- The compile service is TypeScript, stateless, on the private network. [M-0109]
- Sessions see their own proposals in recall; others do not. [M-0110]
- API keys can read or propose, never keep or forget. [M-0111]

## Conventions
- pnpm workspaces only. Never run `npm install` at the root. [M-0200]
- Every write tool returns a receipt ID the caller can cite. [M-0201]
- Run `pnpm format && pnpm lint` before every commit. [M-0202]
- Every user-facing string goes through i18n, in en and zh. [M-0204]
- Commands need an Idempotency-Key; edits need If-Match. [M-0206]
- Nil means disabled: New() returns nil without its key. [M-0207]
- Read env vars once at startup, then inject dependencies. [M-0208]
- Every colour in the Ledger tree is a token, never a literal. [M-0209]
- Prefer small focused functions over large ones. [M-0210]
- Error messages tell the user what to do next. [M-0211]
- Agents propose; people keep. [M-0250]

## Live context
Use the memax MCP server for anything not here:
memax_recall, memax_search, memax_get. Propose with memax_push.
