# Changelog

All notable changes to `memax-sdk` are documented here.

## 0.4.2 - 2026-04-25

- Relicensed from MIT to Apache 2.0. Apache 2.0 adds an explicit
  patent grant and a defensive termination clause; existing
  installs of older versions remain under MIT.
- Added `dreams.usage({ hubId? })` — read-only quota snapshot
  wrapping `GET /v1/usage/dreams`. Returns the new `DreamUsage`
  shape (scope, tier, mode, limit/used/remaining, allowed,
  period bounds, quota source).
- Added `DreamUsage` and `DreamUsageOptions` to the public type
  exports.
- Published the notification kind helper exports used by the
  internal web app.

## 0.4.1 - 2026-04-24

- Added public npm README assets and MIT license packaging.
- Linked package metadata to the public `@memaxlabs` presence.

## 0.4.0 - 2026-04-21

- Split alpha and stable npm publish channels.

## 0.1.x - 2026-04

- Added typed API resources for memories, recall, bar search, hubs, uploads, topics, notifications, dreams, agent configs, and agent sessions.
- Added typed error helpers, `Retry-After` propagation, and request cancellation via `AbortSignal`.
- Kept admin-only API clients in the web app rather than the published SDK.

## Earlier alpha releases

- Initial TypeScript client for the Memax `/v1/*` API.
