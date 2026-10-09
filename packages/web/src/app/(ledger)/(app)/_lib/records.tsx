"use client";

import { useCallback, useState } from "react";
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
  card: (
    kind: string,
    slug: string,
    ref: string,
    version: number,
    facet = "",
  ) =>
    [
      "v2",
      kind,
      "spaces",
      slug,
      "review",
      "card",
      ref,
      version,
      facet,
    ] as const,
  conflict: (kind: string, slug: string, ref: string) =>
    ["v2", kind, "spaces", slug, "review", "conflict", ref] as const,
  memories: (kind: string, slug: string, filter: MemoryFilter) =>
    ["v2", kind, "spaces", slug, "memories", filter] as const,
  memory: (kind: string, slug: string, ref: string) =>
    ["v2", kind, "spaces", slug, "memory", ref] as const,
  tombstone: (kind: string, slug: string, ref: string) =>
    ["v2", kind, "spaces", slug, "memory", ref, "tombstone"] as const,
  forgetPreview: (kind: string, slug: string, ref: string, version: number) =>
    [
      "v2",
      kind,
      "spaces",
      slug,
      "memory",
      ref,
      "forget-preview",
      version,
    ] as const,
};

/**
 * While the judge is checking something in the queue, Review refetches
 * it (plan §5.8: a verdict within about 5 s; there's no SSE yet): after
 * 1 s, 2 s and 4 s, then every 5 s, and it stops once nothing is
 * working. By time since the check was first seen, in steps, so a
 * re-render never reschedules a refetch.
 */
export function judgePollDelay(elapsedMs: number): number {
  if (elapsedMs < 1000) return 1000;
  if (elapsedMs < 3000) return 2000;
  if (elapsedMs < 7000) return 4000;
  return 5000;
}

function anyWorking(data: InfiniteData<ReviewQueue> | undefined): boolean {
  return Boolean(
    data?.pages.some((page) => page.items.some((i) => i.judge === "working")),
  );
}

export function useReviewQueue(space: SpaceSummary) {
  const source = useSource();
  const peek = source.review.peekQueue?.(space.slug);
  const [poll] = useState(() => ({ since: null as number | null }));
  return useInfiniteQuery({
    queryKey: recordKeys.queue(source.kind, space.slug),
    queryFn: ({ pageParam, signal }) =>
      source.review.queue({ space, cursor: pageParam, signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: ReviewQueue) => last.nextCursor ?? undefined,
    initialData: peek ? { pages: [peek], pageParams: [undefined] } : undefined,
    staleTime: 10_000,
    refetchInterval: (query) => {
      if (!anyWorking(query.state.data)) {
        poll.since = null;
        return false;
      }
      poll.since ??= Date.now();
      return judgePollDelay(Date.now() - poll.since);
    },
  });
}

/**
 * What the card shows beyond the version: a verdict landing (working →
 * an update, a conflict) changes it without a new version.
 */
export function cardFacet(item: ReviewItem): string {
  return [
    item.state,
    item.judge ?? "",
    item.updates ?? "",
    item.conflictsWith ?? "",
  ].join("|");
}

function cardQuery(
  source: ReturnType<typeof useSource>,
  space: SpaceSummary,
  item: ReviewItem,
) {
  return {
    queryKey: recordKeys.card(
      source.kind,
      space.slug,
      item.ref,
      item.version,
      cardFacet(item),
    ),
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
      item ? cardFacet(item) : "",
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

/** How often a propagating tombstone is read again (it's done within the minute). */
const TOMBSTONE_POLL_MS = 2000;

/** A forgotten memory's tombstone, read again while its Forget propagates. */
export function useTombstone(space: SpaceSummary, ref: string) {
  const source = useSource();
  const peek = source.memories.peekTombstone?.(space.slug, ref);
  return useQuery({
    queryKey: recordKeys.tombstone(source.kind, space.slug, ref),
    queryFn: ({ signal }) => source.memories.tombstone({ space, ref, signal }),
    initialData: peek,
    refetchInterval: (query) =>
      query.state.data?.status === "propagating" ? TOMBSTONE_POLL_MS : false,
  });
}

/** What a Forget would do, read when the person opens the confirmation. */
export function useForgetPreview(
  space: SpaceSummary,
  ref: string,
  version: number,
  enabled: boolean,
) {
  const source = useSource();
  return useQuery({
    queryKey: recordKeys.forgetPreview(source.kind, space.slug, ref, version),
    queryFn: ({ signal }) =>
      source.memories.previewForget({ space, ref, signal }),
    enabled,
    staleTime: 0,
    gcTime: 0,
  });
}

/** A queue row as it's cached now, and where it sits: what Undo puts back. */
export function useQueueSnapshot(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    (ref: string): { item: ReviewItem; index: number } | null => {
      const data = queryClient.getQueryData<
        InfiniteData<ReviewQueue, string | undefined>
      >(recordKeys.queue(source.kind, space.slug));
      const items = data?.pages.flatMap((p) => p.items) ?? [];
      const index = items.findIndex((i) => i.ref === ref);
      return index < 0 ? null : { item: items[index], index };
    },
    [queryClient, source.kind, space.slug],
  );
}

/** Puts one memory's current row into the cached queue (a verdict that landed while deciding). */
export function usePatchQueueItem(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    (item: ReviewItem) => {
      queryClient.setQueryData<InfiniteData<ReviewQueue, string | undefined>>(
        recordKeys.queue(source.kind, space.slug),
        (data) =>
          data && {
            ...data,
            pages: data.pages.map((page) => ({
              ...page,
              items: page.items.map((i) => (i.ref === item.ref ? item : i)),
            })),
          },
      );
    },
    [queryClient, source.kind, space.slug],
  );
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
