# Decisions in memax-v2

2 decisions and 2 decision gates. Each decision's file in `memories/` has why it was decided, the options weighed and its receipts.

## In force

- [M-0001](memories/M-0001.md) Background jobs run on River, Postgres-backed. We don't use Temporal ("one queue" & <one> DB). · kept

## Superseded

- [M-0002](memories/M-0002.md) Background jobs run on Temporal. · kept

## Decision gates

Questions agents asked a person to decide.

- G-0001 · answered with option 2, kept as [M-0001](memories/M-0001.md): River or Temporal for background jobs?
- G-0002 · waiting until 2026-10-13 09:30 UTC: Fly.io in iad or ams?
