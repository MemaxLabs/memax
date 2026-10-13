// The agents whose session-start hook runs `memax hook session-start`, and
// how each wants the block: the hooks of the agents registry
// (@memaxlabs/agents, copied into lib/agents/; its header has the docs
// checked for each).
//
// Which compiled files each loads at session start: AGENTS.md, plus its
// own shim when the space compiles one (CLAUDE.md, GEMINI.md). Scoped
// rules load only when a matching path is touched, so they aren't counted.
// On a machine whose home is linked, its own home file too (personal-layer
// design §7.1): Claude Code's rules file, Codex's block, and Gemini's block
// in ~/.gemini/AGENTS.md for Gemini CLI once that file's read_by lists it
// (its context.fileName names AGENTS.md), and Copilot CLI's block in
// ~/.copilot/copilot-instructions.md. Cursor loads no file of the
// Personal space (its User Rules aren't a file), so its hook prints the
// canonical copy's kept lines in full at every session start (`prints`),
// held to 1,500 tokens; a session-start hook's `additional_context` is the
// one way in (cursor.com/docs/hooks.md, sessionStart).
import { AGENTS as REGISTRY } from "../agents/agents.js";
import type { HookFormat } from "../agents/types.js";

export type { HookFormat };

export interface HookAgent {
  /** The agent kind the server knows it by (AgentKind). */
  kind: string;
  format: HookFormat;
  /** The target kinds it loads at session start, shim first. */
  loads: string[];
  /** The home kinds it loads in every session, wherever it runs. */
  home: string[];
  /**
   * Home kinds it loads no file of: the hook prints their kept lines in
   * full at every session start, not what changed (Cursor, the canonical).
   */
  prints: string[];
  /** Most tokens the block may take. */
  maxTokens: number;
}

const AGENTS: Record<string, HookAgent> = Object.fromEntries(
  REGISTRY.flatMap((a) =>
    a.hook
      ? [
          [
            a.id,
            {
              kind: a.id,
              format: a.hook.format,
              loads: [...a.hook.loads],
              home: a.home ? [a.home.kind] : [],
              prints: [...a.hook.prints],
              maxTokens: a.hook.maxTokens,
            },
          ],
        ]
      : [],
  ),
);

/**
 * The `--agent` values the hook takes: each hooked agent's id and short
 * key (`gemini`), and `claude` for Claude Code, whose hook this was first.
 */
const ALIASES: Record<string, string> = {
  claude: "claude-code",
  ...Object.fromEntries(
    REGISTRY.flatMap((a) =>
      a.hook
        ? [
            [a.id, a.id],
            [a.key, a.id],
          ]
        : [],
    ),
  ),
};

/** The agent a `--agent` value names; unknown agents get plain text and AGENTS.md. */
export function hookAgent(id: string | undefined): HookAgent {
  const key = ALIASES[(id ?? "").toLowerCase()];
  if (key) return AGENTS[key];
  return {
    kind: "other",
    format: "plain",
    loads: ["agents_md"],
    home: [],
    prints: [],
    maxTokens: 3000,
  };
}

/** Default paths, for a repository whose targets aren't cached here. */
export const DEFAULT_PATHS: Record<string, string> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
};

/**
 * Kinds a space compiles only when it asks for one (the compiler's
 * OPT_IN_TARGET_KINDS): Gemini CLI gets AGENTS.md alone unless the space
 * has the GEMINI.md target (`memax connect gemini --gemini-md`).
 */
export const OPT_IN_KINDS: ReadonlySet<string> = new Set(["gemini_md"]);

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
