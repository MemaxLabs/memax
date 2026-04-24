import { describe, expect, it } from "vitest";
import { MemaxError, parseRetryAfter } from "./errors.js";

describe("MemaxError status-class getters", () => {
  // Each getter is a named predicate for a common HTTP class. The
  // table covers both the "status alone" path and the "code alone"
  // path where both are defined (rate_limited).
  const cases: Array<{
    name: string;
    err: MemaxError;
    truthy: ReadonlyArray<keyof MemaxError>;
  }> = [
    {
      name: "429 via status",
      err: new MemaxError("slow down", "", 429),
      truthy: ["isRateLimited"],
    },
    {
      name: "429 via code",
      err: new MemaxError(
        "slow down",
        "rate_limited",
        200 /* weird but tolerated */,
      ),
      truthy: ["isRateLimited"],
    },
    {
      name: "403 forbidden",
      err: new MemaxError("nope", "forbidden", 403),
      truthy: ["isForbidden"],
    },
    {
      name: "404 not found",
      err: new MemaxError("gone", "not_found", 404),
      truthy: ["isNotFound"],
    },
    {
      name: "409 conflict",
      err: new MemaxError("race", "conflict", 409),
      truthy: ["isConflict"],
    },
    {
      name: "402 quota",
      err: new MemaxError("limit", "", 402),
      truthy: ["isQuotaExceeded"],
    },
    {
      name: "402 quota via code",
      err: new MemaxError("limit", "quota_exceeded", 402),
      truthy: ["isQuotaExceeded"],
    },
    {
      name: "401 unauthorized",
      err: new MemaxError("auth", "unauthorized", 401),
      truthy: ["isUnauthorized"],
    },
    {
      name: "400 bad request",
      err: new MemaxError("malformed", "invalid_body", 400),
      truthy: ["isBadRequest"],
    },
    {
      name: "500 server error",
      err: new MemaxError("boom", "", 500),
      truthy: ["isServerError"],
    },
    {
      name: "503 server error",
      err: new MemaxError("busy", "", 503),
      truthy: ["isServerError"],
    },
    {
      name: "599 still 5xx",
      err: new MemaxError("edge", "", 599),
      truthy: ["isServerError"],
    },
    {
      name: "network status=0",
      err: new MemaxError("offline", "", 0),
      truthy: ["isNetwork"],
    },
  ];

  for (const { name, err, truthy } of cases) {
    it(`${name} exposes exactly the expected getters`, () => {
      const allGetters: ReadonlyArray<keyof MemaxError> = [
        "isRateLimited",
        "isForbidden",
        "isNotFound",
        "isConflict",
        "isQuotaExceeded",
        "isUnauthorized",
        "isBadRequest",
        "isServerError",
        "isNetwork",
      ];
      const truthySet = new Set(truthy);
      for (const g of allGetters) {
        const actual = !!err[g];
        const expected = truthySet.has(g);
        expect(actual, `${g} for ${name}`).toBe(expected);
      }
    });
  }

  it("retryAfterSeconds round-trips through the constructor", () => {
    const err = new MemaxError("slow", "rate_limited", 429, undefined, 42);
    expect(err.retryAfterSeconds).toBe(42);
    expect(err.isRateLimited).toBe(true);
  });

  it("retryAfterSeconds is undefined when not passed", () => {
    const err = new MemaxError("oops", "", 500);
    expect(err.retryAfterSeconds).toBeUndefined();
  });

  it("preserves details for structured error payloads", () => {
    const err = new MemaxError("x", "y", 400, { field: "email" });
    expect(err.details).toEqual({ field: "email" });
  });

  it("carries a readable Error.message for native Error consumers", () => {
    const err = new MemaxError("something specific", "some_code", 418);
    expect(err.message).toBe("something specific");
    // Integrates with things that check err.name (e.g., sentry, console).
    expect(err.name).toBe("MemaxError");
  });
});

describe("parseRetryAfter", () => {
  it("parses integer seconds", () => {
    expect(parseRetryAfter("120")).toBe(120);
    expect(parseRetryAfter("0")).toBe(0);
    expect(parseRetryAfter("  30 ")).toBe(30);
  });

  it("rejects negatives, floats, and gibberish", () => {
    expect(parseRetryAfter("-5")).toBeUndefined();
    expect(parseRetryAfter("1.5")).toBeUndefined();
    expect(parseRetryAfter("abc not a date")).toBeUndefined();
    expect(parseRetryAfter("")).toBeUndefined();
    expect(parseRetryAfter(null)).toBeUndefined();
    expect(parseRetryAfter(undefined)).toBeUndefined();
  });

  it("parses HTTP-date form into whole seconds, rounding up", () => {
    // Anchor `now` 400ms BEFORE the header-represented time.
    // toUTCString() has second resolution (no millis) so the encoded
    // instant is an exact second; the delta-from-now is 400ms →
    // ceil(0.4) = 1.
    const headerInstant = new Date("2026-04-21T00:00:30Z");
    const nearNow = new Date(headerInstant.getTime() - 400);
    expect(parseRetryAfter(headerInstant.toUTCString(), nearNow)).toBe(1);
    // Exact-second delta rounds cleanly.
    const tenSecondsEarlier = new Date(headerInstant.getTime() - 10_000);
    expect(
      parseRetryAfter(headerInstant.toUTCString(), tenSecondsEarlier),
    ).toBe(10);
  });

  it("clamps past HTTP-dates to 0 rather than returning negatives", () => {
    const now = new Date("2026-04-21T00:00:00Z");
    const past = new Date(now.getTime() - 5000).toUTCString();
    expect(parseRetryAfter(past, now)).toBe(0);
  });

  it("returns undefined on unparseable date strings", () => {
    expect(parseRetryAfter("not a date, not a number")).toBeUndefined();
  });
});
