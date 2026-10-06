import type { Actor } from "./records";
import {
  sectionKeyOf,
  type BriefMemory,
  type BriefRow,
  type BriefRowReceipt,
  type BriefSectionView,
  type BriefStructure,
  type BriefView,
} from "./brief";
import type { Section } from "./types";

/**
 * Builds the page's view of a Brief version from its structure and the
 * memories it rests on, by the compiler's placement rules
 * (packages/compiler › select.ts): sections in Brief order, each memory
 * where the Brief places it, kept memories the Brief doesn't mention yet
 * at the end of their own section, and (for the page only) the
 * proposals waiting in each section. Pure, so both sources share it and
 * the rules are unit-tested.
 */

/** A memory as the builder reads it. */
export interface BriefInputMemory {
  ref: string;
  text: string;
  section: Section;
  state: BriefMemory["state"];
  /** Its lifecycle: proposals wait in Review, kept memories compile. */
  lifecycle: "kept" | "proposed" | "other";
  version: number;
  receipt: BriefRowReceipt | null;
  source: string | null;
  scope: string[];
  /** The words the latest version changed, when the source knows. */
  changed?: string | null;
}

/** The compiler's headings for well-known sections the Brief doesn't have yet. */
const DEFAULT_HEADINGS: Record<string, string> = {
  overview: "What this is",
  decisions: "Decisions",
  conventions: "Conventions",
  open: "Open",
  preferences: "Preferences",
};

/** Proposals shown under a section, at most; Review holds them all. */
const WAITING_PER_SECTION = 3;

const rowState = (m: BriefInputMemory): BriefRow["state"] =>
  m.state === "stale" || m.state === "conflict" || m.state === "proposed"
    ? m.state
    : "kept";

function memoryRow(
  m: BriefInputMemory,
  kind: "memory" | "waiting",
  placed: boolean,
): BriefRow {
  return {
    key: m.ref,
    kind,
    ref: m.ref,
    text: m.text,
    cites: [],
    state: rowState(m),
    placed,
    version: m.version,
    receipt: m.receipt,
    source: m.source,
    scope: m.scope,
    changed: m.changed ?? null,
  };
}

/** The key of the n-th line of prose, in reading order. */
export const proseKey = (n: number) => `prose-${n}`;

export function buildBriefView({
  id,
  ref,
  version,
  structure,
  by,
  at,
  reason,
  memories,
  proseAuthors = new Map(),
  titles = {},
  reads = null,
}: {
  id: string;
  ref: string;
  version: number;
  structure: BriefStructure;
  by: Actor | null;
  at: string;
  reason: string | null;
  /** Every memory the source read: kept ones and the proposals waiting. */
  memories: readonly BriefInputMemory[];
  /** Who wrote each line of prose, by proseKey; the version's author otherwise. */
  proseAuthors?: ReadonlyMap<string, BriefRowReceipt>;
  titles?: Record<string, string>;
  reads?: BriefView["reads"];
}): BriefView {
  const byRef = new Map(memories.map((m) => [m.ref, m]));
  const mentioned = new Set<string>();
  for (const section of structure.sections) {
    for (const item of section.items) {
      if ("ref" in item) mentioned.add(item.ref);
      else item.cites.forEach((r) => mentioned.add(r));
    }
  }

  let prose = 0;
  const sections: BriefSectionView[] = structure.sections.map((section) => ({
    key: section.key,
    heading: section.heading,
    rows: section.items.flatMap((item): BriefRow[] => {
      if ("ref" in item) {
        const m = byRef.get(item.ref);
        // A memory that isn't kept any more (forgotten, merged) has no row.
        return m && m.lifecycle === "kept"
          ? [memoryRow(m, "memory", true)]
          : [];
      }
      const key = proseKey(prose++);
      const cited = item.cites.flatMap((r) => byRef.get(r) ?? []);
      // A line that rests on something still waiting reads as waiting,
      // with the waiting proposal's receipt (Codex proposed · 14 min ago).
      const conflict = cited.some((m) => m.state === "conflict");
      const waiting =
        cited.find((m) => m.lifecycle === "proposed") ??
        cited.find((m) => m.state === "conflict");
      const author = proseAuthors.get(key) ?? { by, action: "wrote", at };
      return [
        {
          key,
          kind: "prose",
          ref: null,
          text: item.text,
          cites: item.cites,
          state: conflict ? "conflict" : waiting ? "proposed" : "kept",
          placed: true,
          version: null,
          receipt: waiting?.receipt ?? author,
          source: null,
          scope: [],
          changed: null,
        },
      ];
    }),
  }));

  const sectionFor = (m: BriefInputMemory, create: boolean) => {
    const key = sectionKeyOf(m.section);
    let found = sections.find((s) => s.key === key);
    if (!found && create && DEFAULT_HEADINGS[key]) {
      found = { key, heading: DEFAULT_HEADINGS[key], rows: [] };
      sections.push(found);
    }
    return found;
  };

  // Kept after the last version: the compiler puts them at the end of their section.
  for (const m of memories) {
    if (m.lifecycle !== "kept" || mentioned.has(m.ref)) continue;
    sectionFor(m, true)?.rows.push(memoryRow(m, "memory", false));
  }
  // Waiting in Review: shown in their section, never compiled.
  const shown = new Map<string, number>();
  for (const m of memories) {
    if (m.lifecycle !== "proposed" || mentioned.has(m.ref)) continue;
    const section = sectionFor(m, false);
    if (!section) continue;
    const n = shown.get(section.key) ?? 0;
    if (n >= WAITING_PER_SECTION) continue;
    shown.set(section.key, n + 1);
    section.rows.push(memoryRow(m, "waiting", false));
  }

  const statements: Record<string, BriefMemory> = {};
  for (const m of memories) {
    statements[m.ref] = {
      text: m.text,
      state: m.state,
      section: m.section,
      version: m.version,
      kept: m.lifecycle === "kept",
    };
  }

  return {
    id,
    ref,
    version,
    title: structure.title,
    summary: structure.summary,
    // An empty section has nothing to show (the editor reads the structure).
    sections: sections.filter((s) => s.rows.length > 0),
    by,
    at,
    reason,
    structure,
    memories: statements,
    titles,
    reads,
  };
}

/**
 * Who wrote each line of prose in the current version: the oldest
 * version, walking back from the current one without a gap, that has a
 * line with the same words. Versions are newest first.
 */
export function proseAuthorsOf(
  current: BriefStructure,
  versions: readonly {
    structure: BriefStructure;
    by: Actor | null;
    at: string;
  }[],
): Map<string, BriefRowReceipt> {
  const authors = new Map<string, BriefRowReceipt>();
  const texts = (s: BriefStructure) =>
    new Set(
      s.sections.flatMap((section) =>
        section.items.flatMap((i) => ("text" in i ? [i.text] : [])),
      ),
    );
  const lines = current.sections.flatMap((section) =>
    section.items.flatMap((i) => ("text" in i ? [i.text] : [])),
  );
  const sets = versions.map((v) => texts(v.structure));
  lines.forEach((text, n) => {
    let oldest = -1;
    for (let i = 0; i < versions.length && sets[i]!.has(text); i++) oldest = i;
    const v = versions[oldest];
    if (v) authors.set(proseKey(n), { by: v.by, action: "wrote", at: v.at });
  });
  return authors;
}
