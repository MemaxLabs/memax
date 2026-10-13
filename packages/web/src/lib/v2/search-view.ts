/**
 * What Search (Search.png) shows from a search's results: the words that
 * matched (Highlight), the facets with their counts (kind, state, space,
 * who wrote it) and what passes them. Pure, so the rules are
 * unit-tested; the page and the demo's search both use it.
 */
import type { DisplayState, Actor } from "./data/records";
import type { SearchHit, SearchNote, SearchResults } from "./data/search";

// ---------------------------------------------------------------------
// The words
// ---------------------------------------------------------------------

const WORD = /[\p{L}\p{N}]+/gu;

/** Words too common to match by, as the API's full-text lane skips them. */
const STOPWORDS = new Set(
  (
    "a an and are as at be by do does for from how i in is it of on or should " +
    "that the this to we what when where which who why with use"
  ).split(" "),
);

function fold(text: string): string {
  return text.normalize("NFKC").toLowerCase();
}

/** The query's words, folded, without stopwords, each once (at most 16). */
export function searchTerms(query: string): string[] {
  const terms: string[] = [];
  for (const word of fold(query).match(WORD) ?? []) {
    if (STOPWORDS.has(word) || terms.includes(word)) continue;
    terms.push(word);
    if (terms.length === 16) break;
  }
  return terms;
}

/** How many of the query's words start a word of `text` (prefix matching, like `deploy:*`). */
export function matchTerms(text: string, query: string): number {
  const terms = searchTerms(query);
  if (terms.length === 0) return 0;
  const words = fold(text).match(WORD) ?? [];
  return terms.filter((t) => words.some((w) => w.startsWith(t))).length;
}

/** A run of a statement: highlighted where a query word starts one of its words. */
export interface Run {
  text: string;
  hl: boolean;
}

/**
 * Splits `text` into runs, highlighting each whole word that a query word
 * starts ("deploys" for "deploy"), as the board marks them. A search
 * that matched by meaning alone highlights nothing, which is honest.
 */
export function highlightRuns(text: string, query: string): Run[] {
  const terms = searchTerms(query);
  const runs: Run[] = [];
  const push = (part: string, hl: boolean) => {
    if (!part) return;
    const last = runs[runs.length - 1];
    if (last && last.hl === hl) last.text += part;
    else runs.push({ text: part, hl });
  };
  let at = 0;
  for (const m of text.matchAll(WORD)) {
    const start = m.index ?? 0;
    push(text.slice(at, start), false);
    const word = m[0];
    push(
      word,
      terms.some((t) => fold(word).startsWith(t)),
    );
    at = start + word.length;
  }
  push(text.slice(at), false);
  return runs;
}

// ---------------------------------------------------------------------
// The facets
// ---------------------------------------------------------------------

/** Kind: Memories, Decisions, Raw notes. */
export type KindFacet = "memories" | "decisions" | "notes";
/** State: Kept, Waiting on you, Stale. */
export type StateFacet = "kept" | "waiting" | "stale";

export const KIND_FACETS: readonly KindFacet[] = [
  "memories",
  "decisions",
  "notes",
];
export const STATE_FACETS: readonly StateFacet[] = ["kept", "waiting", "stale"];

/** What the person turned off; anything not listed is on. */
export interface SearchOff {
  kinds: KindFacet[];
  states: StateFacet[];
  /** Space ids. */
  spaces: string[];
  /** authorKey()s. */
  authors: string[];
}

/** Notes are raw material: off until asked for, as drawn. */
export const DEFAULT_OFF: SearchOff = {
  kinds: ["notes"],
  states: [],
  spaces: [],
  authors: [],
};

export function kindOf(hit: SearchHit): KindFacet {
  return hit.kind === "decision" ? "decisions" : "memories";
}

/** The State facet a memory's state counts under; null for states search never returns. */
export function stateOf(state: DisplayState): StateFacet | null {
  switch (state) {
    case "kept":
      return "kept";
    case "proposed":
    case "conflict":
      return "waiting";
    case "stale":
      return "stale";
    default:
      return null;
  }
}

