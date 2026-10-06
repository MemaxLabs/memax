import type { AgentConnectionView } from "./agents";
import { JY, at } from "./demo-review-data";
import { askingAgents, type GateView } from "./gates";
import type { MemoryNote } from "./memories";
import type { ReviewQueue } from "./review";
import type { AgentToday, DreamEdition, TodayData, TodaySource } from "./today";

/**
 * The demo's Today (Main.png): Dream's edition No. 214, Review's queue
 * and the decision gates as this session left them, H-0093 in flight,
 * and the agents' day. Data, not copy.
 */

const NB = "‑";

/** Edition No. 214: 34 notes folded into 6 facts at 03:12, in 41 seconds. */
const EDITION_214: DreamEdition = {
  n: 214,
  at: at("03:12"),
  seconds: 41,
  notes: 34,
  facts: 6,
  noteIds: Array.from({ length: 34 }, (_, i) => `N-${1180 + i}`),
  factIds: ["M-0219", "M-0102", "M-0434", "M-0435", "M-0436", "M-0437"],
  lines: [
    {
      kind: "merged",
      text: "Background jobs run on River, not Temporal.",
      meta: { kind: "folded", notes: 9, into: "M-0219" },
    },
    {
      kind: "conflict",
      text: "Deploy target: Fly.io (Codex) or Railway (Jiahao)?",
      meta: { kind: "needs-you" },
    },
    {
      kind: "faded",
      text: "11 notes unread by any agent for 60 days.",
      meta: { kind: "restorable" },
    },
  ],
};

/** Main.png's "Agents today". */
const V2_AGENTS: AgentToday[] = [
  {
    id: "cc",
    agent: "claude-code",
    name: "Claude Code",
    reads: 41,
    kept: 3,
    proposed: 0,
    lastSeenAt: at("14:38"),
  },
  {
    id: "cx",
    agent: "codex",
    name: "Codex",
    reads: 18,
    kept: 0,
    proposed: 2,
    lastSeenAt: at("14:26"),
  },
  {
    id: "cu",
    agent: "cursor",
    name: "Cursor",
    reads: 12,
    kept: 0,
    proposed: 0,
    lastSeenAt: at("13:40"),
  },
  {
    id: "gpt",
    agent: "chatgpt",
    name: "ChatGPT",
    reads: 3,
    kept: 0,
    proposed: 1,
    lastSeenAt: at("11:40"),
  },
];

const NOTES: Record<string, MemoryNote> = {
  "M-0431": { kind: "conflict", with: "M-0174", keptBy: JY, askedBy: null },
  "M-0187": {
    kind: "stale",
    changedAt: at("10:20", "2026-09-11"),
    source: "PR #198",
  },
};

export function createDemoToday({
  queue,
  gates,
  spaceAgents,
}: {
  /** Review's queue as this session left it. */
  queue: (slug: string) => ReviewQueue | undefined;
  /** The gates still waiting, as this session left them (gates-demo.ts). */
  gates: (slug: string) => GateView[] | undefined;
  spaceAgents: (slug: string) => AgentConnectionView[] | undefined;
}): TodaySource {
  function today(slug: string): TodayData {
    const items = queue(slug)?.items ?? [];
    const asked = gates(slug) ?? [];
    const v2 = slug === "memax-v2";
    const connections = (spaceAgents(slug) ?? []).filter(
      (c) => c.state !== "disconnected",
    );
    // Main.png's lede names Codex's deploy-target question from H-0093
    // while M-0431 waits; the handoff's gate isn't listed (Phase 4).
    const handoffQuestion =
      v2 && items.some((i) => i.ref === "M-0431") ? ["codex"] : [];
    return {
      waiting: {
        items,
        gates: asked,
        total: items.length + asked.length,
        proposals: items.filter((i) => i.lifecycle === "proposed").length,
        stale: items.filter((i) => i.state === "stale").length,
        questions: [...new Set([...handoffQuestion, ...askingAgents(asked)])],
        notes: NOTES,
      },
      dream: v2 ? { kind: "edition", edition: EDITION_214 } : { kind: "quiet" },
      dreamAt: "03:00",
      inFlight: v2
        ? {
            ref: "H-0093",
            from: "claude-code",
            to: "codex",
            title: `Finish the MCP 2026${NB}07${NB}28 migration`,
            questions: 1,
          }
        : null,
      agents: v2
        ? { rows: V2_AGENTS, connected: 5 }
        : {
            rows: connections.map((c) => ({
              id: c.id,
              agent: c.agent,
              name: c.name,
              reads: c.reads7d === null ? null : 0,
              kept: 0,
              proposed: 0,
              lastSeenAt: c.lastSeenAt,
            })),
            connected: connections.length,
          },
    };
  }
  return {
    peek: today,
    async get({ space }) {
      return today(space.slug);
    },
  };
}
