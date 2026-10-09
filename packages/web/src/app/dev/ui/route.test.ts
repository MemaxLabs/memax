import { afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET } from "./route";

function get(query: string) {
  return GET(new NextRequest(`https://memax.app/dev/ui${query}`));
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("/dev/ui", () => {
  it("opts into V2 and opens the Ledger specimen", () => {
    const res = get("?v=2");
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://memax.app/dev/ledger/tokens",
    );
    const cookie = res.headers.get("set-cookie") ?? "";
    expect(cookie).toMatch(/^memax_ui=v2;/);
    expect(cookie).toMatch(/Path=\//);
    expect(cookie).toMatch(/HttpOnly/i);
    expect(cookie).toMatch(/SameSite=lax/i);
    expect(cookie).toMatch(/Secure/);
    expect(res.headers.get("cache-control")).toBe("no-store");
  });

  it("goes back to V1 and clears the cookie", () => {
    const res = get("?v=1");
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("https://memax.app/");
    const cookie = res.headers.get("set-cookie") ?? "";
    expect(cookie).toMatch(/^memax_ui=;/);
    expect(cookie).toMatch(/Max-Age=0/);
    expect(cookie).toMatch(/Path=\//);
  });

  it("lands on a same-origin next path", () => {
    const res = get("?v=2&next=%2Fmemax-v2%2Ftoday");
    expect(res.headers.get("location")).toBe(
      "https://memax.app/memax-v2/today",
    );
  });

  it.each(["https://evil.example", "//evil.example", "/\\evil.example", "x"])(
    "ignores an unsafe next (%s)",
    (next) => {
      const res = get(`?v=1&next=${encodeURIComponent(next)}`);
      expect(res.headers.get("location")).toBe("https://memax.app/");
    },
  );

  it.each(["", "?v=3", "?v=v2"])("explains itself for %j", (query) => {
    const res = get(query);
    expect(res.status).toBe(400);
    expect(res.headers.get("set-cookie")).toBeNull();
  });

  it("is a 404 in production builds", () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("NEXT_PUBLIC_DEV_FIXTURES", "");
    const res = get("?v=2");
    expect(res.status).toBe(404);
    expect(res.headers.get("set-cookie")).toBeNull();
  });

  it("works in production builds that keep the fixtures", () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("NEXT_PUBLIC_DEV_FIXTURES", "1");
    expect(get("?v=2").status).toBe(307);
  });
});
