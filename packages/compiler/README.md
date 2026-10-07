# @memaxlabs/compiler

Compiles a reviewed, cited record of a project's decisions and conventions into the files coding agents already read: `AGENTS.md`, a `CLAUDE.md` shim, path-scoped rules for Cursor, Copilot, Windsurf and Claude Code, and project instructions to paste into ChatGPT. It also reads hand edits of those files back as structured proposals.

It's the compiler behind [Memax](https://memax.app), and it stands on its own. The package is pure TypeScript with no runtime dependencies, no I/O and no clock. The same input gives the same bytes on every machine, in Node or in a browser.

```ts
import { compile, parseBack, isDrifted } from "@memaxlabs/compiler";

const result = compile(input); // a CompileInput, described below
for (const file of result.files) write(file.path, file.content);

// Later, when someone edits a compiled file by hand:
if (isDrifted(file.drift_sha256, onDisk)) {
  const { changes, drift } = parseBack(file, onDisk);
  // changes: [{ kind: "edit", ref: "M-0219", old_text, new_text, ... }, ...]
}
```

## One canonical file, thin shims

In October 2026, nearly every coding agent reads `AGENTS.md`. Claude Code reads it only when there's no `CLAUDE.md`, and Copilot also reads a root `CLAUDE.md` and `GEMINI.md`. Writing the full record into four files would make several tools load it twice and give you four files to drift. So the compiler writes the record once, into `AGENTS.md`:

- Tools with an import syntax get a **shim** that imports it.
- Tools with path-scoped rules get **scoped files**, for path-scoped facts only.
- An unscoped record produces no Cursor, Copilot or Windsurf file at all.

```markdown
<!-- Compiled by Memax from memax-v2 at 2026-10-05 14:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->

# Memax V2 engineering brief

What every agent on this project reads before it writes code.
Every line cites a memory. Ask the memax MCP server for anything else.

## Decisions

- Background jobs run on River, Postgres-backed. We do not use Temporal. [M-0219]
- (Being verified) Ask memax answers with the Haiku tier. [M-0187]

## Conventions

- pnpm workspaces only. Never run `npm install` at the root. [M-0071]

## Open

- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]

## Live context

Use the memax MCP server for anything not here:
memax_recall, memax_search, memax_get. Propose with memax_push.
```

The full example is in [`test/fixtures/demo-memax-v2`](test/fixtures/demo-memax-v2).

## Adapters

| Kind           | Writes                                                       | Format                                                                  | How the tool reads it                                                                                                                          | Limits                                                                            | Default  |
| -------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | -------- |
| `agents_md`    | `AGENTS.md`                                                  | Header, `# title`, `## sections` of `- fact [M-id]`, `## Live context`  | Codex, Cursor, Copilot, Windsurf/Devin, OpenCode and others read it natively. Codex concatenates one file per directory from the git root down | Codex's `project_doc_max_bytes`: **32 KiB** in total. The budget never exceeds it | on       |
| `claude_md`    | `CLAUDE.md` (or `CLAUDE.local.md`)                           | Header, `@AGENTS.md`, then Claude-only facts                            | Claude Code resolves `@` imports up to 4 hops and dedupes `@AGENTS.md`. With a person's own file, one managed block                            | Claude Code suggests under 200 lines (warning)                                    | on       |
| `cursor_mdc`   | `.cursor/rules/memax-<area>.mdc`, one per group of globs     | Frontmatter `description`, `globs` (comma string), `alwaysApply: false` | Attached when matching files are in play. Plain `.md` is ignored there. Reads AGENTS.md for the rest                                           | Cursor suggests under 500 lines (warning)                                         | on       |
| `chatgpt`      | Nothing on disk: text to copy into a ChatGPT project         | Plain text with `- fact [M-id]` lines                                   | There's no API for writing project instructions; ChatGPT reads the record live through the memax connector                                     | None documented                                                                   | on       |
| `gemini_md`    | `GEMINI.md`                                                  | Header, `@./AGENTS.md`, then Gemini-only facts                          | Gemini CLI doesn't read AGENTS.md by default, but resolves `@./file.md` imports (5 levels, inside the project)                                 | —                                                                                 | opt-in   |
| `copilot`      | `.github/instructions/memax-<area>.instructions.md`          | Frontmatter `applyTo: "glob,glob"`                                      | Applied to matching files. Reads AGENTS.md (and root CLAUDE.md, GEMINI.md) for the rest                                                        | "No longer than 2 pages"                                                          | off (P3) |
| `windsurf`     | `.devin/rules/memax-<area>.md` (`.windsurf/rules` works too) | Frontmatter `trigger: glob`, `globs`                                    | Applied to matching files. Reads AGENTS.md for the rest                                                                                        | **12,000 characters per file**, enforced                                          | off (P3) |
| `claude_rules` | `.claude/rules/memax-<area>.md`                              | Frontmatter `paths:` as a quoted YAML list                              | Claude Code loads the rule for matching paths. Invalid YAML would make it load always, so globs are always quoted                              | Claude Code suggests under 200 lines (warning)                                    | off (P3) |

