/**
 * Review's data (Review.png, ReviewEdit.png, ReviewConflict.png): the
 * queue, what one card shows, and the decisions. Part of
 * LedgerDataSource (source.ts) as `source.review`; sdk-review.ts and
 * the demo (demo-records.ts) implement it.
 */
import type { Actor, DecisionResult, TargetLine } from "./records";
import type { Section, SpaceSummary } from "./types";

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
  /** The latest receipt: who proposed or flagged it, and when. */
  by: Actor | null;
  action: "proposed" | "updated" | "flagged";
  at: string;
  /** The session it came from ("3e1a"). */
  session: string | null;
  /** The kept memory this proposal would change ("Updates M-0156"). */
  updates: string | null;
  /**
   * The kept memory this contradicts. Only the judge links conflicts,
   * and it doesn't exist yet, so only the demo sets it; without it
   * there is nothing to compare.
   */
  conflictsWith: string | null;
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
     * Why these: `links` (the memory an update replaces) or `section`
     * (kept memories in the same section, the only signal /v2 serves
     * until the judge relates memories).
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

export type ConflictOptionKind = "proposal" | "kept" | "both" | "open";

export interface ConflictOption {
  kind: ConflictOptionKind;
  /** The judge's short answer ("Fly.io everywhere"); `open` has none. */
  label: string | null;
  /** The judge's own line, when it wrote one; the catalogue's otherwise. */
  detail: string | null;
  /** The decision as it will read when this option is chosen. */
  decision: string;
}

export interface ConflictData {
  /** The judge's question ("Fly.io or Railway for the v2 API?"). */
  question: string;
  kept: ConflictSide;
  proposal: ConflictSide;
  options: ConflictOption[];
  /** The option the judge suggests, preselected. */
  suggested: number;
  /** Compiled files and the agents told, for the footer. */
  recompiles: number | null;
  tells: string[];
}

export interface ReviewSource {
  /** The demo has the first page on hand, so screenshots never catch a loading frame. */
  peekQueue?(slug: string): ReviewQueue | undefined;
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
  /** Keeps the person's answer as a decision. PLACEHOLDER in the SDK (no resolve command). */
  resolveConflict(input: {
    space: SpaceSummary;
    ref: string;
    option: ConflictOptionKind;
    decision: string;
    idempotencyKey: string;
  }): Promise<DecisionResult>;
}
