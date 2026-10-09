/**
 * The adapter interface. One adapter per target kind: it knows the tool's
 * file layout, frontmatter and limits, and nothing else. Selection, budget
 * and rendering are shared (`select.ts`, `document.ts`).
 */
import type { Document, Limit } from "../document.js";
import type { Model, Target } from "../model.js";
import type { AdapterKind, ParsedFile } from "../types.js";

/**
 * - `canonical`: the full record (AGENTS.md).
 * - `shim`: imports the canonical file, plus lines only this tool reads.
 * - `scoped`: path-scoped facts only, one file per group of globs.
 * - `copy`: text for a person to copy out; never a file on disk.
 */
export type Role = "canonical" | "shim" | "scoped" | "copy";

export interface Cap {
  value: number;
  /** Who sets the cap, for messages: `Codex`, `Windsurf`. */
  tool: string;
}

export interface Limits {
  /** Hard cap on UTF-8 bytes per file. The budget never exceeds it. */
  bytes?: Cap;
  /** Hard cap on characters (UTF-16 code units, the stricter count) per file. */
  chars?: Cap;
  /** Lines the tool recommends staying under. Exceeding it only warns. */
  lines?: Cap;
}

export interface Output {
  /** Repository-relative path, or null for copy-out text. */
  path: string | null;
  /** UI name for copy-out text. */
  label?: string;
  content: string;
  /** What drift checks hash: the managed block, or the whole content. */
  driftText: string;
  userOwned: boolean;
  doc: Document;
}

export interface Adapter {
  kind: AdapterKind;
  /** Bumped whenever the adapter's output changes shape. */
  version: number;
  role: Role;
  /** The tool that reads the output, for messages. */
  tool: string;
  defaultPath: string | null;
  /** Says which paths are allowed, for error messages. */
  pathHint: string;
  acceptsPath(path: string): boolean;
  limits: Limits;
  /** The canonical file this tool also reads, for the UI ("Cursor · reads AGENTS.md"). */
  reads(target: Target): string | null;
  render(model: Model, target: Target, limit: Limit): Output[];
  /** Parses a file this adapter wrote. Copy-out targets have nothing to parse. */
  parse?(content: string): ParsedFile;
}

/** True when `dir` appears as whole segments in `path` (`.cursor/rules`, `pkg/.cursor/rules`). */
export function hasSegments(path: string, dir: string): boolean {
  return `/${path}/`.includes(`/${dir}/`);
}
