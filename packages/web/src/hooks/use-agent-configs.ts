"use client";

import { useQuery } from "@tanstack/react-query";
import type { AgentConfigListResult } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

/**
 * Read-only list of agent configs. Delete operations live in
 * `useAgentConfigForget` and route through the SDK's
 * `configs.batchDelete(ids)` method with structured partial-success
 * semantics. Both single-row and "forget all files" paths converge on
 * that one SDK call, the same way `useMemoryForget` routes every
 * memory delete through `memories.batchDelete`.
 */
export function useAgentConfigs() {
  return useQuery<AgentConfigListResult>({
    queryKey: ["agent-configs"],
    queryFn: async () => {
      const res = await getMemaxClient().configs.list();
      return {
        configs: res.configs,
        extraction_counts: res.extraction_counts ?? {},
      };
    },
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}
