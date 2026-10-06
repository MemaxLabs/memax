"use client";

import { useCallback } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import type { MemoryFilter, MemoryPage } from "@/lib/v2/data/memories";
import type { ReviewItem, ReviewQueue } from "@/lib/v2/data/review";
import type { SpaceOverview, SpaceSummary } from "@/lib/v2/data/types";
import { ledgerQueryKeys, useSource } from "./data";

/**
 * TanStack Query over the source's Review and Memories (records data in
 * lib/v2/data). The demo's first pages come from `peek*`, so the first
 * render (and every screenshot) has them without a loading frame.
 */

export const recordKeys = {
  /** Everything a space holds; invalidating it refreshes the overview too. */
  space: (kind: string, slug: string) => ["v2", kind, "spaces", slug] as const,
  queue: (kind: string, slug: string) =>
    ["v2", kind, "spaces", slug, "review"] as const,
  card: (kind: string, slug: string, ref: string, version: number) =>
    ["v2", kind, "spaces", slug, "review", "card", ref, version] as const,
  conflict: (kind: string, slug: string, ref: string) =>
    ["v2", kind, "spaces", slug, "review", "conflict", ref] as const,
  memories: (kind: string, slug: string, filter: MemoryFilter) =>
    ["v2", kind, "spaces", slug, "memories", filter] as const,
  memory: (kind: string, slug: string, ref: string) =>
    ["v2", kind, "spaces", slug, "memory", ref] as const,
};

export function useReviewQueue(space: SpaceSummary) {
  const source = useSource();
  const peek = source.review.peekQueue?.(space.slug);
  return useInfiniteQuery({
    queryKey: recordKeys.queue(source.kind, space.slug),
    queryFn: ({ pageParam, signal }) =>
      source.review.queue({ space, cursor: pageParam, signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: ReviewQueue) => last.nextCursor ?? undefined,
    initialData: peek ? { pages: [peek], pageParams: [undefined] } : undefined,
    staleTime: 10_000,
  });
}

function cardQuery(
  source: ReturnType<typeof useSource>,
  space: SpaceSummary,
  item: ReviewItem,
) {
  return {
    queryKey: recordKeys.card(source.kind, space.slug, item.ref, item.version),
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      source.review.card({ space, item, signal }),
    staleTime: 60_000,
  };
}

export function useReviewCard(
  space: SpaceSummary,
  item: ReviewItem | undefined,
) {
  const source = useSource();
  return useQuery({
    queryKey: recordKeys.card(
      source.kind,
      space.slug,
      item?.ref ?? "",
      item?.version ?? 0,
    ),
    queryFn: ({ signal }) => source.review.card({ space, item: item!, signal }),
    enabled: item !== undefined,
    initialData: item
      ? source.review.peekCard?.(space.slug, item.ref)
      : undefined,
    staleTime: 60_000,
  });
}

/** Loads the next cards before ↓ gets there (plan §6.5: the next three). */
export function usePrefetchCards(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    (items: readonly ReviewItem[]) => {
      for (const item of items) {
        void queryClient.prefetchQuery(cardQuery(source, space, item));
      }
    },
    [queryClient, source, space],
  );
}

export function useConflict(space: SpaceSummary, ref: string) {
  const source = useSource();
  return useQuery({
    queryKey: recordKeys.conflict(source.kind, space.slug, ref),
    queryFn: ({ signal }) => source.review.conflict({ space, ref, signal }),
  });
}

export function useMemoriesList(space: SpaceSummary, filter: MemoryFilter) {
  const source = useSource();
  const peek = source.memories.peekList?.(space.slug, filter);
  return useInfiniteQuery({
    queryKey: recordKeys.memories(source.kind, space.slug, filter),
    queryFn: ({ pageParam, signal }) =>
      source.memories.list({ space, filter, cursor: pageParam, signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: MemoryPage) => last.nextCursor ?? undefined,
    initialData: peek ? { pages: [peek], pageParams: [undefined] } : undefined,
    staleTime: 30_000,
  });
}

export type MemoryPages = InfiniteData<MemoryPage, string | undefined>;

export function useMemoryRecord(space: SpaceSummary, ref: string) {
  const source = useSource();
  const peek = source.memories.peekRecord?.(space.slug, ref);
  return useQuery({
    queryKey: recordKeys.memory(source.kind, space.slug, ref),
    queryFn: ({ signal }) => source.memories.get({ space, ref, signal }),
    initialData: peek,
    staleTime: 30_000,
  });
}

/**
 * After a decision: the rail's Review count drops at once (the overview
 * is patched), then everything the space shows refetches.
 */
export function useAfterDecision(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    ({ leftQueue }: { leftQueue: boolean }) => {
      if (leftQueue) {
        queryClient.setQueryData<SpaceOverview>(
          ledgerQueryKeys.overview(source.kind, space.slug),
          (overview) =>
            overview && {
              ...overview,
              waiting: Math.max(0, overview.waiting - 1),
            },
        );
      }
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
    },
    [queryClient, source.kind, space.slug],
  );
}
