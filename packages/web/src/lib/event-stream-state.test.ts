// @vitest-environment jsdom

import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  getStreamState,
  markClosed,
  markConnected,
  markConnecting,
  markError,
  markEvent,
  markReset,
  subscribeStreamState,
  __resetStreamStateForTests,
} from "./event-stream-state";

describe("event-stream-state", () => {
  beforeEach(() => {
    __resetStreamStateForTests();
  });

  it("starts in idle with all timestamps null", () => {
    const s = getStreamState();
    expect(s.connection).toBe("idle");
    expect(s.lastReadyAt).toBeNull();
    expect(s.lastEventAt).toBeNull();
    expect(s.lastErrorAt).toBeNull();
    expect(s.lastErrorMessage).toBeNull();
    expect(s.watchingHubCount).toBe(0);
  });

  it("markConnecting flips idle → connecting", () => {
    markConnecting();
    expect(getStreamState().connection).toBe("connecting");
  });

  it("markConnected populates readyAt + eventAt + hub count and clears prior error", () => {
    markError("network down");
    expect(getStreamState().lastErrorMessage).toBe("network down");

    markConnected(3);
    const s = getStreamState();
    expect(s.connection).toBe("connected");
    expect(s.lastReadyAt).not.toBeNull();
    expect(s.lastEventAt).toBe(s.lastReadyAt);
    expect(s.watchingHubCount).toBe(3);
    // Reconnect clears the stale error so the debugger header does
    // not misleadingly show a past error alongside "Connected".
    expect(s.lastErrorAt).toBeNull();
    expect(s.lastErrorMessage).toBeNull();
  });

  it("markConnecting does not clobber connected (resubscribe race)", () => {
    markConnected(1);
    markConnecting();
    expect(getStreamState().connection).toBe("connected");
  });

  it("markEvent advances lastEventAt without changing connection", () => {
    markConnected(2);
    const before = getStreamState().lastEventAt!;
    vi.useFakeTimers();
    vi.advanceTimersByTime(5_000);
    markEvent();
    const after = getStreamState().lastEventAt!;
    expect(after).toBeGreaterThan(before);
    expect(getStreamState().connection).toBe("connected");
    vi.useRealTimers();
  });

  it("markError flips to reconnecting with message + timestamp", () => {
    markConnected(1);
    markError("ECONNRESET");
    const s = getStreamState();
    expect(s.connection).toBe("reconnecting");
    expect(s.lastErrorMessage).toBe("ECONNRESET");
    expect(s.lastErrorAt).not.toBeNull();
  });

  it("markClosed flips connected → reconnecting (awaiting retry)", () => {
    markConnected(1);
    markClosed();
    expect(getStreamState().connection).toBe("reconnecting");
  });

  it("markClosed is a no-op if already reconnecting (debounces flicker)", () => {
    markError("initial error");
    expect(getStreamState().connection).toBe("reconnecting");
    markClosed();
    expect(getStreamState().connection).toBe("reconnecting");
  });

  it("notifies subscribers on every state change", () => {
    const listener = vi.fn();
    const unsub = subscribeStreamState(listener);
    markConnecting(); // idle → connecting (emit)
    markConnected(2); // connecting → connected (emit)
    markEvent(); // timestamp advance (emit)
    markError("boom"); // connected → reconnecting (emit)
    // markClosed is intentionally a no-op once already reconnecting
    // (flicker debounce), so it does NOT emit.
    markClosed();
    expect(listener).toHaveBeenCalledTimes(4);
    unsub();
    markConnecting();
    expect(listener).toHaveBeenCalledTimes(4);
  });

  it("reconnect cycle: connected → reconnecting (error) → connected (clears error)", () => {
    markConnected(2);
    markError("transient");
    expect(getStreamState().lastErrorMessage).toBe("transient");
    markConnected(2);
    const s = getStreamState();
    expect(s.connection).toBe("connected");
    expect(s.lastErrorMessage).toBeNull();
    expect(s.lastErrorAt).toBeNull();
  });

  it("markReset returns every field to initial (teardown path)", () => {
    markConnected(5);
    markEvent();
    markError("stale error");
    const before = getStreamState();
    expect(before.connection).toBe("reconnecting");
    expect(before.lastReadyAt).not.toBeNull();
    expect(before.lastEventAt).not.toBeNull();
    expect(before.lastErrorMessage).toBe("stale error");
    expect(before.watchingHubCount).toBe(5);

    markReset();

    const after = getStreamState();
    expect(after.connection).toBe("idle");
    expect(after.lastReadyAt).toBeNull();
    expect(after.lastEventAt).toBeNull();
    expect(after.lastErrorAt).toBeNull();
    expect(after.lastErrorMessage).toBeNull();
    expect(after.watchingHubCount).toBe(0);
  });

  it("markReset notifies subscribers", () => {
    markConnected(3);
    const listener = vi.fn();
    const unsub = subscribeStreamState(listener);
    markReset();
    expect(listener).toHaveBeenCalledTimes(1);
    unsub();
  });
});
