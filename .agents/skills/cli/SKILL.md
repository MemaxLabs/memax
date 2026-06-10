---
name: cli
description: Use when adding, modifying, or fixing CLI commands in the public Memax CLI package. Covers architecture, patterns, UX conventions, and quality standards. ALWAYS trigger on any work touching the public repo's packages/cli/ — including command handlers, lib utilities, output formatting, auth flows, and sync logic. Even 'small' CLI changes need to follow the established patterns.
---

# Memax CLI — Development Skill

The memax CLI is a TypeScript CLI built with Commander.js and chalk. It's the power-user surface of Memax — every interaction should feel fast, intentional, and delightful. This skill covers architecture, patterns, and standards.

CLI source lives in the public `MemaxLabs/memax` repo. In the internal repo, consume the published `memax-cli` package for local usage and make source changes in the public checkout.

## Architecture

Paths below are relative to the public `MemaxLabs/memax` checkout.

```
packages/cli/
  src/
    index.ts              # Command registration (Commander.js)
    commands/             # One file per command (or command group)
      push.ts             # memax push
      recall.ts           # memax recall
      sync.ts             # memax sync, memax sync agents
      setup.ts            # memax setup (orchestrator)
      setup-types.ts      # Shared types for setup modules
      setup-mcp.ts        # MCP config (remote + local)
      setup-hooks.ts      # Hook scripts
      setup-instructions.ts  # Instruction injection, skills
      ...
    lib/                  # Shared utilities — import from here, never duplicate
      api.ts              # HTTP client (auth, retries, envelope parsing)
      config.ts           # ~/.memax/config.json management
      credentials.ts      # OAuth token storage (mode 0o600)
      prompt.ts           # Interactive prompts (confirm, ask, confirmDefault)
      project-context.ts  # Git context detection, .memax.yml hub lookup
  assets/
    skills/               # Bundled skill files installed by memax setup
```

## Adding a New Command

### 1. Create the command file

```typescript
// src/commands/example.ts
import chalk from "chalk";
import { apiPost } from "../lib/api.js";

interface ExampleOptions {
  verbose?: boolean;
}

export async function exampleCommand(
  positionalArg: string | undefined,
  options: ExampleOptions,
): Promise<void> {
  // Validate input early
  if (!positionalArg) {
    console.error(chalk.red("Usage: memax example <arg>"));
    process.exit(1);
  }

  try {
    const result = await apiPost<{ id: string }>("/v1/example", {
      content: positionalArg,
    });
    console.log(chalk.green("  Done."), chalk.gray(result.id));
  } catch (err) {
    console.error(chalk.red(`  Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}
```

### 2. Register in index.ts

```typescript
import { exampleCommand } from "./commands/example.js";

program
  .command("example <arg>")
  .description("One-line description of what this does")
  .option("-v, --verbose", "Show detailed output")
  .action(exampleCommand);
```

### 3. Subcommands

For command groups (like `memax sync agents`), create the parent first, then chain:

```typescript
const syncCmd = program
  .command("sync [directory]")
  .description("Sync files or agent configs")
  .action(syncCommand);

syncCmd
  .command("agents")
  .description("Sync agent config files with Memax cloud")
  .action(syncAgentMemoryCommand);
```

**One way to do each thing.** Never add both a flag (`--agent-memory`) and a subcommand (`sync agents`) for the same action.

## Shared Utilities — Use Them, Never Duplicate

### API Client (`lib/api.ts`)

```typescript
import { apiGet, apiPost, apiPut, apiPatch, apiDelete } from "../lib/api.js";

const result = await apiPost<ResponseType>("/v1/endpoint", { body });
const data = await apiGet<ResponseType>("/v1/endpoint");
```

- Auto-handles auth (env var → stored token → refresh)
- Retries on `"not_ready"` (server cold start)
- Unwraps `{ data }` envelope — you get `T` directly
- Throws `ApiError` with `.code` and `.status` on failure

### Prompts (`lib/prompt.ts`)

```typescript
import { confirm, ask, confirmDefault } from "../lib/prompt.js";

