/**
 * Memories and a memory's page (Memories.png, Memory.png): the record
 * by section, one memory with its lineage, and editing. Part of
 * LedgerDataSource (source.ts) as `source.memories`; sdk-memories.ts
 * and the demo (demo-records.ts) implement it.
 */
import type {
  Actor,
  DecisionResult,
  DisplayState,
  RailReceipt,
  RecordAction,
  TargetLine,
} from "./records";
import type { Section, SpaceSummary } from "./types";

/** Memories' state filter (Memories.png): All, Waiting, Stale, Merged, Forgotten. */
export type MemoryFilter = "all" | "waiting" | "stale" | "merged" | "forgotten";

/** The line under a row's statement. Data; the catalogue words it. */
export type MemoryNote =
  /** "Conflicts with M-0174, kept by Jiahao. Codex asked you to decide." */
  | {
      kind: "conflict";
      with: string;
      keptBy: Actor | null;
      /** The agent waiting on the answer, a registry key. */
      askedBy: string | null;
    }
  /**
   * "Its source changed on Sep 11 · PR #198" (`source`), or "Its date to
   * check again has passed" (`date`).
   */
  | {
      kind: "stale";
      cause: StaleCauseKind;
      changedAt: string;
      source: string | null;
    }
  /** "Merged into M-0219 by Dream" */
  | { kind: "merged"; into: string; by: Actor | null }
  /** "No agent has read it in 60 days. Restore it, or let it rest." */
  | { kind: "faded"; days: number };

/** A forgotten memory: no words, only what the tombstone says. */
export interface Forgotten {
  at: string;
  /** Who forgot it, when it wasn't the reader. */
  by: string | null;
  /** "removed from 4 files and 5 agents", when known. */
  detail: string | null;
}

export interface MemoryListItem {
  ref: string;
  /** Empty once forgotten. */
  statement: string;
  section: Section;
  state: DisplayState;
  /** The latest receipt, for the rail. */
  receipt: RailReceipt | null;
  /** The rail's second line after the ID ("PR #212", "session 3e1a"). */
  source: string | null;
  note: MemoryNote | null;
  forgotten: Forgotten | null;
}

export interface MemoryPage {
  items: MemoryListItem[];
  nextCursor: string | null;
  /** Each section's size across the whole record; null until /v2 counts. */
  sectionCounts: Partial<Record<Section, number>> | null;
  /** How many match the filter across every page; null until /v2 counts. */
  total: number | null;
}

/** One line of a memory's life, oldest first (Lineage). */
export interface LineageEntry {
  key: string;
  action: RecordAction;
  by: Actor | null;
  at: string;
  /** The receipt's reason or the source's own line, as written. */
  detail: string | null;
  /** `merged`: how many notes were folded in. */
  count: number | null;
  /** `handed_off`: the agent it went to and the handoff. */
  to: { agent: string; ref: string } | null;
  /** `merged` by the judge: the memory it was folded into. */
  into?: string | null;
  /** One of the judge's folds, still undoable: its receipt, and until when. */
  undo?: FoldUndo | null;
  /**
   * `flagged`: which flag it set, when the receipt says. A conflict names
   * the memory it contradicts when it can (`with`); null when the receipt
   * doesn't tell (its reason was forgotten).
   */
  flag?: LineageFlag | null;
}

/** The flag a `flagged` receipt set. */
export type LineageFlag =
  | { kind: "stale" }
  | { kind: "conflict"; with: string | null };

/** Undo for one of the judge's folds (14 days, by anyone who may keep). */
export interface FoldUndo {
  receipt: string;
  /** When the window closes (ISO). */
  until: string;
}

export interface SourceLine {
  key: string;
  /** Spec SourceKind: session, pr, file, url, issue, email, note, import. */
  kind: string;
  /** What people see ("PR #212 · Move jobs to River"). */
  label: string;
  /** Where it lives ("GitHub", "Claude Code"); the kind's word otherwise. */
  provider: string | null;
  url: string | null;
}

