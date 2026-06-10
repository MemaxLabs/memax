"use client";

/**
 * Connected agents — list, update, and disconnect.
 *
 * Disconnect feedback stays inline in Settings (same rule as
 * `useAgentConfigForget`). The hook exposes structured feedback
 * state + a clear function; the `AgentConfigsSection` component
 * renders local copy next to the action instead of routing through
 * `useBarToast`. Bar toasts are reserved for global / background
 * notification kinds, not Settings CRUD.
 *
 * Rollback rule mirrors `useAgentConfigForget`:
 *
 *   applyDisconnect throws `AgentDisconnectIncompleteError` only when
 *   the server reports `cascade_failed` — a real failure where the
 *   agent row is still present on the server and the optimistic
 *   removal must be rolled back. `not_found` is the idempotent
 *   success path: the agent is already gone, the client's optimistic
 *   removal was correct, no throw.
 */

import { useCallback, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { ConnectedAgentWithStats, DisconnectAgentResult } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useStreamHealth } from "@/hooks/use-stream-health";
import { useLocale } from "@/i18n";

const CONNECTED_AGENTS_QUERY_KEY = ["connected-agents"] as const;
const AGENT_CONFIGS_QUERY_KEY = ["agent-configs"] as const;
const API_KEYS_QUERY_KEY = ["api-keys"] as const;

export function useConnectedAgents() {
  // Stream-health-conditional freshness — matches use-api-keys.ts.
  // See that file's block for the full rationale: when the stream
  // is healthy, `agent.changed` SSE events (PR C) invalidate this
  // cache — directly when an agent is upserted/disconnected, and
  // transitively when a key mutation transfers attribution (PR C
  // handlers emit agent.changed for both the prior and new agent).
  // `key.changed` itself only invalidates ["api-keys"]; the key_count
  // refresh flows through the accompanying agent.changed emit. We
  // fall back to mount + focus refetch when the stream is degraded
  // so navigation and tab-return still catch cross-device mutations.
  const { connection } = useStreamHealth();
  const streamDown = connection !== "connected";

  return useQuery<ConnectedAgentWithStats[]>({
    queryKey: CONNECTED_AGENTS_QUERY_KEY,
    queryFn: () => getMemaxClient().agents.list(),
    gcTime: 30 * 60 * 1000,
    refetchOnMount: streamDown ? "always" : true,
    refetchOnWindowFocus: streamDown,
  });
}

export function useUpdateAgent() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.updateAgent,
    },
    mutationFn: ({
      slug,
      displayName,
      icon,
    }: {
      slug: string;
      displayName?: string;
      icon?: string;
    }) =>
      getMemaxClient().agents.update(slug, {
        displayName,
        icon,
      }),
    onMutate: async ({ slug, displayName, icon }) => {
      await qc.cancelQueries({ queryKey: CONNECTED_AGENTS_QUERY_KEY });
      const prev = qc.getQueryData<ConnectedAgentWithStats[]>(
        CONNECTED_AGENTS_QUERY_KEY,
      );
      qc.setQueryData<ConnectedAgentWithStats[]>(
        CONNECTED_AGENTS_QUERY_KEY,
        (old) =>
          old?.map((a) =>
            a.agent_name === slug
              ? {
                  ...a,
                  display_name:
                    displayName !== undefined ? displayName : a.display_name,
                  icon: icon !== undefined ? icon : a.icon,
                }
              : a,
          ),
      );
      return { prev };
    },
    onError: (_err, _vars, context) => {
      if (context?.prev)
        qc.setQueryData(CONNECTED_AGENTS_QUERY_KEY, context.prev);
    },
    onSettled: () =>
      qc.invalidateQueries({ queryKey: CONNECTED_AGENTS_QUERY_KEY }),
  });
}

/**
 * Sentinel thrown by applyDisconnect when the server reports
 * `cascade_failed`. Carries the full result so the outer catch can
 * still surface counts if a future reason lands there. `not_found`
 * never reaches this throw — it takes the resolved path as
 * idempotent success.
 */
export class AgentDisconnectIncompleteError extends Error {
  constructor(public readonly result: DisconnectAgentResult) {
    super("agent_disconnect_incomplete");
    this.name = "AgentDisconnectIncompleteError";
  }
}

async function applyDisconnect(slug: string): Promise<DisconnectAgentResult> {
  const result = await getMemaxClient().agents.disconnect(slug);
  // Disconnect is single-entity — skipped has 0 or 1 entries.
  //   not_found       → target reached; return normally, no rollback.
  //   cascade_failed  → row still on server; throw so onError fires.
  const skip = result.skipped[0];
  if (skip?.reason === "cascade_failed") {
    throw new AgentDisconnectIncompleteError(result);
  }
  return result;
}

