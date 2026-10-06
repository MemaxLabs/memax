// How the daemon learns that a space's targets changed. Today that is a
// poll of GET /v2/spaces/{space}/targets: every couple of seconds while a
// compile or a delivery is in flight, slower when nothing moves, backing
// off on errors, with jitter so devices don't poll in step. When /v2
// serves `target.changed` over SSE, a feed that listens to it replaces
// PollingFeed behind the same interface.
import type { V2 } from "memax-sdk";
import { failureOf } from "./api.js";

export interface FeedHandlers {
  fetch(signal: AbortSignal): Promise<V2.Target[]>;
  /** Handles one list; `busy` asks for the next one soon. */
  onTargets(targets: V2.Target[]): Promise<{ busy: boolean }>;
  onError(err: unknown, retryInMs: number): void;
}

export interface TargetFeed {
  start(): void;
  /** Fetch now (a link, a compile request, a local edit). */
  wake(): void;
  stop(): Promise<void>;
}

export interface Cadence {
  /** While a compile or a delivery is in flight. */
  busyMs: number;
  /** Idle, soon after a change. */
  idleMs: number;
  /** Idle for longer than `slowAfterMs`. */
  slowMs: number;
  slowAfterMs: number;
  /** Busy for longer than this without any change: poll at idleMs. */
  stuckAfterMs: number;
  errorBaseMs: number;
  errorMaxMs: number;
  /** ± this fraction of every delay. */
  jitter: number;
}

// Keep → file on disk has to fit in 10 s (N1): a Keep is seen within one
// idle interval, its run compiles in about 2 s, and the busy poll picks it
// up within 2 s more.
export const DEFAULT_CADENCE: Cadence = {
  busyMs: 2_000,
  idleMs: 5_000,
  slowMs: 15_000,
  slowAfterMs: 30 * 60_000,
  stuckAfterMs: 2 * 60_000,
  errorBaseMs: 2_000,
  errorMaxMs: 5 * 60_000,
  jitter: 0.2,
};

/** What a list says about change: runs, states, versions. */
function signature(targets: V2.Target[]): string {
  return targets
    .map(
      (t) =>
        `${t.id}:${t.version}:${t.sync_state}:${t.dirty_gen}:${t.last_compile?.ref ?? ""}`,
    )
    .sort()
    .join("|");
}

/** How long to wait after a failure: exponential, with the server's advice. */
export function errorDelay(err: unknown, failures: number, c: Cadence): number {
  const f = failureOf(err);
  let ms = Math.min(
    c.errorMaxMs,
    c.errorBaseMs * 2 ** Math.max(0, failures - 1),
  );
  if (f.status === 401) ms = Math.max(ms, 60_000); // signed out: wait for `memax login`
  if (f.status === 403 || f.status === 404) ms = Math.max(ms, 5 * 60_000);
  if (f.retryAfterMs) ms = Math.max(ms, f.retryAfterMs);
  return Math.min(ms, Math.max(c.errorMaxMs, f.retryAfterMs ?? 0));
}

export class PollingFeed implements TargetFeed {
  private readonly c: Cadence;
  private abort = new AbortController();
  private timer: NodeJS.Timeout | null = null;
  private running: Promise<void> | null = null;
  private stopped = true;
  private woken = false;
  private failures = 0;
  private lastSig = "";
  private lastChange = Date.now();

  constructor(
    private readonly h: FeedHandlers,
    cadence: Partial<Cadence> = {},
    private readonly random: () => number = Math.random,
  ) {
    this.c = { ...DEFAULT_CADENCE, ...cadence };
  }

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.abort = new AbortController();
    this.schedule(0);
  }

  wake(): void {
    if (this.stopped) return;
    this.lastChange = Date.now();
    if (this.running) {
      this.woken = true; // poll again as soon as this one ends
      return;
    }
    this.schedule(0);
  }

  async stop(): Promise<void> {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.abort.abort();
    await this.running?.catch(() => {});
  }

  private schedule(ms: number): void {
    if (this.stopped) return;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = null;
      this.running = this.tick().finally(() => {
        this.running = null;
      });
    }, ms);
  }

  private async tick(): Promise<void> {
    let delay: number;
    try {
      const targets = await this.h.fetch(this.abort.signal);
      const { busy } = await this.h.onTargets(targets);
      this.failures = 0;
      delay = this.nextDelay(targets, busy);
    } catch (err) {
      if (this.stopped) return;
      this.failures++;
      delay = errorDelay(err, this.failures, this.c);
      this.h.onError(err, delay);
    }
    if (this.woken) {
      this.woken = false;
      delay = 0;
    }
    const spread = 1 + (this.random() * 2 - 1) * this.c.jitter;
    this.schedule(Math.round(delay * spread));
  }

  private nextDelay(targets: V2.Target[], busy: boolean): number {
    const now = Date.now();
    const sig = signature(targets);
    if (sig !== this.lastSig) {
      this.lastSig = sig;
      this.lastChange = now;
    }
    if (busy) {
      // A worker that never compiles mustn't keep every device at 2 s.
      const stuck = now - this.lastChange > this.c.stuckAfterMs;
      return stuck ? this.c.idleMs : this.c.busyMs;
    }
    return now - this.lastChange > this.c.slowAfterMs
      ? this.c.slowMs
      : this.c.idleMs;
  }
}
