---
name: claude-review
description: "Run a Claude CLI code review on recent changes. Use this skill whenever the user asks for a 'claude review', 'second opinion from claude', 'get claude to review', or wants an independent Claude-based review of their changes. Also trigger when the user says 'let claude check this', 'review with claude', or any variation of asking for an automated Claude review pass. This skill is the Claude counterpart to codex-review and handles the full lifecycle: briefing Claude, running the review non-interactively, monitoring the stream-json output, parsing findings, and iterating on fixes via session resume."
---

# Claude Review Skill

Run an independent code review using the Claude CLI (`claude --print`). Claude reads the codebase, runs tests, and produces structured findings with severity levels and file references.

This skill is the Claude counterpart to `codex-review`. Use `codex-review` when you want Codex/GPT as the reviewer (e.g. Claude is the primary agent and wants a second opinion). Use `claude-review` when you want Claude as the reviewer (e.g. Codex, Gemini, or another agent is the primary and wants a Claude second opinion, or when you explicitly want Claude's judgment on top of Claude's own work for a fresh-context look).

When Codex is the caller, keep the roles clear: Claude reviews and verifies; Codex owns edits, final judgment, and user-facing recommendations. Ask Claude for findings and supporting evidence, not patches. Claude may read/search the repo, run commands, and create temporary verification scripts or artifacts, but it must not modify git-tracked project files.

## Why This Exists

Code review by a second model — even the same model family running in a fresh session — catches bugs, edge cases, and design issues that the primary agent may have blind spots on. Claude in `--print` mode runs autonomously, reads any file it needs, can run tests, and produces a structured review without needing interactive input. Fresh-context review also defeats prompt-stickiness: the reviewer has not been rationalizing decisions all session and sees the code on its own terms.

## Prerequisites

- `claude` CLI must be installed and authenticated (Anthropic API key or OAuth session configured — verify with `claude auth status`)
- The repo must be a git repository with a working directory
- `jq` installed for parsing stream-json output (check with `which jq`)

## Workflow

### Step 1: Gather Context

Before running Claude, build a clear review brief. The quality of the review depends on the quality of the prompt — a vague "review everything" yields generic findings; a specific "check the scroll-hide translate math and mobile backdrop-filter composition" yields actionable ones.

Gather:

- **What changed**: `git diff --stat` for scope, key file paths, `git diff` for the actual changes
- **Why it changed**: the intent behind the changes (bug fix, new feature, refactor, perf polish)
- **What to focus on**: specific concerns (correctness, edge cases, security, performance, UX)
- **What to ignore**: known issues, unrelated failures, pre-existing lint warnings, work-in-progress files
- **What was verified**: commands already run locally, plus commands Claude may run if it wants to check its reasoning

Structure the prompt as:

```
Review the recent [description of changes]. Working directory: /workspaces/memax-internal.

Role and boundaries:
You are reviewing only. Do not edit, format, commit, or otherwise modify
git-tracked project files. You may search/read the repository, run
verification commands, and create temporary scripts or artifacts under
/tmp/claude-review if needed to verify a concern. Report findings and
evidence; Codex will decide what to change.

Context — what this change does and why:
[1-3 paragraphs explaining the problem and the fix. Include the
 bug's symptoms, the root cause you identified, and the approach
 you took. This is what lets the reviewer judge whether the fix
 is correct for the actual problem, not just whether the code
 compiles.]

Files changed:

1. [file path] — [what this file does and what changed]
2. [file path] — [what this file does and what changed]
...

What I want you to check:

- [specific concern 1 — usually a correctness question]
- [specific concern 2 — edge case, concurrency, boundary]
- [specific concern 3 — domain-specific rule, e.g. owner isolation]
- Anything else that would affect correctness, performance, or UX.

Verification already run locally:
- `pnpm lint`
- `pnpm --filter @memaxlabs/web test`
- [any other relevant command]

Verification Claude may run:
- [commands Claude can run if needed, or "none requested"]

Format findings with severity (High/Medium/Low), file, line, description.
End with a "No blocking findings" line if the code is clean.
```

Save the prompt to a file (`/tmp/claude-review/prompt.txt`) — multi-line prompts are cumbersome as shell arguments and easy to mangle with quoting.

### Step 2: Run Claude

Launch Claude in the background using `--print` mode with stream-json output:

