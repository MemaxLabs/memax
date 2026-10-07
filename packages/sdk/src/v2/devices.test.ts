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

const device: V2.DeviceAuthorization = {
  user_code: "WQRT-4821",
  state: "pending",
  client_id: "memax-cli",
  client_version: "2.0.0",
  device_name: "ziyang-mbp",
  device_os: "macOS",
  space: "memax-v2",
  address: "203.0.113.4",
  requested_at: "2026-10-05T09:01:48Z",
  expires_at: "2026-10-05T09:11:48Z",
};

describe("memax.v2.devices", () => {
  it("looks a code up with a POST, so it stays out of URLs", async () => {
    const { memax, call } = client(jsonResponse({ data: device }));
    const got = await memax.v2.devices.lookup("WQRT-4821");
    expect(got).toEqual(device);
    const c = call();
    expect(c.url).toBe("https://api.memax.app/v2/device-authorizations:lookup");
    expect(c.method).toBe("POST");
    expect(JSON.parse(String(c.body))).toEqual({ user_code: "WQRT-4821" });
    expect(c.headers["Idempotency-Key"]).toBeUndefined();
  });

  it("confirms and declines with an idempotency key", async () => {
    const { memax, call } = client(
      jsonResponse({ data: { ...device, state: "approved" } }),
      jsonResponse({ data: { ...device, state: "denied" } }),
    );
    expect(
      (await memax.v2.devices.approve("WQRT-4821", { idempotencyKey: "k1" }))
        .state,
    ).toBe("approved");
    expect(call(0).url).toBe(
      "https://api.memax.app/v2/device-authorizations:approve",
    );
    expect(call(0).headers["Idempotency-Key"]).toBe("k1");
    expect(
      (await memax.v2.devices.deny("WQRT-4821", { idempotencyKey: "k2" }))
        .state,
    ).toBe("denied");
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/device-authorizations:deny",
    );
  });

  it("surfaces device_needs_web as a refusal", async () => {
    const { memax } = client(
      jsonResponse(
        {
          error: {
            code: "refused",
            message: "Confirm this code on memax.app/device.",
            details: {
              policy: { effect: "refuse", code: "device_needs_web" },
            },
          },
        },
        { status: 403 },
      ),
    );
    const err = await memax.v2.devices
      .approve("WQRT-4821", { idempotencyKey: "k" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect((err as MemaxError).status).toBe(403);
  });
});

describe("memax.auth device sign-in (RFC 8628)", () => {
  const issued = {
    device_code: "d".repeat(43),
    user_code: "WQRT-4821",
    verification_uri: "https://memax.app/device",
    verification_uri_complete: "https://memax.app/device?code=WQRT-4821",
    expires_in: 600,
    interval: 5,
  };

  it("asks for a code as a form, without credentials", async () => {
    const { memax, call } = client(jsonResponse(issued));
    const got = await memax.auth.startDeviceSignIn({
      clientVersion: "2.0.0",
      deviceName: "ziyang-mbp",
      deviceOs: "darwin",
      space: "memax-v2",
    });
    expect(got).toEqual({
      deviceCode: issued.device_code,
      userCode: "WQRT-4821",
      verificationUri: "https://memax.app/device",
      verificationUriComplete: "https://memax.app/device?code=WQRT-4821",
      expiresIn: 600,
      interval: 5,
    });
    const c = call();
    expect(c.url).toBe("https://api.memax.app/oauth/device_authorization");
    expect(c.headers["Content-Type"]).toBe("application/x-www-form-urlencoded");
    expect(c.headers.Authorization).toBeUndefined();
    expect(Object.fromEntries(new URLSearchParams(String(c.body)))).toEqual({
      client_id: "memax-cli",
      client_version: "2.0.0",
      device_name: "ziyang-mbp",
      device_os: "darwin",
      space: "memax-v2",
    });
  });

  it("reads each of RFC 8628's answers", async () => {
    const answers: Array<[Response, string]> = [
      [
        jsonResponse({ error: "authorization_pending" }, { status: 400 }),
        "pending",
      ],
      [jsonResponse({ error: "slow_down" }, { status: 400 }), "slow_down"],
      [jsonResponse({ error: "access_denied" }, { status: 400 }), "denied"],
      [jsonResponse({ error: "expired_token" }, { status: 400 }), "expired"],
      [jsonResponse({ error: "invalid_grant" }, { status: 400 }), "invalid"],
    ];
    for (const [res, status] of answers) {
      const { memax, call } = client(res);
      expect((await memax.auth.pollDeviceSignIn("dc")).status).toBe(status);
      expect(
        Object.fromEntries(new URLSearchParams(String(call().body))),
      ).toEqual({
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
        client_id: "memax-cli",
        device_code: "dc",
      });
      expect(call().url).toBe("https://api.memax.app/oauth/token");
    }
    const { memax } = client(
      jsonResponse({
        access_token: "at",
        refresh_token: "rt",
        token_type: "Bearer",
        expires_in: 3600,
      }),
    );
    expect(await memax.auth.pollDeviceSignIn("dc")).toEqual({
      status: "signed_in",
      tokens: { access_token: "at", refresh_token: "rt", expires_in: 3600 },
    });
  });

  it("throws on what isn't an answer", async () => {
    const limited = client(
      jsonResponse(
        { error: "slow_down", error_description: "Too many codes." },
        { status: 429, headers: { "Retry-After": "3600" } },
      ),
    );
    const err = (await limited.memax.auth
      .startDeviceSignIn()
      .catch((e: unknown) => e)) as MemaxError;
    expect(err.code).toBe("rate_limited");
    expect(err.retryAfterSeconds).toBe(3600);
    const client2 = client(
      jsonResponse({ error: "invalid_client" }, { status: 401 }),
    );
    const err2 = (await client2.memax.auth
      .pollDeviceSignIn("dc")
      .catch((e: unknown) => e)) as MemaxError;
    expect(err2.code).toBe("invalid_client");
  });
});
