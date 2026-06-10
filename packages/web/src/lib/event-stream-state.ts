"use client";

/**
 * Event-stream connection state — observable by any consumer via
 * useSyncExternalStore. Primary consumer today is the memax debugger
 * (so the user sees stream liveness even between SSE events, instead
 * of interpreting an empty event list as a broken stream). Future
 * consumers:
 *  - reconcile safety net (PR B) — fire one-shot broad invalidation
 *    on disconnected → connected transitions
 *  - per-hook conditional fallback — when stream is disconnected,
 *    settings-page hooks fall back to focus/mount refetch semantics
 *
 * Kept outside React so the SSE subscription in memax-event-bridge
 * can mark state from inside the EventSource callbacks without
 * needing a render. The React surface is a thin hook wrapper around
 * useSyncExternalStore.
 */

export type StreamConnectionState =
  | "idle" // never connected in this session
  | "connecting" // first connect in flight
  | "connected" // ready received; stream is live
  | "reconnecting" // transient error; retrying (bridge re-arms after RECONNECT_DELAY_MS)
  | "disconnected"; // not currently trying — reserved for explicit stop

export interface StreamState {
  connection: StreamConnectionState;
  /** Monotonic ms timestamp of the last `ready` event. Null until first connect. */
  lastReadyAt: number | null;
  /** Monotonic ms timestamp of the last message of any kind (including ping). */
  lastEventAt: number | null;
  /** Monotonic ms timestamp of the last error message. */
  lastErrorAt: number | null;
  /** Human-readable last error message, for the debugger status header. */
  lastErrorMessage: string | null;
  /** Number of hubs the stream is currently watching (from the ready envelope). */
  watchingHubCount: number;
  /**
   * Count of `connect()` invocations since the bridge effect mounted.
   * Healthy steady-state is 1 (mount → subscribe → stay). A number
   * that grows during normal use indicates server-side or network
   * hang-up + bridge-scheduled reconnects, or a latent bug causing
   * the effect to re-run. Exposed in the debugger UI as a diagnostic
   * signal parallel to the PostHog `sse_connect` telemetry — lets
   * a dev spot the problem without opening the PostHog dashboard.
   */
  connectCount: number;
}

const INITIAL: StreamState = {
  connection: "idle",
  lastReadyAt: null,
  lastEventAt: null,
  lastErrorAt: null,
  lastErrorMessage: null,
  watchingHubCount: 0,
  connectCount: 0,
};

let state: StreamState = INITIAL;
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((listener) => listener());
}

function setState(patch: Partial<StreamState>) {
  state = { ...state, ...patch };
  emit();
}

// Marker functions — called from memax-event-bridge.tsx at the right
// points in the SSE subscribe callbacks. Keep them single-purpose so
// the bridge reads cleanly.

export function markConnecting() {
  // Increment connect count on every connect() invocation. Called
  // before the subscribe, so a rising count during normal use flags
  // either server/network hang-ups or a latent re-mount bug —
  // regardless of whether the connection eventually reaches
  // "connected". Reset on markReset (logout / full teardown).
  const nextCount = state.connectCount + 1;
  // Only flip to "connecting" from idle / disconnected / reconnecting.
  // Stays "connected" if a resubscribe races before the new ready
  // lands, to avoid a flicker back to connecting.
  if (state.connection === "connected") {
    setState({ connectCount: nextCount });
    return;
  }
  setState({ connection: "connecting", connectCount: nextCount });
}

export function markConnected(hubCount: number) {
  const now = Date.now();
  setState({
    connection: "connected",
    lastReadyAt: now,
    lastEventAt: now,
    watchingHubCount: hubCount,
    // Clear the last-error on successful reconnect so the debugger
    // header doesn't misleadingly show a stale error message.
    lastErrorAt: null,
    lastErrorMessage: null,
  });
}

export function markEvent() {
  // Called for every SSE message including ping, so the debugger's
  // "last event Xs ago" timer ticks continuously while the stream is
  // healthy. No connection-state change.
  setState({ lastEventAt: Date.now() });
}

export function markError(message: string) {
  setState({
    connection: "reconnecting",
    lastErrorAt: Date.now(),
    lastErrorMessage: message,
  });
}

export function markClosed() {
  // After onClose, the bridge schedules a reconnect. Stay in
  // "reconnecting" rather than "disconnected" so the debugger shows
  // the correct intermediate state — the stream is coming back.
  if (state.connection === "connected") {
    setState({ connection: "reconnecting" });
  }
}

export function markReset() {
  // Teardown path. Called from the bridge cleanup when the effect
  // unmounts (user logout, auth change, full app unmount). Resets
  // every field so the next mount starts from a clean `idle` rather
  // than inheriting the previous session's `connected` state.
  // Without this, useMemaxDebugger's synthetic engage effect can
  // snapshot stale `connected` + hub count from a logged-out session.
  state = INITIAL;
  emit();
}

// Read side — used by useSyncExternalStore in use-stream-health.ts.

export function getStreamState(): StreamState {
  return state;
}

export function subscribeStreamState(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Test-only: reset module-level state between test cases. */
export function __resetStreamStateForTests() {
  state = INITIAL;
  emit();
}
