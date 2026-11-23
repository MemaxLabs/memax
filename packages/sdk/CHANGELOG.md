# Changelog

All notable changes to `memax-sdk` are documented here.

## 0.9.0

The personal layer's machines, stale memories (Verify, conditions and
check runs), cloud agents' session starts and search across spaces.
Nothing is removed; two types are stricter (see Changed).

### New

- **Machines and home files.** `memax.v2.machines.list`, `link`,
  `rename` and `unlink`: a person links a machine to their Personal
  space, which then compiles into that machine's agents' own files
  (Claude Code, Codex, Gemini CLI and Antigravity, Copilot, Windsurf, and
  the canonical copy OpenCode and Cursor read). `Target.root` (`repo` or
  `home`) and `Target.machine` describe a home target, and
  `memax.v2.targets.list` takes `machine` to list one machine's files.
  `TargetList.on_v2` says whether the V2 record serves the space (a
  space switched back to V1 lists its targets, but its files are V1's).
  Types: `Machine`, `MachineList`, `MachineResult`, `LinkMachineInput`,
  `MachineFileInput`, `RenameMachineInput`, `TargetRoot`,
  `TargetMachine`.
- **Verify.** `memax.v2.memories.stale(ref)` says why a memory is stale
  and `memax.v2.memories.verify(ref, …)` answers it: still true, update
  or forget. Types: `StaleReport`, `StaleCause`, `VerifyInput`,
  `VerifyAnswer`, `VerifyResult`.
- **Conditions and checks.** A memory's `conditions` are typed
  (`Condition`: `path_exists`, `file_contains`, `dep_present`,
  `file_unchanged`, `pr_state`, each with an `id` and an `origin`).
  `memax.v2.checks.conditions(space)` lists what a space's machines
  check, with the memories marked stale and why (`stale`), and
  `memax.v2.checks.report(space, …)` reports a machine's results, never
  a file's contents. Types: `SpaceConditions`, `CheckedCondition`,
  `StaleMemory`, `CheckRunInput`, `CheckResultInput`, `CheckEvidence`,
  `CheckRun`, `CheckRunResult`, `ConditionOrigin`.
- **Where a memory applies.** `memax.v2.memories.scope(ref, …)` changes
  it, such as widening a fact kept for one repository to everywhere.
  Types: `ScopeInput`, `MemoryScope`, `RepoKey`.
- **Cloud agents.** `memax.v2.agents.connectCloud(…)` connects a cloud
  agent (Claude Code, Copilot or Codex in a cloud sandbox) to one space
  at Read, with a start key shown once; `memax.v2.agents.rekey(agent)`
  gives it a new key in place. `memax.v2.sessionStarts.start(space, …)`
  is what a cloud agent's session start asks: what changed since the
  compile each committed file holds. Types: `CloudAgentResult`,
  `StartKey`, `CreateCloudAgentInput`, `SessionStart`,
  `SessionStartInput`, `SessionStartFile`, `SessionStartFileInput`,
  `SessionStartLine`, `SessionStartForget`, `SessionStartGate`,
  `AgentStart`.
- **Open Knowledge Format.** `toOkf(parseExport(files), slug)` turns an
  export into an OKF v0.2 bundle: one concept per memory in OKF's fields
  (the rest under the `memax` key), tombstones without words, the Brief
  as `index.md` and the receipts as `log.md`.
- **Search across spaces.** `memax.v2.search(q, …)` searches every space
  of yours on V2, or the ones you name. The spaces list says how many
  memories and agents each has (`SpaceCounts`). Types: `SearchPage`,
  `SearchHit`.
- **Check history and reverted changes.** `MemoryDetail.checked` names
  the latest check, its branch, commit, observer and triggering agent
  (`MemoryChecked`). `CheckRunResult.unflagged` lists memories whose
  failed conditions hold again; their receipts use the new `unflagged`
  action. A rename can supply Verify's update words without a client
  having to write them.
- **Dream's stale reports.** `DreamSurfaced.kind` also admits `stale`
  and `stale_waiting`, with the flag's `receipt_id`. Dates are flagged by
  Memax's hourly sweep; Dream reports them and reminds once after
  30 days, rather than flagging dates itself.
- **Plan refusals.** The policy catalogue includes `space_limit`,
  `agent_kind_limit`, `seat_needed` and `dream_cadence`. V2 plan
  enforcement remains off by default until the server enables
  `V2_PLAN_LIMITS`.
- New policy codes: `space_on_v1`, `verify_by_person`, `checks_by_person`,
  `flag_by_memax`, `cloud_propose_needs_web`, `personal_exists`, and
  `machines_by_owner`, `machines_by_person` and `machines_personal`.

### Changed

- `MemoryScope` is a closed object (`paths`, `agents`, `repos`): the
  API refuses any other key. Only `paths` was written before.
- `Memory.conditions` is a list of typed `Condition`s, where it was
  untyped.
- Handle the added `ReceiptAction` and `DreamSurfaced.kind` cases if
  your code switches exhaustively over those unions.

## 0.8.0

The first release with `memax.v2`, the client for the `/v2` API (the
V2 record: agents propose, people keep, and every change has a
receipt). The V1 resources still talk to `/v1`. Upgrading from 0.7.0,
read Breaking first.

### Breaking

- OAuth consent moved to the web session (OAuthConsent).
  `auth.getOAuthConsentRequest` and the `OAuthConsentRequest`,
  `OAuthConsentHub` and `OAuthConsentPermission` types are removed: the
  API no longer serves `GET /oauth/authorize/consent-request`. Use
  `auth.openOAuthRequest(id)`, `auth.decideOAuthRequest(id, { decision,
  space_id })` and `auth.releaseOAuthRequest(id)` with the `OAuthRequest`,
  `OAuthRequestSpace`, `OAuthDecisionInput` and `OAuthDecision` types, as
  the person signed in on the web.
- `notifications.completeItem` no longer answers `auto_resolved` or
  `auto_resolved_as`. A completion that finishes a checklist answers
  `all_done: true` instead; the notification stays pending with
  `payload.all_done_at` set (`ChecklistPayload.all_done_at`) and
  retires a day later.

### Added

- A request refused with 429 waits out a `Retry-After` of 30 seconds or
  less and goes again, when it is safe to send again (GET, HEAD, or one
  with an `Idempotency-Key`, as every `/v2` command has): at most twice,
  never more than 60 seconds in all, and an abort ends the wait.
  `rateLimitRetries` sets how many times (default 2; 0 turns it off).
- `memax.v2`, the `/v2` API. Its resources: `spaces` (list, create,
  export, and Switch to V2 and back), `memories` (Remember, Keep, Edit,
  Reject, Restore, bulk keep and reject, the near-duplicate check,
  conflicts, Forget with its preview, forget requests and tombstones),
  `review`, `receipts` (with checkpoints and Undo), `reads`, `agents`
  (connections and their autonomy), `briefs`, `targets` (compile,
  preview, drift and deliveries), `gates`, `notices`, `imports`,
  `notes`, `dream`, `sessions`, `devices`, `settings`, and `account`
  (with `account.passkeys`). `memax.v2.ask(space, question)` streams a
  cited answer as server-sent events. Commands take a required
  `idempotencyKey`; edits take the `ifMatch` version they started from.
  `refusalOf(err)` returns the policy decision behind a 403 `refused`,
  `forgetCarriesOf(err)` the memories that would go with a Forget (its
  409 `forget_carries`), and `askEventOf` and `undoableAction` help
  read Ask events and Dream actions. The resource classes are exported (`V2Resource`,
  `V2SpacesResource`, …), and their option types as `V2CommandOptions`,
  `V2ReviewOptions`, ….
- The `V2` type namespace (`V2.Memory`, `V2.CommandResult`,
  `V2.PolicyCode`, …), generated from
  `packages/server/openapi/v2.yaml` into `src/v2/schema.gen.ts`
  (`pnpm gen:v2`).
- Checking what Memax holds, with no dependencies: `verifyReceiptChain`
  recomputes a space's receipt chain against its signed checkpoints
  (with `canonicalReceipt`, `checkpointStatement`, `receiptLeaf`,
  `genesisHash` and `merkleRoot`), and `verifyExport` checks an export
  in the `memax.export.v1` format (with `parseExport`,
  `parseFrontmatter`, `parseReceipts`, `splitDocument`, `EXPORT_FORMAT`
  and `ExportFormatError`). `readEventStream` and `EventStreamParser`
  read server-sent events.
- Device sign-in (RFC 8628) for the CLI: `auth.startDeviceSignIn` and
  `auth.pollDeviceSignIn`, with the `DeviceSignIn`,
  `DeviceSignInOptions` and `DeviceSignInPoll` types. `auth.revoke(token)`
  signs a session out (RFC 7009).
- The web UI a person sees (the per-person V2 UI flag): `auth.me()`
  answers `ui` (`WebUi`: `"v1"` or `"v2"`), and `auth.exchangeCode()`
  returns `ExchangedTokens`, an `AuthTokenPair` with `ui` for a session
  issued to the web app.
- The passkey re-check: `MemaxConfig.passkeyCheck` answers a 403
  `needs_passkey` and sends the same request again with the browser's
  assertion (`PasskeyCheck`, `PasskeyCheckHandler`, `passkeyCheckOf`,
  `encodePasskeyAnswer`). Only the web app on memax.app can answer it.
- `AuthTokenPair.refresh_expires_in`: seconds the session has left.
- `auth.createKey({ standalone: true })` for a key that is you, rather
  than an agent's.
- `memories.batchAttribute(ids, agentName)` re-credits memories you own
  to one of your agents (`BatchAttributeResult`).
- Boards: `boards.resolveSlot` takes the `boardId` of a custom board's
  slot, the `reopen` action undoes a resolve or dismiss, and
  `boards.slotHistory` lists a slot's earlier content
  (`BoardSlotVersion`).
- `Settings.locale` and `SettingsUpdateInput.locale`.

### Changed

- `extraHeaders` moved from `StreamOptions` to `RequestOptions`, so
  every request can carry per-call headers. `StreamOptions` still
  inherits it, so existing stream calls are unaffected.
- Refresh tokens now rotate on every refresh: store the
  `refresh_token` each `auth.refresh` returns. A retired token used
  again after a minute signs the whole session out.
- The API refuses an API key with neither `agentName` nor
  `standalone: true`, so every write has someone to credit.
- `memories.push` leaves out `sourceAgent`, `assistedByAgent` and
  `initiationType` when they are unset, instead of sending empty
  strings.

## 0.7.0 - 2026-08-12

- Added `boards` for Pulse boards (`getForHub`, `listForHub`,
  `getBoard`, `createBoard`, `updateBoard`, `deleteBoard`,
  `resolveSlot`, `requestDecision`) and the `Board*` types.
- Added `personas` (`list`, `delete`, `listRevisions`, `getRevision`,
  `restoreRevision`) and the `Persona`, `PersonaRevision` and
  `PersonaRestoreResult` types.
- Added `topics.archive`, `topics.restore`, `topics.listArchived` and
  `Topic.archived_at`.
- Added `classifyAgentConfigFile` and `AgentConfigClass` (identity,
  memory, rules, settings), and the `profile:<name>` config scope.

## 0.6.0 - 2026-05-22

- Removed agent session sync: `agentSessions` and its types, and
  `agent_session` from `UploadPurpose`.
- Added email code sign-in: `auth.requestEmailOtp` and
  `auth.verifyEmailOtp`.
- Added `onboarding` (`state`, `restart`), checklist and digest
  notifications (`notifications.viewItem` and `completeItem`), and their
  item types (`Item`, `ChecklistPayload`, `DigestPayload`, …,
  `SUPER_NOTIF_KINDS`, `actionToResolution`, `resolveFromAction`).

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
