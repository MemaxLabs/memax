"use client";

import { useSyncExternalStore } from "react";
import {
  getStreamState,
  subscribeStreamState,
  type StreamState,
} from "@/lib/event-stream-state";

/**
 * Exposes the live event-stream connection state to React components.
 * The memax debugger uses this to show a persistent status header
 * (so empty events ≠ broken). Later phases use it for reconcile
 * triggers on reconnect and for per-hook conditional fallback when
 * the stream is degraded.
 *
 * The underlying store lives in `lib/event-stream-state.ts` so the
 * SSE subscription in `memax-event-bridge.tsx` can update it without
 * needing a React render.
 */
export function useStreamHealth(): StreamState {
  return useSyncExternalStore(
    subscribeStreamState,
    getStreamState,
    getStreamState,
  );
}
