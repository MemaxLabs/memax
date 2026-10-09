"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { ActivityPage, ReadsPage } from "@/lib/v2/data/activity";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "../../_lib/data";

// Every key starts with the space's: invalidating ["v2", kind,
// "activity", slug] (an undo, an agent command) refreshes all three.
export const activityKeys = {
  space: (kind: string, slug: string) =>
    ["v2", kind, "activity", slug] as const,
  reads: (kind: string, slug: string) =>
    ["v2", kind, "activity", slug, "reads"] as const,
  seal: (kind: string, slug: string) =>
    ["v2", kind, "activity", slug, "seal"] as const,
};

/** The space's receipts, newest first, a page at a time (cursor pagination). */
export function useActivity(space: SpaceSummary) {
  const source = useSource();
  const first = source.activityPeek?.(space.slug);
  return useInfiniteQuery({
    queryKey: activityKeys.space(source.kind, space.slug),
    queryFn: ({ pageParam, signal }) =>
      source.activity({ space, cursor: pageParam, signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: ActivityPage) => last.nextCursor ?? undefined,
    initialData: first
      ? { pages: [first], pageParams: [undefined] }
      : undefined,
    staleTime: 15_000,
  });
}

/** The space's reads (R-), newest first, a page at a time, with the week's count. */
export function useReads(space: SpaceSummary) {
  const source = useSource();
  const first = source.readsPeek?.(space.slug);
  return useInfiniteQuery({
    queryKey: activityKeys.reads(source.kind, space.slug),
    queryFn: ({ pageParam, signal }) =>
      source.reads({ space, cursor: pageParam, signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: ReadsPage) => last.nextCursor ?? undefined,
    initialData: first
      ? { pages: [first], pageParams: [undefined] }
      : undefined,
    staleTime: 15_000,
  });
}

/** How far the space's receipts are sealed; the sealer runs every 15 seconds. */
export function useSeal(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: activityKeys.seal(source.kind, space.slug),
    queryFn: ({ signal }) => source.seal({ space, signal }),
    initialData: source.sealPeek?.(space.slug),
    staleTime: 15_000,
  });
}
