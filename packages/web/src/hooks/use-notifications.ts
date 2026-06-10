"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import type {
  Notification,
  NotificationBulkQuery,
  NotificationListQuery,
  NotificationListResponse,
  NotificationResolveAction,
  NotificationStatus,
  NotificationSummary,
} from "memax-sdk";
import { isNeedsActionKind } from "@/lib/notification-kinds";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useLocale } from "@/i18n";

/**
 * Phase 4 Chunk 1 — React Query hooks for the notifications API
 * surface landed in Phase 3b. The inbox, bar, and badge all read
 * from here once the Phase 4 migration flips the consumers in
 * chunks 2 and 3. The legacy reviews API stays mounted in parallel
 * until Phase 6 removes it.
 *
 * Scoping contract (matches the server's §4.4 union visibility):
 *
 *   - Default (no `hub` arg) → UNSCOPED. The server returns the
 *     union of every hub the caller is a member of PLUS every
 *     user-audience row addressed to them (dream_run_completed,
 *     hub_invite, system_notice, etc.). This is the right default
 *     for the inbox + badge because a user's notifications do NOT
 *     disappear when they switch the active hub in the UI — a
 *     hub_invite to join Hub B is still visible while the user is
 *     browsing Hub A.
 *
 *   - Explicit `{ hub: "<hub-id>" }` → STRICT hub narrowing. Only
 *     rows where hub_id matches; user-audience rows are excluded
 *     per the Phase 3a follow-up contract. This is the right
 *     scope for the (currently theoretical) "what is happening in
 *     this hub" view — a hub-settings tab, a hub detail page,
 *     etc.
 *
 * An earlier cut of these hooks always passed `hub: activeHubId`,
 * which silently contradicted the audience model: user-audience
 * rows like dream_run_completed and hub_invite disappeared from
 * the inbox whenever the user had an active hub selected. The
 * scoping rule now matches the server contract.
 */

export type { Notification, NotificationSummary };

/**
 * notificationsQueryPrefix — stable invalidation prefix used by the
 * SSE event bridge. Invalidating `["notifications"]` from the bridge
 * catches every cached view (unscoped, hub-scoped, per-kind,
 * summary, etc.) in one call, which is what we want for event
 * convergence.
 */
export const notificationsQueryPrefix = (): readonly ["notifications"] =>
  ["notifications"] as const;

/** Cache-bucket key for the scope axis of a list query. */
const scopeKey = (hub: string | undefined) => hub ?? "all";

const notificationsQueryKey = (query: NotificationListQuery | undefined) =>
  ["notifications", "list", scopeKey(query?.hub), query ?? {}] as const;

const summaryQueryKey = (hub: string | undefined) =>
  ["notifications", "summary", scopeKey(hub)] as const;

type NotificationListQueryKey = readonly [
  "notifications",
  "list",
  string,
  NotificationListQuery | Record<string, never>,
];

type NotificationSummaryQueryKey = readonly [
  "notifications",
  "summary",
  string,
];

interface NotificationMutationSnapshot {
  lists: Array<{
    key: readonly unknown[];
    data: NotificationListResponse | undefined;
  }>;
  summaries: Array<{
    key: readonly unknown[];
    data: NotificationSummary | undefined;
  }>;
}

function matchesNotificationListQuery(
  notification: Notification,
  query: NotificationListQuery | undefined,
): boolean {
  if (query?.hub && notification.hub_id !== query.hub) {
    return false;
  }
  if (query?.kind?.length && !query.kind.includes(notification.kind)) {
    return false;
  }
  if (query?.status === "resolved") {
    return false;
  }
  if (query?.status && query.status !== "pending") {
    return false;
  }
  if (query?.unseenOnly && notification.seen_at) {
    return false;
  }
  if (query?.since) {
    const since = Date.parse(query.since);
    const createdAt = Date.parse(notification.created_at);
    if (
      Number.isFinite(since) &&
      Number.isFinite(createdAt) &&
      createdAt < since
    ) {
      return false;
    }
  }
  return true;
}

function matchesNotificationSummaryScope(
  notification: Notification,
  scope: string,
): boolean {
  if (scope === "all") return true;
  return notification.hub_id === scope;
}

