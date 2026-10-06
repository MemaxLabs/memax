/**
 * The compiler's contract: what goes in, what comes out.
 *
 * Everything here is plain, JSON-serialisable data, so the same input can
 * be compiled by the CLI on a laptop, by the stateless compile service, or
 * in a browser preview, and produce the same bytes.
 */

/** The contract version. Bump it when an input or output shape changes. */
export const CONTRACT_VERSION = 1;

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------

export interface CompileInput {
  /** Contract version; must equal {@link CONTRACT_VERSION}. */
  version: 1;
  compile: CompileRun;
  space: Space;
  brief: Brief;
  /**
   * Everything that may compile: kept memories and open questions. Never
   * proposals, rejected, faded or forgotten memories, and never anything
   * quarantined. The compiler refuses an input that carries any of them.
   * Order doesn't matter.
   */
  memories: Memory[];
  /** One or more targets to render. Order doesn't matter. */
  targets: TargetSettings[];
}

export interface CompileRun {
  /** Display ID of this compile run, for example `C-0881`. */
  id: string;
  /** When the run happened, as ISO 8601 with a zone (`2026-10-05T14:31:00Z`). */
  at: string;
}

export type SpaceKind = "personal" | "project" | "team";

export interface Space {
  /** URL-safe name, for example `memax-v2`. Named in every file's header. */
  slug: string;
  /** Display name, for example `Memax V2`. */
  name: string;
  kind: SpaceKind;
  /** Where a person edits this space's Brief; printed in every header. */
  url: string;
}

/** Well-known section keys. Briefs may add their own keys. */
export type WellKnownSection =
  | "overview"
  | "decisions"
  | "conventions"
  | "open"
  | "preferences";

export interface Brief {
  /** Display ID of the Brief version, for example `B-0043`. */
  id: string;
  /** Rendered as the file's `# title`. */
  title: string;
  /** One optional line under the title. */
  summary?: string;
  /** Sections in the order they render. */
  sections: BriefSection[];
}

export interface BriefSection {
  /** `decisions`, `conventions`, `open`, `preferences`, `overview`, or a custom key. */
  key: string;
  /** Rendered as `## heading`. */
  heading: string;
  /** Items in the order they render. */
  items: BriefItem[];
}

/**
 * A Brief item: a reference to a memory, or a short line of connective prose
 * that cites the memories it rests on. A reference to a memory that isn't in
 * `memories` (a proposal, say) is skipped.
 */
export type BriefItem = MemoryItem | ProseItem;

export interface MemoryItem {
  ref: string;
}

export interface ProseItem {
  text: string;
  /** At least one memory ref, for example `["M-0431", "M-0174"]`. */
  cites: string[];
}

/** Memory kinds, as in the record. */
export type MemoryKind = "fact" | "decision";

/** What may compile: kept memories, and open questions. */
export type MemoryState = "kept" | "open";

/** Conditions on a kept memory. */
export type MemoryFlag = "stale" | "conflict";

/** Trust classes of a memory's sources. `external` is quarantined and refused. */
export type TrustClass = "person" | "agent_own_work" | "repository";

export interface Memory {
  /** Display ID, for example `M-0219`. */
  ref: string;
  /** The memory's words: one line of plain text. */
  statement: string;
  /**
   * The section the memory belongs to. Used when the Brief doesn't place the
   * memory yet (it was kept after the last Brief version): it then renders at
   * the end of this section.
   */
  section: string;
  kind: MemoryKind;
  state: MemoryState;
  flags?: MemoryFlag[];
  /** The minimum trust over the memory's sources. */
  trust?: TrustClass;
  scope?: MemoryScope;
  /** Reads over the last 30 days, with decay. Higher ranks first. Default 0. */
  read_score?: number;
  /** After this time the memory counts as stale (ISO 8601 with a zone). */
  stale_after?: string;
}

export interface MemoryScope {
  /** Repository-relative globs, for example `packages/web/**`. */
  paths?: string[];
  /**
   * Limits a memory to the files only these agents read, for example
   * `["claude-code"]`. Such memories never go into the shared AGENTS.md.
   */
  agents?: string[];
}

export type AdapterKind =
  | "agents_md"
  | "claude_md"
  | "cursor_mdc"
  | "chatgpt"
  | "gemini_md"
  | "copilot"
  | "windsurf"
  | "claude_rules";

export type IncludeMode = "kept_only" | "kept_and_open";
export type StaleMode = "mark" | "omit";
export type ScopedMode = "inline" | "omit";

