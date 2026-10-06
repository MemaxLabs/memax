// The polling feed: when it fetches the targets, when it only probes, and
// how it backs off.
import type { V2 } from "memax-sdk";
import { MemaxError } from "memax-sdk";
import { describe, expect, it } from "vitest";
import {
  DEFAULT_CADENCE,
  errorDelay,
  PollingFeed,
  type FeedHandlers,
} from "../../src/lib/daemon/feed.js";

const target = (state: V2.SyncState, ref = "C-0881") =>
  ({
    id: "t",
    version: 1,
    sync_state: state,
    dirty_gen: 1,
    last_compile: { ref },
  }) as unknown as V2.Target;

function rig(
  opts: {
    state?: () => V2.SyncState;
    token?: () => string;
    busy?: () => boolean;
    fail?: () => unknown;
  } = {},
) {
  const calls: string[] = [];
  const h: FeedHandlers = {
    async fetch() {
      calls.push("fetch");
      const err = opts.fail?.();
      if (err) throw err;
      return [target(opts.state?.() ?? "in_sync")];
    },
    async probe() {
      calls.push("probe");
      return opts.token?.() ?? "r-1";
    },
    async onTargets() {
      return { busy: opts.busy?.() ?? false };
    },
    onError() {
      calls.push("error");
    },
  };
  return { h, calls };
}

const cadence = {
  busyMs: 10,
  idleMs: 30,
  slowMs: 60,
  slowAfterMs: 10_000,
  stuckAfterMs: 10_000,
  errorBaseMs: 10,
  errorMaxMs: 80,
  fullEveryMs: 10_000,
  jitter: 0,
};
const run = async (feed: PollingFeed, ms: number) => {
  feed.start();
  await new Promise((r) => setTimeout(r, ms));
  await feed.stop();
};

describe("PollingFeed", () => {
  it("only probes while idle and nothing changes, and fetches when the probe moves", async () => {
    let token = "r-1";
    const { h, calls } = rig({ token: () => token });
    const feed = new PollingFeed(h, cadence);
    feed.start();
    await new Promise((r) => setTimeout(r, 200));
    const fetches = calls.filter((c) => c === "fetch").length;
    expect(fetches).toBeLessThanOrEqual(2); // the first, then once to learn the token
    expect(calls.filter((c) => c === "probe").length).toBeGreaterThan(3);
    token = "r-2";
    await new Promise((r) => setTimeout(r, 80));
    await feed.stop();
    expect(calls.filter((c) => c === "fetch").length).toBe(fetches + 1);
  });

  it("fetches every busy interval while something is in flight", async () => {
    const { h, calls } = rig({ busy: () => true, state: () => "compiling" });
    await run(new PollingFeed(h, cadence), 120);
    expect(calls.filter((c) => c === "probe")).toHaveLength(0);
    expect(calls.filter((c) => c === "fetch").length).toBeGreaterThanOrEqual(6);
  });

  it("fetches at once on a wake", async () => {
    const { h, calls } = rig();
    const feed = new PollingFeed(h, { ...cadence, idleMs: 5_000 });
    feed.start();
    await new Promise((r) => setTimeout(r, 30));
    expect(calls).toEqual(["fetch"]);
    feed.wake();
    await new Promise((r) => setTimeout(r, 30));
    await feed.stop();
    expect(calls).toEqual(["fetch", "fetch"]);
  });

  it("backs off on errors, and fetches again once they clear", async () => {
    let failing = true;
    const { h, calls } = rig({
      fail: () =>
        failing ? new MemaxError("down", "unavailable", 503) : undefined,
    });
    const feed = new PollingFeed(h, cadence);
    feed.start();
    await new Promise((r) => setTimeout(r, 160));
    const errors = calls.filter((c) => c === "error").length;
    // 10, 20, 40, 80 ms: at most four or five failures in 160 ms.
    expect(errors).toBeGreaterThanOrEqual(3);
    expect(errors).toBeLessThanOrEqual(5);
    failing = false;
    await new Promise((r) => setTimeout(r, 120));
    await feed.stop();
    expect(calls.at(-1)).not.toBe("error");
  });

  it("waits as long as the server asks, and longer when signed out or removed", () => {
    const c = DEFAULT_CADENCE;
    expect(errorDelay(new MemaxError("x", "busy", 503), 1, c)).toBe(2_000);
    expect(errorDelay(new MemaxError("x", "busy", 503), 4, c)).toBe(16_000);
    expect(errorDelay(new MemaxError("x", "busy", 503), 20, c)).toBe(
      c.errorMaxMs,
    );
    expect(
      errorDelay(new MemaxError("x", "rate_limited", 429, undefined, 30), 1, c),
    ).toBe(30_000);
    expect(errorDelay(new MemaxError("x", "unauthorized", 401), 1, c)).toBe(
      60_000,
    );
    expect(errorDelay(new MemaxError("x", "not_found", 404), 1, c)).toBe(
      5 * 60_000,
    );
  });

  it("spreads its polls with jitter", async () => {
    const at: number[] = [];
    const h: FeedHandlers = {
      fetch: async () => (at.push(Date.now()), [target("compiling")]),
      onTargets: async () => ({ busy: true }),
      onError: () => {},
    };
    let flip = false;
    const feed = new PollingFeed(
      h,
      { ...cadence, busyMs: 40, jitter: 0.5 },
      () => ((flip = !flip) ? 0 : 1),
    );
    await run(feed, 260);
    const gaps = at.slice(1).map((t, i) => t - at[i]);
    expect(Math.min(...gaps)).toBeLessThan(35); // 40 × 0.5
    expect(Math.max(...gaps)).toBeGreaterThan(50); // 40 × 1.5
  });
});
