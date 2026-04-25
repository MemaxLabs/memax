# Changelog

All notable changes to `memax-cli` are documented here.

## 0.1.3 - 2026-04-25

- Relicensed from MIT to Apache 2.0. Apache 2.0 adds an explicit
  patent grant and a defensive termination clause; existing
  installs of older versions remain under MIT.
- Added `memax dreams quota [--hub <slug>] [--format text|json]` —
  shows the caller's current dream quota for the billing period.
  Renders tier, used / limit, remaining, and reset date; specialised
  rendering for the disabled-tier and exhausted states. Mirrors
  the per-hub Dream Intelligence indicator on memax.app.

## 0.1.2 - 2026-04-24

- Added public npm README assets and MIT license packaging.
- Linked package metadata to the public `@memaxlabs` presence.

## 0.1.1 - 2026-04-22

- Updated the default API URL to production (`https://api.memax.app`).

## 0.1.0 - 2026-04-21

- First stable CLI package line after the alpha series.
- Includes memory push/recall/list/import commands, agent setup helpers, MCP serving, API-key auth, and agent config/session sync surfaces.

## Earlier alpha releases

- Iterated on MCP schemas, setup flows, session capture, upload handling, topic flags, and config/session sync recovery while the product was still private.
