"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import type { DreamRun, DreamRunListResponse } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

/**
 * Page size for the Dream history infinite query. Server caps at
 * 100; 20 keeps the first paint small while still covering a full
 * "catch up after a weekend" session in one page for most users.
 */
const PAGE_LIMIT = 20;

export interface UseDreamRunHistoryOptions {
  /**
   * Narrow to a specific hub. Undefined spans every hub the caller
   * participates in (the default for the /settings/you history view,
   * per UX scope doc §3.2).
   */
  hubId?: string;
  /**
   * Enable/disable the query. Useful when the view is conditionally
   * rendered — the component hook order stays stable while still
   * avoiding the network call when not in view.
   */
  enabled?: boolean;
}

export const dreamHistoryQueryKey = (hubId: string | undefined) =>
  ["dreams", "history", hubId ?? "all"] as const;

/**
 * useDreamRunHistory walks the cursor-paginated dreams.list SDK
 * call. TanStack's infiniteQuery handles the page stitching; this
 * hook just wires up the query key + cursor extraction.
 *
 * Cursor shape is deliberately opaque (`<rfc3339nano>|<uuid>`)
 * and produced by the server — callers pass it back as-is. When
 * `next_cursor` is absent, `hasNextPage` goes false and the UI
 * can stop showing a "Load more" affordance.
 *
 * Flattened convenience: `runs` is a single array of every loaded
 * page, newest-first, so consumers don't need to walk
 * `data.pages`. Preserved for callers who want page-level
 * rendering via `data.pages`.
 */
export function useDreamRunHistory(opts: UseDreamRunHistoryOptions = {}) {
  const { hubId, enabled = true } = opts;
  const query = useInfiniteQuery<
    DreamRunListResponse,
    Error,
    { pageParams: unknown[]; pages: DreamRunListResponse[] },
    ReturnType<typeof dreamHistoryQueryKey>,
    string | undefined
  >({
    queryKey: dreamHistoryQueryKey(hubId),
    queryFn: ({ pageParam }) =>
      getMemaxClient().dreams.list({
        hubId,
        limit: PAGE_LIMIT,
        cursor: pageParam,
      }),
    initialPageParam: undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    enabled,
    staleTime: 30 * 1000,
    gcTime: 5 * 60 * 1000,
  });

  const runs: DreamRun[] = query.data?.pages.flatMap((page) => page.runs) ?? [];

  return {
    runs,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    hasNextPage: query.hasNextPage,
    isFetchingNextPage: query.isFetchingNextPage,
    fetchNextPage: query.fetchNextPage,
    refetch: query.refetch,
  };
}
