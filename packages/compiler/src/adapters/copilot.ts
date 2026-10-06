/**
 * `copilot`: GitHub Copilot path instructions (P3, not in the default set).
 *
 * Copilot reads AGENTS.md (and root CLAUDE.md and GEMINI.md) for the rest.
 * Path-scoped facts go to `.github/instructions/memax-<area>.instructions.md`
 * with an `applyTo` glob list.
 */
import { quoted, scoped } from "./scoped.js";

export const copilot = scoped({
  kind: "copilot",
  tool: "Copilot",
  defaultPath: ".github/instructions",
  dirs: [".github/instructions"],
  extension: ".instructions.md",
  accepts: (f) => f.agents.length === 0 || f.agents.includes("copilot"),
  frontmatter: (paths) => ["---", `applyTo: ${quoted(paths.join(","))}`, "---"],
  limits: {},
});