function updateSummaryAfterPendingRemoval(
  summary: NotificationSummary,
  notification: Notification,
): NotificationSummary {
  const nextKind = summary.by_kind[notification.kind] ?? {
    pending: 0,
    unseen: 0,
  };
  const byKind = {
    ...summary.by_kind,
    [notification.kind]: {
      pending: Math.max(0, nextKind.pending - 1),
      unseen: notification.seen_at
        ? nextKind.unseen
        : Math.max(0, nextKind.unseen - 1),
    },
  };
  if (isNeedsActionKind(notification.kind)) {
    return {
      ...summary,
      needs_action_pending: Math.max(0, summary.needs_action_pending - 1),
      by_kind: byKind,
    };
  }
  return {
    ...summary,
    updates_pending: Math.max(0, summary.updates_pending - 1),
    updates_unseen: notification.seen_at
      ? summary.updates_unseen
      : Math.max(0, summary.updates_unseen - 1),
    by_kind: byKind,
  };
}

function updateSummaryAfterSeen(
  summary: NotificationSummary,
  notification: Notification,
): NotificationSummary {
  const nextKind = summary.by_kind[notification.kind] ?? {
    pending: 0,
    unseen: 0,
  };
  const byKind = {
    ...summary.by_kind,
    [notification.kind]: {
      pending: nextKind.pending,
      unseen: Math.max(0, nextKind.unseen - 1),
    },
  };
  if (isNeedsActionKind(notification.kind)) {
    return {
      ...summary,
      by_kind: byKind,
    };
  }
  return {
    ...summary,
    updates_unseen: Math.max(0, summary.updates_unseen - 1),
    by_kind: byKind,
  };
}

function optimisticallyRemovePendingNotification(
  notificationID: string,
): NotificationMutationSnapshot | null {
  const lists = queryClient.getQueriesData<NotificationListResponse>({
    queryKey: ["notifications", "list"],
  });
  let notificationToRemove: Notification | null = null;
  for (const [, data] of lists) {
    const match = data?.notifications.find(
      (entry) => entry.id === notificationID,
    );
    if (match) {
      notificationToRemove = match;
      break;
    }
  }
  if (!notificationToRemove) return null;

  const summaries = queryClient.getQueriesData<NotificationSummary>({
    queryKey: ["notifications", "summary"],
  });
  const snapshot: NotificationMutationSnapshot = {
    lists: lists.map(([key, data]) => ({ key, data })),
    summaries: summaries.map(([key, data]) => ({ key, data })),
  };

  for (const [key, data] of lists) {
    const typedKey = key as NotificationListQueryKey;
    const query = typedKey[3] as NotificationListQuery | undefined;
    if (!matchesNotificationListQuery(notificationToRemove, query)) continue;
    queryClient.setQueryData<NotificationListResponse>(key, (current) => {
      if (!current) return current;
      const nextNotifications = current.notifications.filter(
        (entry) => entry.id !== notificationID,
      );
      if (nextNotifications.length === current.notifications.length) {
        return current;
      }
      return {
        ...current,
        notifications: nextNotifications,
      };
    });
  }

  for (const [key, data] of summaries) {
    if (!data) continue;
    const typedKey = key as NotificationSummaryQueryKey;
    const scope = typedKey[2];
    if (!matchesNotificationSummaryScope(notificationToRemove, scope)) continue;
    queryClient.setQueryData<NotificationSummary>(key, (current) =>
      current
        ? updateSummaryAfterPendingRemoval(current, notificationToRemove)
        : current,
    );
  }

  return snapshot;
}

function restoreNotificationMutationSnapshot(
  snapshot: NotificationMutationSnapshot | null | undefined,
) {
  if (!snapshot) return;
  for (const entry of snapshot.lists) {
    queryClient.setQueryData(entry.key, entry.data);
  }
  for (const entry of snapshot.summaries) {
    queryClient.setQueryData(entry.key, entry.data);
  }
}

