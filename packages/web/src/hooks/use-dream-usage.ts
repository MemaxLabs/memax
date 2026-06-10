"use client";

import { useQuery } from "@tanstack/react-query";
import { getMemaxClient } from "@/lib/memax-client";
import { useAuth } from "@/lib/auth";

/**
 * Bridge type — server has shipped DreamUsage but the public memax-sdk
 * npm package hasn't been republished yet to expose it. Mirrors the
 * shape returned by the dreams-usage endpoint. Same pattern as
 * `MemoryWithSeedFields` in use-memories.ts. Drop this once the SDK
 * publishes a new minor with `DreamUsage` exported + `dreams.usage()`
 * method on `DreamsResource`.
 */
export interface DreamUsage {
  scope: { kind: "personal" | "hub"; hubId?: string };
  used: number;
  limit: number;
  allowed: boolean;
  mode: "soft" | "hard";
  period_end: string;
  reset_at?: string;
}

/**
 * Query key for the dream-quota snapshot. Keyed by hubId so
 * switching active hubs in the UI doesn't show stale per-hub data
 * for a moment, and so invalidation after a successful trigger only
 * busts the affected hub's row.
 */
export function dreamUsageQueryKey(hubId: string | null) {
  return ["dream-usage", hubId ?? "personal"] as const;
}

/**
 * Reads the current dream-quota state for the caller's scope.
 *
 * Scope selection mirrors the server:
 *
 *   - hubIdOverride present → that hub's scope (team hub: ScopeHub;
 *     personal hub: that hub owner's personal scope).
 *   - Otherwise activeHubId from auth context.
 *   - Both empty → personal scope against the current user.
 *
 * Result drives two surfaces today: (1) a "X of Y dreams used this
 * month" indicator on hub Intelligence + you-profile dream sections;
 * (2) the `allowed` flag gates the "Dream now" button — soft mode
 * always reports allowed=true under finite-cap exhaustion (Phase 1
 * rollout), so the button only disables on hard mode at-cap or on a
 * disabled tier (limit=0). Loading + error states intentionally fall
 * back to "allow" so we never block a working button on a stale
 * fetch.
 *
 * staleTime = 30s: usage moves on the order of one trigger per hour
 * in the worst case (per-hub limits are 100/mo). We don't need a
 * fresher view than that for indicator copy, and triggers manually
 * invalidate the key on success via the dream-trigger mutation's
 * onSuccess handler.
 */
export function useDreamUsage(hubIdOverride?: string) {
  const { activeHubId } = useAuth();
  const hubId = hubIdOverride ?? activeHubId ?? null;
  return useQuery<DreamUsage>({
    queryKey: dreamUsageQueryKey(hubId),
    // Cast through unknown — `dreams.usage()` exists on the server-side
    // SDK source but the published memax-sdk hasn't republished yet.
    // Drop this cast when the SDK ships the method on DreamsResource.
    queryFn: () =>
      (
        getMemaxClient().dreams as unknown as {
          usage: (opts?: { hubId?: string }) => Promise<DreamUsage>;
        }
      ).usage(hubId ? { hubId } : undefined),
    staleTime: 30 * 1000,
    // Background polling is unnecessary — the trigger mutation
    // invalidates this key on success, and quota state otherwise
    // changes only on plan/seat changes (rare, server-pushed via
    // the events stream when we wire that up later).
    refetchInterval: false,
  });
}
