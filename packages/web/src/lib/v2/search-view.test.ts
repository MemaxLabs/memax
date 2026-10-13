import { describe, expect, it } from "vitest";
import type { SearchHit, SearchNote, SearchResults } from "./data/search";
import {
  authorKey,
  authorsOf,
  DEFAULT_OFF,
  highlightRuns,
  matchTerms,
  searchTerms,
  searchView,
  stateOf,
  toggle,
} from "./search-view";

function hit(
  ref: string,
  fields: Partial<SearchHit> & { state?: SearchHit["item"]["state"] } = {},
): SearchHit {
  const { state = "kept", ...rest } = fields;
  return {
    spaceId: "s1",
    kind: "fact",
    item: {
      ref,
      statement: `${ref} deploys`,
      section: "conventions",
      state,
      receipt: null,
      source: null,
      note: null,
      forgotten: null,
    },
    author: { kind: "person", self: true },
    decision: null,
    at: "2026-10-05T14:00:00Z",
    ...rest,
  };
}

const note = (ref: string, spaceId = "s1"): SearchNote => ({
  ref,
  spaceId,
  title: ref,
  excerpt: `${ref} about deploys`,
  at: "2026-07-10T10:00:00Z",
});

const results = (
  hits: SearchHit[],
  notes: SearchNote[] = [],
): SearchResults => ({
  hits,
  notes,
  more: false,
  lexicalOnly: true,
});

describe("the words", () => {
  it("folds case, drops common words and repeats", () => {
    expect(searchTerms("Where does the v2 API Deploy? deploy")).toEqual([
      "v2",
      "api",
      "deploy",
    ]);
    expect(searchTerms("  ")).toEqual([]);
  });

  it("matches words by their start, as the API's full-text lane does", () => {
    expect(matchTerms("No Friday deploys after 16:00", "deploy")).toBe(1);
    expect(matchTerms("No Friday deploys after 16:00", "friday deploy")).toBe(
      2,
    );
    expect(matchTerms("Redeploy nightly", "deploy")).toBe(0);
    expect(matchTerms("anything", "the")).toBe(0);
  });

  it("highlights each whole word a query word starts", () => {
    expect(highlightRuns("No Friday deploys after 16:00", "deploy")).toEqual([
      { text: "No Friday ", hl: false },
      { text: "deploys", hl: true },
      { text: " after 16:00", hl: false },
    ]);
    expect(highlightRuns("Deploy the API.", "deploy api")).toEqual([
      { text: "Deploy", hl: true },
      { text: " the ", hl: false },
      { text: "API", hl: true },
      { text: ".", hl: false },
    ]);
    // Matched by meaning alone: nothing to mark.
    expect(highlightRuns("Background jobs run on River.", "queue")).toEqual([
      { text: "Background jobs run on River.", hl: false },
    ]);
  });
});

describe("the facets", () => {
  it("count states as Search draws them: kept, waiting on you, stale", () => {
    expect(stateOf("kept")).toBe("kept");
    expect(stateOf("proposed")).toBe("waiting");
    expect(stateOf("conflict")).toBe("waiting");
    expect(stateOf("stale")).toBe("stale");
    expect(stateOf("forgotten")).toBeNull();
  });

  it("key authors it can stamp, and only those", () => {
    expect(authorKey({ kind: "person", self: true })).toBe("you");
    expect(authorKey({ kind: "person", self: false, initials: "JY" })).toBe(
      "person:JY",
    );
    expect(authorKey({ kind: "person", self: false })).toBeNull();
    expect(authorKey({ kind: "agent", agent: "codex" })).toBe("agent:codex");
    expect(authorKey(null)).toBeNull();
  });

  it("list you first, then people, then agents by name", () => {
    const keys = authorsOf([
      hit("M-1", { author: { kind: "agent", agent: "cursor" } }),
      hit("M-2", { author: { kind: "person", self: false, initials: "JY" } }),
      hit("M-3", { author: { kind: "agent", agent: "claude-code" } }),
      hit("M-4"),
      hit("M-5", { author: { kind: "agent", agent: "codex" } }),
      hit("M-6"),
    ]).map((a) => a.key);
    expect(keys).toEqual([
      "you",
      "person:JY",
      "agent:claude-code",
      "agent:codex",
      "agent:cursor",
    ]);
  });

  it("hide notes until asked for, and say how many", () => {
    const r = results(
      [hit("M-1"), hit("M-2", { kind: "decision", decision: "open" })],
      [note("N-1"), note("N-2")],
    );
    const view = searchView(r, DEFAULT_OFF);
    expect(view.memories.map((h) => h.item.ref)).toEqual(["M-1"]);
    expect(view.decisions.map((h) => h.item.ref)).toEqual(["M-2"]);
    expect(view.notes).toEqual([]);
    expect(view.hiddenNotes).toBe(2);
    const shown = searchView(r, {
      ...DEFAULT_OFF,
      kinds: toggle(DEFAULT_OFF.kinds, "notes"),
    });
    expect(shown.notes.map((n) => n.ref)).toEqual(["N-1", "N-2"]);
    expect(shown.hiddenNotes).toBe(0);
  });

  it("filter by kind, state, space and author, counting over everything", () => {
    const r = results(
      [
        hit("M-1"),
        hit("M-2", {
          state: "proposed",
          author: { kind: "agent", agent: "codex" },
        }),
        hit("M-3", { state: "stale", spaceId: "s2" }),
        hit("M-4", { kind: "decision", decision: "in_force", spaceId: "s2" }),
        hit("M-5", { author: { kind: "person", self: false } }),
      ],
      [note("N-1", "s2")],
    );
    const all = searchView(r, DEFAULT_OFF);
    expect(all.counts.kinds).toEqual({ memories: 4, decisions: 1, notes: 1 });
    expect(all.counts.states).toEqual({ kept: 3, waiting: 1, stale: 1 });
    expect(all.counts.spaces).toEqual({ s1: 3, s2: 2 });
    expect(all.counts.authors).toEqual({ you: 3, "agent:codex": 1 });

    const narrowed = searchView(r, {
      kinds: ["notes", "decisions"],
      states: ["stale"],
      spaces: [],
      authors: ["agent:codex"],
    });
    // M-5's author can't be stamped, so it's never filtered by author.
    expect(narrowed.memories.map((h) => h.item.ref)).toEqual(["M-1", "M-5"]);
    expect(narrowed.decisions).toEqual([]);
    // The counts don't move with the filters.
    expect(narrowed.counts).toEqual(all.counts);

    const s1 = searchView(r, { ...DEFAULT_OFF, spaces: ["s2"] });
    expect(s1.memories.map((h) => h.item.ref)).toEqual(["M-1", "M-2", "M-5"]);
    expect(s1.hiddenNotes).toBe(0);
  });
});
