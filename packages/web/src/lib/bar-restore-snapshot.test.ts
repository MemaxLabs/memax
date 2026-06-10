// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearBarRestoreSnapshot,
  consumeBarRestoreSnapshot,
  saveBarRestoreSnapshot,
  type BarRestoreSnapshot,
} from "@/lib/bar-restore-snapshot";

function makeSnapshot(
  overrides: Partial<BarRestoreSnapshot> = {},
): BarRestoreSnapshot {
  return {
    pathname: "/home",
    value: "auth",
    recallQuery: "auth",
    phase: "recall-result",
    activeNavigationIndex: 2,
    routeTopicDismissed: false,
    isBarExpanded: true,
    mobileComposeState: "docked",
    savedAt: Date.now(),
    ...overrides,
  };
}

describe("bar-restore-snapshot", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });
  afterEach(() => {
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it("saves and consumes on matching pathname (one-shot)", () => {
    saveBarRestoreSnapshot(makeSnapshot());
    const first = consumeBarRestoreSnapshot("/home");
    expect(first?.value).toBe("auth");
    // Consume clears — a second call returns null.
    expect(consumeBarRestoreSnapshot("/home")).toBeNull();
  });

  it("returns null if pathname differs (forward-nav); snapshot stays for return", () => {
    saveBarRestoreSnapshot(makeSnapshot());
    expect(consumeBarRestoreSnapshot("/memories/abc")).toBeNull();
    // Back to origin still hits.
    expect(consumeBarRestoreSnapshot("/home")?.value).toBe("auth");
  });

  it("discards stale snapshots past TTL", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-21T10:00:00Z"));
    saveBarRestoreSnapshot(makeSnapshot());
    vi.setSystemTime(new Date("2026-04-21T10:20:00Z")); // +20 min > 10min TTL
    expect(consumeBarRestoreSnapshot("/home")).toBeNull();
  });

  it("gracefully handles malformed storage", () => {
    sessionStorage.setItem("memax-bar-restore", "not-json");
    expect(consumeBarRestoreSnapshot("/home")).toBeNull();
    // Removes the bad payload.
    expect(sessionStorage.getItem("memax-bar-restore")).toBeNull();
  });

  it("clearBarRestoreSnapshot wipes the entry", () => {
    saveBarRestoreSnapshot(makeSnapshot());
    clearBarRestoreSnapshot();
    expect(consumeBarRestoreSnapshot("/home")).toBeNull();
  });
});
