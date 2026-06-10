"use client";

/**
 * useMemoryForget — single authoritative user-forget pipeline.
 *
 * Mirrors useMemoryMove's structure so every surface (batch toolbar,
 * memory modal, detail page) shares one optimistic lifecycle, one
 * error surface, one single-flight gate, and one partial-success UX.
 *
 * React Query cache rule (load-bearing, same as use-memory-move.ts):
 * queryClient.getQueriesData({ queryKey: PREFIX }) returns every
 * cached query whose key starts with PREFIX, including heterogeneous
 * shapes under ["topics"]:
 *   - ["topics", hubId]          → TopicListResponse { topics: [] }
 *   - ["topics", "detail", id]   → Topic
 *   - ["topics", topicId, "memories"] → TopicMemoriesResponse { memories: [] }
 * Every walk over these results MUST filter entries by an explicit
 * query-key shape guard before accessing inner fields.
 *
 * Design decisions locked in v4 of the plan:
 *   - No undo. Explicit user forget is irreversible by design. A
 *     forgotten-inbox surface is a future separate feature and would
 *     require a real soft-delete architecture (deleted_at column,
 *     filter predicates, purge worker, restore endpoint). Not in scope.
 *   - List-only optimistic removal. memoryDetailQueryKey(id) is NOT
 *     touched on the optimistic path so the detail route at
 *     /memories/[id] does not blank for skipped ids. Detail caches
 *     are reconciled exclusively by onSettled invalidation.
 *   - Detail caches are also OMITTED from the snapshot context. If we
 *     don't mutate them on the optimistic path, restoring them on
 *     onError would clobber concurrent refetches / sibling mutations /
 *     socket pushes with stale snapshot data.
 *   - Builder-based success message. forgetWithConfirm takes a
 *     (count) => string builder, not a prebuilt string, so the
 *     effectiveDeleted count and the rendered copy can never drift.
 *   - Reason-aware full-skip branch. deleted === 0 picks copy by
 *     dominant skip reason: delete_failed → retryable forgetFailed,
 *     not_owned → forgetDenied, all-not_found → effective success
 *     (the memory is already in the user's desired end state).
 *   - not_found counts as effective deleted. Selecting 5 memories
 *     where 4 are race-deleted elsewhere emits "Forgot 5 memories."
 *     (matching selection intent), not "Forgot 1 · 4 skipped."
 */

import { useCallback } from "react";
import { useMutation } from "@tanstack/react-query";
import { MemaxError } from "memax-sdk";
import type { BatchDeleteResult, Memory } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import {
  cancelMemoryListMutations,
  invalidateMemoryWriteCaches,
  applyOptimisticMemoryRemoval,
  restoreMemoryListCaches,
  snapshotMemoryListCaches,
  type MemoryListCacheSnapshot,
} from "./memory-cache";
import { useBarToast } from "./use-bar-toast";
import { useInterpolate, useLocale } from "@/i18n";
import { classifyMutationError } from "@/lib/error-copy";

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

interface ForgetMutationVariables {
  ids: string[];
}

const EMPTY_BATCH_RESULT: BatchDeleteResult = { deleted: 0, skipped: [] };

async function applyForget(ids: string[]): Promise<BatchDeleteResult> {
  if (ids.length === 0) return EMPTY_BATCH_RESULT;
  const client = getMemaxClient();
  return client.memories.batchDelete(ids);
}

