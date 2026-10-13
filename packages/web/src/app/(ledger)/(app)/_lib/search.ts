"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { SearchResults } from "@/lib/v2/data/search";
import { useSource } from "./data";

/** A search's results, with the words they're for (while new ones load, the last stay). */
export interface SearchRead {
  query: string;
  results: SearchResults;
}

export const searchKeys = {
  all: (kind: string) => ["v2", kind, "search"] as const,
  query: (kind: string, query: string) =>
    ["v2", kind, "search", query] as const,
};

/**
 * Search across the person's spaces for `query` (trimmed; nothing runs
 * for an empty one). The results of the last search stay on screen
 * while the next one loads, so typing never blanks the page.
 */
export function useSearch(query: string) {
  const source = useSource();
  const q = query.trim();
  return useQuery<SearchRead>({
    queryKey: searchKeys.query(source.kind, q),
    queryFn: async ({ signal }) => ({
      query: q,
      results: await source.search.search({ query: q, signal }),
    }),
    enabled: q !== "",
    staleTime: 30_000,
    placeholderData: keepPreviousData,
  });
}
