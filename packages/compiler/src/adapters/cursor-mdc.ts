/**
 * `cursor_mdc`: Cursor rules, for path-scoped facts only.
 *
 * Cursor reads AGENTS.md (root and nested), so an always-on rule would load
 * the record twice. Instead each group of globs gets
 * `.cursor/rules/memax-<area>.mdc` with `alwaysApply: false`, which Cursor
 * attaches when matching files are in play. Cursor ignores plain `.md` files
 * there, takes `globs` as a comma-separated string (written unquoted, as
 * Cursor itself writes it), and suggests keeping a rule under 500 lines.
 */
import { scoped } from "./scoped.js";

export const cursorMdc = scoped({
  kind: "cursor_mdc",
  tool: "Cursor",
  defaultPath: ".cursor/rules",
  dirs: [".cursor/rules"],
  extension: ".mdc",
  accepts: (f) => f.agents.length === 0 || f.agents.includes("cursor"),
  frontmatter: (paths, space) => [
    "---",
    `description: Facts from ${space} for ${paths.join(", ")}`,
    `globs: ${paths.join(",")}`,
    "alwaysApply: false",
    "---",
  ],
  limits: { lines: { value: 500, tool: "Cursor" } },
});
