<!-- Compiled by Memax from memax-v2 at 2026-10-06 09:00 UTC (C-0900). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->
# Memax V2 engineering brief

Every line cites a memory. Ask the memax MCP server for anything else.

## Decisions
- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]
- (In conflict) Deploy the v2 API to Railway. [M-0174]
- Freeze V1 routes until cutover. [M-0301]

## Open
- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]
- Should Dream run nightly on Free? Ask Ziyang before changing the cadence. [M-0302]
- Is the 25% team-pull gate measured per space or per account? [M-0303]

## Live context
Use the memax MCP server for anything not here:
memax_recall, memax_search, memax_get. Propose with memax_push.
