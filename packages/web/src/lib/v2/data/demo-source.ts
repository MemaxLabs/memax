import {
  DEMO_NEXT_REF,
  DEMO_NOW,
  DEMO_OVERVIEWS,
  DEMO_PNPM_PROPOSAL,
  DEMO_RIVER_ANSWER,
  DEMO_SPACES,
  DEMO_VIEWER,
} from "./demo-dataset";
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
}: { streamDelayMs?: number; commandDelayMs?: number } = {}): LedgerDataSource {
  let nextRef = DEMO_NEXT_REF;
  // Review and Memories (demo-records.ts); their decisions feed the overview.
  const records = createDemoRecords({
    now: () => new Date(DEMO_NOW),
    commandDelayMs,
  });
  const overview = (slug: string): SpaceOverview | undefined => {
    const base = DEMO_OVERVIEWS[slug];
    return base && records.overview(slug, base);
  };
  const kept = (ref: string, slug: string): KeepResult => ({
    ref,
    outcome: "kept",
    recompiled: overview(slug)?.targets?.inSync ?? null,
    undo: async () => {},
  });

  return {
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
    async keepProposal({ space, ref }) {
      return kept(ref, space.slug);
    },
  };
}

/** The one demo source the app uses. */
export const demoSource = createDemoSource();
