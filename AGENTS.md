# Memax — Agent Instructions

## What Is This Project

Memax is a **universal context & memory hub for AI agents**. It's a cloud-hosted memory layer that sits between users and their AI coding agents (Claude Code, Codex, Cursor, etc.), giving agents persistent, shared, secure access to user and team knowledge. A secondary **Ask** surface lets human users query that same knowledge directly and get AI-synthesized answers with citations.

Three co-equal product surfaces:

- **CLI** (`memax-cli` on npm, owned by MemaxLabs org) — for power users and CI/CD
- **Web App** (memax.app) — for all users including non-technical
- **Developer Hub** (docs.memax.app) — docs, API reference, integration guides

## Design Documents

> **Design docs live in the sibling private repo [`MemaxLabs/memax-internal`](https://github.com/MemaxLabs/memax-internal).** When this monorepo went fully open source, the `docs/` tree (plans, infra, design, engineering, benchmarks) stayed private in `memax-internal`. Clone it alongside this repo so paths resolve as `../memax-internal/docs/...`:
>
> ```
> ~/workspaces/
> ├── memax/            (this repo — open source code + CI)
> ├── memax-internal/   (private — design docs under docs/)
> └── internal-docs/    (private — business docs)
> ```
>
> The paths below are written repo-relative (`docs/plans/...`); read them from `../memax-internal/docs/plans/...`. If the sibling checkout isn't present, prompt the user to clone `MemaxLabs/memax-internal` rather than guessing.

Read these before making architectural decisions or starting new features:

- `docs/plans/01-vision-and-strategy.md` — product vision, user scenarios, competitive landscape, strategic positioning
- `docs/plans/02-system-architecture.md` — system design, data flow, infra
- `docs/plans/03-memory-model.md` — memory model, categories, boundaries, lenses, topics
- `docs/plans/04-retrieval-engine.md` — retrieval pipeline, ranking, performance targets
- `docs/plans/05-security.md` — auth, trust levels, encryption, audit
- `docs/plans/06-developer-surface.md` — CLI, SDK, MCP, agent integrations, hooks
- `docs/plans/07-team-hubs.md` — hub architecture, UX, smart routing, team collaboration
- `docs/plans/08-web-experience.md` — web app design, UX patterns, design language
- `docs/plans/09-config-sync.md` — agent config sync, knowledge extraction
- `docs/plans/10-dreams-and-knowledge.md` — dream engine, knowledge organization, topics
- `docs/plans/11-roadmap.md` — phased implementation roadmap with status
- `docs/plans/25-memax-v2.md` — **V2 master plan** (on the `v2` branch of `memax-internal`). Read it before any V2 work. The V2 design handoff (PRD, Ledger design system, screens) is in `docs/v2/handoff/`, and the index is `docs/v2/README.md`

## Business Documents

Business documents live in a separate private repository: [`MemaxLabs/internal-docs`](https://github.com/MemaxLabs/internal-docs). Clone it as a sibling of this monorepo so paths resolve as `../internal-docs/NN-*.md`:

```
~/workspaces/
├── memax/                    (this repo)
└── internal-docs/            (business docs, private)
```

Read these before making pricing, cost, or go-to-market decisions:

- `../internal-docs/01-business-model.md` — pricing tiers (Free/Pro $12 or $120/yr/Team $20/seat or $192/yr/Enterprise custom), revenue model, unit economics
- `../internal-docs/02-go-to-market.md` — launch strategy, channels, growth loops
- `../internal-docs/03-competitive-landscape.md` — competitors (platform memory, memory APIs, cross-tool MCP memory, config translators), positioning
- `../internal-docs/04-growth-engine.md` — PLG mechanics, conversion funnels, virality
- `../internal-docs/05-partnerships.md` — agent platform partnerships, integration strategy
- `../internal-docs/06-fundraising.md` — fundraising strategy, investor targeting
- `../internal-docs/07-cost-analysis.md` — per-operation costs, infrastructure by scale, margin analysis

If the sibling checkout isn't present, prompt the user to clone `MemaxLabs/internal-docs` alongside this repo rather than guessing or answering without it.

## Before Starting Work

- Read the relevant design doc(s) in `docs/plans/` before implementing a feature
- If the task touches architecture or adds a new service, read `docs/plans/02-system-architecture.md` first
- If the task touches retrieval or recall, read `docs/plans/04-retrieval-engine.md` first
- If the task touches security or access control, read `docs/plans/05-security.md` first
- If the task touches hubs, teams, or push routing, read `docs/plans/07-team-hubs.md` first
- If the task touches the web app, read `docs/plans/08-web-experience.md` and `docs/design/memax-design-system.md` first
- If the task touches dreams or knowledge organization, read `docs/plans/10-dreams-and-knowledge.md` first
- If the task touches worker/job logging, observability, or the admin ops logs panel, read `docs/infra/logging.md` first
- If the task touches content states, loading/empty/error UI, or dream experience, reference the north star at `/dev/kitchen` for live visual demos and component-to-file mapping
- If the task touches pricing, business model, competitive analysis, fundraising, or growth strategy, read `.agents/skills/business/SKILL.md` first

## Memax Context Protocol

Memax is both the product being built AND the source of truth for project context. All agents with memax MCP access must use it as persistent memory across sessions.

### Memory Rules (STRICT)

1. **NEVER** rely on your own memory for project-specific details — architecture, API contracts, data models, product requirements, design rationale, implementation decisions.
2. **ALWAYS** `memax_recall` before starting any task.
3. **ALWAYS** `memax_push` after completing significant work.
4. If your memory conflicts with memax, **memax wins**.

### Recall → Act → Push Workflow

**Starting a task:**

1. `memax_recall("{specific topic}")` — targeted, not broad
2. Read context → proceed

**If recall returns insufficient or no results:**

1. You MAY use your own knowledge as a **temporary fallback**
2. **Flag it explicitly**: "memax had no context on this — using my own knowledge, may be stale"
3. After completing the task, **immediately `memax_push`** what you used/decided so future sessions have it

**Making a decision:**

1. Implement → `memax_push` the decision rationale + what changed + what was rejected and why

**Resuming work (new session):**

1. `memax_recall("session summary")` + `memax_recall("{current workstream}")` to rebuild context
2. Do NOT assume continuity from prior conversation

### Recall Best Practices

- Specific queries: `"webhook auth flow callback"` not `"project"`
- If first recall is thin, retry with different keywords before falling back
- Check both recent decisions AND foundational architecture when touching core systems

### What to Push (do this WITHOUT being asked)

| Trigger                             | What to push                                          |
| ----------------------------------- | ----------------------------------------------------- |
| File created/modified significantly | Summary of what + why                                 |
| Decision between 2+ approaches      | Tradeoff analysis, chosen path, rejected alternatives |
| Bug fix that took investigation     | Root cause, symptoms, fix                             |
| Ambiguous product requirement       | Your interpretation + reasoning                       |
| New dependency or integration       | What, why, configuration details                      |
| Schema/API contract change          | Before → after, migration notes                       |

### Session End Protocol

Before ending ANY session, `memax_push` a session summary:

- What was done (with file paths + function names)
- What's in progress
- What's blocked or deferred
- Open questions

### Confidence Annotation

When responding with project-specific claims, annotate source:

- **From memax** — current, trusted
- **Fallback (own knowledge)** — may be stale, will push to memax after
- **Unknown** — need to read code or ask user

## V2 (branch `v2`)

V2 rebuilds Memax as "the context layer you own". **Read `docs/plans/25-memax-v2.md` (in `memax-internal`, branch `v2`) before any V2 work.** The founders accepted its decisions D1–D15 on 2026-10-06. Work follows the phases in its §12 and stops at each gate. The rules below override the V1 conventions for V2 code. V1 code is frozen (bug fixes only) and keeps its V1 rules until it is deleted at cutover.

**The record**

- **One write path.** Every change to the V2 record goes through `internal/ledger` (`Ledger.Apply(ctx, Command)`). No handler, worker, MCP tool or Dream phase writes V2 record tables directly.
- **Receipts.** Every write carries a receipt in the same transaction, or the database refuses it (a deferred constraint trigger). Receipts never contain memory text, so Forget can purge words without rewriting history.
- **Schema.** V2 tables live in the Postgres schema `v2`, with row-level security on every space-scoped table. The app role sets `app.space_ids` / `app.tenant_ids` per transaction, and explicit `space_id` filters stay as defence in depth.
- **Round trips.** Production's API (Fly sjc) is about 24 ms from Neon (us-west-2) per round trip, so latency is counted in round trips. A ledger transaction (`internal/ledger/tx.go`) sends `BEGIN` and the scope's `set_config` in the same pgx pipeline as its first statement (Postgres runs a pipeline in order and skips the rest after an error, so nothing runs before the scope); a read-only one's `COMMIT` goes out after the caller has its answer; River's role switches ride with River's first statement and with `COMMIT`; `execDeferred` sends a write nobody reads with the next statement; and `meter` sends its `COMMIT` with its one statement. Send independent statements together: `tx.SendBatch`, `Ledger.ReadBatch` (`BEGIN`, scope, statements and `COMMIT` in one round trip), `attachDetails`' extras, a command's space read with its idempotency claim (`spaceAndClaim`), Keep's target bump with its undo read (`markDirtyAndJournal`), and goroutines for independent reads (recall's notices and gates beside its search). A display ID resolves inside the statement that locks the memory (`lockRef`, a scalar subquery: ambiguous refs fail without taking a lock). Never send a statement on `tx.Conn()`. `netsim.Audit` reads the wire and fails any statement on a `v2` table outside a transaction, before its scope or as another role (`TestScopeComesFirst`, and every round-trip guard). The `*RoundTrips` tests in `mcpv2`, `v2api`, `judge` and `compile` hold each hot path to its count of round trips (a recall in a space is 17); a change that adds one must lower another or raise the budget with a reason.
- **Cross-space reads.** The few background reads that span spaces (the compile sweep, the seal sweep and verifier, the reads metrics and retention, the product metrics) admit rows through policies keyed on `app.sweep`, which any session can set, so each such policy applies only to a role `memax_v2` can't act as: `memax_v2_compile_sweeper` (migration 040), `memax_v2_sealer` (039), or a `SECURITY DEFINER` function's owner (038, 053). `TestSweepPoliciesAreRoleBound` fails on a new one written for every role.
- **States.** A memory has a lifecycle (`proposed | kept | merged | faded | forgotten | rejected`) plus flags (`stale`, `conflict`). The displayed state is derived from both.
- **IDs.** Display IDs (`M-0219`, `N-`, `H-`, `C-`, `R-`, `D-`, `B-`, `G-`) are per-tenant counters. Internal keys are uuidv7.
- **Trust.** Agents propose and people keep. Autonomy (read / propose / write), roles, quarantine of external content and plan limits are decided in one place: `policy.Decide`. A memory's trust is the minimum of its sources, and Dream can't raise it.
- **Agent connections.** Every API key and OAuth grant resolves to an agent connection (`v2.agent_connections`, migration 029) with autonomy per space; receipts name the connection. A credential with no connection, a paused one, or a space it isn't connected to only reads. Only people change connections (`policy.DecideConnection`), and raising autonomy needs `human_web`.
- **Compiles.** A space's Brief (`B-`) and targets (`AGENTS.md`, the `CLAUDE.md` shim, scoped Cursor rules, the ChatGPT copy-out) live in migration 031. The `GEMINI.md` shim is opt-in, never a default (`ledger.OptInTargetKinds`, the compiler's `OPT_IN_TARGET_KINDS`, checked against each other by `TestRealServiceMatchesTheLedgersTargets`): Antigravity CLI reads `AGENTS.md`, and `memax connect gemini --gemini-md` adds the shim for Gemini CLI. A command that changes what compiles bumps `targets.dirty_gen` and inserts the `compile_target` River jobs with `InsertManyTx` **in the command's transaction** (`internal/ledger/jobs.go` switches back to the login role for River's tables), so a failed insert rolls back the whole command. `internal/compile` runs the jobs against the stateless compile service (`packages/compile-service`, a Cloudflare Worker that requires `COMPILE_SERVICE_TOKEN`) and records each run (`C-`) through the ledger as Memax. Hand edits come back as proposals (`file:line` sources) through observations and `ResolveDrift`; a deleted line never forgets anything by itself.
- **The judge.** Every proposal, and every memory a Write-level agent kept at once, is judged by the River job `judge_proposal` (`internal/judge`), enqueued in the command's transaction: repeats are folded, updates linked, and a contradiction of a decision in force is flagged as a conflict before anyone keeps it (rule 11). It acts only through `ledger.RecordVerdict`, as Memax. Model tiers are explicit config (`JUDGE_*`), never inferred from a model name. The primary and fallback answer at temperature 0 (`JUDGE_TEMPERATURE`, `JUDGE_FALLBACK_TEMPERATURE`); the strong tier sends none, since Claude Sonnet 5.5 refuses any other. A person settles a conflict with `ResolveConflict`, and undoes their own decisions (and the judge's folds) with `Undo`, addressed by receipt.
- **Pinned model hosts (D14).** Every judge and Ask call goes through the shared client (`internal/anthropic`) with OpenRouter's `provider.zdr`, an ordered allowlist of zero-retention hosts (`provider.only` and `provider.order`) and a precision floor (`provider.quantizations`, fp8 or better: no fp4 hosts). Each tier reads its own (`JUDGE_PROVIDERS`, `JUDGE_FALLBACK_PROVIDERS`, `JUDGE_STRONG_PROVIDERS`, `ASK_PROVIDERS`, `JUDGE_MIN_QUANTIZATION`, `ASK_MIN_QUANTIZATION`) once at startup; unset, a tier takes its slug's hosts from `anthropic.DefaultProviders` (V4.1 Flash: Together, Baseten, CoreWeave, DeepInfra; Haiku 4.5: Bedrock, Vertex; Sonnet 5.5: Vertex), and `*_ZDR=false` drops the defaults. The hosts and the reasons are in `eval/judge/RESULTS.md`; the docs site's Security page lists them, with Voyage, as sub-processors. Change a list there too, and re-run the live evals: `eval/livemeter` fails any call served by a host its tier didn't pin or below its floor.
- **Rule 11 after the write (migration 042).** A Write-level agent's write that the judge finds contradicting a decision in force goes back to Review (kept → proposed, in conflict) with a `returned` receipt naming the decision, but only within `DefaultReturnWindow` (10 min) of the agent's own keep or edit and while nothing has changed or built on it (`returnable`); otherwise it is flagged where it stands. The lifecycle guard admits that move only beside Memax's same-transaction `returned` receipt. A return isn't undoable: the person settles the conflict. "Keep both" words that touch another decision in force wait for the judge (mode `settling`, `JudgeArgs.Beside` the conflict's other side): a proposal's are its new version, a kept side's a draft (a version above the current one, `drafted` receipt) that only the applied resolution makes current. The answer is 200 `judge_pending`; the same resolution, sent again, waits (503 `judge_pending`), then applies or answers 409 `in_conflict`.
- **Assurance.** A person's Keep is `human_web` only when the session was issued to the web app (the token's `surface` claim, migration 030) **and** `/api/proxy` signed the request with `WEB_SURFACE_SECRET` (`internal/websurface`, which has the threat model). Everything else, the CLI included, is `client_attested`. The web session lives only in the web app's server (a BFF, `packages/web/src/lib/bff`): `__Host-` HttpOnly, Secure, `SameSite=Strict` cookies `/api/auth/*` set and `/api/proxy` reads, refreshing single-flight; page script never sees a token, never sends `Authorization`, and nothing in either web tree reads or writes a token in `localStorage` (`bundle-scan.test.ts`). Every cookie-backed route checks Fetch Metadata (`csrfRefusal`): state changes need `Sec-Fetch-Site: same-origin` (or our `Origin`).
- **Sessions and refresh tokens.** Every sign-in (web, CLI, device code, MCP OAuth) is a session (`internal/sessions`, migration 049): its refresh token is stored as its SHA-256 and rotates on every refresh (`/v1/auth/refresh` and `/oauth/token` alike); a retired token presented within `ReuseGrace` (60 s) yields the session's current token (racing tabs and processes), and after it revokes the session. Access tokens name their session (`sid`). `/v2/sessions` lists a person's sessions and signs one or all others out (others need `human_web`, `policy.DecideSession`); `POST /oauth/revoke` (RFC 7009) signs a client's own session out (the web's sign-out, `memax logout`). Revoking ends refresh at once and access within the hour.
- **Embeddings.** The current version of every proposal and kept memory is embedded whole into `v2.memory_embeddings` (migration 037: one `halfvec(1024)` per memory, version and model, PLAIN storage, a `(space_id, model)` btree and no HNSW; memax_v2 may only SELECT and INSERT). Every command that writes a searchable version enqueues `index_memory` in its transaction (`ledger.WithIndexJobs`). `internal/v2index` embeds each space's waiting versions in one Voyage request, and a periodic sweep queues any version left without an embedding of the index model. Forget's purge of the words takes the vectors with it, by trigger. Models are explicit config (`V2_EMBED_MODEL` voyage-4, `V2_EMBED_QUERY_MODEL` voyage-4-lite), eval-gated. Without `VOYAGE_API_KEY` nothing is embedded and retrieval stays lexical.
- **Recall and search.** `internal/v2recall` runs the lexical lanes and embeds the query in parallel, then an exact, space-scoped KNN (`ledger.Nearest`), weighted RRF with V1's constants, and Voyage `rerank-3-lite` on more than 8 candidates within 150 ms (`V2_RERANK_MODEL`). A query embedding that misses 120 ms answers lexically, and MCP says so in `_meta["app.memax/retrieval"]` beside `lexical_only`. The judge's vector candidates come from the same embeddings (`JUDGE_VECTOR_FLOOR`, 0.65), and so does Remember's near-duplicate check (`V2_NEAR_DUPLICATE_FLOOR`, 0.85). The recall floor (`V2_RECALL_VECTOR_FLOOR`, 0.35), these floors, the models and the judge's prompt and thresholds were calibrated by the live evals of Oct 6, 2026 (`packages/server/eval/{judge,v2,ask}/RESULTS.md`). Re-run them before changing any.
- **Sealed receipts.** The worker's sealer (`internal/sealer`, migration 039) chains each space's receipts off the write path: a `seal_sweep` every `SEALER_INTERVAL` (10–60 s) moves a cursor over receipts whose transactions have ended (txid below the snapshot's xmin, so an in-flight receipt is sealed in its place once it commits) and queues a `seal_space` job per space; each cuts a checkpoint (`v2.receipt_checkpoints`: range, RFC 6962 Merkle root, chain hash, Ed25519 signature from `RECEIPT_SIGNING_KEY`) and copies it to object storage, best-effort. The encoding is `internal/receiptchain` (format 1, length-prefixed binary; the SDK's `verifyReceiptChain` implements it byte for byte, and golden vectors in both test suites keep them equal: never change format 1, add a format). A receipt commits to its reason as `reason_sha256 = SHA-256(salt ‖ reason)`, written by the database; Forget's redaction nulls the reason and the salt and keeps the commitment, so the chain survives it. Seals carry no receipts (a seal receipt would need sealing, forever); only the role `memax_v2_sealer` writes them, and `memax_v2` only reads them. `receipts_verify` recomputes every chain from genesis nightly; mismatches log at error level with `metric: receipt_chain_mismatch`.
- **Reads are not receipts.** An agent read (`R-`, migration 038) goes to `v2.reads`: append-only for `memax_v2`, partitioned by UTC month (created ahead by `v2.ensure_reads_partitions`; 13 months kept, pruned by the worker's daily `reads_maintain`), with counters in `v2.read_rollups` per space, subject (a memory, or a compile run), UTC day and reader. Read paths hand a `ledger.ReadEvent` to the process's `internal/reads` recorder **after** the response, from the result's items (never inside query code): a bounded buffer flushed every 250 ms through `Ledger.RecordReads`, which drops and counts (`memax.reads.dropped`) rather than block. A session-start hook reports the compile it loaded with `POST /v2/spaces/{space}/compile-loads` (written at once, idempotent, decided by `policy.Decide(ActionRead)`: an agent counts reads only where it's connected); it counts as a read of every fact in the compile, resolved through `compile_runs.refs` when counted. A read never holds memory text or query text. Fading asks `Ledger.ReadStatus` (last read by any observed path, and whether the memory is in a file whose loads Memax can't see); the north star is `Ledger.GetReadMetrics`.
- **Product metrics (migration 053).** The phase gates (plan 25 §12) are computed from receipts, reads, agent connections and imports by weekly signup cohort (Monday, UTC) in `internal/ledger/product_metrics.go`, whose header and the migration's hold the definitions: **signup** is the account's creation, or for someone who wrote V1 memories before their first V2 space (cohort `from_v1`, reported beside `new` and never in a gate's denominator, because the switch connects V1 credentials at once) that first V2 space; people who never reach a V2 space and staff (`admin_roles`) aren't counted. The **first session** is `ledger.FirstSession` (24 h). **Activated**: by its end, 2+ agent connections (their `connected` creation receipts, not disconnected) and a compile the person's own CLI or daemon delivered (a `delivered` receipt by them or their connection); counted only once the session has closed. **First file**: init's first import (`v2.imports`, origin `init`) to the first delivered compile, judged on the median against 5 minutes. **Week-4 keeping**: activated people with their own `kept` receipt in days 21–28. **Team pull**: another member joined a V2 space they own within 60 days (every person until plans are billed). **Review health**: per week proposals were made, the first of kept, rejected, folded or forgotten, the reject rate and time to decision. `v2.product_metrics` and `v2.review_health` are `SECURITY DEFINER` with owner-bound policies on `app.sweep`, return counts and seconds only, and only the NOLOGIN role `memax_v2_metrics` may execute them (`memax_v2` can't); `Ledger.GetProductMetrics` switches to it and `PhaseGates` judges the gates on the `new` cohort. The worker's daily `reads_maintain` logs every cohort, review health and gate beside the north star (`metric`: `product_cohort`, `review_health`, `phase_gate`) and records `memax.product.*` and `memax.review.*` gauges; admins read `GET /v1/admin/v2/metrics` in the ops panel's Gate metrics tab (`@/lib/admin-client`, never the SDK); `cmd/v2-gate-metrics` prints the table. The web sends the onboarding funnel to PostHog (`lib/v2/funnel.ts`: `onboarding.signed_in` … `compile_done_reached`, counts only, by type); the CLI sends no telemetry. `internal/ledger/ledgertest` writes records at chosen times for these tests.
- **Ask.** `POST /v2/spaces/{space}/ask` (`internal/ask`) streams a cited answer from the space's kept memories as server-sent events: `sources`, then `delta` and `cite` in answer order, then `done` (or `error`). It searches through `v2recall` (so superseded decisions and other spaces never come back) and leaves quarantined (external) memories out of answers, since a model can be steered by words in its context and keeping such an answer would launder them. The answer tier is explicit config (`ASK_MODEL`, `ASK_ZDR`, `ASK_PROVIDERS`, `ASK_MIN_QUANTIZATION`, `ASK_TIMEOUT_MS`; `off` answers with the matching memories only). Citations of anything the model wasn't given are removed before they're sent; an answer with none left ends `unsupported`, and the model's `NOT_COVERED` ends `not_covered`. Only a signed-in person asks (`policy.Decide(ActionAsk)`); asks that reach the model count on `v2.ask_usage` (migration 041, RLS on `app.person_id`), and the plan's monthly limit is decided there too (`FreeAskLimit`, D9), none during the alpha unless `ASK_MONTHLY_LIMIT` sets one. An Ask writes no record row and no receipt, and records no `R-` read (reads are agents'; `ask.Observer` is the seam). A disconnect cancels the model call (the web proxy forwards the browser's abort). Keeping an answer is the person's Remember with sources of kind `memory` (the cited memory's trust, resolved in the ledger).
- **Dream (migration 047).** Dream is overnight upkeep in the open (plan 25 §5.10, `internal/v2dream`): each run that has something new to read (notes after its cursor, record changes since the last edition, a `stale_after` that came due) publishes an edition (`D-`, `v2.dream_editions`) of small actions (`v2.dream_actions`) through one ledger command, `PublishEdition`, as Dream, each action with its receipts citing the edition (`source: {kind: dream}`; `published` on the edition, `folded` for notes folded into a kept memory as lineage). The phases: fold notes into kept memories (`folded_from` links, the words never change), propose new facts from notes, fold duplicate proposals before Review, flag conflicts (the judge's classifier, its strong tier on decisions in force), flag what passed its `stale_after`, fade what nobody read in 60 days (`ReadStatus`; never a memory in a file whose loads Memax can't see, a decision in force, a flagged one or one the Brief places), and small cited Brief changes (`ApplyBriefOps`; an uncited line fails validation). Dream never keeps anything and can't raise trust; the ledger re-checks every planned action against the record as it is when the edition lands (Forget during a run wins). Every action has an inverse (`UndoDreamAction`, 30 days, `DREAM_UNDO_DAYS`) that restores the state exactly (rule 9), refused like `Undo` when something changed since; `Restore` brings a faded memory back. Scheduling is a due-time sweep (`dream_sweep`, no River Pro) over `v2.dream_schedules` as `memax_v2_dream_sweeper` (app.sweep = `dream`): 03:00 in the owner's zone (`v2.dream_settings`, observed from the app's `X-Timezone` or set in Settings; never assume Pacific), nightly on Pro's five busiest spaces and weekly otherwise, one run per missed night (catch-up runs the latest only), editions unique per (space, slot). The tiers are explicit config (`DREAM_*`, mirroring `JUDGE_*`: ZDR on, each tier pinned to its hosts at fp8 or better with `DREAM_*PROVIDERS` and `DREAM_MIN_QUANTIZATION`, temperature 0 where the model takes one), with a per-run call budget and a dry run. The morning email (`dream_email`) goes to the people who may keep in the space, with words they can read and a one-click unsubscribe (`/unsubscribe`, `POST /v2/dream/email:unsubscribe`, public). The web shows the edition on Today's card and at `/[space]/dream` (`source.dream`); Dream settings are in Settings › Account.
- **Settings: notifications and security (migration 050, epic 2.6).** A person's notification preferences are `GET`/`PATCH /v2/me/notifications` (`ledger.GetNotificationSettings`/`UpdateNotificationSettings`, `v2.notification_settings`, RLS on `app.person_id`, no receipts: preferences aren't the record): per event (`decision_gate`, `morning_edition`, `review_waiting`, `drift`, `stale`, `write_held`, `weekly_summary`, `forget_done`, `agent_changed`) whether it reaches them by email (in app isn't a choice), quiet hours in their time zone (Dream's, `v2.dream_settings.time_zone`; gates may come through) and the Review reminder's wait. Edits need an Idempotency-Key (a retry replays, from a one-slot key on the row) and If-Match (the settings' version). The morning edition's email has one copy, `v2.dream_settings.morning_email`, which Settings › Account and the one-click unsubscribe also write; a trigger on it moves the settings' version, so an edit made before an unsubscribe answers 412 instead of undoing it. The morning email waits for the end of each recipient's quiet hours (the `dream_email` job snoozes). `email_sent` says which emails go out today (only the morning edition, `v2api.WithNotifications`); the others' choices are kept for when they ship. V1's framework (plan 17) is an inbox (`public.notifications`) and one switch (`notifications_enabled`), so neither table holds these; delivery reuses `internal/email`. `GET /v2/security` is Settings › Security's data from `internal/trust` (`trust.FromEnv`, the same configuration the processes run on): this session's assurance, where data lives (`V2_DATA_RESIDENCY`), every processor that sees a memory's words with each tier's model, pinned hosts and precision floor (tiers that are off aren't listed), Voyage's retention (`unconfirmed` until `VOYAGE_RETENTION=zero`) and the backup window. Both are people's only.
- **Forget (rule 7).** `ledger.Forget` (migration 044) purges a memory's words from Postgres in one transaction: every version (drafts too), its sources' quotes and URIs, its decision fields, search entry and embeddings (by trigger), its receipts' reasons (`v2.redact_receipt_reasons`), the judge's words on both sides of every pair, stored idempotency hashes, a gate's question when it is the gate's answer, the model's words on every import disagreement it belongs to (settled or not), and every Brief line citing it (a new `B-`); a deferred trigger refuses the commit if any word is left or the tombstone (`v2.tombstones`) is missing. Memories carrying its words (folded into it, updating it, citing it, such as a kept Ask answer) go with it, named in `carries`, or it is refused with `forget_carries`. Only a person forgets (`policy.Decide(ActionForget)`; owners by default, and a decision in a team space needs `human_web`, D15); an agent's `memax_forget` records a request (`RequestForget`) that a person forgets or declines (`DeclineForget`) on the web. Undo refuses it. In the same transaction it marks targets dirty, writes the propagation steps, queues a notice for every agent connection that read it or is connected to the space (`v2.agent_notices`, delivered once in the next MCP response, `_meta["app.memax/notices"]`, or `GET /v2/notices` + `:ack` for the stdio server), and inserts `forget_propagate` (queue `forget`, `internal/forget`): recompile every target that held it, re-render or delete the stored artifacts, purge every process's caches over Redis (`memax:v2:forget`), and copy the op (IDs only) to object storage as the forget ledger, within a minute. `GET /v2/memories/{ref}/forget-preview` says what a Forget would do before anyone confirms; the tombstone (`GET /v2/memories/{ref}/tombstone`) says each step and the copies Memax can't reach (git history, agents' own memory, backups for `V2_BACKUP_RETENTION_DAYS`, model providers, hand edits, copy-outs), from configuration (`forget.HonestyFromEnv`). `ForgetSpace` forgets everything in a space (retiring its V2 rows when the space is deleted, so `v2.space_ledgers` keeps receipts, seals and tombstones verifiable); V1's `DELETE /v1/hubs/{id}` and `DELETE /v1/account/data` go through it first (`handler.V2Forgetter`), and `ForgetAccount` adds disconnecting every agent. After any database restore, run `cmd/v2-reapply-forgets`.
- **Export (rule 14).** `POST /v2/spaces/{space}:export` streams a space's whole record as a zip archive in the export format `memax.export.v1` (`internal/export`; documented on the docs site's Export format page): one folder named by the slug with every memory as Markdown (YAML frontmatter in a fixed JSON-valued subset: state, sources with `file:line`, versions, decision fields, conditions, links, its receipts; the statement in force as the body), a tombstone without words per forgotten memory, every Brief version, `gates.json`, `targets.json`, `agents.json`, `reads.json` (counts), `receipts.jsonl` (every receipt in chain order, its canonical fields plus reason and salt while they exist) and `checkpoints.json` (every signed checkpoint and the public keys), with `export.json` listing every file's SHA-256. The same record gives the same bytes (sorted, UTC microseconds, nothing clock-dependent); `TestGoldenExport` writes the golden export the SDK and CLI tests read (`packages/sdk/src/v2/testdata/export-v1`, never reformatted). The export is one `exported` receipt on the space's stream (migration 046; `ledger.Export`, people only, `policy.ActionExport`, refused `export_by_person`), written before `ledger.ReadExport` reads the record in one REPEATABLE READ snapshot on the ledger's read path, pipelined and in batches (the command 5 round trips, a small space's read 10; `TestExportIsScopedAndPipelined` audits and budgets both), so it lists its own (unsealed) receipt; it records no reads, stores nothing on the server (a stored copy would be one more thing for Forget to reach), and is rate-limited per person in-process. A retry with the same `Idempotency-Key` writes nothing and returns the same bytes. The SDK's `verifyExport` (dependency-free) checks every file against `export.json`, the chain against every checkpoint with pinned or the server's keys, and the memory and tombstone files against the receipts; words are never in receipts, so they are proven only as far as `export.json` is.

**API, MCP and CLI**

- **API.** New endpoints go under `/v2`, spec-first in `packages/server/openapi/v2.yaml`. SDK types are generated from that spec. The `model.ApiResponse` envelope still applies. Commands need an `Idempotency-Key`, and edits need `If-Match`. A read that is a `POST` to keep its input out of URLs (the near-duplicate check) is marked `x-memax-read: true` and takes none. `/v1` is frozen for old CLIs, and retired `/v1` routes answer 410 with a pointer.
- **The `/v2` contract workflow.** The spec is written first and the build holds everything else to it:
  1. Change `packages/server/openapi/v2.yaml` (OpenAPI 3.1, Apache-2.0 so the SDK can carry its types). Close every object (`additionalProperties: false`) and name every response schema; `internal/contract` lints these rules.
  2. Implement the handler in `packages/server/internal/handler/v2api` and add the route to `v2api.routes` (the route table must equal the spec, and nothing else in `serverapp` may register a `/v2` path). Handlers call only `internal/ledger` (and `internal/compile` for compiled words: the preview, observations, the drift view; the draft embedder, `v2api.WithDrafts`, for the near-duplicate check; and the device codes, `v2api.WithDevices` over `internal/deviceauth`, for confirming the CLI's sign-in). A credential becomes a ledger actor in one place, `principalFor`.
  3. Test through `env.do` in `v2api`'s tests: every request and response runs through the spec, so an undocumented status, header or field fails, and the run fails if any operation lacks a 2xx test. A `text/event-stream` 200 (Ask) is checked event by event against a named `oneOf` of `{event: const, data: named schema}` objects; `env.live` reads such a stream from a real server as it arrives.
  4. Regenerate the SDK types (`pnpm --filter memax-sdk gen:v2`), add the typed method under `memax.v2`, and commit the spec, server, SDK and `src/v2/schema.gen.ts` together. `pnpm lint` fails when the generated types are stale.
- **MCP.** All 17 V1 tool names keep working (both profiles), and remote and stdio parity still applies.
- **CLI.** The install command is `npx memax-cli init`; the binary is `memax`.
- **Init and imports (migration 043).** `memax init` (`packages/cli/src/commands/init.ts`, the flow in `src/lib/init/run.ts`) detects agents and their files, signs in, connects the agents (writes MCP settings once asked, and installs each connected agent's session-start hook through `memax connect`'s installer, `lib/connect/hooks.ts`; connections start at Propose, Cursor and Gemini CLI at Read, never raised from the CLI), then splits each file into statements **on the machine**: secretlint's recommended preset plus the server's own refusal patterns (`internal/secrets`, ported; `testdata/credentials.json` is the corpus both suites read) keep secrets local, and the compiler's `cleanLine` strips hidden Unicode (counted in `lib/init/hidden.ts`). A file's kind sets its trust (`lib/init/files.ts`: repository files `repository`, `~/.claude/CLAUDE.md` and the like `person`, agents' memory `agent_own_work`, lines only on a non-default branch `external`). Statements go up as one import per space, `POST /v2/spaces/{space}/imports` (≤ 500 items, `file:line` sources, `via=import` so they only ever propose; a statement the space has is `existing`, repeats in one import are `folded`). The import enqueues `judge_import` in its transaction: one model call over the whole import, each statement with its file, globs and headings (`internal/judge/imports.go`, its own bar `ImportConflictBar`, not `Contradicts`, calibrated by `eval/imports`), groups statements that disagree into `v2.import_conflicts`, each receipted and settled once as a group (`…/conflicts/{n}:settle`). `GET …/imports/{id}` says which proposals can be kept in bulk (`bulk`/`held`); `POST …/memories:keep` and `:reject` take up to 200. New people get spaces from `POST /v2/spaces`; a V1 space moves with `POST /v2/spaces/{space}:switch` (below).
- **Switch to V2 and notes (migration 048, epic 2.8).** A V1 space moves to the record with `POST /v2/spaces/{space}:switch` (`to: v2|v1`, `kind`, `repository`; Idempotency-Key), its owner only (`policy.DecideSwitchSpace`), from the web (Today of a space on V1, `_places/today/switch-to-v2.tsx`), `memax switch` or `cmd/v2-switch-space`; `GET /v2/spaces/{space}/switch` previews what moves, read fresh from V1, and says where a switch stands. `internal/ledger/switch.go` runs it as resumable steps recorded in `v2.space_switches` (space, notes, personas, configs, candidates, agents, gates, switch): the hub's kind and repository (a V1 team hub may become a project space; roles map owner → owner, admin → member who can forget, contributor → member, viewer → viewer); every V1 memory but onboarding seeds is numbered as a note in `v2.note_refs` (047's table, widened: the `N-`, where its words live, who wrote them and what the switch did with it, never words), and so are personas and the space's agent files. A person's own V1 memories of one statement (`disposition = candidate`) go up as one import with `origin = v1` and `N-` sources (ReviewImport "From V1"); what agents wrote and longer documents (`fold`) are Dream's, whose snapshot reads only `fold` notes and cites them at the trust the switch recorded; archived and credential-bearing ones stay notes (`note`). The agent files become compile targets and V1's two-way config sync stops for them (`spacemode.ConfigSyncOff`: sync answers `unchanged`, reason and `X-Memax-Warning` `space_on_v2`; `memax agents sync` says so for one release). The members' V1 keys and grants connect at Propose (`cmd/v2-backfill-agents`'s logic), each told once in its next MCP response (`v2.agent_notices`, kind `switched`); V1 Dream runs are read-only history (`GET …/v1-dream-runs`); plans are kept (D9); `hubs.v2_enabled_at` is set last, beside a `switched` receipt. With candidates to judge, the steps run in the worker (`space_switch`, queue `switch`, `internal/v2switch`), else in the request; a failed step resumes when asked again. The switch changes no V1 row, so switching back (`to: v1`, a `switched_back` receipt) gives V1 exactly as before (V1's pipeline included) and the V2 record stays for a later switch. Notes are their owner's to read (`GET /v2/spaces/{space}/notes`, a search over V1's chunks; `…/notes/{note}`; `memax_search` with `include_notes`) and are never compiled. Forget reaches them: `POST …/notes/{note}:forget` (`ledger.ForgetNote`, `policy.Decide(ActionForget)`; `…/forget-preview` first) deletes the V1 row as V1's delete does and scrubs what V1 derived from it (`v2.purge_note_words`), takes the memories citing it with it (`carries`), writes a `note` tombstone and propagates like Forget; `cmd/v2-reapply-forgets` re-applies it.
- **Export and verify-export.** `memax export [--space] [--out dir] [--force]` (`packages/cli/src/commands/export.ts`) writes each V2 space's export into `<out>/<slug>` through a zip reader (`src/lib/export-files.ts`) that checks every entry's CRC and never writes outside the folder; `memax verify-export <dir|zip> [--key …] [--offline]` runs the SDK's `verifyExport`, trusting `--key`, else the server's published keys, else (and saying so) the export's own. The web app's Memories **Export as Markdown** downloads the same archive.
- **Warm start and `memax connect` (epic 2.7).** Agents' SessionStart hooks run `memax hook session-start --agent <id>` (`packages/cli/src/hook-main.ts`, reached through `src/bin.ts` without the rest of the CLI, with Node's compile cache on; the logic is `src/lib/hook/`). It reads only local files: the daemon's cache `~/.memax/daemon/warm.json` (per linked space: each compiled file's latest compile and its refs, the last 30 days' tombstones, waiting gates; refs only, plus a waiting gate's shortened question), the compiled files on disk, and what that agent's last session in that repository was told (`~/.memax/daemon/seen/`, hashes and refs). It prints a `<memax-context>` block with **only what changed** since then (new, changed and gone cited lines; never the compiled file, which the agent already loads), forgets, gates and compiles not on disk yet, and nothing when nothing changed; under 3,000 tokens and 9,000 characters (Codex 2,400), as plain text (Claude Code, Codex) or each agent's JSON (Gemini CLI, Cursor, Copilot CLI). Lines whose cites aren't in the server's compile (a hand-added cite) or name a forgotten memory are never printed. After printing it queues one compile load per compiled file in `~/.memax/daemon/loads/`; the daemon (`lib/daemon/loads.ts`, watching that directory) reports them to `POST /v2/spaces/{space}/compile-loads`, or a detached `memax hook flush` does when no daemon runs (at most one per 30 s), so the hook never opens a socket. Budget: under 100 ms from process start to exit (over 50 cold runs on a 4-core arm64 machine, p50 about 50 ms and p95 60–70 ms, against p50 30 ms for `node -e ''`). `memax connect <agent>` (`src/commands/connect.ts`, flow in `src/lib/connect/`) writes the agent's MCP settings (init's writers), installs the hook in the agent's own settings (`lib/connect/hooks.ts`: Claude Code, Codex, Gemini CLI, Cursor, Copilot CLI; OpenCode, Windsurf and ChatGPT have none), connects an existing connection to the repository's space (never raised from the CLI; a new one connects on the agent's first OAuth sign-in) and compiles once in a linked repository; running it again changes nothing. The Claude Code plugin is `plugins/memax` (remote MCP, the hook, the `memax` skill), and `.claude-plugin/marketplace.json` makes this repository its marketplace.
- **Device sign-in (migration 045, RFC 8628).** Where no browser can open (SSH, no display, CI) or with `--device`, `memax login` and `memax init` sign in with a device code (`packages/cli/src/lib/device-login.ts`, over `memax.auth.startDeviceSignIn`/`pollDeviceSignIn`). The CLI posts `client_id=memax-cli` and what it says about itself to `POST /oauth/device_authorization` and polls `POST /oauth/token` with the `urn:ietf:params:oauth:grant-type:device_code` grant (`internal/handler/oauth_device.go`, beside the MCP OAuth server; the metadata advertises both); a person confirms or declines the code at `/device` through `/v2/device-authorizations:lookup`, `:approve` and `:deny`, and only a person on the web app may (`policy.DecideDevice`: `device_by_person`, `device_needs_web`). `internal/deviceauth` keeps the codes hashed (the device code's SHA-256, the user code's HMAC keyed by `JWT_SECRET`), 10 minutes, decided once, one session per code, `slow_down` adding 5 s; new codes are limited per address (5/min in the route limiter, 20/hour in the table) and lookups of codes that don't exist per person and address. The session issued is the CLI's (surface `cli`), so it is never `human_web`.

**UI (Ledger)**

- **Where it lives.** V2 UI lives in `packages/web/src/app/(ledger)` and is built only from `packages/ledger` (components, `mx-` classes) and `packages/ledger-tokens` (tokens, fonts; Apache-2.0).
- **Visual rules.**
  - Every colour is a token (`var(--seal)`), never a literal.
  - No Tailwind, glass, blur, gradients, sparkles or spinners in the `(ledger)` tree.
  - Base UI (`@base-ui/react`) is allowed only as unstyled behaviour under `mx-` components.
  - Fonts: Newsreader for memory text, Schibsted Grotesk for UI, IBM Plex Mono for receipts and IDs only, Gloock for the wordmark only.
- **Copy.** Sentence case. No "AI", "magic", "smart", "delete", exclamation marks or emoji. The verbs are Keep, Edit, Reject, Forget, Remember, Review, Hand off, Compile and Verify. Every string goes through i18n, with `en` and `zh` both required.
- **Spec.** The visual spec is the V2 handoff in `memax-internal/docs/v2/handoff/`: PNGs are the target, and the `.dc.html` files are the exact spec. Hardening notes are in `docs/v2/design-review.md`.

**Local database without Docker.** Any Postgres 17 with pgvector, pg_trgm, pgcrypto and unaccent works. Point `TEST_DATABASE_URL` at a role that can `CREATE DATABASE`; `internal/testdb` clones a migrated template per test.

## Working in This Repo

- This is a Turborepo monorepo. Changes often span multiple packages.
- The SDK (`packages/sdk`) and CLI (`packages/cli`) now live **in this repo** alongside `server`, `web`, `ui`, and `docs-site` — they are no longer a separate checkout. The web app consumes the SDK via `workspace:*`, so SDK changes are picked up locally with no npm round-trip. The SDK and CLI are still published to npm (`memax-sdk`, `memax-cli`) from this repo via the npm publish workflows; bump versions in their `package.json` when cutting a release.
- The Memax Agent SDK (Go) lives in the public [`MemaxLabs/memax-go-agent-sdk`](https://github.com/MemaxLabs/memax-go-agent-sdk) repo and is checked out under `.refs/memax-go-agent-sdk/`. This is the autonomous-agent runtime that powers Lucid Dream and (in progress) Agent Chat — see `docs/plans/24-agent-runtime-lucid-and-chat.md`. The Go server imports it via `go get github.com/MemaxLabs/memax-go-agent-sdk` like any other Go module; the local checkout exists so agents can read SDK source, run its tests, and stage cross-repo changes when needed. Agent SDK changes should be made in `.refs/memax-go-agent-sdk/`, pushed to the public repo, tagged, and then consumed here by bumping the dependency in `packages/server/go.mod`.
- The devcontainer post-create flow prepares the agent-SDK reference checkout:
  - `.refs/memax-go-agent-sdk/` via `setup-agent-sdk-ref.sh` — clone-and-pull only (no build, no symlink); the Go module resolves through `go.mod` like any other dependency.
- When modifying `@memaxlabs/ui`, verify `packages/web/` still renders correctly. (`packages/docs-site` no longer depends on `@memaxlabs/ui` — it inlines its own `MemaxLogo` so the Apache-licensed docs site does not link the AGPL `ui` package.)
- Prefer editing existing files over creating new ones. Follow the established patterns in each package.
- `memax-sdk` is the canonical TypeScript client for Memax backend `/v1/*` routes. New product API calls in `packages/web/` should go through the SDK rather than raw `fetch` or duplicated route helpers. Direct object-store transfers are separate and do not count as backend `/v1/*` calls.
- **Licensing is per-package** (this repo mixes licenses — see root `LICENSE`): `cli`, `sdk`, `docs-site` are Apache-2.0; `server`, `ui`, `web` are AGPL-3.0. Do not introduce a dependency from an Apache-2.0 package onto an AGPL-3.0 package.
- The public CLI has two distinct ingest surfaces:
  - `memax import <dir>` — one-way directory → memory ingest (with `memax import status` for source/history)
  - `memax agents ...` — agent config sync (configs only — session sync was removed in migration 008; agent CLIs change session formats too often):
    - `memax agents sync` — device-aware config sync (canonical; `memax agents configs sync` is the same command)
    - `memax agents configs ...` — recovery helpers: `configs deleted`, `configs restore`, plus `list`/`doctor`
    - Config files are classified by role — `identity` (SOUL.md, persona files), `memory` (MEMORY.md, memory/\*.md), `rules` (.cursorrules, CLAUDE.md), `settings` (json/yaml, never synced: secrets risk). The classifier lives in `memax-sdk` (`classifyAgentConfigFile`) for TS surfaces; the Go extraction policy mirrors the identity patterns in `config_extract_policy.go` — keep both in sync.

## Claude Code Hook Integration

- Memax integrates with Claude Code through the plugin in `plugins/memax` (remote MCP, the SessionStart hook, the `memax` skill) or `memax connect claude-code`; both run `memax hook session-start` (see "Warm start and `memax connect`" in the V2 section, and plan 25 §7.5)
- When working on the hook (`packages/cli/src/hook-main.ts`, `src/lib/hook/`), test with a real Claude Code installation: `claude plugin validate --strict plugins/memax` and `claude plugin validate --strict .`, then `claude -p … --plugin-dir plugins/memax --output-format stream-json --include-hook-events` in a scratch repository shows the hook's output and the skill loading
- Hook latency budget: the session-start hook must exit within 100 ms of starting and never touch the network; Claude Code's whole hook budget is `<500ms`. Keep `hook-main.ts`'s imports to Node built-ins (no `node:crypto`, no SDK) and measure p50/p95 over cold runs after changing it
- Context injection uses `<memax-context>` tags, stays under 3000 tokens (and Claude Code's 10,000-character stdout cap), and says only what changed: a hook that prints AGENTS.md gives the agent a second copy
- Behavioural guidance (recall first, propose, respect Review) belongs in the plugin's skill, never in MCP tool descriptions

## Monorepo Structure

```
memax/
  .refs/
    memax-go-agent-sdk/     # gitignored checkout of public MemaxLabs/memax-go-agent-sdk (Go agent runtime)
  packages/
    server/          # Go API server (stdlib net/http) + background worker (River queue) — AGPL-3.0
                     #   cmd/server/  — HTTP API (insert-only queue client)
                     #   cmd/worker/  — River job processor (memory processing, dreams)
    web/             # Next.js 16 web app (memax.app) — AGPL-3.0
    ui/              # @memaxlabs/ui shared design system (Tailwind + Radix) — AGPL-3.0
    docs-site/       # Fumadocs developer hub (docs.memax.app) — Apache-2.0
    sdk/             # memax-sdk — TypeScript client, published to npm — Apache-2.0
    cli/             # memax-cli — Commander.js CLI and the local daemon (init, connect, link, daemon, status, compile, export, the session-start hook), published to npm — Apache-2.0
    ledger-tokens/   # V2 Ledger tokens, type styles, fonts, marks (@memaxlabs/ledger-tokens) — Apache-2.0
    ledger/          # V2 Ledger React components, mx- styles, en/zh strings, previews (@memaxlabs/ledger) — AGPL-3.0
    compiler/        # V2 compiler: kept record → AGENTS.md, CLAUDE.md shim, scoped rules; parse-back (@memaxlabs/compiler) — Apache-2.0
    compile-service/ # V2 compile service: the compiler over HTTP, a Cloudflare Worker or node:http (@memaxlabs/compile-service) — AGPL-3.0
  plugins/
    memax/           # Claude Code plugin: remote MCP, the SessionStart hook, the memax skill — Apache-2.0
  .claude-plugin/
    marketplace.json # makes this repository the plugin's marketplace (`claude plugin marketplace add MemaxLabs/memax`)

# Design docs (docs/plans, docs/infra, docs/design, ...) live in the sibling
# private repo MemaxLabs/memax-internal — clone alongside this repo.
```

## Tech Stack

| Component                             | Technology                                                                |
| ------------------------------------- | ------------------------------------------------------------------------- |
| CLI                                   | TypeScript, Commander.js, chalk (`packages/cli`)                          |
| SDK                                   | TypeScript (`memax-sdk`, `packages/sdk`)                                  |
| API Server (includes retrieval)       | Go (stdlib net/http)                                                      |
| Web App                               | Next.js 16 (App Router), Tailwind, Radix UI, TanStack Query, Tiptap, cmdk |
| Developer Hub                         | Fumadocs (Next.js, static export), Pagefind, Scalar                       |
| Design System                         | @memaxlabs/ui — Tailwind + Radix primitives                               |
| Database                              | PostgreSQL (Neon) + pgvector                                              |
| Cache                                 | Redis (Upstash)                                                           |
| Object Storage                        | Cloudflare R2                                                             |
| Embeddings                            | Voyage AI                                                                 |
| Reranking                             | Cohere Rerank                                                             |
| LLM (distillation + classification)   | DeepSeek V4 Flash via OpenRouter (Anthropic-compatible Messages API)      |
| LLM (answer synthesis + agent/dreams) | DeepSeek V4.1 Flash via OpenRouter (Anthropic-compatible Messages API)    |
| Queue                                 | River (Postgres-backed, Go)                                               |
| Compile service (V2)                  | `@memaxlabs/compiler`: a Cloudflare Worker, or `node:http` on Node 24     |
| Auth                                  | OAuth2 (GitHub/Google)                                                    |
| Deployment                            | Fly.io (API, worker); Cloudflare Workers (web, docs, compile service)     |
| CI/CD                                 | GitHub Actions                                                            |
| Package Manager                       | pnpm (workspaces)                                                         |
| Monorepo                              | Turborepo                                                                 |

## Code Conventions

### Format and Lint Before Every Commit (CRITICAL)

**Run `pnpm format && pnpm lint` before EVERY `git commit`.** No exceptions, no "I'll fix it later."

```bash
pnpm format && pnpm lint   # MUST pass before git commit
```

- `pnpm format` runs Prettier on all `*.{ts,tsx,js,jsx,json,md}` files
- `pnpm lint` runs ESLint, `tsc --noEmit` (TypeScript), and `go vet` (Go)
- CI rejects unformatted code AND any lint warning — if you skip this, the push is wasted
- This applies to ALL agents (Claude, Gemini, Copilot, Codex) — there are no git hooks, so **you** are the hook

**Warnings are regressions.** `packages/web` runs ESLint with `--max-warnings 0` — any warning fails lint. Don't `// eslint-disable-line` past a warning unless you can justify it in a comment; root-cause it instead. The common `react-hooks/exhaustive-deps` fix patterns (useMemo-wrap `?? []` fallbacks, add stable-setter deps, useCallback + reorder decls to avoid TDZ, capture refs at effect-open for cleanup, extract complex dep expressions to a variable, align a dep with the variable the body actually reads) are well-tread — see commit `338d53a2` for worked examples.

**Why this is CRITICAL:** We have no pre-commit hooks by design (bad DX). That means every agent is responsible for formatting and linting its own changes. Forgetting to format, or letting warnings accumulate, has caused repeated CI failures and silent tech-debt buildup. Run the command. Every time.

### General

- Use the language/framework conventions of each package (Go conventions for server, TypeScript/React conventions for web, etc.)
- Prefer small, focused functions over large monolithic ones
- No premature abstractions — three similar lines is better than an unnecessary helper
- Write tests alongside features, not as an afterthought
- Error messages should be actionable — tell the user what to do, not just what went wrong

### Commit and PR Conventions

- Commit messages: imperative mood, concise (`Add recall endpoint`, `Fix boundary check in hub query`)
- Prefix with package scope when change is localized: `cli: add sync status command`, `server: fix auth token refresh`
- PRs should be focused — one feature or fix per PR, not kitchen-sink bundles
- Include a test plan in PR descriptions

### Testing

- CLI: unit tests with Vitest, integration tests against a local API
- Server (Go): table-driven tests, use testcontainers for database tests
- Web: React Testing Library for components, Playwright for E2E
- Retrieval (Go, in server): table-driven tests, mock embedder interface
- Always test boundary enforcement — verify that private memories are not accessible cross-user

### API Contract Changes (CRITICAL)

**Never change a server API response format without updating ALL consumers.** This is non-negotiable.

**The rule:** If you modify a handler's response shape (add fields, change from array to object, rename keys, change pagination format), you MUST update every consumer in the same commit:

1. **Web app** (`packages/web/src/hooks/`) — React Query hooks that call the endpoint
2. **SDK** (`packages/sdk/src/`) — TypeScript SDK methods
3. **CLI** (`packages/cli/src/`) — CLI commands that call the endpoint
4. **MCP server** (`packages/server/internal/handler/mcp.go` + `packages/cli/src/commands/mcp.ts`) — both local and remote MCP tools
5. **Eval tests** (`packages/server/eval/`) — if the endpoint is tested

**Why this exists:** We changed `GET /v1/memories` from returning `Memory[]` to `{ memories, next_cursor, has_more }` but only updated the server — the web app, CLI, SDK, and MCP all broke silently.

**How to verify:** Before committing any handler change:

1. Grep for the endpoint path across all packages: `grep -r "/v1/memories" packages/`
2. Check every file that calls it — does it handle the new response format?
3. If you add pagination, sorting, or filtering to an endpoint, update ALL clients to support it (even if they don't use the new params yet, they must handle the new response shape).

### Admin Surface Boundary (CRITICAL)

**Admin endpoints are internal operator tools. They live in `packages/web/src/lib/admin-client/` and NEVER ship in the public `memax-sdk` on npm.**

**The rules:**

- Server-side, admin routes live under `/v1/admin/*`. Fine.
- **Web-side**, every admin call goes through `@/lib/admin-client` (web-only, not published).
- **Never** import `Admin*` types from `memax-sdk`. Never add an `admin.*` resource to the public SDK.
- Never add an `/v1/admin/*` URL string anywhere in the public SDK.
- If web code needs a new admin endpoint, add it to `packages/web/src/lib/admin-client/client.ts` + `types.ts` — same file pattern as existing methods. Call via `adminClient.yourMethod(...)` or a hook in `packages/web/src/hooks/use-admin-*`.

**Why:** admin uses a different auth model (JWT session + `admin_roles` table), is operator-only, and exposing it in a published SDK would document the admin API surface to every npm consumer. It also puts internal-only types and internal-only breaking-change risk on the public package.

**Why this exists:** In commit `e7305d31` (2026-04-15) we accidentally moved admin endpoints INTO the SDK as part of a "use the SDK everywhere" refactor, framed as a "SDK boundary" fix. That was exactly the wrong direction. Days later we had to extract 30+ methods and 40+ types back out. `scripts/check-sdk-boundary.mjs` now enforces the internal side of this rule by blocking non-admin web code from adding raw `/v1/*` calls that bypass `memax-sdk`.

If you find yourself tempted to put admin code in the SDK, stop and ask why. The answer is always: it belongs in `packages/web/src/lib/admin-client/`.

### MCP Tool Parity (CRITICAL)

**The CLI MCP server and Go server MCP handler must expose identical tools.** Both implementations serve the same purpose (giving AI agents access to Memax), and agents should get the same capabilities regardless of which MCP endpoint they connect to.

**The two catalogues:** `packages/server/internal/handler/mcp_tools.json` (Go, remote; served by `mcp.go` on the official go-sdk, both profiles) and `packages/cli/src/commands/mcp-tools.ts` (TypeScript, local; served by `mcp.ts`). Each holds every tool's name, title, description, input schema, output schema and annotations, plus the server instructions.

**The rule:** When adding or modifying an MCP tool, update BOTH catalogues in the same commit. `node scripts/check-mcp-parity.mjs` (part of `pnpm lint`) compares them field by field and fails on any difference. Descriptions say what a tool does, never how an agent should behave ("ALWAYS call …" fails directory review; that guidance belongs in the Claude Code plugin's skill and hooks). Current tools (11): the 10 V1 tools `memax_recall`, `memax_push`, `memax_get`, `memax_list`, `memax_hubs`, `memax_hub_members`, `memax_forget`, `memax_capture`, `memax_topics`, `memax_request_decision`, plus `memax_search`. The ChatGPT profile (`/mcp/chatgpt`, remote only) has 7 aliases that map onto the same handlers.

**V1 and V2 per space.** A space whose `hubs.v2_enabled_at` is set (`internal/spacemode`) is served through the ledger by `internal/mcpv2`; every other space keeps the V1 tools exactly (`TestV1SpacesBehaveExactlyAsV1` holds them byte-for-byte equal). An unscoped recall or search runs V1's pipeline beside the ledger only while a reachable space is still on V1, so V1's round trips, recall metering and activity event go only to V1 spaces (`TestV1PipelineOnlyWithAV1Space`). Switching a space (`:switch`, `memax switch`, `go run ./cmd/v2-switch-space -space <uuid>`) moves it between the two, and back exactly (`TestSwitchingToV2AndBackOverMCP`).

**Why this exists:** We added `memax_topics` and `hint`/`project_context` params to the Go server MCP but forgot the CLI MCP. Agents connecting locally via `memax mcp serve` got different (fewer) capabilities than agents connecting to the remote server.

### Response Envelope (CRITICAL)

**All REST API responses MUST use `model.ApiResponse{Data: ...}`.** The `writeJSON` helper enforces this at compile time — it only accepts `model.ApiResponse`, not `any`.

**Why this exists:** We shipped a config sync endpoint that returned raw JSON (`{actions: [...]}`). The CLI's API client unwraps `response.data` from every response, so it got `undefined` and crashed. The fix: `writeJSON` now has signature `func writeJSON(w, status, model.ApiResponse)` — passing raw data is a compile error.

**The rules:**

- Success: `writeJSON(w, status, model.ApiResponse{Data: yourPayload})`
- Error: `writeError(w, status, "error_code", "message")` (wraps automatically)
- Never use `json.NewEncoder(w).Encode(...)` for REST endpoints — only for non-REST protocols (MCP JSON-RPC, OAuth)

### TypeScript (CLI, SDK, Web, UI)

- Strict TypeScript (`strict: true`) — no `any` unless unavoidable
- Use ESM imports (not CommonJS)
- Prefer `interface` over `type` for object shapes
- Use named exports (not default exports)
- Format with Prettier, lint with ESLint

### Go (API Server)

- Follow standard Go project layout
- Use `context.Context` for all request-scoped operations
- Structured logging (slog or zerolog)
- Table-driven tests
- Handle all errors explicitly — no silent swallowing
- **Shared infrastructure modules** — never duplicate cross-cutting concerns. If two modules need the same capability, extract a shared package:
  - **LLM calls:** Use `internal/anthropic.Client` for ALL Anthropic API calls. Never write raw HTTP calls to the Anthropic API — the shared client handles auth, headers, error handling, timeouts, and response parsing. Each module receives the client via dependency injection.
  - **Embeddings:** Use `internal/ingest/embed.Embedder` — never call Voyage AI directly.
  - **Chunking:** Use `internal/ingest/chunker` — never hand-split markdown.
- **Dependency injection over global state** — modules accept dependencies in their constructor (`New(client *anthropic.Client)`), not by reading env vars inside methods. Env vars are read once at startup in `cmd/server/main.go` and `cmd/worker/main.go`.
- **Nil means disabled** — if a dependency is nil (e.g., no API key set), the module's `New()` returns nil. Callers check for nil before using. This provides graceful degradation without feature flags.

### CSS / Styling & Design Language

> **V1 only.** These rules apply to the frozen V1 web app. V2 UI follows the Ledger rules in the "V2 (branch `v2`)" section above.

- **Read `docs/design/design-system.md` before writing ANY frontend code.** The Memax design language is specific and intentional — not generic shadcn.
- Uses `@base-ui/react` primitives (NOT Radix), Tailwind CSS 4.0, oklch color tokens
- **Liquid glass surfaces** — use `glass`, `glass-subtle`, `glass-strong` instead of flat `bg-card`
- **Colored shadows** — use `shadow-glow`, `shadow-premium` instead of Tailwind's generic `shadow-md`
- **Spring easing** — use `var(--ease-spring)` for all transitions, never `ease-in-out`
- **Display typography** — headings use `text-display-*` classes (Inter, tight letter-spacing); code blocks use JetBrains Mono. Both load via `next/font/google` in `app/(v1)/v1-fonts.ts`.
- **No sidebar layout** — the app uses a floating dock at bottom center. No sidebar. No top bar.
- **Centered modals** — all overlays (search, capture) are centered glass panels, not Sheet/sidebar drawers
- **Entrance animations** — every section uses `animate-fade-up` with `stagger-1` through `stagger-5`
- Dark mode is first-class — test both themes. All custom CSS classes have dark variants.

## Architecture Principles

1. **Private by default** — all memories are private unless explicitly shared
2. **Boundaries are enforced at the data layer** — not application logic (PostgreSQL RLS)
3. **Retrieval precision over recall** — returning irrelevant context is worse than returning nothing
4. **Graceful degradation** — if a service is slow/down, never block the user's agent
5. **Idempotent operations** — content-hash based dedup means repeated pushes are safe
6. **Agent-agnostic** — never assume a specific agent; design for CLI piping as the universal fallback

## Unified Agent Skills

This repository uses a unified "skills" system for all AI agents (Claude, Gemini, Copilot, etc.).

- **Location:** `.agents/skills/`
- **Mandate:** Before performing specialized tasks, ALL agents must check for relevant guidance in `.agents/skills/`.
- **Available skills:**
  - `cli/` — CLI architecture, command patterns, UX conventions, quality standards. **Read before any work in `packages/cli/`.**
  - `server/` — Go backend architecture, handler/store/queue patterns, security rules. **Read before any work in `packages/server/`.**
  - `local-dev-debug/` — local Postgres/Redis/debugging workflows, dirty migration recovery, direct `psql` and `redis-cli` inspection. **Use for `pnpm dev` failures, dirty migrations, and local infra drift.**
  - `design/` — UI design thinking, checklist, implementation rules, anti-patterns
  - `i18n/` — translations, brand voice, no hardcoded strings
  - `ui-feature/` — `/ui-feature`: new frontend feature workflow (strategy → structure check → design → i18n → implement)
  - `eval/` — retrieval eval: run locally before pushing retrieval/ingestion changes. Covers corpus structure, graded metrics, thresholds, and how to investigate failures. **Read before any work touching recall, ask, ingest, store chunks, or scoring.**
  - `ui-fix/` — `/ui-fix`: frontend bug fix workflow (root cause → structural check → fix → prevention)
  - `ui-polish/` — `/ui-polish`: modify/polish existing flows (impact check → cross-cutting consistency → implement)
  - `business/` — business document quality: research standards, internal consistency checks, pricing/projection validation. **Read before any work on business docs (now in the sibling `MemaxLabs/internal-docs` private repo).**
  - `codex-review/` — run Codex CLI code reviews: brief Codex, launch `codex exec`, monitor, parse findings, resume sessions for re-review. **Use when the user asks for a Codex review or second opinion.**
  - `skill-creator/` — create new skills, improve existing ones, run evals, benchmark performance, optimize trigger descriptions. From [anthropics/skills](https://github.com/anthropics/skills).
- **Key rule:** Every user-facing string in the web app must go through the i18n system (`t.*` from `useLocale()`). See `i18n/SKILL.md`.

## Security Rules

### Owner Isolation (CRITICAL)

Every database query that touches user data MUST filter by `owner_id`. This is non-negotiable.

**The rule:** If a Store method reads or deletes memories/chunks, it MUST accept an `ownerID` parameter and include `WHERE owner_id = $N` in the query. No exceptions, no "we'll add it later."

**Why this exists:** We shipped a bug where all users could see all other users' memories. The root cause was Store methods that didn't filter by owner. Application-level checks are not enough — a single missed call leaks all data.

**How to verify:** Before merging any PR that touches the Store interface or handler code:

1. Grep for every `h.store.` call in the handler — does each one pass `ownerID`?
2. Check the SQL — does every `SELECT`, `DELETE` on `memories` have `AND owner_id = $N`?
3. `UpdateMemory` is safe (updates by ID, owner set at creation) but verify the caller checked ownership first via `GetMemory(id, ownerID)`.

**Future: PostgreSQL RLS.** The long-term fix is Row-Level Security policies on the `memories` and `chunks` tables, so the database enforces isolation regardless of application bugs. This is tracked for Phase 3 (team features) when we need proper multi-tenant access control anyway. Until then, application-level `owner_id` filtering is the defense.

### General Security Rules

- **Never store secrets in code** — use environment variables, reference `.env.example`
- **Never skip boundary checks** — every memory access must verify the requester's access level
- **Run secret detection on push** — scan incoming content for API keys, tokens, passwords
- **Encrypt at rest and in transit** — TLS 1.3, TDE in PostgreSQL
- **Audit everything** — log all memory access with actor, action, resource, context
- **Short-lived tokens** — access tokens expire in 1 hour, refresh tokens in 30 days

## Build & Run

Commands will be documented here as packages are scaffolded. The monorepo uses Turborepo + pnpm workspaces:

```bash
# Install dependencies (from root — uses pnpm workspaces)
pnpm install

# Run all packages in dev mode
pnpm dev

# Build all packages
pnpm build

# Run tests
pnpm test

# Lint
pnpm lint
```

Package-specific commands are in each package's README.

### Web: V1 and the V2 Ledger UI side by side

`packages/web/src/app` has two root layouts: `(v1)/` is the frozen V1 app (every V1 route group, `globals.css`, Tailwind) and `(ledger)/` is the V2 Ledger UI (only `@memaxlabs/ledger-tokens` and `@memaxlabs/ledger`). `api/`, `dev/ui/`, `global-error.tsx` and `global-not-found.tsx` sit outside both. Moving between the trees is a full page load, and `(ledger)/isolation.test.ts` keeps V1 styles out of `(ledger)`.

- **V2 routes** are defined once, in `src/lib/ui-gate.ts` (plan §6.3): `/[space]/<place>/…` (today, review, brief, memories, handoffs, agents, decisions, dream, activity, search, settings), `/setup/…`, `/signin`, `/device`, `/unsubscribe` (these three open for every browser), `/settings/…` (V1 keeps the bare `/settings`), `/join/…` and the `/dev/ledger…` fixtures. Every new top-level route must be added to `RESERVED_SPACE_SLUGS`, or a test fails.
- **`memax_ui` cookie.** `memax_ui=v2` opts a browser into V2. Without it, `src/proxy.ts` redirects V2 paths to the V1 home. In dev, `/dev/ui?v=2` sets it and opens `/dev/ledger/tokens`, and `/dev/ui?v=1` clears it; both take `&next=/path` and are a 404 in production unless `NEXT_PUBLIC_DEV_FIXTURES=1`.
- **Theme.** `memax_theme=light|dark` (no cookie means follow the system) becomes `data-theme` on `<html>` before first paint.
- **App frame.** `(ledger)/(app)/` wraps every place (`/[space]/…`) and `/settings/…` in Ledger's `Shell`: the rail, the space switcher, ⌘K, the `?` sheet and toasts. Place pages are in `(app)/_places/`.
- **Data.** The frame reads one interface, `src/lib/v2/data/source.ts`, with two sources: `sdk-source.ts` (`memax.v2`, for a browser with a session) and `demo-source.ts` (the handoff's demo dataset, for dev fixtures and Playwright). `(app)/layout.tsx` picks one per request (`lib/v2/data/mode.ts`). Without a session and with dev fixtures on, you get the demo; `memax_v2_data=demo` forces it when signed in. What `/v2` doesn't serve yet is a `PLACEHOLDER` in the SDK source, never demo data.
- **The session (both trees).** The web app's server holds it (`src/lib/bff`): `/api/auth/exchange` trades a sign-in's one-time code for `__Host-memax_session`/`__Host-memax_refresh` cookies (HttpOnly, Secure, `SameSite=Strict`; plain names over http on localhost), `/api/auth/me` answers the profile and `session.surface`, `/api/auth/logout` revokes and clears, `/api/auth/impersonate` swaps an operator in and out. `/api/proxy` attaches the cookie's token, signs `/v2` for web sessions, refreshes single-flight, refuses the API's token endpoints and cross-site requests (`csrfRefusal`). Browser code calls `/api/proxy` (SDK, `lib/api.ts`, the admin client) with no `Authorization` header; a 401 means the session is over. The readable `memax_session_presence` marker (Lax, no secret) routes the middleware and layouts. D15 screens read `useAuth().session`, never a token.
- **Records (Review, Memories, a memory's page).** Each domain has its own module on the interface (`data/review.ts` as `source.review`, `data/memories.ts` as `source.memories`), with `sdk-review.ts`/`sdk-memories.ts` and the demo's session store (`demo-records.ts`, `demo-memories.ts`). Commands take one idempotency key per user action, reused across retries (`lib/v2/intent-keys.ts`), and errors normalise to `CommandFailure` (`data/command-error.ts`), worded by policy code in `lib/v2/records-copy.ts`.
- **The Brief, targets and Today.** `data/brief.ts` (`source.brief`: the current version as the page shows it, built by the compiler's placement rules in `brief-view.ts`; versions; revise with If-Match), `data/targets.ts` (`source.targets`: list, preview, drift, Compile now, settings, pull/overwrite/stop; `targetStatus` words D2/D3, so ChatGPT is "live over connector" and a Cursor with nothing scoped "reads AGENTS.md"), and `data/today.ts` (`source.today`, whose Dream card comes from `data/dream.ts`, `source.dream`: editions, undo, restore, run now and the settings; the demo's is edition No. 214 of DreamEdition.png). The rail's status line comes from the targets (`syncLineOf`). The demo's compiled files in `data/demo-compiled.ts` are `@memaxlabs/compiler`'s own output for the demo Brief; regenerate them when the demo record changes.
- **Settings.** `/settings/{account,keys,notifications,security}` share `settings-frame.tsx`'s nav (the boards' items, plus Security after Notifications; Spaces, Plan and usage, Integrations and Export are drawn, not built). Notifications (Notifications.png) and Security read `data/settings.ts` (`source.settings`: `memax.v2.settings`, the demo's in `settings-demo.ts`, whose morning email is the demo Dream's setting), worded by `lib/v2/settings-copy.ts`; company and model names are `NAMES` there, not the catalogue. Email changes go from the version last read and queue one after another (`_lib/settings.tsx`); a clash reloads. Security's seals come from each space's checkpoints (`source.seal`, with the signing key) and its agents from `useMyAgents`.
- **Spaces still on V1.** The switcher lists them (`onV2: false`; the demo's is `acme-web`), and their Today is Switch to V2 (`_places/today/switch-to-v2.tsx`, `source.switch` in `data/switch.ts`, words from `switch-copy.ts`): what moves, the switch for the owner, then Review's "From V1" (`origin: v1` imports in ReviewImport). The connect dialog offers only spaces on V2.
- **Keyboard.** Every binding is declared once in `src/lib/v2/keymap/registry.ts`, and the `?` sheet is generated from it. Screens handle a binding with `useHotkey(id, …)`; never add a `window` key listener. Forget has no key.
- **The first session.** SignIn (`/signin`, `/signin/callback`), CliAuth (`/device`) and the setup screens (`/setup/{agents,import,cleanup,done}`: Connect, FirstRun, Cleanup, CompileDone) live in `(ledger)/(auth)` and `(ledger)/(setup)`, built in `(ledger)/_onboarding` on an `OnboardingFrame` (the app frame's data source, keymap and toasts, no rail); ReviewImport is Review's `?filter=import`. They read two more domain modules, `data/imports.ts` (`source.imports`) and `data/devices.ts` (`source.devices`). Sign-in always sends the one-time code to this app's origin (email included), so the session is the web's. After sign-in (`lib/v2/onboarding/routes.ts`): `next`, else FirstRun for someone with no space on the V2 record, ReviewImport for an import still in progress (two weeks), else a project space's Today. `/signin` and `/device` open without `memax_ui=v2` (the CLI sends anyone to `/device`); everything else stays behind it.
- **Specimen and gallery.** `/dev/ledger/tokens` shows every token and type style in Paper and Carbon. `/dev/ledger/components` mounts every `@memaxlabs/ledger` preview at its artboard size in both themes.

```bash
# Playwright (Chromium) against a production build that keeps the /dev fixtures.
# Not part of `pnpm test`. CI also needs the browser's system libraries (--with-deps, root).
pnpm --filter @memaxlabs/web exec playwright install --with-deps chromium
pnpm --filter @memaxlabs/web test:e2e
pnpm --filter @memaxlabs/web test:e2e:update      # rewrite this repo's screenshot baselines
E2E_BASE_URL=http://localhost:3100 pnpm --filter @memaxlabs/web test:e2e   # reuse a running server
# The "handoff" project compares the gallery with the private handoff PNGs, read in
# place from ../memax-internal (or MEMAX_INTERNAL_DIR); it skips without that checkout
# and never writes those PNGs. E2E_HANDOFF_MAX_RATIO=0 prints every preview's difference.
pnpm --filter @memaxlabs/web test:e2e --project=handoff
# The "boards" project measures the first session's screens against their 1x boards
# (screens/png), read in place the same way; E2E_BOARDS_MAX_RATIO=0 prints each difference.
pnpm --filter @memaxlabs/web test:e2e --project=boards
# The web session on the real API (e2e/auth-bff.e2e.ts, skipped without E2E_STACK=1): a
# fresh database, migrations, devseed and the Go API on E2E_API_PORT (18080; e2e/stack.ts),
# and the build pointed at it. Sign-in through the callback, HttpOnly cookies page script
# can't read, Review's Keep recorded as human_web, rotation, CSRF refusals, sign-out.
E2E_STACK=1 pnpm --filter @memaxlabs/web test:e2e --project=chromium auth-bff
```

**On Cloudflare Workers.** `@opennextjs/cloudflare` builds the same app into a Worker (`wrangler.jsonc`, `open-next.config.ts`); `next build`/`next start` (self-hosting) and the Vercel deploy (`vercel.json`) are unchanged. The app uses nothing Vercel-only: no `next/image`, `next/og`, ISR, `"use cache"` or edge runtime, so prerendered pages are served from the Worker's static assets (no R2, KV or queue). `proxy.ts` runs as Next 16's Node middleware, which OpenNext supports (and labels experimental). Settings:

- **Build time** (`next build` inlines them; CI reads them from Doppler per environment): `NEXT_PUBLIC_API_URL` (also where `/api/proxy` and `/api/auth/*` forward), `NEXT_PUBLIC_APP_URL`, `NEXT_PUBLIC_DOCS_URL`, `NEXT_PUBLIC_POSTHOG_KEY`, `NEXT_PUBLIC_POSTHOG_HOST`. `NEXT_PUBLIC_DEV_FIXTURES` is for test builds only; the deploy workflow refuses it.
- **Worker secret**: `WEB_SURFACE_SECRET`, read from `process.env` at runtime (`nodejs_compat` puts secrets there). Locally it goes in `packages/web/.dev.vars`. Never keep secrets in `packages/web/.env*`: OpenNext copies those files into the bundle.
- **The session on Workers**: the BFF's cookies, its refreshes (single-flight per isolate; racing isolates get the same next token from the API's grace window) and multiple `Set-Cookie` headers work the same under workerd as on `next start` and Vercel; the browser's address and city come from `CF-Connecting-IP`/`CF-IPCity` (Vercel: `X-Real-IP`/`X-Vercel-IP-City`).
- **Compatibility flags**: `nodejs_compat` (the proxy's `node:crypto` signing, `Buffer`, streams), `global_fetch_strictly_public`, and `enable_request_signal`, without which a browser that leaves an Ask (or V1's `/v1/events`) doesn't stop the stream on the API.

```bash
# Build the Worker (the SDK first) and run it under workerd, then Playwright against it
pnpm --filter memax-sdk build
NEXT_PUBLIC_DEV_FIXTURES=1 pnpm --filter @memaxlabs/web build:cf
pnpm --filter @memaxlabs/web preview:cf --port 8790 --ip 127.0.0.1
E2E_BASE_URL=http://localhost:8790 pnpm --filter @memaxlabs/web test:e2e
# Worker size: OPEN_NEXT_DEPLOY=true makes wrangler bundle instead of calling OpenNext
cd packages/web && OPEN_NEXT_DEPLOY=true pnpm exec wrangler deploy --dry-run --env production --outdir /tmp/web-worker
```

### Server-specific commands

```bash
# Build and run API server
cd packages/server && go run ./cmd/server/

# Build and run background worker (separate process)
cd packages/server && go run ./cmd/worker/

# Deploy API server to Fly.io (staging; swap to fly.server.production.toml for prod)
cd packages/server && fly deploy -c fly/fly.server.staging.toml

# Deploy the worker to Fly.io (production only; staging's API runs the worker in-process)
cd packages/server && fly deploy -c fly/fly.worker.production.toml

# Create a new migration with the correct next version
pnpm --filter @memaxlabs/server migrate:new <slug>

# Run the LoCoMo benchmark harness
cd packages/server && go run ./cmd/locomo/ -dataset eval/locomo/data/locomo10.json

# Judge eval (eval/judge/pairs.json, holdout.json): the sets, stage 0, the harness on a fake model
# and the candidate sets (fake embedder); JUDGE_EVAL_LIVE=1 also scores the real JUDGE_* tiers (needs
# ANTHROPIC_API_KEY; JUDGE_EVAL_TIERS=pipeline,primary,fallback,strong scores each alone, JUDGE_EVAL_PAIRS=10
# runs a small sample first), and V2_EVAL_LIVE=1 the candidates and floors on Voyage (needs VOYAGE_API_KEY).
# Live runs go through eval/livemeter, which checks zero-retention routing and reports latency and cost
cd packages/server && go test ./eval/judge/ -v

# Import cleanup eval (eval/imports/batches.json, holdout.json; plan 25 §11): labelled imports as memax init
# sends them, scored by group (every planted conflict found, precision, statements held out of bulk keep).
# Always: the sets, the scorer and the harness on a fake model (parsing, the bar, what cutting an import
# loses). IMPORT_EVAL_LIVE=1 also scores the worker's import tiers (JUDGE_MODEL, then JUDGE_FALLBACK_MODEL;
# needs ANTHROPIC_API_KEY, and ANTHROPIC_BASE_URL for OpenRouter) through eval/livemeter, with a sweep of
# ImportConflictBar; IMPORT_EVAL_SET=holdout.json checks a change on the held-out set, IMPORT_EVAL_TIERS=fallback
# scores the fallback alone, IMPORT_EVAL_ONLY=<ids> runs some imports first, IMPORT_EVAL_DEBUG=1 prints refused answers
cd packages/server && go test ./eval/imports/ -v
cd packages/server && IMPORT_EVAL_LIVE=1 go test ./eval/imports/ -run Live -v

# V2 embeddings and hybrid retrieval: indexing, the vector lane, fusion, rerank and deadline
# fallbacks, the judge's vectors, and the near-duplicate check (mock embedders, real Postgres)
cd packages/server && go test ./internal/v2index/ ./internal/v2recall/ ./internal/judge/ && go test ./internal/handler/v2api/ -run NearDuplicates
cd packages/server && go test ./internal/mcpv2/ -run 'RecallByMeaning|FallsBackLexically|HybridLatency' -v

# V2 retrieval eval: lexical vs hybrid on a fake embedder; V2_EVAL_LIVE=1 adds voyage-4/-lite and
# rerank-3-lite against V1's voyage-code-3, the recall floor sweep and the deadlines from your machine
# (needs VOYAGE_API_KEY). The model defaults ship only if this doesn't regress (it asserts that)
cd packages/server && go test ./eval/v2/ -v

# V2 Ask: the service (citation filter, tier config, the shared client's stream) and the
# endpoint through the spec (SSE events, isolation, quarantine, the plan limit, disconnects,
# first token under 1.5 s on a fake model, keeping an answer)
cd packages/server && go test ./internal/ask/ && go test ./internal/handler/v2api/ -run 'Ask|KeepAnAnswer' -v

# Ask eval (eval/ask/corpus.json): citation validity, "not covered", superseded, quarantined
# and other-space exclusion on a fake model; ASK_EVAL_LIVE=1 also scores the real ASK_MODEL
# tier (needs ANTHROPIC_API_KEY, and ANTHROPIC_BASE_URL for OpenRouter), with V2_EVAL_LIVE=1 on
# hybrid search too, and checks every stream's zero-retention routing
cd packages/server && go test ./eval/ask/ -v

# Connect V1 API keys and OAuth grants to the V2 record as agent connections, at Propose
# (idempotent; prefer -user for the people moving to V2)
cd packages/server && go run ./cmd/v2-backfill-agents -user <uuid>

# Switch a space to the V2 record as its owner, the whole switch in the process (-preview says
# what moves and changes nothing; -off switches back; -kind project for a V1 team hub)
cd packages/server && go run ./cmd/v2-switch-space -space <uuid> [-preview | -off] [-kind project] [-repository owner/name]

# Switch to V2 and notes: the steps, nothing lost (count and content hash), resume after a failure,
# rule 7 for a note (the grep), the endpoints through the spec, config sync off, MCP before, after
# and back, and Dream's next edition folding and proposing from the V1 notes (real Postgres)
cd packages/server && go test ./internal/ledger/ -run 'Switch|Note|ClassifyV1|RepoKey' && \
  go test ./internal/handler/v2api/ -run 'Switch|ForgetANote' && go test ./internal/mcpv2/ -run Switch && \
  go test ./internal/handler/ -run 'ConfigSyncIsOff|IntoASpaceOnV2' && go test ./internal/v2dream/ -run Switched

# MCP v2: protocol, OAuth, V2 tool and V1-compatibility tests (real Postgres)
cd packages/server && go test ./internal/handler/ -run 'MCP|ChatGPT' && go test ./internal/mcpv2/ ./internal/v2recall/ ./internal/spacemode/

# /v2 contract: run the spec, handler and parity tests
cd packages/server && go test ./internal/contract/ ./internal/handler/v2api/ ./internal/serverapp/

# /v2 contract: regenerate the SDK's types after changing openapi/v2.yaml
pnpm --filter memax-sdk gen:v2

# /v2 contract: check the committed SDK types match the spec (part of pnpm lint)
pnpm check:v2-types

# V2 compile path: ledger commands and the coordinator (fake compiler) and, with Node,
# the real compile service; then the Phase 1 gate end to end (real service, River, /v2)
cd packages/server && go test ./internal/ledger/ ./internal/compile/...
cd packages/server && go test ./internal/handler/v2api/ -run TestPhase1Gate -v

# Seed the memax-v2 demo space (refuses MEMAX_ENV=production)
cd packages/server && go run ./cmd/devseed

# Verify receipt hash chains on demand (exit 1 on any mismatch); -seal seals what waits
# first, -record stores the outcome on the chain head as the nightly job does
cd packages/server && go run ./cmd/v2-verify-receipts -space <uuid>   # or -all

# Reads (R-) and the receipt sealer: the recorder, the ledger's reads and seals, the
# chain encoding, and the sealer's race, exactly-once, redaction and tamper tests
cd packages/server && go test ./internal/reads/ ./internal/receiptchain/ ./internal/sealer/ && \
  go test ./internal/ledger/ -run 'Read|CompileLoad'

# Product metrics (the phase gates): the gate fixture's exact numbers, isolation (memax_v2 can't
# call or borrow them), a person built through real commands, the daily job's lines, the admin
# endpoint and the gate table's golden output
cd packages/server && go test ./internal/ledger/ -run 'ProductMetrics|SweepPolicies' && \
  go test ./internal/reads/ -run Maintain && go test ./internal/handler/ -run AdminV2Metrics && \
  go test ./cmd/v2-gate-metrics/   # -update rewrites testdata/fixture.golden

# Print the phase gates for a signup range (default: the last 8 weeks); -as-of judges the record as
# it stood then, -json prints the admin endpoint's shape. Reads only, as memax_v2_metrics
cd packages/server && go run ./cmd/v2-gate-metrics -from 2026-10-12 -to 2026-12-07

# Forget (rule 7): the ledger's purge, carries, requests, space and account forgets; the
# propagation job and the forget ledger's re-apply on a restored copy of the database;
# and, on the real stack (compile service, River, Redis), the grep of every text column,
# object and key after a Forget, a V1 space delete and a V1 account wipe
cd packages/server && go test ./internal/ledger/ -run 'Forget|Tombstone' && go test ./internal/forget/
MEMAX_REQUIRE_COMPILE_SERVICE=1 go test ./internal/handler/v2api/ -run 'Forget|V1SpaceDelete|V1AccountData' -v

# Device sign-in (RFC 8628): the codes and their store, the OAuth endpoints (each RFC error, single
# use, the cli surface) and the web's confirmation through the spec (human_web only, guessing waits)
cd packages/server && go test ./internal/deviceauth/ && go test ./internal/handler/ -run DeviceGrant && \
  go test ./internal/handler/v2api/ -run Device

# Re-apply the forget ledger after any database restore (reads object storage, S3_*;
# -dry-run lists the ops, -db-only reads the tombstones alone)
cd packages/server && go run ./cmd/v2-reapply-forgets -all

# Export (rule 14): the format's golden files, determinism, round trip, Forget and tamper
# (no database), then the ledger's export read and the endpoint on the real stack.
# -update rewrites the golden export the SDK's and CLI's tests read, after a deliberate change
cd packages/server && go test ./internal/export/ && go test ./internal/export/ -run 'TestGolden' -update
cd packages/server && go test ./internal/ledger/ -run Export && go test ./internal/handler/v2api/ -run Export -v

# V2 Dream: the ledger's editions, undo (rule 9, every action kind), receipts, RLS, trust,
# Forget during and after an edition, and the wire audit; the engine, schedule (time zones,
# catch-up, caps), sweep and morning email; the /v2 endpoints through the spec
cd packages/server && go test ./internal/ledger/ -run 'Dream|Fade' && go test ./internal/v2dream/... && \
  go test ./internal/handler/v2api/ -run Dream -v

# Settings (migration 050): notification settings (defaults, If-Match, replay, RLS on the
# person, the morning email shared with Dream's settings and its unsubscribe), quiet hours on
# the morning email, the security posture from configuration, and both endpoints through the spec
cd packages/server && go test ./internal/ledger/ -run 'Notification|QuietHours' && go test ./internal/trust/ && \
  go test ./internal/v2dream/ -run MorningEmail && \
  go test ./internal/handler/v2api/ -run 'NotificationSettings|MorningEmailIsDreamsSetting|TestSecurity' -v

# Dream eval (eval/dream/fixture.json): each phase's expected actions on a fake model;
# DREAM_EVAL_LIVE=1 also runs the real DREAM_* tiers (needs ANTHROPIC_API_KEY, and
# ANTHROPIC_BASE_URL for OpenRouter) through eval/livemeter (ZDR, latency, cost)
cd packages/server && go test ./eval/dream/ -v

# Round trips (internal/testdb/netsim): the scope-ordering audit on the wire and the
# round-trip budgets of the hot paths (MCP reads, /v2 reads and commands, Ask, a judge
# job, a compile run); deterministic, so they run in CI
cd packages/server && go test ./internal/testdb/netsim/ ./internal/ledger/ -run 'Audit|Proxy|Tracer|ScopeComesFirst|Pipelined' && \
  go test ./internal/mcpv2/ ./internal/handler/v2api/ ./internal/judge/ ./internal/compile/ -run RoundTrips -v

# Wall clock at 0 and 24 ms of round-trip time (a delaying TCP proxy in front of Postgres),
# printed as a table, with the 24 ms bars asserted; opt-in (MEMAX_LATENCY=1), since a loaded
# machine moves wall clock. TEST_DB_RTT=24ms puts every test database of a run behind the proxy
cd packages/server && MEMAX_LATENCY=1 go test ./internal/mcpv2/ ./internal/handler/v2api/ ./internal/judge/ ./internal/compile/ -run 'Latency$' -v
```

### Compile service

```bash
# Build (it imports the built compiler) and test: one contract, against the Node server and
# against the Worker entry under workerd
pnpm --filter @memaxlabs/compiler build && pnpm --filter @memaxlabs/compile-service build
pnpm --filter @memaxlabs/compile-service test

# Run it locally; point the server and worker at it with COMPILE_SERVICE_URL=http://localhost:8090
# (and COMPILE_SERVICE_TOKEN, if you set one on the service)
PORT=8090 pnpm --filter @memaxlabs/compile-service start
# Or the Worker under workerd
pnpm --filter @memaxlabs/compile-service dev:cf --port 8788 --var COMPILE_SERVICE_TOKEN:dev-token

# The Go compile tests and the Phase 1 gate against a running service instead of the Node one they spawn
cd packages/server && export MEMAX_TEST_COMPILE_SERVICE_URL=http://127.0.0.1:8788 MEMAX_TEST_COMPILE_SERVICE_TOKEN=dev-token && \
  go test ./internal/compile/ && go test ./internal/handler/v2api/ -run TestPhase1Gate

# CPU, memory and bundle size against the Workers limits (README, "Workers limits")
node packages/compile-service/scripts/measure.mjs
pnpm --filter @memaxlabs/compile-service build:cf
```

Every route but `/health` needs `Authorization: Bearer $COMPILE_SERVICE_TOKEN`; the Worker refuses to serve without the secret, and the API and worker send it (`compile.WithToken`). CI deploys it (`deploy-cloudflare.yml`) before the API.

### CLI: init, connect, link, the daemon, status, compile and the session-start hook (V2 local delivery)

The daemon (`packages/cli/src/lib/daemon/`) writes each space's compiled files into the repositories linked on the machine and reports hand edits; it never writes over one (rule 6). Its state, log, pid and control socket live in `~/.memax/daemon/`, beside the session-start hook's files (`warm.json`, which the daemon keeps current, `seen/` and the `loads/` queue it reports). `memax daemon run` is reached through `src/bin.ts` without loading the rest of the CLI (the MCP SDK alone is ~30 MB of memory), and it talks to `/v2` over `node:http(s)` (`lib/daemon/http.ts`), not `fetch`. The CLI carries a verbatim copy of the compiler's managed block (`lib/daemon/compiler/`) because `@memaxlabs/compiler` isn't published yet; edit the compiler, then re-copy.

```bash
# Point the CLI at a local server, sign in, link a repository and run the daemon in the foreground
MEMAX_API_URL=http://localhost:8080 memax login
memax link --space memax-v2 && memax daemon run        # or: memax daemon start | stop | status
memax status && memax compile

# Set a repository up (what a new user runs): detect, sign in, connect, import, settle, compile.
# --dry-run uploads nothing; --yes --space <slug> --format json for CI; --timing shows each step's budget
MEMAX_API_URL=http://localhost:8080 memax init
memax init --dry-run && memax init --yes --format json --timing

# Connect one agent here (MCP settings, its session-start hook, its connection, a first compile);
# running it again changes nothing. Agents: claude-code, codex, cursor, gemini, copilot, opencode, windsurf, chatgpt
memax connect claude-code && memax connect codex --format json

# Switch a V1 space to V2 (its owner): --dry-run says what moves and changes nothing; it asks first
# (--yes without a terminal), waits for a switch running in the background, resumes a failed one,
# and --back switches back. Tests: the flow on a fake /v2, and agents sync's notice for spaces on V2
memax switch --space acme-web --dry-run && memax switch --space acme-web --as project
pnpm --filter memax-cli exec vitest run test/switch

# The session-start hook as an agent runs it (stdin is the agent's event); --debug says why it was quiet
echo '{"session_id":"s1","cwd":"'"$PWD"'","source":"startup"}' | memax hook session-start --agent claude-code --debug

# Hook, warm cache, load reports and connect tests: the block per case, the cap, a socket recorder
# around the built hook (it builds its own copy under node_modules/.cache), and connect in temporary homes
pnpm --filter memax-cli exec vitest run test/hook

# The plugin and its marketplace, with Claude Code's own validator
claude plugin validate --strict plugins/memax && claude plugin validate --strict .

# Export a space's whole record, and verify an export (a folder or a downloaded zip)
memax export --space memax-v2 --out ./backup && memax verify-export ./backup/memax-v2
pnpm --filter memax-cli exec vitest run test/export && pnpm --filter memax-sdk exec vitest run src/v2/export.test.ts

# Re-copy the compiler's managed block (and init's cleanLine, lib/daemon/compiler/sanitize.ts)
# into the CLI after changing them (lint checks the copy)
node packages/cli/scripts/sync-compiler.mjs

# Init tests: detection fixtures in temporary homes, the splitter, the secret scan (against the
# server's corpus), hidden characters, and the whole flow against the fake /v2 server
pnpm --filter memax-cli exec vitest run test/init

# Device sign-in from the CLI (memax login --device) against the fake server's device grant
pnpm --filter memax-cli exec vitest run test/login

# Daemon tests (a fake /v2 server built on the real compiler)
pnpm --filter memax-cli exec vitest run test/daemon

# End to end against the real stack: a fresh database (needs psql and a role that can CREATE
# DATABASE), migrations, devseed, the compile service, the worker and the API server. Skipped
# without the flag. MEMAX_E2E_BIN can point at prebuilt server, worker, migrate and devseed.
MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/daemon/e2e-server.test.ts

# memax init end to end on the same stack, the judge on a fake model: conflicting CLAUDE.md and
# AGENTS.md, the conflict flagged, bulk keep, compile, files on disk, timed against five minutes
MEMAX_E2E_SERVER=1 pnpm --filter memax-cli exec vitest run test/init/e2e-init.test.ts

# The import endpoints, the import check and settling (real Postgres)
cd packages/server && go test ./internal/ledger/ ./internal/judge/ -run Import && \
  go test ./internal/handler/v2api/ -run 'Import|Spaces|Bulk'
```

Migrations use a single shared sequence. Don't hand-pick version numbers — always use `migrate:new`. CI enforces sequential numbering (`internal/migrate/migrate_test.go`) and rejects gaps, duplicates, orphan up/down files, and non-padded versions.

### Deployment Awareness

- `packages/server/` deploys to Fly.io as two processes with per-env tomls in `packages/server/fly/`:
  - API server (`fly.server.{staging,production}.toml`, `Dockerfile.server`) — serves HTTP, insert-only queue client
  - Worker (`fly.worker.production.toml`, `Dockerfile.worker`) — processes River jobs (memory processing, dreams). Staging has no worker app: `MEMAX_EMBEDDED_WORKER=1` makes the staging API run the worker in-process (`cmd/server/worker.go`), so an idle staging machine stops and Neon's staging compute can scale to zero
  - Sizing (Oct 2026 running-cost review): the prod API runs 2 shared-cpu-2x 1gb machines with `min_machines_running = 1`, so the second stays suspended until needed; the prod worker is shared-cpu-1x 1gb; staging is one shared-cpu-1x 512mb machine that stops when idle Each process opens one Postgres pool through `internal/dbpool`: 20 connections for the API and 32 for the worker (River's 23 workers plus its own), unless `DATABASE_URL` sets `pool_max_conns` or `DB_MAX_CONNS` is set. Production connects through Neon's pooler, which takes thousands of client connections.
- Cloudflare Workers (one Workers Paid plan) runs everything that needn't sit next to the database. `.github/workflows/deploy-cloudflare.yml` deploys each Worker with `wrangler deploy`: staging from `ci.yml` on every push to `main`, production from `deploy-production.yml`, with secrets read from Doppler and uploaded with the version. The `staging` and `production` GitHub Environments need `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`. Roll a Worker back with `wrangler rollback --env <env>` in its package.
  - `packages/compile-service/` — `memax-compile-{staging,production}` at `https://compile-staging.memax.app` and `https://compile.memax.app` (custom domains; no workers.dev). The API and worker reach it through `COMPILE_SERVICE_URL` in their tomls and send `COMPILE_SERVICE_TOKEN`; CI deploys it before them. The `Dockerfile` remains for self-hosting the Node server
  - `packages/web/` — `memax-web-{staging,production}`, built by OpenNext. Vercel (`vercel.json`, its Git integration) keeps serving `memax.app` until the DNS cutover; then the Vercel project goes
  - `packages/docs-site/` — `memax-docs-{staging,production}`, the static export (`out/`) as Workers static assets with no script. Vercel keeps serving `docs.memax.app` until the cutover
- This repo publishes `memax-sdk` and `memax-cli` to npm
- CI runs on GitHub Actions — check `.github/workflows/` for pipeline config

#### Moving to Cloudflare (cutover checklist)

Each step can be undone on its own; do them in order.

1. **Cloudflare account.** Subscribe to Workers Paid. Create an API token with Workers Scripts: Edit, Workers Routes: Edit, Zone: Read and DNS: Edit on `memax.app`. Add `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` to the `staging` and `production` GitHub Environments.
2. **Zone.** Add `memax.app` to Cloudflare and copy every record from the current DNS host (the apex and `docs` for Vercel, `api` and `staging-api` for Fly, mail records for Resend, any verification TXT records). Keep proxying off on the Fly and Vercel records so nothing changes, then switch the nameservers at the registrar. Rollback: point the nameservers back.
3. **Doppler** (staging and production configs). Add `COMPILE_SERVICE_TOKEN` (`openssl rand -hex 32`, a different value per environment). Make sure `WEB_SURFACE_SECRET` is there (the API already uses it). Add the web's build settings, taken from the Vercel project's environment variables: `NEXT_PUBLIC_API_URL` and `NEXT_PUBLIC_APP_URL` (required for staging), `NEXT_PUBLIC_DOCS_URL`, `NEXT_PUBLIC_POSTHOG_KEY`, `NEXT_PUBLIC_POSTHOG_HOST`.
4. **Compile service, staging.** Merge to `main`: CI deploys `memax-compile-staging` (it creates `compile-staging.memax.app` and its certificate), checks `/health`, then deploys the staging API, which now calls it with the token. Check a compile in staging. Rollback: `wrangler rollback --env staging`, or revert `COMPILE_SERVICE_URL` in `fly/fly.server.staging.toml` to a Node compile service.
5. **Web and docs, staging.** The same push deploys `memax-web-staging` and `memax-docs-staging` to their workers.dev URLs. Check that pages load there; sign-in needs the real hostname (the API's `APP_BASE_URL` decides which logins are web sessions), so it is checked in step 7.
6. **Production deploy.** Promote to `prod`: the workflow deploys `memax-compile-production` (`compile.memax.app`) before the API and worker, plus the web and docs Workers. Rollback: reset `prod` and re-run with `bump=none`; `wrangler rollback --env production` for a Worker alone.
7. **DNS cutover, staging first.** Uncomment the staging `routes` (with the staging web hostname) and `workers_dev: false` in `packages/web/wrangler.jsonc`, remove the Vercel record for that hostname, and deploy: a Workers custom domain creates its DNS record and certificate. Sign in, run an Ask, keep a proposal and check its receipt says `human_web`. Then do the same for `memax.app` (production `routes` in `packages/web/wrangler.jsonc`) and `docs.memax.app` (`packages/docs-site/wrangler.jsonc`). OAuth redirect URIs and `APP_BASE_URL` don't change, since the hostnames don't. Rollback: delete the custom domain in the Workers dashboard and restore the Vercel record; Vercel is still deployed.
8. **Retire the old hosts** after a week without a rollback: destroy the Fly apps `memax-compile-staging` and `memax-compile-production` (their tomls are gone; recreate from git history if ever needed), delete the Vercel projects, and remove `packages/web/vercel.json`.

## Keeping Documentation Up To Date

Documentation must stay in sync with the code. Stale docs are worse than no docs — they actively mislead.

### After completing work, update these files:

1. **`docs/plans/11-roadmap.md`** — Check off completed milestones (`- [x]`), update the "Immediate Next Steps" section, and adjust the status line at the top of each phase. If a milestone is partially done, note what's left.

2. **`AGENTS.md`** — If you add a new package, command, service, or change the tech stack, update the relevant section (Monorepo Structure, Tech Stack, Build & Run). Keep the "Build & Run" section accurate with any new commands.

3. **`CLAUDE.md`** — If a new design doc is added or the repo structure changes significantly, update the cross-references.

4. **`.env.example`** — If you add a new environment variable, add it here with a comment. Never put real values in this file.

### Rules:

- **Update docs in the same session as the code change.** Don't leave it for "later" — later never comes.
- **Roadmap is the source of truth for progress.** If you finish a task that maps to a roadmap checkbox, check it off immediately.
- **Design docs (`docs/plans/`) describe the target state.** Don't modify them to match shortcuts or temporary implementations. If you deviate from a design doc, add a comment in the code explaining why, not in the doc.
- **Be precise with status.** "~90% complete" with specifics is better than "mostly done." List what's left, not what's finished.

## Local Development

### Prerequisites

Docker and Docker Compose are required for running PostgreSQL and Redis locally.

### Starting the dev environment

```bash
# Start shared local infra (Postgres + Redis + MinIO) from the repo root.
# This is the canonical compose stack for both standalone local dev and the devcontainer.
docker compose up -d

# Run all packages in dev mode
pnpm dev

# Or run individual packages
pnpm --filter @memaxlabs/server dev
pnpm --filter @memaxlabs/web dev
```

### Environment setup

```bash
# Copy the example env file and fill in values
cp .env.example .env

# The Go server reads from environment variables directly.
# The web app reads NEXT_PUBLIC_* vars from .env or .env.local.
# Docker Compose services (Postgres, Redis) use defaults that match .env.example.
```

### Database

```bash
# Migrations run automatically on server startup when DATABASE_URL is set.
# To reset the database:
docker compose down -v && docker compose up -d
```

## What Not To Do

- Don't add features beyond what's asked — no speculative abstractions
- Don't modify design docs in `docs/plans/` without discussing with the team first
- Don't commit `.env` files, credentials, or API keys
- Don't bypass boundary enforcement for convenience
- Don't add dependencies without justification — prefer lightweight, focused packages
- Don't write code that only works with one specific AI agent
