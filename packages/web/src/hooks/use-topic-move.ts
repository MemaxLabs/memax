"use client";

/**
 * useTopicMove — authoritative pipeline for user-initiated topic reparent.
 *
 * Mirrors the use-memory-move.ts pattern: optimistic apply in onMutate,
 * rollback in onError, and unconditional onSettled invalidation for the
 * `["topics"]` query prefix. Single-flight via `isPending`, exact-position
 * undo via `force: true` on the replay.
 *
 * React Query cache rule (load-bearing): `queryClient.getQueriesData({ queryKey: PREFIX })`
 * returns every cached query whose key starts with PREFIX — which, under
 * shared prefixes like `["topics"]`, includes heterogeneous shapes:
 *   - ["topics", hubId]               → TopicListResponse  { topics: TopicTree[] }
 *   - ["topics", "detail", id]        → Topic
 *   - ["topics", topicId, "memories"] → TopicMemoriesResponse
 *
 * Every walk over these results MUST filter entries by an explicit
 * query-key shape guard BEFORE accessing inner fields. Optional chaining
 * alone is not enough: `data` can be a differently-shaped but defined
 * object from a sibling query, so the inner field access crashes with
 * `undefined.forEach` / similar. When in doubt, guard first.
 *
 * Accessibility note: the ⋮ menu picker in TopicTreeNode uses the exact
 * same moveTopicWithUndo code path as drag/drop, so keyboard/touch/mobile
 * users get the same optimistic apply, undo, and error mapping. No
 * KeyboardSensor is registered on TopicDndProvider in this pass; the
 * picker is the canonical fallback.
 */

import { useCallback, useRef } from "react";
import { useMutation } from "@tanstack/react-query";
import { MemaxError } from "memax-sdk";
import type { Topic, TopicListResponse, TopicTree } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { topicQueryKey } from "./use-topics";
import { hubListQueryKey } from "./use-hubs";
import { useBarToast } from "./use-bar-toast";
import { useInterpolate, useLocale } from "@/i18n";
import { classifyMutationError } from "@/lib/error-copy";

const TOPIC_QUERY_PREFIX = ["topics"] as const;

export interface TopicMoveSnapshot {
  id: string;
  hubId: string;
  parentId: string | null;
  position: number;
  name: string;
}

export interface TopicMoveTarget {
  parentId: string | null;
  position?: number;
}

interface TopicMoveCacheContext {
  /**
   * All cached detail entries for the moving topic, captured with their
   * exact keys so restoreTopicCaches can write each value back on
   * rollback — a missing detail restore would make rollback
   * detail-blind (bug caught by Codex review). Plural because
   * topicQueryKey is a prefix: entries carry a trailing hub-scope
   * segment (see use-topics.ts).
   */
  topicDetails: Array<[readonly unknown[], Topic | undefined]>;
  topicLists: Array<[readonly unknown[], TopicListResponse | undefined]>;
}

interface TopicMoveMutationVariables {
  snapshot: TopicMoveSnapshot;
  target: TopicMoveTarget;
  /** Undo sets this to bypass the client-side already-at-target optimization. */
  force?: boolean;
}

// ── Query-key shape guards ────────────────────────────────────────────────

function isTopicListQueryKey(
  value: readonly unknown[],
): value is readonly ["topics", string] {
  // ["topics", hubId] — length 2 distinguishes from detail/memories keys.
  return (
    value.length === 2 && value[0] === "topics" && typeof value[1] === "string"
  );
}

// ── Pure tree transforms ───────────────────────────────────────────────────

/**
 * Walk the TopicTree forest and detach the subtree rooted at topicId.
 * Returns the new forest and the detached node (null if not found).
 * Does not mutate inputs.
 */
function detachSubtree(
  forest: readonly TopicTree[],
  topicId: string,
): { forest: TopicTree[]; detached: TopicTree | null } {
  let detached: TopicTree | null = null;
  const walk = (nodes: readonly TopicTree[]): TopicTree[] => {
    const out: TopicTree[] = [];
    for (const node of nodes) {
      if (node.id === topicId) {
        detached = node;
        continue;
      }
      const children = walk(node.children);
      out.push(children === node.children ? node : { ...node, children });
    }
    return out;
  };
  const next = walk(forest);
  return { forest: next, detached };
}