const yes = await confirm("  Delete? [y/N] "); // true on 'y'
const answer = await ask("  Enter name: "); // trimmed string
const proceed = await confirmDefault("  Continue? [Y/n] "); // true unless 'n'
```

Never use `createInterface` directly. All readline usage goes through these helpers.

### Project Context (`lib/project-context.ts`)

```typescript
import {
  detectProjectContext,
  readMemaxYmlHub,
} from "../lib/project-context.js";

const ctx = detectProjectContext(); // { repo, project, branch }
const hub = readMemaxYmlHub(); // hub ID from .memax.yml or undefined
```

### Public API Types (`memax-sdk`)

```typescript
import type { Memory, RecallResult, AgentConfig } from "memax-sdk";
```

Never define local interfaces for types that exist in `memax-sdk`. Check there first.

## UX Standards

### Output Formatting

The CLI output should feel like a premium, quiet tool — not a chatty script.

**Indentation:** 2-space indent for all output. Nested items get 4 spaces.

```
  Memax Config Sync

  Claude Code
    = CLAUDE.md                          unchanged
    ↑ projects/memax/memory/feedback.md  pushing (local newer)

  Done: 1 pushed, 1 unchanged
```

**Colors:**

| Color           | When to Use                                 |
| --------------- | ------------------------------------------- |
| `chalk.red`     | Errors, failures                            |
| `chalk.green`   | Success, completion                         |
| `chalk.yellow`  | Warnings, confirmations requiring attention |
| `chalk.cyan`    | Highlights, pull/download actions           |
| `chalk.gray`    | Secondary info, hints, metadata             |
| `chalk.bold`    | Key values (titles, names)                  |
| `chalk.white`   | Section headers within output               |
| `chalk.magenta` | Special tags, merge actions                 |

**Icons (Unicode):**

| Icon           | Meaning                |
| -------------- | ---------------------- |
| `✓` (`\u2713`) | Success / installed    |
| `✗` (`\u2717`) | Failure / error        |
| `↑` (`\u2191`) | Pushing / uploading    |
| `↓` (`\u2193`) | Pulling / downloading  |
| `↔` (`\u2194`) | Merged (bidirectional) |
| `=`            | Unchanged              |
| `-`            | Skipped                |
| `?`            | Unknown / unresolved   |
| `•`            | Bullet point           |

**Section structure:**

```typescript
console.log(chalk.bold("\n  Memax Config Sync\n")); // Title with padding
console.log(chalk.gray("  Scanning...\n")); // Subtitle

// ... per-item output ...

console.log(chalk.bold(`\n  Done: ${summary}\n`)); // Summary with padding
```

### Piping & TTY Detection

Every command that produces output should work in pipes:

```typescript
if (!process.stdout.isTTY) {
  // Machine-readable: one result per line, no colors, no decoration
  for (const m of results) {
    console.log(`${m.id}\t${m.title}`);
  }
  return;
}
// Human-readable: colors, indentation, summaries
```

Stdin piping for content input:

```typescript
if (!process.stdin.isTTY) {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  content = Buffer.concat(chunks).toString("utf-8");
}
```

### Confirmation for Destructive or Large Operations

```typescript
// Destructive: default NO
const confirmed = await confirm("  Delete all memories? [y/N] ");

// Large but safe: default YES
const proceed = await confirmDefault("  Sync 47 files? [Y/n] ");
```

Skip confirmations with `--yes` / `-y` flag.

### Error Messages

Errors should tell the user what to DO, not just what went wrong:

```typescript
// Bad
console.error(chalk.red("Authentication failed"));

// Good
console.error(chalk.red("Not authenticated — run `memax login` first"));
```

```typescript
// Bad
console.error(chalk.red("API error 404"));

