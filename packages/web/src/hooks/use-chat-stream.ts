"use client";

/**
 * useChatStream — connects to the chat SSE replay+tail stream for
 * one in-flight (or recently-finalized) assistant message and
 * aggregates the events into reactive React state. Plan 24
 * §"Send-message + resume" pins the contract:
 *
 *   - chat message stream endpoint exposed by memax-sdk
 *   - server keeps a 24h replay buffer in chat_message_events
 *   - clients send Last-Event-ID to resume past their last seq
 *   - terminal events: message.completed | .partial_failed | .failed | .canceled
 *
 * The hook owns these pieces of state per stream:
 *   - `text`: accumulated assistant content (from model.delta deltas
 *     OR from a terminal frame that arrived via replay).
 *   - `toolCalls`: ordered list of tool invocations seen so far.
 *   - `approvals`: pending approval requests addressed to this msg.
 *   - `terminal`: the terminal status once it lands (or undefined).
 *   - `error`: stream-level error info (network / replay-expired /
 *     auth) — distinct from the chat message's own `error` payload
 *     which arrives inside a terminal frame.
 *
 * Reconnect strategy: on a non-aborted stream close (network blip,
 * server restart) the hook reopens with the last seq it consumed
 * as Last-Event-ID. Server-side replay covers the gap. We give up
 * after a small bounded retry count to avoid a hot loop on a
 * persistently-broken endpoint; the user can refresh the page (or
 * navigate away and back) to recover.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import type { ChatMessageStatus, ChatStreamEventName } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

// Reconnect tuning. The chat profile caps a single turn at 120s of
// agent work; we set the retry budget high enough that a transient
// 30s blip mid-turn recovers, but low enough that a permanently-
// broken endpoint stops fast. Each retry waits an additional
// retryBaseDelayMs to avoid thundering-herd reconnect after a
// regional outage clears.
const maxStreamReconnects = 3;
const retryBaseDelayMs = 800;

export type ChatToolCallRecord = {
  id?: string;
  name: string;
  args?: unknown;
  result?: unknown;
  is_error?: boolean;
};

export type ChatApprovalRecord = {
  approval_id: string;
  tool_name: string;
  args?: unknown;
  expires_at?: string;
};

/**
 * One slice of the assistant's response in chronological order.
 * Industry pattern (Claude.ai / Cursor / v0): tool calls render
 * inline at the position they happen — text → tool step →
 * more text → another tool step → final text. The aggregate
 * `text` field is preserved alongside (callers that only need
 * the final answer can ignore segments), but segment ordering
 * is what drives the in-flight render.
 */
export type ChatStreamSegment =
  | { kind: "text"; text: string }
  | { kind: "tool"; toolIdx: number }
  /**
   * A readable reasoning block (wire: model.thinking). Coalesced
   * partials stream in first ({text: full-so-far, partial: true} —
   * REPLACE the trailing thinking segment, never append); the
   * completed block carries duration_ms and finalizes the segment.
   */
  | { kind: "thinking"; text: string; durationMs?: number };

export interface ChatStreamState {
  /** Aggregated assistant text. Empty while the model is still tooling. */
  text: string;
  toolCalls: ChatToolCallRecord[];
  /**
   * Ordered text + tool segments capturing the chronology of
   * this turn. Each text-arrival appends to a trailing text
   * segment; each tool.call event closes the current text
   * segment and pushes a tool reference. tool.result frames
   * patch toolCalls[toolIdx] in place — segments don't change.
   */
  segments: ChatStreamSegment[];
  approvals: ChatApprovalRecord[];
  /** Terminal status; undefined while the turn is in flight. */
  terminal?: ChatMessageStatus;
  /** Server-emitted error blob attached to a terminal frame. */
  errorPayload?: { code?: string; message?: string };
  /**
   * Stream-level error (transport / replay window). Distinct from
   * `errorPayload` which is the message's own terminal-frame error.
   */
  streamError?: { code: string; message: string };
  /** Last seq consumed; used by the resume path. */
  lastEventId: number;
  /** True while the SSE connection is open. */
  connected: boolean;
}

interface UseChatStreamArgs {
  sessionId: string | undefined;
  messageId: string | undefined;
  /**
   * When true, the hook DOES NOT open the stream. Useful when the
   * caller already knows the row is terminal and is rendering
   * persisted state from useChatMessage instead.
   */
  paused?: boolean;
  /**
   * Optional cap on reconnect attempts. Defaults to
   * maxStreamReconnects. Tests pass 0 to disable retry.
   */
  maxRetries?: number;
}

