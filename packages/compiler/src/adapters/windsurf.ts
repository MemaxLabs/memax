/**
 * `windsurf`: Windsurf and Devin rules (P3, not in the default set).
 *
 * Both read AGENTS.md for the rest. Path-scoped facts go to
 * `.devin/rules/memax-<area>.md` (`.windsurf/rules` also works) with
 * `trigger: glob`. Each rule file is capped at 12,000 characters, so the
 * fill stops there whatever the byte budget says.
 */
import { scoped } from "./scoped.js";

export const windsurf = scoped({
  kind: "windsurf",
  tool: "Windsurf",
  defaultPath: ".devin/rules",
  dirs: [".devin/rules", ".windsurf/rules"],
  extension: ".md",
  accepts: (f) => f.agents.length === 0 || f.agents.includes("windsurf"),
  frontmatter: (paths) => [
    "---",
    "trigger: glob",
    `globs: ${paths.join(",")}`,
    "---",
  ],
  limits: { chars: { value: 12_000, tool: "Windsurf" } },
});
