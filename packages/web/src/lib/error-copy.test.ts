/**
 * Tests for the mutation error classifier. Pins two things:
 *
 *   1. Each HTTP-status class maps to the right i18n template
 *      (and the template interpolates `{action}` / `{seconds}` right).
 *   2. The "unknown error" + "401 unauthorized" paths return undefined
 *      so callers can fall through to their own copy / re-auth flow.
 *
 * English is the source-of-truth bundle — we don't exercise zh here
 * because the Translations type guarantees key parity, and the
 * classifier just interpolates into whichever snapshot is active.
 */
import { MemaxError } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { classifyMutationError } from "./error-copy";
import { setTranslationsSnapshot } from "./translations-snapshot";

// Ensure every test runs against a known translations bundle and
// resets navigator.onLine to the JSDOM default (true) so one test's
// offline branch doesn't bleed into another.
beforeEach(() => {
  setTranslationsSnapshot(en);
  Object.defineProperty(navigator, "onLine", {
    value: true,
    configurable: true,
    writable: true,
  });
});

afterEach(() => {
  setTranslationsSnapshot(en);
  Object.defineProperty(navigator, "onLine", {
    value: true,
    configurable: true,
    writable: true,
  });
});

const act = { action: en.errors.action.moveMemory };

describe("classifyMutationError — status classes", () => {
  it("rate-limited without Retry-After", () => {
    const err = new MemaxError("slow down", "rate_limited", 429);
    const got = classifyMutationError(err, act);
    expect(got).toBeDefined();
    expect(got?.message).toContain("move that memory");
    expect(got?.message.toLowerCase()).toContain("wait a moment");
    expect(got?.autoDismissMs).toBe(8000); // longer — user needs time to read
  });

  it("rate-limited WITH Retry-After uses the seconds template", () => {
    const err = new MemaxError("slow down", "rate_limited", 429, undefined, 42);
    const got = classifyMutationError(err, act);
    expect(got?.message).toContain("42s");
    expect(got?.message).toContain("move that memory");
    expect(got?.autoDismissMs).toBe(8000);
  });

  it("rate-limited with Retry-After=0 falls back to the generic template", () => {
    const err = new MemaxError("slow down", "rate_limited", 429, undefined, 0);
    const got = classifyMutationError(err, act);
    // 0 is a useless countdown — use the "wait a moment" template.
    expect(got?.message.toLowerCase()).toContain("wait a moment");
    expect(got?.message).not.toContain("0s");
  });

  it("forbidden (403)", () => {
    const err = new MemaxError("nope", "forbidden", 403);
    const got = classifyMutationError(err, act);
    expect(got?.message).toContain("don't have permission");
    expect(got?.message).toContain("move that memory");
  });

  it("not found (404)", () => {
    const err = new MemaxError("gone", "not_found", 404);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("gone");
    // `missing` template doesn't interpolate {action} — that's by
    // design since "that's gone" reads better without a verb phrase.
  });

  it("conflict (409)", () => {
    const err = new MemaxError("race", "conflict", 409);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("someone else changed");
  });

  it("quota exceeded (402) returns upgrade-CTA copy, not retry copy", () => {
    const err = new MemaxError("plan limit", "quota_exceeded", 402);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("plan");
    expect(got?.message.toLowerCase()).not.toContain("wait a moment");
  });

  it("bad request (400)", () => {
    const err = new MemaxError("malformed", "invalid_body", 400);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("couldn't be processed");
  });

  it("server error (500/503)", () => {
    for (const status of [500, 503, 599]) {
      const err = new MemaxError("boom", "", status);
      const got = classifyMutationError(err, act);
      expect(got?.message.toLowerCase()).toContain("hiccup");
    }
  });

  it("network (status 0)", () => {
    const err = new MemaxError("offline", "network_error", 0);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("offline");
  });
});

describe("classifyMutationError — fallthrough cases", () => {
  it("unauthorized (401) returns undefined so re-auth flow handles it", () => {
    const err = new MemaxError("expired", "unauthorized", 401);
    expect(classifyMutationError(err, act)).toBeUndefined();
  });

  it("random Error (not MemaxError) returns undefined", () => {
    expect(classifyMutationError(new TypeError("boom"), act)).toBeUndefined();
  });

  it("non-Error throwable returns undefined", () => {
    expect(classifyMutationError("string error", act)).toBeUndefined();
    expect(classifyMutationError({ oh: "no" }, act)).toBeUndefined();
    expect(classifyMutationError(null, act)).toBeUndefined();
  });

  it("unclassified MemaxError status returns undefined", () => {
    // 418 is real but the classifier shouldn't invent copy for it.
    const err = new MemaxError("teapot", "", 418);
    expect(classifyMutationError(err, act)).toBeUndefined();
  });
});

describe("classifyMutationError — offline override", () => {
  it("navigator.onLine === false wins over any MemaxError shape", () => {
    Object.defineProperty(navigator, "onLine", {
      value: false,
      configurable: true,
      writable: true,
    });
    // Even if the server "responded" with a 429, if the browser
    // thinks it's offline, show the offline message — the 429 was
    // probably cached or surfaced by a service worker.
    const err = new MemaxError("slow", "rate_limited", 429, undefined, 10);
    const got = classifyMutationError(err, act);
    expect(got?.message.toLowerCase()).toContain("offline");
  });

  it("offline applies even when the error isn't a MemaxError", () => {
    Object.defineProperty(navigator, "onLine", {
      value: false,
      configurable: true,
      writable: true,
    });
    const got = classifyMutationError(new TypeError("fetch failed"), act);
    expect(got?.message.toLowerCase()).toContain("offline");
  });
});

describe("classifyMutationError — locale sensitivity", () => {
  it("uses the active translation snapshot", () => {
    setTranslationsSnapshot(zh);
    const err = new MemaxError("slow", "rate_limited", 429);
    const got = classifyMutationError(err, {
      action: zh.errors.action.moveMemory,
    });
    // Chinese template: "操作有点频繁 — 稍等一下再试。"
    expect(got?.message).toContain("操作");
    expect(got?.message).toContain("稍等");
  });

  it("missing action key leaves {action} placeholder visible (not silently blank)", () => {
    const err = new MemaxError("nope", "forbidden", 403);
    const got = classifyMutationError(err, {}); // no action
    expect(got?.message).toContain("{action}");
  });
});
