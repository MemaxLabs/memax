"use client";

/**
 * useMemoryMove — single authoritative user-move pipeline.
 *
 * React Query cache rule (load-bearing): `queryClient.getQueriesData({ queryKey: PREFIX })`
 * returns every cached query whose key starts with PREFIX — which, under shared
 * prefixes like `["topics"]`, includes heterogeneous shapes:
 *   - ["topics", hubId]          → TopicListResponse  { topics: TopicTree[] }
 *   - ["topics", "detail", id]   → Topic
 *   - ["topics", topicId, "memories"] → TopicMemoriesResponse { memories: Memory[] }
 *
 * Every walk over these results MUST filter entries by an explicit query-key
 * shape guard (e.g. `isTopicMemoriesQueryKey`) BEFORE accessing inner fields.
 * Optional chaining alone (`data?.memories`) is NOT enough: `data` can be a
 * differently-shaped but defined object from a sibling query, so the inner
 * field access crashes with `undefined.forEach` / similar. When in doubt,
 * filter + double-optional-chain (`data?.memories?.forEach`).
 */

import { useCallback, useRef } from "react";
import { useMutation, type InfiniteData } from "@tanstack/react-query";
import { MemaxError } from "memax-sdk";
import type {
  BatchMoveResult,
  BatchMoveSkippedMemory,
  HubWithRole,
  Memory,
  TopicMemoriesResponse,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import {
  memoryDetailQueryKey,
  memoryListQueryPrefix,
  type MemoriesListResponse,
  type MemorySort,
} from "./use-memories";
import { hubListQueryKey } from "./use-hubs";
import { recentMemoriesQueryKey, type TimeWindow } from "./use-recent-memories";
import { useBarToast } from "./use-bar-toast";
import { pluralize, useInterpolate, useLocale } from "@/i18n";
import { classifyMutationError } from "@/lib/error-copy";
import { recentActorForMemory } from "@/lib/recent-actor";

const RECENT_QUERY_PREFIX = ["recent-memories"] as const;
const TOPIC_QUERY_PREFIX = ["topics"] as const;
const WINDOW_MS: Record<TimeWindow, number> = {
  "12h": 12 * 3600_000,
  "1d": 24 * 3600_000,
  "3d": 3 * 24 * 3600_000,
  "7d": 7 * 24 * 3600_000,
};

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export interface MemoryMoveSnapshot {
  id: string;
  hubId: string;
  topicId?: string;
}

export interface MemoryMoveTarget {
  hubId?: string;
  topicId?: string;
}

interface MoveCacheContext {
  memoryDetails: Array<[readonly unknown[], Memory | undefined]>;
  memoryLists: Array<
    [readonly unknown[], InfiniteData<MemoriesListResponse> | undefined]
  >;
  recentLists: Array<
    [readonly unknown[], InfiniteData<MemoriesListResponse> | undefined]
  >;
  topicLists: Array<[readonly unknown[], TopicMemoriesResponse | undefined]>;
}

function groupByDestination(snapshots: MemoryMoveSnapshot[]) {
  const groups = new Map<string, { target: MemoryMoveTarget; ids: string[] }>();
  for (const snapshot of snapshots) {
    const target = { hubId: snapshot.hubId, topicId: snapshot.topicId };
    const key = `${target.hubId}::${target.topicId ?? ""}`;
    const existing = groups.get(key);
    if (existing) {
      existing.ids.push(snapshot.id);
      continue;
    }
    groups.set(key, { target, ids: [snapshot.id] });
  }
  return groups;
}

function normalizeTopicId(topicId: string | undefined) {
  return topicId && topicId.length > 0 ? topicId : undefined;
}

function isSnapshotAlreadyAtTarget(
  snapshot: MemoryMoveSnapshot,
  target: MemoryMoveTarget,
) {
  const targetHubId = target.hubId ?? snapshot.hubId;
  const sourceTopicId = normalizeTopicId(snapshot.topicId);
  const targetTopicId = normalizeTopicId(target.topicId);
  return snapshot.hubId === targetHubId && sourceTopicId === targetTopicId;
}

function isServerMovableSnapshot(snapshot: MemoryMoveSnapshot) {
  return UUID_PATTERN.test(snapshot.id);
}

function isRecentQueryKey(
  value: readonly unknown[],
): value is readonly [
  "recent-memories",
  string,
  TimeWindow,
  string,
  "preview" | "full",
] {
  return (
    value[0] === "recent-memories" &&
    typeof value[1] === "string" &&
    (value[2] === "12h" ||
      value[2] === "1d" ||
      value[2] === "3d" ||
      value[2] === "7d") &&
    typeof value[3] === "string" &&
    (value[4] === "preview" || value[4] === "full")
  );
}

function isTopicMemoriesQueryKey(
  value: readonly unknown[],
): value is readonly ["topics", string, "memories"] {
  return (
    value[0] === "topics" &&
    typeof value[1] === "string" &&
    value[2] === "memories"
  );
}

function isWithinRecentWindow(memory: Memory, window: TimeWindow) {
  return (
    Date.now() - new Date(memory.created_at).getTime() <= WINDOW_MS[window]
  );
}

function getHubType(hubs: HubWithRole[] | undefined, hubId: string) {
  return hubs?.find((entry) => entry.hub.id === hubId)?.hub.hub_type;
}

function matchesRecentActor(memory: Memory, actor: string, hubType?: string) {
  if (actor === "all") return true;
  return (
    recentActorForMemory(memory, { hubType }) ===
    (actor as "self" | `agent:${string}` | `author:${string}`)
  );
}

function compareMemories(a: Memory, b: Memory, sort: MemorySort) {
  if (sort === "recalled") {
    const accessDiff = (b.access_count || 0) - (a.access_count || 0);
    if (accessDiff !== 0) return accessDiff;
  }
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
}

function patchInfiniteDataForHub(
  data: InfiniteData<MemoriesListResponse> | undefined,
  memory: Memory,
  sourceHubId: string,
  targetHubId: string,
  sort: MemorySort,
) {
  if (!data?.pages?.length) return data;

  let removed = 0;
  let foundExisting = false;
  const pages = data.pages.map((page) => {
    // Track per-page whether we touched a memory. Length equality is NOT a
    // valid proxy for "unchanged" — a same-hub in-place replacement
    // preserves page length (we swap one memory object for another), so
    // comparing `nextMemories.length === page.memories.length` silently
    // discards the patched memory and returns the original page reference.
    // That manifested as stale row chips after a move: detail cache got
    // the new topic_id, but the grid/inbox kept showing the old one until
    // the server refetch landed ~hundreds of ms later.
    let pageChanged = false;
    const nextMemories = page.memories.flatMap((item) => {
      if (item.id !== memory.id) return [item];
      foundExisting = true;
      pageChanged = true;
      if (targetHubId !== sourceHubId) {
        removed += 1;
        return [];
      }
      return [memory];
    });
    return pageChanged ? { ...page, memories: nextMemories } : page;
  });

  if (targetHubId !== sourceHubId) {
    if (removed === 0) return data;
    const first = pages[0];
    return {
      ...data,
      pages: pages.map((page, index) =>
        index === 0
          ? {
              ...page,
              total: Math.max(
                0,
                (first.total ?? first.memories.length) - removed,
              ),
            }
          : page,
      ),
    };
  }

  if (foundExisting) {
    return { ...data, pages };
  }

  const first = pages[0];
  if (!first) return data;
  const nextFirstMemories = [...first.memories, memory].sort((a, b) =>
    compareMemories(a, b, sort),
  );
  return {
    ...data,
    pages: [
      {
        ...first,
        memories: nextFirstMemories,
        total: first.total ?? nextFirstMemories.length,
      },
      ...pages.slice(1),
    ],
  };
}

function patchRecentDataForHub(
  data: InfiniteData<MemoriesListResponse> | undefined,
  memory: Memory,
  sourceHubId: string,
  targetHubId: string,
  mode: "preview" | "full",
  shouldInclude: boolean,
) {
  if (!data?.pages?.length) return data;

  let removed = 0;
  let foundExisting = false;
  const pages = data.pages.map((page) => {
    // Same per-page-changed invariant as patchInfiniteDataForHub — length
    // equality is not "unchanged" when we swap a memory in place. See the
    // longer comment on that helper for the stale-row-chip symptom.
    let pageChanged = false;
    const nextMemories = page.memories.flatMap((item) => {
      if (item.id !== memory.id) return [item];
      foundExisting = true;
      pageChanged = true;
      if (!shouldInclude) {
        removed += 1;
        return [];
      }
      return [memory];
    });
    return pageChanged ? { ...page, memories: nextMemories } : page;
  });

  if (!shouldInclude) {
    if (removed === 0) return data;
    const first = pages[0];
    return {
      ...data,
      pages: pages.map((page, index) =>
        index === 0
          ? {
              ...page,
              total: Math.max(
                0,
                (first.total ?? first.memories.length) - removed,
              ),
            }
          : page,
      ),
    };
  }

  if (foundExisting) {
    return { ...data, pages };
  }

  if (targetHubId !== memory.hub_id || sourceHubId === targetHubId) {
    return data;
  }

  const first = pages[0];
  if (!first) return data;
  const nextFirstMemories = [...first.memories, memory].sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
  return {
    ...data,
    pages: [
      {
        ...first,
        memories:
          mode === "preview"
            ? nextFirstMemories.slice(0, 5)
            : nextFirstMemories,
        total: (first.total ?? first.memories.length) + 1,
      },
      ...pages.slice(1),
    ],
  };
}

function patchTopicMemoriesData(
  data: TopicMemoriesResponse | undefined,
  memory: Memory,
  targetTopicId?: string,
  queryTopicId?: string,
) {
  if (!data?.memories) return data;
  const withoutMemory = data.memories.filter((item) => item.id !== memory.id);
  if (!targetTopicId || queryTopicId !== targetTopicId) {
    return withoutMemory.length === data.memories.length
      ? data
      : { ...data, memories: withoutMemory };
  }

  const nextMemories = [memory, ...withoutMemory].sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
  return { ...data, memories: nextMemories };
}

function buildOptimisticMemory(
  memory: Memory,
  target: MemoryMoveTarget,
  nowIso: string,
  hubs: HubWithRole[] | undefined,
) {
  const nextHubId = target.hubId ?? memory.hub_id;
  const nextHub = hubs?.find((entry) => entry.hub.id === nextHubId)?.hub;
  return {
    ...memory,
    hub_id: nextHubId,
    hub_name: nextHub?.name ?? memory.hub_name,
    topic_id: target.topicId ?? "",
    updated_at: nowIso,
  };
}

function snapshotMoveCaches(ids: string[]) {
  return {
    memoryDetails: ids.map((id) => [
      memoryDetailQueryKey(id),
      queryClient.getQueryData<Memory>(memoryDetailQueryKey(id)),
    ]),
    memoryLists: queryClient.getQueriesData<InfiniteData<MemoriesListResponse>>(
      {
        queryKey: memoryListQueryPrefix,
      },
    ),
    recentLists: queryClient.getQueriesData<InfiniteData<MemoriesListResponse>>(
      {
        queryKey: RECENT_QUERY_PREFIX,
      },
    ),
    topicLists: queryClient
      .getQueriesData<TopicMemoriesResponse>({ queryKey: TOPIC_QUERY_PREFIX })
      .filter(
        ([queryKey]) =>
          Array.isArray(queryKey) && isTopicMemoriesQueryKey(queryKey),
      ),
  } satisfies MoveCacheContext;
}

function restoreMoveCaches(context: MoveCacheContext | undefined) {
  context?.memoryDetails?.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
  context?.memoryLists?.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
  context?.recentLists?.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
  context?.topicLists?.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
}

function collectKnownMemories(ids: string[]) {
  const map = new Map<string, Memory>();

  ids.forEach((id) => {
    const detail = queryClient.getQueryData<Memory>(memoryDetailQueryKey(id));
    if (detail) {
      map.set(id, detail);
    }
  });

  queryClient
    .getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: memoryListQueryPrefix,
    })
    .forEach(([, data]) => {
      data?.pages.forEach((page) => {
        page.memories.forEach((memory) => {
          if (ids.includes(memory.id) && !map.has(memory.id)) {
            map.set(memory.id, memory);
          }
        });
      });
    });

  queryClient
    .getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: RECENT_QUERY_PREFIX,
    })
    .forEach(([, data]) => {
      data?.pages.forEach((page) => {
        page.memories.forEach((memory) => {
          if (ids.includes(memory.id) && !map.has(memory.id)) {
            map.set(memory.id, memory);
          }
        });
      });
    });

  queryClient
    .getQueriesData<TopicMemoriesResponse>({ queryKey: TOPIC_QUERY_PREFIX })
    .filter(
      ([queryKey]) =>
        Array.isArray(queryKey) && isTopicMemoriesQueryKey(queryKey),
    )
    .forEach(([, data]) => {
      data?.memories?.forEach((memory) => {
        if (ids.includes(memory.id) && !map.has(memory.id)) {
          map.set(memory.id, memory);
        }
      });
    });

  return map;
}

