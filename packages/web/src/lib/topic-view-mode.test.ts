import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getStoredTopicViewMode,
  getTopicViewModeStorageKey,
  resolveTopicViewMode,
  resolveTopicViewModeDefault,
  setStoredTopicViewMode,
} from "@/lib/topic-view-mode";

describe("topic view mode", () => {
  const localStorageMock = (() => {
    const store = new Map<string, string>();
    return {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => {
        store.set(key, value);
      },
      removeItem: (key: string) => {
        store.delete(key);
      },
      clear: () => {
        store.clear();
      },
    };
  })();

  beforeEach(() => {
    vi.stubGlobal("window", { localStorage: localStorageMock });
    localStorageMock.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("resolves mode A/B/C from direct child count", () => {
    expect(resolveTopicViewMode(12)).toBe("a");
    expect(resolveTopicViewMode(40)).toBe("b");
    expect(resolveTopicViewMode(120)).toBe("c");
  });

  it("defaults to compact mode on mobile", () => {
    expect(resolveTopicViewModeDefault(8, true)).toBe("c");
    expect(resolveTopicViewModeDefault(60, true)).toBe("c");
  });

  it("uses the desktop scale rules when not mobile", () => {
    expect(resolveTopicViewModeDefault(8, false)).toBe("a");
    expect(resolveTopicViewModeDefault(60, false)).toBe("b");
    expect(resolveTopicViewModeDefault(120, false)).toBe("c");
  });

  it("stores and reads a per-hub, per-viewport override", () => {
    setStoredTopicViewMode("hub_1", false, "b");
    expect(getStoredTopicViewMode("hub_1", false)).toBe("b");
    expect(
      window.localStorage.getItem(getTopicViewModeStorageKey("hub_1", false)),
    ).toBe("b");
  });

  it("ignores invalid stored values", () => {
    window.localStorage.setItem(
      getTopicViewModeStorageKey("hub_1", false),
      "bad",
    );
    expect(getStoredTopicViewMode("hub_1", false)).toBeUndefined();
  });

  it("does not leak desktop preference onto mobile", () => {
    // User picks mode "a" (grid) on desktop.
    setStoredTopicViewMode("hub_1", false, "a");
    // Mobile read on the same hub must not see it — must fall through to
    // the mobile default ("c") at the call site.
    expect(getStoredTopicViewMode("hub_1", true)).toBeUndefined();
    // And vice versa.
    setStoredTopicViewMode("hub_1", true, "c");
    expect(getStoredTopicViewMode("hub_1", false)).toBe("a");
  });
});
