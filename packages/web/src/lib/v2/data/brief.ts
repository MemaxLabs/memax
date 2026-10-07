/**
 * The Brief (epic 1.5, Brief.png, BriefEdit.png, BriefHistory.png): the
 * living document a space compiles into every agent's files. Part of
 * LedgerDataSource (source.ts) as `source.brief`; brief-sdk.ts
 * (memax.v2.briefs) and brief-demo.ts implement it.
 *
 * A version (B-) is ordered sections of items: a kept memory (`ref`), or
 * a line of connective prose that cites what it rests on. What the page
 * shows adds two things the compiler also uses: kept memories the Brief
 * doesn't place yet (they compile at the end of their section), and the
 * proposals waiting in each section (italic, until a person keeps them).
 * No words live here.
 */
import type { Actor, RecordAction } from "./records";
import type { Section, SpaceSummary } from "./types";

/** A Brief item as stored (spec BriefItem). */
export type BriefItem = { ref: string } | { text: string; cites: string[] };

export interface BriefSectionStructure {
  /** The compiler's key: decisions, conventions, preferences, open, overview, or your own. */
  key: string;
  heading: string;
  items: BriefItem[];
}

/** A version's content, as revise takes it and history restores it. */
export interface BriefStructure {
  title: string;
  summary: string | null;
  sections: BriefSectionStructure[];
}

/** How a row is set: kept roman, waiting italic, stale dotted. */
export type BriefRowState = "kept" | "stale" | "conflict" | "proposed";

export interface BriefRowReceipt {
  by: Actor | null;
  /** A receipt's verb; "wrote" for the person who wrote a line of prose. */
  action: RecordAction | "wrote";
  at: string;
}

export interface BriefRow {
  /** Stable within the version: the ref, or the prose's position. */
  key: string;
  /** A placed or unplaced kept memory, a line of prose, or a proposal waiting in Review. */
  kind: "memory" | "prose" | "waiting";
  /** The memory's display ID; null for prose. */
  ref: string | null;
  text: string;
  /** Prose: the memories it rests on, in order. */
  cites: string[];
  state: BriefRowState;
  /** False for a kept memory the Brief doesn't place yet; it compiles at the end of its section. */
  placed: boolean;
  /** The memory's version, the If-Match of an edit; null for prose. */
  version: number | null;
  receipt: BriefRowReceipt | null;
  /** The rail's second line after the ID ("PR #212", "session 3e1a"). */
  source: string | null;
  /** The paths a scoped memory applies to ("packages/web/**"). */
  scope: string[];
  /** The words the latest version changed, highlighted; null when unknown. */
  changed: string | null;
}

export interface BriefSectionView {
  key: string;
  heading: string;
  rows: BriefRow[];
}

export interface BriefMemory {
  text: string;
  state: BriefRowState | "merged" | "faded" | "forgotten";
  section: Section;
  /** Its version, the If-Match of an edit. */
  version: number;
  /** Kept memories compile; proposals wait in Review. */
  kept: boolean;
}

export interface BriefView {
  id: string;
  /** "B-0043". */
  ref: string;
  /** The If-Match of a revision. */
  version: number;
  title: string;
  summary: string | null;
  sections: BriefSectionView[];
  /** Who wrote this version, when and why. */
  by: Actor | null;
  at: string;
  reason: string | null;
  /** The version as stored, for the editor and restore. */
  structure: BriefStructure;
  /** Statements of the memories the source read, for history and the cite picker. */
  memories: Record<string, BriefMemory>;
  /** Short titles for the Sources panel, where the source has them (the demo). */
  titles: Record<string, string>;
  /**
   * PLACEHOLDER: "Read this week" by agent. Reads are recorded (R-), but
   * /v2 serves no count of the Brief's (its compiles') reads by agent, so
   * the SDK source leaves this null; only the demo has it.
   */
  reads: { total: number; agents: { agent: string; reads: number }[] } | null;
}

/**
 * PLACEHOLDER: what a Dream edition changed in memories (a rewording,
 * a stale flag), which a Brief version's structure doesn't record. Only
 * the demo has them, until Dream editions are recorded (plan §5.10).
 */
