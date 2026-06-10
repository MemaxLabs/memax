"use client";

import { useEffect, useMemo, useState } from "react";
import { Surface } from "@memaxlabs/ui";
import { ChevronDown, ChevronUp, Trash2, Radar } from "lucide-react";
import { useLocale } from "@/i18n";
import { useMemaxDebugger } from "@/hooks/use-memax-debugger";
import { useStreamHealth } from "@/hooks/use-stream-health";
import type { MemaxDebugChannel, MemaxDebugTone } from "@/lib/memax-debugger";
import type { StreamConnectionState } from "@/lib/event-stream-state";

const COLLAPSED_KEY = "memax-debugger-collapsed";

const CHANNEL_STYLES: Record<MemaxDebugChannel, { dot: string; bg: string }> = {
  recall: {
    dot: "var(--signature)",
    bg: "color-mix(in oklch, var(--signature) 10%, transparent)",
  },
  ai: {
    dot: "oklch(0.68 0.14 250)",
    bg: "oklch(0.68 0.14 250 / 0.10)",
  },
  remember: {
    dot: "oklch(0.68 0.14 155)",
    bg: "oklch(0.68 0.14 155 / 0.10)",
  },
  system: {
    dot: "oklch(from var(--foreground) l c h / 0.55)",
    bg: "oklch(from var(--foreground) l c h / 0.06)",
  },
};

const TONE_STYLES: Record<MemaxDebugTone, string> = {
  active: "var(--signature)",
  success: "oklch(0.68 0.14 155)",
  neutral: "var(--fg-3)",
  error: "var(--destructive)",
};

function formatAgo(at: number) {
  const diff = Math.max(0, Date.now() - at);
  if (diff < 1000) return "now";
  if (diff < 60_000) return Math.round(diff / 1000) + "s";
  if (diff < 3_600_000) return Math.round(diff / 60_000) + "m";
  return Math.round(diff / 3_600_000) + "h";
}

// Maps the stream connection state to the debugger's color palette.
// Connection state is the truth the debugger surfaces to the user:
// the event list is derived activity, but the header tells them
// whether the stream is live at all. Designed so a dev user who
// opens the panel and sees "Connected · 2 hubs · 3s ago" can
// correctly conclude "the pipe is healthy, nothing has happened
// yet" — an empty event list is no longer ambiguous. Labels come
// from t.dev.streamState* to honor the web i18n rule.
const STREAM_STATE_STYLES: Record<
  StreamConnectionState,
  { dot: string; tone: "active" | "neutral" | "error" }
> = {
  idle: {
    dot: "oklch(from var(--foreground) l c h / 0.35)",
    tone: "neutral",
  },
  connecting: {
    dot: "oklch(0.72 0.14 85)",
    tone: "neutral",
  },
  connected: {
    dot: "oklch(0.68 0.14 155)",
    tone: "active",
  },
  reconnecting: {
    dot: "oklch(0.72 0.14 60)",
    tone: "neutral",
  },
  disconnected: {
    dot: "var(--destructive)",
    tone: "error",
  },
};