const EMPTY_BATCH_RESULT: BatchMoveResult = { moved: 0, skipped: [] };

/**
 * Sentinel thrown by applyMove when every expected move was skipped
 * by the server. Carries the full `skipped` array so the outer catch
 * in moveWithUndo can inspect per-id reasons and pick reason-specific
 * toast copy (e.g. all source_delete_forbidden → moveSourceDenied
 * instead of the generic noWriteAccess fallback).
 *
 * Extending Error preserves React Query's onError lifecycle — the
 * mutation is still treated as a failure, so the optimistic snapshot
 * rollback in onError still fires. Using a dedicated class (rather
 * than string-matching `error.message`) lets the catch-ladder
 * type-guard cleanly and carries the structured payload inline.
 */
class MemoryMoveIncompleteError extends Error {
  constructor(public readonly skipped: BatchMoveSkippedMemory[]) {
    super("memory_move_incomplete");
    this.name = "MemoryMoveIncompleteError";
  }
}

async function applyMove(
  snapshots: MemoryMoveSnapshot[],
  target: MemoryMoveTarget,
  force = false,
): Promise<BatchMoveResult> {
  if (snapshots.length === 0) return EMPTY_BATCH_RESULT;

  const client = getMemaxClient();
  const targetHubId = target.hubId ?? snapshots[0]?.hubId;
  if (!targetHubId) return EMPTY_BATCH_RESULT;
  // Undo replays the mutation with the original hub/topic as the target — and
  // because the snapshots carry that same original state, the default
  // isSnapshotAlreadyAtTarget short-circuit would no-op the undo entirely.
  // Callers that know they want to force a server roundtrip (undo) pass
  // force=true to bypass the client-side optimization.
  const expectedMoves = force
    ? snapshots.length
    : snapshots.filter(
        (snapshot) => !isSnapshotAlreadyAtTarget(snapshot, target),
      ).length;
  if (expectedMoves === 0) {
    return EMPTY_BATCH_RESULT;
  }
  const result = await client.memories.batchMove(
    snapshots.map((snapshot) => snapshot.id),
    {
      hubId: targetHubId,
      topicId: target.topicId,
    },
  );
  // Batch move can be partial (e.g. mixed selection that includes memories
  // the current user cannot move). Treat "some moved" as success and rely on
  // cache invalidation to reconcile exact server state.
  if (expectedMoves > 0 && result.moved === 0) {
    // Throw the structured sentinel so moveWithUndo's catch branch
    // can inspect result.skipped and pick reason-specific toast copy.
    // React Query still sees a thrown error → onError fires → the
    // optimistic snapshot from onMutate is restored, same as before.
    throw new MemoryMoveIncompleteError(result.skipped);
  }
  return result;
}

