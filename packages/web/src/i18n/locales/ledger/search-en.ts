// Search across spaces (Search.png), mounted as `t.ledger.search`. The
// board's copy, plus the states it doesn't draw (nothing typed yet,
// nothing found), in the Ledger voice (./voice.test.ts). Placeholders
// are {name}; `<key>One` is the singular of `<key>`.
export const ledgerSearchEn = {
  field: "Search everything",
  placeholder: "Search memories, decisions and notes",
  askInstead: "Ask instead:",
  // The bar's accessible name: what pressing it does.
  askLabel: "Ask {space} instead: {question}",
  facets: {
    label: "Filters",
    kind: "Kind",
    state: "State",
    space: "Space",
    writtenBy: "Written by",
    // A stamp in "Written by" is a toggle: its name for assistive technology.
    author: "What {name} wrote",
  },
  kinds: {
    memories: "Memories",
    decisions: "Decisions",
    notes: "Raw notes",
  },
  states: {
    kept: "Kept",
    waiting: "Waiting on you",
    stale: "Stale",
  },
  // "9 results for “deploy” in three spaces · 9 raw notes hidden"
  results: "{n} results for “{query}” in {where}",
  resultsOne: "1 result for “{query}” in {where}",
  // More matched than a search ranks: it shows its best 50.
  best: "The best {n} results for “{query}” in {where}",
  spaces: "{n} spaces",
  hidden: "{n} raw notes hidden",
  hiddenOne: "1 raw note hidden",
  decisionsIn: "{n} in {space}",
  more: "{n} more",
  fewer: "Show fewer",
  open: "Open",
  inForce: "In force",
  proposed: "Proposed",
  stale: "Stale",
  // Nothing typed yet.
  start: {
    title: "Search your spaces.",
    detail:
      "Memories, decisions and your raw notes, in every space you're part of.",
  },
  // A search that found nothing.
  none: {
    title: "Nothing matches “{query}”.",
    detail:
      "Try other words, or ask: Memax answers from what's kept and cites it.",
  },
  // It found something, and the filters leave out all of it.
  filtered: {
    title: "Nothing here with these filters.",
    detail: "{n} results for “{query}” are left out.",
    detailOne: "1 result for “{query}” is left out.",
    reset: "Show everything",
  },
  loading: "Searching your spaces",
  loadingMeta: "Searching",
  // Memories' own field found nothing: the way to every space.
  everywhere: "Search all your spaces",
};
