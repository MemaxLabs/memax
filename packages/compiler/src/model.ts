/**
 * The validated, normalised form of a {@link CompileInput}. Adapters only
 * ever see this: text is clean, sets are sorted, defaults are applied.
 */
import type {
  AdapterKind,
  CompileWarning,
  IncludeMode,
  MemoryState,
  ScopedMode,
  SpaceKind,
  StaleMode,
} from "./types.js";

/** Headings for well-known sections, used when the Brief doesn't have them yet. */
export const DEFAULT_HEADINGS: Record<string, string> = {
  overview: "What this is",
  decisions: "Decisions",
  conventions: "Conventions",
  open: "Open",
  preferences: "Preferences",
};

export interface Fact {
  ref: string;
  /** Clean, single-line statement. */
  text: string;
  section: string;
  state: MemoryState;
  /** Flagged stale, or past `stale_after` at compile time. */
  stale: boolean;
  conflict: boolean;
  /** Sorted, unique globs. */
  paths: string[];
  /** Sorted, unique agent IDs. */
  agents: string[];
  score: number;
}

export type ModelItem =
  | { kind: "memory"; ref: string }
  | { kind: "prose"; text: string; cites: string[] };

export interface ModelSection {
  key: string;
  heading: string;
  items: ModelItem[];
}

export interface Target {
  kind: AdapterKind;
  path: string;
  include: IncludeMode;
  stale: StaleMode;
  /** Requested budget in bytes, or null for the adapter's default. */
  budget: number | null;
  /** Section keys to compile, or null for the default (all but `overview`). */
  sections: string[] | null;
  scoped: ScopedMode;
  canonical: string;
  userOwned: boolean;
  current: string;
}

export interface Model {
  compileId: string;
  compiledAt: string;
  /** `2026-10-05 14:31 UTC`, for headers. */
  stamp: string;
  /** Milliseconds since the epoch, for `stale_after`. */
  time: number;
  space: { slug: string; name: string; kind: SpaceKind; url: string };
  brief: {
    id: string;
    title: string;
    summary: string | null;
    sections: ModelSection[];
  };
  facts: Map<string, Fact>;
  targets: Target[];
  warnings: CompileWarning[];
}
