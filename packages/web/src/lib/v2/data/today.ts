/**
 * Today (epic 1.9, Main.png, TodayDark.png, MobileToday.png): what
 * changed while you were away and what needs you. Part of
 * LedgerDataSource (source.ts) as `source.today`; today-sdk.ts and
 * today-demo.ts implement it. The compiled files come from
 * `source.targets`, and the space's counts from the overview.
 *
 * What /v2 doesn't serve yet is said so, never invented: Dream editions
 * (plan §5.10) and handoffs in flight (Phase 4). No words live here.
 */
import type { GateView } from "./gates";
import type { MemoryNote } from "./memories";
import type { ReviewItem } from "./review";
import type { SpaceSummary } from "./types";

export type DreamItemKind = "merged" | "conflict" | "faded";

/** One line of an edition, as the card lists it. */
export interface DreamLine {
  kind: DreamItemKind;
  text: string;
  /** "9 notes → M-0219", "needs you", "restorable". */
  meta:
    | { kind: "folded"; notes: number; into: string }
    | { kind: "needs-you" }
    | { kind: "restorable" };
}

/** Last night's edition (D-). */
export interface DreamEdition {
  n: number;
  at: string;
  /** How long the run took, in seconds. */
  seconds: number | null;
  notes: number;
  facts: number;
  noteIds: string[];
  factIds: string[];
  lines: DreamLine[];
}

export type TodayDream =
  | { kind: "edition"; edition: DreamEdition }
  /** Nothing new to read last night, so no edition (empty nights cost nothing). */
  | { kind: "quiet" }
  /** PLACEHOLDER: Dream editions aren't built yet (plan §5.10). */
  | { kind: "unavailable" };

/** A session passed on and still running (H-). */
export interface HandoffInFlight {
  ref: string;
  /** Registry keys. */
  from: string;
  to: string;
  title: string;
  /** Questions it raised that wait on the viewer. */
  questions: number;
}

/** One connected agent's day. */
export interface AgentToday {
  id: string;
  /** Registry key. */
  agent: string;
  name: string;
  /** Its reads (R-) here since the start of the viewer's day; null when they weren't counted. */
  reads: number | null;
  kept: number;
  proposed: number;
  lastSeenAt: string | null;
}

export interface TodayData {
  /** Review's first items, in its order, with the counts Today's lede and panel read. */
  waiting: {
    items: ReviewItem[];
    /** The decision gates waiting on an answer, in Review's order (gates.ts). */
    gates: GateView[];
    /** Everything waiting: Review's memories and its gates. */
    total: number;
    proposals: number;
    stale: number;
    /** Agents (registry keys) waiting on a question from the viewer. */
    questions: string[];
    /** The line under a row's statement, where the source knows it. */
    notes: Record<string, MemoryNote>;
  };
  dream: TodayDream;
  /** When Dream runs, in the viewer's day ("03:00"); null while Dream isn't scheduled by /v2. */
  dreamAt: string | null;
  /** PLACEHOLDER: undefined until handoffs are served; null when none is in flight. */
  inFlight: HandoffInFlight | null | undefined;
  /** The space's connected agents, active first; null when they didn't load. */
  agents: { rows: AgentToday[]; connected: number } | null;
}

export interface TodaySource {
  /** The demo's day, on hand for the first render. */
  peek?(slug: string): TodayData | undefined;
  get(input: { space: SpaceSummary; signal?: AbortSignal }): Promise<TodayData>;
}

/** Whether an agent did anything today: wrote something, or was seen since the start of the viewer's day. */
export function activeToday(agent: AgentToday, dayStart: Date): boolean {
  return (
    agent.kept + agent.proposed > 0 ||
    (agent.lastSeenAt !== null &&
      new Date(agent.lastSeenAt).getTime() >= dayStart.getTime())
  );
}

/** Midnight of `now`'s day in a time zone. */
export function startOfDay(now: Date, timeZone: string): Date {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    hourCycle: "h23",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).formatToParts(now);
  const get = (type: string) =>
    Number(parts.find((p) => p.type === type)?.value ?? 0);
  const elapsed =
    ((get("hour") * 60 + get("minute")) * 60 + get("second")) * 1000 +
    now.getMilliseconds();
  return new Date(now.getTime() - elapsed);
}

/**
 * Today's "Waiting on you" (Main.png): a few of Review's items, one of
 * each kind first (a conflict, a proposal, a stale fact), then the rest
 * in Review's order. The kinds keep Review's relative order.
 */
export function pickWaiting(items: readonly ReviewItem[], n = 3): ReviewItem[] {
  const firsts = [
    items.find((i) => i.state === "conflict"),
    items.find((i) => i.state === "proposed"),
    items.find((i) => i.state === "stale"),
  ].filter((i): i is ReviewItem => i !== undefined);
  const picked = [...firsts];
  for (const item of items) {
    if (picked.length >= n) break;
    if (!picked.includes(item)) picked.push(item);
  }
  return picked.slice(0, n);
}

/**
 * The rows "Waiting on you" shows: every question an agent is waiting
 * on first, as Review lists them, then Review's items (pickWaiting) to
 * fill `n` rows, at least one of them when there are any.
 */
export function pickToday(
  gates: readonly GateView[],
  items: readonly ReviewItem[],
  n = 3,
): { gates: GateView[]; items: ReviewItem[] } {
  const shown = gates.slice(0, n);
  const room = Math.max(n - shown.length, items.length > 0 ? 1 : 0);
  return { gates: shown, items: pickWaiting(items, room) };
}
