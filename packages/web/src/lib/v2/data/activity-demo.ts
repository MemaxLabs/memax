import type {
  ActivityActor,
  ActivityData,
  ActivityEntry,
  ActivityPage,
  ViaPart,
} from "./activity";
import { DEMO_AGENT_IDS } from "./agents-demo";
import { DEMO_FOLD } from "./demo-review-data";
import type { GateView } from "./gates";
import { DEMO_GATES } from "./gates-demo";

/**
 * Activity.png's receipts for memax-v2, newest first, at Monday October 5
 * 2026, 14:40 in Vancouver. The demo plays an API that also serves the
 * week's totals and the words a receipt points at, so the screen
 * reproduces the board. Data, not copy.
 */

const at = (day: string, time: string) => `2026-${day}T${time}:00-07:00`;
const you: ActivityActor = { kind: "you", initials: "ZZ" };
const agent = (key: string): ActivityActor => ({
  kind: "agent",
  agent: key,
  connectionId: DEMO_AGENT_IDS[key],
});
let n = 0;
const uid = () => `0192a7c0-0000-7000-8000-f${String(++n).padStart(11, "0")}`;

function entry(
  e: Omit<ActivityEntry, "id" | "rawVia" | "session" | "source" | "reason"> &
    Partial<Pick<ActivityEntry, "rawVia" | "session" | "source" | "reason">>,
): ActivityEntry {
  const viaPart = e.via.find(
    (p): p is Extract<ViaPart, { kind: "via" }> => p.kind === "via",
  );
  const sessionPart = e.via.find(
    (p): p is Extract<ViaPart, { kind: "session" }> => p.kind === "session",
  );
  return {
    id: uid(),
    rawVia: viaPart?.via ?? null,
    session: sessionPart?.ref ?? null,
    source: null,
    reason: null,
    ...e,
  };
}

const MEMAX_V2: ActivityEntry[] = [
  entry({
    at: at("10-05", "14:40"),
    actor: agent("codex"),
    action: "asked",
    object: { kind: "gate", ref: "H-0093", id: uid() },
    via: [{ kind: "session", ref: "9f1c", cloud: true }],
    detail: {
      kind: "question",
      text: "Which deploy target should the v2 API use?",
    },
  }),
  entry({
    at: at("10-05", "14:31"),
    actor: agent("claude-code"),
    action: "verified",
    object: { kind: "memory", ref: "M-0219", id: uid() },
    via: [{ kind: "session", ref: "7c2f" }],
    detail: { kind: "verified" },
  }),
  entry({
    at: at("10-05", "14:31"),
    actor: { kind: "memax" },
    action: "compiled",
    object: { kind: "compile", ref: "C-0881", id: uid() },
    via: [{ kind: "targets", done: 3, total: 4 }],
    rawVia: "system",
    detail: {
      kind: "compiled",
      targets: ["CLAUDE.md", "AGENTS.md", "chatgpt"],
      skipped: { agent: "cursor" },
    },
  }),
  entry({
    at: at("10-05", "14:20"),
    actor: you,
    action: "handed_off",
    object: { kind: "handoff", ref: "H-0093", id: uid() },
    via: [{ kind: "via", via: "web" }],
    detail: { kind: "handoff", to: "codex", carries: 7 },
  }),
  entry({
    at: at("10-05", "14:02"),
    actor: agent("claude-code"),
    action: "read",
    object: { kind: "read", ref: "R-5512", id: uid() },
    via: [
      { kind: "via", via: "mcp" },
      { kind: "session", ref: "7c2f" },
    ],
    detail: { kind: "read", memories: 12, brief: true },
  }),
  entry({
    at: at("10-05", "13:48"),
    actor: you,
    action: "kept",
    object: { kind: "memory", ref: "M-0427", id: uid() },
    via: [
      { kind: "via", via: "review" },
      { kind: "from", agent: "cursor" },
    ],
    detail: {
      kind: "quote",
      text: "Name compiled files after the tool they serve.",
    },
  }),
  entry({
    at: at("10-05", "09:41"),
    actor: you,
    action: "rejected",
    object: { kind: "memory", ref: "M-0429", id: uid() },
    via: [
      { kind: "via", via: "review" },
      { kind: "from", agent: "cursor" },
    ],
    reason: "superseded by M-0219.",
    detail: { kind: "quote", text: "Use Temporal for long-running workflows." },
  }),
  entry({
    at: at("10-05", "03:12"),
    actor: { kind: "dream" },
    action: "merged",
    object: { kind: "dream", ref: "D-0214", id: uid() },
    via: [{ kind: "edition", n: 214 }],
    rawVia: "system",
    detail: { kind: "dream", notes: 34, facts: 6, stale: 1, faded: 11 },
  }),
  entry({
    at: at("10-04", "16:40"),
    actor: { kind: "repository" },
    action: "drifted",
    object: { kind: "compile", ref: "C-0874", id: uid() },
    via: [{ kind: "repository" }],
    detail: { kind: "drift", path: ".cursor/rules/memax.mdc" },
  }),
  entry({
    at: at("10-04", "16:02"),
    actor: you,
    action: "edited",
    object: { kind: "brief", ref: "B-0042", id: uid() },
    via: [{ kind: "via", via: "web" }],
    detail: { kind: "brief", facts: 1 },
  }),
  entry({
    at: at("10-04", "12:30"),
    actor: agent("cursor"),
    action: "read",
    object: { kind: "read", ref: "R-5470", id: uid() },
    via: [
      { kind: "via", via: "mcp" },
      { kind: "surface", surface: "ide" },
    ],
    detail: { kind: "read", memories: 18, brief: false },
  }),
  entry({
    at: at("10-03", "10:12"),
    actor: you,
    action: "forgot",
    object: { kind: "memory", ref: "M-0201", id: uid() },
    via: [{ kind: "via", via: "web" }],
    detail: { kind: "forgot", files: 4, agents: 5 },
  }),
];

