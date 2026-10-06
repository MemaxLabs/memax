"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import type { ActivityPage } from "@/lib/v2/data/activity";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "../../_lib/data";

export const activityKeys = {
  space: (kind: string, slug: string) =>
    ["v2", kind, "activity", slug] as const,
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
