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
}

/** No single request may hold the loop up for longer than this. */
export const REQUEST_TIMEOUT_MS = 15_000;

function timed(signal?: AbortSignal): AbortSignal {
  const t = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  return signal ? AbortSignal.any([signal, t]) : t;
}

/** The daemon's calls through memax-sdk, as the CLI (`X-Memax-Via: cli`). */
export function sdkDaemonApi(memax: Memax): DaemonApi {
  const t = memax.v2.targets;
  return {
    async listTargets(space, signal) {
      return (await t.list(space, { signal: timed(signal) })).items;
    },
    async changeToken(space, signal) {
      const page = await memax.v2.receipts.list(space, {
        limit: 1,
        signal: timed(signal),
      });
      return page.items[0]?.id ?? "";
    },
    preview: (target, signal) => t.preview(target, { signal: timed(signal) }),
    async runs(target, limit, signal) {
      return (await t.runs(target, { limit, signal: timed(signal) })).items;
    },
    deliver: (target, input, key, signal) =>
      t.deliver(target, input, {
        idempotencyKey: key,
        via: "cli",
        signal: timed(signal),
      }),
    observe: (target, input, key, signal) =>
      t.observe(target, input, {
        idempotencyKey: key,
        via: "cli",
        signal: timed(signal),
      }),
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