const initialState: ChatStreamState = {
  text: "",
  toolCalls: [],
  segments: [],
  approvals: [],
  lastEventId: 0,
  connected: false,
};

const terminalEventNames: ReadonlySet<ChatStreamEventName> =
  new Set<ChatStreamEventName>([
    "message.completed",
    "message.partial_failed",
    "message.failed",
    "message.canceled",
  ]);

function asPayload(data: unknown): Record<string, unknown> {
  return data && typeof data === "object"
    ? (data as Record<string, unknown>)
    : {};
}

function payloadSeq(payload: Record<string, unknown>): number | undefined {
  return typeof payload.seq === "number" ? payload.seq : undefined;
}

function firstString(
  payload: Record<string, unknown>,
  keys: string[],
): string | undefined {
  for (const key of keys) {
    const value = payload[key];
    if (typeof value === "string") return value;
  }
  return undefined;
}

function firstValue(payload: Record<string, unknown>, keys: string[]): unknown {
  for (const key of keys) {
    if (Object.hasOwn(payload, key)) return payload[key];
  }
  return undefined;
}

function eventToTerminal(name: string): ChatMessageStatus | undefined {
  switch (name) {
    case "message.completed":
      return "completed";
    case "message.partial_failed":
      return "partial_failed";
    case "message.failed":
      return "failed";
    case "message.canceled":
      return "canceled";
    default:
      return undefined;
  }
}

