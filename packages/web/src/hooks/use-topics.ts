"use client";

import { useQuery, useMutation } from "@tanstack/react-query";
import type {
  Topic,
  TopicListResponse,
  TopicMemoriesResponse,
  TopicTree,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { hubListQueryKey } from "./use-hubs";
import {
  memoryListQueryPrefix,
  recentMemoriesQueryPrefix,
} from "./use-memories";

export type { Topic, TopicTree, TopicListResponse };

export const topicQueryKey = (
  id: string,
): readonly ["topics", "detail", string] => ["topics", "detail", id];

export const topicMemoriesQueryKey = (
  topicId: string,
): readonly ["topics", string, "memories"] => ["topics", topicId, "memories"];

// --- Queries ---

export function getTopicsQueryOptions(hubId?: string) {
  return {
    queryKey: ["topics", hubId ?? "all"] as const,
    queryFn: () => getMemaxClient().topics.list(hubId),
    staleTime: 30 * 1000,
  } as const;
}

export function getTopicQueryOptions(id: string, hubId?: string) {
  return {
    queryKey: topicQueryKey(id),
    queryFn: () => getMemaxClient().topics.get(id, hubId),
    enabled: !!id,
    staleTime: 30 * 1000,
  } as const;
}

export function getTopicMemoriesQueryOptions(
  topicId: string,
  limit = 20,
  hubId?: string,
) {
  return {
    queryKey: topicMemoriesQueryKey(topicId),
    queryFn: () =>
      getMemaxClient().topics.listMemories(topicId, {
        limit,
        hubId,
      }),
    enabled: !!topicId,
    staleTime: 30 * 1000,
  } as const;
}

export function useTopics(hubId?: string) {
  const { activeHubId } = useAuth();
  const resolvedHubId = (hubId ?? activeHubId) || undefined;
  return useQuery<TopicListResponse>({
    ...getTopicsQueryOptions(resolvedHubId),
  });
}

export function useTopic(id: string) {
  const { activeHubId } = useAuth();
  return useQuery<Topic>({
    ...getTopicQueryOptions(id, activeHubId || undefined),
  });
}

export function useTopicMemories(topicId: string, limit = 20) {
  const { activeHubId } = useAuth();
  return useQuery<TopicMemoriesResponse>({
    ...getTopicMemoriesQueryOptions(topicId, limit, activeHubId || undefined),
  });
}

// --- Mutations ---

export function useCreateTopic() {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  return useMutation({
    meta: {
      errorMessage: t.toast.topicFailed,
      errorAction: t.errors.action.createTopic,
    },
    mutationFn: (params: {
      name: string;
      description?: string;
      icon?: string;
      parent_id?: string;
    }) => getMemaxClient().topics.create(params, activeHubId || undefined),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

export function useUpdateTopic() {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  return useMutation({
    meta: {
      errorMessage: t.toast.topicFailed,
      errorAction: t.errors.action.updateTopic,
    },
    mutationFn: ({
      id,
      ...params
    }: {
      id: string;
      name?: string;
      description?: string;
      icon?: string;
      position?: number;
      pinned?: boolean;
    }) => getMemaxClient().topics.update(id, params, activeHubId || undefined),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

export function useArchivedTopics(enabled = true) {
  const { activeHubId } = useAuth();
  const hubId = activeHubId || undefined;
  return useQuery({
    queryKey: ["topics", "archived", hubId ?? "all"] as const,
    queryFn: () => getMemaxClient().topics.listArchived(hubId),
    staleTime: 30 * 1000,
    enabled,
  });
}

export function useArchiveTopic() {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  return useMutation({
    meta: {
      errorMessage: t.toast.topicFailed,
      errorAction: t.errors.action.archiveTopic,
    },
    mutationFn: (id: string) =>
      getMemaxClient().topics.archive(id, activeHubId || undefined),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

export function useRestoreTopic() {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  return useMutation({
    meta: {
      errorMessage: t.toast.topicFailed,
      errorAction: t.errors.action.restoreTopic,
    },
    mutationFn: (id: string) =>
      getMemaxClient().topics.restore(id, activeHubId || undefined),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

export function useDeleteTopic() {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  return useMutation({
    meta: {
      errorMessage: t.toast.topicFailed,
      errorAction: t.errors.action.deleteTopic,
    },
    mutationFn: (id: string) =>
      getMemaxClient().topics.delete(id, activeHubId || undefined),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

/**
 * useMarkTopicVisit — records a topic visit, anchoring the clear-on-visit
 * semantics for scan-surface dream-delta signals.
 *
 * STRICT fire contract: this hook must fire ONLY on real topic page mount,
 * never from prefetch, hover, or memory detail. Callers gate on
 * !isPlaceholderData from React Query and a 300ms dwell timer cleared on
 * unmount — if the user bounces off the page faster than 300ms, the
 * visit does not register. Matches the inbox seen-on-expand intent
 * semantics.
 *
 * On success, invalidates topic + memory query keys so the next read
 * resolves lifecycle signals against the updated visit timestamp.
 */
export function useMarkTopicVisit() {
  return useMutation({
    mutationFn: (topicId: string) => getMemaxClient().topics.markVisit(topicId),
    onSuccess: (_data, topicId) => {
      // Every surface that renders memory.lifecycle or
      // topic.lifecycle must be invalidated so the next read resolves
      // against the updated topic_visits.last_visited_at:
      //   - ["topics"]                 — TopicGrid + topic tree (delta chip)
      //   - ["memory-lists"]           — paginated memories list pages
      //   - ["recent-memories"]        — Recent feed
      //   - ["topics", id, "memories"] — topic-detail memory list
      //     (useTopicMemories → topicMemoriesQueryKey)
      //   - ["memories", id]           — memory detail pages cached
      //     after this visit also need a refetch so pending_dream_action
      //     nulls; invalidating the whole ["memories"] prefix catches
      //     both the list single-memory cache and the detail cache.
      queryClient.invalidateQueries({ queryKey: ["topics"] });
      queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
      queryClient.invalidateQueries({ queryKey: recentMemoriesQueryPrefix });
      queryClient.invalidateQueries({
        queryKey: topicMemoriesQueryKey(topicId),
      });
      queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
  });
}

// Memory↔topic assignment hooks intentionally removed from the web client.
// User-initiated moves (picker, drag/drop, batch, detail route) route through
// memories.batchMove via useMemoryMove — that's the single authoritative move
// contract. topics.assignMemory / topics.unassignMemory remain in the SDK
// for the backend ingest + dreams workers and are confidence-gated by design.
