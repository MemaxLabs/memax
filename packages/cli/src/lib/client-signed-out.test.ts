// onSignedOut: a refresh the server refuses for good (401) says the
// session is over, which takes the Personal layer off the machine; a
// transient failure (5xx, 429, the network) says nothing and never does.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./config.js", () => ({
  loadConfig: () => ({ api_url: "http://memax.test" }),
}));

vi.mock("./credentials.js", () => ({
  loadCredentials: () => ({
    access_token: "expired-access",
    refresh_token: "refresh-token",
    expires_at: Date.now() - 1_000,
  }),
  saveCredentials: vi.fn(),
  isTokenExpired: () => true,
  getLocalAgentKey: () => undefined,
}));

import {
  getAuthHeaders,
  onSignedOut,
  setClientFetch,
  type SignedOut,
} from "./client.js";

function answer(status: number, code: string): typeof fetch {
  return (async () =>
    new Response(JSON.stringify({ error: { code, message: code } }), {
      status,
      headers: { "Content-Type": "application/json" },
    })) as typeof fetch;
}

let heard: SignedOut[];
let off: () => void;
beforeEach(() => {
  heard = [];
  off = onSignedOut((s) => heard.push(s));
});
afterEach(() => off());

describe("onSignedOut", () => {
  it.each(["session_revoked", "expired_token", "invalid_token"])(
    "hears a refresh refused with %s",
    async (code) => {
      setClientFetch(answer(401, code));
      const headers = await getAuthHeaders();
      // The stale token goes on; the API's 401 says to sign in again.
      expect(headers.Authorization).toBe("Bearer expired-access");
      expect(heard).toEqual([{ code }]);
    },
  );

  it.each([
    [503, "unavailable"],
    [500, "internal"],
    [429, "rate_limited"],
  ])("ignores a refresh that failed with %i", async (status, code) => {
    setClientFetch(answer(status, code));
    await getAuthHeaders();
    expect(heard).toEqual([]);
  });

  it("ignores a refresh that never reached the server", async () => {
    setClientFetch((async () => {
      throw new TypeError("fetch failed");
    }) as typeof fetch);
    await getAuthHeaders();
    expect(heard).toEqual([]);
  });

  it("stops hearing once unsubscribed", async () => {
    off();
    setClientFetch(answer(401, "session_revoked"));
    await getAuthHeaders();
    expect(heard).toEqual([]);
  });
});