function invalidateMoveCaches(ids: string[]) {
  queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
  queryClient.invalidateQueries({
    queryKey: recentMemoriesQueryKey(undefined, "12h", "all", "preview").slice(
      0,
      1,
    ),
  });
  queryClient.invalidateQueries({ queryKey: ["topics"] });
  queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
  queryClient.invalidateQueries({ queryKey: hubListQueryKey });
  queryClient.invalidateQueries({ queryKey: ["recall"] });
  queryClient.invalidateQueries({ queryKey: ["memory-search"] });
  ids.forEach((id) => {
    queryClient.invalidateQueries({ queryKey: memoryDetailQueryKey(id) });
  });
}

function applyOptimisticMove(
  snapshots: MemoryMoveSnapshot[],
  target: MemoryMoveTarget,
) {
  const ids = snapshots.map((snapshot) => snapshot.id);
  const knownMemories = collectKnownMemories(ids);
  const hubs = queryClient.getQueryData<HubWithRole[]>(hubListQueryKey);
  const nowIso = new Date().toISOString();
  const optimisticById = new Map<string, Memory>();

  snapshots.forEach((snapshot) => {
    const base = knownMemories.get(snapshot.id);
    if (!base) return;
    optimisticById.set(
      snapshot.id,
      buildOptimisticMemory(base, target, nowIso, hubs),
    );
  });

  optimisticById.forEach((memory, id) => {
    queryClient.setQueryData(memoryDetailQueryKey(id), memory);
  });

  queryClient
    .getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: memoryListQueryPrefix,
    })
    .forEach(([queryKey, data]) => {
      if (!Array.isArray(queryKey)) return;
      const [, hubId, sort] = queryKey as ["memory-lists", string, MemorySort];
      let next = data;
      snapshots.forEach((snapshot) => {
        const memory = optimisticById.get(snapshot.id);
        if (!memory) return;
        const sourceHubId = snapshot.hubId || "all";
        const targetHubId = memory.hub_id || "all";
        const relevantHubId = hubId === "all" ? targetHubId : hubId;
        if (relevantHubId !== sourceHubId && relevantHubId !== targetHubId)
          return;
        next = patchInfiniteDataForHub(
          next,
          memory,
          sourceHubId,
          targetHubId,
          sort,
        );
      });
      if (next && next !== data) {
        queryClient.setQueryData(queryKey, next);
      }
    });

  queryClient
    .getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: RECENT_QUERY_PREFIX,
    })
    .forEach(([queryKey, data]) => {
      if (!Array.isArray(queryKey) || !isRecentQueryKey(queryKey)) return;
      const [, hubId, window, actor, mode] = queryKey;
      let next = data;
      snapshots.forEach((snapshot) => {
        const memory = optimisticById.get(snapshot.id);
        if (!memory) return;
        const sourceHubType = getHubType(hubs, snapshot.hubId);
        const targetHubType = getHubType(hubs, memory.hub_id);
        const shouldInclude =
          hubId === memory.hub_id &&
          isWithinRecentWindow(memory, window) &&
          matchesRecentActor(memory, actor, targetHubType);
        const sourceMatches =
          hubId === snapshot.hubId &&
          isWithinRecentWindow(memory, window) &&
          matchesRecentActor(
            {
              ...memory,
              hub_id: snapshot.hubId,
              author_name: memory.author_name,
            },
            actor,
            sourceHubType,
          );
        if (!shouldInclude && !sourceMatches) return;
        next = patchRecentDataForHub(
          next,
          memory,
          snapshot.hubId,
          memory.hub_id,
          mode,
          shouldInclude,
        );
      });
      if (next && next !== data) {
        queryClient.setQueryData(queryKey, next);
      }
    });

  queryClient
    .getQueriesData<TopicMemoriesResponse>({ queryKey: TOPIC_QUERY_PREFIX })
    .forEach(([queryKey, data]) => {
      if (!Array.isArray(queryKey) || !isTopicMemoriesQueryKey(queryKey))
        return;
      let next = data;
      snapshots.forEach((snapshot) => {
        const memory = optimisticById.get(snapshot.id);
        if (!memory) return;
        next = patchTopicMemoriesData(
          next,
          memory,
          target.topicId,
          queryKey[1],
        );
      });
      if (next && next !== data) {
        queryClient.setQueryData(queryKey, next);
      }
    });
}