/**
 * Insert a node into a siblings array. Matches the server's render order
 * (ORDER BY position ASC, created_at ASC — see postgres_topics.go ListTopics).
 *
 *   - When `explicitPosition` is undefined (forward user moves — drag/drop,
 *     picker without a slot), append to the end. The user's intent is
 *     "drop it here" with no slot preference, and the server will assign
 *     the canonical order on the next refetch.
 *   - When `explicitPosition` is defined (undo replays the original slot,
 *     or a caller passes a target position), insert sorted by
 *     (moving.position, moving.created_at) so the optimistic tree matches
 *     what the server will return after onSettled invalidation.
 *
 * Codex-review fix: previously this always appended, which meant undo
 * would visibly land the topic at the end of its siblings before the
 * server response corrected it — violating the plan's "exact-position
 * restore" promise for the undo path.
 */
function insertIntoChildren(
  children: readonly TopicTree[],
  moving: TopicTree,
  explicitPosition: number | undefined,
): TopicTree[] {
  if (explicitPosition === undefined) {
    return [...children, moving];
  }
  const result: TopicTree[] = [];
  let inserted = false;
  for (const child of children) {
    if (!inserted && compareBySortOrder(moving, child) <= 0) {
      result.push(moving);
      inserted = true;
    }
    result.push(child);
  }
  if (!inserted) result.push(moving);
  return result;
}

function compareBySortOrder(a: TopicTree, b: TopicTree): number {
  if (a.position !== b.position) return a.position - b.position;
  const aTime = Date.parse(a.created_at);
  const bTime = Date.parse(b.created_at);
  if (aTime !== bTime) return aTime - bTime;
  return 0;
}

/**
 * Insert a subtree under the parent with the given id. If parentId is null,
 * insert at the forest root. Does not mutate inputs.
 */
function attachSubtree(
  forest: readonly TopicTree[],
  parentId: string | null,
  subtree: TopicTree,
  explicitPosition: number | undefined,
): TopicTree[] {
  if (parentId === null) {
    return insertIntoChildren(forest, subtree, explicitPosition);
  }
  return forest.map((node) => {
    if (node.id === parentId) {
      return {
        ...node,
        children: insertIntoChildren(node.children, subtree, explicitPosition),
      };
    }
    return {
      ...node,
      children: attachSubtree(
        node.children,
        parentId,
        subtree,
        explicitPosition,
      ),
    };
  });
}

/**
 * Apply a reparent to a TopicListResponse: remove the subtree from its
 * current position, update the moving node's parent_id/position/user_modified,
 * and re-insert under the target parent at the correct slot.
 */
export function patchTopicListForMove(
  data: TopicListResponse,
  snapshot: TopicMoveSnapshot,
  target: TopicMoveTarget,
): TopicListResponse {
  if (!data?.topics) return data;
  const { forest, detached } = detachSubtree(data.topics, snapshot.id);
  if (!detached) return data;
  const moved: TopicTree = {
    ...detached,
    parent_id: target.parentId,
    position: target.position ?? detached.position,
    user_modified: true,
  };
  const nextTopics = attachSubtree(
    forest,
    target.parentId,
    moved,
    target.position,
  );
  return { ...data, topics: nextTopics };
}

// ── Snapshot / restore helpers ─────────────────────────────────────────────

function snapshotTopicCaches(topicId: string): TopicMoveCacheContext {
  return {
    topicDetails: queryClient.getQueriesData<Topic>({
      queryKey: topicQueryKey(topicId),
    }),
    topicLists: queryClient
      .getQueriesData<TopicListResponse>({ queryKey: TOPIC_QUERY_PREFIX })
      .filter(([queryKey]) => isTopicListQueryKey(queryKey)),
  };
}

