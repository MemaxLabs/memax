/**
 * Batch toolbar active signal — connects section-scoped BatchToolbar
 * (inside SelectionProvider) to the layout-level GlobalBar (outside).
 *
 * Same bridge pattern as mutation-toast.ts: module-level callback ref.
 * BatchToolbar signals on mount/unmount. GlobalBar listens and hides.
 *
 * Uses a Set (not a counter) for idempotent add/remove — safe regardless
 * of React effect timing with multiple simultaneous SelectionProviders.
 */

type Listener = (active: boolean) => void;
const _activeIds = new Set<string>();
let _listener: Listener | null = null;
let _nextId = 0;

/** Called by GlobalBar on mount to listen for batch toolbar visibility. */
export function registerBatchActiveListener(fn: Listener) {
  _listener = fn;
  fn(_activeIds.size > 0); // sync immediately
}

/** Called by GlobalBar on unmount. */
export function unregisterBatchActiveListener() {
  _listener = null;
}

/** Called by BatchToolbar on mount. Returns an id for cleanup. */
export function signalBatchToolbarActive(): string {
  const id = `batch-${_nextId++}`;
  _activeIds.add(id);
  _listener?.(_activeIds.size > 0);
  return id;
}

/** Called by BatchToolbar on unmount with the id from active(). */
export function signalBatchToolbarInactive(id: string) {
  _activeIds.delete(id);
  _listener?.(_activeIds.size > 0);
}
