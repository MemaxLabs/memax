"use client";

import { useQuery } from "@tanstack/react-query";
import type { Usage, UsageWithLimits } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

export const usageQueryKey = ["usage"] as const;

/**
 * Returns usage data for the current billing period. When the plan registry
 * is active, the response includes plan limits (UsageWithLimits). The flat
 * Usage fields are always present for backward compatibility.
 */
export function useUsage() {
  return useQuery<UsageWithLimits | Usage>({
    queryKey: usageQueryKey,
    queryFn: () => getMemaxClient().settings.usage(),
    staleTime: 60 * 1000,
  });
}

/** Type guard: checks if usage data includes plan limits. */
export function hasLimits(
  usage: UsageWithLimits | Usage | undefined,
): usage is UsageWithLimits {
  return !!usage && "limits" in usage;
}
