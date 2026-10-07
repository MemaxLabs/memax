import { MemaxError } from "memax-sdk";
import { CommandFailedError } from "./command-error";
import { createDemoMemories } from "./demo-memories";
import {
  DEMO_CARDS,
  DEMO_CONFLICTS,
  DEMO_FOLD,
  DEMO_JUDGING,
  DEMO_QUEUES,
  ZZ,
} from "./demo-review-data";
import type { MemoryListItem } from "./memories";
import type { DecisionResult } from "./records";
import type {
  ReviewCardData,
  ReviewItem,
  ReviewQueue,
  ReviewSource,
} from "./review";
import type { Section, SpaceOverview } from "./types";
import { undoWindowMs, type UndoCommand, type UndoSource } from "./undo";

/**
 * The demo's Review and Memories: the boards' records behind the
 * ReviewSource and MemoriesSource interfaces, with the decisions made
 * in this browser session applied on top (so Keep leaves the queue,
 * the rail's count drops and Memories shows it kept). Commands settle
 * after a short delay, so the optimistic seal is visible first, and
 * replay by idempotency key like the server does.
 *
 * It holds the server's contract where the boards meet the judge: a
 * proposal the judge is still checking (DEMO_JUDGING) answers Keep with
 * `busy` while it touches a decision in force, a flagged proposal can't
 * be kept until its conflict is settled, and every person's decision is
 * journalled so Undo can reverse it within its window.
 */

export type Decided = {
  slug: string;
  outcome: "kept" | "rejected";
  item: ReviewItem;
};

const EMPTY_CARD: ReviewCardData = {
  before: null,
  conflict: null,
  evidence: null,
  readFrom: null,
  sourceUrl: null,
  touches: {
    memories: [],
    basis: "section",
    replacesOnKeep: false,
    targets: null,
  },
};

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

/** A decision Undo can reverse: what it was, when, and how to put it back. */
interface Journalled {
  slug: string;
  command: UndoCommand;
  ref: string;
  /** Real time, like the server's window. */
  at: number;
  undone: boolean;
  revert: () => void;
}

export interface DemoJournal {
  /** Journals a decision; returns its receipt. */
  record(entry: Omit<Journalled, "at" | "undone">): string;
}

