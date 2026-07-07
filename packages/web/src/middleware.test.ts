import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";

function makeRequest(
  pathname: string,
  opts: { sessionPresence?: boolean } = {},
): NextRequest {
  const url = `https://memax.app${pathname}`;
  return new NextRequest(url, {
    headers: opts.sessionPresence
      ? { cookie: "memax_session_presence=1" }
      : undefined,
  });
}

/**
 * Plan 24 phase 4b removed the v1/v2 cookie dispatch. Middleware now
 * redirects legacy /memories paths unconditionally; cookie-gated cases
 * dropped from the test (`when cookie is v1 or unset` was the v1
 * fallback path that no longer exists).
 */
describe("middleware legacy path normalization", () => {
  it("redirects /memories to /h/personal/memories", () => {
    const res = middleware(makeRequest("/memories"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });

  it("redirects /memories/ (trailing slash) the same way", () => {
    const res = middleware(makeRequest("/memories/"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });

  it("redirects /memories/topics/<id> to /h/personal/topics/<id>", () => {
    const res = middleware(makeRequest("/memories/topics/welcome"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/topics/welcome",
    );
  });

  it("does NOT redirect /memories/<id> (hub identity is in memory data, not URL)", () => {
    // Redirecting to /h/personal/<id> would force V2HubRoute to switch
    // the active hub to personal even when the memory belongs to a
    // team hub — leaking wrong-hub chrome around right-content. In-app
    // navigation emits hub-aware paths directly; only old shared/email
    // links land here, and rendering the v1 path under v2 chrome is
    // correct.
    const res = middleware(makeRequest("/memories/abc-123"));
    expect(res.status).toBe(200);
  });

  it("does NOT redirect /memories/topics (overview, not detail)", () => {
    const res = middleware(makeRequest("/memories/topics"));
    // /memories/topics is the topics overview (no trailing id) — not a
    // memory detail (because "topics" is reserved). No v2 equivalent
    // beyond /h/personal/memories — so the middleware passes through;
    // the route segment handles it.
    expect(res.status).toBe(200);
  });

  it("does NOT redirect /memories/new (reserved segment)", () => {
    const res = middleware(makeRequest("/memories/new"));
    expect(res.status).toBe(200);
  });
});

describe("middleware silent session restore", () => {
  it.each(["/", "/login", "/register"])(
    "redirects %s to /home when the session-presence cookie is set",
    (path) => {
      const res = middleware(makeRequest(path, { sessionPresence: true }));
      expect(res.status).toBe(307);
      expect(res.headers.get("location")).toBe("https://memax.app/home");
    },
  );

  it.each(["/", "/login", "/register"])(
    "leaves %s alone without the cookie",
    (path) => {
      const res = middleware(makeRequest(path));
      expect(res.status).toBe(200);
    },
  );

  it("does not let the cookie affect legacy path normalization", () => {
    const res = middleware(makeRequest("/memories", { sessionPresence: true }));
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });
});
