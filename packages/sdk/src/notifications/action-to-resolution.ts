// =============================================================================
// memax-sdk — kind/action → resolution lookup (plan 17 §6.4, plan 18 §4.4)
// =============================================================================
//
// Single source of truth for the per-kind `/resolve` action allow-list
// on the TS side. Mirrors the Go map at
// packages/server/internal/handler/notifications.go `notificationResolveAllowList`.
//
// Why this exists:
//   - Pages that compose a resolve action (memory-row Resolve menu,
//     inbox decision buttons) need to know the legal actions per kind
//     without re-encoding the map in every consumer.
//   - The optimistic update path also needs the post-resolution value
//     so the client can paint the resolved row before the server SSE
//     `notification.resolved` event arrives.
//
// Drift contract:
//   - Adding a new decision kind → update both this map and the Go
//     allow-list in the SAME PR.
//   - Adding a new action to an existing kind → same.
//   - Server-only actions (e.g. `complete_all` on checklist) are kept
//     OUT of this map intentionally — there is no UI path that fires
//     them. The server writes the corresponding resolution directly
//     when its internal condition triggers.

import type { NotificationResolution } from "../types.js";

/**
 * `actionToResolution[kind][action]` → the resolution value the server
 * persists when this action is fired by a client. `undefined` when the
 * action isn't legal for the kind.
 */
export const actionToResolution: Readonly<
  Record<string, Readonly<Record<string, NotificationResolution>>>
> = {
  review_contradiction: {
    keep_a: "kept_a",
    keep_b: "kept_b",
    keep_both: "kept_both",
    dismiss: "dismissed",
  },
  review_topic_merge: {
    merge: "merged",
    keep_separate: "kept_separate",
    dismiss: "dismissed",
  },
  review_topic_restructure: {
    apply: "applied",
    keep: "kept",
    dismiss: "dismissed",
  },
  hub_invite: {
    accept: "accepted",
    decline: "declined",
  },
  hub_ownership_transfer: {
    accept: "accepted",
    decline: "declined",
  },
  // Plan 18 — checklist accepts only `dismiss` from clients. The
  // server-driven `complete_all` action that produces `applied_auto`
  // is deliberately omitted here: clients never have a UI path that
  // fires it, and surfacing it would invite drift between the SDK and
  // the server's per-kind allow-list.
  checklist: {
    dismiss: "dismissed",
  },
};

/**
 * Returns the resolution that would land if the caller fires `action`
 * on a notification of `kind`, or `undefined` when the action is not
 * legal for the kind. Useful for optimistic UI updates.
 */
export function resolveFromAction(
  kind: string,
  action: string,
): NotificationResolution | undefined {
  return actionToResolution[kind]?.[action];
}
