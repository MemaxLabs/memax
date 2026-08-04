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

## Business Documents

Business documents live in a separate private repository: [`MemaxLabs/internal-docs`](https://github.com/MemaxLabs/internal-docs). Clone it as a sibling of this monorepo so paths resolve as `../internal-docs/NN-*.md`:

```
~/workspaces/
├── memax/                    (this repo)
└── internal-docs/            (business docs, private)
```

Read these before making pricing, cost, or go-to-market decisions:

- `../internal-docs/01-business-model.md` — pricing tiers (Free/$9/$19/$15), revenue model, unit economics
- `../internal-docs/02-go-to-market.md` — launch strategy, channels, growth loops
- `../internal-docs/03-competitive-landscape.md` — competitors (Mem0, QMD, memsearch), positioning
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

- Memax itself integrates with Claude Code via hooks (see `docs/plans/06-developer-surface.md`)
- When working on the hook system (`packages/cli/src/commands/hook.ts`), test with a real Claude Code installation
- Hook latency budget: `<500ms` total. Be aggressive with caching.
- Context injection should use `<memax-context>` tags and stay under 3000 tokens

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
    cli/             # memax-cli — Commander.js CLI, published to npm — Apache-2.0

# Design docs (docs/plans, docs/infra, docs/design, ...) live in the sibling
# private repo MemaxLabs/memax-internal — clone alongside this repo.
```

## Tech Stack

| Component                           | Technology                                                                |
| ----------------------------------- | ------------------------------------------------------------------------- |
| CLI                                 | TypeScript, Commander.js, chalk (`packages/cli`)                          |
| SDK                                 | TypeScript (`memax-sdk`, `packages/sdk`)                                  |
| API Server (includes retrieval)     | Go (stdlib net/http)                                                      |
| Web App                             | Next.js 16 (App Router), Tailwind, Radix UI, TanStack Query, Tiptap, cmdk |
| Developer Hub                       | Fumadocs (Next.js), Pagefind, Scalar                                      |
| Design System                       | @memaxlabs/ui — Tailwind + Radix primitives                               |
| Database                            | PostgreSQL (Neon) + pgvector                                              |
| Cache                               | Redis (Upstash)                                                           |
| Object Storage                      | Cloudflare R2                                                             |
| Embeddings                          | Voyage AI                                                                 |
| Reranking                           | Cohere Rerank                                                             |
| LLM (distillation + classification) | Claude Haiku                                                              |
| LLM (answer synthesis)              | Claude Haiku 3.5 / Sonnet 4                                               |
| Queue                               | River (Postgres-backed, Go)                                               |
| Auth                                | OAuth2 (GitHub/Google)                                                    |
| Deployment                          | Fly.io (API + worker), Vercel (web)                                       |
| CI/CD                               | GitHub Actions                                                            |
| Package Manager                     | pnpm (workspaces)                                                         |
| Monorepo                            | Turborepo                                                                 |

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

**The two files:** `packages/server/internal/handler/mcp.go` (Go, remote) and `packages/cli/src/commands/mcp.ts` (TypeScript, local) — both now in this repo.

**The rule:** When adding or modifying an MCP tool (name, description, parameters), update BOTH files in the same commit. Current tools (9): `memax_recall`, `memax_push`, `memax_get`, `memax_list`, `memax_hubs`, `memax_hub_members`, `memax_forget`, `memax_capture`, `memax_topics`.

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

- **Read `docs/design/design-system.md` before writing ANY frontend code.** The Memax design language is specific and intentional — not generic shadcn.
- Uses `@base-ui/react` primitives (NOT Radix), Tailwind CSS 4.0, oklch color tokens
- **Liquid glass surfaces** — use `glass`, `glass-subtle`, `glass-strong` instead of flat `bg-card`
- **Colored shadows** — use `shadow-glow`, `shadow-premium` instead of Tailwind's generic `shadow-md`
- **Spring easing** — use `var(--ease-spring)` for all transitions, never `ease-in-out`
- **Display typography** — headings use `text-display-*` classes (Inter, tight letter-spacing); code blocks use JetBrains Mono. Both load via `next/font/google` in `app/layout.tsx`.
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

### Server-specific commands

```bash
# Build and run API server
cd packages/server && go run ./cmd/server/

# Build and run background worker (separate process)
cd packages/server && go run ./cmd/worker/

# Deploy API server to Fly.io (staging; swap to fly.server.production.toml for prod)
cd packages/server && fly deploy -c fly/fly.server.staging.toml

# Deploy worker to Fly.io (staging; swap to fly.worker.production.toml for prod)
cd packages/server && fly deploy -c fly/fly.worker.staging.toml

# Create a new migration with the correct next version
pnpm --filter @memaxlabs/server migrate:new <slug>

# Run the LoCoMo benchmark harness
cd packages/server && go run ./cmd/locomo/ -dataset eval/locomo/data/locomo10.json
```

Migrations use a single shared sequence. Don't hand-pick version numbers — always use `migrate:new`. CI enforces sequential numbering (`internal/migrate/migrate_test.go`) and rejects gaps, duplicates, orphan up/down files, and non-padded versions.

### Deployment Awareness

- `packages/server/` deploys to Fly.io as two processes with per-env tomls in `packages/server/fly/`:
  - API server (`fly.server.{staging,production}.toml`, `Dockerfile.server`) — serves HTTP, insert-only queue client
  - Worker (`fly.worker.{staging,production}.toml`, `Dockerfile.worker`) — processes River jobs (memory processing, dreams)
  - Prod tomls allocate bigger VMs (shared-cpu-2x, 1gb) and `min_machines_running ≥ 1` for HA; staging stays cheap (1x, 256mb)
- `packages/web/` deploys to Vercel (`memax.app`)
- `packages/docs-site/` deploys to Vercel (`docs.memax.app`)
- This repo publishes `memax-sdk` and `memax-cli` to npm
- CI runs on GitHub Actions — check `.github/workflows/` for pipeline config

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
