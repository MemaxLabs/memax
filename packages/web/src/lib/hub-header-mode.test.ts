import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyStoredHubHeaderMode,
  getStoredHubHeaderMode,
  resolveHubHeaderMode,
  setStoredHubHeaderMode,
} from "./hub-header-mode";

describe("hub header mode local preference", () => {
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

  it("stores and reads a valid local mode", () => {
    setStoredHubHeaderMode("none");
    expect(getStoredHubHeaderMode()).toBe("none");
  });

  it("ignores invalid stored values", () => {
    window.localStorage.setItem("memax_ui_hub_header_aurora_mode", "bad");
    expect(getStoredHubHeaderMode()).toBeUndefined();
  });

  it("applies a stored mode over server settings", () => {
    setStoredHubHeaderMode("time");
    expect(
      applyStoredHubHeaderMode({
        theme: "system",
        dreams_enabled: true,
        dreams_merge_enabled: true,
        dreams_archive_enabled: true,
        dreams_excluded_kinds: [],
        dreams_similarity_threshold: 0.85,
        dreams_staleness_days: 60,
        notifications_enabled: true,
        hub_header_aurora_mode: "signature",
      }),
    ).toMatchObject({
      hub_header_aurora_mode: "time",
    });
  });
});

describe("resolveHubHeaderMode", () => {
  it("uses user settings for personal hubs", () => {
    expect(
      resolveHubHeaderMode(
        { hub_type: "personal", header_aurora_mode: "none" },
        { hub_header_aurora_mode: "time" },
      ),
    ).toBe("time");
  });

  it("ignores the per-hub override on personal hubs", () => {
    // personal hubs read from user settings; a stray value on the hub row
    // should not leak through.
    expect(
      resolveHubHeaderMode(
        { hub_type: "personal", header_aurora_mode: "none" },
        { hub_header_aurora_mode: "signature" },
      ),
    ).toBe("signature");
  });

  it("uses the per-hub override for team hubs", () => {
    expect(
      resolveHubHeaderMode(
        { hub_type: "team", header_aurora_mode: "time" },
        { hub_header_aurora_mode: "none" },
      ),
    ).toBe("time");
  });

  it("defaults team hubs with no override to signature", () => {
    expect(
      resolveHubHeaderMode(
        { hub_type: "team", header_aurora_mode: undefined },
        { hub_header_aurora_mode: "none" },
      ),
    ).toBe("signature");
  });

  it("returns the neutral default when no hub is provided", () => {
    // Pre-hydration (activeHub not yet resolved): we do NOT leak the
    // user-level personal setting into a team-hub page that's about to
    // load, so the default is used regardless of the settings value.
    expect(resolveHubHeaderMode(null, null)).toBe("signature");
    expect(resolveHubHeaderMode(null, { hub_header_aurora_mode: "time" })).toBe(
      "signature",
    );
  });
});