export function useMemoryForget() {
  const toast = useBarToast();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const mutation = useMutation<
    BatchDeleteResult,
    Error,
    ForgetMutationVariables,
    MemoryListCacheSnapshot
  >({
    mutationFn: ({ ids }) => applyForget(ids),
    onMutate: async ({ ids }) => {
      await cancelMemoryListMutations();
      const snapshot = snapshotMemoryListCaches();
      applyOptimisticMemoryRemoval(ids);
      return snapshot;
    },
    onError: (_error, _variables, snapshot) => {
      restoreMemoryListCaches(snapshot);
    },
    // onSuccess is intentionally a no-op. Partial-skip reconciliation
    // happens entirely through onSettled invalidation. Attempting to
    // surgically restore list entries for skipped ids here duplicates
    // server-truth logic on the client and is fragile under concurrent
    // writes — the brief flash of skipped rows re-appearing after
    // invalidation is acceptable for the rare partial-skip scenario.
    onSettled: (_data, _error, variables) => {
      invalidateMemoryWriteCaches(variables.ids);
    },
  });

  const { mutateAsync, isPending } = mutation;

  // Default success-message builder. Singular for count === 1, plural
  // otherwise, using existing i18n keys. Callers can override by passing
  // their own (count) => string function to forgetWithConfirm — useful
  // when the caller wants a different voice ("Topic emptied." etc).
  const forgetSuccessMessage = useCallback(
    (count: number) =>
      count === 1 ? t.toast.forgot : interpolate(t.batch.forgot, { n: count }),
    [interpolate, t.toast.forgot, t.batch.forgot],
  );

  /**
   * Single authoritative forget primitive. Returns true on success
   * (including partial-success where effectiveDeleted > 0), false on
   * single-flight reject, UUID validation failure, or full-skip with
   * no effective deletes.
   *
   * Callers MUST gate side effects (selection.exit, router.push,
   * modal close) on the returned boolean. The hook does not throw on
   * expected failures — use the boolean. When wrapped inside
   * useDestructiveAction.run(), return the boolean from the action
   * body so the destructive confirmation card stays mounted on false.
   */
  const forgetWithConfirm = useCallback(
    async (
      ids: string[],
      buildSuccessMessage?: (count: number) => string,
    ): Promise<boolean> => {
      if (isPending) return false;
      const validIds = ids.filter((id) => UUID_PATTERN.test(id));
      if (validIds.length === 0) {
        toast.error(t.batch.forgetNotReady);
        return false;
      }

      try {
        const result = await mutateAsync({ ids: validIds });

        // Count per-reason skips. notFound is the user's desired end
        // state (the memory is already gone), so it counts as effective
        // success; the other two are real failures.
        const notFoundCount = result.skipped.filter(
          (s) => s.reason === "not_found",
        ).length;
        const notOwnedCount = result.skipped.filter(
          (s) => s.reason === "not_owned",
        ).length;
        const failedCount = result.skipped.filter(
          (s) => s.reason === "delete_failed",
        ).length;

        const effectiveDeleted = result.deleted + notFoundCount;
        const realSkipped = notOwnedCount + failedCount;

        if (effectiveDeleted === 0) {
          // Full skip — pick error copy by dominant reason. delete_failed
          // takes precedence because retry may fix infra issues, while
          // not_owned is a permission issue retry won't resolve.
          if (failedCount > 0) {
            toast.error(t.batch.forgetFailed);
          } else {
            toast.error(t.batch.forgetDenied);
          }
          return false;
        }

        // Hook owns the success-message count so the builder's rendered
        // copy and the `realSkipped` suffix are always arithmetically
        // consistent. Callers cannot pre-bake a count that contradicts
        // the server's actual outcome.
        const builder = buildSuccessMessage ?? forgetSuccessMessage;
        const base = builder(effectiveDeleted);
        const message =
          realSkipped > 0
            ? interpolate(t.batch.partialForget, {
                success: base,
                skipped: realSkipped,
              })
            : base;
        toast.success(message);
        return true;
      } catch (error) {
        // Business codes beat status classes (permission is non-retryable;
        // rate-limit is retryable). When neither matches, the classifier
        // gives us offline/5xx/rate-limit copy BEFORE the generic
        // forgetFailed fallback — so a 429 during a batch forget says
        // "try again in Ns" instead of the identical copy a server error
        // would produce.
        const action =
          validIds.length > 1
            ? t.errors.action.forgetMemories
            : t.errors.action.forgetMemory;
        if (error instanceof MemaxError) {
          if (
            error.code === "not_member" ||
            error.code === "no_write_access" ||
            error.code === "permission_denied" ||
            error.code === "forbidden"
          ) {
            toast.error(t.batch.forgetDenied);
          } else {
            const classified = classifyMutationError(error, { action });
            if (classified) {
              toast.error(classified.message, {
                autoDismissMs: classified.autoDismissMs,
              });
            } else {
              console.error("[useMemoryForget] unmapped MemaxError", error);
              toast.error(t.batch.forgetFailed);
            }
          }
        } else {
          const classified = classifyMutationError(error, { action });
          if (classified) {
            toast.error(classified.message, {
              autoDismissMs: classified.autoDismissMs,
            });
          } else {
            console.error("[useMemoryForget] unexpected forget failure", error);
            toast.error(t.batch.forgetFailed);
          }
        }
        return false;
      }
    },
    [
      forgetSuccessMessage,
      interpolate,
      isPending,
      mutateAsync,
      t.batch.forgetDenied,
      t.batch.forgetFailed,
      t.batch.forgetNotReady,
      t.batch.partialForget,
      t.errors.action.forgetMemory,
      t.errors.action.forgetMemories,
      toast,
    ],
  );

  return {
    isPending,
    forgetWithConfirm,
    forgetSuccessMessage,
  };
}

// Re-export for tests that seed caches with placeholder memories.
export type { Memory };
