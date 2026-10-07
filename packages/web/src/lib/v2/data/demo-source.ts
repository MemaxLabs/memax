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
import { askingAgents, type WebSession } from "./gates";
import { createDemoGates } from "./gates-demo";
import { createDemoDevices } from "./devices-demo";
import { createDemoDream } from "./dream-demo";
import { createDemoImports } from "./imports-demo";
import { DEMO_V1_SPACE } from "./demo-switch-data";
import { createDemoSwitch } from "./switch-demo";
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
  judging,
  clock,
  webSession = true,
}: {
  streamDelayMs?: number;
  commandDelayMs?: number;
  /** How long a demo compile takes before its file reads in sync. */
  settleMs?: number;
  /** The demo judge's script (demo-review-data.ts DEMO_JUDGING), for tests. */
  judging?: Parameters<typeof createDemoRecords>[0]["judging"];
  /** Real time for the judge and Undo's window, for tests. */
  clock?: () => number;
  /** The demo plays the web app; tests set false to see D15's notice. */
  webSession?: WebSession;
} = {}): LedgerDataSource {
  let nextRef = DEMO_NEXT_REF;
  const allocRef = () => `M-${String(nextRef++).padStart(4, "0")}`;
  const now = () => new Date(DEMO_NOW);
  // acme-web is on V1 until switched in this session (switch-demo.ts).
  const switching = createDemoSwitch({ commandDelayMs });
  const spaces = () => [
    ...DEMO_SPACES,
    { ...DEMO_V1_SPACE, onV2: switching.onV2(DEMO_V1_SPACE.slug) },
  ];
  // Review and Memories (demo-records.ts); their decisions feed the overview.
  const records = createDemoRecords({ now, commandDelayMs, judging, clock });
  const agents = createDemoAgents();
  const targets = createDemoTargets({
    now,
    commandDelayMs,
    settleMs,
    nextRef: allocRef,
    propose: records.propose,
    decided: (slug, ref) => records.session.decided(slug, ref) !== undefined,
  });
  const brief = createDemoBrief({
    now,
    session: records.session,
    commandDelayMs,
  });
  // A gate's answer is kept as the viewer's decision: Memories lists it
  // and the Brief places it, as the compiler does.
  const gates = createDemoGates({
    now,
    commandDelayMs,
    webSession,
    nextRef: allocRef,
    kept: (slug, { ref, statement, gate }) => {
      records.keptElsewhere(slug, {
        ref,
        statement,
        section: "decisions",
        source: gate,
      });
      brief.remembered(slug, { ref, statement, section: "decisions" });
    },
    recompiled: (slug) => files(slug),
  });
  const today = createDemoToday({
    queue: (slug) => records.review.peekQueue?.(slug),
    gates: (slug) => gates.peekWaiting?.(slug),
    spaceAgents: (slug) => agents.agentsPeek?.spaceAgents(slug),
  });
  /** Review's records and the compile targets, as this session left them. */
  const recordsOverview = (
    slug: string,
    base: SpaceOverview,
  ): SpaceOverview => {
    const merged = records.overview(slug, base);
    const list = targets.peekList?.(slug) ?? [];
    if (list.length === 0) return merged;
    // The status line follows the targets once this session changes
    // them; the board's "5 agents in sync" stays while nothing drifted
    // or is held.
    const line = syncLineOf(list);
    return {
      ...merged,
      status:
        line?.kind === "drifted" ||
        line?.kind === "held" ||
        base.status.kind !== "in-sync"
          ? (line ?? base.status)
          : base.status,
      targets: {
        total: list.filter((t) => t.syncState !== "off").length,
        inSync: list.filter((t) => targetStatus(t).kind === "in_sync").length,
      },
    };
  };
  const overview = (slug: string): SpaceOverview | undefined => {
    const base = DEMO_OVERVIEWS[slug];
    if (!base) return undefined;
    const merged = recordsOverview(slug, base);
    // The questions still waiting, and the decisions answers kept.
    const waiting = gates.peekWaiting?.(slug) ?? [];
    const answered = gates.answeredCount(slug);
    return {
      ...merged,
      gatesWaiting: waiting.length,
      waitingBreakdown: merged.waitingBreakdown && {
        ...merged.waitingBreakdown,
        questions: [
          ...new Set([
            ...merged.waitingBreakdown.questions,
            ...askingAgents(waiting),
          ]),
        ],
      },
      memories:
        answered > 0 && merged.memories.kept !== null
          ? { ...merged.memories, kept: merged.memories.kept + answered }
          : merged.memories,
    };
  };
  /** A Keep recompiles every file the space compiles to (not ChatGPT's copy-out). */
  const files = (slug: string) => {
    const list = targets.peekList?.(slug) ?? [];
    if (list.length === 0) return overview(slug)?.targets?.inSync ?? null;
    return list.filter((t) => t.delivery !== "copy" && t.syncState !== "off")
      .length;
  };
  const remembered = new Map<string, KeepResult>();
  // A person's own Remember isn't undoable on the server (no undo journal
  // for it), so, like the SDK source, it carries no receipt.
  const kept = (ref: string, slug: string): KeepResult => ({
    ref,
    outcome: "kept",
    recompiled: files(slug),
    receipt: null,
  });

  return {
    ...createDemoActivity(),
    ...agents,
    kind: "demo",
    peek: {
      spaces,
      overview,
    },
    now,
    viewer: DEMO_VIEWER,
    review: records.review,
    memories: records.memories,
    brief,
    targets,
    today,
    gates,
    imports: createDemoImports({
      commandDelayMs,
      nextRef: allocRef,
      switched: switching.onV2,
    }),
    devices: createDemoDevices({ now, commandDelayMs }),
    dream: createDemoDream({ commandDelayMs }),
    switch: switching,
    spaces: async () => spaces(),
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
              lifecycle: "proposed" as const,
              agent: DEMO_PNPM_PROPOSAL.agent,
              writtenAt: DEMO_PNPM_PROPOSAL.proposedAt,
              match: "near" as const,
            }
          : null;
      return {
        duplicate,
        section: statement.trim() ? guessSection(statement) : null,
        condition: duplicate ? DEMO_PNPM_PROPOSAL.condition : null,
      };
    },
    async remember({ space, statement, section, idempotencyKey }) {
      // The same key is the same command, as on the server.
      const replay = remembered.get(idempotencyKey);
      if (replay) return replay;
      const ref = allocRef();
      // So the Brief places it, as the compiler does.
      brief.remembered(space.slug, { ref, statement, section });
      const result = kept(ref, space.slug);
      remembered.set(idempotencyKey, result);
      return result;
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
