"use client";

import { useCallback, useEffect, useRef } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useStreamHealth } from "@/hooks/use-stream-health";

/**
 * Stream-health safety net — fires a bounded broad invalidation on
 * two triggers so degraded-stream moments don't leave the UI holding
 * stale data:
 *
 *   1. `reconnect` — when the stream transitions from non-connected
 *      to connected. Events that arrived while the stream was down
 *      were not delivered; a reconcile catches us back up.
 *   2. `visibility resume` — when the tab becomes visible again.
 *      Browsers throttle hidden tabs' SSE activity heavily (laptop
 *      sleep completely stops it), so on resume we cannot trust the
 *      cache even if the stream is technically `connected`.
 *
 * Per Codex #4 on the final plan review: a shared cooldown prevents
 * duplicate refetches when both triggers fire in quick succession
 * (e.g. unhide tab → focus refetch + stream reconnect), and the
 * whitelist is explicit and narrow — broad invalidation is cheap
 * per-query but adds up fast. Queries NOT on the whitelist don't
 * pay the reconcile cost; they refresh via normal SSE routes or
 * staleTime-gated refetches.
 *
 * PR B wires the hook into `MemaxEventBridge`. Consumers of the
 * whitelisted queries get reconcile freshness automatically; no
 * per-hook changes required. PR E later removes the per-hook
 * `refetchOnMount: 'always'` / `refetchOnWindowFocus: true`
 * overrides that predate this safety net.
 */

export const RECONCILE_COOLDOWN_MS = 8_000;

/**
 * Query prefixes that get a broad invalidation on reconcile. Keep
 * narrow — adding a key here means every tab focus and every
 * reconnect burns a network round-trip (gated by the 8s cooldown).
 * Extend deliberately.
 *
 * Membership rationale:
 *   Settings / inbox surfaces: api-keys, connected-agents,
 *     agent-configs, notifications. These are the surfaces whose
 *     "no refresh triggers until next mutation" staleness directly
 *     motivated the safety net.
 *   Primary memory UI: memory-lists, recent-memories, hub-summary,
 *     usage. These depend on SSE delivery of hub.memories.changed /
 *     hub.topics.changed; if the tab sleeps through those, reconcile
 *     is the only path that recovers them without waiting for
 *     staleTime or a fresh mutation. Added per Codex PR B review.
 *   Dreams: report + history. These also depend on SSE today; the
 *     coarse hub.dreams.changed event is the only driver.
 */
export const RECONCILE_WHITELIST: readonly QueryKey[] = [
  ["api-keys"],
  ["connected-agents"],
  ["agent-configs"],
  ["memory-lists"],
  ["recent-memories"],
  ["hub-summary"],
  ["usage"],
  ["dreams", "report"],
  ["dreams", "history"],
  ["notifications"],
];

export function useStreamReconcile() {
  const qc = useQueryClient();
  const { connection } = useStreamHealth();

  const prevConnectionRef = useRef(connection);
  const lastReconcileAtRef = useRef(0);

  const reconcile = useCallback(() => {
    const now = Date.now();
    if (now - lastReconcileAtRef.current < RECONCILE_COOLDOWN_MS) return;
    lastReconcileAtRef.current = now;
    RECONCILE_WHITELIST.forEach((queryKey) => {
      qc.invalidateQueries({ queryKey });
    });
  }, [qc]);

  // Trigger 1: reconnect transition. Use a ref to observe the
  // previous connection value without adding it as a dep; effect
  // re-runs exactly when `connection` changes.
  useEffect(() => {
    const prev = prevConnectionRef.current;
    prevConnectionRef.current = connection;
    if (prev !== "connected" && connection === "connected") {
      reconcile();
    }
  }, [connection, reconcile]);

  // Trigger 2: visibility resume. Fires on every show/hide
  // transition; cooldown debounces rapid alt-tabs.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const onVisibility = () => {
      if (document.visibilityState === "visible") {
        reconcile();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [reconcile]);
}
