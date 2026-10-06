import type {
  AnswerPart,
  AskSource,
  SpaceOverview,
  SpaceSummary,
  Viewer,
} from "./types";

/**
 * The handoff's demo dataset (memax-internal docs/v2/handoff/screens/
 * INDEX.md › Demo data), so the frame can be compared with the boards
 * screen for screen: Ziyang (ZZ) and Jiahao (JY), the memax-v2 project
 * space, five agents, Monday October 5 2026. Data, not copy: it stays in
 * English like the boards' own content.
 */

/** 14:40 in Vancouver, the boards' "now". */
export const DEMO_NOW = "2026-10-05T14:40:00-07:00";
const at = (time: string) => `2026-10-05T${time}:00-07:00`;

export const DEMO_VIEWER: Viewer = {
  initials: "ZZ",
  name: "Ziyang Zeng",
  timeZone: "America/Vancouver",
};

/** In the switcher's order, which is also ⌘1…⌘4. */
export const DEMO_SPACES: readonly SpaceSummary[] = [
  {
    id: "0192a7c0-0000-7000-8000-000000000001",
    slug: "personal",
    name: "Personal",
    kind: "personal",
    role: "owner",
    kept: 61,
    agents: 5,
    people: 1,
    waiting: 0,
  },
  {
    id: "0192a7c0-0000-7000-8000-000000000002",
    slug: "memax-v2",
    name: "memax-v2",
    kind: "project",
    role: "owner",
    repository: "MemaxLabs/memax",
    kept: 214,
    agents: 5,
    people: 2,
    waiting: 5,
  },
  {
    id: "0192a7c0-0000-7000-8000-000000000003",
    slug: "memax-web",
    name: "memax-web",
    kind: "project",
    role: "owner",
    repository: "MemaxLabs/memax-web",
    kept: 0,
    agents: 0,
    people: 1,
    waiting: 0,
  },
  {
    id: "0192a7c0-0000-7000-8000-000000000004",
    slug: "memax-team",
    name: "Memax team",
    kind: "team",
    role: "owner",
    kept: 38,
    agents: 7,
    people: 2,
    waiting: 2,
  },
];

const EMPTY: SpaceOverview = {
  waiting: 0,
  oldestWaitingAt: null,
  reviewFilters: { conflicts: 0, external: 0, stale: 0 },
  lastReview: null,
  waitingBreakdown: { proposals: 0, stale: 0, questions: [] },
  openHandoffs: 0,
  handoffs: 0,
  status: { kind: "no-agents" },
  dream: null,
  memories: { any: false, kept: 0, forgotten: 0 },
  brief: null,
  agents: { connected: 0, active: 0 },
  targets: { total: 0, inSync: 0 },
  activity: { any: false },
  decisions: null,
};

export const DEMO_OVERVIEWS: Readonly<Record<string, SpaceOverview>> = {
  personal: {
    ...EMPTY,
    // States board: "Last review today at 09:41 · 4 kept, 1 rejected".
    lastReview: { at: at("09:41"), kept: 4, rejected: 1 },
    status: { kind: "in-sync", agents: 5 },
    memories: { any: true, kept: 61, forgotten: 0 },
    brief: { title: "Personal", facts: 9, rewrittenAt: null },
    agents: { connected: 5, active: 5 },
    targets: { total: 4, inSync: 4 },
    activity: { any: true },
  },
  "memax-v2": {
    waiting: 5,
    oldestWaitingAt: at("11:40"),
    reviewFilters: { conflicts: 1, external: 1, stale: 1 },
    lastReview: { at: at("09:41"), kept: 4, rejected: 1 },
    waitingBreakdown: { proposals: 4, stale: 1, questions: ["codex"] },
    openHandoffs: 1,
    handoffs: 14,
    status: { kind: "drifted", agent: "cursor" },
    dream: { notes: 34, facts: 6 },
    memories: { any: true, kept: 214, forgotten: 3 },
    brief: {
      title: "Memax V2 engineering brief",
      facts: 14,
      rewrittenAt: at("03:12"),
    },
    agents: { connected: 6, active: 5 },
    targets: { total: 4, inSync: 3 },
    activity: { any: true },
    decisions: null,
  },
  "memax-web": EMPTY,
  "memax-team": {
    ...EMPTY,
    waiting: 2,
    oldestWaitingAt: at("13:20"),
    reviewFilters: { conflicts: 0, external: 0, stale: 0 },
    waitingBreakdown: { proposals: 2, stale: 0, questions: [] },
    status: { kind: "in-sync", agents: 7 },
    memories: { any: true, kept: 38, forgotten: 0 },
    brief: { title: "Memax team", facts: 38, rewrittenAt: null },
    agents: { connected: 7, active: 7 },
    targets: { total: 3, inSync: 3 },
    activity: { any: true },
    decisions: 38,
  },
};

/** Ask.png: "Why did we pick River over Temporal?" in memax-v2. */
export const DEMO_RIVER_ANSWER: { sources: AskSource[]; parts: AnswerPart[] } =
  {
    sources: [
      {
        n: 1,
        statement:
          "Background jobs run on River, Postgres‑backed. We do not use Temporal.",
        state: "kept",
        receipt: {
          person: "ZZ",
          action: "kept",
          at: "2026-10-02T10:12:00-07:00",
          ref: "M-0219",
        },
      },
      {
        n: 2,
        statement:
          "Temporal spike: replay non-determinism in the dream workers; too much to operate for two people.",
        state: "merged",
        receipt: {
          agent: "dream",
          action: "merged",
          at: "2026-08-21T03:12:00-07:00",
          ref: "N-0882",
        },
      },
      {
        n: 3,
        statement:
          "River is in go.mod, and jobs live in packages/server/internal/queue.",
        state: "kept",
        receipt: {
          agent: "claude-code",
          action: "verified",
          at: at("14:31"),
          ref: "PR #212",
        },
      },
    ],
    parts: [
      {
        kind: "highlight",
        text: "River runs on the Postgres we already operate",
      },
      {
        kind: "text",
        text: ", so a job commits in the same transaction as the rows it touches.",
      },
      { kind: "cite", n: 1 },
      {
        kind: "text",
        text: " Temporal was tried in August and dropped: a second cluster to run, and its replay rules fought the dream workers.",
      },
      { kind: "cite", n: 2 },
      {
        kind: "text",
        text: " You kept the decision on Oct 2, and it still matches the code.",
      },
      { kind: "cite", n: 3 },
    ],
  };

/** Remember.png: Codex proposed the pnpm catalog rule 22 minutes before "now". */
export const DEMO_PNPM_PROPOSAL = {
  ref: "M-0432",
  agent: "codex",
  proposedAt: at("14:18"),
  matches: /pnpm.*catalog|catalog.*pnpm/i,
  condition: { subject: "pnpm-workspace.yaml", rest: "has a catalog" },
} as const;

/** The next display ID the demo hands out (Remember.png shows M-0439). */
export const DEMO_NEXT_REF = 439;
