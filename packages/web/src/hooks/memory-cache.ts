"use client";

/**
 * memory-cache — shared React Query cache primitives for memory writes.
 *
 * Owns every piece of cache surgery that any memory delete/forget/move
 * mutation needs: cancel in-flight queries, snapshot for rollback,
 * optimistically strip ids from list/recent/topic caches, restore on
 * error, and invalidate on settle.
 *
 * Why this module exists
 * ----------------------
 * useDeleteMemory and useMemoryForget both write to the same cache
 * families and must keep them in sync. A shared primitive eliminates
 * drift between them by construction. useMemoryMove still owns its
 * own cancel/invalidate set today (different optimistic shape — moves
 * mutate detail caches in place); migrating it to these primitives is
 * a follow-up.
 *
 * Cache families touched by a memory write
 * ----------------------------------------
 *   memory-lists     — /memories page (useMemories infinite query)
 *   recent-memories  — Brain home feed (useRecentMemories infinite query)
 *   topics/<id>/memories — topic detail memory list
 *   topics           — topic counts/summary
 *   hub-summary      — hub overview tiles
 *   hubs             — hub list with counts
 *   recall           — bar recall results (search relevance)
 *   memory-search    — explicit memory search
 *   memories/<id>    — per-memory detail cache
 *
 * Detail-cache rule (load-bearing, mirrors use-memory-forget.ts v4)
 * -----------------------------------------------------------------
 * The optimistic path NEVER mutates memoryDetailQueryKey(id). The
 * detail route at /memories/[id] must keep rendering during the
 * in-flight window so that a server-side skip (not_owned, not_found,
 * delete_failed) does not blank the page. Detail caches are also
 * OMITTED from the snapshot so that a rollback cannot clobber a
 * concurrent detail refetch with stale snapshot data. Detail
 * reconciliation happens exclusively via onSettled invalidation.
 */

import type { InfiniteData } from "@tanstack/react-query";
import type { TopicMemoriesResponse } from "memax-sdk";
import { queryClient } from "@/lib/query-client";
import { hubListQueryKey } from "./use-hubs";

export type MemorySort = "recent" | "recalled";

export const memoryListQueryKey = (
  hubId: string | undefined,
  sort: MemorySort,
): readonly ["memory-lists", string, MemorySort] => [
  "memory-lists",
  hubId ?? "all",
  sort,
];

export const memoryListQueryPrefix = ["memory-lists"] as const;
export const recentMemoriesQueryPrefix = ["recent-memories"] as const;
export const topicQueryPrefix = ["topics"] as const;

export const memoryDetailQueryKey = (
  id: string,
): readonly ["memories", string] => ["memories", id];

export interface MemoriesListResponse {
  memories: import("memax-sdk").Memory[];
  next_cursor: string;
  has_more: boolean;
  total: number;
  actors?: Record<string, number>;
}

function isTopicMemoriesQueryKey(
  value: readonly unknown[],
): value is readonly ["topics", string, "memories"] {
  return (
    value[0] === "topics" &&
    typeof value[1] === "string" &&
    value[2] === "memories"
  );
}

function removeIdsFromInfiniteData(
  data: InfiniteData<MemoriesListResponse> | undefined,
  ids: Set<string>,
) {
  if (!data?.pages?.length) return data;
  let removed = 0;
  const pages = data.pages.map((page) => {
    const memories = page.memories.filter((memory) => {
      const shouldKeep = !ids.has(memory.id);
      if (!shouldKeep) removed += 1;
      return shouldKeep;
    });
    return memories.length === page.memories.length
      ? page
      : { ...page, memories };
  });
  if (removed === 0) return data;
  const firstPage = pages[0];
  pages[0] = {
    ...firstPage,
    total: Math.max(
      0,
      (firstPage.total ?? firstPage.memories.length) - removed,
    ),
  };
  return { ...data, pages };
}

