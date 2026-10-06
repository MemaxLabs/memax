/**
 * `agents_md`: the canonical file.
 *
 * Nearly every agent reads AGENTS.md (Codex, Cursor, Copilot, Windsurf and
 * Devin, OpenCode, and Claude Code when there's no CLAUDE.md), so the full
 * compiled Brief goes here once and the other targets point at it.
 *
 * Codex concatenates AGENTS.md files from the git root down and truncates
 * past `project_doc_max_bytes` (32 KiB), so the budget never exceeds 32 KiB.
 */
import { compose, headerComment } from "../document.js";
import { parseFile } from "../parse.js";
import { select } from "../select.js";
import { basename } from "../text.js";
import type { Adapter } from "./adapter.js";

export const INTRO_LINE =
  "Every line cites a memory. Ask the memax MCP server for anything else.";

export const LIVE_CONTEXT = [
  "## Live context",
  "Use the memax MCP server for anything not here:",
  "memax_recall, memax_search, memax_get. Propose with memax_push.",
];

export const agentsMd: Adapter = {
  kind: "agents_md",
  version: 1,
  role: "canonical",
  tool: "Codex",
  defaultPath: "AGENTS.md",
  pathHint: "ending in AGENTS.md",
  acceptsPath: (path) => basename(path) === "AGENTS.md",
  limits: { bytes: { value: 32 * 1024, tool: "Codex" } },
  reads: () => null,
  parse: parseFile,

  render(model, target, limit) {
    const selection = select(model, target, {
      // Agent-only facts go to that agent's own file; path-scoped facts get
      // subsections here unless the scoped targets carry them (`omit`).
      accepts: (f) =>
        f.agents.length === 0 &&
        (f.paths.length === 0 || target.scoped === "inline"),
      prose: true,
    });
    const { title, summary } = model.brief;
    const head = [
      headerComment(model),
      `# ${title}`,
      "",
      ...(summary ? [summary] : []),
      INTRO_LINE,
    ];
    const doc = compose(
      selection,
      { style: "markdown", head, tail: LIVE_CONTEXT },
      limit,
    );
    return [
      {
        path: target.path,
        content: doc.content,
        driftText: doc.content,
        userOwned: false,
        doc,
      },
    ];
  },
};
