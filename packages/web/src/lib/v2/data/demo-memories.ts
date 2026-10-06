import { CommandFailedError } from "./command-error";
import {
  DEMO_MEMORY_PAGES,
  DEMO_RECORDS,
  DEMO_SECTION_COUNTS,
  DEMO_TOTALS,
} from "./demo-memories-data";
import { DEMO_CARDS, DEMO_FOLD, ZZ } from "./demo-review-data";
import { DEMO_TARGETS } from "./demo-targets-data";
import { reachesTarget } from "./targets";
import type { Decided, DemoJournal } from "./demo-records";
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
  /** Folds undone this session (the team space's DEMO_FOLD): proposals again. */
  unfolded: Set<string>;
  id: (slug: string, ref: string) => string;
  stamp: () => string;
  queueOf: (slug: string) => ReviewItem[];
  command: <T>(key: string, run: () => T) => Promise<T>;
  /** Undo's journal (demo-records.ts): each person's decision, to put back. */
  journal: DemoJournal;
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
  unfolded,
  id,
  stamp,
  queueOf,
  command,
  journal,
}: DemoStore): MemoriesSource {
  const isUnfolded = (slug: string, ref: string) => unfolded.has(id(slug, ref));

  /** Every row of a space, with this session's decisions and edits. */
  function rowsOf(slug: string): MemoryListItem[][] {
    return (DEMO_MEMORY_PAGES[slug] ?? []).map((page) =>
      page.flatMap((row): MemoryListItem[] => {
        const done = decided.get(id(slug, row.ref));
        const edit = edits.get(id(slug, row.ref));
        if (done?.outcome === "rejected") return [];
        let next = edit ? { ...row, statement: edit.statement } : row;
        if (isUnfolded(slug, row.ref) && !done) {
          // Unfolded: a proposal in Review again.
          next = {
            ...next,
            state: "proposed",
            note: null,
            receipt: {
              by: DEMO_FOLD.item.by,
              action: "proposed",
              at: DEMO_FOLD.item.at,
            },
          };
        }
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
    // An undone fold leaves no fold on either side.
    const foldGone = isUnfolded(slug, DEMO_FOLD.item.ref);
    const extra =
      foldGone && ref === DEMO_FOLD.item.ref
        ? {}
        : foldGone && ref === DEMO_FOLD.into
          ? { ...DEMO_RECORDS[ref], merged: null }
          : (DEMO_RECORDS[ref] ?? {});
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
      return command(idempotencyKey, (): DecisionResult => {
        const queued = queueOf(space.slug).find((i) => i.ref === ref);
        const found = queued ?? record(space.slug, ref);
        if (!found) throw new CommandFailedError({ kind: "not-found" });
        if (found.version !== version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: found.version,
          });
        }
        // Like the server: a flagged proposal can't be kept until its
        // conflict is settled, edited or not.
        if (keep && queued?.state === "conflict") {
          throw new CommandFailedError({ kind: "decided" });
        }
        const key = id(space.slug, ref);
        const before = { edit: edits.get(key), decided: decided.get(key) };
        edits.set(key, { statement, version: version + 1 });
        const keeps = Boolean(keep && queued);
        if (keeps && queued) {
          decided.set(key, {
            slug: space.slug,
            outcome: "kept",
            item: { ...queued, statement },
          });
        }
        const receipt = journal.record({
          slug: space.slug,
          command: "edit",
          ref,
          revert: () => {
            if (before.edit) edits.set(key, before.edit);
            else edits.delete(key);
            if (before.decided) decided.set(key, before.decided);
            else decided.delete(key);
          },
        });
        return {
          ref,
          outcome: keeps ? "kept" : "edited",
          version: version + 1,
          recompiled: DEMO_CARDS[ref]?.touches.targets?.length ?? null,
          receipt,
        };
      });
    },
  };

  return memories;
}
