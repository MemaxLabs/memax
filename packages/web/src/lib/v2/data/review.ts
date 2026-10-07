/**
 * Review's data (Review.png, ReviewEdit.png, ReviewConflict.png): the
 * queue, what one card shows, and the decisions. Part of
 * LedgerDataSource (source.ts) as `source.review`; sdk-review.ts and
 * the demo (demo-records.ts) implement it.
 */
import type { Actor, DecisionResult, TargetLine } from "./records";
import type { Section, SpaceSummary } from "./types";

/**
 * Where the judge is with a proposal (plan §5.8): still checking it
 * (`working`, Review's neutral mark) or it couldn't (`failed`: reviewed
 * as usual, and Dream catches what it missed). Null once judged, and for
 * what the judge doesn't look at (a person's own keep).
 */
export type JudgeMark = "working" | "failed" | null;

/** One memory waiting on a person, as the queue lists it. */
export interface ReviewItem {
  /** Display ID ("M-0430"); commands address it with the space. */
  ref: string;
  /** The version reviewed: the If-Match of every decision on it. */
  version: number;
  statement: string;
  section: Section;
  /**
   * How the queue draws it: a proposal (italic), a conflict (italic,
   * vermilion mark) or a kept memory gone stale (dotted underline).
   */
  state: "proposed" | "conflict" | "stale";
  /** Proposals take Keep, Edit and Reject; kept memories don't. */
  lifecycle: "proposed" | "kept";
  /** Quarantined: it quotes content the agent didn't write. */
  external: boolean;
  /**
   * Who wrote it and when (a proposal, or a write the judge returned), or
   * who flagged a kept memory.
   */
  by: Actor | null;
  /**
   * The queue's verb: `returned` is a Write agent's write that was kept
   * at once until the judge found it contradicts a decision in force.
   */
  action: "proposed" | "updated" | "flagged" | "returned";
  at: string;
  /**
   * Rule 11: the judge put this write back in Review. `decision` is the
   * decision in force its `returned` receipt names.
   */
  returned?: { decision: string | null } | null;
  /** The session it came from ("3e1a"). */
  session: string | null;
  /** The kept memory this proposal would change ("Updates M-0156"). */
  updates: string | null;
  /**
   * The decision in force this contradicts: the judge's `conflicts_with`
   * link. Without it there is nothing to compare.
   */
  conflictsWith: string | null;
  judge: JudgeMark;
  /** The space it goes into when that isn't the one being reviewed. */
  intoSpace: string | null;
}

export interface ReviewQueue {
  items: ReviewItem[];
  /** The whole queue, across pages: the rail's count. */
  total: number;
  nextCursor: string | null;
}

/** A kept memory the card relates to ("This touches"). */
export interface TouchedMemory {
  ref: string;
  statement: string;
  by: Actor | null;
  at: string;
}

/** Everything the card needs beyond the queue row. */
export interface ReviewCardData {
  /** The kept statement an update would change, for the word diff. */
  before: { ref: string; statement: string } | null;
  /** The kept statement a conflict contradicts. */
  conflict: { ref: string; statement: string } | null;
  /** The words the agent read, and where ("modelcontextprotocol.io · spec …"). */
  evidence: { quote: string; source: string } | null;
  /** What an external claim was read from ("the MCP specification"). */
  readFrom: string | null;
  /** Where O opens: the source's address. */
  sourceUrl: string | null;
  touches: {
    memories: TouchedMemory[];
    /**
     * Why these: `links` (what the judge linked: the memory an update
     * replaces, the decision a conflict contradicts) or `section` (kept
     * memories in the same section, when nothing is linked).
     */
    basis: "links" | "section";
    /** Keeping an update merges the memory it replaces into it. */
    replacesOnKeep: boolean;
    /** Null until compile targets are served (PLACEHOLDER in the SDK). */
    targets: TargetLine[] | null;
  };
}

/** One side of a conflict (ReviewConflict.png). */
export interface ConflictSide {
  ref: string;
  /** Its version now: the If-Match of the answer, for the flagged side. */
  version: number;
  statement: string;
  by: Actor | null;
  at: string;
  /** Data rows under the receipt, as the judge wrote them. */
  why: string | null;
  source: string | null;
  /** The kept side: files and reads ("4 files · read 61 times"). */
  reaches: { files: number; reads: number } | null;
  /** The proposed side: a file or link in mono, and when it changed. */
  evidence: { code: string; changedAt: string } | null;
  session: string | null;
}

