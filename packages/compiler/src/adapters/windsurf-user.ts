/**
 * `windsurf_user`: one managed block in Windsurf's global rules,
 * `~/.codeium/windsurf/memories/global_rules.md` (Devin Desktop, which
 * Windsurf became, keeps the path).
 *
 * Cascade reads that one file in every workspace, always on, with no
 * frontmatter, and limits it to 6,000 characters: the person's own rules
 * and Memax's block together. So the block never passes 6,000 characters,
 * its default budget is 4,000 bytes (decision 4), leaving room for the
 * person's lines, and the daemon writes nothing when the two together
 * would pass the limit. Cascade documents no imports, so the lines are
 * inline. The file is the person's whenever they have one, so Memax always
 * manages one block in it.
 */
import { agent, capOf, WINDSURF_GLOBAL_RULES } from "@memaxlabs/agents";
import { home } from "./home.js";

export { WINDSURF_GLOBAL_RULES };

const WINDSURF = agent("windsurf");

export const windsurfUser = home({
  kind: "windsurf_user",
  tool: WINDSURF.name,
  agent: WINDSURF.key,
  defaultPath: WINDSURF_GLOBAL_RULES,
  pathHint: WINDSURF_GLOBAL_RULES,
  acceptsPath: (path) => path === WINDSURF_GLOBAL_RULES,
  owner: () => "block",
  limits: {
    chars: {
      value: capOf(WINDSURF.id, { user: "windsurf_global_rules" }).chars!,
      tool: WINDSURF.name,
    },
  },
  budget: 4_000,
});