export type EditionChange =
  | {
      kind: "reworded";
      ref: string;
      section: string;
      before: string;
      after: string;
      note: string | null;
    }
  | { kind: "flagged"; ref: string; section: string; note: string | null };

export interface BriefVersionView {
  ref: string;
  version: number;
  /** The version it was revised from; null for the first. */
  parent: number | null;
  current: boolean;
  by: Actor | null;
  at: string;
  reason: string | null;
  facts: number;
  structure: BriefStructure;
  edition?: EditionChange[];
  /** PLACEHOLDER (demo only): a line under a change, by memory ref. */
  notes?: Record<string, string>;
}

export interface BriefVersionsPage {
  /** Newest first. */
  items: BriefVersionView[];
  nextCursor: string | null;
}

/** Something a restore left out of the version it restored (spec BriefDrop), by ID only. */
export interface BriefDrop {
  /** The section's key. */
  section: string;
  /** The line in the restored version: a ref, or "P:<section>:<index>" for prose. */
  item: string;
  /** A memory line, a line of prose, or a citation taken off a line that stays. */
  kind: "memory" | "prose" | "cite";
  /** The memories that made it go. */
  refs: string[];
  reason: "forgotten" | "not_kept";
}

/** What a memory is now, as a restore reads it; null when it isn't in the space. */
export type RestoreState =
  | "kept"
  | "proposed"
  | "merged"
  | "faded"
  | "rejected"
  | "forgotten"
  | null;

/**
 * An older version as it can stand now, by the server's rules
 * (ledger.RestoreBriefSections): a memory line stays while its memory is
 * kept; a line of prose goes when its words were forgotten, when it cites
 * a forgotten memory, or when it cites no kept one, and otherwise loses
 * only the citations that can't be made any more. Every section stays.
 * The demo restores with it; the SDK source asks the server.
 */
export function restoreStructure(
  structure: BriefStructure,
  stateOf: (ref: string) => RestoreState,
): { structure: BriefStructure; dropped: BriefDrop[] } {
  const dropped: BriefDrop[] = [];
  const sections = structure.sections.map((section) => {
    const items: BriefItem[] = [];
    section.items.forEach((item, index) => {
      if ("ref" in item) {
        const state = stateOf(item.ref);
        if (state === "kept") items.push({ ref: item.ref });
        else
          dropped.push({
            section: section.key,
            item: item.ref,
            kind: "memory",
            refs: [item.ref],
            reason: state === "forgotten" ? "forgotten" : "not_kept",
          });
        return;
      }
      const id = `P:${section.key}:${index}`;
      const forgotten = item.cites.filter(
        (ref) => stateOf(ref) === "forgotten",
      );
      const gone = item.cites.filter((ref) => {
        const state = stateOf(ref);
        return state === null || state === "rejected";
      });
      const cites = item.cites.filter(
        (ref) => !forgotten.includes(ref) && !gone.includes(ref),
      );
      if (forgotten.length > 0) {
        dropped.push({
          section: section.key,
          item: id,
          kind: "prose",
          refs: forgotten,
          reason: "forgotten",
        });
      } else if (!cites.some((ref) => stateOf(ref) === "kept")) {
        dropped.push({
          section: section.key,
          item: id,
          kind: "prose",
          refs: [...item.cites],
          reason: "not_kept",
        });
      } else {
        items.push({ text: item.text, cites });
        for (const ref of gone) {
          dropped.push({
            section: section.key,
            item: id,
            kind: "cite",
            refs: [ref],
            reason: "not_kept",
          });
        }
      }
    });
    return { ...section, items };
  });
  return { structure: { ...structure, sections }, dropped };
}

/** How many lines a restore left out (a citation taken off a line isn't one). */
export function droppedLines(dropped: readonly BriefDrop[]): number {
  return dropped.filter((d) => d.kind !== "cite").length;
}

