"use client";

/**
 * useV2RouteHubSync — keeps `useAuth().activeHubId` aligned with the
 * URL slug on shell-v2 routes.
 *
 * Shell-v2 routes carry the hub identity in the URL path
 * (`/h/<slug>/memories`). The rest of the app (BarProvider's "active
 * hub" awareness, useTopics, useNotificationSummary, hub badge in the
 * top-right v1 chrome, etc.) reads from `auth.activeHubId`. This hook
 * bridges the two: when the route slug resolves to a hub the user is
 * a member of, we call `switchHub(targetHubId)` so existing components
 * see a consistent active hub regardless of how they got here.
 *
 * The slug → hub UUID resolution is identical to `<LeftRail>`'s — both
 * use `resolveHubFromSlug({ slug, hubs })`. Returns null if the slug
 * doesn't resolve (route 404 path); caller decides what to do.
 *
 * Three observable states callers branch on:
 *
 *   - `isAuthLoading=true`         — `/auth/me` in flight; hubs not yet
 *                                     known. Render a loading skeleton.
 *   - `isAuthLoading=false &&
 *      resolvedHubId===null`       — auth done; slug didn't match any
 *                                     hub the user belongs to → render
 *                                     a real 404 / recovery surface.
 *   - `isSynced=true`              — safe to render route content.
 */

import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { resolveHubFromSlug } from "@/lib/hub-from-slug";

export interface V2RouteHubSync {
  /** The hub UUID resolved from the URL slug. `null` if no match. */
  resolvedHubId: string | null;
  /**
   * `true` while `/auth/me` is in flight. While loading,
   * `resolvedHubId === null` doesn't mean "not found" yet — the hubs
   * list might not have arrived. Callers use this to differentiate
   * loading skeleton vs 404.
   */
  isAuthLoading: boolean;
  /**
   * `true` once `auth.activeHubId === resolvedHubId`. Callers should
   * gate route content on this — rendering before sync completes can
   * briefly show data from the previous active hub under the new URL
   * (e.g., personal memories on a team-hub URL during a deep link).
   * `null` slug or unresolved slug → `false` (caller can decide
   * whether to render a 404 vs wait).
   */
  isSynced: boolean;
}

export function useV2RouteHubSync(slug: string): V2RouteHubSync {
  const { hubs, activeHubId, switchHub, loading } = useAuth();
  const resolvedHubId = resolveHubFromSlug({ slug, hubs });
  const isSynced = resolvedHubId !== null && resolvedHubId === activeHubId;

  useEffect(() => {
    if (!resolvedHubId) return;
    if (resolvedHubId === activeHubId) return;
    switchHub(resolvedHubId);
  }, [resolvedHubId, activeHubId, switchHub]);

  return { resolvedHubId, isAuthLoading: loading, isSynced };
}
