import { CommandFailedError } from "./command-error";
import {
  DEMO_MEMORY_PAGES,
  DEMO_RECORDS,
  DEMO_SECTION_COUNTS,
  DEMO_TOTALS,
} from "./demo-memories-data";
import { DEMO_CARDS, ZZ } from "./demo-review-data";
import { DEMO_TARGETS } from "./demo-targets-data";
import { reachesTarget } from "./targets";
import type { Decided } from "./demo-records";
import type {
  MemoriesSource,
  MemoryFilter,
  MemoryListItem,
  MemoryPage,
  MemoryRecord,
} from "./memories";
import type { DecisionResult } from "./records";
import type { ReviewItem } from "./review";
import type { Section } from "./types";

/**
 * The demo's Memories on the same session store as its Review
 * (demo-records.ts): what was kept in Review reads as kept here, and
 * what was rejected is gone.
 */

/** What the demo's Review and Memories share for one browser session. */
export interface DemoStore {
  decided: Map<string, Decided>;
  edits: Map<string, { statement: string; version: number }>;
  id: (slug: string, ref: string) => string;
  stamp: () => string;
  queueOf: (slug: string) => ReviewItem[];
  command: (key: string, run: () => DecisionResult) => Promise<DecisionResult>;
}

const FILTER_STATES: Record<MemoryFilter, string[] | null> = {
  all: null,
  waiting: ["proposed", "conflict", "stale"],
  stale: ["stale"],
  merged: ["merged"],
  forgotten: ["forgotten"],
};

function countSections(
  rows: MemoryListItem[],
): Partial<Record<Section, number>> {
  const counts: Partial<Record<Section, number>> = {};
  for (const row of rows) counts[row.section] = (counts[row.section] ?? 0) + 1;
  return counts;
}

export function createDemoMemories({
  decided,
  edits,
  id,
  stamp,
  queueOf,
  command,
}: DemoStore): MemoriesSource {
  /** Every row of a space, with this session's decisions and edits. */
  function rowsOf(slug: string): MemoryListItem[][] {
    return (DEMO_MEMORY_PAGES[slug] ?? []).map((page) =>
      page.flatMap((row): MemoryListItem[] => {
        const done = decided.get(id(slug, row.ref));
        const edit = edits.get(id(slug, row.ref));
        if (done?.outcome === "rejected") return [];
        let next = edit ? { ...row, statement: edit.statement } : row;
        if (done?.outcome === "kept") {
          next = {
            ...next,
            state: "kept",
            note: null,
            receipt: { by: ZZ, action: "kept", at: stamp() },
          };
        }
        return [next];
      }),
    );
  }

  function listPage(
    slug: string,
    filter: MemoryFilter,
    cursor: string | undefined,
  ): MemoryPage {
    const pages = rowsOf(slug);
    const states = FILTER_STATES[filter];
    if (states) {
      const items = pages.flat().filter((r) => states.includes(r.state));
      return {
        items,
        nextCursor: null,
        sectionCounts: countSections(items),
        total: items.length,
      };
    }
    const index = cursor ? Number(cursor) : 0;
    const rejected = [...decided.values()].filter(
      (d) => d.slug === slug && d.outcome === "rejected",
    ).length;
    return {
      items: pages[index] ?? [],
      nextCursor: index + 1 < pages.length ? String(index + 1) : null,
      sectionCounts: DEMO_SECTION_COUNTS[slug] ?? countSections(pages.flat()),
      total: (DEMO_TOTALS[slug] ?? pages.flat().length) - rejected,
    };
  }

  function record(slug: string, ref: string): MemoryRecord | null {
    const row = rowsOf(slug)
      .flat()
      .find((r) => r.ref === ref);
    if (!row) return null;
    const extra = DEMO_RECORDS[ref] ?? {};
    const edit = edits.get(id(slug, ref));
    const keptNow = decided.get(id(slug, ref))?.outcome === "kept";
    // Stale is a flag on a kept memory; the demo's conflict is a proposal.
    const lifecycle =
      row.state === "stale"
        ? "kept"
        : row.state === "conflict"
          ? "proposed"
          : row.state;
    const base: MemoryRecord = {
      ref,
      version: edit?.version ?? extra.version ?? 1,
      statement: row.statement,
      section: row.section,
      state: row.state,
      lifecycle,
      kept:
        row.state === "kept" && row.receipt
          ? { by: row.receipt.by, at: row.receipt.at }
          : null,
      latest: row.receipt,
      reads: null,
      reach: null,
      lineage: row.receipt
        ? [
            {
              key: "latest",
              action: row.receipt.action,
              by: row.receipt.by,
              at: row.receipt.at,
              detail: null,
              count: null,
              to: null,
            },
          ]
        : [],
      merged: null,
      sources: [],
      // The demo's compiled files that hold its words, or read one that does.
      reaches:
        lifecycle === "kept"
          ? (DEMO_TARGETS[slug] ?? []).filter((t) =>
              reachesTarget(ref, t, DEMO_TARGETS[slug] ?? []),
            )
          : null,
      conditions: [],
      checked: null,
      forgotten: row.forgotten,
    };
    return keptNow ? base : { ...base, ...extra, statement: base.statement };
  }

  const memories: MemoriesSource = {
    peekList: (slug, filter) => listPage(slug, filter, undefined),
    peekRecord: (slug, ref) => record(slug, ref),
    async list({ space, filter, cursor }) {
      return listPage(space.slug, filter, cursor);
    },
    async get({ space, ref }) {
      return record(space.slug, ref);
    },
    async latest({ space, ref }) {
      const found = record(space.slug, ref);
      if (!found) return null;
      return {
        version: found.version,
        statement: found.statement,
        by: found.latest?.by ?? null,
        at: found.latest?.at ?? stamp(),
      };
    },
    edit({ space, ref, version, statement, keep, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const queued = queueOf(space.slug).find((i) => i.ref === ref);
        const found = queued ?? record(space.slug, ref);
        if (!found) throw new CommandFailedError({ kind: "not-found" });
        if (found.version !== version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: found.version,
          });
        }
        edits.set(id(space.slug, ref), { statement, version: version + 1 });
        const keeps = Boolean(keep && queued);
        if (keeps && queued) {
          decided.set(id(space.slug, ref), {
            slug: space.slug,
            outcome: "kept",
            item: { ...queued, statement },
          });
        }
        return {
          ref,
          outcome: keeps ? "kept" : "edited",
          version: version + 1,
          recompiled: DEMO_CARDS[ref]?.touches.targets?.length ?? null,
        };
      });
    },
  };

  return memories;
}