The P3 adapters render, parse and pass the same tests as the rest. They're left out of `defaultTargets()` until their phase. `gemini_md` stays out for good (`OPT_IN_TARGET_KINDS`): Antigravity CLI, which replaced Gemini CLI for most people, reads AGENTS.md, so the shim is compiled only for a space that asks for it (`memax connect gemini --gemini-md`). Every adapter has a `version`, its `limits` and the tool it's written for, so a caller can show "Cursor · reads AGENTS.md" when a target writes no files.

## The input

A `CompileInput` is plain JSON. It's validated in full, every problem is reported at once (`CompileInputError.issues`), and unknown fields are ignored.

```jsonc
{
  "version": 1,
  "compile": { "id": "C-0881", "at": "2026-10-05T14:31:00Z" },
  "space": {
    "slug": "memax-v2",
    "name": "Memax V2",
    "kind": "project",
    "url": "https://memax.app/memax-v2/brief",
  },
  "brief": {
    "id": "B-0043",
    "title": "Memax V2 engineering brief",
    "summary": "What every agent on this project reads before it writes code.",
    "sections": [
      {
        "key": "decisions",
        "heading": "Decisions",
        "items": [{ "ref": "M-0219" }],
      },
      {
        "key": "open",
        "heading": "Open",
        "items": [
          {
            "text": "Fly.io or Railway is undecided.",
            "cites": ["M-0431", "M-0174"],
          },
        ],
      },
    ],
  },
  "memories": [
    {
      "ref": "M-0219",
      "statement": "Background jobs run on River, Postgres-backed. We do not use Temporal.",
      "section": "decisions",
      "kind": "decision", // fact | decision
      "state": "kept", // kept | open (an open question)
      "flags": [], // stale | conflict
      "trust": "person", // person | agent_own_work | repository
      "scope": { "paths": ["packages/web/**"], "agents": ["claude-code"] },
      "read_score": 41, // reads over 30 days, with decay
      "stale_after": "2026-12-31T00:00:00Z",
    },
  ],
  "targets": [
    {
      "kind": "agents_md",
      "include": "kept_and_open",
      "stale": "mark",
      "size_budget": 25600,
    },
    { "kind": "claude_md", "user_owned": true, "current": "# My notes\n" },
    { "kind": "cursor_mdc" },
    { "kind": "chatgpt" },
  ],
}
```

