"use client";

import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { getMemaxClient } from "@/lib/memax-client";
import { useAuth } from "@/lib/auth";
import type { InfiniteData } from "@tanstack/react-query";
import type { Memory } from "memax-sdk";
import type { MemoriesListResponse } from "./use-memories";

export const RECENT_PREVIEW_LIMIT = 5;
export const RECENT_PAGE_LIMIT = 20;
export const TIME_WINDOWS = ["12h", "1d", "3d", "7d"] as const;
export type TimeWindow = (typeof TIME_WINDOWS)[number];
export type RecentActor =
  | "all"
  | "self"
  | `agent:${string}`
  | `author:${string}`;
export type RecentMode = "preview" | "full";

const WINDOW_MS: Record<TimeWindow, number> = {
  "12h": 12 * 3600_000,
  "1d": 24 * 3600_000,
  "3d": 3 * 24 * 3600_000,
  "7d": 7 * 24 * 3600_000,
};

export const recentMemoriesQueryKey = (
  hubId: string | undefined,
  window: TimeWindow,
  actor: RecentActor,
  mode: RecentMode,
) => ["recent-memories", hubId ?? "all", window, actor, mode] as const;

export function getRecentMemoriesInfiniteQueryOptions({
  hubId,
  window,
  actor,
  expanded,
  queryClient,
}: {
  hubId?: string;
  window: TimeWindow;
  actor: RecentActor;
  expanded: boolean;
  queryClient?: ReturnType<typeof useQueryClient>;
}) {
  const mode: RecentMode = expanded ? "full" : "preview";
  const limit = expanded ? RECENT_PAGE_LIMIT : RECENT_PREVIEW_LIMIT;
  return {
    queryKey: recentMemoriesQueryKey(hubId, window, actor, mode),
    queryFn: async ({
      pageParam,
      signal,
    }: {
      pageParam: unknown;
      signal: AbortSignal;
    }) =>
      normalizeRecentResponse(
        await getMemaxClient().memories.list({
          limit,
          sort: "newest",
          cursor: (pageParam as string) || undefined,
          hubId,
          createdAfter: createdAfterForWindow(window),
          actor: actor === "all" ? undefined : actor,
          signal,
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (last: MemoriesListResponse) =>
      last.has_more ? last.next_cursor : undefined,
    placeholderData: (prev: InfiniteData<MemoriesListResponse> | undefined) =>
      prev ??
      (expanded && queryClient
        ? (queryClient.getQueryData(
            recentMemoriesQueryKey(hubId, window, actor, "preview"),
          ) as InfiniteData<MemoriesListResponse> | undefined)
        : undefined),
    staleTime: 60_000,
    refetchInterval: false,
    refetchIntervalInBackground: false,
  } as const;
}

function normalizeRecentResponse(
  res: MemoriesListResponse | Memory[],
): MemoriesListResponse {
  if (Array.isArray(res)) {
    return {
      memories: res,
      next_cursor: "",
      has_more: false,
      total: res.length,
      actors: {},
    };
  }
  return res;
}

function createdAfterForWindow(window: TimeWindow): string {
  return new Date(Date.now() - WINDOW_MS[window]).toISOString();
}

export function useRecentMemories({
  hubId,
  window,
  actor,
  expanded,
}: {
  hubId?: string;
  window: TimeWindow;
  actor: RecentActor;
  expanded: boolean;
}) {
  const { activeHubId } = useAuth();
  const resolvedHubId = (hubId ?? activeHubId) || undefined;
  const mode: RecentMode = expanded ? "full" : "preview";
  const queryClient = useQueryClient();

  return useInfiniteQuery<MemoriesListResponse, Error>({
    ...getRecentMemoriesInfiniteQueryOptions({
      hubId: resolvedHubId,
      window,
      actor,
      expanded,
      queryClient,
    }),
  });
}

export function flattenRecentMemories(
  data: InfiniteData<MemoriesListResponse> | undefined,
): Memory[] {
  if (!data) return [];
  return data.pages.flatMap((page) => page.memories);
}

export function recentMemoriesTotal(
  data: InfiniteData<MemoriesListResponse> | undefined,
): number {
  return data?.pages[0]?.total ?? 0;
}

export function recentActorCounts(
  data: InfiniteData<MemoriesListResponse> | undefined,
): Record<string, number> {
  return data?.pages[0]?.actors ?? {};
}