/**
 * Structured feedback for the Settings UI to render inline near the
 * disconnect button. The UI owns i18n; the hook tracks the raw
 * outcome kind + counts.
 *
 * - `success`: cascade committed. Shows "Disconnected claude-code —
 *   revoked N keys, forgot M configs".
 * - `not_found`: idempotent success (agent was already gone). Shows
 *   "Already disconnected" or nothing, UI's choice.
 * - `error`: cascade_failed or network/parse. Rollback has already
 *   restored the agent row to the list; UI shows retry copy.
 */
export type AgentDisconnectFeedbackKind = "success" | "not_found" | "error";

export interface AgentDisconnectFeedback {
  kind: AgentDisconnectFeedbackKind;
  slug: string;
  keysRevoked: number;
  configsTombstoned: number;
}

export function useDisconnectAgent() {
  const qc = useQueryClient();
  const [feedback, setFeedback] = useState<AgentDisconnectFeedback | null>(
    null,
  );

  const mutation = useMutation<
    DisconnectAgentResult,
    Error,
    { slug: string },
    { prev: ConnectedAgentWithStats[] | undefined }
  >({
    mutationFn: ({ slug }) => applyDisconnect(slug),
    onMutate: async ({ slug }) => {
      await qc.cancelQueries({ queryKey: CONNECTED_AGENTS_QUERY_KEY });
      const prev = qc.getQueryData<ConnectedAgentWithStats[]>(
        CONNECTED_AGENTS_QUERY_KEY,
      );
      qc.setQueryData<ConnectedAgentWithStats[]>(
        CONNECTED_AGENTS_QUERY_KEY,
        (old) => old?.filter((a) => a.agent_name !== slug),
      );
      return { prev };
    },
    onError: (_error, _variables, context) => {
      // Fires on AgentDisconnectIncompleteError (cascade_failed) AND on
      // generic mutation failures. Restores the snapshot so the agent
      // row reappears immediately instead of waiting for onSettled
      // refetch. not_found never lands here — applyDisconnect returns
      // resolved for that reason.
      if (context?.prev) {
        qc.setQueryData(CONNECTED_AGENTS_QUERY_KEY, context.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: CONNECTED_AGENTS_QUERY_KEY });
      qc.invalidateQueries({ queryKey: AGENT_CONFIGS_QUERY_KEY });
      qc.invalidateQueries({ queryKey: API_KEYS_QUERY_KEY });
    },
  });

  const { mutateAsync, isPending } = mutation;

  /**
   * Disconnect a single agent. Returns `true` on success (real or
   * idempotent), `false` on single-flight reject or cascade failure.
   * Writes `feedback` state for the Settings UI to render inline.
   *
   * Callers wrap this in `useDestructiveAction.run(() => ...)` inside
   * the component layer.
   */
  const disconnectAgent = useCallback(
    async (slug: string): Promise<boolean> => {
      if (isPending) return false;
      if (!slug) return false;

      try {
        const result = await mutateAsync({ slug });
        // result.disconnected === true: real cascade success.
        // result.disconnected === false with not_found skip: idempotent
        // success (applyDisconnect returned normally for this branch).
        const skip = result.skipped[0];
        if (result.disconnected) {
          setFeedback({
            kind: "success",
            slug,
            keysRevoked: result.keys_revoked,
            configsTombstoned: result.configs_tombstoned,
          });
        } else if (skip?.reason === "not_found") {
          setFeedback({
            kind: "not_found",
            slug,
            keysRevoked: 0,
            configsTombstoned: 0,
          });
        } else {
          // Shouldn't happen — applyDisconnect guarantees this branch
          // is unreachable for known reasons. Guard anyway.
          setFeedback({
            kind: "error",
            slug,
            keysRevoked: 0,
            configsTombstoned: 0,
          });
          return false;
        }
        return true;
      } catch (error) {
        // onError already restored the snapshot. Whether the throw
        // was an AgentDisconnectIncompleteError (cascade_failed) or a
        // generic network error, the user sees the same retry copy.
        if (!(error instanceof AgentDisconnectIncompleteError)) {
          console.error("[useDisconnectAgent] unexpected", error);
        }
        setFeedback({
          kind: "error",
          slug,
          keysRevoked: 0,
          configsTombstoned: 0,
        });
        return false;
      }
    },
    [isPending, mutateAsync],
  );

  const clearFeedback = useCallback(() => setFeedback(null), []);

  return { disconnectAgent, isPending, feedback, clearFeedback };
}
