"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listOrphanCandidates,
  getUserOrphanStatus,
  reconcileOrphanInvites,
} from "@/lib/admin-client/client";
import type {
  AdminOrphanListParams,
  AdminReconcileRequest,
} from "@/lib/admin-client/types";
import { useLocale } from "@/i18n";

// --- Query keys ---

export const adminOrphansKey = ["admin", "waitlist", "orphans"] as const;

/** Per-user orphan-status cache key. The user-detail page reads
 * this to decide whether to show the reconcile card. */
export const adminUserOrphanStatusKey = (userId: string) =>
  ["admin", "users", userId, "orphan-status"] as const;

// --- Queries ---

/**
 * Lists the orphan cohort visible at `since`. The server requires
 * the since param, so callers MUST pass it. Typical value:
 * `2026-04-14T00:00:00Z` (start of the regression window). Limit
 * default 100, max 500 server-side.
 */
export function useAdminOrphans(params: AdminOrphanListParams) {
  return useQuery({
    queryKey: [...adminOrphansKey, params],
    queryFn: () => listOrphanCandidates(params),
    // 30s matches the other admin queries' staleTime; the cohort
    // doesn't move fast and a manual refetch is always one click
    // away via the "Refresh" button.
    staleTime: 30 * 1000,
  });
}

/**
 * Single-user orphan check for the admin user-detail page.
 * Returns undefined while loading — callers should gate the
 * reconcile-card render on `data?.is_orphan === true`.
 */
export function useAdminUserOrphanStatus(userId: string | null) {
  return useQuery({
    queryKey: userId
      ? adminUserOrphanStatusKey(userId)
      : ["admin", "users", "nil", "orphan-status"],
    queryFn: () => getUserOrphanStatus(userId!),
    enabled: !!userId,
    staleTime: 60 * 1000,
  });
}

// --- Mutations ---

/**
 * Batch repair for a specific user_ids list. Idempotent — re-running
 * on already-repaired users surfaces as "skipped: already_upgraded"
 * in the response, so there's no double-upgrade risk.
 *
 * On success we invalidate three keys **within the current tab**:
 *   1. the bulk orphan list
 *   2. each touched user's orphan-status cache
 *   3. each touched user's `/v1/admin/users/{id}` detail cache
 *
 * Cross-tab invalidation (e.g. another tab open on the same
 * user-detail page) is NOT handled here — the app wires a plain
 * `QueryClientProvider` without a BroadcastChannel bridge or
 * persisted-query layer, and the shared client disables
 * `refetchOnWindowFocus`, so other tabs only pick up the change
 * on a manual refresh, the next component remount, or a fresh
 * route navigation. That's acceptable for an operator tool;
 * adding broadcast sync would be a separate, whole-app change
 * rather than an orphans-only concern.
 */
export function useReconcileOrphans() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.admin.waitlist.orphans.reconcileError,
      errorAction: t.errors.action.adminReconcileOrphans,
    },
    mutationFn: (body: AdminReconcileRequest) => reconcileOrphanInvites(body),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: adminOrphansKey });
      for (const id of variables.user_ids) {
        qc.invalidateQueries({ queryKey: adminUserOrphanStatusKey(id) });
        // The user-detail page reads plan/invite_id from
        // /v1/admin/users/{id}; invalidate that too so the
        // reconciled state propagates without a refresh.
        qc.invalidateQueries({ queryKey: ["admin", "users", id] });
      }
    },
  });
}
