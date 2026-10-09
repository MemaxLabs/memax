// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import {
  useBottomChromeOccupied,
  useOccupyBottomChrome,
} from "./bottom-chrome-store";

describe("bottom-chrome-store", () => {
  it("reports occupied while a composer is registered and clears on unmount", () => {
    const observer = renderHook(() => useBottomChromeOccupied());
    expect(observer.result.current).toBe(false);

    const composer = renderHook(
      ({ active }: { active: boolean }) => useOccupyBottomChrome(active),
      { initialProps: { active: true } },
    );
    expect(observer.result.current).toBe(true);

    // Flipping active off releases without unmounting.
    act(() => composer.rerender({ active: false }));
    expect(observer.result.current).toBe(false);

    act(() => composer.rerender({ active: true }));
    expect(observer.result.current).toBe(true);

    composer.unmount();
    expect(observer.result.current).toBe(false);
  });

  it("stays occupied until the last registrant leaves", () => {
    const observer = renderHook(() => useBottomChromeOccupied());
    const a = renderHook(() => useOccupyBottomChrome(true));
    const b = renderHook(() => useOccupyBottomChrome(true));
    expect(observer.result.current).toBe(true);
    a.unmount();
    expect(observer.result.current).toBe(true);
    b.unmount();
    expect(observer.result.current).toBe(false);
  });
});
