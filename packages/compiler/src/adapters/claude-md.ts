/**
 * `claude_md`: the Claude Code shim.
 *
 * Claude Code reads AGENTS.md only when no CLAUDE.md exists, and most repos
 * have one. So CLAUDE.md becomes `@AGENTS.md` plus Claude-only lines. Claude
 * Code resolves `@path` imports up to four hops deep and dedupes repeated
 * `@AGENTS.md` imports. Its guidance is to keep memory files under 200 lines.
 */
import { agent, capOf } from "@memaxlabs/agents";
import { shim } from "./shim.js";

const CLAUDE = agent("claude-code");

export const claudeMd = shim({
  kind: "claude_md",
  tool: CLAUDE.name,
  agent: CLAUDE.key,
  defaultPath: "CLAUDE.md",
  fileNames: ["CLAUDE.md", "CLAUDE.local.md"],
  importLine: (relative) => `@${relative}`,
  limits: {
    lines: {
      value: capOf(CLAUDE.id, { repo: "claude_md" }).lines!,
      tool: CLAUDE.name,
    },
  },
});