export interface TargetSettings {
  kind: AdapterKind;
  /**
   * Where the target writes, relative to the repository root. A file for
   * `agents_md` and the shims, a directory for the scoped adapters; ignored
   * by `chatgpt`. Each adapter has a default.
   */
  path?: string;
  /** Whether open questions compile. Default `kept_and_open`. */
  include?: IncludeMode;
  /** Mark stale memories `(Being verified)`, or leave them out. Default `mark`. */
  stale?: StaleMode;
  /** Size budget per file, in bytes. Capped by the tool's own limit. */
  size_budget?: number;
  /** Section keys to compile. Default: every section except `overview`. */
  sections?: string[];
  /**
   * `agents_md` and `chatgpt`: path-scoped memories render in their own
   * subsections (`inline`, the default) or are left to the scoped targets
   * (`omit`).
   */
  scoped?: ScopedMode;
  /** Shims and scoped adapters: the canonical file. Default `AGENTS.md`. */
  canonical_path?: string;
  /**
   * Shims only: the person owns this file. Memax then manages one marked
   * block inside `current` and leaves every other byte alone.
   */
  user_owned?: boolean;
  /** Shims with `user_owned`: the file's content right now (`""` if absent). */
  current?: string;
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

export interface CompileResult {
  version: 1;
  compile_id: string;
  compiled_at: string;
  brief_id: string;
  space: string;
  /** Every file to write, sorted by path. */
  files: CompiledFile[];
  /** Text for a person to copy out (ChatGPT). Never written to disk. */
  copies: CompiledCopy[];
  /** One summary per input target, in a stable order. */
  targets: TargetResult[];
  warnings: CompileWarning[];
}

export interface CompiledText {
  target: AdapterKind;
  content: string;
  /** UTF-8 bytes. */
  bytes: number;
  lines: number;
  /** Lowercase hex sha256 of the UTF-8 content. */
  sha256: string;
  /** Memories whose statements are in the content, sorted. */
  refs: string[];
  /** Every memory ref cited anywhere in the content, sorted. */
  cites: string[];
  /** Memories that belonged here but didn't fit the budget, sorted. */
  dropped_for_budget: string[];
}

export interface CompiledFile extends CompiledText {
  /** Repository-relative path. */
  path: string;
  /**
   * What drift checks compare against: the managed block's hash for a file
   * the person owns, otherwise the same as `sha256`. Store this as the last
   * delivered hash.
   */
  drift_sha256: string;
  /** True when Memax manages one block inside a file the person owns. */
  user_owned: boolean;
}

export interface CompiledCopy extends CompiledText {
  /** What to call it in the UI, for example `ChatGPT project instructions`. */
  label: string;
}

export interface TargetResult {
  kind: AdapterKind;
  /** The configured or default path; `null` for copy-out targets. */
  path: string | null;
  /** `file`: written to disk. `copy`: shown for a person to copy out. */
  delivery: "file" | "copy";
  /**
   * The canonical file this tool also reads for everything not in its own
   * files, for example `AGENTS.md` for Cursor. `null` for the canonical file.
   */
  reads: string | null;
  /** Paths written by this target (empty when, say, nothing is path-scoped). */
  files: string[];
  refs: string[];
  dropped_for_budget: string[];
}

export type WarningCode =
  | "near_limit"
  | "over_guidance"
  | "dropped_for_budget"
  | "prose_dropped_for_budget"
  | "budget_capped"
  | "hidden_characters";

export interface CompileWarning {
  code: WarningCode;
  message: string;
  target?: AdapterKind;
  path?: string;
  ref?: string;
}

// ---------------------------------------------------------------------------
// Parse-back
// ---------------------------------------------------------------------------

export type LineKind =
  | "frontmatter"
  | "header"
  | "marker"
  | "comment"
  | "heading"
  | "import"
  | "fence"
  | "item"
  | "text"
  | "blank";

export interface ParsedLine {
  /** 1-based line number in the file. Continuation lines fold into the item. */
  line: number;
  kind: LineKind;
  /** Normalised text: for items, the statement without marker or cites. */
  text: string;
  /** `[M-…]` cites at the end of an item or text line. */
  refs: string[];
  /** For items: `Being verified` or `In conflict`, when present. */
  marker: string | null;
  /** The nearest `##` heading above the line. */
  section: string | null;
  /** Globs that apply here, from the frontmatter or an `### In …` heading. */
  paths: string[];
}

export interface ParsedFile {
  lines: ParsedLine[];
  /** The Memax header comment, if present. */
  header: string | null;
  /** Frontmatter lines without the `---` fences, if present. */
  frontmatter: string[] | null;
  /** The managed block's inner line range (1-based, inclusive), if present. */
  managed: { start: number; end: number } | null;
  /** Hidden characters found (and removed from `lines`): a reason to look closer. */
  hidden_characters: number;
}

export type Change = EditChange | NewChange | RemoveChange;

/** A cited line whose words changed: propose an edit to that memory. */
export interface EditChange {
  kind: "edit";
  /** The first cite on the line: the memory a single-cite line belongs to. */
  ref: string;
  /** Every cite on the line (connective prose may cite several). */
  refs: string[];
  old_text: string;
  new_text: string;
  old_line: number;
  new_line: number;
}

/** A line with no matching cite in the last compile: propose a new memory. */
export interface NewChange {
  kind: "new";
  text: string;
  line: number;
  /** Heading of the section it was added under, if any. */
  section: string | null;
  /** Globs that apply where it was added (scoped files and subsections). */
  paths: string[];
  /** Cites the person typed, if any (they match nothing compiled). */
  cites: string[];
}

/** A cited line that's gone: propose forgetting or excluding. Never automatic. */
export interface RemoveChange {
  kind: "remove";
  ref: string;
  refs: string[];
  old_text: string;
  old_line: number;
}

export interface ChangeSet {
  /** Proposals, in file order: edits and new lines, then removals. */
  changes: Change[];
  drift: DriftInfo;
}

export interface DriftInfo {
  /** The content differs beyond line endings and trailing whitespace. */
  changed: boolean;
  /** The Memax header line was changed or removed. */
  header_edited: boolean;
  /** Frontmatter (globs, applyTo, paths) was changed or removed. */
  frontmatter_edited: boolean;
  /** Compiler lines (headings, imports, Live context) changed. */
  layout_edited: boolean;
  /** For files the person owns: the state of the managed block. */
  managed_block: "intact" | "edited" | "removed" | null;
  /** Hidden characters in the current file's compared region; they never reach a proposal. */
  hidden_characters: number;
}