```bash
mkdir -p /tmp/claude-review
cat > /tmp/claude-review/prompt.txt <<'EOF'
<review prompt>
EOF

cat /tmp/claude-review/prompt.txt | claude \
  --allow-dangerously-skip-permissions \
  --dangerously-skip-permissions \
  --model opus \
  --effort high \
  --print \
  --include-partial-messages \
  --output-format=stream-json \
  --verbose \
  2>&1
```

Flag meanings:

- `--print` — non-interactive, print response and exit (reads prompt from stdin or from the trailing positional argument)
- `--allow-dangerously-skip-permissions` + `--dangerously-skip-permissions` — reviewer needs to inspect code and run verification without permission prompts. The prompt must still forbid edits to git-tracked project files. Put any throwaway scripts or artifacts under `/tmp/claude-review`.
- `--model opus` — thorough model for careful review (alias resolves to the current Opus)
- `--effort high` — longer thinking budget for edge-case analysis
- `--output-format=stream-json` — line-delimited JSON events so progress and the final `result` event can be parsed
- `--verbose` — required by `--output-format=stream-json`
- `--include-partial-messages` — emit partial content deltas as they arrive, not just on message boundaries

Run with `run_in_background: true`. Reviews take 2–8 minutes depending on model + effort + diff size. Note the output-file path the runtime prints:

```
Output is being written to: /tmp/claude-1000/.../tasks/<id>.output
```

### Step 3: Capture the Session ID

Every stream-json event carries a `session_id` — the field appears on the very first event, so you can extract it as soon as output begins:

```bash
# jq — preferred
jq -r 'select(.session_id) | .session_id' /tmp/claude-1000/.../tasks/<id>.output | head -1

# grep fallback if jq isn't installed
grep -oE '"session_id":"[^"]+"' /tmp/claude-1000/.../tasks/<id>.output | head -1 | cut -d'"' -f4
```

Save this ID — it is the key to resume later. Example: `019da810-44c5-7e50-b25b-13eac274a431`.

### Step 4: Monitor for Completion

Use the Monitor tool to watch for the terminal `result` event. The result event is the authoritative end-of-review signal and contains the full review text in its `result` field:

```bash
tail -f <output_file> 2>&1 | grep -E --line-buffered \
  '"type":"result"|"is_error":true|"subtype":"error"|"api_error_status":"[^n]'
```

One notification fires when:

- `"type":"result"` — review completed (success or error; check `"subtype"` and `"is_error"` inside)
- `"is_error":true` or `"subtype":"error"` — terminal error

Do NOT poll or sleep. The monitor pattern handles notification.

### Step 5: Read and Parse Findings

When the result event fires, extract the review text from the final JSONL event:

```bash
jq -r 'select(.type=="result") | .result' <output_file>
```

This prints the full review as plain text. Claude structures its review as:

```
Findings

1. High: [description]
   [file:line] — [details]

2. Medium: [description]
   [file:line] — [details]

Verification
- [what was tested]
- [what passed / failed]
```

Extract:

- **Session ID** (Step 3) — needed for resume
- **Findings** (numbered, with severity)
- **Verification results** (what Claude actually ran)
- **Cost + duration** from the result event (optional, useful for budget tracking):
  ```bash
  jq -r 'select(.type=="result") | "cost=$\(.total_cost_usd) duration=\(.duration_ms)ms turns=\(.num_turns)"' <output_file>
  ```

Present findings to the user in a clear table. Separate Claude's findings from your own judgment — if a finding is weak or doesn't apply to this codebase, say so explicitly rather than silently dropping it.

### Step 6: Handle Incomplete Reviews

Claude may get cut off if the review hits a context limit, the max-turns budget, or an API error. Signs:

- `"type":"result"` with `"subtype":"error"` or `"is_error":true`
- `"stop_reason"` is `"max_turns"`, `"max_tokens"`, or `"refusal"`
- The review text ends mid-analysis (reads files but no conclusions)

To resume, use the saved session ID:

```bash
mkdir -p /tmp/claude-review
cat > /tmp/claude-review/resume_prompt.txt <<'EOF'
<resume prompt with context>
EOF

cat /tmp/claude-review/resume_prompt.txt | claude \
  --resume <SESSION_ID> \
  --allow-dangerously-skip-permissions \
  --dangerously-skip-permissions \
  --model opus \
  --effort high \
  --print \
  --include-partial-messages \
  --output-format=stream-json \
  --verbose \
  2>&1
```

