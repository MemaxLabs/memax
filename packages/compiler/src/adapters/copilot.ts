/**
 * `copilot`: GitHub Copilot path instructions. Opt-in (OPT_IN_TARGET_KINDS),
 * for repositories that use Copilot's cloud agent or code review: VS Code and
 * Copilot CLI read AGENTS.md and `.claude/rules` too.
 *
 * Copilot reads AGENTS.md (and root CLAUDE.md and GEMINI.md) for the rest.
 * Path-scoped facts go to `.github/instructions/memax-<area>.instructions.md`
 * with an `applyTo` glob list.
 */
import { agent } from "@memaxlabs/agents";
import { quoted, scoped } from "./scoped.js";

const COPILOT = agent("copilot");

export const copilot = scoped({
  kind: "copilot",
  tool: COPILOT.name,
  defaultPath: ".github/instructions",
  dirs: [".github/instructions"],
  extension: ".instructions.md",
  accepts: (f) => f.agents.length === 0 || f.agents.includes(COPILOT.key),
  frontmatter: (paths) => ["---", `applyTo: ${quoted(paths.join(","))}`, "---"],
  limits: {},
});
