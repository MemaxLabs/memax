import { readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  bareSpaceSlug,
  decideUiGate,
  hasV2Opt,
  isOpenV2Path,
  isSpaceSlug,
  isV2Path,
  needsV2Opt,
  RESERVED_SPACE_SLUGS,
  V2_SPACE_PLACES,
  v1HomePath,
  v2PageFor,
} from "./ui-gate";

const webRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);

/**
 * First URL segments of every route in src/app: walks through route
 * groups `(x)`, skips private `_x`, slots `@x` and dynamic `[x]`.
 */
function topLevelRouteSegments(dir: string): string[] {
  const segments: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const name = entry.name;
    if (name.startsWith("(") && name.endsWith(")")) {
      segments.push(...topLevelRouteSegments(path.join(dir, name)));
    } else if (!/^[_@[]/.test(name)) {
      segments.push(name);
    }
  }
  return segments;
}

describe("RESERVED_SPACE_SLUGS", () => {
  it("includes the plan's list (§6.3)", () => {
    for (const word of [
      "settings",
      "setup",
      "signin",
      "device",
      "join",
      "oauth",
      "pricing",
      "security",
      "docs",
      "admin",
      "api",
      "h",
    ]) {
      expect(RESERVED_SPACE_SLUGS.has(word), word).toBe(true);
    }
  });

  it("includes every top-level route in src/app", () => {
    const segments = topLevelRouteSegments(path.join(webRoot, "src/app"));
    // Sanity: the walk sees both UIs and the route handlers.
    expect(segments).toEqual(
      expect.arrayContaining(["api", "admin", "home", "dev"]),
    );
    const missing = segments.filter((s) => !RESERVED_SPACE_SLUGS.has(s));
    expect(missing, "add new top-level routes to RESERVED_SPACE_SLUGS").toEqual(
      [],
    );
  });

  it("includes every folder in public/", () => {
    const folders = readdirSync(path.join(webRoot, "public"), {
      withFileTypes: true,
    })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
    expect(folders.filter((f) => !RESERVED_SPACE_SLUGS.has(f))).toEqual([]);
  });

  it("holds only lowercase slug-shaped words", () => {
    for (const word of RESERVED_SPACE_SLUGS) {
      expect(word, word).toMatch(/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/);
    }
  });
});

describe("isSpaceSlug", () => {
  it.each(["memax-v2", "personal", "memax-team", "a", "x1"])(
    "accepts %s",
    (slug) => expect(isSpaceSlug(slug)).toBe(true),
  );

  it.each([
    "settings",
    "h",
    "home",
    "Memax",
    "-memax",
    "memax-",
    "memax_v2",
    "memax.v2",
    "",
    "a".repeat(51),
  ])("rejects %j", (slug) => expect(isSpaceSlug(slug)).toBe(false));
});

describe("isV2Path", () => {
  it.each([
    "/signin",
    "/signin/",
    "/device",
    "/setup",
    "/setup/import",
    "/join/inv_123",
    "/settings/plan",
    "/settings/export",
    "/dev/ledger",
    "/dev/ledger/tokens",
    ...V2_SPACE_PLACES.map((place) => `/memax-v2/${place}`),
    "/memax-v2/memories/M-0219",
    "/memax-v2/review/M-0431/compare",
    "/memax-v2/dream/214",
    "/oauth/authorize",
  ])("%s is V2", (pathname) => expect(isV2Path(pathname)).toBe(true));

  it.each([
    "/",
    "/settings",
    "/settings/",
    "/home",
    "/login",
    "/agents",
    "/agents/connect",
    "/memories",
    "/memories/today",
    "/h/personal/memories",
    "/h/today",
    "/dev",
    "/dev/kitchen",
    "/dev/ledgers",
    "/dev/ui",
    "/memax-v2",
    "/memax-v2/unknown",
    "/Memax-V2/today",
    "/api/auth/me",
    "/oauth/consent",
    "/oauth",
  ])("%s is not V2", (pathname) => expect(isV2Path(pathname)).toBe(false));
});