function optimisticallyMarkNotificationSeen(
  notificationID: string,
): NotificationMutationSnapshot | null {
  const lists = queryClient.getQueriesData<NotificationListResponse>({
    queryKey: ["notifications", "list"],
  });
  let notificationToMark: Notification | null = null;
  for (const [, data] of lists) {
    const match = data?.notifications.find(
      (entry) => entry.id === notificationID,
    );
    if (match) {
      notificationToMark = match;
      break;
    }
  }
  if (!notificationToMark || notificationToMark.seen_at) return null;

  const summaries = queryClient.getQueriesData<NotificationSummary>({
    queryKey: ["notifications", "summary"],
  });
  const snapshot: NotificationMutationSnapshot = {
    lists: lists.map(([key, data]) => ({ key, data })),
    summaries: summaries.map(([key, data]) => ({ key, data })),
  };
  const seenAt = new Date().toISOString();

  for (const [key, data] of lists) {
    const typedKey = key as NotificationListQueryKey;
    const query = typedKey[3] as NotificationListQuery | undefined;
    if (!matchesNotificationListQuery(notificationToMark, query)) continue;
    queryClient.setQueryData<NotificationListResponse>(key, (current) => {
      if (!current) return current;
      let changed = false;
      const notifications = current.notifications.map((entry) => {
        if (entry.id !== notificationID || entry.seen_at) return entry;
        changed = true;
        return { ...entry, seen_at: seenAt };
      });
      return changed ? { ...current, notifications } : current;
    });
  }

  for (const [key, data] of summaries) {
    if (!data) continue;
    const typedKey = key as NotificationSummaryQueryKey;
    const scope = typedKey[2];
    if (!matchesNotificationSummaryScope(notificationToMark, scope)) continue;
    queryClient.setQueryData<NotificationSummary>(key, (current) =>
      current ? updateSummaryAfterSeen(current, notificationToMark) : current,
    );
  }

  return snapshot;
}

/**
 * useNotifications lists notifications visible to the caller.
 * Defaults to pending rows across the full visibility union (see
 * the scoping contract above). Pass a NotificationListQuery to
 * narrow — common examples:
 *
 *   useNotifications()                      // inbox + badge default
 *   useNotifications({ unseenOnly: true })  // bar push hydration
 *   useNotifications({ hub: hubId })        // "what is in this hub"
 *   useNotifications({ kind: ["review_contradiction"] })
 */
export function useNotifications(query?: NotificationListQuery) {
  return useQuery<NotificationListResponse>({
    queryKey: notificationsQueryKey(query),
    queryFn: () => getMemaxClient().notifications.list(query),
    staleTime: 30 * 1000,
  });
}

export function prefetchNotifications(query?: NotificationListQuery) {
  return queryClient.prefetchQuery<NotificationListResponse>({
    queryKey: notificationsQueryKey(query),
    queryFn: () => getMemaxClient().notifications.list(query),
    staleTime: 30 * 1000,
  });
}

/**
 * useNotificationSummary reads the canonical /summary shape.
 * Single source of truth for the top-right badge dot
 * (`needs_action_pending`), the Updates header count
 * (`updates_pending` / `updates_unseen`), and the settings > dreams
 * contradictions counter (`by_kind.review_contradiction.pending`).
 * Every supported kind is pre-populated with zero counts so
 * consumers iterate by_kind[kind] without null-checking.
 *
 * Unscoped by default so the badge reflects the full visibility
 * surface — a pending hub invite to another hub should still light
 * up the dot. Callers that genuinely want "the summary for this
 * hub" (e.g. a hypothetical hub-detail tab) can pass
 * `{ hub: hubId }` explicitly.
 */
export function useNotificationSummary(opts?: { hub?: string }) {
  const hub = opts?.hub;
  return useQuery<NotificationSummary>({
    queryKey: summaryQueryKey(hub),
    queryFn: () => getMemaxClient().notifications.summary(hub),
    staleTime: 30 * 1000,
  });
}

