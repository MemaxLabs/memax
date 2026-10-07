import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError } from "../index.js";
import type { V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

function client(...responses: Response[]) {
  let i = 0;
  const fetchMock = vi.fn(async () =>
    responses[Math.min(i++, responses.length - 1)].clone(),
  );
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    fetch: fetchMock,
    maxRetries: 0,
  });
  const call = (n = 0) => {
    const [url, init] = fetchMock.mock.calls[n] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return { url, method: init.method, headers: init.headers, body: init.body };
  };
  return { memax, call };
}

const web: V2.Session = {
  id: "0199b0f3-2c4e-7000-8000-000000000001",
  surface: "web",
  client: "Chrome on macOS",
  address: "203.0.113.7",
  city: "Lisbon",
  signed_in_at: "2026-10-06T09:00:00Z",
  last_used_at: "2026-10-07T09:00:00Z",
  expires_at: "2026-11-05T09:00:00Z",
  current: true,
};
const device: V2.Session = {
  ...web,
  id: "0199b0f3-2c4e-7000-8000-000000000002",
  surface: "device",
  client: "memax CLI 2.0.0 on zz-mbp (macOS)",
  current: false,
};

describe("memax.v2.sessions", () => {
  it("lists your sessions", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { items: [web, device] } }),
    );
    const got = await memax.v2.sessions.list();
    expect(got.items.map((s) => s.surface)).toEqual(["web", "device"]);
    expect(got.items[0].current).toBe(true);
    expect(call().url).toBe("https://api.memax.app/v2/sessions");
    expect(call().method).toBe("GET");
  });

  it("signs one out, and the rest, with an idempotency key", async () => {
    const { memax, call } = client(
      jsonResponse({
        data: { ...device, revoked_at: "2026-10-07T10:00:00Z" },
      }),
      jsonResponse({ data: { revoked: 3 } }),
    );
    const ended = await memax.v2.sessions.revoke(device.id, {
      idempotencyKey: "k-1",
    });
    expect(ended.revoked_at).toBe("2026-10-07T10:00:00Z");
    expect(call(0).url).toBe(
      `https://api.memax.app/v2/sessions/${device.id}:revoke`,
    );
    expect(call(0).method).toBe("POST");
    expect(call(0).headers["Idempotency-Key"]).toBe("k-1");

    const n = await memax.v2.sessions.revokeOthers({ idempotencyKey: "k-2" });
    expect(n.revoked).toBe(3);
    expect(call(1).url).toBe("https://api.memax.app/v2/sessions:revoke-others");
    expect(call(1).headers["Idempotency-Key"]).toBe("k-2");
  });

  it("surfaces session_needs_web as a refusal", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message: "Sign other sessions out on memax.app.",
            details: {
              policy: { effect: "refuse", code: "session_needs_web" },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await memax.v2.sessions
      .revoke(device.id, { idempotencyKey: "k" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect((err as MemaxError).code).toBe("refused");
  });
});

describe("memax.auth.revoke", () => {
  it("signs a session out at /oauth/revoke with a form", async () => {
    const { memax, call } = client(new Response(null, { status: 200 }));
    await memax.auth.revoke("refresh-token");
    const c = call();
    expect(c.url).toBe("https://api.memax.app/oauth/revoke");
    expect(c.method).toBe("POST");
    expect(c.headers["Content-Type"]).toBe("application/x-www-form-urlencoded");
    expect(String(c.body)).toBe("token=refresh-token");
  });

  it("throws the OAuth error when refused", async () => {
    const { memax } = client(
      jsonResponse(
        { error: "invalid_request", error_description: "token is required" },
        { status: 400 },
      ),
    );
    const err = await memax.auth.revoke("x").catch((e: unknown) => e);
    expect((err as MemaxError).code).toBe("invalid_request");
  });
});