/**
 * ReviewConflict's answers, relative to the flagged side: `proposal`
 * wins (spec keep_this), `kept` stays (keep_other), `both` stand, each
 * narrowed (keep_both), or it stays `open` (leave_open).
 */
export type ConflictOptionKind = "proposal" | "kept" | "both" | "open";

/** What an answer does to one side (spec ConflictChange). */
export type ConflictChange =
  | "kept"
  | "rejected"
  | "superseded"
  | "faded"
  | "open"
  | "stays";

export interface ConflictOption {
  kind: ConflictOptionKind;
  /** A short answer someone wrote ("Fly.io everywhere"); the catalogue's otherwise. */
  label: string | null;
  /** A line someone wrote about it; the catalogue's otherwise. */
  detail: string | null;
  /** What it does to each side, the flagged side first. */
  effects: { ref: string; change: ConflictChange }[];
  /** Whether the person may take it. */
  allowed: boolean;
  /** Why not, when policy says so: a policy code and the server's English. */
  refusal: { code: string | null; message: string | null } | null;
  /** The decision as it will read: the side that stands. Empty for `both` and `open`. */
  decision: string;
  /**
   * `both`: each side's words, to narrow before keeping. The server
   * narrows both sides rather than writing a third memory.
   */
  narrowed: { proposal: string; kept: string } | null;
}

export interface ConflictData {
  /** A question someone wrote ("Fly.io or Railway for the v2 API?"); null when there's none. */
  question: string | null;
  /** The decision's area ("deploy target"), to ask by when there's no question. */
  area: string | null;
  kept: ConflictSide;
  proposal: ConflictSide;
  /** In the board's order: proposal, kept, both, open. */
  options: ConflictOption[];
  /** The option someone suggests, preselected; null when nobody does. */
  suggested: number | null;
  /** Compiled files and the agents told, for the footer. */
  recompiles: number | null;
  tells: string[];
}

export interface ReviewSource {
  /** The demo has the first page on hand, so screenshots never catch a loading frame. */
  peekQueue?(slug: string): ReviewQueue | undefined;
  peekCard?(slug: string, ref: string): ReviewCardData | undefined;
  /** The queue: proposals, then conflicts, then stale; oldest first in each. */
  queue(input: {
    space: SpaceSummary;
    cursor?: string;
    signal?: AbortSignal;
  }): Promise<ReviewQueue>;
  card(input: {
    space: SpaceSummary;
    item: ReviewItem;
    signal?: AbortSignal;
  }): Promise<ReviewCardData>;
  /** One memory as the queue would list it now; null once nothing waits on it. */
  item(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<ReviewItem | null>;
  /**
   * Keeps a proposal. Before the judge has looked at one that touches a
   * decision in force, this throws `busy` (CommandFailure) with the
   * seconds to wait; a proposal the judge flagged throws `decided` (409
   * invalid_transition) until its conflict is settled.
   */
  keep(input: {
    space: SpaceSummary;
    item: ReviewItem;
    idempotencyKey: string;
  }): Promise<DecisionResult>;
  reject(input: {
    space: SpaceSummary;
    item: ReviewItem;
    reason?: string;
    idempotencyKey: string;
  }): Promise<DecisionResult>;
  /** Null when the memory has no linked conflict to compare. */
  conflict(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<ConflictData | null>;
  /**
   * Settles a conflict with the person's answer, through the ledger.
   * Rule 11 for `both`: narrower words that touch another decision in
   * force are saved and wait for the judge (`judgePending`, with the
   * flagged side's new `version`); the same answer sent again throws
   * `busy` (judge) until the judge has looked, then settles it or throws
   * `in-conflict` with the decision the words contradict.
   */
  resolveConflict(input: {
    space: SpaceSummary;
    /** The flagged side: every answer is relative to it. */
    ref: string;
    /** The decision in force on the other side. */
    other: string;
    /** The flagged side's version the person compared (If-Match). */
    version: number;
    option: ConflictOptionKind;
    /** `both`: the narrower words for the flagged side, and for the other. */
    statement?: string;
    otherStatement?: string;
    idempotencyKey: string;
  }): Promise<DecisionResult>;
}