The resume prompt should remind Claude what it was doing and ask it to produce findings:

```
You were reviewing [description] and got cut off. You had already:
1. [what Claude read]
2. [what tests it ran]

Continue in review-only mode. Do not modify git-tracked project files.
Temporary verification artifacts under /tmp/claude-review are allowed.

Please produce your review findings now. Format: numbered findings
with severity (High/Medium/Low), file, line, description. End with
a "No blocking findings" line if the code is clean.
```

### Step 7: Fix and Re-review

After addressing findings, resume the same session with an update:

```bash
mkdir -p /tmp/claude-review
cat > /tmp/claude-review/rereview_prompt.txt <<'EOF'
<update about fixes and request for re-review>
EOF

cat /tmp/claude-review/rereview_prompt.txt | claude \
  --resume <SESSION_ID> \
  --allow-dangerously-skip-permissions \
  --dangerously-skip-permissions \
  --model opus \
  --effort high \
  --print \
  --include-partial-messages \
  --output-format=stream-json \
  --verbose \
  2>&1
```

Brief Claude on what was fixed and ask for another pass:

- Which findings were addressed and how
- Any findings intentionally deferred with rationale
- Request re-review of the specific changes
- Reminder that Claude remains in review-only mode: no edits to git-tracked project files; temporary verification artifacts under `/tmp/claude-review` are allowed

Before resuming, run the validation suite locally so the review session doesn't burn tokens rediscovering failures you already know about:

```bash
pnpm format && pnpm lint
pnpm --filter @memaxlabs/web test
cd packages/server && go build ./... && go vet ./... && go test ./...
```

### Step 8: Iterate Until Clean

Repeat steps 4–7 until Claude reports no blocking findings. A clean review looks like:

```
No blocking findings in this pass.
[description of what was verified]
Verification: [tests run and passed]
```

Then report to the user:

- Final verdict (clean / has intentional tradeoffs)
- What was changed in response to the review
- What was intentionally left as-is and why
- Follow-up work for later
- Session ID (so a future round can resume the same conversation)

## Key Patterns

### Stream-JSON Output Format

The `--output-format=stream-json` emits one JSON object per line. Every event carries a `session_id`; event types in order of typical occurrence:

| Event type                                           | Meaning                                                                             |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `stream_event` with `event.type=content_block_start` | Claude started a text/tool-use block                                                |
| `stream_event` with `event.type=content_block_delta` | Text chunk (partial message content)                                                |
| `stream_event` with `event.type=content_block_stop`  | Block finished                                                                      |
| `stream_event` with `event.type=message_delta`       | Usage + `stop_reason` for the turn                                                  |
| `stream_event` with `event.type=message_stop`        | Message complete (may be followed by tool calls)                                    |
| `result`                                             | Terminal. `result` field contains the final text; `subtype` is `success` or `error` |

To watch text as it streams:

```bash
tail -f <output_file> 2>&1 | jq -r --unbuffered \
  'select(.event.delta.text) | .event.delta.text'
```

Useful during debugging; too noisy for normal monitoring.

### Model Selection

Default to `opus` + `--effort high` for thorough reviews. Other options:

```bash
--model opus --effort high      # thorough, recommended
--model opus --effort xhigh     # deepest; use for critical/subtle reviews (slower, more expensive)
--model sonnet --effort high    # faster; use for mechanical reviews (typos, style)
--model haiku                   # triage only; not recommended for logic review
```

Model aliases resolve to the current model of that class (opus → latest Opus). Pin explicit model IDs (`claude-opus-4-7`) only when reproducibility across a time window matters.

### Background Execution

Always run Claude in the background. Reviews take 2–8 minutes. Use the main session productively while waiting — work on other tasks, prepare for potential fixes, or brief the user on what you asked Claude to check.

### Session Persistence

The session_id is the key to the iterative review loop. Extract it from the first stream event (all events carry it) and save it. `--resume <id>` carries full conversation context, so Claude remembers what it already reviewed, what tests it ran, and what findings it produced.

Claude sessions live on disk under `~/.claude/projects/<cwd-hash>/` by default. `--no-session-persistence` disables this — **do not** use that flag for reviews; it breaks resume.

### Prompt via stdin vs positional

Both work with `--print`:

