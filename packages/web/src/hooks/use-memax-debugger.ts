"use client";

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useDevMode } from "@/hooks/use-dev-mode";
import {
  clearMemaxDebugEvents,
  getMemaxDebugEvents,
  pushMemaxDebugEvent,
  subscribeMemaxDebugEvents,
  type MemaxDebugEvent,
} from "@/lib/memax-debugger";
import { getStreamState } from "@/lib/event-stream-state";
import { useLocale } from "@/i18n";

export type { MemaxDebugEvent } from "@/lib/memax-debugger";

// Module-level latch: tracks whether we've already pushed a synthetic
// "Debugger engaged" event for the current enabled-transition. Reset
// when enabled flips back to false so toggling off → on pushes a
// fresh event on re-engage. Module-scoped (not per-hook-instance)
// because multiple consumers mount useMemaxDebugger simultaneously;
// without the latch every consumer would push one redundant event
// per transition.
let engagedLatch = false;

export function useMemaxDebugger() {
  const { isDevUser, flags } = useDevMode();
  const { t } = useLocale();
  const events = useSyncExternalStore(
    subscribeMemaxDebugEvents,
    getMemaxDebugEvents,
    getMemaxDebugEvents,
  );

  const enabled = isDevUser && flags.debuggerEnabled;

  const recordEvent = useCallback(
    (event: Omit<MemaxDebugEvent, "id" | "at">) => {
      if (enabled === false) return;
      pushMemaxDebugEvent(event);
    },
    [enabled],
  );

  // Synthetic "Debugger engaged" event. The SSE `ready` event fires
  // once at app mount; if the debugger was disabled then, that event
  // was gated out of the event bus and never rendered. Between real
  // state-change events the bus is quiet, so the panel looks empty
  // even though the stream is fine. Pushing a one-time engaged event
  // with the current stream state gives immediate feedback on
  // toggle-on and makes the panel's liveness obvious.
  useEffect(() => {
    if (enabled === false) {
      engagedLatch = false;
      return;
    }
    if (engagedLatch) return;
    engagedLatch = true;
    const s = getStreamState();
    const stateLabel = (() => {
      switch (s.connection) {
        case "idle":
          return t.dev.streamStateIdle;
        case "connecting":
          return t.dev.streamStateConnecting;
        case "connected":
          return t.dev.streamStateConnected;
        case "reconnecting":
          return t.dev.streamStateReconnecting;
        case "disconnected":
          return t.dev.streamStateDisconnected;
      }
    })();
    pushMemaxDebugEvent({
      channel: "system",
      tone: s.connection === "connected" ? "success" : "neutral",
      title: t.dev.debuggerEngaged,
      detail:
        s.connection === "connected" && s.watchingHubCount > 0
          ? t.dev.streamConnectedWithHubs.replace(
              "{count}",
              String(s.watchingHubCount),
            )
          : t.dev.streamIsState.replace("{state}", stateLabel),
    });
  }, [enabled, t.dev]);

  const clear = useCallback(() => {
    clearMemaxDebugEvents();
  }, []);

  return {
    isDevUser,
    enabled,
    events,
    recordEvent,
    clear,
  };
}
