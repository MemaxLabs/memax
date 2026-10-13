// Copied verbatim from packages/agents/src/loads.ts by scripts/sync-agents.mjs.
// Don't edit it here: change @memaxlabs/agents, then run the script.

// The load matrix: which of Memax's files each tool loads, at repository
// and at user level, with how each cell was established (memax-internal
// docs/v2/adapter-load-matrix-2026-10.md, Oct 11, 2026, and the Part B
// runs after it). Every cell comes from a run of the real tool, its
// source at a pinned version, or its official docs, in that order of
// preference; `unverified` means none of the three settles it.
//
// What it settled: every agent reads AGENTS.md, which carries path-scoped
// facts inline, so every scoped rules format is opt-in (a second copy of
// each scoped fact otherwise), and each user-level file is read by the
// agents meant to share it, with three exceptions said in the cells
// (VS Code's Local agent, OpenCode's fallback, Gemini CLI's two ways in).
import type {
  Cap,
  Cell,
  Reader,
  RepoFile,
  Surface,
  UserFile,
} from "./types.js";

/** Where each file of the matrix is, for people to read. */
export const REPO_FILES: Readonly<Record<RepoFile, string>> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
  cursor_mdc: ".cursor/rules/*.mdc",
  claude_rules: ".claude/rules/*.md",
  copilot: ".github/instructions",
  windsurf: ".devin/rules, .windsurf/rules",
};

export const USER_FILES: Readonly<Record<UserFile, string>> = {
  canonical: "~/.memax/personal/AGENTS.md",
  claude_rules_file: "~/.claude/rules/memax-personal.md",
  claude_md_block: "~/.claude/CLAUDE.md",
  codex_agents_md: "~/.codex/AGENTS.md",
  gemini_agents_md: "~/.gemini/AGENTS.md",
  copilot_instructions: "~/.copilot/copilot-instructions.md",
  windsurf_global_rules: "~/.codeium/windsurf/memories/global_rules.md",
};

const no: Cell = { loads: "no" };
const yes = (note?: string, cap?: Cap): Cell => ({
  loads: "yes",
  ...(note ? { note } : {}),
  ...(cap ? { cap } : {}),
});

/** Claude Code's guidance for an instructions file: under 200 lines. */
const CLAUDE_LINES: Cap = { lines: 200 };

