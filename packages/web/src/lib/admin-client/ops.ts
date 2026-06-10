// Admin ops client — thin wrappers over /v1/admin/ops/*. Kept
// separate from client.ts so the ops surface can be audited in one
// place, and because the SSE subscription doesn't fit adminReq's
// parsed-body contract (it streams, not returns).

import { API_URL } from "@/lib/urls";
import { apiAuthHeaders } from "@/lib/api";
import { adminReq } from "./transport";
import type {
  AdminOpsIngestionSnapshot,
  AdminOpsJobDetail,
  AdminOpsJobListParams,
  AdminOpsJobListResponse,
  AdminOpsJobLogsResponse,
  AdminOpsJobLogsStreamEvent,
  AdminOpsPulse,
  AdminOpsStreamEvent,
} from "./ops-types";

export const adminOpsClient = {
  getPulse(windowMinutes?: number): Promise<AdminOpsPulse> {
    const qs = windowMinutes
      ? `?window_minutes=${encodeURIComponent(String(windowMinutes))}`
      : "";
    return adminReq<AdminOpsPulse>("GET", `/v1/admin/ops/pulse${qs}`);
  },

  getIngestionSnapshot(params?: {
    limit?: number;
    sinceMinutes?: number;
    stuckAgeSeconds?: number;
  }): Promise<AdminOpsIngestionSnapshot> {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.sinceMinutes)
      qs.set("since_minutes", String(params.sinceMinutes));
    if (params?.stuckAgeSeconds)
      qs.set("stuck_age_seconds", String(params.stuckAgeSeconds));
    const suffix = qs.toString() ? `?${qs}` : "";
    return adminReq<AdminOpsIngestionSnapshot>(
      "GET",
      `/v1/admin/ops/memory-ingestion${suffix}`,
    );
  },

  listJobs(
    params: AdminOpsJobListParams = {},
  ): Promise<AdminOpsJobListResponse> {
    const qs = new URLSearchParams();
    if (params.states?.length) qs.set("states", params.states.join(","));
    if (params.kinds?.length) qs.set("kinds", params.kinds.join(","));
    if (params.kind_search) qs.set("kind_search", params.kind_search);
    if (params.queues?.length) qs.set("queues", params.queues.join(","));
    if (params.queue_search) qs.set("queue_search", params.queue_search);
    if (params.cursor) qs.set("cursor", params.cursor);
    if (params.limit) qs.set("limit", String(params.limit));
    if (params.created_after) qs.set("created_after", params.created_after);
    if (params.created_before) qs.set("created_before", params.created_before);
    const suffix = qs.toString() ? `?${qs}` : "";
    return adminReq<AdminOpsJobListResponse>(
      "GET",
      `/v1/admin/ops/jobs${suffix}`,
    );
  },

  getJob(id: number | string): Promise<AdminOpsJobDetail> {
    return adminReq<AdminOpsJobDetail>("GET", `/v1/admin/ops/jobs/${id}`);
  },

  retryJob(id: number | string): Promise<{ ok: boolean }> {
    return adminReq<{ ok: boolean }>("POST", `/v1/admin/ops/jobs/${id}/retry`);
  },

  cancelJob(id: number | string): Promise<{ ok: boolean }> {
    return adminReq<{ ok: boolean }>("POST", `/v1/admin/ops/jobs/${id}/cancel`);
  },

  forceActiveMemory(id: string, note?: string): Promise<{ ok: boolean }> {
    return adminReq<{ ok: boolean }>(
      "POST",
      `/v1/admin/ops/memories/${id}/force-active`,
      note ? { note } : undefined,
    );
  },

  /**
   * One-shot logs fetch for a specific job. Used for the panel's
   * initial paint + any manual refresh; the live tail uses
   * subscribeJobLogsStream below.
   */
  getJobLogs(
    id: number | string,
    params?: { since?: string; until?: string; limit?: number },
  ): Promise<AdminOpsJobLogsResponse> {
    const qs = new URLSearchParams();
    if (params?.since) qs.set("since", params.since);
    if (params?.until) qs.set("until", params.until);
    if (params?.limit) qs.set("limit", String(params.limit));
    const suffix = qs.toString() ? `?${qs}` : "";
    return adminReq<AdminOpsJobLogsResponse>(
      "GET",
      `/v1/admin/ops/jobs/${id}/logs${suffix}`,
    );
  },
};

/**
 * subscribeOpsStream opens an SSE connection to /v1/admin/ops/stream.
 * Uses fetch + ReadableStream (not EventSource) because:
 *   1. EventSource doesn't support custom Authorization headers —
 *      we'd have to pass the bearer via query string, which admin
 *      routes intentionally reject.
 *   2. Fetch SSE gives us proper cancellation via AbortController
 *      so React components can unsubscribe cleanly on unmount.
 *
 * Reconnection is the caller's responsibility — when onClose fires,
 * the caller decides whether to retry. See useAdminOpsStream in
 * hooks/use-admin-ops.ts for the React wrapper that backs off.
 */
