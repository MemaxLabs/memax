import { describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

// The Landing at "/" (lib/ui-gate.ts LANDING_AT_ROOT): with the switch on,
// the gate answers "rewrite" for a signed-out "/", and the proxy serves
// /welcome at that address, query and all. The switch is a constant, so
// this file runs the proxy with the gate deciding as if it were on.
vi.mock("./lib/ui-gate", async (importOriginal) => {
  const gate = await importOriginal<typeof import("./lib/ui-gate")>();
  return {
    ...gate,
    decideUiGate: (args: Parameters<typeof gate.decideUiGate>[0]) =>
      gate.decideUiGate({ ...args, landingAtRoot: true }),
  };
});

const { proxy } = await import("./proxy");

function request(path: string, cookie?: string) {
  return new NextRequest(`https://memax.app${path}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

describe("proxy with the Landing at /", () => {
  it("serves the Landing at / to a signed-out visitor, query and all", () => {
    const res = proxy(request("/?utm_source=hn"));
    expect(res.headers.get("location")).toBeNull();
    expect(res.headers.get("x-middleware-rewrite")).toBe(
      "https://memax.app/welcome?utm_source=hn",
    );
  });

  it("still sends a signed-in browser home", () => {
    const res = proxy(request("/", "memax_session_presence=1"));
    expect(res.headers.get("x-middleware-rewrite")).toBeNull();
    expect(new URL(res.headers.get("location")!).pathname).toBe("/home");
  });
});
