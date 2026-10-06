import { DEMO_TARGETS } from "./demo-targets-data";
import type { Actor, TargetLine } from "./records";
import type { ConflictData, ReviewCardData, ReviewItem } from "./review";

/**
 * The handoff's demo records for Review (screens/INDEX.md › Demo data,
 * and the Review, ReviewEdit and ReviewConflict boards), so those
 * screens reproduce the boards. Memories' rows are in
 * demo-memories-data.ts. Data, not copy: it stays in English like the
 * boards' own content.
 */

/** A time on the boards' Monday (Vancouver), or on another day. */
export const at = (time: string, day = "2026-10-05") =>
  `${day}T${time}:00-07:00`;

export const ZZ: Actor = {
  kind: "person",
  self: true,
  initials: "ZZ",
  name: "Ziyang",
};
export const JY: Actor = {
  kind: "person",
  self: false,
  initials: "JY",
  name: "Jiahao",
};
export const agent = (key: string): Actor => ({ kind: "agent", agent: key });
export const DREAM: Actor = { kind: "dream" };

/** Review.png's "Keeping recompiles": the files a Keep rewrites (ChatGPT's copy-out isn't one). */
const V2_TARGETS: TargetLine[] = (DEMO_TARGETS["memax-v2"] ?? []).filter(
  (t) => t.delivery !== "copy",
);

/** Memory.png's "Reaches" for M-0219: every target, ChatGPT included. */
export const V2_REACHES: TargetLine[] = [...(DEMO_TARGETS["memax-v2"] ?? [])];

const item = (
  fields: Partial<ReviewItem> &
    Pick<ReviewItem, "ref" | "statement" | "section" | "by" | "at">,
): ReviewItem => ({
  version: 1,
  state: "proposed",
  lifecycle: "proposed",
  external: false,
  action: "proposed",
  session: null,
  updates: null,
  conflictsWith: null,
  judge: null,
  intoSpace: null,
  ...fields,
});

/** Review.png's queue, in the board's order. */
export const DEMO_QUEUES: Record<string, ReviewItem[]> = {
  "memax-v2": [
    item({
      ref: "M-0430",
      statement:
        "MCP write tools must ask for confirmation with input_required.",
      section: "conventions",
      external: true,
      by: agent("claude-code"),
      action: "updated",
      at: at("13:40"),
      session: "3e1a",
      updates: "M-0156",
    }),
    item({
      ref: "M-0431",
      statement: "Deploy the v2 API to Fly.io in iad and ams.",
      section: "decisions",
      state: "conflict",
      by: agent("codex"),
      at: at("14:26"),
      session: "9f1c",
      conflictsWith: "M-0174",
    }),
    item({
      ref: "M-0432",
      statement: "Pin shared dependency versions with the pnpm catalog.",
      section: "conventions",
      by: agent("codex"),
      at: at("14:18"),
      session: "8f2c",
    }),
    item({
      ref: "M-0433",
      statement: "Prefers diffs over prose summaries in code review.",
      section: "preferences",
      by: agent("chatgpt"),
      at: at("11:40"),
      intoSpace: "Personal",
    }),
    item({
      ref: "M-0187",
      statement: "Ask memax answers with the Haiku tier.",
      section: "conventions",
      state: "stale",
      lifecycle: "kept",
      by: DREAM,
      action: "flagged",
      at: at("03:12"),
    }),
  ],
  // Not drawn on a board: the team space shows the judge at work. The
  // judge couldn't run on M-0444, and is still checking M-0445, which
  // touches a decision in force, so a Keep waits for it (DEMO_JUDGING).
  "memax-team": [
    item({
      ref: "M-0444",
      statement:
        "The web app reads feature flags from the API, never from env vars.",
      section: "conventions",
      by: agent("claude-code"),
      at: at("14:35"),
      session: "71c0",
      judge: "failed",
    }),
    item({
      ref: "M-0445",
      statement: "Release notes go out on Thursdays, after the staging soak.",
      section: "decisions",
      by: agent("codex"),
      at: at("13:20"),
      judge: "working",
    }),
  ],
};

/**
 * One of the judge's folds, in the team space (no board draws one): a
 * proposal folded into the kept memory it repeats. Its receipt is what
 * Undo addresses; unfolded, it comes back to Review as this proposal.
 */
export const DEMO_FOLD = {
  slug: "memax-team",
  receipt: "0192a7c0-0000-7000-8000-f0000000f01d",
  into: "M-0310",
  at: at("14:33"),
  item: item({
    ref: "M-0446",
    statement: "Reviews need a member who isn't the author.",
    section: "decisions",
    by: agent("codex"),
    at: at("14:32"),
    session: "4d1b",
  }),
} as const;

/**
 * The demo's judge: proposals it's still checking, how long the check
 * takes from the first time the demo serves them, and whether they touch
 * a decision in force (then Keep answers `busy` until the check is done).
 */
export const DEMO_JUDGING: Record<
  string,
  { afterMs: number; touchesDecision: boolean }
> = {
  "M-0445": { afterMs: 6000, touchesDecision: true },
};

