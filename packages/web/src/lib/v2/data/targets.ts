/**
 * The compile targets of a space (epics 1.5, 1.6): where the Brief
 * compiles to, each file as compiled, its settings, and the hand edits
 * that drifted it. Part of LedgerDataSource (source.ts) as
 * `source.targets`; targets-sdk.ts (memax.v2.targets) and
 * targets-demo.ts implement it.
 *
 * Names follow the /v2 contract (openapi/v2.yaml › Target, TargetPreview,
 * Drift). D2/D3 shape what people see: AGENTS.md is canonical, the
 * CLAUDE.md shim imports it, Cursor writes a rule only for path-scoped
 * facts (otherwise "reads AGENTS.md"), and ChatGPT is copy-out text read
 * live over its connector, never "in sync". No words live here.
 */
import type { SpaceSummary, SyncLine } from "./types";

export type TargetKind =
  | "agents_md"
  | "claude_md"
  | "cursor_mdc"
  | "chatgpt"
  | "gemini_md"
  | "copilot"
  | "windsurf"
  | "claude_rules";

/** in_sync, compiling, pending_delivery (not on disk yet), drifted (a hand edit) or off. */
export type SyncState =
  | "in_sync"
  | "compiling"
  | "pending_delivery"
  | "drifted"
  | "off";

/** local (the CLI writes it), pr (a pull request), mcp, or copy (ChatGPT). */
export type Delivery = "local" | "pr" | "mcp" | "copy";
export type IncludeMode = "kept_only" | "kept_and_open";
export type StaleMode = "mark" | "omit";

export interface TargetSettingsView {
  include: IncludeMode;
  stale: StaleMode;
  /** Bytes per file, 1 KiB to 32 KiB. */
  sizeBudget: number;
}

export const SIZE_BUDGET_MIN = 1024;
export const SIZE_BUDGET_MAX = 32768;

/** One compile run (C-), as the screens show it. */
export interface CompileSummary {
  ref: string;
  status: "compiled" | "delivered" | "failed";
  /** When it compiled (or was queued, before it ran). */
  at: string;
  bytes: number;
  lines: number;
  /** Memories whose statements it contains. */
  refs: string[];
  /** Every memory it cites, prose included. */
  cites: string[];
  /** Memories that didn't fit the budget; they stay live over MCP. */
  dropped: string[];
  /** The files it writes (none for a scoped tool with no scoped facts, or copy-out). */
  files: string[];
}

export interface TargetView {
  id: string;
  /** The URL segment under /[space]/brief/targets/. */
  slug: string;
  kind: TargetKind;
  /** What people see: "AGENTS.md", ".cursor/rules", "ChatGPT project". */
  label: string;
  /** A file, or a rules directory for scoped kinds; null for ChatGPT. */
  path: string | null;
  /** The canonical file this tool also reads ("AGENTS.md" for the shim and Cursor). */
  reads: string | null;
  syncState: SyncState;
  delivery: Delivery;
  settings: TargetSettingsView;
  /** The If-Match of a settings change. */
  version: number;
  /** Files with a hand edit waiting on a person. */
  openDrift: number;
  /** The latest run, whatever its status; null before the first compile. */
  lastCompile: CompileSummary | null;
}

/** What a person reads about a target's state (target-status.tsx words it). */
export type TargetStatus =
  | { kind: "in_sync" }
  | { kind: "compiling" }
  /** Compiled, but the CLI hasn't written it to disk yet. */
  | { kind: "pending" }
  | { kind: "drifted"; edits: number }
  | { kind: "off" }
  /** ChatGPT: read live over its connector, copied out by a person (D3). */
  | { kind: "live" }
  /** A scoped tool with nothing scoped to write reads the canonical file (D2). */
  | { kind: "reads"; file: string };

