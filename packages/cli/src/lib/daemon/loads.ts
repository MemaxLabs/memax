// The daemon's side of the compile-load queue (lib/hook/loads.ts): it
// watches ~/.memax/daemon/loads/ and reports each load the session-start
// hook queued, soon after the hook exits, and retries the ones that
// couldn't be sent (offline, signed out) every minute, for up to a day.
import { createHash } from "node:crypto";
import { mkdirSync, watch, type FSWatcher } from "node:fs";
import type { V2 } from "memax-sdk";
import {
  flushLoads,
  pruneSessions,
  type QueuedLoad,
  type ReportOutcome,
} from "../hook/loads.js";
import { failureOf, type DaemonApi } from "./api.js";
import type { Logger } from "./log.js";
import type { DaemonPaths } from "./paths.js";

/** What a failed report means for the load. */
export function outcomeOf(err: unknown): ReportOutcome {
  const f = failureOf(err);
  if (f.status === 0 || f.status === 401 || f.status === 429) return "retry";
  if (f.status >= 500) return "retry";
  return "refused";
}

/**
 * A load's Idempotency-Key, from what it says: the same load sent twice
 * (a retry, or two flushers) is recorded once.
 */
export function loadKey(l: QueuedLoad): string {
  const h = createHash("sha256")
    .update(
      [l.space, l.compile, l.agent, l.session_ref ?? "", l.loaded_at].join(
        "\u0000",
      ),
    )
    .digest("hex");
  return `memax-cli-load-${h.slice(0, 48)}`;
}

/** Sends one load; never throws. */
export async function reportLoad(
  api: DaemonApi,
  l: QueuedLoad,
): Promise<ReportOutcome> {
  try {
    await api.recordCompileLoad(
      l.space,
      {
        compile: l.compile,
        agent: l.agent as V2.AgentKind,
        ...(l.session_ref ? { session_ref: l.session_ref } : {}),
        loaded_at: l.loaded_at,
      },
      loadKey(l),
    );
    return "recorded";
  } catch (err) {
    return outcomeOf(err);
  }
}

export interface LoadReporterOptions {
  paths: DaemonPaths;
  api: DaemonApi;
  log: Logger;
  retryMs?: number;
  debounceMs?: number;
}

export class LoadReporter {
  private watcher: FSWatcher | null = null;
  private timer: NodeJS.Timeout | null = null;
  private retry: NodeJS.Timeout | null = null;
  private running: Promise<void> | null = null;
  private again = false;
  private stopped = false;
  private lastPrune = 0;

  constructor(private readonly o: LoadReporterOptions) {}

  start(): void {
    try {
      mkdirSync(this.o.paths.loads, { recursive: true, mode: 0o700 });
      this.watcher = watch(this.o.paths.loads, { persistent: false }, () =>
        this.soon(),
      );
      this.watcher.on("error", () => {
        this.watcher?.close();
        this.watcher = null; // the minute's retry still finds them
      });
    } catch {
      this.watcher = null;
    }
    this.retry = setInterval(() => this.soon(), this.o.retryMs ?? 60_000);
    this.retry.unref?.();
    this.soon();
  }

  /** Flushes after a short pause, so a burst of loads goes in one pass. */
  soon(): void {
    if (this.stopped || this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.flush();
    }, this.o.debounceMs ?? 100);
    this.timer.unref?.();
  }

  /** One pass over the queue (another follows if loads arrived meanwhile). */
  flush(): Promise<void> {
    if (this.running) {
      this.again = true;
      return this.running;
    }
    this.running = this.pass().finally(() => {
      this.running = null;
      if (this.again && !this.stopped) {
        this.again = false;
        this.soon();
      }
    });
    return this.running;
  }

  private async pass(): Promise<void> {
    const r = await flushLoads(this.o.paths.loads, (l) =>
      reportLoad(this.o.api, l),
    );
    if (r.recorded + r.refused + r.expired > 0 || r.retry > 0)
      this.o.log.info("reported compile loads", {
        recorded: r.recorded,
        refused: r.refused,
        expired: r.expired,
        retry: r.retry,
      });
    if (Date.now() - this.lastPrune > 60 * 60_000) {
      this.lastPrune = Date.now();
      pruneSessions(this.o.paths.seen);
    }
  }

  async stop(): Promise<void> {
    this.stopped = true;
    this.watcher?.close();
    if (this.timer) clearTimeout(this.timer);
    if (this.retry) clearInterval(this.retry);
    await this.running?.catch(() => {});
  }
}
