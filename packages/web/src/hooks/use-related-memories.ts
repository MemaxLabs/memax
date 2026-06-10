"use client";

import { useQuery } from "@tanstack/react-query";
import type { RelatedMemory } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

/**
 * Fetches semantically-related memories. Gated on memoryState so that
 * processing memories (which have no chunks yet) don't fire a network
 * request that the server will short-circuit to []. Without this gate
 * every detail-page mount during processing would round-trip needlessly.
 */
export function useRelatedMemories(memoryId: string, memoryState?: string) {
  return useQuery<RelatedMemory[]>({
    queryKey: ["memories", memoryId, "related"],
    queryFn: ({ signal }) =>
      getMemaxClient().memories.related(memoryId, { signal }),
    enabled: !!memoryId && memoryState !== "processing",
    staleTime: 2 * 60 * 1000,
  });
}