export function MemaxDebugger() {
  const { t } = useLocale();
  const { enabled, events, clear } = useMemaxDebugger();
  const streamHealth = useStreamHealth();
  const [collapsed, setCollapsed] = useState(false);
  // 1-second ticker purely for rendering the "last event Xs ago"
  // label. The stream-state store updates `lastEventAt` on every SSE
  // frame (including the server's ~30s ping), so without this
  // ticker the relative timestamp in the header would jump in 30s
  // increments. 1s cadence matches how event timestamps are
  // displayed below.
  const [, setNow] = useState(() => Date.now());

  useEffect(() => {
    const raw = localStorage.getItem(COLLAPSED_KEY);
    setCollapsed(raw === "true");
  }, []);

  useEffect(() => {
    localStorage.setItem(COLLAPSED_KEY, String(collapsed));
  }, [collapsed]);

  useEffect(() => {
    if (enabled === false || collapsed) return;
    const handle = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(handle);
  }, [enabled, collapsed]);

  const counts = useMemo(() => {
    return events.reduce(
      (acc, event) => {
        acc[event.channel] += 1;
        return acc;
      },
      { recall: 0, ai: 0, remember: 0, system: 0 },
    );
  }, [events]);

  const streamStyles = STREAM_STATE_STYLES[streamHealth.connection];
  const streamStateLabel = (() => {
    switch (streamHealth.connection) {
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
  const streamHubLabel =
    streamHealth.watchingHubCount === 1
      ? t.dev.streamHubsSingular
      : t.dev.streamHubsPlural.replace(
          "{count}",
          String(streamHealth.watchingHubCount),
        );

  if (enabled === false) return null;

  if (collapsed) {
    return (
      <button
        onClick={() => setCollapsed(false)}
        className="fixed right-4 bottom-4 z-[58] flex items-center gap-2 rounded-full px-3 py-2 text-[13px] text-fg-2 cursor-pointer"
        style={{
          background: "color-mix(in oklch, var(--card) 92%, var(--signature))",
          border: "1px solid var(--bar-border)",
          boxShadow: "var(--bar-shadow)",
        }}
      >
        <Radar className="h-3.5 w-3.5" style={{ color: "var(--signature)" }} />
        {t.dev.debuggerTitle}
        <ChevronUp className="h-3.5 w-3.5 text-fg-3" />
      </button>
    );
  }

  return (
    <div className="fixed right-4 bottom-4 z-[58] w-[calc(100vw-32px)] max-w-[380px]">
      <Surface variant="default" rounded="2xl" className="overflow-hidden">
        <div className="px-4 py-3 border-b border-border/50">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Radar
                  className="h-3.5 w-3.5"
                  style={{ color: "var(--signature)" }}
                />
                <p className="text-[14px] font-medium text-foreground">
                  {t.dev.debuggerTitle}
                </p>
                <span
                  className="rounded-full px-2 py-0.5 text-[10px] font-medium"
                  style={{
                    color: "var(--signature)",
                    background:
                      "color-mix(in oklch, var(--signature) 10%, transparent)",
                  }}
                >
                  {t.dev.debuggerLive}
                </span>
              </div>
              <p className="mt-1 text-[12px] text-fg-3">
                {t.dev.debuggerDescription}
              </p>
            </div>
            <button
              onClick={() => setCollapsed(true)}
              className="rounded-lg p-1.5 text-fg-3 hover:bg-surface-2 hover:text-fg-2 transition-colors cursor-pointer"
              aria-label={t.dev.debuggerCollapse}
            >
              <ChevronDown className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Persistent stream-status header. Shows the live connection
            state so an empty event list is no longer ambiguous — the
            user can tell whether the stream is healthy even between
            real events. Updates via useStreamHealth() (SSE state
            marker fns in memax-event-bridge.tsx) plus a 1s ticker
            for the "last event Xs ago" relative timestamp. */}
        <div className="px-4 py-2.5 border-b border-border/50">
          <div className="flex items-center gap-2 text-[12px]">
            <span
              className="h-1.5 w-1.5 rounded-full shrink-0"
              style={{ background: streamStyles.dot }}
              aria-hidden
            />
            <span
              className="font-medium"
              style={{ color: TONE_STYLES[streamStyles.tone] }}
            >
              {streamStateLabel}
            </span>
            {streamHealth.connection === "connected" &&
              streamHealth.watchingHubCount > 0 && (
                <span className="text-fg-3">· {streamHubLabel}</span>
              )}
            {streamHealth.connection === "connected" &&
              streamHealth.lastReadyAt !== null && (
                <span
                  className="text-fg-4 ml-auto tabular-nums"
                  title={t.dev.streamConnectedSinceTooltip}
                >
                  {`${t.dev.streamConnectedSinceLabel} ${formatAgo(streamHealth.lastReadyAt)}`}
                </span>
              )}
            {streamHealth.lastEventAt !== null && (
              <span
                className={`text-fg-4 tabular-nums ${
                  streamHealth.connection === "connected" &&
                  streamHealth.lastReadyAt !== null
                    ? ""
                    : "ml-auto"
                }`}
                title={t.dev.streamLastEventTooltip}
              >
                {`${t.dev.streamLastEventLabel} ${formatAgo(streamHealth.lastEventAt)}`}
              </span>
            )}
            {streamHealth.connectCount > 1 && (
              // When the count is >1, something is re-running connect()
              // — either transient server/network hang-ups + scheduled
              // reconnects, or a latent re-mount bug. Tinted red to
              // flag it visually. Healthy steady-state is count=1.
              <span
                className="tabular-nums font-semibold"
                style={{ color: "var(--destructive)" }}
                title={t.dev.streamConnectCountTooltip}
              >
                {`×${streamHealth.connectCount}`}
              </span>
            )}
          </div>
          {streamHealth.connection !== "connected" &&
            streamHealth.lastErrorMessage && (
              <p
                className="mt-1 text-[11px] text-fg-4 break-words"
                title={streamHealth.lastErrorMessage}
              >
                {streamHealth.lastErrorMessage}
              </p>
            )}
        </div>

        <div className="px-4 py-3 border-b border-border/50">
          <div className="flex flex-wrap gap-2">
            {[
              [t.dev.debuggerRecall, counts.recall, "recall"],
              [t.dev.debuggerAI, counts.ai, "ai"],
              [t.dev.debuggerRemember, counts.remember, "remember"],
              [t.dev.debuggerSystem, counts.system, "system"],
            ].map(([label, count, channel]) => {
              const styles = CHANNEL_STYLES[channel as MemaxDebugChannel];
              return (
                <div
                  key={String(channel)}
                  className="flex items-center gap-2 rounded-full px-2.5 py-1 text-[11px] text-fg-2"
                  style={{ background: styles.bg }}
                >
                  <span
                    className="h-1.5 w-1.5 rounded-full"
                    style={{ background: styles.dot }}
                  />
                  <span>{label}</span>
                  <span className="tabular-nums text-fg-3">{count}</span>
                </div>
              );
            })}
          </div>
        </div>

        <div className="max-h-[360px] overflow-y-auto px-3 py-3 space-y-2">
          {events.length === 0 ? (
            <div
              className="rounded-surface px-3.5 py-4 text-center"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
              }}
            >
              <p className="text-[13px] font-medium text-fg-2">
                {t.dev.debuggerEmptyTitle}
              </p>
              <p className="mt-1 text-[12px] text-fg-3">
                {t.dev.debuggerEmptyDescription}
              </p>
            </div>
          ) : (
            events.map((event) => {
              const channelStyles = CHANNEL_STYLES[event.channel];
              return (
                <div
                  key={event.id}
                  className="rounded-surface px-3.5 py-3"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.03)",
                    border: "1px solid var(--border)",
                  }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span
                          className="h-1.5 w-1.5 rounded-full"
                          style={{ background: channelStyles.dot }}
                        />
                        <span className="text-[11px] uppercase tracking-[0.14em] text-fg-4">
                          {event.channel}
                        </span>
                      </div>
                      <p
                        className="mt-1 text-[13px] font-medium"
                        style={{ color: TONE_STYLES[event.tone] }}
                      >
                        {event.title}
                      </p>
                      {event.detail && (
                        <p className="mt-1 text-[12px] text-fg-2 break-words">
                          {event.detail}
                        </p>
                      )}
                      {event.context && (
                        <p className="mt-1 text-[11px] text-fg-4 break-words">
                          {event.context}
                        </p>
                      )}
                    </div>
                    <span className="shrink-0 text-[11px] text-fg-4 tabular-nums">
                      {formatAgo(event.at)}
                    </span>
                  </div>
                </div>
              );
            })
          )}
        </div>

        <div className="px-4 py-3 border-t border-border/50 flex justify-end">
          <button
            onClick={clear}
            className="inline-flex items-center gap-2 rounded-lg px-3 py-1.5 text-[12px] text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t.dev.debuggerClear}
          </button>
        </div>
      </Surface>
    </div>
  );
}