/**
 * One "stays true while" condition, as data (the catalogue words it): the
 * kinds of the record's typed conditions, and the date to check again.
 */
export type ConditionLine = { key: string } & (
  | { kind: "path_exists"; path: string | null; glob: string | null }
  | { kind: "file_contains"; path: string; text: string; word: boolean }
  | {
      kind: "dep_present";
      manifest: string;
      name: string;
      range: string | null;
    }
  | { kind: "file_unchanged"; path: string }
  | {
      kind: "pr_state";
      repo: string;
      number: number;
      state: "open" | "merged" | "closed";
    }
  /** Its date to check again (stale_after). */
  | { kind: "date"; at: string }
);

export interface MergedNote {
  ref: string;
  statement: string;
  by: Actor | null;
  at: string;
  /** A proposal the judge folded into this one, still undoable. */
  undo?: FoldUndo | null;
}

export interface MemoryRecord {
  ref: string;
  version: number;
  statement: string;
  section: Section;
  state: DisplayState;
  lifecycle: "proposed" | "kept" | "merged" | "faded" | "forgotten";
  /** The Keep the seal is for. */
  kept: { by: Actor | null; at: string } | null;
  /** The latest receipt, for a memory that isn't simply kept. */
  latest: RailReceipt | null;
  /** Its reads by agents over the last 13 months; null when they couldn't be counted. */
  reads: number | null;
  /**
   * It's in a compiled file agents load without Memax seeing the loads,
   * so it may be read more than `reads` says (MemoryReads.unobserved_target).
   */
  readsUnobserved?: boolean;
  /**
   * How far it reaches: the compiled files that hold it (from the
   * targets) and the agents that read it (from its reads). Either is
   * null when the source doesn't know it.
   */
  reach: { files: number | null; agents: number | null } | null;
  lineage: LineageEntry[];
  /** What was merged into it: Dream's notes, the judge's folds. Null when the source can't say. */
  merged: { notes: MergedNote[]; total: number } | null;
  sources: SourceLine[];
  /** Null until compile targets are served (PLACEHOLDER in the SDK). */
  reaches: TargetLine[] | null;
  conditions: ConditionLine[];
  /** The last check of its conditions (null: not checked yet). */
  checked: { at: string; by: Actor | null } | null;
  forgotten: Forgotten | null;
  /** Agents' requests that a person forget it, waiting (Forget, or Keep it). */
  forgetRequests?: ForgetRequestLine[];
  /**
   * The repositories it applies in (spec MemoryScope.repos, "owner/name"):
   * a personal fact init tied to one repository's files. A home file
   * leaves it out. Absent or empty is everywhere.
   */
  repos?: string[];
}

/** Why a memory is stale, as its marks say it: a date that passed, or a source that changed. */
export type StaleCauseKind = "date" | "source";

/** One line of a diff of a cited file, as VerifyStale shows it. */
export interface HunkLine {
  op: "del" | "add" | "eq";
  text: string;
}

/**
 * Why a memory is stale, said as data (VerifyStale's "What changed"):
 * its date to check again passed, or a pull request changed the file it
 * cites (with the lines around it, when they can be shown).
 */
export type StaleCause =
  | { kind: "date"; staleAfter: string }
  | {
      kind: "source";
      /** "PR #198". */
      ref: string;
      title: string | null;
      by: Actor | null;
      at: string;
      hunk: { path: string; lines: HunkLine[] } | null;
    };

/**
 * GET /v2/memories/{ref}/stale as VerifyStale reads it: the words being
 * verified and their version (the If-Match), who flagged it and when,
 * the keep before it and the reads since, and why.
 */
export interface StaleWhy {
  ref: string;
  version: number;
  statement: string;
  kind: "fact" | "decision";
  /** False once someone answered it. */
  stale: boolean;
  /** When it was flagged ("Stale since Sep 11"); null when the source can't say. */
  since: string | null;
  flaggedBy: Actor | null;
  /** Its latest keep, or verification, for the receipt under the words. */
  kept: { by: Actor | null; at: string; action: "kept" | "verified" } | null;
  /** Agents' reads since that keep; null when they couldn't be counted. */
  readsSince: number | null;
  causes: StaleCause[];
  /** Words a source suggests for "Update it", and who suggested them from what. */
  suggestion: { statement: string; by: Actor; from: string } | null;
}