export function createDemoRecords({
  now,
  commandDelayMs = 240,
  judging = DEMO_JUDGING,
  clock = () => Date.now(),
}: {
  now: () => Date;
  commandDelayMs?: number;
  /** The judge's script: how long each check takes once the demo serves it. */
  judging?: typeof DEMO_JUDGING;
  /** Real time, for the judge and the undo window (the demo's `now` is fixed). */
  clock?: () => number;
}) {
  const decided = new Map<string, Decided>();
  const edits = new Map<string, { statement: string; version: number }>();
  const replays = new Map<string, unknown>();
  const journal = new Map<string, Journalled>();
  const seen = new Map<string, number>();
  /** Folds undone in this session: back in Review as proposals. */
  const unfolded = new Set<string>();
  // Proposals that arrived this session (a pulled hand edit), oldest first.
  const arrived = new Map<string, ReviewItem[]>();
  let nextReceipt = 1;
  const id = (slug: string, ref: string) => `${slug}/${ref}`;
  const stamp = () => now().toISOString();
  // The judge's fold in the team space, undoable like the server's.
  journal.set(DEMO_FOLD.receipt, {
    slug: DEMO_FOLD.slug,
    command: "fold",
    ref: DEMO_FOLD.item.ref,
    at: clock(),
    undone: false,
    revert: () => unfolded.add(id(DEMO_FOLD.slug, DEMO_FOLD.item.ref)),
  });

  /** Whether the demo's judge is still checking it. */
  function checking(slug: string, ref: string): boolean {
    const script = judging[ref];
    if (!script) return false;
    const key = id(slug, ref);
    const first = seen.get(key) ?? clock();
    seen.set(key, first);
    return clock() - first < script.afterMs;
  }

  /**
   * Edit, then keep: whether the new words wait for the judge (they touch
   * a decision in force, in the demo's script). If so, the judge starts
   * checking the new version now, as the server's would.
   */
  function holdsForJudge(slug: string, ref: string): boolean {
    if (judging[ref]?.touchesDecision !== true) return false;
    seen.set(id(slug, ref), clock());
    return true;
  }

  /** Keep on a flagged proposal, as the server refuses it. */
  function inConflict(item: ReviewItem): MemaxError {
    const other = item.conflictsWith;
    return new MemaxError(
      `${item.ref} contradicts ${other ?? "a decision in force"}, a decision in force, so it can't be kept as it is. Settle the conflict first: compare both sides and choose.`,
      "in_conflict",
      409,
      other ? { ref: other } : {},
    );
  }

  function queueOf(slug: string): ReviewItem[] {
    const back =
      slug === DEMO_FOLD.slug && unfolded.has(id(slug, DEMO_FOLD.item.ref))
        ? [DEMO_FOLD.item]
        : [];
    return [...(DEMO_QUEUES[slug] ?? []), ...(arrived.get(slug) ?? []), ...back]
      .filter((item) => !decided.has(id(slug, item.ref)))
      .map((item) => {
        const edit = edits.get(id(slug, item.ref));
        const next = edit ? { ...item, ...edit } : item;
        if (!judging[item.ref]) return next;
        return { ...next, judge: checking(slug, item.ref) ? "working" : null };
      });
  }

  function peekQueue(slug: string): ReviewQueue {
    const items = queueOf(slug);
    return { items, total: items.length, nextCursor: null };
  }

  /** Runs a command once per idempotency key, like the server. */
  async function command<T>(key: string, run: () => T): Promise<T> {
    await sleep(commandDelayMs);
    if (replays.has(key)) return replays.get(key) as T;
    const result = run();
    replays.set(key, result);
    return result;
  }

  const record: DemoJournal["record"] = (entry) => {
    const receipt = `demo-receipt-${nextReceipt++}`;
    journal.set(receipt, { ...entry, at: clock(), undone: false });
    return receipt;
  };

  /** Records a decision so Undo can put back what was there. */
  function decide(
    slug: string,
    ref: string,
    command: UndoCommand,
    next: Decided,
  ): string {
    const key = id(slug, ref);
    const before = decided.get(key);
    decided.set(key, next);
    return record({
      slug,
      command,
      ref,
      revert: () => {
        if (before) decided.set(key, before);
        else decided.delete(key);
      },
    });
  }

  function current(slug: string, item: ReviewItem): ReviewItem {
    if (decided.has(id(slug, item.ref))) {
      throw new CommandFailedError({ kind: "decided" });
    }
    const live = queueOf(slug).find((i) => i.ref === item.ref);
    if (!live) throw new CommandFailedError({ kind: "not-found" });
    if (live.version !== item.version) {
      throw new CommandFailedError({
        kind: "clash",
        currentVersion: live.version,
      });
    }
    return live;
  }

  const review: ReviewSource = {
    peekQueue,
    peekCard: (_slug, ref) => DEMO_CARDS[ref] ?? EMPTY_CARD,
    async queue({ space }) {
      return peekQueue(space.slug);
    },
    async card({ item }) {
      return DEMO_CARDS[item.ref] ?? EMPTY_CARD;
    },
    async item({ space, ref }) {
      return queueOf(space.slug).find((i) => i.ref === ref) ?? null;
    },
    keep({ space, item, idempotencyKey }) {
      return command(idempotencyKey, (): DecisionResult => {
        const live = current(space.slug, item);
        // The server's answers, word for word where it has them.
        if (live.state === "conflict") throw inConflict(live);
        if (
          live.judge === "working" &&
          judging[live.ref]?.touchesDecision === true
        ) {
          throw new MemaxError(
            `Memax is still checking ${live.ref} against the decisions in force. Try again in a moment.`,
            "judge_pending",
            503,
            { retry_after: 1, ref: live.ref },
            1,
          );
        }
        const receipt = decide(space.slug, item.ref, "keep", {
          slug: space.slug,
          outcome: "kept",
          item: live,
        });
        return {
          ref: item.ref,
          outcome: "kept",
          version: live.version + 1,
          recompiled: DEMO_CARDS[item.ref]?.touches.targets?.length ?? null,
          receipt,
        };
      });
    },
    reject({ space, item, idempotencyKey }) {
      return command(idempotencyKey, (): DecisionResult => {
        const live = current(space.slug, item);
        const receipt = decide(space.slug, item.ref, "reject", {
          slug: space.slug,
          outcome: "rejected",
          item: live,
        });
        return {
          ref: item.ref,
          outcome: "rejected",
          version: live.version + 1,
          recompiled: null,
          receipt,
        };
      });
    },
    async conflict({ space, ref }) {
      if (decided.has(id(space.slug, ref))) return null;
      return DEMO_CONFLICTS[ref] ?? null;
    },
    resolveConflict({ space, ref, version, option, idempotencyKey }) {
      return command(idempotencyKey, (): DecisionResult => {
        const item = queueOf(space.slug).find((i) => i.ref === ref);
        if (!item) throw new CommandFailedError({ kind: "decided" });
        if (item.version !== version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: item.version,
          });
        }
        // Every answer settles the flagged side: kept (as an open
        // question when left open), or rejected when the decision stays.
        const outcome = option === "kept" ? "rejected" : "kept";
        const receipt = decide(space.slug, ref, "resolve", {
          slug: space.slug,
          outcome,
          item,
        });
        return {
          ref,
          outcome,
          version: item.version + 1,
          recompiled:
            option === "kept"
              ? null
              : (DEMO_CONFLICTS[ref]?.recompiles ?? null),
          receipt,
        };
      });
    },
  };

  const undo: UndoSource["undo"] = ({ receipt, idempotencyKey }) =>
    command(idempotencyKey, () => {
      const entry = journal.get(receipt);
      const refuse = (reason: string, ref: string | null) =>
        new MemaxError(`undo refused: ${reason}`, "undo_refused", 409, {
          reason,
          ...(ref ? { ref } : {}),
        });
      if (!entry) throw refuse("not_undoable", null);
      if (entry.undone) throw refuse("already_undone", entry.ref);
      if (clock() - entry.at > undoWindowMs(entry.command)) {
        throw refuse("window_passed", entry.ref);
      }
      // Journalled in order: anything after it on the same memory is a
      // later change it would lose.
      const entries = [...journal.values()];
      const later = entries
        .slice(entries.indexOf(entry) + 1)
        .some((e) => !e.undone && e.slug === entry.slug && e.ref === entry.ref);
      if (later) throw refuse("later_changes", entry.ref);
      entry.revert();
      entry.undone = true;
      return { refs: [entry.ref] };
    });

  // Memories kept outside Review this session (a gate's answer), newest first.
  const added = new Map<string, MemoryListItem[]>();
  const memories = createDemoMemories({
    decided,
    edits,
    unfolded,
    added,
    id,
    stamp,
    queueOf,
    command,
    journal: { record },
    holdsForJudge,
    inConflict,
  });

  /** The frame's overview with this session's decisions taken out of it. */
  function overview(slug: string, base: SpaceOverview): SpaceOverview {
    const mine = [...decided.values()].filter((d) => d.slug === slug);
    // An unfolded proposal is back in Review, too.
    const back = [...unfolded].some((k) => k.startsWith(`${slug}/`));
    if (mine.length === 0 && !arrived.get(slug)?.length && !back) return base;
    const left = queueOf(slug);
    const kept = mine.filter((d) => d.outcome === "kept").length;
    const was = (test: (i: ReviewItem) => boolean) =>
      mine.filter((d) => test(d.item)).length;
    return {
      ...base,
      waiting: left.length,
      // The oldest proposal, as the server's overview counts it.
      oldestWaitingAt:
        left
          .filter((i) => i.lifecycle === "proposed")
          .map((i) => i.at)
          .sort((a, b) => Date.parse(a) - Date.parse(b))[0] ?? null,
      reviewFilters: base.reviewFilters && {
        conflicts: Math.max(
          0,
          base.reviewFilters.conflicts - was((i) => i.state === "conflict"),
        ),
        external: Math.max(
          0,
          base.reviewFilters.external - was((i) => i.external),
        ),
        stale: Math.max(
          0,
          base.reviewFilters.stale - was((i) => i.state === "stale"),
        ),
      },
      waitingBreakdown: base.waitingBreakdown && {
        ...base.waitingBreakdown,
        proposals: left.filter((i) => i.lifecycle === "proposed").length,
        stale: left.filter((i) => i.state === "stale").length,
      },
      lastReview:
        mine.length > 0
          ? { at: stamp(), kept, rejected: mine.length - kept }
          : base.lastReview,
      memories: {
        ...base.memories,
        kept: base.memories.kept === null ? null : base.memories.kept + kept,
      },
    };
  }

  /** New proposals in a space's queue, after the boards' own (a pulled hand edit). */
  function propose(slug: string, items: ReviewItem[]) {
    arrived.set(slug, [...(arrived.get(slug) ?? []), ...items]);
  }

  /** A decision the viewer kept outside Review (a gate's answer): Memories lists it, kept by them. */
  function keptElsewhere(
    slug: string,
    memory: {
      ref: string;
      statement: string;
      section: Section;
      source: string;
    },
  ) {
    const row: MemoryListItem = {
      ref: memory.ref,
      statement: memory.statement,
      section: memory.section,
      state: "kept",
      receipt: { by: ZZ, action: "kept", at: stamp() },
      source: memory.source,
      note: null,
      forgotten: null,
    };
    added.set(slug, [row, ...(added.get(slug) ?? [])]);
  }

  /** What this session changed about a memory, for the demo's Brief. */
  const session = {
    edited: (slug: string, ref: string) => edits.get(id(slug, ref)),
    decided: (slug: string, ref: string) => decided.get(id(slug, ref)),
    arrived: (slug: string) => arrived.get(slug) ?? [],
  };

  return {
    review,
    memories,
    overview,
    propose,
    keptElsewhere,
    session,
    undo,
    journal: { record },
  };
}
