/**
 * `claude_rules`: Claude Code path rules (P3, not in the default set).
 *
 * Path-scoped facts go to `.claude/rules/memax-<area>.md`. `paths:` is the
 * only frontmatter field Claude Code reads, and invalid YAML makes the rule
 * load unconditionally, so every glob is written as a quoted list item.
 * Claude-only facts go to the CLAUDE.md shim instead, so this file never
 * repeats them.
 */
import { quoted, scoped } from "./scoped.js";

export const claudeRules = scoped({
  kind: "claude_rules",
  tool: "Claude Code",
  defaultPath: ".claude/rules",
  dirs: [".claude/rules"],
  extension: ".md",
  accepts: (f) => f.agents.length === 0,
  frontmatter: (paths) => [
    "---",
    "paths:",
    ...paths.map((p) => `  - ${quoted(p)}`),
    "---",
  ],
  limits: { lines: { value: 200, tool: "Claude Code" } },
});
