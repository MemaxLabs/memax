"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import {
  useQuery,
  useMutation,
  useQueryClient,
  useInfiniteQuery,
  keepPreviousData,
} from "@tanstack/react-query";

import {
  adminOpsClient,
  subscribeJobLogsStream,
  subscribeOpsStream,
} from "@/lib/admin-client";
import type {
  AdminOpsIngestionSnapshot,
  AdminOpsJobDetail,
  AdminOpsJobListParams,
  AdminOpsJobListResponse,
  AdminOpsJobLogLine,
  AdminOpsJobLogsStreamEvent,
  AdminOpsPulse,
  AdminOpsStreamEvent,
} from "@/lib/admin-client";
import { useLocale } from "@/i18n";

// --- Query keys ---

const adminOpsKey = ["admin", "ops"] as const;
const adminOpsPulseKey = [...adminOpsKey, "pulse"] as const;
const adminOpsIngestionKey = [...adminOpsKey, "ingestion"] as const;
const adminOpsJobsKey = [...adminOpsKey, "jobs"] as const;

// --- Queries ---

/**
 * useAdminOpsPulse fetches the pulse snapshot for dashboards that
 * need a one-shot read. For live updates, use useAdminOpsStream —
 * this hook is fine as a fallback when SSE is unsupported or the
 * page just wants a snapshot (e.g., print view).
 */
export function useAdminOpsPulse(windowMinutes = 60) {
  return useQuery<AdminOpsPulse>({
    queryKey: [...adminOpsPulseKey, windowMinutes],
    queryFn: () => adminOpsClient.getPulse(windowMinutes),
    staleTime: 10 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function useAdminOpsIngestion(params?: {
  limit?: number;
  sinceMinutes?: number;
  stuckAgeSeconds?: number;
}) {
  return useQuery<AdminOpsIngestionSnapshot>({
    queryKey: [...adminOpsIngestionKey, params ?? {}],
    queryFn: () => adminOpsClient.getIngestionSnapshot(params),
    staleTime: 10 * 1000,
  });
}

export function useAdminOpsJobs(params: AdminOpsJobListParams = {}) {
  // useInfiniteQuery manages the cursor + page accumulation in the
  // TanStack cache, not component state. That means:
  //   - Accumulation survives navigate-away / back navigation.
  //   - Invalidation (after retry/cancel) refetches each loaded page
  //     in order, so the stitched list stays consistent.
  //   - Fast "Load more" clicks can't race into wrong orderings
  //     because the query key pins each page.
  //
  // Filter changes create a new queryKey, so the old pages are
  // dropped automatically.
  return useInfiniteQuery<AdminOpsJobListResponse>({
    queryKey: [...adminOpsJobsKey, params],
    queryFn: ({ pageParam }) =>
      adminOpsClient.listJobs({
        ...params,
        cursor: pageParam as string | undefined,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.has_more ? last.next_cursor : undefined),
    staleTime: 5 * 1000,
    placeholderData: keepPreviousData,
  });
}

export function useAdminOpsJob(jobId: number | string | null | undefined) {
  return useQuery<AdminOpsJobDetail>({
    queryKey: [...adminOpsJobsKey, "detail", jobId],
    queryFn: () => adminOpsClient.getJob(jobId as number | string),
    enabled: !!jobId,
    staleTime: 5 * 1000,
  });
}

// --- Mutations ---

/** Retries a job via the admin endpoint. */
export function useRetryJob() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.admin.infra.ops.actionFailed,
    },
    mutationFn: (jobId: number) => adminOpsClient.retryJob(jobId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminOpsKey });
    },
  });
}

export function useCancelJob() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.admin.infra.ops.actionFailed,
    },
    mutationFn: (jobId: number) => adminOpsClient.cancelJob(jobId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminOpsKey });
    },
  });
}

export function useForceActiveMemory() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.admin.infra.ingestion.stuck.forceActiveError,
    },
    mutationFn: ({ id, note }: { id: string; note?: string }) =>
      adminOpsClient.forceActiveMemory(id, note),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminOpsIngestionKey });
      qc.invalidateQueries({ queryKey: adminOpsPulseKey });
    },
  });
}

// --- SSE stream ---

export type AdminOpsStreamState = {
  pulse: AdminOpsPulse | null;
  connected: boolean;
  error: unknown;
  lastEventAt: number;
};

/**
 * useAdminOpsStream subscribes to /v1/admin/ops/stream and exposes a
 * state hook backed by useSyncExternalStore. Reconnects with
 * exponential backoff (2s → 4s → 8s, capped at 30s).
 *
 * The hook DOES NOT write to the TanStack Query cache — the stream
 * is ephemeral live state and should be treated as "what the pulse
 * says right now." Components that also want the cache (e.g., to
 * hydrate from a router preload) should combine this with
 * useAdminOpsPulse and prefer whichever has the newer generated_at.
 */
export function useAdminOpsStream(): AdminOpsStreamState {
  const [, forceTick] = useState(0);
  const stateRef = useRef<AdminOpsStreamState>({
    pulse: null,
    connected: false,
    error: null,
    lastEventAt: 0,
  });

  useEffect(() => {
    let cancelled = false;
    let unsubscribe: (() => void) | null = null;
    let retryDelay = 2000;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    const schedule = () => {
      if (cancelled) return;
      if (retryTimer) clearTimeout(retryTimer);
      retryTimer = setTimeout(() => {
        retryDelay = Math.min(retryDelay * 2, 30_000);
        connect();
      }, retryDelay);
    };

    const commit = (next: Partial<AdminOpsStreamState>) => {
      stateRef.current = { ...stateRef.current, ...next };
      forceTick((n) => n + 1);
    };

    const connect = () => {
      commit({ connected: false, error: null });
      unsubscribe = subscribeOpsStream({
        onEvent: (evt: AdminOpsStreamEvent) => {
          if (cancelled) return;
          retryDelay = 2000; // reset backoff on any event, including heartbeat
          if (evt.type === "pulse" && evt.pulse) {
            commit({
              pulse: evt.pulse,
              connected: true,
              error: null,
              lastEventAt: Date.now(),
            });
          } else {
            commit({ connected: true, lastEventAt: Date.now() });
          }
        },
        onError: (err) => {
          if (cancelled) return;
          commit({ connected: false, error: err });
        },
        onClose: () => {
          if (cancelled) return;
          commit({ connected: false });
          schedule();
        },
      });
    };

    connect();

    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      unsubscribe?.();
    };
  }, []);

  return stateRef.current;
}

