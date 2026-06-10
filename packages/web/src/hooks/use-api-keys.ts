"use client";

/**
 * API keys — list + revoke.
 *
 * Revoke feedback stays inline in Settings (same rule as
 * `useAgentConfigForget` and `useDisconnectAgent`). The hook exposes
 * structured feedback state + a clear function; the
 * `AgentConfigsSection` component renders local copy next to the
 * action instead of routing through `useBarToast`. Bar toasts are
 * reserved for global / background notification kinds, not Settings
 * CRUD.
 *
 * Rollback rule mirrors the other Settings destructive hooks:
 *
 *   applyRevoke throws `ApiKeyRevokeIncompleteError` only when the
 *   server reports `revoke_failed` — a real failure where the key
 *   is still present on the server and the optimistic removal must
 *   be rolled back. `not_found` is the idempotent success path:
 *   the key is already gone (race with another device, or another
 *   tab already revoked it), the client's optimistic removal was
 *   correct, no throw.
 */

import { useCallback, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  ApiKeyListItem,
  ApiKeyRevokeResult,
  UpdateApiKeyPayload,
  UpdateApiKeyResult,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useBarToast } from "@/hooks/use-bar-toast";
import { useStreamHealth } from "@/hooks/use-stream-health";
import { classifyMutationError } from "@/lib/error-copy";
import { useLocale } from "@/i18n";

const API_KEYS_QUERY_KEY = ["api-keys"] as const;

export type ApiKeyItem = ApiKeyListItem;

export function useApiKeys() {
  // Stream-health-conditional freshness (PR E — per-surface cleanup).
  //
  // When the SSE stream is connected, server-pushed `key.changed` +
  // `agent.changed` events (PR C) invalidate this cache whenever a
  // mutation happens on any device, so the 30s global staleTime +
  // focus-off default is enough — we can trust the push path.
  //
  // When the stream is degraded (pre-connect, reconnecting after
  // laptop sleep, network drop, or Redis unavailable on the server
  // side), push events are lost. We fall back to the pre-push
  // behavior: `refetchOnMount: 'always'` + focus-refetch, so
  // navigation and tab-return still catch cross-device mutations.
  //
  // The reconcile safety net (PR B) already fires a broad invalidate
  // on reconnect/visibility-resume, but that one-shot fires on the
  // transition — this hook's conditional guard handles the window
  // between the disconnect and the next connected mount.
  const { connection } = useStreamHealth();
  const streamDown = connection !== "connected";

  return useQuery<ApiKeyListItem[]>({
    queryKey: API_KEYS_QUERY_KEY,
    queryFn: () => getMemaxClient().auth.listKeys(),
    gcTime: 30 * 60 * 1000,
    refetchOnMount: streamDown ? "always" : true,
    refetchOnWindowFocus: streamDown,
  });
}

/**
 * Sentinel thrown by applyRevoke when the server reports
 * `revoke_failed`. Carries the full result so the outer catch can
 * inspect counts if a future reason lands there. `not_found` never
 * reaches this throw — it takes the resolved path as idempotent
 * success.
 */
export class ApiKeyRevokeIncompleteError extends Error {
  constructor(public readonly result: ApiKeyRevokeResult) {
    super("api_key_revoke_incomplete");
    this.name = "ApiKeyRevokeIncompleteError";
  }
}

async function applyRevoke(id: string): Promise<ApiKeyRevokeResult> {
  const result = await getMemaxClient().auth.revokeKey(id);
  // Revoke is single-entity — skipped has 0 or 1 entries.
  //   not_found     → target reached; return normally, no rollback.
  //   revoke_failed → row still on server; throw so onError fires.
  const skip = result.skipped[0];
  if (skip?.reason === "revoke_failed") {
    throw new ApiKeyRevokeIncompleteError(result);
  }
  return result;
}

/**
 * Structured feedback for the Settings UI to render inline near the
 * revoke button. The UI owns i18n; the hook tracks the raw outcome
 * kind.
 *
 * - `success`: the key was revoked.
 * - `not_found`: idempotent success (the key was already gone).
 *   UI can show "Already revoked" or nothing.
 * - `error`: revoke_failed or network/parse. Rollback has already
 *   restored the key; UI shows retry copy.
 */
export type ApiKeyRevokeFeedbackKind = "success" | "not_found" | "error";

export interface ApiKeyRevokeFeedback {
  kind: ApiKeyRevokeFeedbackKind;
  id: string;
}