interface MoveMutationVariables {
  snapshots: MemoryMoveSnapshot[];
  target: MemoryMoveTarget;
  /** Undo sets this to bypass the client-side already-at-target optimization. */
  force?: boolean;
}

export function useMemoryMove() {
  const toast = useBarToast();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  // React Query owns the lifecycle: optimistic apply in onMutate, rollback in
  // onError, and ALWAYS invalidate in onSettled — defense against partial
  // moves where the server committed some subset the optimistic patch missed.
  const mutation = useMutation<
    BatchMoveResult,
    Error,
    MoveMutationVariables,
    MoveCacheContext
  >({
    mutationFn: ({ snapshots, target, force }) =>
      applyMove(snapshots, target, force),
    onMutate: async ({ snapshots, target }) => {
      const ids = snapshots.map((snapshot) => snapshot.id);
      // Cancel in-flight fetches first so they don't clobber optimistic state.
      await Promise.all([
        queryClient.cancelQueries({ queryKey: memoryListQueryPrefix }),
        queryClient.cancelQueries({ queryKey: RECENT_QUERY_PREFIX }),
        queryClient.cancelQueries({ queryKey: TOPIC_QUERY_PREFIX }),
        ...ids.map((id) =>
          queryClient.cancelQueries({ queryKey: memoryDetailQueryKey(id) }),
        ),
      ]);
      const context = snapshotMoveCaches(ids);
      applyOptimisticMove(snapshots, target);
      return context;
    },
    onError: (_error, _variables, context) => {
      if (context) {
        restoreMoveCaches(context);
      }
    },
    onSettled: (_data, _error, variables) => {
      invalidateMoveCaches(variables.snapshots.map((snapshot) => snapshot.id));
    },
  });

  const { mutateAsync, isPending } = mutation;

  // Single-flight guard for undo. useRef (not useState) is load-bearing:
  // two synchronous toast-action clicks within the same React frame both
  // read stale state under useState, whereas ref mutation is visible
  // immediately to the second caller. The toast action button is also
  // dismissed synchronously in moveWithUndo.onAction before any await so
  // the second click cannot find a target — defense in depth.
  const undoInFlightRef = useRef(false);

  const undoMove = useCallback(
    async (snapshots: MemoryMoveSnapshot[]) => {
      if (undoInFlightRef.current) return;
      undoInFlightRef.current = true;
      try {
        // Persistent replacement notification so the user sees feedback
        // for the whole roundtrip. The original success toast was dismissed
        // synchronously by moveWithUndo.onAction; this info card replaces
        // it and is itself replaced by the final success/error toast below.
        // `info()` has no autoDismissMs default — stays visible until
        // the next setBarNotification call.
        toast.info(t.toast.undoing);
        const groups = groupByDestination(snapshots);
        for (const { target, ids } of groups.values()) {
          const groupSnapshots = snapshots.filter((snapshot) =>
            ids.includes(snapshot.id),
          );
          // force=true bypasses the client-side already-at-target check:
          // undo passes the original snapshots AND the original target, so
          // the optimization would incorrectly no-op the server roundtrip.
          // The server is the source of truth here — the memory is currently
          // at the *previous* destination, and the undo must actually move
          // it back.
          await mutateAsync({
            snapshots: groupSnapshots,
            target,
            force: true,
          });
        }
        toast.success(t.toast.moveUndone);
      } catch (error) {
        const classified = classifyMutationError(error, {
          action:
            snapshots.length > 1
              ? t.errors.action.moveMemories
              : t.errors.action.moveMemory,
        });
        // Pass autoDismissMs so rate-limit toasts get their 8s reading
        // window. useBarToast.error() defaults to 5s which is too short
        // for "try again in Ns" copy the user needs to read the countdown.
        if (classified) {
          toast.error(classified.message, {
            autoDismissMs: classified.autoDismissMs,
          });
        } else {
          toast.error(t.toast.moveUndoFailed);
        }
      } finally {
        undoInFlightRef.current = false;
      }
    },
    [
      mutateAsync,
      t.errors.action.moveMemory,
      t.errors.action.moveMemories,
      t.toast.moveUndone,
      t.toast.moveUndoFailed,
      t.toast.undoing,
      toast,
    ],
  );

  const moveWithUndo = useCallback(
    async (
      snapshots: MemoryMoveSnapshot[],
      target: MemoryMoveTarget,
      successMessage: string,
    ): Promise<boolean> => {
      // Single-flight UI: block re-entry while a move is in flight on this
      // hook instance. Rapid clicks / drag drops don't stack up.
      if (isPending) {
        return false;
      }
      const movableSnapshots = snapshots.filter(isServerMovableSnapshot);
      if (movableSnapshots.length === 0) {
        toast.error(t.batch.moveNotReady);
        return false;
      }
      try {
        const result = await mutateAsync({
          snapshots: movableSnapshots,
          target,
        });
        // Partial success: append a "· N skipped" suffix to the caller's
        // success message so the user knows some memories were held back
        // (e.g. team-hub content they don't own, or already-at-target no-ops).
        const resolvedMessage =
          result.skipped.length > 0
            ? interpolate(t.batch.partialMove, {
                success: successMessage,
                skipped: result.skipped.length,
              })
            : successMessage;
        // Undo must only replay the subset the server actually moved.
        // already_at_target snapshots never changed on the server, and
        // not_owned/not_found items were never ours to touch. Reverting
        // them would cause a phantom move for memories the user did not
        // intend to relocate.
        const skippedIds = new Set(result.skipped.map((entry) => entry.id));
        const undoSnapshots = movableSnapshots.filter(
          (snapshot) => !skippedIds.has(snapshot.id),
        );
        toast.success(resolvedMessage, {
          actionLabel: t.import.undo,
          autoDismissMs: 6000,
          onAction: () => {
            if (undoSnapshots.length === 0) return;
            // Kill the success toast synchronously before any await so a
            // second rapid click cannot re-enter through the same stale
            // action button. The useRef latch inside undoMove is the
            // correctness guarantee; this dismissal is the UX one.
            toast.dismiss();
            void undoMove(undoSnapshots);
          },
        });
        return true;
      } catch (error) {
        // User-facing errors must never leak raw JS error messages. Business
        // codes (topic_not_found, permission_denied) win first; then the
        // status-class classifier surfaces rate-limit / offline / 5xx copy
        // BEFORE falling through to the generic moveFailed. Without the
        // classifier layer, a 429 during a drag-drop burst just said
        // "Couldn't move memories" — the user had no idea rate limits
        // applied or how long to wait.
        if (error instanceof MemaxError) {
          if (error.code === "topic_not_found") {
            toast.error(t.batch.targetNotFound);
          } else if (
            error.code === "not_member" ||
            error.code === "no_write_access" ||
            error.code === "permission_denied"
          ) {
            toast.error(t.batch.noWriteAccess);
          } else {
            const classified = classifyMutationError(error, {
              action:
                snapshots.length > 1
                  ? t.errors.action.moveMemories
                  : t.errors.action.moveMemory,
            });
            if (classified) {
              toast.error(classified.message, {
                autoDismissMs: classified.autoDismissMs,
              });
            } else {
              console.error("[useMemoryMove] unmapped MemaxError", error);
              toast.error(t.batch.moveFailed);
            }
          }
        } else if (error instanceof MemoryMoveIncompleteError) {
          // Full-skip sentinel from applyMove. Inspect the carried
          // skipped array to pick reason-specific copy. The common
          // bypass-block case (every id blocked by source hub delete
          // policy) shows a dedicated message; mixed reasons or pure
          // not_owned fall through to the generic noWriteAccess.
          const allSourceDelete =
            error.skipped.length > 0 &&
            error.skipped.every((s) => s.reason === "source_delete_forbidden");
          toast.error(
            allSourceDelete ? t.batch.moveSourceDenied : t.batch.noWriteAccess,
          );
        } else {
          // Unknown error path: give the classifier a shot first (offline
          // detection via navigator.onLine works even without a MemaxError),
          // then fall through to the generic moveFailed with console log.
          const classified = classifyMutationError(error, {
            action:
              snapshots.length > 1
                ? t.errors.action.moveMemories
                : t.errors.action.moveMemory,
          });
          if (classified) {
            toast.error(classified.message, {
              autoDismissMs: classified.autoDismissMs,
            });
          } else {
            console.error("[useMemoryMove] unexpected move failure", error);
            toast.error(t.batch.moveFailed);
          }
        }
        return false;
      }
    },
    [
      interpolate,
      isPending,
      mutateAsync,
      t.batch.moveFailed,
      t.batch.moveNotReady,
      t.batch.moveSourceDenied,
      t.batch.noWriteAccess,
      t.batch.partialMove,
      t.batch.targetNotFound,
      t.errors.action.moveMemory,
      t.errors.action.moveMemories,
      t.import.undo,
      toast,
      undoMove,
    ],
  );

  // Success-message helpers are destination-aware. When the caller knows the
  // human-readable destination name (topic name, or hub display name for
  // hub-only moves), they pass it in — the toast becomes "Moved to Design."
  // or "Moved 3 memories to Design." All three user surfaces (pill, batch,
  // drag/drop) share this pattern so the UX voice is uniform regardless of
  // which control the user interacted with. Callers that can't resolve a
  // destination name fall back to the generic "Moved." / "Moved N memories."
  // strings — defensive, not expected in practice.
  const moveManySuccess = useCallback(
    (count: number, destinationName?: string) =>
      destinationName
        ? count === 1
          ? interpolate(t.batch.movedToDestinationOne, {
              name: destinationName,
            })
          : interpolate(t.batch.movedToDestination, {
              n: count,
              name: destinationName,
            })
        : pluralize(t.batch.movedOne, t.batch.moved, count),
    [
      interpolate,
      t.batch.moved,
      t.batch.movedOne,
      t.batch.movedToDestination,
      t.batch.movedToDestinationOne,
    ],
  );

  const moveOneSuccess = useCallback(
    (destinationName?: string) =>
      destinationName
        ? interpolate(t.toast.movedTo, { name: destinationName })
        : t.toast.moved,
    [interpolate, t.toast.moved, t.toast.movedTo],
  );

  // Topic-clear is semantically distinct from "Moved to X" — there is no
  // destination. Callers fire this from TopicLocation.handleRemove when the
  // user explicitly clears the current topic assignment.
  const moveOneCleared = useCallback(
    () => t.toast.topicCleared,
    [t.toast.topicCleared],
  );

  return {
    isPending,
    moveWithUndo,
    moveManySuccess,
    moveOneSuccess,
    moveOneCleared,
  };
}
