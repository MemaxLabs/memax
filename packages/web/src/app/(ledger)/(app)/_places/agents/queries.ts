"use client";

import { useQuery, type QueryClient } from "@tanstack/react-query";
import type {
  AgentConnectionView,
  AgentDetailView,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "../../_lib/data";

// The agents' queries, keyed by source like the frame's own
// (_lib/data.tsx), with the demo's data on hand for the first render.

export const agentKeys = {
  all: (kind: string) => ["v2", kind, "agents"] as const,
  space: (kind: string, slug: string) =>
    ["v2", kind, "agents", "space", slug] as const,
  mine: (kind: string) => ["v2", kind, "agents", "mine"] as const,
  detail: (kind: string, id: string) =>
    ["v2", kind, "agents", "detail", id] as const,
  keys: (kind: string) => ["v2", kind, "agents", "keys"] as const,
};

export function useSpaceAgents(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: agentKeys.space(source.kind, space.slug),
    queryFn: ({ signal }) => source.spaceAgents(space, signal),
    initialData: source.agentsPeek?.spaceAgents(space.slug),
    staleTime: 30_000,
  });
}

export function useMyAgents() {
  const source = useSource();
  return useQuery({
    queryKey: agentKeys.mine(source.kind),
    queryFn: ({ signal }) => source.myAgents(signal),
    initialData: source.agentsPeek?.myAgents(),
    staleTime: 30_000,
  });
}

export function useAgent(id: string) {
  const source = useSource();
  return useQuery({
    queryKey: agentKeys.detail(source.kind, id),
    queryFn: ({ signal }) => source.agent(id, signal),
    initialData: source.agentsPeek?.agent(id),
    staleTime: 30_000,
  });
}

export function useApiKeys() {
  const source = useSource();
  return useQuery({
    queryKey: agentKeys.keys(source.kind),
    queryFn: ({ signal }) => source.apiKeys(signal),
    initialData: source.agentsPeek?.apiKeys(),
    staleTime: 30_000,
  });
}

/**
 * Puts a connection the server (or an optimistic change) returned into
 * every cached list and page, so the Agents table, the agent's page and
 * Settings agree at once. A disconnected agent leaves the lists.
 */
export function patchAgent(
  queryClient: QueryClient,
  kind: string,
  next: AgentConnectionView,
) {
  const gone = next.state === "disconnected";
  queryClient.setQueriesData<AgentConnectionView[]>(
    {
      predicate: (q) => {
        const [v2, k, agents, scope] = q.queryKey;
        return (
          v2 === "v2" &&
          k === kind &&
          agents === "agents" &&
          (scope === "space" || scope === "mine")
        );
      },
    },
    (list) =>
      list && Array.isArray(list)
        ? gone
          ? list.filter((a) => a.id !== next.id)
          : list.map((a) => (a.id === next.id ? next : a))
        : list,
  );
  queryClient.setQueryData<AgentDetailView | null>(
    agentKeys.detail(kind, next.id),
    (detail) => (detail ? { ...detail, connection: next } : detail),
  );
}

/** After a command: what else it changed (the space's counts, its receipts). */
export function refreshAfterAgentCommand(
  queryClient: QueryClient,
  kind: string,
) {
  void queryClient.invalidateQueries({ queryKey: ["v2", kind, "spaces"] });
  void queryClient.invalidateQueries({ queryKey: ["v2", kind, "activity"] });
}
