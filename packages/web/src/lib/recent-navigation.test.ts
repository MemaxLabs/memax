import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  clearLiveSurfaceTransition,
  consumeRecentExpandOnArrival,
  consumeSurfaceTransition,
  requestNamedHubSwitchTransition,
  requestRecentExpandOnArrival,
  requestSurfaceTransitionForNavigation,
} from "@/lib/recent-navigation";

class MemorySessionStorage implements Storage {
  private store = new Map<string, string>();

  get length() {
    return this.store.size;
  }

  clear() {
    this.store.clear();
  }

  getItem(key: string) {
    return this.store.get(key) ?? null;
  }

  key(index: number) {
    return Array.from(this.store.keys())[index] ?? null;
  }

  removeItem(key: string) {
    this.store.delete(key);
  }

  setItem(key: string, value: string) {
    this.store.set(key, value);
  }
}

describe("recent navigation", () => {
  const originalSessionStorage = globalThis.sessionStorage;

  beforeEach(() => {
    Object.defineProperty(globalThis, "sessionStorage", {
      value: new MemorySessionStorage(),
      configurable: true,
      writable: true,
    });
  });

  afterEach(() => {
    clearLiveSurfaceTransition();
    Object.defineProperty(globalThis, "sessionStorage", {
      value: originalSessionStorage,
      configurable: true,
      writable: true,
    });
  });

  it("expands recent only once for the matching hub", () => {
    requestRecentExpandOnArrival("hub-1");

    expect(consumeRecentExpandOnArrival("hub-2")).toBe(false);
    expect(consumeRecentExpandOnArrival("hub-1")).toBe(true);
    expect(consumeRecentExpandOnArrival("hub-1")).toBe(false);
  });

  it("persists named hub switch transitions through session storage", () => {
    requestNamedHubSwitchTransition({
      hubName: "Platform",
      hubBadgeLabel: "P",
      hubAccent: "emerald",
      hubKind: "team",
    });

    expect(consumeSurfaceTransition()).toEqual({
      kind: "hub-switch",
      hubName: "Platform",
      hubBadgeLabel: "P",
      hubAccent: "emerald",
      hubKind: "team",
    });
    expect(consumeSurfaceTransition()).toBeNull();
  });

  it("requests a brain-to-memory transition only for surface changes", () => {
    requestSurfaceTransitionForNavigation("/home", "/memories");
    expect(consumeSurfaceTransition()).toEqual({ kind: "brain-to-memory" });

    requestSurfaceTransitionForNavigation("/memories", "/memories/123");
    expect(consumeSurfaceTransition()).toBeNull();
  });

  it("requests a memory-to-brain transition when returning home", () => {
    requestSurfaceTransitionForNavigation("/memories/topics/topic-1", "/home");
    expect(consumeSurfaceTransition()).toEqual({ kind: "memory-to-brain" });
  });
});