export function useRevokeApiKey() {
  const qc = useQueryClient();
  const [feedback, setFeedback] = useState<ApiKeyRevokeFeedback | null>(null);

  const mutation = useMutation<
    ApiKeyRevokeResult,
    Error,
    { id: string },
    { prev: ApiKeyListItem[] | undefined }
  >({
    mutationFn: ({ id }) => applyRevoke(id),
    onMutate: async ({ id }) => {
      await qc.cancelQueries({ queryKey: API_KEYS_QUERY_KEY });
      const prev = qc.getQueryData<ApiKeyListItem[]>(API_KEYS_QUERY_KEY);
      qc.setQueryData<ApiKeyListItem[]>(API_KEYS_QUERY_KEY, (old) =>
        old?.filter((k) => k.id !== id),
      );
      return { prev };
    },
    onError: (_error, _variables, context) => {
      // Fires on ApiKeyRevokeIncompleteError (revoke_failed) AND on
      // generic mutation failures. Restores the snapshot so the key
      // reappears immediately. not_found never lands here —
      // applyRevoke returns resolved for that reason.
      if (context?.prev) {
        qc.setQueryData(API_KEYS_QUERY_KEY, context.prev);
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: API_KEYS_QUERY_KEY }),
  });

  const { mutateAsync, isPending } = mutation;

  /**
   * Revoke a single API key. Returns `true` on success (real or
   * idempotent), `false` on single-flight reject or revoke failure.
   * Writes `feedback` state for the Settings UI to render inline.
   *
   * Callers wrap this in `useDestructiveAction.run(() => ...)` inside
   * the component layer.
   */
  const revokeKey = useCallback(
    async (id: string): Promise<boolean> => {
      if (isPending) return false;
      if (!id) return false;

      try {
        const result = await mutateAsync({ id });
        const skip = result.skipped[0];
        if (result.revoked) {
          setFeedback({ kind: "success", id });
        } else if (skip?.reason === "not_found") {
          setFeedback({ kind: "not_found", id });
        } else {
          // Defensive guard — applyRevoke should not reach this
          // branch for known reasons.
          setFeedback({ kind: "error", id });
          return false;
        }
        return true;
      } catch (error) {
        // onError already restored the snapshot. Whether the throw
        // was ApiKeyRevokeIncompleteError or a generic network
        // error, the user sees the same retry copy.
        if (!(error instanceof ApiKeyRevokeIncompleteError)) {
          console.error("[useRevokeApiKey] unexpected", error);
        }
        setFeedback({ kind: "error", id });
        return false;
      }
    },
    [isPending, mutateAsync],
  );

  const clearFeedback = useCallback(() => setFeedback(null), []);

  return { revokeKey, isPending, feedback, clearFeedback };
}

/**
 * Patch API key metadata. Handles two user-facing cases:
 *
 *   - Assigning an agent to an unassigned key (Assign ▾ dropdown).
 *   - Marking a key as standalone so the Assign affordance stops
 *     showing.
 *
 * Optimistically flips the row in the cache. Rolls back on server
 * error. Invalidates both api-keys and connected-agents because a
 * successful assign upserts a connected-agents row on the server.
 *
 * Success is self-evident via the classifier state flip (the row
 * re-renders as Linked/Standalone), so this hook does not carry a
 * feedback channel like useRevokeApiKey does. If error copy becomes
 * important, wire it up via a toast rather than inline state — the
 * rollback already visually reverts the row.
 */
export function useUpdateApiKey() {
  const qc = useQueryClient();
  const toast = useBarToast();
  const { t } = useLocale();
  // pendingKeyId drives the "Assigning…" busy UI. A separate piece of
  // state (not mutation.isPending) so a specific row knows whether
  // the pending mutation belongs to it, and so the busy state clears
  // on both success and error paths.
  const [pendingKeyId, setPendingKeyId] = useState<string | null>(null);

  const mutation = useMutation<
    UpdateApiKeyResult,
    Error,
    { id: string; payload: UpdateApiKeyPayload },
    { prev: ApiKeyListItem[] | undefined }
  >({
    mutationFn: ({ id, payload }) =>
      getMemaxClient().auth.updateKey(id, payload),
    onMutate: async ({ id, payload }) => {
      await qc.cancelQueries({ queryKey: API_KEYS_QUERY_KEY });
      const prev = qc.getQueryData<ApiKeyListItem[]>(API_KEYS_QUERY_KEY);
      // Mirror the server-side invariant from both sides: assigning a
      // non-empty agent forces standalone=false, and setting
      // standalone=true forces agent_name=''. Without both rules a
      // separate-request sequence can land the cache in a contradictory
      // state where the row stays visually "linked" even after the user
      // marked it standalone (the classifier's first-branch-wins logic
      // would keep the agent badge).
      const assigningAgent =
        payload.agent_name !== undefined && payload.agent_name !== "";
      const markingStandalone = payload.standalone === true;
      qc.setQueryData<ApiKeyListItem[]>(API_KEYS_QUERY_KEY, (old) =>
        old?.map((k) =>
          k.id === id
            ? {
                ...k,
                agent_name: markingStandalone
                  ? ""
                  : (payload.agent_name ?? k.agent_name),
                standalone: assigningAgent
                  ? false
                  : (payload.standalone ?? k.standalone),
              }
            : k,
        ),
      );
      return { prev };
    },
    onError: (_error, _variables, context) => {
      if (context?.prev) {
        qc.setQueryData(API_KEYS_QUERY_KEY, context.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: API_KEYS_QUERY_KEY });
      qc.invalidateQueries({ queryKey: ["connected-agents"] });
    },
  });

  const { mutateAsync, isPending } = mutation;

  const updateKey = useCallback(
    async (id: string, payload: UpdateApiKeyPayload): Promise<boolean> => {
      if (isPending) return false;
      if (!id) return false;
      setPendingKeyId(id);
      try {
        await mutateAsync({ id, payload });
        return true;
      } catch (error) {
        console.error("[useUpdateApiKey] unexpected", error);
        // Surface a toast so silent failures (stale server, 405, 500,
        // network drop) stop looking like "the app logged me out."
        // Rollback already reverts the row visually; this tells the
        // user the server rejected the change. Classifier supplies
        // rate-limit / offline / 5xx copy with the "update that key"
        // context before falling through to the generic fallback.
        const classified = classifyMutationError(error, {
          action: t.errors.action.updateApiKey,
        });
        if (classified) {
          toast.error(classified.message, {
            autoDismissMs: classified.autoDismissMs,
          });
        } else {
          toast.error(t.apiKeys?.updateFailed ?? "Couldn't update key — retry");
        }
        return false;
      } finally {
        setPendingKeyId(null);
      }
    },
    [isPending, mutateAsync, toast, t.apiKeys, t.errors.action.updateApiKey],
  );

  return { updateKey, isPending, pendingKeyId };
}
