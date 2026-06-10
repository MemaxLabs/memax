// @vitest-environment jsdom

/**
 * report-render-error — the single channel from error boundaries
 * into PostHog. This file locks in the privacy contract:
 *
 *   • Production payload MUST NOT contain message / stack — thrown
 *     errors can embed user content (memory titles, search queries)
 *     and PostHog is not a PII store.
 *   • Production pathname MUST redact UUIDs, invite tokens, API
 *     keys, and long numeric IDs — the route *shape* is useful for
 *     breakdowns, the specific ID is not.
 *   • Development payload keeps message / stack / raw pathname so
 *     you can click through from an event to the failing page.
 *   • Any failure in the reporter itself must be swallowed so the
 *     fallback UI still renders.
 *
 * If you loosen any assertion here, read
 * `packages/web/src/lib/report-render-error.ts` first — the
 * comments there spell out why each field is redacted.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The tested module, plus mocks for its transitive deps. We mock
// posthog.initPostHog as a no-op and trackEvent as a spy so we can
// inspect the payload shape without booting PostHog.
const trackEventMock = vi.fn();
vi.mock("./posthog", () => ({
  initPostHog: () => {},
  trackEvent: (event: string, props?: Record<string, unknown>) =>
    trackEventMock(event, props),
}));

import { reportRenderError, sanitizePathname } from "./report-render-error";

beforeEach(() => {
  trackEventMock.mockReset();
});

afterEach(() => {
  // Restore NODE_ENV between tests — Vitest's stubEnv pairs with
  // unstubAllEnvs so the production/development flips below don't
  // leak into unrelated test files.
  vi.unstubAllEnvs();
});

describe("sanitizePathname", () => {
  it("redacts UUID segments", () => {
    expect(
      sanitizePathname(
        "/admin/users/7f1f3b3f-8c2f-4a3d-8fb6-0c3d9a4e1b12/detail",
      ),
    ).toBe("/admin/users/:id/detail");
  });

  it("redacts opaque invite tokens", () => {
    // 32-char url-safe nanoid — invite tokens fit this shape.
    expect(sanitizePathname("/invite/Jf8a2KcQpR9vUb3wLmXs0TzYe1Hq6d4N")).toBe(
      "/invite/:token",
    );
  });

  it("redacts long numeric IDs but keeps short ones (version, HTTP codes)", () => {
    expect(sanitizePathname("/api/memories/1234567890")).toBe(
      "/api/memories/:id",
    );
    // Short digits (version strings, HTTP codes) stay readable.
    expect(sanitizePathname("/errors/404")).toBe("/errors/404");
  });

  it("leaves static route segments intact", () => {
    expect(sanitizePathname("/admin/users")).toBe("/admin/users");
    expect(sanitizePathname("/home")).toBe("/home");
    expect(sanitizePathname("/")).toBe("/");
  });

  it("handles empty pathname", () => {
    expect(sanitizePathname("")).toBe("");
  });
});

describe("reportRenderError — production payload", () => {
  beforeEach(() => {
    vi.stubEnv("NODE_ENV", "production");
  });

  it("omits message and stack, and sanitizes pathname", () => {
    const err = new Error("user-content: their memory title here");
    err.stack = "Error: user-content\n  at SomeComponent (page.tsx:42)";

    reportRenderError(err, {
      boundary: "app",
      digest: "abc123",
      pathname: "/memories/7f1f3b3f-8c2f-4a3d-8fb6-0c3d9a4e1b12",
    });

    expect(trackEventMock).toHaveBeenCalledTimes(1);
    const [event, props] = trackEventMock.mock.calls[0];
    expect(event).toBe("web.render_error");
    expect(props).toMatchObject({
      name: "Error",
      digest: "abc123",
      boundary: "app",
      pathname: "/memories/:id",
    });
    // The privacy contract: no raw error content in production.
    expect(props).not.toHaveProperty("message");
    expect(props).not.toHaveProperty("stack");
  });

  it("redacts invite tokens out of /invite/[token]", () => {
    reportRenderError(new Error("boom"), {
      boundary: "root",
      pathname: "/invite/Jf8a2KcQpR9vUb3wLmXs0TzYe1Hq6d4N",
    });
    const [, props] = trackEventMock.mock.calls[0];
    expect(props?.pathname).toBe("/invite/:token");
  });

  it("passes through pathname=undefined cleanly", () => {
    reportRenderError(new Error("boom"), { boundary: "global" });
    const [, props] = trackEventMock.mock.calls[0];
    expect(props?.pathname).toBeUndefined();
  });
});

describe("reportRenderError — development payload", () => {
  beforeEach(() => {
    vi.stubEnv("NODE_ENV", "development");
  });

  it("includes message and stack and keeps the raw pathname", () => {
    const err = new Error("dev error");
    err.stack = "stack trace";

    reportRenderError(err, {
      boundary: "admin",
      pathname: "/admin/users/7f1f3b3f-8c2f-4a3d-8fb6-0c3d9a4e1b12",
    });

    const [, props] = trackEventMock.mock.calls[0];
    expect(props).toMatchObject({
      name: "Error",
      boundary: "admin",
      message: "dev error",
      stack: "stack trace",
      // Raw UUID preserved in dev so you can click through.
      pathname: "/admin/users/7f1f3b3f-8c2f-4a3d-8fb6-0c3d9a4e1b12",
    });
  });
});

describe("reportRenderError — resilience", () => {
  it("never throws when telemetry itself errors", () => {
    trackEventMock.mockImplementationOnce(() => {
      throw new Error("posthog exploded");
    });
    expect(() =>
      reportRenderError(new Error("boom"), { boundary: "app" }),
    ).not.toThrow();
  });

  it("coerces non-Error values into an Error", () => {
    reportRenderError("a string was thrown", { boundary: "app" });
    const [, props] = trackEventMock.mock.calls[0];
    expect(props?.name).toBe("Error");
  });
});
