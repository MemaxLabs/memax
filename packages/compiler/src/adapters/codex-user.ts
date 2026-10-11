/**
 * `codex_user`: one managed block in Codex's global file,
 * `$CODEX_HOME/AGENTS.md` (default `~/.codex`), or in `AGENTS.override.md`
 * when that's the file Codex reads.
 *
 * Codex reads only the first non-empty one of the two, has no imports, and
 * loads it before the project's files, in untrusted projects too. The file
 * is the person's: Memax manages one block in it. The global file isn't
 * counted against `project_doc_max_bytes`, so only the budget bounds it.
 */
import { basename } from "../text.js";
import { home } from "./home.js";

export const codexUser = home({
  kind: "codex_user",
  tool: "Codex",
  agent: "codex",
  defaultPath: ".codex/AGENTS.md",
  pathHint: "ending in AGENTS.md or AGENTS.override.md",
  acceptsPath: (path) =>
    basename(path) === "AGENTS.md" || basename(path) === "AGENTS.override.md",
  owner: () => "block",
  limits: {},
});