- **Brief items** are memory references or short lines of connective prose that cite what they rest on. Prose must cite at least one memory.
- **Memories** are only what may compile: kept memories and open questions. Proposals, rejected, faded and forgotten memories never go in, and neither does anything from an external source (it's quarantined). The compiler refuses an input that carries any of them, rather than quietly dropping them.
- **Target settings:**

  | Setting          | Applies to              | Default            | Meaning                                                                                     |
  | ---------------- | ----------------------- | ------------------ | ------------------------------------------------------------------------------------------- |
  | `path`           | all but `chatgpt`       | per adapter        | A file for `agents_md` and the shims, a directory for scoped adapters                       |
  | `include`        | all                     | `kept_and_open`    | `kept_only` leaves out the `open` section and every open question                           |
  | `stale`          | all                     | `mark`             | `mark` writes `(Being verified)`; `omit` leaves stale facts out                             |
  | `size_budget`    | all                     | 32 KiB             | Bytes per file, at least 1,024, capped by the tool's own limit                              |
  | `sections`       | all                     | all but `overview` | Section keys to compile, in Brief order                                                     |
  | `scoped`         | `agents_md`, `chatgpt`  | `inline`           | Path-scoped facts in `### In <globs>` subsections, or `omit` when scoped targets carry them |
  | `canonical_path` | shims, scoped           | `AGENTS.md`        | The canonical file a shim imports and a scoped tool also reads                              |
  | `user_owned`     | shims                   | `false`            | The person owns the file; Memax manages one block in it                                     |
  | `current`        | shims with `user_owned` | `""`               | The file's content now                                                                      |

## The output

`compile` returns a `CompileResult`:

- `files`: every file to write, sorted by path. Each one has `content`, `bytes`, `lines`, `sha256` and `drift_sha256` (what drift checks compare). It also lists `refs` (memories whose statements it contains), `cites` (every ref cited in it) and `dropped_for_budget`.
- `copies`: text for a person to copy out (the ChatGPT instructions), never written to disk.
- `targets`: one summary per target, with `delivery` (`file` or `copy`), `reads` (the canonical file it also reads), `files`, `refs` and `dropped_for_budget`.
- `warnings`: for example `AGENTS.md at 31.2 KiB is near Codex's 32 KiB cap.`, `3 facts didn't fit the 25 KiB budget for AGENTS.md. The rest stay live over MCP.`, or `Removed 2 hidden characters from M-0219.`

## How a file is written

- **The header.** Every file starts (after any frontmatter) with exactly one quiet header line. It names the space, where to edit it, and the compile ID and time from the input. There's no other timestamp.
- **Placement.** Sections render in Brief order, and each memory renders where the Brief places it. A memory the Brief only cites in prose is represented by that prose. A memory kept after the last Brief version goes to the end of its own section, so a Keep reaches every file without waiting for a new Brief.
- **Short beats complete.** The `overview` section is left out by default: context files help most with what's non-standard.
- **The budget.** Facts are ranked by `read_score` within each section, and the fill takes every section's best fact, then every section's second-best, and so on. Each fact that fits is kept. Sections share the budget, the most-read facts go first, and the file never goes over. What didn't fit is listed and stays available over MCP.
- **States.** Stale facts (flagged, or past `stale_after`) are marked `(Being verified)` or left out. Facts in conflict are marked `(In conflict)`. Open questions compile only with `kept_and_open`.
- **Scope.** A memory's `scope.paths` sends it to the scoped files and, by default, to a `### In <globs>` subsection of `AGENTS.md`. Codex and other tools without scoped rules still see it there. Scoped files are named after their globs (`packages/web/**` → `memax-packages-web`). A memory's `scope.agents` limits it to that agent's own files: `CLAUDE.md` for `claude-code`, `GEMINI.md` for `gemini`, and the scoped files of Cursor, Copilot and Windsurf. It never goes into the shared `AGENTS.md`.
- **Bytes.** LF line endings, a trailing newline and no trailing whitespace. Each fact is one line, so line numbers map back to memories. The one exception is a file the person owns, which keeps its own line endings.

### A `CLAUDE.md` the person owns

With `user_owned`, Memax writes one block between `<!-- memax:start -->` and `<!-- memax:end -->`, which are the markers the V1 CLI already writes. If there's no block yet, it's appended after a blank line. If there is one, it's replaced in place. Every byte outside the markers stays as it was, and running it again changes nothing. `upsertManagedBlock`, `removeManagedBlock` and `findManagedBlock` are exported for callers that need them directly.

## Security

- **Hidden characters.** Every line of text is cleaned before it's written. The compiler removes control characters, format characters (bidi embeddings, overrides and isolates, zero-width characters, the BOM, soft hyphens, tag characters), variation selectors and invisible fillers. That's the "rules file backdoor" and ASCII-smuggling class of attack. Line breaks inside a statement become spaces, so a statement can't add a heading or a line. Each removal is reported as a warning, and parse-back strips the same characters from what it reads.
- **Proposals and quarantined content never compile.** The input is refused if it carries them.
- **Validated headers and frontmatter.** Slugs, URLs and globs are checked, so nothing can close the header comment or break the frontmatter. Globs can't contain spaces, commas, braces, quotes, colons or `#`, and can't climb out of the repository.

## Parse-back

`parseBack(lastCompiled, currentContent)` compares a file with the last compile and returns `{ changes, drift }`.

| The person…                                       | Change                                                                          |
| ------------------------------------------------- | ------------------------------------------------------------------------------- |
| changed the words of a line that carries `[M-id]` | `edit` of that memory, with `old_text`, `new_text` and both line numbers        |
| added a line with no matching cite                | `new` proposal, with its line, the section heading and any globs it falls under |
| removed a cited line                              | `remove`: a proposal to forget or exclude, **never** an automatic forget        |
| moved lines, within or across sections            | nothing                                                                         |
| removed only the cite, keeping the words          | nothing                                                                         |

Line endings, a BOM, trailing whitespace, list markers (`-`, `*`, `+`, `1.`), indented continuation lines and Memax's own markers don't count as edits. A cited line with several cites (connective prose) reports them all in `refs`. `drift` reports whether the content changed, and whether the header, the frontmatter, the layout (headings, imports, Live context) or the managed block were edited. It also reports how many hidden characters the file now holds.

`isDrifted(lastDeliveredSha, currentContent)` says whether a file changed since it was delivered. Line endings and a BOM don't count. Pass the file's `drift_sha256`; for a file the person owns, only the managed block counts.

## Design notes

- **Zero runtime dependencies.** SHA-256 comes from `node:crypto`, reached through `process.getBuiltinModule` so nothing imports a Node module. Where that's missing (browsers, edge runtimes), a small FIPS 180-4 implementation takes over. Web Crypto isn't used, because `crypto.subtle.digest` is async and `compile` stays synchronous. Tests check both paths against each other.
- **Isomorphic by construction.** `src` compiles with `types: []`, so a Node-only API can't creep in.
- **Fast.** The demo compiles in about 0.3 ms. 400 facts through all eight adapters take under 10 ms at p99.

## Development

```bash
pnpm --filter @memaxlabs/compiler test     # vitest
pnpm --filter @memaxlabs/compiler lint     # eslint, tsc (src and tests)
pnpm --filter @memaxlabs/compiler build    # tsc to dist/
UPDATE_GOLDEN=1 pnpm --filter @memaxlabs/compiler test   # regenerate golden files, then review the diff
```

Golden fixtures live in `test/fixtures/<case>/`, as `input.json` plus `expected/`. Expected file names are the output paths with `/` written as `__` (the repository ignores `.claude/` directories), and copy-out text is `copy__<label>.txt`. `result.json` holds the result without file contents. Prettier skips the fixtures, so the bytes stay exact.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