describe("decideUiGate", () => {
  it("continues on V1 paths whatever the cookie", () => {
    for (const uiCookie of [undefined, "v2", "v1"]) {
      expect(
        decideUiGate({ pathname: "/home", uiCookie, hasSession: true }),
      ).toEqual({ action: "continue" });
    }
  });

  it("continues on V2 paths with memax_ui=v2", () => {
    expect(
      decideUiGate({
        pathname: "/memax-v2/today",
        uiCookie: "v2",
        hasSession: false,
      }),
    ).toEqual({ action: "continue" });
  });

  it("opens the device sign-in, the sign-in, unsubscribing and OAuthConsent for every browser", () => {
    for (const pathname of [
      "/device",
      "/device/",
      "/signin",
      "/signin/callback",
      "/unsubscribe",
      "/oauth/authorize",
    ]) {
      expect(isOpenV2Path(pathname)).toBe(true);
      for (const uiCookie of [undefined, "v1", "v2"]) {
        for (const hasSession of [false, true]) {
          expect(decideUiGate({ pathname, uiCookie, hasSession })).toEqual({
            action: "continue",
          });
        }
      }
    }
    for (const pathname of ["/setup", "/setup/import", "/join/x", "/"]) {
      expect(isOpenV2Path(pathname)).toBe(false);
    }
  });

  it("sends a signed-out browser on a V2 path to sign in, then on by its flag", () => {
    // A V2 link from an email or the CLI: the person signs in and lands by
    // their flag, so no re-check marker is needed.
    expect(
      decideUiGate({
        pathname: "/memax-v2/today",
        search: "?x=1",
        uiCookie: undefined,
        hasSession: false,
      }),
    ).toEqual({
      action: "redirect",
      pathname: "/signin",
      search: `?${new URLSearchParams({ next: "/memax-v2/today?x=1" })}`,
      recheck: false,
    });
    expect(
      decideUiGate({
        pathname: "/setup/import",
        uiCookie: "1",
        hasSession: false,
      }),
    ).toEqual({
      action: "redirect",
      pathname: "/signin",
      search: `?${new URLSearchParams({ next: "/setup/import" })}`,
      recheck: false,
    });
    // The dev fixtures aren't anyone's page: still V1's home.
    expect(
      decideUiGate({
        pathname: "/dev/ledger/tokens",
        uiCookie: undefined,
        hasSession: false,
      }),
    ).toEqual({ action: "redirect", pathname: "/" });
  });

  it("sends a signed-in browser without the hint to read its V2 UI flag again, once", () => {
    // The flag may have turned on after the session started (memax init
    // made the person's first space): the sign-in page reads it, which
    // sets the hint, and comes back with the path and its query.
    expect(
      decideUiGate({
        pathname: "/memax-v2/review",
        search: "?filter=import&import=i-1",
        uiCookie: undefined,
        hasSession: true,
      }),
    ).toEqual({
      action: "redirect",
      pathname: "/signin",
      search: "?next=%2Fmemax-v2%2Freview%3Ffilter%3Dimport%26import%3Di-1",
      recheck: true,
    });
    expect(
      decideUiGate({
        pathname: "/settings/plan",
        uiCookie: "1",
        hasSession: true,
      }),
    ).toEqual({
      action: "redirect",
      pathname: "/signin",
      search: "?next=%2Fsettings%2Fplan",
      recheck: true,
    });
    // A client navigation's router query isn't the page's.
    expect(
      decideUiGate({
        pathname: "/memax-v2/search",
        search: "?q=river&_rsc=1x2y",
        uiCookie: undefined,
        hasSession: true,
      }),
    ).toMatchObject({ search: "?next=%2Fmemax-v2%2Fsearch%3Fq%3Driver" });
    // Re-checked a moment ago, and still no hint: the person doesn't see
    // V2 (or the hint can't be set), so V1's home, never a loop.
    expect(
      decideUiGate({
        pathname: "/setup/import",
        uiCookie: undefined,
        hasSession: true,
        rechecked: true,
      }),
    ).toEqual({ action: "redirect", pathname: "/home" });
    // The dev fixtures aren't anyone's page: no re-check.
    expect(
      decideUiGate({
        pathname: "/dev/ledger/tokens",
        uiCookie: undefined,
        hasSession: true,
      }),
    ).toEqual({ action: "redirect", pathname: "/home" });
    // With the hint, nothing to read again.
    expect(
      decideUiGate({
        pathname: "/setup/import",
        uiCookie: "v2",
        hasSession: true,
        rechecked: true,
      }),
    ).toEqual({ action: "continue" });
  });

  it("says which paths need the opt-in, query and all", () => {
    for (const path of [
      "/setup/import?space=memax-v2",
      "/memax-v2/today",
      "/memax-v2/review?filter=import#k",
      "/settings/account",
      "/join/abc",
    ]) {
      expect(needsV2Opt(path)).toBe(true);
    }
    for (const path of [
      "/device?code=WQRT-4821",
      "/oauth/authorize?request=r1",
      "/signin",
      "/unsubscribe?token=t",
      "/home",
      "/h/personal/memories",
      "/settings",
      "/memax-v2",
      "/",
    ]) {
      expect(needsV2Opt(path)).toBe(false);
    }
  });

  it("sends every browser from V1's retired consent page to OAuthConsent, query and all", () => {
    for (const pathname of ["/oauth/consent", "/oauth/consent/"]) {
      expect(v2PageFor(pathname)).toBe("/oauth/authorize");
      for (const uiCookie of [undefined, "v1", "v2"]) {
        for (const hasSession of [false, true]) {
          expect(decideUiGate({ pathname, uiCookie, hasSession })).toEqual({
            action: "redirect",
            pathname: "/oauth/authorize",
            keepQuery: true,
          });
        }
      }
    }
    expect(v2PageFor("/oauth/consent/x")).toBeNull();
    expect(v2PageFor("/oauth/authorize")).toBeNull();
  });

  it("only accepts the exact v2 value", () => {
    expect(hasV2Opt("v2")).toBe(true);
    for (const value of [undefined, "", "V2", "v2 ", "1", "true"]) {
      expect(hasV2Opt(value)).toBe(false);
    }
  });

  it("opens a bare /[space] on its Today, for V2 browsers only", () => {
    expect(
      decideUiGate({ pathname: "/memax-v2", uiCookie: "v2", hasSession: true }),
    ).toEqual({ action: "redirect", pathname: "/memax-v2/today" });
    expect(
      decideUiGate({
        pathname: "/memax-v2/",
        uiCookie: "v2",
        hasSession: false,
      }),
    ).toEqual({ action: "redirect", pathname: "/memax-v2/today" });
    expect(
      decideUiGate({
        pathname: "/memax-v2",
        uiCookie: undefined,
        hasSession: true,
      }),
    ).toEqual({ action: "continue" });
    // Reserved words are V1 routes, never spaces.
    expect(
      decideUiGate({ pathname: "/home", uiCookie: "v2", hasSession: true }),
    ).toEqual({ action: "continue" });
  });

  it("names the space of a bare /[space] only", () => {
    expect(bareSpaceSlug("/memax-v2")).toBe("memax-v2");
    expect(bareSpaceSlug("/memax-v2/today")).toBeNull();
    expect(bareSpaceSlug("/settings")).toBeNull();
    expect(bareSpaceSlug("/Memax")).toBeNull();
    expect(bareSpaceSlug("/")).toBeNull();
  });

  it("picks the V1 home from the session", () => {
    expect(v1HomePath(true)).toBe("/home");
    expect(v1HomePath(false)).toBe("/");
  });
});