export function targetStatus(target: TargetView): TargetStatus {
  if (target.syncState === "off") return { kind: "off" };
  if (target.syncState === "compiling") return { kind: "compiling" };
  if (target.syncState === "drifted") {
    return { kind: "drifted", edits: Math.max(1, target.openDrift) };
  }
  if (target.delivery === "copy" || target.kind === "chatgpt") {
    return { kind: "live" };
  }
  if (
    isScoped(target.kind) &&
    target.lastCompile !== null &&
    target.lastCompile.files.length === 0
  ) {
    return { kind: "reads", file: target.reads ?? "AGENTS.md" };
  }
  if (target.syncState === "pending_delivery") return { kind: "pending" };
  return { kind: "in_sync" };
}

/** Kinds that hold path-scoped facts only. */
export function isScoped(kind: TargetKind): boolean {
  return (
    kind === "cursor_mdc" ||
    kind === "copilot" ||
    kind === "windsurf" ||
    kind === "claude_rules"
  );
}

/** Kinds that import the canonical file. */
export function isShim(kind: TargetKind): boolean {
  return kind === "claude_md" || kind === "gemini_md";
}

/** What a compact row names a target by: its one file, or its label. */
export function targetName(target: TargetView): string {
  const files = target.lastCompile?.files ?? [];
  if (isScoped(target.kind) && files.length === 1) return files[0]!;
  return target.label;
}

/** The registry keys of the agents that read a kind's file natively. */
export const TARGET_READERS: Record<TargetKind, readonly string[]> = {
  agents_md: ["codex", "cursor", "opencode"],
  claude_md: ["claude-code"],
  cursor_mdc: ["cursor"],
  chatgpt: ["chatgpt"],
  gemini_md: ["gemini"],
  copilot: ["copilot"],
  windsurf: ["windsurf"],
  claude_rules: ["claude-code"],
};

/** The agent a drifted file belongs to, for "Cursor file drifted"; none for AGENTS.md. */
export function driftAgent(kind: TargetKind): string | undefined {
  return kind === "agents_md" ? undefined : TARGET_READERS[kind][0];
}

/** The URL segment for a target: its kind ("claude-md") when that's unique in the space, else its id. */
export function targetSlug(
  target: { id: string; kind: TargetKind },
  all: readonly { kind: TargetKind }[],
): string {
  const same = all.filter((t) => t.kind === target.kind).length;
  return same <= 1 ? target.kind.replace(/_/g, "-") : target.id;
}

/** Finds a target by the URL segment: its slug or its id. */
export function findTarget(
  targets: readonly TargetView[],
  segment: string,
): TargetView | undefined {
  return targets.find((t) => t.slug === segment || t.id === segment);
}

/**
 * The rail's status line from the targets (plan §6.4): a drifted file
 * first ("Cursor file drifted"), then anything compiling or waiting for
 * the CLI, then "N files in sync". ChatGPT and stopped targets don't
 * count: nothing is on disk to be in sync. Null with no targets.
 */
export function syncLineOf(targets: readonly TargetView[]): SyncLine | null {
  const active = targets.filter((t) => t.syncState !== "off");
  if (active.length === 0) return null;
  const drifted = active.find((t) => t.syncState === "drifted");
  if (drifted) {
    const agent = driftAgent(drifted.kind);
    return agent
      ? { kind: "drifted", agent }
      : { kind: "drifted", file: targetName(drifted) };
  }
  if (active.some((t) => t.syncState === "compiling")) {
    return { kind: "compiling" };
  }
  const statuses = active.map(targetStatus);
  const pending = statuses.filter((s) => s.kind === "pending").length;
  if (pending > 0) return { kind: "waiting-delivery", files: pending };
  const files = statuses.filter((s) => s.kind === "in_sync").length;
  return files > 0 ? { kind: "files-in-sync", files } : null;
}

/**
 * Whether a memory reaches a target: its words are in the file, or the
 * tool imports or also reads a canonical file that holds them.
 */
export function reachesTarget(
  ref: string,
  target: TargetView,
  all: readonly TargetView[],
): boolean {
  const has = (t: TargetView) =>
    Boolean(
      t.lastCompile?.refs.includes(ref) || t.lastCompile?.cites.includes(ref),
    );
  if (has(target)) return true;
  if (!target.reads) return false;
  const canonical = all.find(
    (t) => t.kind === "agents_md" && (t.path ?? t.label) === target.reads,
  );
  return canonical ? has(canonical) : false;
}

