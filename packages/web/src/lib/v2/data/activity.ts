/**
 * The activity domain of the V2 data layer (epic 1.1): every receipt in a
 * space, newest first. LedgerDataSource extends ActivityData (source.ts);
 * the SDK source implements it in activity-sdk.ts (memax.v2.receipts) and
 * the demo in activity-demo.ts (the Activity board's rows).
 *
 * An entry is a receipt plus what the source knows to say about it.
 * Receipts never hold a memory's words, so `detail` carries the extras
 * only a source that has them can give (the demo's quotes, a compile's
 * targets). No words live here: activity-copy.ts builds the sentences.
 */
import type { Autonomy } from "./agents";
import type { SpaceSummary } from "./types";

/** The receipt verbs (spec ReceiptAction), plus reads, which are not receipts (plan §5.3). */
export type ActivityAction =
  | "proposed"
  | "kept"
  | "edited"
  | "rejected"
  | "merged"
  | "flagged"
  | "resolved"
  | "verified"
  | "faded"
  | "restored"
  | "forgot"
  | "moved"
  | "compiled"
  | "handed_off"
  | "answered"
  | "undid"
  | "connected"
  | "autonomy_changed"
  | "paused"
  | "resumed"
  | "disconnected"
  | "read"
  /** A decision gate an agent raised (H-…). */
  | "asked"
  /** A compiled file edited by hand outside Memax. */
  | "drifted";

export type ObjectKind =
  | "memory"
  | "note"
  | "brief"
  | "target"
  | "compile"
  | "handoff"
  | "gate"
  | "dream"
  | "agent"
  | "space"
  | "read";

/** Who made the change. */
export type ActivityActor =
  | { kind: "you"; initials: string }
  /** Another person. Receipts carry only their id, so their name may be unknown. */
  | { kind: "person"; initials?: string; name?: string }
  /** An agent: a registry key, and the connection when known. */
  | { kind: "agent"; agent: string; connectionId?: string }
  | { kind: "dream" }
  | { kind: "memax" }
  | { kind: "repository" };

/** The surface a change came through (spec Via). */
export type ActivityVia =
  | "web"
  | "cli"
  | "mcp"
  | "review"
  | "api"
  | "email"
  | "slack"
  | "github"
  | "linear"
  | "import"
  | "system";

/** One piece of the "via" column; joined with " · ". */
export type ViaPart =
  | { kind: "via"; via: ActivityVia }
  /** A session: an agent's cloud task, or a plain session id. */
  | { kind: "session"; ref: string; cloud?: boolean }
  /** Review of a proposal that came from an agent ("from Cursor"). */
  | { kind: "from"; agent: string }
  /** Where the agent runs ("IDE"). */
  | { kind: "surface"; surface: "cli" | "ide" | "cloud" | "chat" }
  | { kind: "targets"; done: number; total: number }
  | { kind: "edition"; n: number }
  | { kind: "repository" }
  /** A source pointer from the receipt ("PR #212"). */
  | { kind: "source"; ref: string };

/** What only some sources can say about a receipt. */
export type ActivityDetail =
  /** The memory's words, where the source has them. */
  | { kind: "quote"; text: string }
  | { kind: "question"; text: string }
  /** Verified against the code: still true. */
  | { kind: "verified" }
  /** Compiled targets (paths, or "chatgpt" for the ChatGPT project), and one left alone for its local edit. */
  | { kind: "compiled"; targets: string[]; skipped?: { agent: string } }
  | { kind: "handoff"; to: string; carries: number }
  | { kind: "read"; memories: number; brief: boolean }
  | {
      kind: "dream";
      notes: number;
      facts: number;
      stale: number;
      faded: number;
    }
  | { kind: "drift"; path: string }
  | { kind: "brief"; facts: number }
  | { kind: "forgot"; files: number; agents: number }
  /** An agent's autonomy set, or its connection made, at this level. */
  | { kind: "autonomy"; level: Autonomy };

export interface ActivityEntry {
  /** The receipt's id. */
  id: string;
  /** When it happened (occurred_at). */
  at: string;
  actor: ActivityActor;
  action: ActivityAction;
  object: {
    kind: ObjectKind;
    /** The display ID ("M-0219"); for an agent receipt, the agent's slug. */
    ref: string;
    id: string;
  };
  via: ViaPart[];
  /** For the CSV: the raw surface and session. */
  rawVia: ActivityVia | null;
  session: string | null;
  source: { kind: string; ref: string } | null;
  /** Why, if given (a reject's reason). Redacted once the memory is forgotten. */
  reason: string | null;
  detail?: ActivityDetail;
}

/** This week's counts, as the aside shows them. */
export interface WeeklyTotals {
  /** Null when reads aren't known (they aren't receipts). */
  reads: number | null;
  proposals: number;
  kept: number;
  rejected: number;
  forgotten: number;
  compiles: number;
}

export interface ActivityPage {
  entries: ActivityEntry[];
  /** Pass back as `cursor` for older receipts; null at the end. */
  nextCursor: string | null;
  /**
   * The space's totals for the last 7 days, when the API serves them.
   * PLACEHOLDER: /v2 doesn't, so the SDK source leaves this null and the
   * page counts what it has loaded (activity-totals.ts).
   */
  totals: WeeklyTotals | null;
}

/** The activity part of LedgerDataSource. */
export interface ActivityData {
  /** The first page on hand for the first render. The demo has it. */
  readonly activityPeek?: (slug: string) => ActivityPage | undefined;
  /** One page of the space's receipts, newest first. */
  activity(input: {
    space: SpaceSummary;
    cursor?: string;
    signal?: AbortSignal;
  }): Promise<ActivityPage>;
}

/** The board's filters. */
export type ActivityFilter =
  | "all"
  | "writes"
  | "reads"
  | "compiles"
  | "forgets";
export const ACTIVITY_FILTERS: readonly ActivityFilter[] = [
  "all",
  "writes",
  "reads",
  "compiles",
  "forgets",
];

/** Which filter an entry falls under: reads, compiles (and drift), forgets, or a write. */
export function activityCategory(
  entry: ActivityEntry,
): Exclude<ActivityFilter, "all"> {
  switch (entry.action) {
    case "read":
      return "reads";
    case "compiled":
    case "drifted":
      return "compiles";
    case "forgot":
      return "forgets";
    default:
      return "writes";
  }
}

export function matchesFilter(
  entry: ActivityEntry,
  filter: ActivityFilter,
): boolean {
  return filter === "all" || activityCategory(entry) === filter;
}
