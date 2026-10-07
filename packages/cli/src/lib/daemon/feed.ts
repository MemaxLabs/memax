// How the daemon learns that a space's targets changed. Today that is a
// poll: every couple of seconds while a compile or a delivery is in flight,
// slower when nothing moves, backing off on errors, with jitter so devices
// don't poll in step. When /v2 serves `target.changed` over SSE, a feed
// that listens to it replaces PollingFeed behind the same interface.
//
// An idle poll is a probe: the space's newest receipt (about 1 KB), not
// its targets (about 9 KB, uncompressed). Every change that matters here
// writes a receipt (a Keep, a Brief revision, a target change, a request,
// a delivery, a hand edit), so the targets are fetched only when the probe
// moves, while something is in flight (a compile run writes no receipt),
// after a wake, and every few minutes regardless.
import type { V2 } from "memax-sdk";
import { failureOf } from "./api.js";

export interface FeedHandlers {
  fetch(signal: AbortSignal): Promise<V2.Target[]>;
  /** A cheap token that changes whenever the space does. */
  probe?(signal: AbortSignal): Promise<string>;
  /**
   * Handles one list; `busy` asks for the next one soon. `changed` is false
   * only for a busy re-poll (a compile in flight): any other list may
   * follow a change the targets don't show, such as a gate or a Forget.
   */
  onTargets(
    targets: V2.Target[],
    info: { changed: boolean },
  ): Promise<{ busy: boolean }>;
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
  /** Fetch the targets at least this often, whatever the probe says. */
  fullEveryMs: number;
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
  fullEveryMs: 10 * 60_000,
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
  /** Fetch the targets on the next tick (start, wake, busy, an error). */
  private needFull = true;
  private lastFull = 0;
  private lastToken: string | null = null;
  private lastBusy = false;

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
    this.needFull = true;
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
      const signal = this.abort.signal;
      const now = Date.now();
      let token: string | null = null;
      let full =
        this.needFull ||
        this.lastBusy ||
        !this.h.probe ||
        now - this.lastFull > this.c.fullEveryMs;
      if (!full && this.h.probe) {
        token = await this.h.probe(signal);
        full = token !== this.lastToken;
      }
      if (full) {
        const changed = this.needFull || !this.lastBusy;
        this.needFull = false;
        const targets = await this.h.fetch(signal);
        const { busy } = await this.h.onTargets(targets, { changed });
        this.lastFull = Date.now();
        this.lastBusy = busy;
        // The token read before this list, or none: the next probe then
        // refetches once and settles.
        this.lastToken = token;
        delay = this.nextDelay(targets, busy);
      } else {
        delay = this.idleDelay(Date.now());
      }
      this.failures = 0;
    } catch (err) {
      if (this.stopped) return;
      this.failures++;
      this.needFull = true;
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
    return this.idleDelay(now);
  }

  private idleDelay(now: number): number {
    return now - this.lastChange > this.c.slowAfterMs
      ? this.c.slowMs
      : this.c.idleMs;
  }
}