/** A person's answer to a stale flag (spec VerifyAnswer). */
export type VerifyAnswer = "still_true" | "update" | "forget";

export interface VerifyResult {
  ref: string;
  answer: VerifyAnswer;
  /** The memory's version after it (its next If-Match). */
  version: number;
  /** The receipt Undo addresses; null for forget, which nothing undoes. */
  receipt: string | null;
}

/** Why a memory goes with another one's Forget (spec CarryReason). */
export type CarryReason = "folded" | "updates" | "cites" | "space";

/**
 * What a Forget would do, read before the person confirms it (GET
 * /v2/memories/{ref}/forget-preview): States' "Removes the words from
 * Memax, 4 compiled files and 5 agents."
 */
export interface ForgetPreview {
  /** Sent as If-Match. */
  version: number;
  /** What goes with it, forgotten in the same step. */
  carries: { ref: string; reason: CarryReason }[];
  /** The compiled files that hold it. */
  files: number;
  /** The copy-outs (ChatGPT) that hold it. */
  copies: number;
  /** The agents told on their next read. */
  agents: number;
  /** Null when the person may forget it; else why not (spec PolicyCode). */
  refusal: { code: string | null; message: string | null } | null;
}

/** An agent's request that a person forget a memory, still waiting. */
export interface ForgetRequestLine {
  /** A Ledger registry key, or the agent's name when it has none. */
  agent: string;
  reason: string | null;
  at: string;
}

/** A step of how a memory was forgotten (spec StepKind). */
export type TombstoneStepKind =
  | "asked"
  | "removed"
  | "target"
  | "artifacts"
  | "caches"
  | "ledger"
  | "agent";

export interface TombstoneStepLine {
  key: string;
  kind: TombstoneStepKind;
  /** done, waiting, held, failed or unreachable (spec StepStatus). */
  status: "done" | "waiting" | "held" | "failed" | "unreachable";
  /** Spec StepReason: hand_edit, stopped, copy, delivery, compiling, paused, disconnected, next_read. */
  reason: string | null;
  at: string | null;
  target: { label: string; kind: string; delivery: string } | null;
  /** The compile (C-) that rewrote it. */
  compile: string | null;
  /** An agent told: its registry key (or name). */
  agent: string | null;
  count: number | null;
}

/** A copy Memax can't reach (spec UnreachableCopy), said as data. */
export interface UnreachableLine {
  kind:
    | "git_history"
    | "agent_memory"
    | "backups"
    | "llm"
    | "hand_edits"
    | "copies";
  files: string[];
  repositories: string[];
  agents: string[];
  days: number | null;
  processors: {
    name: string;
    purpose: "embeddings" | "judge" | "ask";
    zeroRetention: boolean;
  }[];
  targets: string[];
}

/**
 * A forgotten memory's tombstone (Tombstone.png): who asked and when,
 * what went with it, each step and where it stands, what is gone, and
 * the copies Memax can't reach. Never words.
 */
export interface TombstoneView {
  ref: string;
  at: string;
  /** Who forgot it; null for Memax re-applying the forget ledger. */
  by: Actor | null;
  /** The agent whose request led to it, a registry key. */
  requestedBy: string | null;
  /** Where it was forgotten (spec Via): web, cli, mcp, api or system. */
  via: string;
  /** The person's own note on why (it stays). */
  note: string | null;
  keptAt: string | null;
  readsBefore: number;
  status: "propagating" | "done";
  /** The other memories forgotten in the same step. */
  with: string[];
  /** It went with another memory's Forget. */
  carried: { reason: CarryReason; primary: string } | null;
  gone: {
    versions: number;
    sources: number;
    embeddings: number;
    files: number;
  };
  /** How many agents are told. */
  agents: number;
  steps: TombstoneStepLine[];
  unreachable: UnreachableLine[];
}

