"use client";

/**
 * useAgentConfigForget — single authoritative agent-config forget pipeline.
 *
 * Mirrors useMemoryForget's optimistic + rollback shape so both the
 * per-row "Forget" button and the "Forget all files" bulk action share
 * one optimistic lifecycle, one error surface, and one single-flight
 * gate. Single and batch paths converge through
 * `configs.batchDelete([...ids])`, the same way useMemoryForget routes
 * every delete through `memories.batchDelete`.
 *
 * Feedback placement (important — differs from useMemoryForget):
 *
 *   This hook does NOT call useBarToast. Agent config delete is a
 *   Settings CRUD action — its feedback must stay inline in the
 *   Settings surface, not fire into the global bar toast queue. The
 *   bar toast is reserved for global / background notification kinds
 *   (memory delete, move, dream-run receipts) where the user is
 *   outside the Settings context when the result lands.
 *
 *   The hook exposes a structured `feedback` state + `clearFeedback`
 *   so `agent-configs-section.tsx` can render inline copy next to the
 *   action (success count, partial count, error retry hint). The
 *   consuming component does the i18n — the hook only tracks raw
 *   counts and a kind discriminator.
 *
 * Rollback rule (mirrors use-memory-move.ts applyMove):
 *
 *   applyForget throws `AgentConfigForgetIncompleteError` only when
 *   every attempted delete came back as a REAL skip (delete_failed)
 *   — i.e. `effectiveDeleted === 0 && realSkipped > 0`. In that case
 *   React Query's onError fires, the optimistic list snapshot is
 *   restored, and the user sees the failed configs reappear
 *   immediately with inline error feedback.
 *
 *   Partial success (some deleted, some delete_failed) is NOT a
 *   throw — it's the resolved-value path. Throwing on partial would
 *   roll back the successful deletes too, causing a worse visual
 *   flash than just letting onSettled invalidation reconcile the
 *   failed rows back in. See use-memory-move.ts:520-526 for the same
 *   reasoning on batch-move.
 *
 *   `not_found` is NEVER a real skip. The server collapses "unknown
 *   id", "already deleted", and "wrong owner" into `not_found`
 *   because they're indistinguishable from the user's perspective
 *   (the config is gone). not_found counts as effective success and
 *   stays on the resolved path — no rollback, no error feedback.
 *
 * Dead-code note (Codex review pass 4): the config skip taxonomy is
 * only `not_found | delete_failed`. Because `realSkipped` excludes
 * `not_found` and the throw condition is `realSkipped > 0`, every
 * thrown IncompleteError is guaranteed to carry at least one
 * `delete_failed` skip. The UI's error branch therefore doesn't need
 * a reason-aware ternary — showing the generic "couldn't forget"
 * copy is always correct. If we later add a permission-style reason
 * (requires a pre-lookup in the server handler to distinguish miss
 * from wrong-owner), reintroduce a branch here.
 */

