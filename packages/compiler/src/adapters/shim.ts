/**
 * Shims: a tool-specific file that imports the canonical AGENTS.md and adds
 * only the lines that tool alone should read (memories with
 * `scope.agents` naming it). Nothing in AGENTS.md is repeated.
 *
 * When the person owns the file, Memax manages one marked block in it
 * instead (see `managed-block.ts`); the budget applies to the block.
 */
import { compose, headerComment } from "../document.js";
import { extractManagedBlock, upsertManagedBlock } from "../managed-block.js";
import { parseFile } from "../parse.js";
import { select } from "../select.js";
import { basename, relativePath } from "../text.js";
import type { Adapter, Limits } from "./adapter.js";

export interface ShimSpec {
  kind: Adapter["kind"];
  tool: string;
  /** The agent ID whose agent-only memories land here. */
  agent: string;
  defaultPath: string;
  fileNames: string[];
  /** Writes the import line for a path relative to the shim. */
  importLine(relative: string): string;
  limits: Limits;
}

export function shim(spec: ShimSpec): Adapter {
  return {
    kind: spec.kind,
    version: 1,
    role: "shim",
    tool: spec.tool,
    defaultPath: spec.defaultPath,
    pathHint: `ending in ${spec.fileNames.join(" or ")}`,
    acceptsPath: (path) => spec.fileNames.includes(basename(path)),
    limits: spec.limits,
    reads: (target) => target.canonical,
    parse: parseFile,

    render(model, target, limit) {
      const selection = select(model, target, {
        accepts: (f) => f.agents.includes(spec.agent),
        prose: false,
      });
      const head = [
        headerComment(model),
        spec.importLine(relativePath(target.path, target.canonical)),
      ];
      const doc = compose(
        selection,
        { style: "markdown", head, tail: [] },
        limit,
      );
      if (!target.userOwned) {
        return [
          {
            path: target.path,
            content: doc.content,
            driftText: doc.content,
            userOwned: false,
            doc,
          },
        ];
      }
      const content = upsertManagedBlock(
        target.current,
        doc.content.slice(0, -1).split("\n"),
      );
      return [
        {
          path: target.path,
          content,
          driftText: extractManagedBlock(content) ?? content,
          userOwned: true,
          doc,
        },
      ];
    },
  };
}
