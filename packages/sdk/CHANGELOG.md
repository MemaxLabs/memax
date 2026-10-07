# Changelog

All notable changes to `memax-sdk` are documented here.

## Unreleased

- **Breaking:** OAuth consent moved to the web session (OAuthConsent).
  `auth.getOAuthConsentRequest` and the `OAuthConsentRequest`,
  `OAuthConsentHub` and `OAuthConsentPermission` types are removed: the
  API no longer serves `GET /oauth/authorize/consent-request`. Use
  `auth.openOAuthRequest(id)`, `auth.decideOAuthRequest(id, { decision,
  space_id })` and `auth.releaseOAuthRequest(id)` with the `OAuthRequest`,
  `OAuthRequestSpace`, `OAuthDecisionInput` and `OAuthDecision` types, as
  the person signed in on the web. Bump the minor version (0.8.0) when
  this is released.
- Added `memax.v2`, the first resources on the `/v2` API (the V2
  record): `spaces.list`, `memories.remember` / `list` / `get` /
  `keep` / `edit` / `reject`, `review.list` and `receipts.list`.
  Commands take a required `idempotencyKey`; `edit` takes the
  `ifMatch` version it started from. `refusalOf(err)` returns the
  policy decision behind a 403 `refused`.
- Added the `V2` type namespace (`V2.Memory`, `V2.CommandResult`,
  `V2.PolicyCode`, …), generated from
  `packages/server/openapi/v2.yaml` into `src/v2/schema.gen.ts`
  (`pnpm gen:v2`).
- `extraHeaders` moved from `StreamOptions` to `RequestOptions`, so
  every request can carry per-call headers (`StreamOptions` still
  inherits it). V1 resources are unchanged.

## 0.5.0 - 2026-04-28

- Added `chats` resource for plan 24's Agent Chat surface:
  - Session lifecycle: `createSession`, `listSessions`,
    `getSession`, `patchSession`, `deleteSession`.
  - Messages: `sendMessage`, `listMessages`, `getMessage`.
  - Stream: `streamMessage` opens the SSE replay+tail buffer with
    `Last-Event-ID` resume, mirroring native EventSource semantics.
  - Lifecycle controls: `cancelMessage` (idempotent soft-cancel
    returning the row's actual status + a strict
    `cancel_registered` boolean), `regenerateMessage` (creates a
    new assistant via supersession through `parent_message_id`).
  - Tooling: `listTools` for the catalog, `decideApproval` for
    mutating-tool approval prompts.
- Added `extraHeaders` to the transport's `StreamOptions` so the
  chat resume header (`Last-Event-ID`) can be threaded per-call
  without mutating the transport singleton.
- Re-exports: `ChatSession`, `ChatMessage`, `ChatToolDescriptor`,
  `CreateChatSessionInput`, `PatchChatSessionInput`,
  `ListChatSessionsOptions`/`Result`, `ListChatMessagesOptions`/
  `Result`, `SendChatMessageInput`/`Result`, `ChatTurnOutcome`,
  `CancelChatMessageResult`, `RegenerateChatMessageResult`,
  `DecideApprovalResult`, `ChatStreamOptions`,
  `ChatStreamEventName`, `ChatScopeType`, `ChatSessionStatus`,
  `ChatMessageRole`, `ChatMessageStatus`.

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
