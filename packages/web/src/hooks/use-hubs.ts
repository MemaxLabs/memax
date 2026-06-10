"use client";

import { useQuery } from "@tanstack/react-query";
import type { HubWithRole } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

export const hubListQueryKey = ["hubs"] as const;

export function useHubs() {
  return useQuery<HubWithRole[]>({
    queryKey: hubListQueryKey,
    queryFn: () => getMemaxClient().hubs.list(),
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}