```bash
# stdin — preferred for multi-line prompts stored in files
cat prompt.txt | claude --print ...

# positional — works for short one-liners
claude --print ... "Review the diff in packages/web and summarize."
```

stdin is cleaner because shell quoting doesn't mangle backticks, dollar signs, or newlines inside the prompt.

### Output Location

Background commands write to a temp file. The path is printed when the command starts:

```
Output is being written to: /tmp/claude-1000/.../tasks/<id>.output
```

Save this path — every step after Step 2 reads from it.

## Common Issues

- **"--output-format requires --verbose"**: Stream JSON needs `--verbose` to emit. Add the flag.
- **Empty output file**: Claude is still spinning up. Wait for the first `stream_event` before assuming failure.
- **`"is_error":true` at session start**: Usually an auth or MCP config problem. Check `claude auth status` and `~/.claude/settings.json`.
- **Review stops after reading many files but before producing findings**: Hit max-turns or max-tokens. Resume the session and ask specifically for conclusions.
- **MCP server errors in the stream**: Transient MCP connection errors are NOT review findings. Filter out lines matching `rmcp::transport::worker` or similar from your findings presentation.
- **jq command not found**: Fall back to `grep -oE '"[^"]+"'` / `sed` parsing, but install jq — it is on the repo's devcontainer baseline.
- **Session not resumable**: Confirm the session ID is correct (UUID, not truncated) and that `--no-session-persistence` was not set on the original run.
- **Claude is rationalizing its own primary-session work**: Fresh-context review partly defeats this. If the review still feels too lenient, switch to `codex-review` for a different model's perspective.

## Memax-Specific Review Standards

All other sections of this skill are project-agnostic. This section — and only this section — carries Memax-specific rules. When briefing Claude for a review on this repo, include the relevant subset of these rules as explicit review criteria in the prompt. Do NOT paste them all every time — pick the ones that match the diff's blast radius.

### Always-on (include on every review of this repo)

- **Owner isolation** — every store method that reads or deletes user data must accept `ownerID` and include `WHERE owner_id = $N`. Application-level checks are not enough. Reference: `AGENTS.md` § Security Rules.
- **ApiResponse envelope** — every REST handler returns `model.ApiResponse{Data: ...}` via `writeJSON(w, status, ApiResponse{...})` or `writeError(...)`. Raw `json.NewEncoder(w).Encode(...)` for REST endpoints is a bug (the CLI client unwraps `response.data` and crashes on raw payloads).
- **Fail-closed enforcement** — DB errors in authorization or cap-enforcement paths must block the operation, never allow it.

### Frontend / web (include when `packages/web/` is in diff)

- **i18n** — no hardcoded English strings in JSX. All user-visible text goes through `t.*` from `useLocale()`. Reference: `.agents/skills/i18n/SKILL.md`.
- **Design system** — new UI uses `@base-ui/react` primitives, oklch tokens, `glass-*` surfaces, `shadow-glow`/`shadow-premium`, `var(--ease-spring)`. No generic shadcn patterns. Reference: `docs/design/memax-design-system.md`.
- **Component size** — no monolith components. ~300 lines max per component; prefer route-based view switching over conditional state branches.
- **Admin surface boundary** — admin calls in the web app go through `@/lib/admin-client`, never through `memax-sdk`. The SDK is published to npm; admin is internal. CI check: `scripts/check-sdk-boundary.mjs`.

### Backend / Go server (include when `packages/server/` is in diff)

- **Shared infra modules** — LLM calls via `internal/anthropic.Client`, embeddings via `internal/ingest/embed.Embedder`, chunking via `internal/ingest/chunker`. No raw Anthropic/Voyage HTTP calls or hand-rolled markdown splitters.
- **Dependency injection** — modules accept deps in their constructor (`New(client *anthropic.Client)`), not by reading env vars inside methods. Env vars read once at startup in `cmd/server/main.go` / `cmd/worker/main.go`. Nil deps mean disabled; callers null-check.
- **Cap / member enforcement atomicity** — ownership, member, and quota enforcement runs inside a transaction with advisory locks to prevent TOCTOU races.
- **Scope validation** — plan mutations validate scope (personal vs hub) before enforcing limits; plans are per-scope, not per-user.
- **Migration hygiene** — new migrations use `pnpm --filter @memaxlabs/server migrate:new <slug>`, not hand-picked version numbers. CI enforces sequential numbering.

