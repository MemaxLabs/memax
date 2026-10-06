/**
 * The V2 app frame's data model: what the rail, the space switcher, the
 * status line, the page headers and ⌘K read. Two sources implement it
 * (source.ts): the SDK (memax.v2, for signed-in people) and the handoff
 * demo dataset (dev fixtures and Playwright).
 *
 * Names follow the /v2 contract (packages/server/openapi/v2.yaml) where
 * it has them. A `null` count means "not served yet": the UI leaves it
 * out rather than showing a zero it doesn't know.
 *
 * No words live here: everything a person reads is built from these
 * values by the i18n catalogue.
 */

export type SpaceKind = "personal" | "project" | "team";
export type SpaceRole = "owner" | "member" | "viewer";
/** Where a memory sits in the Brief (spec `Section`). */
export type Section =
  | "decisions"
  | "conventions"
  | "preferences"
  | "open_question";

/** The signed-in person. */
export interface Viewer {
  /** Their user id: what a person's receipts name (actor_id), so receipts and agent connections that name it read as "you". */
  id?: string;
  /** What their receipts show ("ZZ"). */
  initials: string;
  name: string;
  /** IANA zone; receipts, Today's date and Dream use it. */
  timeZone: string;
}

/** A space as the switcher lists it. */
export interface SpaceSummary {
  id: string;
  /** The URL segment: /memax-v2/today. */
  slug: string;
  /** Named by people ("memax-v2", "Personal", "Memax team"). */
  name: string;
  kind: SpaceKind;
  role: SpaceRole;
  /** The repository it compiles for, if any ("MemaxLabs/memax"). */
  repository?: string;
  kept: number | null;
  /** Connected agents that are not paused. */
  agents: number | null;
  people: number | null;
  /** Waiting on the viewer: the switcher's badge for other spaces. */
  waiting: number | null;
}

/** The rail's status line (plan §6.4): "5 agents in sync", "Cursor file drifted". */
export type SyncLine =
  | { kind: "in-sync"; agents: number }
  /** From the compile targets: every file on disk matches its compile. */
  | { kind: "files-in-sync"; files: number }
  /**
   * A compiled file was edited by hand: `agent` is the registry key of
   * the tool it belongs to ("cursor"), or `file` names it when no one
   * agent owns it (AGENTS.md).
   */
  | { kind: "drifted"; agent?: string; file?: string }
  | { kind: "compiling" }
  /** Compiled, but the CLI hasn't written these files to disk yet. */
  | { kind: "waiting-delivery"; files: number }
  | { kind: "no-agents" }
  /** PLACEHOLDER: the API serves no compile targets yet. */
  | { kind: "not-compiling" };

/** Everything the frame and the page headers show about one space. */
export interface SpaceOverview {
  /** Waiting on the viewer: Review's total, and the rail's ochre count. */
  waiting: number;
  /** When the oldest item in Review arrived. */
  oldestWaitingAt: string | null;
  /** Review's filter counts, when served. */
  reviewFilters: { conflicts: number; external: number; stale: number } | null;
  lastReview: { at: string; kept: number; rejected: number } | null;
  /** What's waiting, by kind, for Today's lede. */
  waitingBreakdown: {
    proposals: number;
    stale: number;
    /** Agents (registry keys) waiting on a question. */
    questions: string[];
  } | null;
  /** The rail's Handoffs count: in flight or waiting on the viewer. */
  openHandoffs: number | null;
  handoffs: number | null;
  status: SyncLine;
  /** Last night's Dream edition. */
  dream: { notes: number; facts: number } | null;
  memories: { any: boolean; kept: number | null; forgotten: number | null };
  /** Null when the space has no Brief yet. */
  brief: {
    title: string | null;
    facts: number | null;
    /** When Dream last rewrote it. */
    rewrittenAt: string | null;
  } | null;
  agents: { connected: number; active: number } | null;
  /** Compiled files. */
  targets: { total: number; inSync: number } | null;
  activity: { any: boolean };
  /** Decisions in force (team spaces). */
  decisions: number | null;
}

// ⌘K

/** One run of an answer: plain, highlighted (the span that matched), or a citation numeral. */
export type AnswerPart =
  | { kind: "text"; text: string }
  | { kind: "highlight"; text: string }
  | { kind: "cite"; n: number };

/** A receipt as Ask shows it under a source. */
export interface ReceiptLine {
  /** A person's initials, or an agent's registry key. */
  person?: string;
  agent?: string;
  /** A past-tense receipt verb. */
  action: "kept" | "merged" | "verified" | "proposed" | "flagged";
  at: string;
  /** "M-0219", "N-0882", "PR #212". */
  ref: string;
}

export interface AskSource {
  n: number;
  statement: string;
  /** Merged notes read quieter than kept memories. */
  state: "kept" | "merged";
  receipt: ReceiptLine;
}

export type AskEvent =
  | { type: "sources"; sources: AskSource[] }
  | { type: "part"; part: AnswerPart }
  | { type: "done" }
  /** Nothing in the space answers it. */
  | { type: "none" }
  /** PLACEHOLDER: the source can't ask yet (no /v2 ask endpoint). */
  | { type: "unavailable" };

/** What Remember shows while the person types: the synchronous near-duplicate check (plan §5.8). */
export interface RememberCheck {
  duplicate: {
    ref: string;
    /** Registry key of the agent that proposed it. */
    agent: string;
    proposedAt: string;
  } | null;
  /** The section the check suggests. */
  section: Section | null;
  /** A "stays true while" condition: `subject` set in mono, then the rest. */
  condition: { subject: string; rest: string } | null;
}

export interface RememberInput {
  space: SpaceSummary;
  statement: string;
  section: Section;
  /** One per intent; the same key on a retry (spec: Idempotency-Key). */
  idempotencyKey: string;
}

export interface KeepResult {
  /** The memory's display ID. */
  ref: string;
  /** `proposed` when policy sent it to Review (a viewer, say). */
  outcome: "kept" | "proposed";
  /** Compiled files rewritten, when the source knows. */
  recompiled: number | null;
  /**
   * The receipt Undo addresses, when the command can be undone: a Keep
   * can. A person's own Remember can't (the server journals no undo for
   * it), so it carries none.
   */
  receipt?: string | null;
}