// The compiled content (TargetPreview).

/** One compiled file, or the copy-out text. */
export interface CompiledFileView {
  /** The file's path; null for copy-out text. */
  path: string | null;
  /** What people see: the path, or "ChatGPT project instructions". */
  label: string;
  content: string;
  bytes: number;
  lines: number;
  refs: string[];
  cites: string[];
  dropped: string[];
}

export interface TargetPreviewView {
  target: TargetView;
  /** The latest good run, whose content this is; null before the first compile. */
  compile: { ref: string; at: string } | null;
  /** The canonical file this tool also reads. */
  reads: string | null;
  files: CompiledFileView[];
  /** ChatGPT's project instructions. */
  copies: CompiledFileView[];
}

// Hand edits (DriftResolve).

/** One change parse-back read from a hand edit (spec DriftChange). */
export type DriftChangeView =
  /** A cited line's words changed: an edit proposal for that memory. */
  | {
      kind: "edit";
      ref: string;
      oldText: string;
      newText: string;
      oldLine: number;
      newLine: number;
    }
  /** A line with no matching cite: a new proposal. */
  | { kind: "new"; text: string; line: number; section: string | null }
  /** A cited line is gone: a proposal to forget or exclude, never an automatic forget. */
  | { kind: "remove"; ref: string; oldText: string; oldLine: number };

export interface DriftItemView {
  observationId: string;
  path: string;
  /** When the device (or GitHub) saw it. */
  observedAt: string;
  commit: string | null;
  /** The file as Memax last wrote it, and when that compile ran. */
  compiled: string;
  compiledAt: string | null;
  /** The file as it is now. */
  observed: string;
  changes: DriftChangeView[];
  /** Hidden characters in the edit; they never reach a proposal. */
  hiddenCharacters: number;
}

export interface DriftView {
  target: TargetView;
  /** Open hand edits, one per file; empty when there are none. */
  items: DriftItemView[];
}

export type DriftMode = "pull" | "overwrite" | "stop";

export interface DriftResolution {
  target: TargetView;
  /** The proposals a pull wrote, in file order. */
  proposals: { ref: string; statement: string }[];
  /** Removed lines waiting on a person to forget or exclude their memory. */
  waiting: number;
  /** Changes nothing was written for (too long, empty, refused). */
  skipped: number;
}

/** A settings change: what's left out stays (spec ConfigureTargetRequest). */
export interface TargetChange {
  settings?: Partial<TargetSettingsView>;
  delivery?: Delivery;
  enabled?: boolean;
}

export interface TargetCommand {
  space: SpaceSummary;
  target: TargetView;
  /** One per intent; the same key on a retry (spec: Idempotency-Key). */
  idempotencyKey: string;
}

export interface TargetsSource {
  /** The demo's data, on hand for the first render. */
  peekList?(slug: string): TargetView[] | undefined;
  peekPreview?(slug: string, id: string): TargetPreviewView | undefined;
  peekDrift?(slug: string, id: string): DriftView | undefined;
  /** Where the space compiles to, in the order the API lists them. */
  list(input: {
    space: SpaceSummary;
    signal?: AbortSignal;
  }): Promise<TargetView[]>;
  preview(input: {
    space: SpaceSummary;
    target: TargetView;
    signal?: AbortSignal;
  }): Promise<TargetPreviewView>;
  drift(input: {
    space: SpaceSummary;
    target: TargetView;
    signal?: AbortSignal;
  }): Promise<DriftView>;
  /** Compile now: queued, so the target comes back compiling. */
  compile(input: TargetCommand): Promise<TargetView>;
  /** PATCH with If-Match on the target's version. A clash throws (command-error.ts). */
  configure(
    input: TargetCommand & { change: TargetChange },
  ): Promise<TargetView>;
  resolveDrift(
    input: TargetCommand & { mode: DriftMode },
  ): Promise<DriftResolution>;
}
