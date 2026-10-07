/**
 * Scoped rule files: one file per group of globs, holding only the facts
 * with exactly those `scope.paths`. The tool loads the file when it works on
 * matching files and reads AGENTS.md for everything else, so an unscoped
 * record produces no file at all.
 */
import { compose, headerComment } from "../document.js";
import type { Fact } from "../model.js";
import { parseFile } from "../parse.js";
import { select, type Selection } from "../select.js";
import { byCodeUnit } from "../text.js";
import {
  hasSegments,
  type Adapter,
  type Limits,
  type Output,
} from "./adapter.js";

export interface ScopedSpec {
  kind: Adapter["kind"];
  tool: string;
  defaultPath: string;
  /** Directories the tool reads rules from. */
  dirs: string[];
  /** File name suffix, for example `.mdc`. */
  extension: string;
  /** Whether a fact belongs here, given its agent scope. */
  accepts(fact: Fact): boolean;
  /** Frontmatter lines, fences included. */
  frontmatter(paths: string[], space: string): string[];
  limits: Limits;
}

export function scoped(spec: ScopedSpec): Adapter {
  return {
    kind: spec.kind,
    version: 1,
    role: "scoped",
    tool: spec.tool,
    defaultPath: spec.defaultPath,
    pathHint: `inside ${spec.dirs.join(" or ")}`,
    acceptsPath: (path) => spec.dirs.some((dir) => hasSegments(path, dir)),
    limits: spec.limits,
    reads: (target) => target.canonical,
    parse: parseFile,

    render(model, target, limit) {
      const selection = select(model, target, {
        accepts: (f) => f.paths.length > 0 && spec.accepts(f),
        prose: false,
      });
      const groups = [...new Set(selection.entries.map((e) => e.group))].sort(
        byCodeUnit,
      );
      const taken = new Set<string>();
      const outputs: Output[] = [];
      for (const group of groups) {
        const entries = selection.entries.filter((e) => e.group === group);
        const paths = entries[0].paths;
        const file: Selection = {
          sections: selection.sections,
          entries: entries.map((e) => ({ ...e, group: "" })),
        };
        const head = [
          ...spec.frontmatter(paths, model.space.slug),
          headerComment(model),
        ];
        const doc = compose(file, { style: "markdown", head, tail: [] }, limit);
        const path = `${target.path}/memax-${uniqueSlug(areaSlug(paths), taken)}${spec.extension}`;
        outputs.push({
          path,
          content: doc.content,
          driftText: doc.content,
          userOwned: false,
          doc,
        });
      }
      return outputs;
    },
  };
}

/** `packages/web/**` → `packages-web`; `**\/*.test.ts` → `test-ts`. */
export function areaSlug(paths: string[]): string {
  const parts = paths.map((glob) => {
    const fixed: string[] = [];
    for (const segment of glob.split("/")) {
      if (/[*?[\]!]/.test(segment)) break;
      fixed.push(segment);
    }
    const base = fixed.length > 0 ? fixed.join("-") : glob;
    return base
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
  });
  const slug = [...new Set(parts.filter(Boolean))]
    .join("-")
    .slice(0, 60)
    .replace(/-+$/, "");
  return slug || "scoped";
}

function uniqueSlug(slug: string, taken: Set<string>): string {
  let candidate = slug;
  for (let n = 2; taken.has(candidate); n += 1) candidate = `${slug}-${n}`;
  taken.add(candidate);
  return candidate;
}

/** A YAML double-quoted scalar. Globs are validated, so only `\` and `"` need escaping. */
export function quoted(value: string): string {
  return `"${value.replace(/[\\"]/g, (c) => `\\${c}`)}"`;
}
