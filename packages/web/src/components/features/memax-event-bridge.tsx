"use client";

import { useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { getMemaxClient } from "@/lib/memax-client";
import { useMemaxDebugger } from "@/hooks/use-memax-debugger";
import {
  markClosed,
  markConnected,
  markConnecting,
  markError,
  markEvent,
  markReset,
} from "@/lib/event-stream-state";
import {
  applyEventRoute,
  routeLabel,
  type StreamEnvelope,
} from "@/lib/event-routing";
import { useStreamReconcile } from "@/hooks/use-stream-reconcile";
import { queryClient } from "@/lib/query-client";
import { trackEvent } from "@/lib/posthog";

const RECONNECT_DELAY_MS = 2000;

// Shared debugger-detail formatter for registry-routed events.
// Matches the legacy per-branch formatters so migrated events don't
// change dev-visible output: notification-shaped payloads (kind or
// change fields) use the inbox-style detail; everything else uses
// the entity_id/state style with a created_at time fallback. Takes
// the locale dict so payload-free fallbacks render translated.
function formatRouteDetail(
  payload: StreamEnvelope,
  strings: { stateUpdated: string; cacheInvalidated: string },
): string {
  if (payload.kind && payload.entity_id) {
    return `${payload.kind} · ${payload.entity_id.slice(0, 8)}`;
  }
  if (payload.change) {
    return `change=${payload.change}`;
  }
  if (payload.entity_id) {
    return `${payload.state || strings.stateUpdated} · ${payload.entity_id.slice(0, 8)}`;
  }
  if (payload.created_at) {
    return new Date(payload.created_at).toLocaleTimeString();
  }
  return strings.cacheInvalidated;
}

interface StreamError {
  code?: string;
  message?: string;
  status?: number;
}

// Memory cache-patching hydration lives in lib/memory-event-hydration.ts
// and is registered as the hub.memories.changed route's hydrate hook in
// lib/event-routing.ts. The bridge only plumbs SSE frames into the registry.
export function MemaxEventBridge() {
  const { user, activeHubId } = useAuth();
  const { recordEvent } = useMemaxDebugger();
  const { t } = useLocale();
  const activeHubIdRef = useRef(activeHubId);
  const recordEventRef = useRef(recordEvent);
  // Ref so the SSE callback closures always read the latest
  // translations without needing to be part of the effect deps.
  const tRef = useRef(t);

  // Stream-health safety net — broad invalidation on reconnect
  // transition + visibility resume, with shared cooldown and a
  // narrow whitelist. See use-stream-reconcile.ts for rationale.
  useStreamReconcile();

  useEffect(() => {
    activeHubIdRef.current = activeHubId;
  }, [activeHubId]);

  useEffect(() => {
    recordEventRef.current = recordEvent;
  }, [recordEvent]);

  useEffect(() => {
    tRef.current = t;
  }, [t]);

  useEffect(() => {
    if (!user?.id) return;

    let cancelled = false;
    let controller: AbortController | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    // Tag each connect() call with what triggered it so the PostHog
    // telemetry distinguishes the expected "one mount, one connect"
    // steady state from a bug-shaped "many reconnects per session"
    // pattern. Reset counter lives inside the effect closure, so
    // logout → new effect starts a fresh count.
    let sessionConnectCount = 0;
    let nextConnectReason:
      | "initial_mount"
      | "reconnect_after_close"
      | "reconnect_after_error" = "initial_mount";

    const connect = () => {
      if (cancelled) return;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }

      // Abort any previous subscription before replacing the controller.
      // The SDK's events.subscribe returns its AbortController SYNCHRONOUSLY,
      // but the underlying fetch runs inside an async IIFE that first
      // awaits getAuth(). If connect() is called twice before the old
      // sub's fetch actually started, overwriting `controller` orphans
      // the old sub — its async IIFE continues, opens its own fetch with
      // its own internal signal, and stays alive indefinitely because
      // our cleanup only aborts the latest reference. The symptom is
      // N-way simultaneous SSE streams in the Network tab with counter
      // resets every time a new one opens. Aborting here is idempotent
      // when controller is null or already-aborted.
      controller?.abort();

      // Mark the initial attempt so the debugger's stream-status
      // header reflects "connecting…" before the first ready lands.
      markConnecting();

      sessionConnectCount += 1;
      trackEvent("sse_connect", {
        reason: nextConnectReason,
        session_connect_count: sessionConnectCount,
      });

      controller = getMemaxClient().events.subscribe({
        onEvent: (event, data) => {
          if (cancelled) return;

          // Every SSE frame — including ping — advances the
          // "last event" timer so the debugger's liveness indicator
          // keeps ticking between real state-change events.
          markEvent();

          if (event === "ping") return;

          if (event === "error") {
            const payload = (data ?? {}) as StreamError;
            const message =
              payload.message ?? tRef.current.dev.eventStreamUnknownError;
            markError(message);
            recordEventRef.current({
              channel: "system",
              tone: "error",
              title: tRef.current.dev.eventStreamDisconnected,
              detail: message,
              context: activeHubIdRef.current
                ? `hub:${activeHubIdRef.current}`
                : undefined,
            });
            // The SDK's fetch-based stream transport does NOT call
            // onClose for initial-connect failures (network error,
            // non-OK response, empty body). It emits "error" and
            // returns early. Without self-scheduling a reconnect
            // here, the stream stays dead and the debugger header
            // reports "reconnecting…" indefinitely. Idempotent —
            // connect() clears any existing timer before running,
            // so a later onClose-scheduled reconnect won't double up.
            if (!reconnectTimer) {
              nextConnectReason = "reconnect_after_error";
              reconnectTimer = setTimeout(connect, RECONNECT_DELAY_MS);
            }
            return;
          }

          if (event === "ready") {
            const payload = data as { hub_ids?: string[]; user_id?: string };
            const hubCount = payload.hub_ids?.length ?? 0;
            markConnected(hubCount);
            recordEventRef.current({
              channel: "system",
              tone: "success",
              title: tRef.current.dev.eventStreamReady,
              detail:
                hubCount > 0
                  ? tRef.current.dev.eventStreamWatchingHubs.replace(
                      "{count}",
                      String(hubCount),
                    )
                  : tRef.current.dev.eventStreamWatchingLive,
              context: payload.user_id ? `user:${payload.user_id}` : undefined,
            });
            return;
          }

          // All known event types route through the declarative
          // registry (hub.memories.changed, hub.topics/dreams/members,
          // notification.*, agent.changed, key.changed). Unknown
          // events are a no-op — the registry returns false and the
          // bridge silently ignores, preserving forward compat with
          // any future event type that lands server-side before its
          // client route is registered. Debugger formatting mirrors
          // the legacy per-branch formatter via formatRouteDetail.
          const rpayload = (data ?? {}) as StreamEnvelope;
          const routeApplied = applyEventRoute(event, rpayload, queryClient);
          if (routeApplied) {
            recordEventRef.current({
              channel: "system",
              tone: "neutral",
              title: routeLabel(event) ?? event,
              detail: formatRouteDetail(rpayload, {
                stateUpdated: tRef.current.dev.eventStreamStateUpdated,
                cacheInvalidated: tRef.current.dev.eventStreamCacheInvalidated,
              }),
              context: rpayload.hub_id
                ? `hub:${rpayload.hub_id}`
                : rpayload.recipient_user_id
                  ? `user:${rpayload.recipient_user_id.slice(0, 8)}`
                  : activeHubIdRef.current
                    ? `hub:${activeHubIdRef.current}`
                    : undefined,
            });
          }
        },
        onClose: () => {
          if (cancelled) return;
          markClosed();
          // Symmetric guard with the error handler above: if a reconnect
          // is already scheduled (e.g. error already fired self-schedule),
          // don't overwrite it — the first-scheduled timer should fire
          // first and connect() clears any existing timer at entry, so
          // double-arming would only delay reconnection.
          if (!reconnectTimer) {
            nextConnectReason = "reconnect_after_close";
            reconnectTimer = setTimeout(connect, RECONNECT_DELAY_MS);
          }
        },
      });
    };

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
      }
      controller?.abort();
      // Reset stream state on bridge teardown so the debugger header
      // and the synthetic "engaged" event don't inherit stale
      // connected/hub-count data on the next mount (e.g. after
      // logout → login, or auth-token refresh cycling the effect).
      markReset();
    };
  }, [user?.id]);

  return null;
}