function restoreTopicCaches(context: TopicMoveCacheContext | undefined) {
  if (!context) return;
  // Codex-review fix: restore the topic detail cache on rollback.
  // Previously this helper only touched topicLists, so a failed reparent
  // left the detail page stuck at the optimistic parent/position until
  // the next refetch. Only write if we captured an actual value — if
  // the detail cache was empty at snapshot time, applyOptimisticTopicMove
  // did not touch it either, and writing `undefined` would create a
  // spurious cache entry.
  context.topicDetails.forEach(([queryKey, data]) => {
    // Only write captured values — a key snapshotted as `undefined`
    // was not touched by applyOptimisticTopicMove either, and writing
    // `undefined` would create a spurious cache entry.
    if (data !== undefined) {
      queryClient.setQueryData(queryKey, data);
    }
  });
  context.topicLists.forEach(([queryKey, data]) => {
    queryClient.setQueryData(queryKey, data);
  });
}

function applyOptimisticTopicMove(
  snapshot: TopicMoveSnapshot,
  target: TopicMoveTarget,
) {
  queryClient
    .getQueriesData<TopicListResponse>({ queryKey: TOPIC_QUERY_PREFIX })
    .forEach(([queryKey, data]) => {
      if (!isTopicListQueryKey(queryKey)) return;
      if (!data) return;
      const next = patchTopicListForMove(data, snapshot, target);
      if (next !== data) {
        queryClient.setQueryData(queryKey, next);
      }
    });

  // Topic detail cache: patch parent_id + position + user_modified on
  // every cached detail entry for the moving topic (topicQueryKey is a
  // prefix — entries carry a hub-scope segment). Don't seed the cache
  // from here — if there's nothing to patch, leave it for the refetch.
  queryClient
    .getQueriesData<Topic>({ queryKey: topicQueryKey(snapshot.id) })
    .forEach(([queryKey, detail]) => {
      if (!detail) return;
      queryClient.setQueryData(queryKey, {
        ...detail,
        parent_id: target.parentId,
        position: target.position ?? detail.position,
        user_modified: true,
      });
    });
}

function invalidateTopicCaches() {
  // Invalidate the whole topic prefix — covers list, detail, and memory
  // queries. Hub summary + list also touches unassigned counts, mirroring
  // the memory mover.
  queryClient.invalidateQueries({ queryKey: TOPIC_QUERY_PREFIX });
  queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
  queryClient.invalidateQueries({ queryKey: hubListQueryKey });
}

// ── Server application ───────────────────────────────────────────────────

function isTopicAlreadyAtTarget(
  snapshot: TopicMoveSnapshot,
  target: TopicMoveTarget,
) {
  return snapshot.parentId === target.parentId;
}

async function applyTopicMove(
  snapshot: TopicMoveSnapshot,
  target: TopicMoveTarget,
  force = false,
): Promise<Topic> {
  if (!force && isTopicAlreadyAtTarget(snapshot, target)) {
    // No-op guard — return a synthetic "unchanged" Topic so the mutation
    // succeeds without a server roundtrip. The type shape matches what
    // client.topics.update would return.
    return {
      id: snapshot.id,
      owner_id: "",
      hub_id: snapshot.hubId,
      parent_id: snapshot.parentId,
      name: snapshot.name,
      description: "",
      icon: "",
      position: snapshot.position,
      pinned: false,
      user_modified: false,
      created_at: new Date(0).toISOString(),
      updated_at: new Date().toISOString(),
    } as Topic;
  }
  const client = getMemaxClient();
  // parent_id === "" is the server's "clear parent / reparent to root"
  // sentinel. null is also accepted per SDK types but we normalize to ""
  // here for a single wire representation.
  const parentIdParam = target.parentId ?? "";
  return client.topics.update(
    snapshot.id,
    { parent_id: parentIdParam, position: target.position },
    snapshot.hubId,
  );
}

// ── Hook ──────────────────────────────────────────────────────────────────

