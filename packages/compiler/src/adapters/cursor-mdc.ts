/**
 * `cursor_mdc`: Cursor rules, for path-scoped facts only. Opt-in
 * (OPT_IN_TARGET_KINDS): Cursor and Devin CLI read AGENTS.md too, whose
 * subsections hold the same facts unless it's set to `scoped: omit`.
 *
 * Cursor reads AGENTS.md (root and nested), so an always-on rule would load
 * the record twice. Instead each group of globs gets
 * `.cursor/rules/memax-<area>.mdc` with `alwaysApply: false`, which Cursor
 * attaches when matching files are in play. Cursor ignores plain `.md` files
 * there, takes `globs` as a comma-separated string (written unquoted, as
 * Cursor itself writes it), and suggests keeping a rule under 500 lines.
 */
import { agent, capOf } from "@memaxlabs/agents";
import { scoped } from "./scoped.js";

const CURSOR = agent("cursor");

export const cursorMdc = scoped({
  kind: "cursor_mdc",
  tool: CURSOR.name,
  defaultPath: ".cursor/rules",
  dirs: [".cursor/rules"],
  extension: ".mdc",
  accepts: (f) => f.agents.length === 0 || f.agents.includes(CURSOR.key),
  frontmatter: (paths, space) => [
    "---",
    `description: Facts from ${space} for ${paths.join(", ")}`,
    `globs: ${paths.join(",")}`,
    "alwaysApply: false",
    "---",
  ],
  limits: {
    lines: {
      value: capOf(CURSOR.id, { repo: "cursor_mdc" }).lines!,
      tool: CURSOR.name,
    },
  },
});
