// The agents whose session-start hook runs `memax hook session-start`,
// and how each wants the block (checked against each agent's hook docs,
// Oct 2026; see docs-site cli/hook.mdx for the links):
//
// - Claude Code: plain stdout is added to the context (capped at 10,000
//   characters).
// - Codex: plain stdout is added as developer context (about 2,500 tokens
//   by default, `additionalContextLimit`); it must not start with { or [.
// - Gemini CLI: JSON only, `hookSpecificOutput.additionalContext`.
// - Cursor: JSON only, `additional_context`.
// - Copilot CLI: JSON only, `additionalContext`.
//
// Which compiled files each loads at session start: AGENTS.md, plus its
// own shim when the space compiles one (CLAUDE.md, GEMINI.md). Scoped
// rules load only when a matching path is touched, so they aren't counted.

export type HookFormat = "plain" | "gemini" | "cursor" | "copilot";

export interface HookAgent {
  /** The agent kind the server knows it by (AgentKind). */
  kind: string;
  format: HookFormat;
  /** The target kinds it loads at session start, shim first. */
  loads: string[];
  /** Most tokens the block may take. */
  maxTokens: number;
}

const AGENTS: Record<string, HookAgent> = {
  "claude-code": {
    kind: "claude-code",
    format: "plain",
    loads: ["claude_md", "agents_md"],
    maxTokens: 3000,
  },
  codex: {
    kind: "codex",
    format: "plain",
    loads: ["agents_md"],
    maxTokens: 2400,
  },
  "gemini-cli": {
    kind: "gemini-cli",
    format: "gemini",
    loads: ["gemini_md", "agents_md"],
    maxTokens: 3000,
  },
  cursor: {
    kind: "cursor",
    format: "cursor",
    loads: ["agents_md"],
    maxTokens: 3000,
  },
  copilot: {
    kind: "copilot",
    format: "copilot",
    loads: ["agents_md"],
    maxTokens: 3000,
  },
};

const ALIASES: Record<string, string> = {
  claude: "claude-code",
  "claude-code": "claude-code",
  codex: "codex",
  gemini: "gemini-cli",
  "gemini-cli": "gemini-cli",
  cursor: "cursor",
  copilot: "copilot",
};

/** The agent a `--agent` value names; unknown agents get plain text and AGENTS.md. */
export function hookAgent(id: string | undefined): HookAgent {
  const key = ALIASES[(id ?? "").toLowerCase()];
  if (key) return AGENTS[key];
  return {
    kind: "other",
    format: "plain",
    loads: ["agents_md"],
    maxTokens: 3000,
  };
}

/** Default paths, for a repository whose targets aren't cached here. */
export const DEFAULT_PATHS: Record<string, string> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
};

/** Wraps the block the way the agent reads it; "" stays "". */
export function wrapFor(format: HookFormat, block: string): string {
  if (block === "") return "";
  switch (format) {
    case "plain":
      return block.endsWith("\n") ? block : `${block}\n`;
    case "gemini":
      return `${JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "SessionStart",
          additionalContext: block,
        },
      })}\n`;
    case "cursor":
      return `${JSON.stringify({ additional_context: block })}\n`;
    case "copilot":
      return `${JSON.stringify({ additionalContext: block })}\n`;
  }
}
