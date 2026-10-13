import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { config, proxy } from "./proxy";
import { isV2Path, RESERVED_SPACE_SLUGS, V2_SPACE_PLACES } from "./lib/ui-gate";

function makeRequest(
  pathname: string,
  opts: { sessionPresence?: boolean; ui?: string; extra?: string } = {},
): NextRequest {
  const url = `https://memax.app${pathname}`;
  const cookies = [
    opts.sessionPresence ? "memax_session_presence=1" : null,
    opts.ui ? `memax_ui=${opts.ui}` : null,
    opts.extra ?? null,
  ].filter(Boolean);
  return new NextRequest(url, {
    headers: cookies.length ? { cookie: cookies.join("; ") } : undefined,
  });
}

// The V2 areas every browser may open: the CLI's device sign-in, the
// sign-in on the way to it, the morning email's unsubscribe link and the
// public site (lib/ui-gate.ts V2_OPEN_AREAS).
const OPEN_V2_PATHS = [
  "/signin",
  "/signin/callback",
  "/device",
  "/device?code=WQRT-4821",
  "/unsubscribe?token=abc",
  "/oauth/authorize?request_id=r1&consent_token=t1",
  "/welcome",
  "/pricing",
  "/pricing?utm_source=hn",
  "/security",
  "/docs",
];

// Representative V2 paths, one or more per gated area in lib/ui-gate.ts.
const V2_PATHS = [
  "/setup",
  "/setup/import",
  "/setup/agents",
  "/setup/done",
  "/join/abc123",
  "/settings/plan",
  "/settings/keys",
  "/dev/ledger",
  "/dev/ledger/tokens",
  "/memax-v2/today",
  "/memax-v2/review",
  "/memax-v2/memories/M-0219",
  "/personal/brief/targets/claude-md/drift",
  "/memax-team/decisions/D-214",
  ...V2_SPACE_PLACES.map((place) => `/memax-v2/${place}`),
];

// V1 paths, including ones shaped like V2 paths, that must stay V1.
// (V1's home is the one V1 path a browser with the hint leaves: below.)
const V1_PATHS = [
  "/",
  "/login",
  "/settings",
  "/admin",
  "/admin/users/u-1",
  "/agents",
  "/agents/connect",
  "/memories/today",
  "/h/personal/memories",
  "/h/today",
  "/inbox/review",
  "/dev/kitchen",
  "/invite/tok_123",
  "/memax-v2",
  "/Memax-V2/today",
];

describe("proxy V2 gating", () => {
  it.each(V2_PATHS)(
    "sends a signed-out browser on %s to sign in, coming back after",
    (path) => {
      const res = proxy(makeRequest(path));
      expect(res.status).toBe(307);
      const to = new URL(res.headers.get("location")!);
      if (path.startsWith("/dev/")) {
        // The dev fixtures aren't anyone's page: V1's home.
        expect(to.pathname).toBe("/");
        return;
      }
      expect(to.pathname).toBe("/signin");
      expect(to.searchParams.get("next")).toBe(path);
    },
  );

  it("sends a signed-in browser once to /signin to read its V2 UI flag again", () => {
    // The sign-in page reads the profile (which sets the hint) and goes
    // on to the path, query and all, for a person who sees V2.
    const res = proxy(
      makeRequest("/memax-v2/review?filter=import", { sessionPresence: true }),
    );
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/signin?next=%2Fmemax-v2%2Freview%3Ffilter%3Dimport",
    );
    const marker = res.headers
      .getSetCookie()
      .find((c) => c.startsWith("memax_ui_check="));
    expect(marker).toContain("memax_ui_check=1");
    expect(marker).toContain("Max-Age=60");
    expect(marker).toContain("Path=/");
    expect(marker).toContain("HttpOnly");
    expect(marker?.toLowerCase()).toContain("samesite=lax");
    expect(marker).toContain("Secure");
  });

  it("sends a signed-in browser re-checked a moment ago straight to /home", () => {
    const res = proxy(
      makeRequest("/memax-v2/today", {
        sessionPresence: true,
        extra: "memax_ui_check=1",
      }),
    );
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://memax.app/home");
    expect(res.headers.getSetCookie()).toEqual([]);
  });

  it("never re-checks for the dev fixtures", () => {
    const res = proxy(
      makeRequest("/dev/ledger/tokens", { sessionPresence: true }),
    );
    expect(res.headers.get("location")).toBe("https://memax.app/home");
  });

  it("keeps the V2 query string in next, for after sign-in", () => {
    const res = proxy(makeRequest("/memax-v2/search?q=river"));
    const to = new URL(res.headers.get("location")!);
    expect(to.pathname).toBe("/signin");
    expect(to.searchParams.get("next")).toBe("/memax-v2/search?q=river");
  });

  it.each(V2_PATHS)("lets %s through with memax_ui=v2", (path) => {
    const res = proxy(makeRequest(path, { ui: "v2" }));
    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
  });

  it("ignores any other memax_ui value", () => {
    const res = proxy(makeRequest("/setup/import", { ui: "v1" }));
    expect(res.status).toBe(307);
  });

  it.each(OPEN_V2_PATHS)(
    "opens %s without the opt-in, signed in or not",
    (path) => {
      for (const sessionPresence of [false, true]) {
        for (const ui of [undefined, "v1", "v2"]) {
          const res = proxy(makeRequest(path, { sessionPresence, ui }));
          expect(res.status).toBe(200);
          expect(res.headers.get("location")).toBeNull();
        }
      }
    },
  );

  it.each(OPEN_V2_PATHS)("runs the proxy for %s", (path) => {
    expect(
      unstable_doesMiddlewareMatch({ config, url: path.split("?")[0]! }),
    ).toBe(true);
  });

  it.each(V1_PATHS)("leaves V1 path %s to the V1 rules", (path) => {
    for (const ui of [undefined, "v2"]) {
      const res = proxy(makeRequest(path, { ui }));
      const location = res.headers.get("location");
      // Only the existing V1 redirects may fire; never the V2 gate.
      if (location) {
        expect(location).not.toBe("https://memax.app/");
      } else {
        expect(res.status).toBe(200);
      }
    }
  });

  it("sends every browser on V1's retired consent page to OAuthConsent with its request", () => {
    const query = "?request_id=r1&consent_token=t%2B1";
    for (const sessionPresence of [false, true]) {
      for (const ui of [undefined, "v1", "v2"]) {
        const res = proxy(
          makeRequest(`/oauth/consent${query}`, { sessionPresence, ui }),
        );
        expect(res.status).toBe(307);
        expect(res.headers.get("location")).toBe(
          `https://memax.app/oauth/authorize${query}`,
        );
      }
    }
    expect(
      unstable_doesMiddlewareMatch({ config, url: "/oauth/consent" }),
    ).toBe(true);
  });

  it("keeps the V2 redirect ahead of the session fast path", () => {
    const res = proxy(
      makeRequest("/signin", { sessionPresence: true, ui: "v2" }),
    );
    expect(res.status).toBe(200);
  });
});

