import { CommandFailedError } from "./command-error";
import {
  DEMO_IMPORT_ID,
  DEMO_IMPORT_SPACE,
  demoImportView,
} from "./demo-imports-data";
import {
  DEMO_V1_IMPORT_ID,
  DEMO_V1_SPACE,
  demoV1ImportView,
} from "./demo-switch-data";
import type {
  BulkOutcome,
  ImportMemoryView,
  ImportView,
  ImportsSource,
} from "./imports";

/**
 * The demo's imports (imports.ts): memax-v2's import as the boards draw
 * it, settled and kept in this tab's session the way the server would:
 * keep_one keeps one member and rejects the rest, keep_suggestion keeps
 * new words and rejects the members, keep_all keeps them all and
 * leave_open keeps them as open questions; a bulk keep refuses anything
 * that can't be kept in bulk. Answers at once through `peek*`. acme-web's
 * V1 import (ReviewImport "From V1") is there once the space switched.
 */
export function createDemoImports({
  commandDelayMs = 0,
  nextRef,
  switched = () => true,
}: {
  commandDelayMs?: number;
  /** A display ID for the suggestion a settlement keeps. */
  nextRef: () => string;
  /** Whether a space switched to V2 in this session (switch-demo.ts). */
  switched?: (slug: string) => boolean;
}): ImportsSource {
  const all = new Map<string, ImportView>([
    [DEMO_IMPORT_SPACE, demoImportView()],
    [DEMO_V1_SPACE.slug, demoV1ImportView()],
  ]);
  const views = {
    get: (slug: string) => (switched(slug) ? all.get(slug) : undefined),
  };
  const delay = () =>
    new Promise<void>((r) => setTimeout(r, Math.max(0, commandDelayMs)));
  const viewOf = (slug: string, id: string) => {
    const v = views.get(slug);
    return v && v.summary.id === id ? v : undefined;
  };
  const decide = (m: ImportMemoryView, state: "kept" | "rejected" | "open") => {
    m.state = state === "open" ? "kept" : state;
    m.held = "decided";
    m.bulk = false;
  };
  const settleConflict = (v: ImportView, n: number) => {
    const c = v.conflicts.find((x) => x.n === n);
    if (!c) throw new CommandFailedError({ kind: "not-found" });
    if (c.state === "settled")
      throw new CommandFailedError({ kind: "decided" });
    c.state = "settled";
    const members = c.members.map(
      (p) => v.memories.find((m) => m.id === p.id)!,
    );
    return { c, members };
  };
  const bulk = async (
    slug: string,
    items: Array<{ ref: string; version: number }>,
    to: "kept" | "rejected",
  ): Promise<BulkOutcome> => {
    await delay();
    const v = views.get(slug);
    const out: BulkOutcome = { applied: [], refused: [], failed: [] };
    for (const item of items) {
      const m = v?.memories.find((x) => x.ref === item.ref);
      if (!m || m.held === "decided") {
        out.failed.push({ ref: item.ref, code: "invalid_transition" });
      } else if (to === "kept" && !m.bulk) {
        out.refused.push({
          ref: item.ref,
          code:
            m.held === "quarantined" ? "external_needs_review" : "in_conflict",
        });
      } else {
        decide(m, to);
        out.applied.push(item.ref);
      }
    }
    return out;
  };

  return {
    peekList: (slug) => {
      const v = views.get(slug);
      return v ? [structuredClone(v.summary)] : slug ? [] : undefined;
    },
    peekView: (slug, id) => {
      const v = viewOf(slug, id);
      return v ? structuredClone(v) : undefined;
    },
    async list({ space }) {
      const v = views.get(space.slug);
      return v ? [structuredClone(v.summary)] : [];
    },
    async get({ space, id }) {
      const v = viewOf(space.slug, id);
      return v ? structuredClone(v) : null;
    },
    async settle({ space, importId, n, choice }) {
      await delay();
      const v = viewOf(space.slug, importId);
      if (!v) throw new CommandFailedError({ kind: "not-found" });
      const { c, members } = settleConflict(v, n);
      c.choice = choice.choice;
      switch (choice.choice) {
        case "keep_one": {
          const keep = members.find(
            (m) => m.ref === choice.keep || m.id === choice.keep,
          );
          if (!keep) throw new CommandFailedError({ kind: "not-found" });
          for (const m of members) decide(m, m === keep ? "kept" : "rejected");
          c.chosen = keep.ref;
          break;
        }
        case "keep_all":
          for (const m of members) decide(m, "kept");
          break;
        case "leave_open":
          for (const m of members) decide(m, "open");
          break;
        case "keep_suggestion": {
          for (const m of members) decide(m, "rejected");
          const ref = nextRef();
          v.memories.push({
            ...members[0]!,
            id: `${members[0]!.id}-s`,
            ref,
            statement: choice.statement ?? c.suggestion ?? "",
            state: "kept",
            held: "decided",
            conflict: null,
            refs: members.flatMap((m) => m.refs),
            files: [...new Set(members.flatMap((m) => m.files))],
            items: members.reduce((s, m) => s + m.items, 0),
          });
          c.chosen = ref;
          break;
        }
      }
      return structuredClone(c);
    },
    keep: ({ space, items }) => bulk(space.slug, items, "kept"),
    reject: ({ space, items }) => bulk(space.slug, items, "rejected"),
  };
}

export { DEMO_IMPORT_ID, DEMO_V1_IMPORT_ID };