/**
 * Who wrote it, as a facet key: "you", "person:JY", "agent:codex",
 * "dream". Null for an actor the page can't stamp (another person whose
 * initials the receipts don't carry): such a hit is never filtered out.
 */
export function authorKey(actor: Actor | null): string | null {
  if (!actor) return null;
  switch (actor.kind) {
    case "person":
      if (actor.self) return "you";
      return actor.initials ? `person:${actor.initials}` : null;
    case "agent":
      return `agent:${actor.agent}`;
    case "dream":
      return "dream";
    case "memax":
    case "repository":
      return null;
  }
}

/** A stampable author with its facet key, in the facet's order. */
export interface AuthorFacet {
  key: string;
  actor: Actor;
}

/**
 * The people and agents who wrote what was found, each once: you first,
 * then other people by their initials, then agents by name.
 */
export function authorsOf(hits: readonly SearchHit[]): AuthorFacet[] {
  const seen = new Map<string, Actor>();
  for (const h of hits) {
    const key = authorKey(h.author);
    if (key && h.author && !seen.has(key)) seen.set(key, h.author);
  }
  const rank = (key: string) =>
    key === "you" ? 0 : key.startsWith("person:") ? 1 : 2;
  return [...seen.entries()]
    .map(([key, actor]) => ({ key, actor }))
    .sort((a, b) => rank(a.key) - rank(b.key) || a.key.localeCompare(b.key));
}

function passes(hit: SearchHit, off: SearchOff): boolean {
  if (off.kinds.includes(kindOf(hit))) return false;
  const state = stateOf(hit.item.state);
  if (state && off.states.includes(state)) return false;
  if (off.spaces.includes(hit.spaceId)) return false;
  const author = authorKey(hit.author);
  return !(author && off.authors.includes(author));
}

/** What a page of results shows under the facets, and the facets' counts. */
export interface SearchView {
  memories: SearchHit[];
  decisions: SearchHit[];
  /** Notes shown (the Raw notes kind is on). */
  notes: SearchNote[];
  /** Notes found but not shown, in the spaces that are on. */
  hiddenNotes: number;
  /** Each facet value's count over everything found, whatever is turned off. */
  counts: {
    kinds: Record<KindFacet, number>;
    states: Record<StateFacet, number>;
    spaces: Record<string, number>;
    authors: Record<string, number>;
  };
}

export function searchView(results: SearchResults, off: SearchOff): SearchView {
  const counts: SearchView["counts"] = {
    kinds: { memories: 0, decisions: 0, notes: results.notes.length },
    states: { kept: 0, waiting: 0, stale: 0 },
    spaces: {},
    authors: {},
  };
  for (const hit of results.hits) {
    counts.kinds[kindOf(hit)]++;
    const state = stateOf(hit.item.state);
    if (state) counts.states[state]++;
    counts.spaces[hit.spaceId] = (counts.spaces[hit.spaceId] ?? 0) + 1;
    const author = authorKey(hit.author);
    if (author) counts.authors[author] = (counts.authors[author] ?? 0) + 1;
  }
  const shown = results.hits.filter((hit) => passes(hit, off));
  const notesInSpaces = results.notes.filter(
    (n) => !off.spaces.includes(n.spaceId),
  );
  const notesOn = !off.kinds.includes("notes");
  return {
    memories: shown.filter((h) => h.kind !== "decision"),
    decisions: shown.filter((h) => h.kind === "decision"),
    notes: notesOn ? notesInSpaces : [],
    hiddenNotes: notesOn ? 0 : notesInSpaces.length,
    counts,
  };
}

/** Turns one facet value on or off. */
export function toggle<T extends string>(list: readonly T[], value: T): T[] {
  return list.includes(value)
    ? list.filter((v) => v !== value)
    : [...list, value];
}
