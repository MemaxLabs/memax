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
import { createDemoBrief } from "./brief-demo";
import { createDemoRecords } from "./demo-records";
import type { LedgerDataSource } from "./source";
import { syncLineOf, targetStatus } from "./targets";
import { createDemoTargets } from "./targets-demo";
import { createDemoToday } from "./today-demo";
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
  settleMs,
}: {
  streamDelayMs?: number;
  commandDelayMs?: number;
  /** How long a demo compile takes before its file reads in sync. */
  settleMs?: number;
} = {}): LedgerDataSource {
  let nextRef = DEMO_NEXT_REF;
  const allocRef = () => `M-${String(nextRef++).padStart(4, "0")}`;
  const now = () => new Date(DEMO_NOW);
  // Review and Memories (demo-records.ts); their decisions feed the overview.
  const records = createDemoRecords({ now, commandDelayMs });
  const agents = createDemoAgents();
  const targets = createDemoTargets({
    now,
    commandDelayMs,
    settleMs,
    nextRef: allocRef,
    propose: records.propose,
  });
  const brief = createDemoBrief({
    now,
    session: records.session,
    commandDelayMs,
  });
  const today = createDemoToday({
    queue: (slug) => records.review.peekQueue?.(slug),
    spaceAgents: (slug) => agents.agentsPeek?.spaceAgents(slug),
  });
  const overview = (slug: string): SpaceOverview | undefined => {
    const base = DEMO_OVERVIEWS[slug];
    if (!base) return undefined;
    const merged = records.overview(slug, base);
    const list = targets.peekList?.(slug) ?? [];
    if (list.length === 0) return merged;
    // The status line follows the targets once this session changes
    // them; the board's "5 agents in sync" stays while nothing drifted.
    const line = syncLineOf(list);
    return {
      ...merged,
      status:
        line?.kind === "drifted" || base.status.kind !== "in-sync"
          ? (line ?? base.status)
          : base.status,
      targets: {
        total: list.filter((t) => t.syncState !== "off").length,
        inSync: list.filter((t) => targetStatus(t).kind === "in_sync").length,
      },
    };
  };
  /** A Keep recompiles every file the space compiles to (not ChatGPT's copy-out). */
  const files = (slug: string) => {
    const list = targets.peekList?.(slug) ?? [];
    if (list.length === 0) return overview(slug)?.targets?.inSync ?? null;
    return list.filter((t) => t.delivery !== "copy" && t.syncState !== "off")
      .length;
  };
  const kept = (ref: string, slug: string): KeepResult => ({
    ref,
    outcome: "kept",
    recompiled: files(slug),
    undo: async () => {},
  });

  return {
    ...createDemoActivity(),
    ...agents,
    kind: "demo",
    peek: {
      spaces: () => [...DEMO_SPACES],
      overview,
    },
    now,
    viewer: DEMO_VIEWER,
    review: records.review,
    memories: records.memories,
    brief,
    targets,
    today,
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
    async remember({ space, statement, section }) {
      const ref = allocRef();
      // So the Brief places it, as the compiler does.
      brief.remembered(space.slug, { ref, statement, section });
      return kept(ref, space.slug);
    },
    async keepProposal({ space, ref }) {
      return kept(ref, space.slug);
    },
  };
}

/** The one demo source the app uses. */
export const demoSource = createDemoSource();
