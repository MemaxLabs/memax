import { CommandFailedError } from "./command-error";
import { createDemoMemories } from "./demo-memories";
import { DEMO_CARDS, DEMO_CONFLICTS, DEMO_QUEUES } from "./demo-review-data";
import type { DecisionResult } from "./records";
import type {
  ReviewCardData,
  ReviewItem,
  ReviewQueue,
  ReviewSource,
} from "./review";
import type { SpaceOverview } from "./types";

/**
 * The demo's Review and Memories: the boards' records behind the
 * ReviewSource and MemoriesSource interfaces, with the decisions made
 * in this browser session applied on top (so Keep leaves the queue,
 * the rail's count drops and Memories shows it kept). Commands settle
 * after a short delay, so the optimistic seal is visible first, and
 * replay by idempotency key like the server does.
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

export function createDemoRecords({
  now,
  commandDelayMs = 240,
}: {
  now: () => Date;
  commandDelayMs?: number;
}) {
  const decided = new Map<string, Decided>();
  const edits = new Map<string, { statement: string; version: number }>();
  const replays = new Map<string, DecisionResult>();
  let nextRef = 450;
  const id = (slug: string, ref: string) => `${slug}/${ref}`;
  const stamp = () => now().toISOString();

  function queueOf(slug: string): ReviewItem[] {
    return (DEMO_QUEUES[slug] ?? [])
      .filter((item) => !decided.has(id(slug, item.ref)))
      .map((item) => {
        const edit = edits.get(id(slug, item.ref));
        return edit ? { ...item, ...edit } : item;
      });
  }

  function peekQueue(slug: string): ReviewQueue {
    const items = queueOf(slug);
    return { items, total: items.length, nextCursor: null };
  }

  /** Runs a command once per idempotency key, like the server. */
  async function command(
    key: string,
    run: () => DecisionResult,
  ): Promise<DecisionResult> {
    await sleep(commandDelayMs);
    const replay = replays.get(key);
    if (replay) return replay;
    const result = run();
    replays.set(key, result);
    return result;
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
    keep({ space, item, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = current(space.slug, item);
        decided.set(id(space.slug, item.ref), {
          slug: space.slug,
          outcome: "kept",
          item: live,
        });
        return {
          ref: item.ref,
          outcome: "kept",
          version: live.version + 1,
          recompiled: DEMO_CARDS[item.ref]?.touches.targets?.length ?? null,
        };
      });
    },
    reject({ space, item, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = current(space.slug, item);
        decided.set(id(space.slug, item.ref), {
          slug: space.slug,
          outcome: "rejected",
          item: live,
        });
        return {
          ref: item.ref,
          outcome: "rejected",
          version: live.version + 1,
          recompiled: null,
        };
      });
    },
    async conflict({ space, ref }) {
      if (decided.has(id(space.slug, ref))) return null;
      return DEMO_CONFLICTS[ref] ?? null;
    },
    resolveConflict({ space, ref, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const item = queueOf(space.slug).find((i) => i.ref === ref);
        if (!item) throw new CommandFailedError({ kind: "decided" });
        decided.set(id(space.slug, ref), {
          slug: space.slug,
          outcome: "kept",
          item,
        });
        return {
          ref: `M-${String(nextRef++).padStart(4, "0")}`,
          outcome: "kept",
          version: 1,
          recompiled: DEMO_CONFLICTS[ref]?.recompiles ?? null,
        };
      });
    },
  };

  const memories = createDemoMemories({
    decided,
    edits,
    id,
    stamp,
    queueOf,
    command,
  });

  /** The frame's overview with this session's decisions taken out of it. */
  function overview(slug: string, base: SpaceOverview): SpaceOverview {
    const mine = [...decided.values()].filter((d) => d.slug === slug);
    if (mine.length === 0) return base;
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
      lastReview: { at: stamp(), kept, rejected: mine.length - kept },
      memories: {
        ...base.memories,
        kept: base.memories.kept === null ? null : base.memories.kept + kept,
      },
    };
  }

  return { review, memories, overview };
}
