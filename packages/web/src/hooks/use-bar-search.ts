import { useQuery } from "@tanstack/react-query";
import type { BarSearchResult } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useDebouncedValue } from "./use-debounced-value";

const EMPTY_RESULT: BarSearchResult = {
  query: "",
  memories: [],
  topics: [],
  hubs: [],
};

/**
 * Unified search for the bar's pre-Enter "quick matches" layer.
 * Debounced 150ms so typing "hello" doesn't fire 5 HTTP calls.
 *
 * Returns `resolvedQuery` alongside `data` so callers can detect when
 * the dataset belongs to a past keystroke (debounce still pending)
 * and skip rendering mismatched rows.
 *
 * keyword (0ms, client) → bar.search (150ms, server) → semantic (1-3s) → AI (3-8s)
 */
export function useBarSearch(query: string, hubId?: string, topicId?: string) {
  const debouncedQuery = useDebouncedValue(query, 150);
  const result = useQuery({
    queryKey: ["bar-search", debouncedQuery, hubId ?? "all", topicId ?? "all"],
    queryFn: async ({ signal }) => {
      if (debouncedQuery.length < 2) return EMPTY_RESULT;
      return getMemaxClient().bar.search(debouncedQuery, {
        limit: 10,
        hubId,
        topicId,
        signal,
      });
    },
    staleTime: 30_000,
    gcTime: 60_000,
    refetchOnWindowFocus: false,
  });
  return { ...result, resolvedQuery: debouncedQuery };
}
