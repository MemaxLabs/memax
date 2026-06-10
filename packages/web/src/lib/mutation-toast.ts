/**
 * Mutation toast bridge — connects TanStack Query's MutationCache
 * (outside React) to the bar notification system (inside React).
 *
 * Pattern: module-level callback ref.
 * - MutationCache handlers call `fireMutationToast()`
 * - BarProvider registers/unregisters the handler on mount/unmount
 *
 * This is the same pattern React Query's own `notifyManager` uses.
 */

import type { BarNotification } from "@/lib/bar-types";

type ToastHandler = (notification: BarNotification) => void;
let _handler: ToastHandler | null = null;

/** Called by BarProvider on mount to wire the bridge. */
export function registerMutationToastHandler(fn: ToastHandler) {
  _handler = fn;
}

/** Called by BarProvider on unmount. */
export function unregisterMutationToastHandler() {
  _handler = null;
}

/** Called by MutationCache handlers to show a toast. */
export function fireMutationToast(notification: BarNotification) {
  _handler?.(notification);
}
