/**
 * Selection: which lines belong in a file, where they go, and which go first
 * when the budget runs out.
 *
 * - Sections render in Brief order. By default every section compiles except
 *   `overview` ("short beats complete": context files help most with what's
 *   non-standard, so we skip the overview). A target can name its sections.
 * - A memory renders where the Brief places it. A memory the Brief only cites
 *   in prose is represented by that prose. A memory the Brief doesn't mention
 *   yet (kept after the last Brief version) goes to the end of its own
 *   section, most-read first, so a Keep reaches every file without waiting
 *   for a new Brief.
 * - Open questions compile only with `include: kept_and_open`; with
 *   `kept_only` the `open` section and every open memory stay out.
 * - Stale memories are marked `(Being verified)` or left out; memories in
 *   conflict are marked `(In conflict)`.
 */
import {
  DEFAULT_HEADINGS,
  type Fact,
  type Model,
  type Target,
} from "./model.js";
import { byCodeUnit } from "./text.js";

export interface Entry {
  /** Index into {@link Selection.sections}. */
  section: number;
  /** `""` when unscoped, otherwise the scope key (its globs, comma-joined). */
  group: string;
  paths: string[];
  /** Position in the section when rendered. */
  order: number;
  /** Fill priority in the section; 0 goes first. */
  rank: number;
  /** The memory whose statement this line is, or null for prose. */
  ref: string | null;
  /** The cites printed at the end of the line. */
  refs: string[];
  text: string;
  marker: "Being verified" | "In conflict" | null;
}

export interface Selection {
  sections: { key: string; heading: string }[];
  entries: Entry[];
}

export interface SelectOptions {
  /** Whether a fact belongs to this file at all. */
  accepts(fact: Fact): boolean;
  /** Whether the Brief's connective prose belongs to this file. */
  prose: boolean;
}

const WELL_KNOWN = Object.keys(DEFAULT_HEADINGS);

export function scopeKey(paths: string[]): string {
  return paths.join(",");
}

export function select(
  model: Model,
  target: Target,
  options: SelectOptions,
): Selection {
  const wanted = (key: string) =>
    (target.sections ? target.sections.includes(key) : key !== "overview") &&
    !(key === "open" && target.include === "kept_only");

  const placed = new Set<string>();
  for (const s of model.brief.sections) {
    for (const item of s.items) {
      if (item.kind === "memory") placed.add(item.ref);
      else item.cites.forEach((r) => placed.add(r));
    }
  }

  const compiles = (fact: Fact) =>
    options.accepts(fact) &&
    !(fact.state === "open" && target.include === "kept_only") &&
    !(fact.stale && target.stale === "omit");

  const unplaced = [...model.facts.values()]
    .filter((f) => !placed.has(f.ref) && compiles(f))
    .sort((a, b) => b.score - a.score || byCodeUnit(a.ref, b.ref));

  const briefKeys = new Set(model.brief.sections.map((s) => s.key));
  const extra = WELL_KNOWN.filter(
    (k) => !briefKeys.has(k) && unplaced.some((f) => f.section === k),
  ).map((key) => ({ key, heading: DEFAULT_HEADINGS[key], items: [] }));

  const selection: Selection = { sections: [], entries: [] };
  for (const s of [...model.brief.sections, ...extra]) {
    if (!wanted(s.key)) continue;
    const index =
      selection.sections.push({ key: s.key, heading: s.heading }) - 1;
    const prose: Entry[] = [];
    const facts: Entry[] = [];
    const scores = new Map<Entry, number>();
    let order = 0;

    for (const item of s.items) {
      order += 1;
      if (item.kind === "prose") {
        if (options.prose)
          prose.push(proseEntry(index, order, item.text, item.cites));
        continue;
      }
      const fact = model.facts.get(item.ref);
      if (!fact || !compiles(fact)) continue;
      const entry = factEntry(index, order, fact);
      scores.set(entry, fact.score);
      facts.push(entry);
    }
    for (const fact of unplaced.filter((f) => f.section === s.key)) {
      order += 1;
      const entry = factEntry(index, order, fact);
      scores.set(entry, fact.score);
      facts.push(entry);
    }

    // Prose goes first (it frames the section); then facts, most-read first.
    prose.forEach((e, i) => (e.rank = i));
    [...facts]
      .sort(
        (a, b) =>
          (scores.get(b) ?? 0) - (scores.get(a) ?? 0) || a.order - b.order,
      )
      .forEach((e, i) => (e.rank = prose.length + i));
    selection.entries.push(...prose, ...facts);
  }
  return selection;
}

function factEntry(section: number, order: number, fact: Fact): Entry {
  return {
    section,
    group: scopeKey(fact.paths),
    paths: fact.paths,
    order,
    rank: 0,
    ref: fact.ref,
    refs: [fact.ref],
    text: fact.text,
    marker: fact.conflict
      ? "In conflict"
      : fact.stale
        ? "Being verified"
        : null,
  };
}

function proseEntry(
  section: number,
  order: number,
  text: string,
  cites: string[],
): Entry {
  return {
    section,
    group: "",
    paths: [],
    order,
    rank: 0,
    ref: null,
    refs: cites,
    text,
    marker: null,
  };
}