describe("proxy V1 home for a person who sees V2", () => {
  // Their V2 UI flag turned on while they were in V1, and a profile read
  // set the hint: the next time they open the app, it's V2. The sign-in
  // page lands a signed-in person by their flag.
  it.each([
    ["/home", false],
    ["/home", true],
    ["/", true],
    ["/login", true],
  ])("sends %s (signed in: %s) to the V2 landing, once", (path, signedIn) => {
    const res = proxy(
      makeRequest(path, { sessionPresence: signedIn, ui: "v2" }),
    );
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://memax.app/signin");
    const marker = res.headers
      .getSetCookie()
      .find((c) => c.startsWith("memax_ui_check="));
    expect(marker).toContain("Max-Age=60");
    expect(marker).toContain("HttpOnly");
  });

  it("opens V1's home when the flag was read a moment ago, never a loop", () => {
    const res = proxy(
      makeRequest("/home", {
        sessionPresence: true,
        ui: "v2",
        extra: "memax_ui_check=1",
      }),
    );
    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
    // The root still takes the session fast path to V1's home.
    const root = proxy(
      makeRequest("/", {
        sessionPresence: true,
        ui: "v2",
        extra: "memax_ui_check=1",
      }),
    );
    expect(root.headers.get("location")).toBe("https://memax.app/home");
  });

  it("leaves V1's home to V1 without the hint", () => {
    for (const ui of [undefined, "v1"]) {
      const res = proxy(makeRequest("/home", { sessionPresence: true, ui }));
      expect(res.status).toBe(200);
      expect(res.headers.get("location")).toBeNull();
    }
  });

  it("keeps the V1 pages V2 doesn't replace open", () => {
    for (const path of ["/settings", "/admin", "/admin/users/u-1", "/agents"]) {
      const res = proxy(makeRequest(path, { sessionPresence: true, ui: "v2" }));
      expect(res.status, path).toBe(200);
    }
    // Hubs, V1's settings and the admin pages skip the proxy entirely.
    for (const path of ["/h/acme/memories", "/settings", "/admin/users"]) {
      expect(unstable_doesMiddlewareMatch({ config, url: path }), path).toBe(
        false,
      );
    }
  });

  it("runs the proxy for /home", () => {
    expect(unstable_doesMiddlewareMatch({ config, url: "/home" })).toBe(true);
  });
});

