// @vitest-environment jsdom

import {
  describe,
  it,
  expect,
  vi,
  beforeEach,
  afterEach,
  type MockInstance,
} from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  markConnected,
  markError,
  markReset,
  __resetStreamStateForTests,
} from "@/lib/event-stream-state";
import {
  useStreamReconcile,
  RECONCILE_COOLDOWN_MS,
  RECONCILE_WHITELIST,
} from "./use-stream-reconcile";

// ── Helpers ─────────────────────────────────────────────────

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function fireVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}

// ── Tests ───────────────────────────────────────────────────

describe("useStreamReconcile", () => {
  let qc: QueryClient;
  // Loose MockInstance typing — vi.spyOn is overloaded and the narrow
  // return type of `spyOn(qc, "invalidateQueries")` is awkward to
  // spell across vitest versions; MockInstance<never> captures the
  // spy surface we actually use (mockClear / mock.calls).
  let invalidateSpy: MockInstance;

  beforeEach(() => {
    __resetStreamStateForTests();
    qc = new QueryClient();
    invalidateSpy = vi.spyOn(qc, "invalidateQueries");
  });

  afterEach(() => {
    invalidateSpy.mockRestore();
    qc.clear();
    vi.useRealTimers();
  });

  it("on mount with no state transition, does not fire reconcile", () => {
    renderHook(() => useStreamReconcile(), { wrapper: makeWrapper(qc) });
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("fires reconcile on reconnect transition (non-connected → connected)", () => {
    // Start in idle. Mount hook. No reconcile yet.
    const { rerender } = renderHook(() => useStreamReconcile(), {
      wrapper: makeWrapper(qc),
    });
    expect(invalidateSpy).not.toHaveBeenCalled();

    // Transition to connected. Triggers reconcile over the whitelist.
    act(() => {
      markConnected(2);
    });
    rerender();

    expect(invalidateSpy).toHaveBeenCalledTimes(RECONCILE_WHITELIST.length);
    for (const queryKey of RECONCILE_WHITELIST) {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey });
    }
  });

  it("does not re-fire reconcile if connection stays connected", () => {
    const { rerender } = renderHook(() => useStreamReconcile(), {
      wrapper: makeWrapper(qc),
    });
    act(() => {
      markConnected(1);
    });
    rerender();
    invalidateSpy.mockClear();

    // Another markConnected (e.g. resubscribe with new hub count)
    // keeps connection === "connected"; no transition → no reconcile.
    act(() => {
      markConnected(2);
    });
    rerender();
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("fires reconcile on visibilitychange → visible", () => {
    renderHook(() => useStreamReconcile(), { wrapper: makeWrapper(qc) });
    invalidateSpy.mockClear();
    act(() => {
      fireVisibility("visible");
    });
    expect(invalidateSpy).toHaveBeenCalledTimes(RECONCILE_WHITELIST.length);
  });

  it("does not fire reconcile on visibilitychange → hidden", () => {
    renderHook(() => useStreamReconcile(), { wrapper: makeWrapper(qc) });
    invalidateSpy.mockClear();
    act(() => {
      fireVisibility("hidden");
    });
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("cooldown debounces duplicate reconciles within the window", () => {
    vi.useFakeTimers();
    renderHook(() => useStreamReconcile(), { wrapper: makeWrapper(qc) });

    // Fire reconnect transition.
    act(() => {
      markConnected(1);
    });
    const firstCallCount = invalidateSpy.mock.calls.length;
    expect(firstCallCount).toBe(RECONCILE_WHITELIST.length);

    // Fire visibility-resume immediately — still inside cooldown window.
    act(() => {
      fireVisibility("visible");
    });
    expect(invalidateSpy.mock.calls.length).toBe(firstCallCount); // no change

    // Advance past cooldown.
    vi.advanceTimersByTime(RECONCILE_COOLDOWN_MS + 100);

    // Now visibility resume should fire again.
    act(() => {
      fireVisibility("visible");
    });
    expect(invalidateSpy.mock.calls.length).toBe(
      firstCallCount + RECONCILE_WHITELIST.length,
    );
  });

  it("reconcile fires again after disconnect → reconnect cycle", () => {
    vi.useFakeTimers();
    const { rerender } = renderHook(() => useStreamReconcile(), {
      wrapper: makeWrapper(qc),
    });

    act(() => {
      markConnected(1);
    });
    rerender();
    const afterFirstReconnect = invalidateSpy.mock.calls.length;

    // Advance past cooldown so the next reconcile is not debounced.
    vi.advanceTimersByTime(RECONCILE_COOLDOWN_MS + 100);

    // Disconnect, then reconnect. Transition from reconnecting →
    // connected is a valid reconnect trigger.
    act(() => {
      markError("boom");
    });
    rerender();
    act(() => {
      markConnected(2);
    });
    rerender();

    expect(invalidateSpy.mock.calls.length).toBe(
      afterFirstReconnect + RECONCILE_WHITELIST.length,
    );
  });

  it("unmount removes the visibilitychange listener", () => {
    const { unmount } = renderHook(() => useStreamReconcile(), {
      wrapper: makeWrapper(qc),
    });
    invalidateSpy.mockClear();
    unmount();
    markReset();

    // After unmount, a visibilitychange event should not trigger
    // reconcile because the listener was removed in the effect
    // cleanup.
    act(() => {
      fireVisibility("visible");
    });
    expect(invalidateSpy).not.toHaveBeenCalled();
  });
});
