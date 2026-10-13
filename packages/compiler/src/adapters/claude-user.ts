/**
 * `claude_user`: Claude Code's personal file, `~/.claude/rules/memax-personal.md`
 * (or under `$CLAUDE_CONFIG_DIR`).
 *
 * Claude Code loads every user rule without `paths:` frontmatter in every
 * session, before the project's rules, as it does `~/.claude/CLAUDE.md`. So
 * the file is Memax's, has no frontmatter, and the person's own CLAUDE.md is
 * never touched (decision 1). VS Code's Claude harness reads the same folder.
 *
 * An installed Claude Code that predates user rules gets one managed block
 * in `~/.claude/CLAUDE.md` instead: the same kind, the person's file.
 * Claude Code suggests keeping memory files under 200 lines (a warning).
 */
import { agent, capOf } from "@memaxlabs/agents";
import { basename } from "../text.js";
import { home } from "./home.js";

const CLAUDE = agent("claude-code");
const HOME = CLAUDE.home!;
const RULES_FILE = basename(HOME.path);

const inRules = (path: string) =>
  basename(path) === RULES_FILE && path.split("/").at(-2) === "rules";

export const claudeUser = home({
  kind: "claude_user",
  tool: CLAUDE.name,
  agent: CLAUDE.key,
  defaultPath: HOME.path,
  pathHint: `ending in rules/${RULES_FILE}, or CLAUDE.md for a block in the person's own file`,
  acceptsPath: (path) => inRules(path) || basename(path) === "CLAUDE.md",
  owner: (path) => (inRules(path) ? "memax" : "block"),
  limits: {
    lines: {
      value: capOf(CLAUDE.id, { user: "claude_rules_file" }).lines!,
      tool: CLAUDE.name,
    },
  },
});