describe("proxy bare /[space]", () => {
  it("opens a space on its Today for a V2 browser", () => {
    const res = proxy(makeRequest("/memax-v2", { ui: "v2" }));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/memax-v2/today",
    );
  });

  it("leaves it to V1's 404 without the opt-in", () => {
    const res = proxy(makeRequest("/memax-v2"));
    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
  });

  it.each(["/memax-v2", "/personal", "/a", "/memax-team", "/x1"])(
    "runs the proxy for %s",
    (path) => {
      expect(unstable_doesMiddlewareMatch({ config, url: path })).toBe(true);
    },
  );

  it("never matches a reserved slug as a space", () => {
    // The matcher without its bare-space entry: /login, /memories and
    // the V2 areas (/signin…) run the proxy for their own rules.
    const others = {
      matcher: config.matcher.filter((m) => !m.startsWith("/:space((")),
    };
    expect(others.matcher).toHaveLength(config.matcher.length - 1);
    const leaks = [...RESERVED_SPACE_SLUGS].filter(
      (slug) =>
        unstable_doesMiddlewareMatch({ config, url: `/${slug}` }) &&
        !unstable_doesMiddlewareMatch({ config: others, url: `/${slug}` }),
    );
    expect(leaks, "list the slug in the bare-space matcher").toEqual([]);
    for (const slug of RESERVED_SPACE_SLUGS) {
      const location = proxy(makeRequest(`/${slug}`, { ui: "v2" })).headers.get(
        "location",
      );
      expect(location ?? "", slug).not.toMatch(/\/today$/);
    }
  });

  it("skips files and other shapes", () => {
    for (const path of ["/favicon.svg", "/robots.txt", "/Memax"]) {
      expect(unstable_doesMiddlewareMatch({ config, url: path })).toBe(false);
    }
  });
});

describe("proxy matcher covers the V2 path set", () => {
  // config.matcher is static (Next reads it at build time), so it
  // repeats lib/ui-gate's areas and places. These fail if they drift.
  it.each(V2_PATHS)("runs the proxy for %s", (path) => {
    expect(isV2Path(path)).toBe(true);
    expect(unstable_doesMiddlewareMatch({ config, url: path })).toBe(true);
  });

  it.each(["/", "/login", "/memories", "/memories/topics/welcome"])(
    "still runs the proxy for V1 path %s",
    (path) => {
      expect(unstable_doesMiddlewareMatch({ config, url: path })).toBe(true);
    },
  );

  it.each([
    "/settings",
    "/h/personal/memories",
    "/brain",
    "/agents/connect",
    "/register",
    "/api/auth/me",
    "/dev/kitchen",
  ])("skips the proxy for %s", (path) => {
    expect(isV2Path(path)).toBe(false);
    expect(unstable_doesMiddlewareMatch({ config, url: path })).toBe(false);
  });
});

/**
 * Plan 24 phase 4b removed the v1/v2 cookie dispatch. The proxy now
 * redirects legacy /memories paths unconditionally; cookie-gated cases
 * dropped from the test (`when cookie is v1 or unset` was the v1
 * fallback path that no longer exists).
 */
describe("proxy legacy path normalization", () => {
  it("redirects /memories to /h/personal/memories", () => {
    const res = proxy(makeRequest("/memories"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });

  it("redirects /memories/ (trailing slash) the same way", () => {
    const res = proxy(makeRequest("/memories/"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });

  it("redirects /memories/topics/<id> to /h/personal/topics/<id>", () => {
    const res = proxy(makeRequest("/memories/topics/welcome"));
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
    const res = proxy(makeRequest("/memories/abc-123"));
    expect(res.status).toBe(200);
  });

  it("does NOT redirect /memories/topics (overview, not detail)", () => {
    const res = proxy(makeRequest("/memories/topics"));
    // /memories/topics is the topics overview (no trailing id) — not a
    // memory detail (because "topics" is reserved). No v2 equivalent
    // beyond /h/personal/memories — so the proxy passes through;
    // the route segment handles it.
    expect(res.status).toBe(200);
  });

  it("does NOT redirect /memories/new (reserved segment)", () => {
    const res = proxy(makeRequest("/memories/new"));
    expect(res.status).toBe(200);
  });
});

describe("proxy silent session restore", () => {
  it.each(["/", "/login"])(
    "redirects %s to /home when the session-presence cookie is set",
    (path) => {
      const res = proxy(makeRequest(path, { sessionPresence: true }));
      expect(res.status).toBe(307);
      expect(res.headers.get("location")).toBe("https://memax.app/home");
    },
  );

  it.each(["/", "/login"])("leaves %s alone without the cookie", (path) => {
    const res = proxy(makeRequest(path));
    expect(res.status).toBe(200);
  });

  it("never redirects /register — invite links carry ?invite=TOKEN", () => {
    const res = proxy(
      makeRequest("/register?invite=tok_123", { sessionPresence: true }),
    );
    expect(res.status).toBe(200);
  });

  it("skips the fast path when the request carries a query string", () => {
    // /login?returnTo=… and OAuth error params encode flow state the
    // /home resolver knows nothing about — redirecting would drop it.
    const res = proxy(
      makeRequest("/login?returnTo=%2Fagents", { sessionPresence: true }),
    );
    expect(res.status).toBe(200);
  });

  it("does not let the cookie affect legacy path normalization", () => {
    const res = proxy(makeRequest("/memories", { sessionPresence: true }));
    expect(res.headers.get("location")).toBe(
      "https://memax.app/h/personal/memories",
    );
  });
});
