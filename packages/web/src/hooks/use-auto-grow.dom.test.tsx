// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useRef, type RefObject } from "react";
import { useAutoGrow } from "./use-auto-grow";

/**
 * Controllable ResizeObserver mock. The real RO would fire its
 * callback when the textarea's box changes; jsdom doesn't compute
 * layout, so the test owns when the observer "sees" a width change
 * and calls `triggerLast()` to fire the most recently registered
 * callback. This is what lets us assert that the observer-driven
 * width-change path never feeds back into expansion.
 */
class ControllableResizeObserver {
  static lastCallback: ResizeObserverCallback | null = null;
  callback: ResizeObserverCallback;
  constructor(cb: ResizeObserverCallback) {
    this.callback = cb;
    ControllableResizeObserver.lastCallback = cb;
  }
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {
    if (ControllableResizeObserver.lastCallback === this.callback) {
      ControllableResizeObserver.lastCallback = null;
    }
  }
  static triggerLast(): void {
    this.lastCallback?.([], {} as ResizeObserver);
  }
}
(
  globalThis as unknown as { ResizeObserver: typeof ControllableResizeObserver }
).ResizeObserver = ControllableResizeObserver;

interface Harness {
  onExpand: ReturnType<typeof vi.fn>;
  setWidth(next: number): void;
  type(nextValue: string, isExpanded: boolean): void;
  fireResizeObserver(): void;
  cleanup(): void;
}

function renderAutoGrow(initial: {
  value: string;
  width: number;
  scrollHeightFor: (width: number, value: string) => number;
}): Harness {
  const textarea = document.createElement("textarea");
  document.body.appendChild(textarea);

  let currentWidth = initial.width;
  let currentValue = initial.value;
  Object.defineProperty(textarea, "clientWidth", {
    configurable: true,
    get: () => currentWidth,
  });
  Object.defineProperty(textarea, "scrollHeight", {
    configurable: true,
    get: () => initial.scrollHeightFor(currentWidth, currentValue),
  });

  const onExpand = vi.fn();

  const { rerender, unmount } = renderHook(
    ({ value, isExpanded }: { value: string; isExpanded: boolean }) => {
      const ref = useRef<HTMLTextAreaElement | null>(textarea);
      useAutoGrow(
        ref as RefObject<HTMLTextAreaElement | null>,
        value,
        200,
        isExpanded,
        onExpand,
      );
    },
    { initialProps: { value: initial.value, isExpanded: false } },
  );

  return {
    onExpand,
    setWidth(next: number) {
      currentWidth = next;
    },
    type(nextValue: string, isExpanded: boolean) {
      currentValue = nextValue;
      rerender({ value: nextValue, isExpanded });
    },
    fireResizeObserver() {
      ControllableResizeObserver.triggerLast();
    },
    cleanup() {
      unmount();
      textarea.remove();
    },
  };
}

describe("useAutoGrow — wrap-edge feedback loop", () => {
  it("fires onExpand once at the wrap boundary, not on every keystroke", () => {
    // Simulate: narrow compact width wraps long values (scrollHeight 48);
    // wide expanded width fits them on one line (scrollHeight 24).
    const NARROW = 200;
    const WIDE = 400;
    const scrollHeightFor = (width: number, v: string): number => {
      if (v.length === 0) return 24;
      return width === NARROW && v.length > 30 ? 48 : 24;
    };
    const harness = renderAutoGrow({
      value: "",
      width: NARROW,
      scrollHeightFor,
    });

    harness.type("hello world", false);
    expect(harness.onExpand).not.toHaveBeenCalled();

    harness.type("a".repeat(60), false);
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    // Consumer flips isBarExpanded → grid widens. Subsequent value-change
    // re-measures see WIDE width (scrollHeight back to 24) — must NOT
    // trigger another onExpand.
    harness.setWidth(WIDE);
    harness.type("a".repeat(61), true);
    harness.type("a".repeat(62), true);
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    harness.cleanup();
  });

  it("ResizeObserver width changes never call onExpand", () => {
    // The load-bearing fix: a width change caused by our own expansion
    // would otherwise re-decide expansion and flicker. The observer is
    // height-only.
    const harness = renderAutoGrow({
      value: "a".repeat(60),
      width: 200,
      scrollHeightFor: (w, v) => (w === 200 && v.length > 30 ? 48 : 24),
    });

    harness.type("a".repeat(60), false);
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    // Consumer expands. Width changes from 200 → 400. Fire the observer.
    // Even though scrollHeight drops to 24 at the new width, no onExpand
    // call should follow — observer is height-only.
    harness.setWidth(400);
    harness.fireResizeObserver();
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    // Width swings back (hypothetical layout change). Same deal.
    harness.setWidth(200);
    harness.fireResizeObserver();
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    harness.cleanup();
  });

  it("never calls onExpand with false (collapse is consumer-owned)", () => {
    const harness = renderAutoGrow({
      value: "x".repeat(60),
      width: 200,
      scrollHeightFor: (_w, v) => (v.length > 30 ? 48 : 24),
    });

    harness.type("x".repeat(60), false);
    expect(harness.onExpand).toHaveBeenCalledTimes(1);

    harness.type("", true);
    expect(harness.onExpand).toHaveBeenCalledTimes(1);
    expect(harness.onExpand).not.toHaveBeenCalledWith(false);

    harness.cleanup();
  });
});