export interface BriefSource {
  /** The demo's Brief, on hand for the first render; null when the space has none. */
  peek?(slug: string): BriefView | null | undefined;
  peekVersions?(slug: string): BriefVersionsPage | undefined;
  /** The current version as the page shows it; null when the space has no Brief yet. */
  get(input: {
    space: SpaceSummary;
    signal?: AbortSignal;
  }): Promise<BriefView | null>;
  versions(input: {
    space: SpaceSummary;
    cursor?: string;
    signal?: AbortSignal;
  }): Promise<BriefVersionsPage>;
  /**
   * Writes a new version. `base` is the version it started from (null
   * for the space's first Brief); a newer one throws a clash. Items must
   * be kept memories of the space, and prose must cite one.
   */
  revise(input: {
    space: SpaceSummary;
    base: number | null;
    structure: BriefStructure;
    reason?: string;
    idempotencyKey: string;
  }): Promise<{ ref: string; version: number }>;
  /**
   * Writes an older version (`version`) back as a new one. `base` is the
   * version in force; a newer one throws a clash. What can't stand any
   * more is left out (restoreStructure), and `dropped` says which.
   */
  restore(input: {
    space: SpaceSummary;
    base: number;
    version: number;
    reason?: string;
    idempotencyKey: string;
  }): Promise<{ ref: string; version: number; dropped: BriefDrop[] }>;
}

/** Where a memory of this section sits in the Brief, by the compiler's keys. */
export function sectionKeyOf(section: Section): string {
  return section === "open_question" ? "open" : section;
}

/** The memory section a new fact under this Brief section is kept in. */
export function memorySectionOf(key: string): Section {
  switch (key) {
    case "decisions":
      return "decisions";
    case "preferences":
      return "preferences";
    case "open":
      return "open_question";
    default:
      return "conventions";
  }
}

/** Every memory a version places or cites, in order, once each. */
export function briefRefs(structure: BriefStructure): string[] {
  const refs: string[] = [];
  for (const section of structure.sections) {
    for (const item of section.items) {
      for (const ref of "ref" in item ? [item.ref] : item.cites) {
        if (!refs.includes(ref)) refs.push(ref);
      }
    }
  }
  return refs;
}

/**
 * The numbered sources of the page, in reading order (Brief.png's
 * Sources panel and the [n] cites): each kept memory row and each kept
 * memory a line of prose cites. Stale facts carry Verify instead, and
 * waiting proposals aren't sources yet.
 */
export function numberSources(
  sections: readonly BriefSectionView[],
  memories: Readonly<Record<string, BriefMemory>>,
): Map<string, number> {
  const numbers = new Map<string, number>();
  const add = (ref: string) => {
    if (!numbers.has(ref)) numbers.set(ref, numbers.size + 1);
  };
  for (const section of sections) {
    for (const row of section.rows) {
      if (row.kind === "memory" && row.state === "kept" && row.ref) {
        add(row.ref);
      } else if (row.kind === "prose") {
        for (const ref of row.cites) {
          if (memories[ref]?.state === "kept") add(ref);
        }
      }
    }
  }
  return numbers;
}

/**
 * What the page sets as one block: a row, or consecutive kept lines of
 * prose read as one paragraph (Brief.png's "What this is"), with the
 * first line's receipt in the margin.
 */
export type BriefBlock =
  | { kind: "row"; row: BriefRow }
  | { kind: "paragraph"; rows: BriefRow[] };

export function blocksOf(rows: readonly BriefRow[]): BriefBlock[] {
  const blocks: BriefBlock[] = [];
  for (const row of rows) {
    const last = blocks[blocks.length - 1];
    const prose = row.kind === "prose" && row.state === "kept";
    if (prose && last?.kind === "paragraph") last.rows.push(row);
    else if (prose) blocks.push({ kind: "paragraph", rows: [row] });
    else blocks.push({ kind: "row", row });
  }
  return blocks;
}

/** How many memories the page places or cites: the eyebrow's "14 facts". */
export function factCount(view: BriefView): number {
  const refs = new Set<string>();
  for (const section of view.sections) {
    for (const row of section.rows) {
      if (row.kind === "waiting") continue;
      if (row.ref) refs.add(row.ref);
      row.cites.forEach((ref) => refs.add(ref));
    }
  }
  return refs.size;
}
