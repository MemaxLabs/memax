/**
 * `claude_md`: the Claude Code shim.
 *
 * Claude Code reads AGENTS.md only when no CLAUDE.md exists, and most repos
 * have one. So CLAUDE.md becomes `@AGENTS.md` plus Claude-only lines. Claude
 * Code resolves `@path` imports up to four hops deep and dedupes repeated
 * `@AGENTS.md` imports. Its guidance is to keep memory files under 200 lines.
 */
import { shim } from "./shim.js";

export const claudeMd = shim({
  kind: "claude_md",
  tool: "Claude Code",
  agent: "claude-code",
  defaultPath: "CLAUDE.md",
  fileNames: ["CLAUDE.md", "CLAUDE.local.md"],
  importLine: (relative) => `@${relative}`,
  limits: { lines: { value: 200, tool: "Claude Code" } },
});