### Cross-cutting (include when the diff changes an API contract)

- **SDK parity** — if a server response shape changed (fields added, array → object, key renamed, pagination added), the web app (`packages/web/src/hooks/`), the public CLI (`MemaxLabs/memax/packages/cli/src/`), the public SDK (`MemaxLabs/memax/packages/sdk/src/`), and the MCP server (`packages/server/internal/handler/mcp.go` + public `MemaxLabs/memax/packages/cli/src/commands/mcp.ts`) all need to handle the new shape.
- **MCP parity** — the public CLI MCP server (`MemaxLabs/memax/packages/cli/src/commands/mcp.ts`) and Go server MCP handler (`packages/server/internal/handler/mcp.go`) must expose identical tools. Changes to one require changes to the other.

### Retrieval / ingest (include when `retrieval/`, `ingest/`, `recall.go`, `ask.go`, `postgres_chunks.go`, `postgres_memories.go`, or migrations are in diff)

- **Eval-before-push** — retrieval and ingestion changes must run `packages/server/eval/` locally before merging. A single constant change can regress quality across 59 graded queries. Reference: `.agents/skills/eval/SKILL.md`.

### Format & lint (include always — check commits)

- **Format before commit** — `pnpm format && pnpm lint` must pass. There are no git hooks (by design — bad DX). CI rejects unformatted code. Reference: `AGENTS.md` § Code Conventions.

## Prompt Template

A starter template for a Memax review prompt. Trim to the subsystem being changed:

```text
Review the uncommitted changes in this repository. Working directory:
/workspaces/memax-internal.

Role and boundaries:
You are reviewing only. Do not edit, format, commit, or otherwise modify
git-tracked project files. You may search/read the repository, run
verification commands, and create temporary scripts or artifacts under
/tmp/claude-review if needed to verify a concern. Report findings and
evidence; Codex will decide what to change.

Context — what this change does and why:
[1-3 paragraphs. Problem, root cause, approach.]

Files changed:

1. [path] — [purpose + what changed]
2. [path] — [purpose + what changed]
...

What I want you to check:

- Correctness of [specific logic].
- Edge cases around [specific boundary].
- [Any subsystem-specific concern.]

Memax-specific invariants that apply to this diff:
[Pick from .agents/skills/claude-review/SKILL.md → Memax-Specific
 Review Standards. Include only the ones the diff could break.
 Example list for a server+web change:]

- Owner isolation — store methods filter by owner_id.
- ApiResponse envelope — REST responses use model.ApiResponse{Data: ...}.
- SDK parity — server response shape changes propagate to web, CLI,
  SDK, MCP in the same commit.
- i18n — no hardcoded English strings in JSX; all text through t.*.

Verification already run locally:
- `pnpm lint`
- `pnpm --filter @memaxlabs/web test`
- [any other relevant command, or "none"]

Verification Claude may run:
- [commands Claude can run if needed, or "none requested"]

Format findings with severity (High/Medium/Low), file, line,
description. End with a "No blocking findings" line if the code
is clean. Call out anything that would affect correctness,
security, performance, or UX quality.
```

## Example

A typical review cycle:

1. User: "Get Claude to review the backdrop-filter fix on the mobile bar."
2. Agent gathers `git diff --stat` + `git diff`, notes files and intent, writes a prompt focused on the fix's specific questions (translate math, cascade, edge cases).
3. Agent saves prompt to `/tmp/claude-review/prompt.txt`.
4. Agent runs `claude --print --model opus --effort high --output-format=stream-json --verbose --include-partial-messages` in the background, piping the prompt via stdin.
5. Agent extracts `session_id` from the first JSONL event.
6. Agent starts Monitor on the output file, watching for `"type":"result"`.
7. Claude completes in ~4 minutes. Agent reads the result text via `jq -r 'select(.type=="result") | .result'`.
8. Claude produces 2 Medium findings + 1 Low. Agent presents them in a table with its own judgment on each.
9. Agent fixes the two Mediums, notes the Low as an intentional deferral with rationale.
10. Agent runs `pnpm format && pnpm lint && pnpm --filter @memaxlabs/web test` to confirm fixes don't regress.
11. Agent resumes the same session with a re-review prompt listing fixes + the deferred Low's rationale.
12. Claude reports "No blocking findings."
13. Agent reports clean review to user, notes session ID for future continuation.