export function subscribeOpsStream(opts: {
  onEvent: (evt: AdminOpsStreamEvent) => void;
  onError?: (err: unknown) => void;
  onClose?: () => void;
  signal?: AbortSignal;
}): () => void {
  const controller = new AbortController();
  const combinedSignal = opts.signal
    ? mergeAbortSignals(controller.signal, opts.signal)
    : controller.signal;

  let cancelled = false;

  (async () => {
    try {
      const res = await fetch(`${API_URL}/v1/admin/ops/stream`, {
        method: "GET",
        headers: {
          ...apiAuthHeaders(),
          Accept: "text/event-stream",
        },
        signal: combinedSignal,
        cache: "no-store",
      });
      if (!res.ok || !res.body) {
        throw new Error(`SSE stream HTTP ${res.status}`);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder("utf-8");
      let buf = "";
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        // Normalize CRLF → LF so frame splitting handles both our
        // Go server's \n-delimited output AND standards-compliant
        // intermediaries that rewrite to \r\n. Also handles a
        // lone \r which is legal but rare.
        buf += decoder.decode(value, { stream: true }).replace(/\r\n?/g, "\n");

        // Parse SSE frames separated by blank lines ("\n\n"). We
        // keep the trailing partial in `buf` for the next read.
        let idx = buf.indexOf("\n\n");
        while (idx !== -1) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          processFrame(frame, opts.onEvent);
          idx = buf.indexOf("\n\n");
        }
      }
    } catch (err) {
      if (!cancelled) opts.onError?.(err);
    } finally {
      if (!cancelled) opts.onClose?.();
    }
  })();

  return () => {
    cancelled = true;
    controller.abort();
  };
}

function processFrame(
  frame: string,
  onEvent: (evt: AdminOpsStreamEvent) => void,
) {
  // One frame can contain multiple lines. Ignore ":" comment lines
  // (heartbeats) and lines without a colon. `event:` and `data:`
  // lines set the event type + payload.
  let event = "message";
  let data = "";
  for (const line of frame.split("\n")) {
    if (!line || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    if (colon === -1) continue;
    const field = line.slice(0, colon);
    const value = line.slice(colon + 1).replace(/^ /, "");
    if (field === "event") event = value;
    else if (field === "data") data += (data ? "\n" : "") + value;
  }
  if (!data) return;
  try {
    const parsed = JSON.parse(data) as AdminOpsStreamEvent;
    // Trust the server's `type` field over the event: header if
    // both are present. Keeps client tolerant to minor transport
    // drift.
    if (!parsed.type && event === "pulse") {
      parsed.type = "pulse";
    }
    onEvent(parsed);
  } catch {
    // Drop malformed frames silently — no production signal beats
    // the client telemetry we already get from onError.
  }
}

/** Merge two AbortSignals — aborting either aborts the result. */
function mergeAbortSignals(a: AbortSignal, b: AbortSignal): AbortSignal {
  const c = new AbortController();
  const onAbort = () => c.abort();
  if (a.aborted || b.aborted) c.abort();
  else {
    a.addEventListener("abort", onAbort, { once: true });
    b.addEventListener("abort", onAbort, { once: true });
  }
  return c.signal;
}

/**
 * subscribeJobLogsStream tails /v1/admin/ops/jobs/{id}/logs/stream.
 * Follows the same fetch+ReadableStream pattern as subscribeOpsStream
 * for the same reasons (auth header + clean cancellation).
 *
 * The server sends an initial `snapshot` frame on connect and `append`
 * frames thereafter, each with only the new lines since the last
 * emission. Callers typically concat append.lines onto their local
 * buffer.
 */
export function subscribeJobLogsStream(opts: {
  jobId: number | string;
  onEvent: (evt: AdminOpsJobLogsStreamEvent) => void;
  onError?: (err: unknown) => void;
  onClose?: () => void;
  signal?: AbortSignal;
}): () => void {
  const controller = new AbortController();
  const combinedSignal = opts.signal
    ? mergeAbortSignals(controller.signal, opts.signal)
    : controller.signal;

  let cancelled = false;

  (async () => {
    try {
      const res = await fetch(
        `${API_URL}/v1/admin/ops/jobs/${opts.jobId}/logs/stream`,
        {
          method: "GET",
          headers: {
            ...apiAuthHeaders(),
            Accept: "text/event-stream",
          },
          signal: combinedSignal,
          cache: "no-store",
        },
      );
      if (!res.ok || !res.body) {
        throw new Error(`Logs stream HTTP ${res.status}`);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder("utf-8");
      let buf = "";
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true }).replace(/\r\n?/g, "\n");

        let idx = buf.indexOf("\n\n");
        while (idx !== -1) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          processJobLogsFrame(frame, opts.onEvent);
          idx = buf.indexOf("\n\n");
        }
      }
    } catch (err) {
      if (!cancelled) opts.onError?.(err);
    } finally {
      if (!cancelled) opts.onClose?.();
    }
  })();

  return () => {
    cancelled = true;
    controller.abort();
  };
}

function processJobLogsFrame(
  frame: string,
  onEvent: (evt: AdminOpsJobLogsStreamEvent) => void,
) {
  let event = "message";
  let data = "";
  for (const line of frame.split("\n")) {
    if (!line || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    if (colon === -1) continue;
    const field = line.slice(0, colon);
    const value = line.slice(colon + 1).replace(/^ /, "");
    if (field === "event") event = value;
    else if (field === "data") data += (data ? "\n" : "") + value;
  }
  if (!data) return;
  try {
    const parsed = JSON.parse(data) as AdminOpsJobLogsStreamEvent;
    // Trust server-emitted `type` first; fall back to the event:
    // header. Required for frames the backend emits without an
    // explicit type field.
    if (!parsed.type) {
      if (event === "snapshot" || event === "append" || event === "error") {
        parsed.type = event;
      } else {
        parsed.type = "append";
      }
    }
    onEvent(parsed);
  } catch {
    // Silently drop malformed frames.
  }
}