// --- Job logs (per-job live tail) ---

export interface AdminOpsJobLogsState {
  lines: AdminOpsJobLogLine[];
  environment: string;
  connected: boolean;
  error: unknown | null;
  initialLoaded: boolean;
  /** Unavailable = server returned 503 (Loki not configured). */
  unavailable: boolean;
  lastEventAt: number;
}

/**
 * useJobLogs tails the per-job logs stream. Unlike useAdminOpsStream
 * we append lines as they arrive rather than replacing a single
 * snapshot. The caller typically renders `lines` in a virtualized
 * list with auto-scroll.
 *
 * `environment` is sourced from the server (baked into the query
 * client from MEMAX_ENV) so the UI can trustably label which env's
 * logs it's showing, without relying on any client-side guess.
 *
 * Reconnects on transient errors with exponential backoff. A 503
 * from the server (Loki unconfigured) sets `unavailable: true`
 * and stops retrying — UI shows a clear empty state instead of a
 * perpetual spinner.
 */
export function useJobLogs(
  jobId: number | string | null | undefined,
): AdminOpsJobLogsState {
  const [, forceTick] = useState(0);
  const stateRef = useRef<AdminOpsJobLogsState>({
    lines: [],
    environment: "",
    connected: false,
    error: null,
    initialLoaded: false,
    unavailable: false,
    lastEventAt: 0,
  });

  // When jobId changes, reset the buffer rather than leaking the
  // previous job's lines into the new subscription.
  const lastJobIdRef = useRef(jobId);
  if (lastJobIdRef.current !== jobId) {
    stateRef.current = {
      lines: [],
      environment: "",
      connected: false,
      error: null,
      initialLoaded: false,
      unavailable: false,
      lastEventAt: 0,
    };
    lastJobIdRef.current = jobId;
  }

  useEffect(() => {
    if (jobId === null || jobId === undefined) return;
    let cancelled = false;
    let unsubscribe: (() => void) | null = null;
    let retryDelay = 2000;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    const commit = (next: Partial<AdminOpsJobLogsState>) => {
      stateRef.current = { ...stateRef.current, ...next };
      forceTick((n) => n + 1);
    };

    const schedule = () => {
      if (cancelled || stateRef.current.unavailable) return;
      if (retryTimer) clearTimeout(retryTimer);
      retryTimer = setTimeout(() => {
        retryDelay = Math.min(retryDelay * 2, 30_000);
        connect();
      }, retryDelay);
    };

    const connect = () => {
      commit({ connected: false, error: null });
      unsubscribe = subscribeJobLogsStream({
        jobId,
        onEvent: (evt: AdminOpsJobLogsStreamEvent) => {
          if (cancelled) return;
          if (evt.type === "snapshot") {
            // Successful initial load resets backoff — we know the
            // whole path (auth, route, Loki) works.
            retryDelay = 2000;
            commit({
              lines: evt.lines ?? [],
              environment: evt.environment ?? stateRef.current.environment,
              connected: true,
              error: null,
              initialLoaded: true,
              lastEventAt: Date.now(),
            });
          } else if (evt.type === "append") {
            retryDelay = 2000;
            const incoming = evt.lines ?? [];
            if (incoming.length === 0) return;
            // Append with a safety cap to keep the DOM/virt list
            // bounded even on a runaway job. 5k lines covers any
            // reasonable operator session.
            const MAX_LINES = 5000;
            const merged = [...stateRef.current.lines, ...incoming];
            const trimmed =
              merged.length > MAX_LINES ? merged.slice(-MAX_LINES) : merged;
            commit({
              lines: trimmed,
              environment: evt.environment ?? stateRef.current.environment,
              connected: true,
              error: null,
              lastEventAt: Date.now(),
            });
          } else if (evt.type === "error") {
            // Do NOT reset backoff on server-side error events. The
            // stream closes right after one of these, and if we
            // reset here every reconnect that fails again stays at
            // the 2s floor — a tight retry loop while Loki is down.
            commit({
              connected: false,
              error: new Error(evt.error ?? "stream_error"),
            });
          }
        },
        onError: (err) => {
          if (cancelled) return;
          // Status 503 surfaces as an HTTP error in the subscription
          // layer; detect it so we don't retry forever against a
          // dev env with no Loki.
          const msg = err instanceof Error ? err.message : String(err);
          const unavailable = /HTTP 503/.test(msg);
          commit({
            connected: false,
            error: err,
            unavailable,
            initialLoaded: true,
          });
        },
        onClose: () => {
          if (cancelled) return;
          commit({ connected: false });
          schedule();
        },
      });
    };

    connect();

    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      unsubscribe?.();
    };
  }, [jobId]);

  return stateRef.current;
}

// useSyncExternalStore is exported for downstream hooks that want a
// stronger guarantee of stable references. Right now we use
// useState+ref — good enough for the ops dashboard's volume.
export { useSyncExternalStore };