/**
 * useResolveNotification wraps the notifications resolve mutation.
 * Decision-only: the server refuses receipt kinds with
 * `400 resolve_not_applicable_to_receipt`. Consumers should switch
 * on `notification.kind` before rendering the resolve affordance.
 *
 * No hub passthrough: the server's notification visibility check
 * walks the caller's full hub-membership list, so pinning a
 * specific hub on the mutation call is both unnecessary and
 * misleading (it would imply resolve is hub-scoped when the
 * visibility check isn't).
 *
 * No optimistic update in this cut — the SSE invalidation path
 * (notification.resolved → bridge invalidation → list re-fetch) is
 * fast enough for the first pass and avoids needing a typed
 * per-kind action→resolution map on the client side. Chunk 3 adds
 * optimistic behavior if the flicker is noticeable in practice.
 */
export function useResolveNotification() {
  const { t } = useLocale();
  return useMutation<
    Awaited<
      ReturnType<ReturnType<typeof getMemaxClient>["notifications"]["resolve"]>
    >,
    Error,
    { id: string; action: NotificationResolveAction },
    NotificationMutationSnapshot | null
  >({
    meta: {
      errorMessage: t.toast.reviewFailed,
      errorAction: t.errors.action.resolveNotification,
    },
    mutationFn: ({ id, action }) =>
      getMemaxClient().notifications.resolve(id, action),
    onMutate: async ({ id }) => {
      await queryClient.cancelQueries({ queryKey: notificationsQueryPrefix() });
      return optimisticallyRemovePendingNotification(id);
    },
    onError: (_error, _variables, context) => {
      restoreNotificationMutationSnapshot(context);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
      queryClient.invalidateQueries({ queryKey: ["dreams", "report"] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      // The legacy reviews API is still mounted alongside
      // notifications and the server mirrors the resolve into the
      // reviews table, so keep the legacy cache in sync for any
      // remaining consumers.
      queryClient.invalidateQueries({ queryKey: ["reviews"] });
    },
  });
}

/**
 * useNotificationMarkSeen is idempotent. Called by the bar push
 * renderer the moment a notification is shown so the same row
 * cannot re-push on reconnect or first-load hydration. Phase 4
 * Chunk 2 wires this into the bar card mount path.
 *
 * No hub passthrough — same reason as useResolveNotification.
 */
export function useNotificationMarkSeen() {
  return useMutation<
    unknown,
    Error,
    string,
    NotificationMutationSnapshot | null
  >({
    mutationFn: (id) => getMemaxClient().notifications.markSeen(id),
    onMutate: async (id) => {
      await queryClient.cancelQueries({ queryKey: notificationsQueryPrefix() });
      return optimisticallyMarkNotificationSeen(id);
    },
    onError: (_error, _id, context) => {
      restoreNotificationMutationSnapshot(context);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

/**
 * useNotificationDismiss is the receipt-only dismiss path. Server
 * refuses decision kinds with `400 dismiss_not_applicable_to_decision`.
 * Consumers must check `notification.kind` against the decision
 * kind set (`NEEDS_ACTION_KINDS` from memax-sdk) before
 * exposing this action in the UI.
 */
export function useNotificationDismiss() {
  const { t } = useLocale();
  return useMutation<
    unknown,
    Error,
    string,
    NotificationMutationSnapshot | null
  >({
    meta: {
      errorMessage: t.toast.reviewFailed,
      errorAction: t.errors.action.dismissNotification,
    },
    mutationFn: (id) => getMemaxClient().notifications.dismiss(id),
    onMutate: async (id) => {
      await queryClient.cancelQueries({ queryKey: notificationsQueryPrefix() });
      return optimisticallyRemovePendingNotification(id);
    },
    onError: (_error, _id, context) => {
      restoreNotificationMutationSnapshot(context);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

/**
 * useBulkNotificationSeen / useBulkNotificationDismiss power the
 * Updates-bucket header "Mark all as seen" / "Clear all" buttons.
 * Server refuses any decision kind in the request body.
 *
 * Bulk endpoints DO support a `hub` filter (that's the whole point
 * of the Updates header — bulk-clear everything in the current
 * hub) so callers pass it in the NotificationBulkQuery body.
 * Unlike the list default, bulk without a hub is unusual enough
 * that we expect callers to be explicit.
 */
export function useBulkNotificationSeen() {
  return useMutation<unknown, Error, NotificationBulkQuery>({
    mutationFn: (query) => getMemaxClient().notifications.bulkSeen(query),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

export function useBulkNotificationDismiss() {
  return useMutation<unknown, Error, NotificationBulkQuery>({
    mutationFn: (query) => getMemaxClient().notifications.bulkDismiss(query),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

/**
 * useViewChecklistItem stamps payload.items[itemId].viewed_at on a
 * super-notif row (plan 18 §4.3). Idempotent — calling twice is a
 * no-op. Optimistic update patches the cached notification so the UI
 * paints the seen state before the network round-trip lands. Snapshot
 * + rollback on error keeps the cache honest if the server rejects
 * the call (terminal row, auth expiry, etc.) — codex P1 M3.
 */
export function useViewChecklistItem() {
  return useMutation<
    unknown,
    Error,
    { notificationId: string; itemId: string },
    NotificationMutationSnapshot | null
  >({
    mutationFn: ({ notificationId, itemId }) =>
      getMemaxClient().notifications.viewItem(notificationId, itemId),
    onMutate: async ({ notificationId, itemId }) => {
      await queryClient.cancelQueries({ queryKey: notificationsQueryPrefix() });
      const snapshot = snapshotChecklistCache();
      patchChecklistItemInCache(notificationId, itemId, {
        viewed_at: new Date().toISOString(),
      });
      return snapshot;
    },
    onError: (_error, _variables, context) => {
      restoreNotificationMutationSnapshot(context);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

/**
 * useCompleteChecklistItem stamps completed_at + viewed_at on a
 * checklist item. Refused server-side for digest kinds (the wire
 * contract is checklist-only) and for items whose locked_by
 * dependencies are unfinished.
 *
 * When the server reports auto_resolved=true the parent row has been
 * flipped to status=resolved + resolution=applied_auto in the same
 * round-trip — the celebration overlay can fire off the HTTP response
 * without waiting for the SSE notification.resolved that arrives
 * shortly after.
 *
 * Snapshot + rollback on error keeps the cache honest if the server
 * refuses the completion (locked dep, terminal row, auth expiry).
 * Codex P1 M3.
 */
export function useCompleteChecklistItem() {
  return useMutation<
    Awaited<
      ReturnType<
        ReturnType<typeof getMemaxClient>["notifications"]["completeItem"]
      >
    >,
    Error,
    { notificationId: string; itemId: string },
    NotificationMutationSnapshot | null
  >({
    // Suppress the query-client global error toast. This hook is a
    // reconciliation primitive — fired both by user clicks (the
    // first_dream / first_hub_invite CTA flow) AND by auto-tick
    // effects (the connect_agent / five_memories live-fallback
    // write-through). Both paths handle the failure visually via
    // the onError snapshot rollback below; the noisy generic 400
    // toast ("That request couldn't be processed. Refresh and try
    // again.") is actively misleading when a dream HAS just
    // triggered successfully but the locked_by gate hasn't caught
    // up. The dependent UI already conveys "not yet" through the
    // un-checked state — no extra surface needed.
    meta: { skipGlobalToast: true },
    mutationFn: ({ notificationId, itemId }) =>
      getMemaxClient().notifications.completeItem(notificationId, itemId),
    onMutate: async ({ notificationId, itemId }) => {
      await queryClient.cancelQueries({ queryKey: notificationsQueryPrefix() });
      const snapshot = snapshotChecklistCache();
      const now = new Date().toISOString();
      patchChecklistItemInCache(notificationId, itemId, {
        viewed_at: now,
        completed_at: now,
      });
      return snapshot;
    },
    onError: (_error, _variables, context) => {
      restoreNotificationMutationSnapshot(context);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
    },
  });
}

/**
 * useRestartOnboarding fires the SDK onboarding.restart() call.
 * Server resolves any current pending checklist as dismissed and
 * emits a fresh checklist at the next version. Returns the new
 * notification.
 *
 * UI surface: Settings → Getting started row, with "Start" or
 * "Restart" copy depending on `useOnboardingState`. Server is
 * rate-limited to 3 calls per 24h.
 */
export function useRestartOnboarding() {
  return useMutation<Notification, Error>({
    mutationFn: () => getMemaxClient().onboarding.restart(),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: notificationsQueryPrefix() });
      queryClient.invalidateQueries({ queryKey: ["onboarding", "state"] });
    },
  });
}

/**
 * useOnboardingState queries the small Settings-only surface that
 * tells the UI whether the user has any prior onboarding row.
 * Distinguishes "never had a checklist" (Start) from "had one and
 * dismissed/resolved/expired" (Restart) — codex P3 L1.
 */
// Local mirror of the SDK's OnboardingState (resources/onboarding.ts).
// Defined here rather than imported from a memax-sdk subpath because
// the SDK's index.ts doesn't re-export OnboardingState yet and tsc
// flags the resolved subpath as non-portable. Wire shape is tiny and
// stable; will swap to a top-level SDK import when the re-export lands.
interface OnboardingStateResult {
  has_prior: boolean;
  current_version: number;
  current_status?: NotificationStatus;
}

export function useOnboardingState() {
  return useQuery<OnboardingStateResult>({
    queryKey: ["onboarding", "state"] as const,
    queryFn: () =>
      getMemaxClient().onboarding.state() as Promise<OnboardingStateResult>,
    staleTime: 5 * 60 * 1000,
  });
}

/**
 * snapshotChecklistCache captures every cached `["notifications", ...]`
 * query's data pointer so the optimistic patch from
 * patchChecklistItemInCache can be rolled back on mutation error. We
 * snapshot the full list payload (cheap — the cache shape is small)
 * rather than just the patched item, because subsequent SSE patches
 * during the in-flight mutation could otherwise tangle the rollback.
 */
function snapshotChecklistCache(): NotificationMutationSnapshot | null {
  const queries = queryClient.getQueriesData({
    queryKey: notificationsQueryPrefix(),
  });
  const lists: NotificationMutationSnapshot["lists"] = [];
  for (const [key, value] of queries) {
    if (!value || typeof value !== "object") continue;
    if (Array.isArray((value as { notifications?: unknown }).notifications)) {
      lists.push({
        key,
        data: value as NotificationListResponse,
      });
    }
  }
  if (lists.length === 0) return null;
  return { lists, summaries: [] };
}

/**
 * patchChecklistItemInCache walks every cached `notifications` query
 * and updates the named item in payload.items[]. Used by the
 * view/complete optimistic paths AND by the SSE handler that consumes
 * `notification.updated change=item_updated` events — so HTTP-driven
 * and event-driven updates take the same path.
 *
 * The optional `qc` argument lets the event-routing patch hook pass
 * the runtime-injected QueryClient (codex P1 L1: keep the route
 * abstraction honest for tests + future providers). Production
 * callers pass nothing and the helper falls back to the singleton.
 */
export function patchChecklistItemInCache(
  notificationId: string,
  itemId: string,
  patch: {
    viewed_at?: string;
    completed_at?: string;
    progress?: { current: number; target: number };
  },
  qc: typeof queryClient = queryClient,
) {
  const queries = qc.getQueriesData({
    queryKey: notificationsQueryPrefix(),
  });
  for (const [key, value] of queries) {
    if (!value || typeof value !== "object") continue;
    const data = value as { notifications?: Notification[] };
    if (!Array.isArray(data.notifications)) continue;
    let changed = false;
    const next = data.notifications.map((n) => {
      if (n.id !== notificationId) return n;
      const payload = (n.payload ?? {}) as {
        items?: Array<Record<string, unknown> & { id?: string }>;
      };
      if (!Array.isArray(payload.items)) return n;
      const idx = payload.items.findIndex((item) => item?.id === itemId);
      if (idx < 0) return n;
      const oldItem = payload.items[idx] ?? {};
      // Skip undefined fields so an SSE /view event arriving after an
      // optimistic /complete doesn't wipe completed_at / progress
      // (codex P1 pass-2 M1). Server only stamps the timestamps that
      // changed; spreading undefined would erase the others.
      const defined: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(patch)) {
        if (v !== undefined) defined[k] = v;
      }
      const nextItem = { ...oldItem, ...defined };
      const nextItems = payload.items.slice();
      nextItems[idx] = nextItem;
      changed = true;
      return { ...n, payload: { ...payload, items: nextItems } };
    });
    if (changed) {
      qc.setQueryData(key, { ...data, notifications: next });
    }
  }
}