import { useCallback, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  AgentConfigBatchDeleteResult,
  AgentConfigListResult,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

const AGENT_CONFIGS_QUERY_KEY = ["agent-configs"] as const;

/**
 * Sentinel thrown by applyForget when every attempted delete was a
 * real failure (all delete_failed, no effective success). Carries the
 * full result so the outer catch can still inspect the skip breakdown
 * if new reasons appear later.
 *
 * Extending Error preserves React Query's onError lifecycle: the
 * mutation is treated as a failure, the optimistic snapshot rollback
 * in onError still fires.
 */
export class AgentConfigForgetIncompleteError extends Error {
  constructor(public readonly result: AgentConfigBatchDeleteResult) {
    super("agent_config_forget_incomplete");
    this.name = "AgentConfigForgetIncompleteError";
  }
}

/**
 * Structured feedback for the Settings UI to render inline. The hook
 * writes this on every resolved mutation (success or error); the UI
 * reads it and picks the right i18n copy + visual treatment. The UI
 * owns i18n because it already has `useLocale()` and `interpolate`.
 *
 * - `success`: `deleted > 0 && failed === 0`. User saw every
 *   requested config removed (or already gone via not_found). UI
 *   shows success copy with `deleted` as the count.
 * - `partial`: `deleted > 0 && failed > 0`. Some deletes committed,
 *   some stay on the server. UI shows partial copy with both counts
 *   and a retry hint for the failed ones.
 * - `error`: `deleted === 0 && failed > 0`. Full skip with real
 *   failures. Rollback has already restored the snapshot so the
 *   failed configs are back in the list. UI shows retry copy.
 */
export type AgentConfigForgetFeedbackKind = "success" | "partial" | "error";

export interface AgentConfigForgetFeedback {
  kind: AgentConfigForgetFeedbackKind;
  /** Effective deletes (deleted + not_found). Zero in the error case. */
  deleted: number;
  /** Count of real delete_failed skips (not including not_found). */
  failed: number;
  /** The ids the user originally attempted to forget, for retry. */
  attemptedIds: string[];
}

const EMPTY_BATCH_RESULT: AgentConfigBatchDeleteResult = {
  deleted: 0,
  skipped: [],
};

async function applyForget(
  ids: string[],
): Promise<AgentConfigBatchDeleteResult> {
  if (ids.length === 0) return EMPTY_BATCH_RESULT;
  const result = await getMemaxClient().configs.batchDelete(ids);

  // not_found = target reached (server row already gone). Counts as
  // effective success and does NOT need rollback. delete_failed is
  // the only reason that justifies throwing — server still has the
  // row but we optimistically removed it, so onError must restore
  // the snapshot.
  const notFoundCount = result.skipped.filter(
    (s) => s.reason === "not_found",
  ).length;
  const effectiveDeleted = result.deleted + notFoundCount;
  const realSkipped = result.skipped.length - notFoundCount;

  if (effectiveDeleted === 0 && realSkipped > 0) {
    throw new AgentConfigForgetIncompleteError(result);
  }
  return result;
}

interface ForgetContext {
  prev: AgentConfigListResult | undefined;
}

export function useAgentConfigForget() {
  const queryClient = useQueryClient();
  const [feedback, setFeedback] = useState<AgentConfigForgetFeedback | null>(
    null,
  );

  const mutation = useMutation<
    AgentConfigBatchDeleteResult,
    Error,
    { ids: string[] },
    ForgetContext
  >({
    mutationFn: ({ ids }) => applyForget(ids),
    onMutate: async ({ ids }) => {
      await queryClient.cancelQueries({ queryKey: AGENT_CONFIGS_QUERY_KEY });
      const prev = queryClient.getQueryData<AgentConfigListResult>(
        AGENT_CONFIGS_QUERY_KEY,
      );
      if (prev) {
        const idSet = new Set(ids);
        queryClient.setQueryData<AgentConfigListResult>(
          AGENT_CONFIGS_QUERY_KEY,
          {
            ...prev,
            configs: prev.configs.filter((c) => !idSet.has(c.id)),
          },
        );
      }
      return { prev };
    },
    onError: (_error, _variables, context) => {
      // Fires on AgentConfigForgetIncompleteError (all-delete_failed)
      // AND on generic mutation failures (network, parse). Restores
      // the list snapshot so failed rows reappear without waiting
      // for the onSettled invalidate round-trip.
      if (context?.prev) {
        queryClient.setQueryData(AGENT_CONFIGS_QUERY_KEY, context.prev);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: AGENT_CONFIGS_QUERY_KEY });
    },
  });

  const { mutateAsync, isPending } = mutation;

  /**
   * Single authoritative forget primitive. Returns `true` on success
   * (including partial success where effectiveDeleted > 0), `false`
   * on single-flight reject or full-skip with real failures.
   *
   * Also writes `feedback` state that the Settings UI reads to
   * render inline copy. The hook intentionally does not toast —
   * Settings feedback stays in Settings.
   *
   * Callers MUST wrap this in `useDestructiveAction.run(() => ...)`
   * inside the component layer so the confirm gesture stays mounted
   * on `false` for retry.
   */
  const forgetConfigs = useCallback(
    async (ids: string[]): Promise<boolean> => {
      if (isPending) return false;
      if (ids.length === 0) return false;

      try {
        const result = await mutateAsync({ ids });
        const notFoundCount = result.skipped.filter(
          (s) => s.reason === "not_found",
        ).length;
        const failedCount = result.skipped.filter(
          (s) => s.reason === "delete_failed",
        ).length;
        const effectiveDeleted = result.deleted + notFoundCount;

        setFeedback({
          kind: failedCount > 0 ? "partial" : "success",
          deleted: effectiveDeleted,
          failed: failedCount,
          attemptedIds: ids,
        });
        return true;
      } catch (error) {
        // onError already restored the snapshot. Config skip taxonomy
        // guarantees every thrown Incomplete is all-delete_failed, so
        // `failed` can be derived from the attempted count. Generic
        // errors (network, parse) produce the same error kind — the
        // user sees one retry hint and acts on it.
        if (error instanceof AgentConfigForgetIncompleteError) {
          const failedCount = error.result.skipped.filter(
            (s) => s.reason === "delete_failed",
          ).length;
          setFeedback({
            kind: "error",
            deleted: 0,
            failed: failedCount,
            attemptedIds: ids,
          });
        } else {
          console.error("[useAgentConfigForget] unexpected", error);
          setFeedback({
            kind: "error",
            deleted: 0,
            failed: ids.length,
            attemptedIds: ids,
          });
        }
        return false;
      }
    },
    [isPending, mutateAsync],
  );

  const clearFeedback = useCallback(() => setFeedback(null), []);

  return { forgetConfigs, isPending, feedback, clearFeedback };
}
