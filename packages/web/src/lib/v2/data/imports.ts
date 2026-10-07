/**
 * Imports (Phase 2 epic 2.1): what `npx memax-cli init` read from the
 * agents' files and uploaded as proposals, the disagreements its check
 * found, and keeping what agrees in bulk. FirstRun follows an import as
 * it lands, Cleanup settles its disagreements, ReviewImport keeps or
 * rejects the rest, and the routing after sign-in sends a person back to
 * an import still in progress. Part of LedgerDataSource (source.ts) as
 * `source.imports`; imports-sdk.ts (memax.v2.imports, memories:keep and
 * :reject) and imports-demo.ts implement it.
 *
 * Names follow the /v2 contract (openapi/v2.yaml › Import, ImportView,
 * ImportConflict, BulkReviewResult). No words live here.
 */
import type { SpaceSummary } from "./types";

/** Where a file lives: shared with everyone who clones it, or on the person's machine. */
export type ImportLocation = "repository" | "home";
/** Why init kept a statement on the machine. */
export type ImportSkipReason = "secret" | "too_long" | "limit";
/** How far the import's conflict check got. */
export type ImportCheckState =
  | "pending"
  | "checked"
  | "no_model"
  | "failed"
  | "skipped";
/** Why a proposal can't be kept in bulk (spec ImportHeld). */
export type ImportHeld =
  | "decided"
  | "conflict"
  | "quarantined"
  | "checking"
  | "hidden_characters"
  | "stale"
  | "unchecked";
/** How a disagreement is settled. */
export type ImportChoice =
  | "keep_one"
  | "keep_all"
  | "leave_open"
  | "keep_suggestion";

/** One file an import read. Never its contents. */
export interface ImportFileView {
  /** What people see: "CLAUDE.md", "~/.codex/memories/notes.md". */
  path: string;
  /** The CLI's name for its role: claude_md, agents_md, cursor_rule, codex_memory… */
  kind: string;
  /** The agent it belongs to, as a Ledger registry key ("claude-code", "gemini"); null when unknown. */
  agent: string | null;
  location: ImportLocation;
  statements: number;
  skipped: number;
  hiddenCharacters: number;
}

/** A statement init kept on the machine: a ref and a rule, never its words. */
export interface ImportSkipView {
  ref: string;
  reason: ImportSkipReason;
  /** The rule that matched ("GitHub token"); never the matched text. */
  detail: string | null;
}

export interface ImportCounts {
  items: number;
  proposed: number;
  folded: number;
  existing: number;
  refused: number;
  conflicts: number;
}

/** One upload, as the imports list has it. */
export interface ImportSummary {
  id: string;
  spaceId: string;
  /** The uploader, e.g. "memax-cli 2.0.0". */
  client: string | null;
  createdAt: string;
  files: ImportFileView[];
  skipped: ImportSkipView[];
  counts: ImportCounts;
  check: ImportCheckState;
}

/** A memory the import proposed or found, as ReviewImport lists it. */
export interface ImportMemoryView {
  id: string;
  /** Display ID ("M-0501"); commands address it with the space. */
  ref: string;
  /** The version the person sees (If-Match of a keep). */
  version: number;
  statement: string;
  /** Its displayed state now (spec State): proposed until someone decides. */
  state: string;
  /** proposed (this import proposed it) or existing (the space had it). */
  outcome: "proposed" | "existing";
  /** The import's statements it stands for, folded repeats included. */
  items: number;
  /** Where those statements are: "CLAUDE.md:4", in the import's order. */
  refs: string[];
  /** The files they came from (ImportFileView.path), each once. */
  files: string[];
  /** The agent whose file it came from first, as a registry key. */
  agent: string | null;
  /** It can be kept in bulk with the ones that agree. */
  bulk: boolean;
  held: ImportHeld | null;
  /** The disagreement it is in, by its number. */
  conflict: number | null;
  /** An outside source it cites (quarantined): the host it was read on. */
  external: string | null;
  /** The memory's trust, the least of its sources'. */
  trust: string | null;
}

/** One side of a disagreement. */
export interface ImportConflictMember {
  id: string;
  ref: string;
  statement: string;
  /** Where it was said ("CLAUDE.md:12"). */
  refs: string[];
  agent: string | null;
}

/** A disagreement among an import's proposals ("Test command, 3 files disagree"). */
export interface ImportConflictView {
  id: string;
  /** Its number within the import ("1 of 3"). */
  n: number;
  /** What they disagree about, in the check's words; null when it gave none. */
  subject: string | null;
  rationale: string | null;
  /** One statement that holds for all of them, when the check found one. */
  suggestion: string | null;
  members: ImportConflictMember[];
  state: "open" | "settled";
  choice: ImportChoice | null;
  /** The memory that holds (keep_one, keep_suggestion), by display ID. */
  chosen: string | null;
}

export interface ImportProgress {
  proposals: number;
  working: number;
  judged: number;
  failed: number;
  /** Nothing is being judged and the conflict check is done. */
  ready: boolean;
}

/** One import in full. */
export interface ImportView {
  summary: ImportSummary;
  /** Every memory the items became or matched, once each, in the import's order. */
  memories: ImportMemoryView[];
  conflicts: ImportConflictView[];
  progress: ImportProgress;
}