export const M0156 = {
  ref: "M-0156",
  statement: "MCP write tools must ask for confirmation through elicitation.",
  by: ZZ,
  at: at("10:02", "2026-08-14"),
};
export const M0102 = {
  ref: "M-0102",
  statement: "Remote MCP is stateless streamable HTTP with OAuth 2.1.",
  by: ZZ,
  at: at("16:20", "2026-09-30"),
};
export const M0174 = {
  ref: "M-0174",
  statement: "Deploy the v2 API to Railway for its preview environments.",
  by: JY,
  at: at("15:05", "2026-09-18"),
};
export const M0071 = {
  ref: "M-0071",
  statement: "pnpm workspaces only. Never run `npm install` at the root.",
  by: JY,
  at: at("11:30", "2026-08-29"),
};

const none = {
  before: null,
  conflict: null,
  evidence: null,
  readFrom: null,
  sourceUrl: null,
};

/** What each card shows beyond its queue row. */
export const DEMO_CARDS: Record<string, ReviewCardData> = {
  "M-0430": {
    before: { ref: M0156.ref, statement: M0156.statement },
    conflict: null,
    evidence: {
      quote:
        "“A server that needs input returns an input_required result, and the client retries with the answer.”",
      source: "modelcontextprotocol.io · spec 2026‑07‑28",
    },
    readFrom: "the MCP specification",
    sourceUrl: "https://modelcontextprotocol.io/specification/2026-07-28",
    touches: {
      memories: [M0156, M0102],
      basis: "links",
      replacesOnKeep: true,
      targets: V2_TARGETS,
    },
  },
  "M-0431": {
    ...none,
    conflict: { ref: M0174.ref, statement: M0174.statement },
    touches: {
      memories: [M0174],
      basis: "links",
      replacesOnKeep: false,
      targets: V2_TARGETS,
    },
  },
  "M-0432": {
    ...none,
    touches: {
      memories: [M0071],
      basis: "section",
      replacesOnKeep: false,
      targets: V2_TARGETS,
    },
  },
  "M-0433": {
    ...none,
    touches: {
      memories: [],
      basis: "section",
      replacesOnKeep: false,
      targets: [...(DEMO_TARGETS.personal ?? [])],
    },
  },
  "M-0187": {
    ...none,
    touches: {
      memories: [],
      basis: "section",
      replacesOnKeep: false,
      targets: V2_TARGETS,
    },
  },
};

const allowed = { allowed: true, refusal: null, narrowed: null } as const;

/**
 * ReviewConflict.png: M-0431 against M-0174, with the effects the server
 * plans for each answer. "Both" narrows each side (the server's
 * keep_both) rather than writing the board's third memory; its words are
 * the board's decision, split between the two sides.
 */
export const DEMO_CONFLICTS: Record<string, ConflictData> = {
  "M-0431": {
    question: "Fly.io or Railway for the v2 API?",
    area: "deploy target",
    kept: {
      ...M0174,
      version: 1,
      why: "Simpler preview environments, one per pull request.",
      source: "A chat with Claude on Sep 18",
      reaches: { files: 4, reads: 61 },
      evidence: null,
      session: null,
    },
    proposal: {
      ref: "M-0431",
      version: 1,
      statement: "Deploy the v2 API to Fly.io in iad and ams.",
      by: agent("codex"),
      at: at("14:26"),
      why: "The current API and workers already run on Fly.io in iad and ams, next to Postgres.",
      source: null,
      reaches: null,
      evidence: {
        code: "infra/fly/api.toml",
        changedAt: at("09:10", "2026-10-01"),
      },
      session: "Cloud task 9f1c, during handoff H-0093",
    },
    options: [
      {
        ...allowed,
        kind: "proposal",
        label: "Fly.io everywhere",
        detail: null,
        effects: [
          { ref: "M-0431", change: "kept" },
          { ref: "M-0174", change: "superseded" },
        ],
        decision: "Deploy the v2 API to Fly.io in iad and ams.",
      },
      {
        ...allowed,
        kind: "kept",
        label: "Railway, as kept",
        detail: null,
        effects: [
          { ref: "M-0431", change: "rejected" },
          { ref: "M-0174", change: "stays" },
        ],
        decision: "Deploy the v2 API to Railway for its preview environments.",
      },
      {
        ...allowed,
        kind: "both",
        label: "Both, each with its own scope",
        detail: "Production on Fly.io; previews on Railway.",
        effects: [
          { ref: "M-0431", change: "kept" },
          { ref: "M-0174", change: "stays" },
        ],
        decision: "",
        narrowed: {
          proposal: "The v2 API runs on Fly.io in iad and ams.",
          kept: "Preview environments for pull requests run on Railway.",
        },
      },
      {
        ...allowed,
        kind: "open",
        label: null,
        detail: "Codex keeps both configs behind a flag.",
        effects: [
          { ref: "M-0431", change: "open" },
          { ref: "M-0174", change: "open" },
        ],
        decision: "",
      },
    ],
    suggested: 2,
    recompiles: 4,
    tells: ["codex", "cursor", "claude-code"],
  },
};