/** A memory's newest version, after an edit clash. */
export interface LatestVersion {
  version: number;
  statement: string;
  by: Actor | null;
  at: string;
}

/** A space's export, ready to download (memax.export.v1). */
export interface SpaceExportFile {
  blob: Blob;
  /** `memax-<slug>-<yyyy-mm-dd>.zip`. */
  filename: string;
  /** The export's own `exported` receipt; empty in the demo. */
  receipt: string;
}

export interface MemoriesSource {
  /**
   * The space's whole record as a zip archive in the export format: one
   * `exported` receipt per idempotency key. A failure throws (see
   * command-error.ts).
   */
  exportSpace(input: {
    space: SpaceSummary;
    idempotencyKey: string;
    signal?: AbortSignal;
  }): Promise<SpaceExportFile>;
  /** The demo's first page, on hand for the first render. */
  peekList?(slug: string, filter: MemoryFilter): MemoryPage | undefined;
  peekRecord?(slug: string, ref: string): MemoryRecord | null | undefined;
  /** Newest first; grouped by section on screen. */
  list(input: {
    space: SpaceSummary;
    filter: MemoryFilter;
    cursor?: string;
    signal?: AbortSignal;
  }): Promise<MemoryPage>;
  /** Null when there's no memory with this ID the person can open. */
  get(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<MemoryRecord | null>;
  latest(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<LatestVersion | null>;
  /**
   * A new version of the statement, from `version` (If-Match). `keep`
   * also keeps a proposal (Review's edit, then keep). A clash throws
   * (see command-error.ts).
   */
  edit(input: {
    space: SpaceSummary;
    ref: string;
    version: number;
    statement: string;
    reason?: string;
    keep?: boolean;
    idempotencyKey: string;
  }): Promise<DecisionResult>;
  /**
   * Which repositories it applies in, from `version` (If-Match); `repos:
   * []` is everywhere (POST /v2/memories/{ref}:scope). The words don't
   * change. A failure throws (see command-error.ts).
   */
  scope(input: {
    space: SpaceSummary;
    ref: string;
    version: number;
    repos: string[];
    idempotencyKey: string;
  }): Promise<{ ref: string; version: number; repos: string[] }>;
  /** What a Forget would do. Changes nothing. */
  previewForget(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<ForgetPreview>;
  /**
   * Forget it everywhere, from `version` (If-Match), with what goes with
   * it named (`carries`). It can't be undone. A failure throws (see
   * command-error.ts; `carries` when what goes with it changed).
   */
  forget(input: {
    space: SpaceSummary;
    ref: string;
    version: number;
    carries: string[];
    note?: string;
    idempotencyKey: string;
  }): Promise<{ ref: string }>;
  /** Keep it instead: every waiting request to forget it is declined. */
  declineForget(input: {
    space: SpaceSummary;
    ref: string;
    idempotencyKey: string;
  }): Promise<void>;
  /** Why a memory is stale (VerifyStale); null when there's no such memory. */
  stale(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<StaleWhy | null>;
  /**
   * Answer a stale flag from `version` (If-Match): still true, new words
   * (`statement`), or forget it (with `carries` and `note`, as Forget).
   * A failure throws (see command-error.ts).
   */
  verify(input: {
    space: SpaceSummary;
    ref: string;
    version: number;
    answer: VerifyAnswer;
    statement?: string;
    carries?: string[];
    note?: string;
    idempotencyKey: string;
  }): Promise<VerifyResult>;
  /** The demo's report, on hand for the first render. */
  peekStale?(slug: string, ref: string): StaleWhy | null | undefined;
  /** A forgotten memory's tombstone; null when it isn't forgotten. */
  tombstone(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<TombstoneView | null>;
  /** The demo's tombstone, on hand for the first render. */
  peekTombstone?(slug: string, ref: string): TombstoneView | null | undefined;
}
