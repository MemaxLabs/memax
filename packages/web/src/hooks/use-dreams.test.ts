import { describe, it, expect } from "vitest";
import { MemaxError } from "memax-sdk";
import { mapDreamTriggerError } from "./use-dreams";

/**
 * Locks in the business-code mapping used by useDreamTrigger. The
 * helper returns undefined for anything it doesn't recognize so the
 * query-client's precedence chain can fall through to the status
 * classifier — that's how rate-limit / offline / 5xx copy lands
 * with zero per-hook wiring. Covering the undefined branch here
 * guards against someone reintroducing the old "always return
 * fallback" shape that would mask the classifier.
 */
describe("mapDreamTriggerError", () => {
  const copy = {
    hubFrozen: "hub-frozen-copy",
    forbidden: "forbidden-copy",
  };

  it("maps hub_frozen MemaxError to the frozen copy", () => {
    const err = new MemaxError("frozen", "hub_frozen", 409);
    expect(mapDreamTriggerError(err, copy)).toBe("hub-frozen-copy");
  });

  it("maps forbidden MemaxError to the permission copy", () => {
    const err = new MemaxError("forbidden", "forbidden", 403);
    expect(mapDreamTriggerError(err, copy)).toBe("forbidden-copy");
  });

  it("returns undefined for unknown MemaxError codes (classifier takes over)", () => {
    const err = new MemaxError("something", "unexpected_code", 500);
    expect(mapDreamTriggerError(err, copy)).toBeUndefined();
  });

  it("returns undefined for plain Error instances (classifier takes over)", () => {
    const err = new Error("network blew up");
    expect(mapDreamTriggerError(err, copy)).toBeUndefined();
  });

  it("returns undefined for quota_exceeded (402) — classifier handles plan limits", () => {
    // Dream trigger doesn't need a bespoke quota message; the
    // classifier's quotaExceeded copy with the "trigger a dream"
    // action reads naturally. Keeping this test pinned so a future
    // reintroduction of per-hook quota copy is a deliberate choice.
    const err = new MemaxError("limit", "quota_exceeded", 402);
    expect(mapDreamTriggerError(err, copy)).toBeUndefined();
  });
});
