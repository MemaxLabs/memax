/**
 * Search across spaces (Search.png, epic 3.3): the memories that match
 * some words in every space the person reads, with their notes beside.
 * Part of LedgerDataSource (source.ts) as `source.search`; search-sdk.ts
 * (GET /v2/search) and the demo (search-demo.ts) implement it.
 *
 * Data, no words: the page builds every sentence from these values
 * (search-view.ts, the catalogue).
 */
import type { MemoryListItem } from "./memories";
import type { Actor } from "./records";

/** One memory a search found. */
export interface SearchHit {
  /** The space it is in (SpaceSummary.id). */
  spaceId: string;
  kind: "fact" | "decision";
  /** The row as Memories draws it: statement, state, receipt, note. */
  item: MemoryListItem;
  /** Who wrote it: the receipt that made it. */
  author: Actor | null;
  /**
   * A decision's standing, for its row: `open` while it's undecided,
   * else in force (superseded decisions never come back).
   */
  decision: "open" | "in_force" | null;
  /** When it was last kept, proposed or changed: a decision row's date. */
  at: string;
}

/** One of the person's notes (N-) that matched: raw material, never kept context. */
export interface SearchNote {
  /** N-0882, once numbered. */
  ref: string | null;
  spaceId: string;
  title: string;
  /** The start of its words. */
  excerpt: string;
  at: string;
}

export interface SearchResults {
  /** Best first. */
  hits: SearchHit[];
  /** The person's notes that match, best first. */
  notes: SearchNote[];
  /** The search found more than it returned (it ranks its best 50). */
  more: boolean;
  /** Matched by the words alone, not their meaning. */
  lexicalOnly: boolean;
}

export interface SearchSource {
  /** Search every space the person reads that is on the V2 record. */
  search(input: {
    query: string;
    signal?: AbortSignal;
  }): Promise<SearchResults>;
}

/** The longest search the API takes, in characters. */
export const MAX_SEARCH = 500;