function removeIdsFromTopicMemories(
  data: TopicMemoriesResponse | undefined,
  ids: Set<string>,
) {
  if (!data?.memories?.length) return data;
  const memories = data.memories.filter((memory) => !ids.has(memory.id));
  return memories.length === data.memories.length
    ? data
    : { ...data, memories };
}

/**
 * Snapshot of every memory list cache before an optimistic mutation.
 * Detail caches are intentionally OMITTED — see module docstring.
 */
export interface MemoryListCacheSnapshot {
  memoryLists: Array<
    [readonly unknown[], InfiniteData<MemoriesListResponse> | undefined]
  >;
  recentLists: Array<
    [readonly unknown[], InfiniteData<MemoriesListResponse> | undefined]
  >;
  topicLists: Array<[readonly unknown[], TopicMemoriesResponse | undefined]>;
}

/**
 * Cancel in-flight queries for every cache family the optimistic path
 * is about to mutate. Detail caches are NOT cancelled — concurrent
 * detail refetches must continue uninterrupted since the optimistic
 * path doesn't touch them.
 */
export async function cancelMemoryListMutations(): Promise<void> {
  await Promise.all([
    queryClient.cancelQueries({ queryKey: memoryListQueryPrefix }),
    queryClient.cancelQueries({ queryKey: recentMemoriesQueryPrefix }),
    queryClient.cancelQueries({ queryKey: topicQueryPrefix }),
  ]);
}

export function snapshotMemoryListCaches(): MemoryListCacheSnapshot {
  return {
    memoryLists: queryClient.getQueriesData<InfiniteData<MemoriesListResponse>>(
      { queryKey: memoryListQueryPrefix },
    ),
    recentLists: queryClient.getQueriesData<InfiniteData<MemoriesListResponse>>(
      { queryKey: recentMemoriesQueryPrefix },
    ),
    topicLists: queryClient
      .getQueriesData<TopicMemoriesResponse>({ queryKey: topicQueryPrefix })
      .filter(
        ([queryKey]) =>
          Array.isArray(queryKey) && isTopicMemoriesQueryKey(queryKey),
      ),
  };
}

export function restoreMemoryListCaches(
  snapshot: MemoryListCacheSnapshot | undefined,
): void {
  if (!snapshot) return;
  snapshot.memoryLists.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
  snapshot.recentLists.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
  snapshot.topicLists.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
}

/**
 * Optimistically strip the given memory ids from every list cache.
 * Does NOT touch detail caches — see module docstring.
 */
export function applyOptimisticMemoryRemoval(ids: string[]): void {
  if (ids.length === 0) return;
  const idSet = new Set(ids);

  queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
    { queryKey: memoryListQueryPrefix },
    (old) => removeIdsFromInfiniteData(old, idSet),
  );
  queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
    { queryKey: recentMemoriesQueryPrefix },
    (old) => removeIdsFromInfiniteData(old, idSet),
  );
  queryClient
    .getQueriesData<TopicMemoriesResponse>({ queryKey: topicQueryPrefix })
    .forEach(([queryKey, data]) => {
      if (!Array.isArray(queryKey) || !isTopicMemoriesQueryKey(queryKey)) {
        return;
      }
      queryClient.setQueryData(
        queryKey,
        removeIdsFromTopicMemories(data, idSet),
      );
    });
}

/**
 * Invalidate every cache family that a memory write affects. Detail
 * keys are invalidated per-id so the detail route at /memories/[id]
 * learns about real deletions via refetch → 404.
 */
export function invalidateMemoryWriteCaches(ids: readonly string[] = []): void {
  queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
  queryClient.invalidateQueries({ queryKey: recentMemoriesQueryPrefix });
  queryClient.invalidateQueries({ queryKey: topicQueryPrefix });
  queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
  queryClient.invalidateQueries({ queryKey: hubListQueryKey });
  queryClient.invalidateQueries({ queryKey: ["recall"] });
  queryClient.invalidateQueries({ queryKey: ["memory-search"] });
  ids.forEach((id) => {
    queryClient.invalidateQueries({ queryKey: memoryDetailQueryKey(id) });
  });
}
