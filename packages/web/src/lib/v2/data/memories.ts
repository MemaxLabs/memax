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
  /** "Its source changed on Sep 11 · PR #198" */
  | { kind: "stale"; changedAt: string; source: string | null }
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
}

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

/** "Stays true while": `code` in mono, then the rest. */
export interface ConditionLine {
  key: string;
  code: string | null;
  text: string;
}

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
  /** Reads by agents, when counted (not served yet). */
  reads: number | null;
  /** How far it reaches, when compile targets are served. */
  reach: { files: number; agents: number } | null;
  lineage: LineageEntry[];
  /** What was merged into it: Dream's notes, the judge's folds. Null when the source can't say. */
  merged: { notes: MergedNote[]; total: number } | null;
  sources: SourceLine[];
  /** Null until compile targets are served (PLACEHOLDER in the SDK). */
  reaches: TargetLine[] | null;
  conditions: ConditionLine[];
  /** The last time someone checked it against its conditions. */
  checked: { at: string; by: Actor | null } | null;
  forgotten: Forgotten | null;
}

/** A memory's newest version, after an edit clash. */
export interface LatestVersion {
  version: number;
  statement: string;
  by: Actor | null;
  at: string;
}

export interface MemoriesSource {
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
}
