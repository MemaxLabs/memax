// The /v2 calls the daemon makes, behind an interface so tests can run it
// against a fake server and SSE can later replace the polling.
import { createHash } from "node:crypto";
import type { Memax, V2 } from "memax-sdk";

export interface DaemonApi {
  listTargets(space: string, signal?: AbortSignal): Promise<V2.Target[]>;
  /** The id of the space's newest receipt: it moves whenever the space changes. */
  changeToken(space: string, signal?: AbortSignal): Promise<string>;
  preview(target: string, signal?: AbortSignal): Promise<V2.TargetPreview>;
  runs(
    target: string,
    limit: number,
    signal?: AbortSignal,
  ): Promise<V2.CompileRun[]>;
  deliver(
    target: string,
    input: V2.DeliveryInput,
    key: string,
    signal?: AbortSignal,
  ): Promise<V2.DeliveryResult>;
  observe(
    target: string,
    input: V2.ObservationInput,
    key: string,
    signal?: AbortSignal,
  ): Promise<V2.ObservationResult>;
  /** The space's gates waiting for a person, newest first (the warm cache). */
  waitingGates(space: string, signal?: AbortSignal): Promise<V2.Gate[]>;
  /** The space's latest tombstones, newest first (the warm cache). */
  tombstones(space: string, signal?: AbortSignal): Promise<V2.Tombstone[]>;
  /** Reports a compile a session-start hook saw its agent load. */
  recordCompileLoad(
    space: string,
    input: V2.CompileLoadInput,
    key: string,
    signal?: AbortSignal,
  ): Promise<V2.CompileLoadResult>;
}

/** No single request may hold the loop up for longer than this. */
export const REQUEST_TIMEOUT_MS = 15_000;

/**
 * Runs `fn` with a signal that aborts after REQUEST_TIMEOUT_MS or when
 * `outer` does. A controller per request whose listener is taken off
 * afterwards, rather than AbortSignal.any() on the feed's long-lived
 * signal, so nothing accumulates over a day of polls.
 */
async function timed<T>(
  outer: AbortSignal | undefined,
  fn: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  const ctl = new AbortController();
  const timer = setTimeout(
    () => ctl.abort(new DOMException("The request timed out", "TimeoutError")),
    REQUEST_TIMEOUT_MS,
  );
  const onAbort = () => ctl.abort(outer?.reason);
  if (outer?.aborted) onAbort();
  else outer?.addEventListener("abort", onAbort, { once: true });
  try {
    return await fn(ctl.signal);
  } finally {
    clearTimeout(timer);
    outer?.removeEventListener("abort", onAbort);
  }
}

/** The daemon's calls through memax-sdk, as the CLI (`X-Memax-Via: cli`). */
export function sdkDaemonApi(memax: Memax): DaemonApi {
  const t = memax.v2.targets;
  return {
    listTargets: (space, outer) =>
      timed(outer, async (signal) => (await t.list(space, { signal })).items),
    changeToken: (space, outer) =>
      timed(
        outer,
        async (signal) =>
          (await memax.v2.receipts.list(space, { limit: 1, signal })).items[0]
            ?.id ?? "",
      ),
    preview: (target, outer) =>
      timed(outer, (signal) => t.preview(target, { signal })),
    runs: (target, limit, outer) =>
      timed(
        outer,
        async (signal) => (await t.runs(target, { limit, signal })).items,
      ),
    deliver: (target, input, key, outer) =>
      timed(outer, (signal) =>
        t.deliver(target, input, { idempotencyKey: key, via: "cli", signal }),
      ),
    observe: (target, input, key, outer) =>
      timed(outer, (signal) =>
        t.observe(target, input, { idempotencyKey: key, via: "cli", signal }),
      ),
    waitingGates: (space, outer) =>
      timed(
        outer,
        async (signal) =>
          (
            await memax.v2.gates.list(space, {
              status: "waiting",
              limit: 20,
              signal,
            })
          ).items,
      ),
    tombstones: (space, outer) =>
      timed(
        outer,
        async (signal) =>
          (await memax.v2.memories.tombstones(space, { limit: 50, signal }))
            .tombstones,
      ),
    recordCompileLoad: (space, input, key, outer) =>
      timed(outer, (signal) =>
        memax.v2.reads.recordCompileLoad(space, input, {
          idempotencyKey: key,
          via: "cli",
          signal,
        }),
      ),
  };
}

/**
 * A stable Idempotency-Key for one command: a retry after a lost answer
 * sends the same key, and the server returns the first result instead of
 * writing twice. The target's version is part of it, so the same edit
 * reported again after a person resolved it is a new command.
 */
export function commandKey(
  kind: "dlv" | "obs",
  ...parts: Array<string | number>
): string {
  const h = createHash("sha256").update(parts.join("\u0000")).digest("hex");
  return `memax-cli-${kind}-${h.slice(0, 48)}`;
}

export interface ApiFailure {
  status: number;
  code: string;
  retryAfterMs?: number;
}

/** The status, code and Retry-After of a failed call, whatever threw it. */
export function failureOf(err: unknown): ApiFailure {
  const e = err as {
    status?: number;
    code?: string;
    retryAfter?: number;
    retryAfterSeconds?: number;
    name?: string;
  };
  if (e?.name === "AbortError" || e?.name === "TimeoutError") {
    return { status: 0, code: "timeout" };
  }
  const seconds =
    typeof e?.retryAfterSeconds === "number"
      ? e.retryAfterSeconds
      : e?.retryAfter;
  return {
    status: typeof e?.status === "number" ? e.status : 0,
    code: typeof e?.code === "string" ? e.code : "network_error",
    retryAfterMs:
      typeof seconds === "number" && seconds > 0 ? seconds * 1000 : undefined,
  };
}
