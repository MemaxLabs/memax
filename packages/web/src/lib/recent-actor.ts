import { getAttributionAgentSlug } from "@/lib/memory-attribution";

export type RecentActorValue = "self" | `agent:${string}` | `author:${string}`;

interface RecentActorMemory {
  author_name?: string | null;
  source_agent?: string | null;
  provenance?: {
    created_by_slug?: string | null;
  } | null;
}

export const getMemoryAgentSlug = getAttributionAgentSlug;

export function recentActorForMemory(
  memory: RecentActorMemory,
  options?: { hubType?: string | null },
): RecentActorValue {
  if (options?.hubType === "team" && memory.author_name) {
    return `author:${memory.author_name}`;
  }

  const agentSlug = getMemoryAgentSlug(memory);
  if (agentSlug) {
    return `agent:${agentSlug}`;
  }

  if (memory.author_name) {
    return `author:${memory.author_name}`;
  }

  return "self";
}

export function memoryMatchesRecentActor(
  memory: RecentActorMemory,
  actor: "all" | RecentActorValue,
  options?: { hubType?: string | null },
): boolean {
  if (actor === "all") return true;
  return recentActorForMemory(memory, options) === actor;
}