/** How a person settles a disagreement. */
export type SettleChoice =
  | { choice: "keep_one"; keep: string }
  | { choice: "keep_all" }
  | { choice: "leave_open" }
  | { choice: "keep_suggestion"; statement?: string };

/** What a bulk keep or reject did. */
export interface BulkOutcome {
  /** Display IDs kept (or rejected). */
  applied: string[];
  /** Ones policy held back, with its code ("in_conflict", "decision_needs_web"). */
  refused: Array<{ ref: string; code: string | null }>;
  /** Ones that failed (a newer version, the network). */
  failed: Array<{ ref: string; code: string | null }>;
}

export interface ImportsSource {
  /** The demo has every space's imports on hand, so screenshots never catch a loading frame. */
  peekList?(slug: string): ImportSummary[] | undefined;
  peekView?(slug: string, id: string): ImportView | undefined;
  /** The space's imports, newest first. */
  list(input: {
    space: SpaceSummary;
    limit?: number;
    signal?: AbortSignal;
  }): Promise<ImportSummary[]>;
  /** One import in full; null when the space has none by that id. */
  get(input: {
    space: SpaceSummary;
    id: string;
    signal?: AbortSignal;
  }): Promise<ImportView | null>;
  /**
   * Settles one disagreement as a group. Refusals throw CommandFailure-shaped
   * errors (command-error.ts): 403 `refused` (`decision_needs_web`,
   * `external_needs_review`, `viewer`), 409 `in_conflict` or
   * `invalid_transition` (already settled).
   */
  settle(input: {
    space: SpaceSummary;
    importId: string;
    n: number;
    choice: SettleChoice;
    idempotencyKey: string;
  }): Promise<ImportConflictView>;
  /** Keeps proposals in bulk, each by Keep's rules, at the version the person saw. */
  keep(input: {
    space: SpaceSummary;
    items: Array<{ ref: string; version: number }>;
    idempotencyKey: string;
  }): Promise<BulkOutcome>;
  /** Rejects proposals in bulk. */
  reject(input: {
    space: SpaceSummary;
    items: Array<{ ref: string; version: number }>;
    idempotencyKey: string;
  }): Promise<BulkOutcome>;
}

/** The file a statement ref names: "CLAUDE.md:12" → "CLAUDE.md". */
export function fileOfRef(ref: string): string {
  return ref.replace(/:\d+$/, "");
}

/**
 * The import's file a statement's file belongs to: the same path, or a
 * folder the import lists as one file ("~/.codex/memories" holding
 * "~/.codex/memories/notes.md").
 */
export function importFileOf(
  file: string,
  paths: readonly string[],
): string | null {
  return (
    paths.find((p) => p === file) ??
    paths.find((p) => file.startsWith(`${p}/`)) ??
    null
  );
}

/** Proposals waiting on a person: neither kept nor rejected yet. */
export function waitingMemories(view: ImportView): ImportMemoryView[] {
  return view.memories.filter(
    (m) => m.outcome === "proposed" && m.held !== "decided",
  );
}

/** The ones that can be kept in bulk now, most files agreeing first. */
export function bulkMemories(view: ImportView): ImportMemoryView[] {
  return view.memories
    .map((m, i) => ({ m, i }))
    .filter(({ m }) => m.bulk)
    .sort((a, b) => b.m.items - a.m.items || a.i - b.i)
    .map(({ m }) => m);
}

/** Disagreements still open. */
export function openConflicts(view: ImportView): ImportConflictView[] {
  return view.conflicts.filter((c) => c.state === "open");
}

/**
 * Whether an import still needs the person: a disagreement open, or a
 * proposal it brought still waiting in Review.
 */
export function importInProgress(view: ImportView): boolean {
  return openConflicts(view).length > 0 || waitingMemories(view).length > 0;
}

/** The groups ReviewImport filters by: one per kind of file, in the import's order. */
export interface SourceGroup {
  /** The file kind ("claude_md"), or "all". */
  key: string;
  /** The files in it. */
  files: string[];
  /** The memories it holds that wait on the person. */
  count: number;
}

export function sourceGroups(view: ImportView): SourceGroup[] {
  const byKind = new Map<string, string[]>();
  for (const f of view.summary.files) {
    const list = byKind.get(f.kind) ?? [];
    list.push(f.path);
    byKind.set(f.kind, list);
  }
  const waiting = waitingMemories(view);
  const groups: SourceGroup[] = [
    { key: "all", files: [], count: waiting.length },
  ];
  for (const [key, files] of byKind) {
    const count = waiting.filter((m) =>
      m.files.some((f) => importFileOf(f, files) !== null),
    ).length;
    if (count > 0) groups.push({ key, files, count });
  }
  return groups;
}

/** The import's files as people group them: the agent each belongs to, in order. */
export function agentsOfImport(summary: ImportSummary): string[] {
  const out: string[] = [];
  for (const f of summary.files) {
    if (f.agent && !out.includes(f.agent)) out.push(f.agent);
  }
  return out;
}

/** The secrets init kept on the machine. */
export function skippedSecrets(summary: ImportSummary): ImportSkipView[] {
  return summary.skipped.filter((s) => s.reason === "secret");
}