export const READERS: readonly Reader[] = [
  {
    id: "claude-code",
    name: "Claude Code",
    agent: "claude-code",
    repo: {
      agents_md: {
        loads: "import",
        note: "through the CLAUDE.md shim's import",
      },
      claude_md: yes("`@AGENTS.md` inlined", CLAUDE_LINES),
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: { loads: "on_match", cap: CLAUDE_LINES },
      copilot: no,
      windsurf: no,
    },
    user: {
      canonical: no,
      claude_rules_file: yes("every session, no frontmatter", CLAUDE_LINES),
      claude_md_block: yes(undefined, CLAUDE_LINES),
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["run"],
    measured:
      "Claude Code 2.1.295: `claude -p` with a scratch CLAUDE_CONFIG_DIR and an InstructionsLoaded hook logging every file it loads and why",
    sources: [
      "https://code.claude.com/docs/en/memory: Claude reads AGENTS.md only when there's no CLAUDE.md; .claude/rules and ~/.claude/CLAUDE.md keep loading alongside; user rules load before project rules; guidance under 200 lines",
      "https://code.claude.com/docs/en/hooks: InstructionsLoaded, load_reason include and path_glob_match",
      "run: session_start User …/rules/memax-personal.md, session_start Project CLAUDE.md, include Project AGENTS.md, path_glob_match Project .claude/rules/memax-src-web.md",
    ],
  },
  {
    id: "codex",
    name: "Codex",
    agent: "codex",
    repo: {
      agents_md: yes("git root to the working directory", { bytes: 32 * 1024 }),
      claude_md: no,
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: no,
      copilot: no,
      windsurf: no,
    },
    user: {
      canonical: no,
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: yes(
        "the first non-empty of AGENTS.override.md and AGENTS.md",
      ),
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["run", "source"],
    measured:
      "Codex 0.160.1: `codex exec` with a scratch CODEX_HOME; the session file records the one instructions message Codex sent",
    sources: [
      "codex rust-v0.160.1 (c3e23d4) codex-rs/core/src/agents_md.rs L43–45 (AGENTS.md, AGENTS.override.md), L64 (untrusted projects skip project files), L68 (project_doc_max_bytes, 32 KiB), L272–276 (candidates: override, AGENTS.md, then project_doc_fallback_filenames)",
      "codex-rs/codex-home/src/instructions/mod.rs L43 (override first), L69–70 (first non-empty)",
    ],
  },
  {
    id: "cursor",
    name: "Cursor",
    agent: "cursor",
    repo: {
      agents_md: yes("nested files too"),
      claude_md: { loads: "text", note: "`@` mentions aren't inlined" },
      gemini_md: no,
      cursor_mdc: { loads: "on_match", cap: { lines: 500 } },
      claude_rules: no,
      copilot: no,
      windsurf: no,
    },
    user: {
      canonical: {
        loads: "hook",
        note: "its User Rules aren't a file; the session-start hook prints the canonical's kept lines",
      },
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["run", "docs"],
    measured:
      "cursor-agent 2026.10.01: `-p --trust` with and without reading the matching file and with the import probe; with a scratch home holding every user-level file (none loaded), and a sessionStart hook's additional_context (it reached the model)",
    sources: [
      "https://cursor.com/docs/rules.md: .mdc only in .cursor/rules; a file's contents are not inlined; nested AGENTS.md; User Rules are settings",
      "https://cursor.com/help/customization/rules.md: Cursor reads CLAUDE.md the way it reads AGENTS.md, always applied; user rule files in ~/.cursor/rules (called a bug by Cursor staff, Apr 2026)",
      "https://cursor.com/docs/hooks.md L1322–1352: sessionStart returns additional_context, added to the conversation's initial system context",
    ],
  },
  {
    id: "vscode-local",
    name: "VS Code, Local agent",
    repo: {
      agents_md: yes("chat.useAgentsMdFile"),
      claude_md: { loads: "text", note: "chat.useClaudeMdFile" },
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: { loads: "on_match" },
      copilot: { loads: "on_match" },
      windsurf: no,
    },
    user: {
      canonical: no,
      claude_rules_file: yes("`paths` absent means `**`"),
      claude_md_block: yes("chat.useClaudeMdFile"),
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: yes(
        "beside ~/.claude/rules, so it loads the Personal space twice where both are written",
      ),
      windsurf_global_rules: no,
    },
    method: ["source", "docs"],
    measured: "source at microsoft/vscode 32bbe1b (Oct 11) and docs",
    sources: [
      "src/vs/workbench/contrib/chat/common/promptSyntax/config/promptFileLocations.ts L184–189 (instruction folders: .github/instructions, .claude/rules, ~/.copilot/instructions, ~/.claude/rules)",
      "common/promptSyntax/promptFileParser.ts L639–644 (Claude rules use `paths`, defaulting to `**`)",
      "common/promptSyntax/computeAutomaticInstructions.ts L193–213, L266–290 (agent files always added)",
      "common/promptSyntax/service/promptsServiceImpl.ts L883–917 (AGENTS.md, CLAUDE.md, ~/.claude/CLAUDE.md, ~/.copilot/copilot-instructions.md)",
      "browser/chat.shared.contribution.ts L2139, L2157 (chat.useAgentsMdFile, chat.useClaudeMdFile: true)",
      "https://code.visualstudio.com/docs/agent-customization/custom-instructions",
    ],
  },
  {
    id: "vscode-claude",
    name: "VS Code, Claude harness",
    repo: {
      agents_md: { loads: "import", note: "as Claude Code" },
      claude_md: yes("as Claude Code"),
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: { loads: "on_match" },
      copilot: no,
      windsurf: no,
    },
    user: { claude_rules_file: yes("as Claude Code") },
    method: ["docs"],
    measured: "docs",
    sources: [
      "https://code.visualstudio.com/docs/agent-customization/custom-instructions: the Claude harness reads ~/.claude/rules",
    ],
  },
  {
    id: "vscode-copilot",
    name: "VS Code, Copilot harness",
    repo: {
      agents_md: yes(),
      claude_md: { loads: "unverified" },
      gemini_md: { loads: "unverified" },
      cursor_mdc: no,
      claude_rules: no,
      copilot: { loads: "on_match" },
      windsurf: no,
    },
    user: { copilot_instructions: yes() },
    method: ["docs"],
    measured: "docs",
    sources: [
      "https://code.visualstudio.com/docs/agent-customization/custom-instructions: for personal, always-on instructions in Copilot Agent Host sessions, use ~/.copilot/copilot-instructions.md",
    ],
  },
  {
    id: "copilot",
    name: "Copilot CLI",
    agent: "copilot",
    repo: {
      agents_md: yes(),
      claude_md: yes("`@` expanded"),
      gemini_md: yes("`@` not expanded"),
      cursor_mdc: no,
      claude_rules: { loads: "on_match" },
      copilot: { loads: "on_match" },
      windsurf: no,
    },
    user: {
      canonical: no,
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: yes(),
      windsurf_global_rules: no,
    },
    method: ["run", "docs"],
    measured:
      "Copilot CLI 1.0.95: `copilot instruction list --json` (works signed out) in the repository and with a scratch HOME",
    sources: [
      "https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions: `@` reads the referenced file immediately; absolute and ~/ paths aren't loaded; not expanded in GEMINI.md or *.instructions.md; removes duplicate copies of identical files",
      "run: repository AGENTS.md, CLAUDE.md, GEMINI.md, .github/instructions and .claude/rules; personal ~/.copilot/copilot-instructions.md and ~/.copilot/instructions only",
    ],
  },
  {
    id: "gemini-cli",
    name: "Gemini CLI",
    agent: "gemini-cli",
    repo: {
      agents_md: {
        loads: "when",
        note: "only when its context.fileName lists AGENTS.md (memax connect gemini --agents-md)",
      },
      claude_md: no,
      gemini_md: yes("`@./AGENTS.md` inlined"),
      cursor_mdc: no,
      claude_rules: no,
      copilot: no,
      windsurf: no,
    },
    user: {
      canonical: no,
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: no,
      gemini_agents_md: {
        loads: "when",
        note: "only when its context.fileName lists AGENTS.md",
      },
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["source"],
    measured:
      "source at google-gemini/gemini-cli 9b6e026 (Oct 9); not installed",
    sources: [
      "packages/core/src/tools/memoryTool.ts L11 (GEMINI.md default), L83–88 (context.fileName list)",
      "packages/core/src/utils/memoryDiscovery.ts L209–233 (each file's imports processed separately), L317–340 (global ~/.gemini/<name> per file name)",
      "packages/core/src/utils/memoryImportProcessor.ts L195–196, L336",
      "packages/core/src/utils/paths.ts L19–27 (GEMINI_CLI_HOME); packages/cli/src/config/settingsSchema.ts L1395 (context.fileName)",
    ],
  },
  {
    id: "antigravity",
    name: "Antigravity",
    mark: "AG",
    surface: "ide",
    repo: {
      agents_md: yes(),
      claude_md: no,
      gemini_md: { loads: "text", note: "`@` not inlined" },
      cursor_mdc: no,
      claude_rules: no,
      copilot: no,
      windsurf: { loads: "no", note: ".agents/rules only" },
    },
    user: {
      canonical: no,
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: no,
      gemini_agents_md: yes("always on, beside ~/.gemini/GEMINI.md", {
        bytes: 24_000,
      }),
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["docs"],
    measured: "official docs; not installed",
    sources: [
      "https://antigravity.google/docs/rules.md: repository AGENTS.md, GEMINI.md and .agents/rules; global ~/.gemini/AGENTS.md and ~/.gemini/GEMINI.md; `@filename` does not inline file contents; 24,000 bytes per file; 20,000 tokens across global and always-on rules",
    ],
  },
  {
    id: "windsurf",
    name: "Windsurf / Devin Desktop",
    agent: "windsurf",
    repo: {
      agents_md: yes("subfolder files act as globs"),
      claude_md: { loads: "unverified", note: "not documented" },
      gemini_md: no,
      cursor_mdc: { loads: "unverified", note: "not documented" },
      claude_rules: { loads: "unverified", note: "not documented" },
      copilot: no,
      windsurf: { loads: "on_match", cap: { chars: 12_000 } },
    },
    user: {
      canonical: no,
      claude_rules_file: no,
      claude_md_block: no,
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: yes("the person's own rules included", {
        chars: 6_000,
      }),
    },
    method: ["docs"],
    measured: "official docs; not installed",
    sources: [
      "https://docs.devin.ai/desktop/cascade/memories.md: global_rules.md limited to 6,000 characters; workspace rules 12,000 each; .devin/rules over .windsurf/rules",
      "https://docs.devin.ai/desktop/cascade/agents-md.md: subfolder files act as <directory>/** globs",
    ],
  },
  {
    id: "devin",
    name: "Devin CLI, Devin Local",
    repo: {
      agents_md: yes(),
      claude_md: yes("always on; `@` handling not documented"),
      gemini_md: no,
      cursor_mdc: { loads: "on_match" },
      claude_rules: yes("reads the .claude/ directory"),
      copilot: no,
      windsurf: { loads: "on_match" },
    },
    user: {
      canonical: no,
      claude_rules_file: {
        loads: "unverified",
        note: ".claude/ is documented for projects",
      },
      claude_md_block: yes(),
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["docs"],
    measured: "official docs; not installed",
    sources: [
      "https://docs.devin.ai/cli/extensibility/rules.md: AGENTS.md and CLAUDE.md treated identically, always on; reads .cursor/rules/*.md and *.mdc; reads the .claude/ directory; .devin/rules and .windsurf/rules both loaded; global ~/.config/devin/AGENTS.md and ~/.claude/CLAUDE.md",
      "https://docs.devin.ai/desktop/devin-local.md: shares Devin CLI's harness and rules",
    ],
  },
  {
    id: "opencode",
    name: "OpenCode",
    agent: "opencode",
    repo: {
      agents_md: yes("the first match; it stops there"),
      claude_md: { loads: "no", note: "read only when there's no AGENTS.md" },
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: no,
      copilot: no,
      windsurf: no,
    },
    user: {
      canonical: {
        loads: "when",
        note: "through its instructions setting, once memax connect opencode names it",
      },
      claude_rules_file: no,
      claude_md_block: {
        loads: "when",
        note: "while there's no ~/.config/opencode/AGENTS.md and OPENCODE_DISABLE_CLAUDE_CODE(_PROMPT) isn't set, so memax connect opencode writes no entry then",
      },
      codex_agents_md: no,
      gemini_agents_md: no,
      copilot_instructions: no,
      windsurf_global_rules: no,
    },
    method: ["run", "source"],
    measured:
      "OpenCode 1.18.35 (`opencode run` with a scratch HOME) and 1.19.0 against a local server recording the system prompt",
    sources: [
      "opencode v1.18.35 (53d1eab) packages/opencode/src/session/instruction.ts L60–68 (global ~/.config/opencode/AGENTS.md, else ~/.claude/CLAUDE.md; project AGENTS.md, else CLAUDE.md), L110–152 (systemPaths: one Set of the first global, the first project match and instructions with ~/ and absolute paths), L155–169 (files read raw)",
      "packages/opencode/src/config/config.ts L272–274 (config.json, opencode.json, opencode.jsonc merged; a later list replaces an earlier one), L240 (JSONC)",
    ],
  },
  {
    id: "junie",
    name: "Junie",
    repo: {
      agents_md: yes("unless .junie/AGENTS.md exists"),
      claude_md: no,
      gemini_md: no,
      cursor_mdc: no,
      claude_rules: no,
      copilot: no,
      windsurf: no,
    },
    user: {},
    method: ["docs"],
    measured: "official docs (Oct 8); not installed",
    sources: [
      "https://junie.jetbrains.com/docs/guidelines-and-memory.html: .junie/AGENTS.md, else AGENTS.md with .junie/rules, else .junie/guidelines.md; global ~/.junie/AGENTS.md",
    ],
  },
];

/** The tool a matrix row is about, or undefined. */
export function readerOf(id: string): Reader | undefined {
  return READERS.find((r) => r.id === id);
}

/**
 * The limit a tool documents for one of its files; throws where the
 * matrix records none, so a caller can't silently lose a limit.
 */
export function capOf(
  id: string,
  file: { repo: RepoFile } | { user: UserFile },
): Cap {
  const r = readerOf(id);
  const cell = "repo" in file ? r?.repo[file.repo] : r?.user[file.user];
  if (!cell?.cap) throw new Error(`no cap for ${id} ${JSON.stringify(file)}`);
  return cell.cap;
}

/** A tool's mark and surface, for one that is shown but isn't a connection. */
export function readerMark(
  id: string,
): { mark: string; name: string; surface: Surface } | undefined {
  const r = readerOf(id);
  return r?.mark && r.surface
    ? { mark: r.mark, name: r.name, surface: r.surface }
    : undefined;
}
