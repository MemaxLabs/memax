import { CommandFailedError } from "./command-error";
import { demoExport } from "./demo-export";
import {
  DEMO_FORGET_REQUESTS,
  DEMO_MEMORY_PAGES,
  DEMO_RECORDS,
  DEMO_SECTION_COUNTS,
  DEMO_TOMBSTONES,
  DEMO_TOTALS,
} from "./demo-memories-data";
import { DEMO_CARDS, DEMO_FOLD, ZZ } from "./demo-review-data";
import { DEMO_TARGETS } from "./demo-targets-data";
import { reachesTarget } from "./targets";
import type { Decided, DemoJournal } from "./demo-records";
import type {
  ForgetPreview,
  MemoriesSource,
  MemoryFilter,
  MemoryListItem,
  MemoryPage,
  MemoryRecord,
  TombstoneStepLine,
  TombstoneView,
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
  /** Memories kept this session outside Review (a gate's answer), newest first, by space. */
  added: Map<string, MemoryListItem[]>;
  id: (slug: string, ref: string) => string;
  stamp: () => string;
  queueOf: (slug: string) => ReviewItem[];
  command: <T>(key: string, run: () => T) => Promise<T>;
  /** Undo's journal (demo-records.ts): each person's decision, to put back. */
  journal: DemoJournal;
  /** Edit, then keep: whether the new words wait for the demo's judge. */
  holdsForJudge: (slug: string, ref: string) => boolean;
  /** Keep on a flagged proposal, as the server refuses it (409 in_conflict). */
  inConflict: (item: ReviewItem) => Error;
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
  added,
  id,
  stamp,
  queueOf,
  command,
  journal,
  holdsForJudge,
  inConflict,
}: DemoStore): MemoriesSource {
  const isUnfolded = (slug: string, ref: string) => unfolded.has(id(slug, ref));
  /** Forgotten this session: the tombstone, by space/ref. */
  const forgotNow = new Map<string, TombstoneView>();
  /** Forget requests a person kept the memory against this session. */
  const declined = new Set<string>();

  /** Every row of a space, with this session's decisions, edits and new memories (on the first page). */
  function rowsOf(slug: string): MemoryListItem[][] {
    const pages = DEMO_MEMORY_PAGES[slug] ?? [];
    const fresh = added.get(slug) ?? [];
    const withFresh =
      fresh.length === 0
        ? pages
        : pages.length === 0
          ? [fresh]
          : [[...fresh, ...pages[0]!], ...pages.slice(1)];
    return withFresh.map((page) =>
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
        const gone = forgotNow.get(id(slug, row.ref));
        if (gone) {
          next = {
            ...next,
            statement: "",
            state: "forgotten",
            note: null,
            receipt: { by: ZZ, action: "forgot", at: gone.at },
            forgotten: { at: gone.at, by: null, detail: null },
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
      total:
        (DEMO_TOTALS[slug] ?? pages.flat().length) -
        rejected +
        (DEMO_TOTALS[slug] === undefined ? 0 : (added.get(slug)?.length ?? 0)),
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
      forgetRequests:
        row.state === "forgotten" || declined.has(id(slug, ref))
          ? []
          : (DEMO_FORGET_REQUESTS[`${slug}/${ref}`] ?? []),
    };
    if (row.state === "forgotten") {
      // No words, no seal, no lineage of words: the tombstone says the rest.
      return { ...base, kept: null, merged: null, sources: [], reaches: null };
    }
    return keptNow ? base : { ...base, ...extra, statement: base.statement };
  }

  /** The demo's tombstone: the board's M-0201, or one forgotten this session. */
  function tombstoneOf(slug: string, ref: string): TombstoneView | null {
    return (
      forgotNow.get(id(slug, ref)) ?? DEMO_TOMBSTONES[`${slug}/${ref}`] ?? null
    );
  }

  /** What the demo's Forget would do: its files from the demo targets. */
  function preview(slug: string, found: MemoryRecord): ForgetPreview {
    const targets = (DEMO_TARGETS[slug] ?? []).filter(
      (t) =>
        t.syncState !== "off" &&
        reachesTarget(found.ref, t, DEMO_TARGETS[slug] ?? []),
    );
    const copies = targets.filter((t) => t.delivery === "copy").length;
    return {
      version: found.version,
      carries: [],
      files: targets.length - copies,
      copies,
      agents: found.reach?.agents ?? 3,
      refusal: null,
    };
  }

  /** A tombstone for a memory forgotten this session, done at once. */
  function forgetNow(
    slug: string,
    found: MemoryRecord,
    note: string | null,
  ): TombstoneView {
    const at = stamp();
    const p = preview(slug, found);
    const targets = (DEMO_TARGETS[slug] ?? []).filter(
      (t) =>
        t.syncState !== "off" &&
        reachesTarget(found.ref, t, DEMO_TARGETS[slug] ?? []),
    );
    const agents = [
      "claude-code",
      "codex",
      "cursor",
      "opencode",
      "gemini",
    ].slice(0, p.agents);
    const step = (
      kind: TombstoneStepLine["kind"],
      fields: Partial<TombstoneStepLine> = {},
    ): TombstoneStepLine => ({
      key: `${kind}-${fields.target?.label ?? fields.agent ?? ""}`,
      kind,
      status: "done",
      reason: null,
      at,
      target: null,
      compile: null,
      agent: null,
      count: null,
      ...fields,
    });
    return {
      ref: found.ref,
      at,
      by: ZZ,
      requestedBy: found.forgetRequests?.[0]?.agent ?? null,
      via: "web",
      note,
      keptAt: found.kept?.at ?? null,
      readsBefore: found.reads ?? 0,
      status: "done",
      with: [],
      carried: null,
      gone: {
        versions: found.version,
        sources: found.sources.length,
        embeddings: 1,
        files: p.files + p.copies,
      },
      agents: agents.length,
      steps: [
        step("asked"),
        step("removed"),
        ...targets.map((t) =>
          step("target", {
            target: { label: t.label, kind: t.kind, delivery: t.delivery },
            reason: t.delivery === "copy" ? "copy" : null,
          }),
        ),
        step("artifacts"),
        step("caches"),
        step("ledger"),
        ...agents.map((a) =>
          step("agent", {
            agent: a,
            status: "waiting",
            reason: "next_read",
            at: null,
          }),
        ),
      ],
      unreachable: [
        {
          kind: "git_history",
          files: targets
            .filter((t) => t.delivery !== "copy")
            .map((t) => t.label),
          repositories: ["MemaxLabs/memax"],
          agents: [],
          days: null,
          processors: [],
          targets: [],
        },
        {
          kind: "agent_memory",
          files: [],
          repositories: [],
          agents: [],
          days: null,
          processors: [],
          targets: [],
        },
        {
          kind: "backups",
          files: [],
          repositories: [],
          agents: [],
          days: 7,
          processors: [],
          targets: [],
        },
      ],
    };
  }

  const memories: MemoriesSource = {
    peekList: (slug, filter) => listPage(slug, filter, undefined),
    peekRecord: (slug, ref) => record(slug, ref),
    peekTombstone: (slug, ref) => tombstoneOf(slug, ref),
    async tombstone({ space, ref }) {
      return tombstoneOf(space.slug, ref);
    },
    async previewForget({ space, ref }) {
      const found = record(space.slug, ref);
      if (!found) throw new CommandFailedError({ kind: "not-found" });
      if (found.lifecycle === "forgotten") {
        throw new CommandFailedError({ kind: "decided" });
      }
      return preview(space.slug, found);
    },
    forget({ space, ref, version, carries, note, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const found = record(space.slug, ref);
        if (!found) throw new CommandFailedError({ kind: "not-found" });
        if (found.lifecycle === "forgotten") {
          throw new CommandFailedError({ kind: "decided" });
        }
        if (found.version !== version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: found.version,
          });
        }
        if (carries.length > 0) {
          throw new CommandFailedError({ kind: "carries", refs: [] });
        }
        forgotNow.set(
          id(space.slug, ref),
          forgetNow(space.slug, found, note?.trim() || null),
        );
        return { ref };
      });
    },
    declineForget({ space, ref, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const found = record(space.slug, ref);
        if (!found?.forgetRequests?.length) {
          throw new CommandFailedError({ kind: "decided" });
        }
        declined.add(id(space.slug, ref));
      });
    },
    exportSpace({ space, idempotencyKey }) {
      return command(idempotencyKey, () => ({
        blob: new Blob(
          [demoExport(space.slug, space.name, rowsOf(space.slug).flat())],
          { type: "application/zip" },
        ),
        filename: `memax-${space.slug}-${stamp().slice(0, 10)}.zip`,
        receipt: "",
      }));
    },
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
        if (keep && queued?.state === "conflict") throw inConflict(queued);
        const key = id(space.slug, ref);
        const before = { edit: edits.get(key), decided: decided.get(key) };
        // Edit, then keep, whose new words touch a decision in force: saved
        // as the proposal's new version, judged, and not kept yet.
        const held = Boolean(keep && queued && holdsForJudge(space.slug, ref));
        edits.set(key, { statement, version: version + 1 });
        const keeps = Boolean(keep && queued && !held);
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
          // Saved for the judge, nothing compiles yet.
          recompiled: held
            ? null
            : (DEMO_CARDS[ref]?.touches.targets?.length ?? null),
          receipt,
          ...(held ? { judgePending: true } : {}),
        };
      });
    },
  };

  return memories;
}