/** The team space's two questions (gates-demo.ts), as their `asked` receipts. */
const asked = (gate: GateView): ActivityEntry =>
  entry({
    at: gate.askedAt,
    actor: agent(gate.agent),
    action: "asked",
    object: { kind: "gate", ref: gate.ref, id: gate.id },
    via: [
      { kind: "via", via: "mcp" },
      ...(gate.session
        ? [{ kind: "session" as const, ref: gate.session }]
        : []),
    ],
    detail: { kind: "question", text: gate.question },
  });
const [TEAM_FIRST, TEAM_SECOND] = DEMO_GATES["memax-team"] ?? [];

// The team space's receipts (no board draws them): the judge folding a
// repeat into what's kept, with the receipt Undo addresses, and the two
// questions agents asked.
const MEMAX_TEAM: ActivityEntry[] = [
  ...(TEAM_SECOND ? [asked(TEAM_SECOND)] : []),
  {
    ...entry({
      at: DEMO_FOLD.at,
      actor: { kind: "memax" },
      action: "merged",
      object: { kind: "memory", ref: DEMO_FOLD.item.ref, id: uid() },
      via: [{ kind: "via", via: "system" }],
      source: { kind: "memory", ref: DEMO_FOLD.into },
      reason: `A near-verbatim repeat of ${DEMO_FOLD.into}.`,
    }),
    id: DEMO_FOLD.receipt,
  },
  entry({
    at: DEMO_FOLD.item.at,
    actor: agent("codex"),
    action: "proposed",
    object: { kind: "memory", ref: DEMO_FOLD.item.ref, id: uid() },
    via: [
      { kind: "via", via: "mcp" },
      { kind: "session", ref: "4d1b" },
    ],
  }),
  ...(TEAM_FIRST ? [asked(TEAM_FIRST)] : []),
];

const PAGES: Readonly<Record<string, ActivityPage>> = {
  "memax-team": { entries: MEMAX_TEAM, nextCursor: null, totals: null },
  "memax-v2": {
    entries: MEMAX_V2,
    nextCursor: null,
    totals: {
      reads: 693,
      proposals: 31,
      kept: 22,
      rejected: 4,
      forgotten: 1,
      compiles: 41,
    },
  },
};

const EMPTY: ActivityPage = { entries: [], nextCursor: null, totals: null };

export function createDemoActivity(): ActivityData {
  const page = (slug: string) => PAGES[slug] ?? EMPTY;
  return {
    activityPeek: (slug) => page(slug),
    activity: async ({ space }) => page(space.slug),
  };
}
