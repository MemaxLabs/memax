import {
  DEMO_NEXT_REF,
  DEMO_NOW,
  DEMO_OVERVIEWS,
  DEMO_PNPM_PROPOSAL,
  DEMO_RIVER_ANSWER,
  DEMO_SPACES,
  DEMO_VIEWER,
} from "./demo-dataset";
import { createDemoActivity } from "./activity-demo";
import { createDemoAgents } from "./agents-demo";
import { createDemoRecords } from "./demo-records";
import type { LedgerDataSource } from "./source";
import type { AskEvent, KeepResult, Section, SpaceOverview } from "./types";

/**
 * The demo source: the handoff dataset behind the LedgerDataSource
 * interface, for dev fixtures and Playwright. Answers synchronously
 * through `peek` (so screenshots never catch a loading frame), keeps a
 * fixed clock, and streams the one answer Ask.png draws.
 */

function sleep(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (ms <= 0 || signal?.aborted) return resolve();
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

/** The section a statement most likely belongs in. The real judge does better. */
function guessSection(statement: string): Section {
  if (/\b(prefer|prefers|i like|rather)\b/i.test(statement)) {
    return "preferences";
  }
  if (
    /\b(we (use|chose|picked|decided)|instead of|not use|over)\b/i.test(
      statement,
    )
  ) {
    return "decisions";
  }
  return "conventions";
}

export function createDemoSource({
  streamDelayMs = 14,
  commandDelayMs,
  judging,
  clock,
}: {
  streamDelayMs?: number;
  commandDelayMs?: number;
  /** The demo judge's script (demo-review-data.ts DEMO_JUDGING), for tests. */
  judging?: Parameters<typeof createDemoRecords>[0]["judging"];
  /** Real time for the judge and Undo's window, for tests. */
  clock?: () => number;
} = {}): LedgerDataSource {
  let nextRef = DEMO_NEXT_REF;
  // Review and Memories (demo-records.ts); their decisions feed the overview.
  const records = createDemoRecords({
    now: () => new Date(DEMO_NOW),
    commandDelayMs,
    judging,
    clock,
  });
  const overview = (slug: string): SpaceOverview | undefined => {
    const base = DEMO_OVERVIEWS[slug];
    return base && records.overview(slug, base);
  };
  // A person's own Remember isn't undoable on the server (no undo journal
  // for it), so, like the SDK source, it carries no receipt.
  const kept = (ref: string, slug: string): KeepResult => ({
    ref,
    outcome: "kept",
    recompiled: overview(slug)?.targets?.inSync ?? null,
    receipt: null,
  });

  return {
    ...createDemoActivity(),
    ...createDemoAgents(),
    kind: "demo",
    peek: {
      spaces: () => [...DEMO_SPACES],
      overview,
    },
    now: () => new Date(DEMO_NOW),
    viewer: DEMO_VIEWER,
    review: records.review,
    memories: records.memories,
    spaces: async () => [...DEMO_SPACES],
    overview: async (space) => {
      const found = overview(space.slug);
      if (!found) throw new Error(`No demo space "${space.slug}"`);
      return found;
    },
    async *ask({ space, question, signal }): AsyncGenerator<AskEvent> {
      await sleep(streamDelayMs * 8, signal);
      if (signal?.aborted) return;
      if (space.slug !== "memax-v2" || !/river|temporal/i.test(question)) {
        yield { type: "none" };
        return;
      }
      yield { type: "sources", sources: DEMO_RIVER_ANSWER.sources };
      for (const part of DEMO_RIVER_ANSWER.parts) {
        if (part.kind !== "text") {
          yield { type: "part", part };
          continue;
        }
        // Word by word, so the stream reads like one.
        for (const word of part.text.match(/\s*\S+/g) ?? []) {
          await sleep(streamDelayMs, signal);
          if (signal?.aborted) return;
          yield { type: "part", part: { kind: "text", text: word } };
        }
      }
      yield { type: "done" };
    },
    async checkRemember({ space, statement }) {
      const duplicate =
        space.slug === "memax-v2" && DEMO_PNPM_PROPOSAL.matches.test(statement)
          ? {
              ref: DEMO_PNPM_PROPOSAL.ref,
              agent: DEMO_PNPM_PROPOSAL.agent,
              proposedAt: DEMO_PNPM_PROPOSAL.proposedAt,
            }
          : null;
      return {
        duplicate,
        section: statement.trim() ? guessSection(statement) : null,
        condition: duplicate ? DEMO_PNPM_PROPOSAL.condition : null,
      };
    },
    async remember({ space }) {
      const ref = `M-${String(nextRef++).padStart(4, "0")}`;
      return kept(ref, space.slug);
    },
    // The near-duplicate offer keeps the proposal waiting in Review: the
    // same Keep as Review's, so it leaves the queue and can be undone.
    async keepProposal({ space, ref, idempotencyKey }) {
      const item = await records.review.item({ space, ref });
      if (!item) return kept(ref, space.slug);
      const result = await records.review.keep({ space, item, idempotencyKey });
      return {
        ref: result.ref,
        outcome: result.outcome === "kept" ? "kept" : "proposed",
        recompiled: result.recompiled,
        receipt: result.receipt ?? null,
      };
    },
    undo: records.undo,
  };
}

/** The one demo source the app uses. */
export const demoSource = createDemoSource();
