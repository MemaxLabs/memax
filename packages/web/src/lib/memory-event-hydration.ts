"use client";

import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { HubWithRole, Memory } from "memax-sdk";
import type { StreamEnvelope } from "@/lib/event-routing";
import {
  getMemoryQueryOptions,
  memoryDetailQueryKey,
  memoryListQueryPrefix,
  type MemoriesListResponse,
} from "@/hooks/use-memories";
import { hubListQueryKey } from "@/hooks/use-hubs";
import { topicMemoriesQueryKey, topicQueryKey } from "@/hooks/use-topics";
import { usageQueryKey } from "@/hooks/use-usage";
import {
  RECENT_PREVIEW_LIMIT,
  type RecentActor,
  type TimeWindow,
} from "@/hooks/use-recent-memories";
import { markRecentArrival } from "@/lib/recent-arrivals";
import { memoryMatchesRecentActor } from "@/lib/recent-actor";

/**
 * Custom hydration path for `hub.memories.changed` events.
 *
 * Lives here (not as an inline `invalidate` array in event-routing.ts)
 * because it does more than broad invalidation: it fetches the fresh
 * Memory and patches three cache families in place (recent-memories,
 * memory-lists, topic-memories) plus the detail row, so the UI can
 * show the updated row without a full refetch. It ALSO fires broad
 * invalidations for the cross-cutting surfaces (topics, hub list,
 * hub summary, usage) where per-row patching isn't worth it.
 *
 * Extracted from memax-event-bridge.tsx in PR D.5 — the bridge now
 * registers this function as the hub.memories.changed route's
 * `hydrate` hook; see event-routing.ts. Extraction lets the cache
 * mutation code live next to the cache shapes it touches and keeps
 * the bridge focused on SSE plumbing.
 *
 * qc is passed in rather than imported from lib/query-client so the
 * function is testable in isolation with a fresh QueryClient.
 */

const RECENT_QUERY_PREFIX = ["recent-memories"] as const;

const RECENT_WINDOW_MS: Record<TimeWindow, number> = {
  "12h": 12 * 3600_000,
  "1d": 24 * 3600_000,
  "3d": 3 * 24 * 3600_000,
  "7d": 7 * 24 * 3600_000,
};

type MemoryListSort = "recent" | "recalled";

function isRecentQueryKey(
  value: readonly unknown[],
): value is readonly [
  "recent-memories",
  string,
  TimeWindow,
  RecentActor,
  "preview" | "full",
] {
  return (
    value[0] === "recent-memories" &&
    typeof value[1] === "string" &&
    (value[2] === "12h" ||
      value[2] === "1d" ||
      value[2] === "3d" ||
      value[2] === "7d") &&
    typeof value[3] === "string" &&
    (value[4] === "preview" || value[4] === "full")
  );
}

function memoryWithinWindow(memory: Memory, window: TimeWindow) {
  return (
    Date.now() - new Date(memory.created_at).getTime() <=
    RECENT_WINDOW_MS[window]
  );
}

export function updateMemoryListData(
  data: InfiniteData<MemoriesListResponse> | undefined,
  memory: Memory,
  state: string | undefined,
  sort: MemoryListSort,
) {
  if (!data?.pages?.length) return data;

  let found = false;
  let removed = false;
  const nextPages = data.pages.map((page) => {
    const memories = page.memories.flatMap((item) => {
      if (item.id !== memory.id) return [item];
      found = true;
      if (state === "deleted") {
        removed = true;
        return [];
      }
      return [memory];
    });
    return memories.length === page.memories.length
      ? page
      : { ...page, memories };
  });

  if (state === "deleted") {
    if (!removed) return data;
    const first = nextPages[0];
    return {
      ...data,
      pages: nextPages.map((page, index) =>
        index === 0
          ? {
              ...page,
              total: Math.max(0, (first.total ?? first.memories.length) - 1),
            }
          : page,
      ),
    };
  }

  if (found) {
    return { ...data, pages: nextPages };
  }

  if (state === "created" && sort === "recent") {
    const first = nextPages[0];
    if (!first) return data;
    return {
      ...data,
      pages: [
        {
          ...first,
          memories: [memory, ...first.memories],
          total: (first.total ?? first.memories.length) + 1,
        },
        ...nextPages.slice(1),
      ],
    };
  }

  return data;
}

export function updateTopicMemoriesData(
  data: { memories: Memory[]; next_cursor: string; has_more: boolean } | null,
  memory: Memory,
  state: string | undefined,
) {
  if (!data) return data;
  const existingIndex = data.memories.findIndex(
    (item) => item.id === memory.id,
  );
  if (state === "deleted") {
    if (existingIndex === -1) return data;
    return {
      ...data,
      memories: data.memories.filter((item) => item.id !== memory.id),
    };
  }

  if (existingIndex >= 0) {
    const next = [...data.memories];
    next[existingIndex] = memory;
    return { ...data, memories: next };
  }

  if (state === "created") {
    return { ...data, memories: [memory, ...data.memories] };
  }

  return data;
}