export function useTopicMove() {
  const toast = useBarToast();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const mutation = useMutation<
    Topic,
    Error,
    TopicMoveMutationVariables,
    TopicMoveCacheContext
  >({
    mutationFn: ({ snapshot, target, force }) =>
      applyTopicMove(snapshot, target, force),
    onMutate: async ({ snapshot, target }) => {
      await queryClient.cancelQueries({ queryKey: TOPIC_QUERY_PREFIX });
      const context = snapshotTopicCaches(snapshot.id);
      applyOptimisticTopicMove(snapshot, target);
      return context;
    },
    onError: (_error, _variables, context) => {
      restoreTopicCaches(context);
    },
    onSettled: () => {
      invalidateTopicCaches();
    },
  });

  const { mutateAsync, isPending } = mutation;

  // Single-flight guard for undo. See useMemoryMove for the rationale on
  // useRef vs useState here — rapid synchronous toast clicks need immediate
  // visibility between callers.
  const undoInFlightRef = useRef(false);

  const undoTopicMove = useCallback(
    async (snapshot: TopicMoveSnapshot) => {
      if (undoInFlightRef.current) return;
      undoInFlightRef.current = true;
      try {
        toast.info(t.toast.undoing);
        // Replay with force=true so the client-side no-op guard doesn't
        // short-circuit (the snapshot carries the original parent/position,
        // which matches the current server state of the moved topic only in
        // the no-op branch — in the real undo path, the server still needs
        // to hear about the move back to the prior location).
        await mutateAsync({
          snapshot,
          target: {
            parentId: snapshot.parentId,
            position: snapshot.position,
          },
          force: true,
        });
        toast.success(t.topics.topicMoveUndone);
      } catch (error) {
        const classified = classifyMutationError(error, {
          action: t.errors.action.moveTopic,
        });
        if (classified) {
          toast.error(classified.message, {
            autoDismissMs: classified.autoDismissMs,
          });
        } else {
          toast.error(t.topics.topicMoveUndoFailed);
        }
      } finally {
        undoInFlightRef.current = false;
      }
    },
    [
      mutateAsync,
      t.errors.action.moveTopic,
      t.topics.topicMoveUndone,
      t.topics.topicMoveUndoFailed,
      t.toast.undoing,
      toast,
    ],
  );

  const moveTopicWithUndo = useCallback(
    async (
      snapshot: TopicMoveSnapshot,
      target: TopicMoveTarget,
      successMessage: string,
    ): Promise<boolean> => {
      if (isPending) {
        return false;
      }
      // Client-side no-op short-circuit: if the target parent matches the
      // current parent, drop silently. No toast, no server call — this is
      // the "dragged back onto current parent" case.
      if (isTopicAlreadyAtTarget(snapshot, target)) {
        return false;
      }
      try {
        await mutateAsync({ snapshot, target });
        toast.success(successMessage, {
          actionLabel: t.import.undo,
          autoDismissMs: 6000,
          onAction: () => {
            toast.dismiss();
            void undoTopicMove(snapshot);
          },
        });
        return true;
      } catch (error) {
        // Business codes (cycle/depth/invalid_parent/duplicate_name) are
        // non-retryable and take priority. Everything else routes through
        // the classifier for rate-limit / offline / 5xx copy, falling
        // through to topicMoveFailed as the final generic message.
        const fallback = () => {
          const classified = classifyMutationError(error, {
            action: t.errors.action.moveTopic,
          });
          if (classified) {
            toast.error(classified.message, {
              autoDismissMs: classified.autoDismissMs,
            });
          } else {
            toast.error(t.topics.topicMoveFailed);
            console.error("[useTopicMove] unmapped error", error);
          }
        };
        if (error instanceof MemaxError) {
          switch (error.code) {
            case "cycle_detected":
              toast.error(t.topics.topicCycleDetected);
              break;
            case "max_depth_subtree":
            case "max_depth":
              toast.error(t.topics.topicMaxDepthReached);
              break;
            case "invalid_parent":
              toast.error(t.topics.topicInvalidParent);
              break;
            case "duplicate_name":
              toast.error(t.topics.topicMoveFailed);
              break;
            default:
              fallback();
          }
        } else {
          fallback();
        }
        return false;
      }
    },
    [
      isPending,
      mutateAsync,
      t.errors.action.moveTopic,
      t.import.undo,
      t.topics.topicCycleDetected,
      t.topics.topicInvalidParent,
      t.topics.topicMaxDepthReached,
      t.topics.topicMoveFailed,
      toast,
      undoTopicMove,
    ],
  );

  return {
    isPending,
    moveTopicWithUndo,
  };
}