// Good
console.error(chalk.red(`Memory not found: ${id}`));
```

## File Size Discipline

**Max ~300 lines per command file.** If a command grows beyond this:

1. Extract implementation into `command-helpers.ts` or `command-section.ts`
2. Keep the main file as the orchestrator
3. Use a shared types file to avoid circular deps

Example: `setup.ts` (orchestrator) → `setup-mcp.ts`, `setup-hooks.ts`, `setup-instructions.ts`, `setup-types.ts`

The sub-modules import only from the types file — never from the main command file. This prevents circular dependencies.

## Auth Patterns

### Commands That Need Auth

```typescript
// API calls auto-attach auth headers — no manual handling needed.
// If not logged in, apiPost/apiGet throw with "Not authenticated" message.
try {
  const result = await apiPost("/v1/endpoint", body);
} catch (err) {
  // ApiError with message "Not authenticated — run `memax login` first"
  console.error(chalk.red((err as Error).message));
  process.exit(1);
}
```

### Commands That Work Without Auth

Some commands (like `memax mcp serve` for local mode) don't need auth. These should never import or call auth functions.

### Hub Context

```typescript
// Hub header for scoped operations
const hubHeaders = hubId ? { "X-Hub-ID": hubId } : {};
const result = await apiPost("/v1/memories", body, hubHeaders);
```

## Testing

- Unit tests with Vitest: `pnpm --filter memax-cli test`
- Test command logic, not Commander.js parsing
- Mock `apiPost`/`apiGet` for API tests
- Test both TTY and non-TTY output paths

## Anti-Patterns

### Never Do This

| Anti-Pattern                                     | Instead                                                    |
| ------------------------------------------------ | ---------------------------------------------------------- |
| Duplicate `createInterface` + `rl.question`      | Use `lib/prompt.ts`                                        |
| Define `RecallResult` locally                    | Import from `memax-sdk`                                    |
| Copy `detectProjectContext()` into a new command | Import from `lib/project-context.ts`                       |
| Add both `--flag` and subcommand for same action | Pick one — subcommand for nouns, flags for modifiers       |
| `console.log("Error: ...")`                      | `console.error(chalk.red("..."))`                          |
| Swallow errors silently (`catch {}`)             | Log with `chalk.gray` or comment explaining why            |
| Read env vars in command functions               | Read in `lib/` modules, inject via function params         |
| Import from parent command file                  | Use a shared types file to avoid circular deps             |
| Add a dependency without justification           | Prefer stdlib; chalk + commander is enough for most things |

### Command Naming

- **Nouns as subcommands:** `memax sync agents`, `memax auth create-key`
- **Verbs as top-level:** `memax push`, `memax recall`, `memax login`
- **Flags modify behavior:** `--push`, `--pull`, `--format json`, `--hook`
- **No aliases** unless there's a strong user expectation (e.g., `forget` → `delete`)

## Topic Commands

```bash
memax topic list [--hub <slug>]       # Show topic tree
memax topic create <name> [-p parent] # Create topic
memax topic add <mem-id> -t <topic>   # Assign memory
memax topic remove <mem-id> -t <topic># Unassign memory
memax topic delete <topic-id>         # Delete topic
```

## MCP Parity Rule

The CLI MCP server (`commands/mcp.ts`) must expose **identical** tools to the Go server MCP handler (`handler/mcp.go`). When adding a tool or parameter to either side, update both. Current tools: memax_recall, memax_push, memax_get, memax_list, memax_hubs, memax_hub_members, memax_forget, memax_capture, memax_topics.

## No Admin APIs in the CLI

The CLI MUST NOT call `/v1/admin/*` routes or import admin types. Admin is an internal operator surface, served only to the web app via `packages/web/src/lib/admin-client/`. A CLI user is a product user; they get product APIs via `memax-sdk`. If you're about to add an admin-style command ("impersonate", "list all users", "edit plan limits"), stop — that's a web-only feature. See AGENTS.md "Admin Surface Boundary (CRITICAL)" and the `check-sdk-boundary` CI lint that enforces it.

## Checklist: Before Submitting CLI Changes

1. Does it use shared utilities (`lib/prompt.ts`, `lib/api.ts`, `lib/project-context.ts`)?
2. Is the command file under 300 lines? If not, decompose.
3. Does it handle both TTY and pipe output?
4. Does it show actionable error messages?
5. Is there a `--yes` flag for any confirmations?
6. Are reusable API contract types imported from `memax-sdk` (not local duplicates)?
7. Does it follow the indentation and color conventions?
8. `pnpm format && pnpm lint` passes?
