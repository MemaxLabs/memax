// Copied verbatim from packages/agents/src/types.ts by scripts/sync-agents.mjs.
// Don't edit it here: change @memaxlabs/agents, then run the script.

/**
 * The agents Memax connects, as the API's `AgentKind` names them (its
 * `other` is any agent Memax doesn't know by name, so it isn't here).
 */
export type AgentId =
  | "claude-code"
  | "codex"
  | "cursor"
  | "gemini-cli"
  | "copilot"
  | "opencode"
  | "windsurf"
  | "chatgpt"
  | "claude";

/** Where an agent runs. */
export type Surface = "cli" | "ide" | "cloud" | "chat";

/** How an agent wants what its session-start hook prints. */
export type HookFormat = "plain" | "gemini" | "cursor" | "copilot";

/** The Memax file kinds the registry names (the API's `TargetKind`s). */
export type FileKind =
  | "agents_md"
  | "claude_md"
  | "gemini_md"
  | "cursor_mdc"
  | "claude_rules"
  | "copilot"
  | "windsurf"
  | "chatgpt"
  | "claude_user"
  | "codex_user"
  | "gemini_user"
  | "copilot_user"
  | "windsurf_user";

/** An agent's session-start hook, where `memax connect` installs it. */
export interface Hook {
  /** Its settings file, under the home folder, with forward slashes. */
  file: string;
  /** The event it runs on, as the settings file names it. */
  event: string;
  format: HookFormat;
  /** The most tokens the block it prints may take. */
  maxTokens: number;
  /** The repository files it loads at session start, a shim first. */
  loads: readonly FileKind[];
  /**
   * Home files it loads no file of: the hook prints their kept lines in
   * full at every session start (Cursor, the canonical copy).
   */
  prints: readonly FileKind[];
  /** Its hook documentation. */
  docs: string;
}

/** A hook committed to a repository, which the agent's cloud sandbox runs. */
export interface CloudHook {
  /** Repository-relative, with forward slashes. */
  file: string;
  event: string;
}

/** Where an agent keeps its MCP servers, as `memax connect` writes them. */
export interface McpSettings {
  /** Under the home folder, or in the repository. */
  root: "home" | "repo";
  /** Relative to the root, with forward slashes. */
  file: string;
  format: "json" | "toml";
  /** The key its servers go under. */
  key: string;
}

/** The user-level file Memax writes the Personal space into for an agent. */
export interface HomeFile {
  kind: FileKind;
  /** Its default path, relative to the home folder. */
  path: string;
}

export interface Agent {
  id: AgentId;
  /**
   * Its short key, where it differs from `id`: in a memory's
   * `scope.agents`, Ledger's marks and the CLI's arguments (`gemini`).
   */
  key: string;
  /** Its name, a proper noun, never translated. */
  name: string;
  /** Its two- or three-letter mark: a monogram, never a vendor's logo. */
  mark: string;
  surface: Surface;
  /**
   * The autonomy it starts at in a new space (plan 25 §7.3): Propose, and
   * Read for Cursor and Gemini CLI; a person raises it later.
   */
  start: "read" | "propose";
  /** Whether `memax init` and `memax connect` know it. */
  cli: boolean;
  /** It connects through its connector, in a browser, not a local install. */
  connector: boolean;
  /** Where people see it lives: `~/.claude`, or `connector`. */
  where: string;
  /** What shows it is installed, without running it. */
  detect: {
    /** Folders under the home folder, with forward slashes. */
    home: readonly string[];
    /** Folders in the repository. */
    repo: readonly string[];
    /** Commands on PATH. */
    commands: readonly string[];
  };
  /** Its own user-level file for the Personal space, if it has one. */
  home: HomeFile | null;
  hook: Hook | null;
  cloudHook: CloudHook | null;
  mcp: McpSettings | null;
}

/** How a cell of the load matrix was established, best first. */
export type Method = "run" | "source" | "docs";

/**
 * Whether a tool loads a file: `yes` every session; `on_match` once it
 * works with a file the rule's globs match; `text` loaded with its `@`
 * lines left as text; `import` only through another file's import;
 * `when` only under the condition in `note`; `hook` only through
 * Memax's session-start hook; `unverified` when no run, source or doc
 * settles it.
 */
export type Loads =
  | "yes"
  | "no"
  | "on_match"
  | "text"
  | "import"
  | "when"
  | "hook"
  | "unverified";

/** A limit the tool documents for a file it loads. */
export interface Cap {
  bytes?: number;
  chars?: number;
  lines?: number;
}

export interface Cell {
  loads: Loads;
  note?: string;
  cap?: Cap;
}

/** The repository files of the load matrix: Memax's repository targets. */
export type RepoFile =
  | "agents_md"
  | "claude_md"
  | "gemini_md"
  | "cursor_mdc"
  | "claude_rules"
  | "copilot"
  | "windsurf";

/** The user-level files of the load matrix: what Memax writes on a linked machine. */
export type UserFile =
  | "canonical"
  | "claude_rules_file"
  | "claude_md_block"
  | "codex_agents_md"
  | "gemini_agents_md"
  | "copilot_instructions"
  | "windsurf_global_rules";

/** One row of the load matrix: a tool and which of Memax's files it loads. */
export interface Reader {
  /** `claude-code`, or a tool's own id where it isn't a connection (`antigravity`). */
  id: string;
  name: string;
  /** The connection it is, when it is one. */
  agent?: AgentId;
  /** Its mark and surface, for a tool that isn't a connection but is shown. */
  mark?: string;
  surface?: Surface;
  repo: Partial<Record<RepoFile, Cell>>;
  user: Partial<Record<UserFile, Cell>>;
  method: readonly Method[];
  /** What was run, or the version read. */
  measured: string;
  /** Docs, and source files with lines, behind each cell. */
  sources: readonly string[];
}
