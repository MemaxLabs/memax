"use client";

import { useQuery } from "@tanstack/react-query";
import type { PlanDefinition } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

export const plansQueryKey = ["plans"] as const;

/** Fetches all active plan definitions. Public endpoint, cached aggressively. */
export function usePlans() {
  return useQuery<PlanDefinition[]>({
    queryKey: plansQueryKey,
    queryFn: () => getMemaxClient().settings.plans(),
    staleTime: 5 * 60 * 1000, // 5 minutes — plans change rarely
  });
}
