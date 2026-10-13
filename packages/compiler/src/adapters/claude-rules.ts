/**
 * `claude_rules`: Claude Code path rules. Opt-in (OPT_IN_TARGET_KINDS): the
 * CLAUDE.md shim already imports AGENTS.md's scoped subsections, and VS Code
 * and Copilot CLI read this folder beside AGENTS.md.
 *
 * Path-scoped facts go to `.claude/rules/memax-<area>.md`. `paths:` is the
 * only frontmatter field Claude Code reads, and invalid YAML makes the rule
 * load unconditionally, so every glob is written as a quoted list item.
 * Claude-only facts go to the CLAUDE.md shim instead, so this file never
 * repeats them.
 */
import { agent, capOf } from "@memaxlabs/agents";
import { quoted, scoped } from "./scoped.js";

const CLAUDE = agent("claude-code");

export const claudeRules = scoped({
  kind: "claude_rules",
  tool: CLAUDE.name,
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
  limits: {
    lines: {
      value: capOf(CLAUDE.id, { repo: "claude_rules" }).lines!,
      tool: CLAUDE.name,
    },
  },
});