export function upsertRecentData(
  data: InfiniteData<MemoriesListResponse> | undefined,
  memory: Memory,
  mode: "preview" | "full",
  state: string | undefined,
) {
  if (!data?.pages?.length) return data;

  let foundExisting = false;
  const pages = data.pages.map((page) => {
    if (!page.memories.some((item) => item.id === memory.id)) {
      return page;
    }
    foundExisting = true;
    return {
      ...page,
      memories: page.memories.map((item) =>
        item.id === memory.id ? memory : item,
      ),
    };
  });

  if (foundExisting) {
    return { ...data, pages };
  }

  if (state !== "created") {
    return data;
  }

  const firstPage = data.pages[0];
  const nextFirstPage = {
    ...firstPage,
    memories:
      mode === "preview"
        ? [memory, ...firstPage.memories].slice(0, RECENT_PREVIEW_LIMIT)
        : [memory, ...firstPage.memories],
    total: (firstPage.total ?? firstPage.memories.length) + 1,
  };

  return {
    ...data,
    pages: [nextFirstPage, ...data.pages.slice(1)],
  };
}

/**
 * Fetch the updated memory and patch it into every cache family that
 * renders it. Called by the SSE route registry for
 * `hub.memories.changed` events that carry an entity_id. Swallows
 * fetch failures (the memory may have been deleted or become
 * inaccessible between the event and the fetch).
 */
export async function hydrateMemoryFromEvent(
  payload: StreamEnvelope,
  qc: QueryClient,
): Promise<void> {
  if (!payload.entity_id) return;

  try {
    // Route the hydrate fetch through React Query so it dedupes with the
    // processing-aware poll in useMemory(). Without fetchQuery, a raw SDK
    // memories.get bypasses the keyed cache and an overlapping poll can
    // fire a second request. fetchQuery collapses concurrent fetches on
    // the same query key — if a poll is already in flight, we reuse its
    // promise. staleTime=0 forces a fresh fetch regardless of the
    // detail query's default 60s staleness — the SSE event is our signal
    // that server state moved, so cached data is by definition stale.
    const memory = await qc.fetchQuery({
      ...getMemoryQueryOptions(payload.entity_id),
      staleTime: 0,
    });
    const hubs = qc.getQueryData<HubWithRole[]>(hubListQueryKey);
    let inserted = false;

    qc.getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: RECENT_QUERY_PREFIX,
    }).forEach(([queryKey, data]) => {
      if (!Array.isArray(queryKey) || !isRecentQueryKey(queryKey)) return;

      const [, hubId, window, actor, mode] = queryKey;
      if (hubId !== memory.hub_id) return;
      if (!memoryWithinWindow(memory, window)) return;
      const hubType =
        hubs?.find((entry) => entry.hub.id === hubId)?.hub.hub_type ?? null;
      if (!memoryMatchesRecentActor(memory, actor, { hubType })) return;

      const alreadyPresent = !!data?.pages.some((page) =>
        page.memories.some((item) => item.id === memory.id),
      );
      const next = upsertRecentData(data, memory, mode, payload.state);
      if (!next || next === data) return;
      qc.setQueryData(queryKey, next);
      if (!alreadyPresent) {
        inserted = true;
      }
    });

    qc.setQueryData(memoryDetailQueryKey(memory.id), memory);

    qc.getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: memoryListQueryPrefix,
    }).forEach(([queryKey, current]) => {
      if (!Array.isArray(queryKey)) return;
      const [, hubId, sort] = queryKey as [
        "memory-lists",
        string,
        MemoryListSort,
      ];
      if (hubId !== (memory.hub_id || "all")) return;
      const next = updateMemoryListData(current, memory, payload.state, sort);
      if (next && next !== current) {
        qc.setQueryData(queryKey, next);
      }
    });

    if (payload.topic_id) {
      qc.setQueryData(
        topicMemoriesQueryKey(payload.topic_id),
        (
          current: {
            memories: Memory[];
            next_cursor: string;
            has_more: boolean;
          } | null,
        ) => updateTopicMemoriesData(current, memory, payload.state),
      );
      qc.invalidateQueries({
        queryKey: topicQueryKey(payload.topic_id),
      });
      qc.invalidateQueries({ queryKey: ["topics"] });
    }

    if (inserted && payload.state === "created") {
      markRecentArrival(memory.id);
    }

    qc.invalidateQueries({ queryKey: ["hub-summary"] });
    qc.invalidateQueries({ queryKey: hubListQueryKey });
    qc.invalidateQueries({ queryKey: usageQueryKey });
  } catch {
    // Memory may have been deleted or become inaccessible before the
    // hydration fetch. Swallow — the event's broad invalidate still
    // fires (via the route's invalidate list) so the UI reconciles.
  }
}