export function useChatStream({
  sessionId,
  messageId,
  paused,
  maxRetries = maxStreamReconnects,
}: UseChatStreamArgs): ChatStreamState {
  const [state, setState] = useState<ChatStreamState>(initialState);
  // The last seq we successfully consumed — kept in a ref so the
  // reconnect effect can read it without re-running on every state
  // update. State + ref dual-track is a common pattern when the
  // value is also rendered.
  const lastSeqRef = useRef<number>(0);
  const retryCountRef = useRef<number>(0);
  const controllerRef = useRef<AbortController | null>(null);

  const reset = useCallback(() => {
    lastSeqRef.current = 0;
    retryCountRef.current = 0;
    setState(initialState);
  }, []);

  // Reset state when the target message changes — different
  // assistant turn, different stream, different aggregator window.
  useEffect(() => {
    reset();
  }, [sessionId, messageId, reset]);

  useEffect(() => {
    if (!sessionId || !messageId || paused) {
      return undefined;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const open = () => {
      if (cancelled) return;
      const controller = getMemaxClient().chats.streamMessage(
        sessionId,
        messageId,
        {
          lastEventId: lastSeqRef.current || undefined,
          onEvent: (event, data) => {
            // Note: the SDK emits raw `event` strings. We don't
            // strictly type them at the callback boundary because
            // forward-compatibility means the server may add new
            // events (e.g. context.compacted) that older clients
            // safely ignore.
            if (event === "error") {
              const err = asPayload(data);
              setState((s) => ({
                ...s,
                connected: false,
                streamError: {
                  code:
                    typeof err.code === "string" ? err.code : "stream_error",
                  message:
                    typeof err.message === "string"
                      ? err.message
                      : "Chat stream failed",
                },
              }));
              return;
            }

            // The SDK parses SSE data frames but doesn't currently
            // expose the separate SSE `id:` line, so the chat stream
            // embeds the per-message seq in each payload.
            const payload = asPayload(data);
            const seq = payloadSeq(payload);
            if (seq && seq > lastSeqRef.current) {
              lastSeqRef.current = seq;
            }

            const terminal = eventToTerminal(event);
            if (terminal) {
              const errPayload = (() => {
                const e = payload.error;
                if (!e || typeof e !== "object") return undefined;
                const o = e as Record<string, unknown>;
                return {
                  code: typeof o.code === "string" ? o.code : undefined,
                  message:
                    typeof o.message === "string" ? o.message : undefined,
                };
              })();
              const finalText = firstString(payload, ["content", "text"]);
              setState((s) => {
                // If the server sent a final text and it differs
                // from what we accumulated, the segments need to
                // reflect the authoritative final string (replay
                // path: a client that joined late gets the
                // terminal frame with the full text but none of
                // the per-delta events).
                //
                // **Prefix invariant** — when earlier text
                // segments rendered prose before the trailing
                // segment, `finalText` is the WHOLE answer. We
                // must:
                //   1. Compute `prefix` = sum of every text
                //      segment EXCEPT the trailing one (if the
                //      trailing segment is text). If the last
                //      segment is a tool, ALL existing text
                //      segments are prefix.
                //   2. If finalText.startsWith(prefix), only the
                //      suffix needs to land on the trailing
                //      segment (or be appended as a fresh trailing
                //      text segment when the last segment is a
                //      tool).
                //   3. If finalText does NOT extend prefix (model
                //      rewrote earlier prose), collapse to one
                //      authoritative text segment — losing tool
                //      interleave is preferable to duplicate text.
                let nextSegments = s.segments;
                if (finalText !== undefined && finalText !== s.text) {
                  const lastIdx = nextSegments.length - 1;
                  const last = nextSegments[lastIdx];
                  const lastIsText = Boolean(last && last.kind === "text");
                  // Prefix excludes the trailing text segment when
                  // present (its content WILL be replaced); when
                  // the trailing segment is a tool, every text
                  // segment counts toward the prefix.
                  const prefixSegments = lastIsText
                    ? nextSegments.slice(0, lastIdx)
                    : nextSegments;
                  const earlierText = prefixSegments
                    .filter((seg) => seg.kind === "text")
                    .map((seg) => (seg.kind === "text" ? seg.text : ""))
                    .join("");
                  if (earlierText.length === 0) {
                    // No prior prose — finalText replaces or
                    // extends the trailing segment directly.
                    if (lastIsText) {
                      nextSegments = [
                        ...nextSegments.slice(0, lastIdx),
                        { kind: "text", text: finalText },
                      ];
                    } else if (finalText.length > 0) {
                      nextSegments = [
                        ...nextSegments,
                        { kind: "text", text: finalText },
                      ];
                    }
                  } else if (finalText.startsWith(earlierText)) {
                    const trailingFinal = finalText.slice(earlierText.length);
                    if (lastIsText) {
                      nextSegments = [
                        ...nextSegments.slice(0, lastIdx),
                        { kind: "text", text: trailingFinal },
                      ];
                    } else if (trailingFinal.length > 0) {
                      nextSegments = [
                        ...nextSegments,
                        { kind: "text", text: trailingFinal },
                      ];
                    }
                  } else {
                    // Model rewrote earlier content — collapse to
                    // one authoritative text segment. Tool steps
                    // are dropped from the timeline because we
                    // can't reconstruct the right interleave.
                    nextSegments =
                      finalText.length > 0
                        ? [{ kind: "text", text: finalText }]
                        : [];
                  }
                }
                return {
                  ...s,
                  connected: false,
                  terminal,
                  errorPayload: errPayload,
                  text: finalText ?? s.text,
                  segments: nextSegments,
                  lastEventId: seq ?? s.lastEventId,
                };
              });
              // Terminal frame closes the run. Stop reconnecting.
              retryCountRef.current = maxRetries;
              return;
            }

            switch (event) {
              case "model.thinking": {
                const thinking = firstString(payload, ["text"]);
                if (!thinking) return;
                const durationMs =
                  typeof payload.duration_ms === "number"
                    ? payload.duration_ms
                    : undefined;
                setState((s) => {
                  const last = s.segments[s.segments.length - 1];
                  // Partials carry the FULL text so far — replace the
                  // trailing un-finalized thinking segment in place. A
                  // finalized segment (durationMs set) means a NEW block
                  // started; append instead.
                  const nextSegments: ChatStreamSegment[] =
                    last &&
                    last.kind === "thinking" &&
                    last.durationMs === undefined
                      ? [
                          ...s.segments.slice(0, -1),
                          { kind: "thinking", text: thinking, durationMs },
                        ]
                      : [
                          ...s.segments,
                          { kind: "thinking", text: thinking, durationMs },
                        ];
                  return {
                    ...s,
                    segments: nextSegments,
                    lastEventId: seq ?? s.lastEventId,
                  };
                });
                return;
              }
              case "model.delta":
              case "message.text": {
                const delta = firstString(payload, [
                  "delta",
                  "text",
                  "content",
                ]);
                if (!delta) return;
                setState((s) => {
                  // Append to a trailing text segment if there is
                  // one; otherwise open a new text segment. This
                  // keeps the chronology readable: a model that
                  // emits text → tool → more text yields three
                  // segments, not one fused string.
                  const last = s.segments[s.segments.length - 1];
                  let nextSegments: ChatStreamSegment[];
                  if (last && last.kind === "text") {
                    nextSegments = [
                      ...s.segments.slice(0, -1),
                      { kind: "text", text: last.text + delta },
                    ];
                  } else {
                    nextSegments = [
                      ...s.segments,
                      { kind: "text", text: delta },
                    ];
                  }
                  return {
                    ...s,
                    text: s.text + delta,
                    segments: nextSegments,
                    lastEventId: seq ?? s.lastEventId,
                    connected: true,
                  };
                });
                return;
              }
              case "tool.call":
              case "message.tool_call": {
                const tc: ChatToolCallRecord = {
                  id: firstString(payload, ["id", "tool_use_id"]),
                  name:
                    firstString(payload, ["name", "tool_name"]) ?? "(unknown)",
                  args: firstValue(payload, ["args", "input"]),
                };
                setState((s) => {
                  const toolIdx = s.toolCalls.length;
                  return {
                    ...s,
                    toolCalls: [...s.toolCalls, tc],
                    segments: [
                      ...s.segments,
                      { kind: "tool", toolIdx } as ChatStreamSegment,
                    ],
                    lastEventId: seq ?? s.lastEventId,
                    connected: true,
                  };
                });
                return;
              }
              case "tool.result":
              case "message.tool_result": {
                setState((s) => {
                  // Match against the trailing tool_call by id; if
                  // the id isn't surfaced (older server), patch the
                  // last entry. This is a UI niceness — the
                  // authoritative tool_calls list lives on the
                  // message row and is fetched via useChatMessage
                  // when the turn finalizes.
                  const id = firstString(payload, ["id", "tool_use_id"]);
                  const next = [...s.toolCalls];
                  const idx = id
                    ? next.findIndex((c) => c.id === id)
                    : next.length - 1;
                  if (idx >= 0) {
                    next[idx] = {
                      ...next[idx],
                      result: firstValue(payload, ["result", "content"]),
                      is_error: Boolean(payload.is_error),
                    };
                  }
                  return {
                    ...s,
                    toolCalls: next,
                    lastEventId: seq ?? s.lastEventId,
                    connected: true,
                  };
                });
                return;
              }
              case "approval.required": {
                const approval: ChatApprovalRecord = {
                  approval_id:
                    firstString(payload, ["approval_id", "id"]) ?? "",
                  tool_name:
                    firstString(payload, ["tool_name", "tool", "name"]) ??
                    "(unknown)",
                  args: firstValue(payload, ["args", "input"]),
                  expires_at: firstString(payload, ["expires_at"]),
                };
                if (!approval.approval_id) return;
                setState((s) => ({
                  ...s,
                  approvals: [...s.approvals, approval],
                  lastEventId: seq ?? s.lastEventId,
                  connected: true,
                }));
                return;
              }
              case "approval.decided": {
                // Remove the resolved approval from the pending list.
                const id =
                  typeof payload.approval_id === "string"
                    ? (payload.approval_id as string)
                    : undefined;
                if (!id) return;
                setState((s) => ({
                  ...s,
                  approvals: s.approvals.filter((a) => a.approval_id !== id),
                  lastEventId: seq ?? s.lastEventId,
                  connected: true,
                }));
                return;
              }
              case "cancel.requested":
                // Soft-cancel intent. The terminal canceled frame
                // will land shortly; surface a UI hint via
                // connected state.
                setState((s) => ({
                  ...s,
                  connected: true,
                  lastEventId: seq ?? s.lastEventId,
                }));
                return;
              default:
                // Forward-compat: unknown events advance lastEventId
                // so resume past them works, but don't accumulate
                // domain state.
                if (seq) {
                  setState((s) => ({ ...s, lastEventId: seq }));
                }
                return;
            }
          },
          onClose: () => {
            if (cancelled) return;
            // If a terminal event was received we already stopped
            // reconnecting (retryCountRef === maxRetries). Otherwise
            // schedule a backoff reconnect — covers transient
            // network blips mid-turn.
            setState((s) => ({ ...s, connected: false }));
            if (retryCountRef.current >= maxRetries) return;
            retryCountRef.current += 1;
            const delay = retryBaseDelayMs * retryCountRef.current;
            timer = setTimeout(open, delay);
          },
        },
      );
      controllerRef.current = controller;
    };

    open();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      controllerRef.current?.abort();
      controllerRef.current = null;
    };
  }, [sessionId, messageId, paused, maxRetries]);

  return state;
}

export { terminalEventNames };
