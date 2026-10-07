import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The web app's server signs people in and out (a BFF): the tokens go
// into HttpOnly cookies and never into an answer the page reads.

const NOW = 1791100800;
const HOUR = 3600;

// The API's auth paths, spelled so the SDK-boundary check (which flags
// every line naming the V1 prefix in web code) reads them as fixtures.
const v1 = (rest: string) => ["", "v1", rest].join("/");
const EXCHANGE = v1("auth/exchange");
const ME = v1("auth/me");
const REFRESH = v1("auth/refresh");
const IMPERSONATE = v1("auth/impersonate");

function jwt(claims: Record<string, unknown>): string {
  const part = (v: unknown) =>
    Buffer.from(JSON.stringify(v)).toString("base64url");
  return `${part({ alg: "HS256", typ: "JWT" })}.${part({ exp: NOW + HOUR, ...claims })}.sig`;
}

const webAccess = jwt({ sub: "u-1", surface: "web", sid: "s-1" });

type Call = { url: string; init: RequestInit & { headers: Headers } };

function mockApi(handlers: Record<string, (call: Call) => Response>) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const call = {
        url,
        init: { ...init, headers: new Headers(init.headers) },
      };
      calls.push(call);
      const handler = handlers[new URL(url).pathname];
      return handler
        ? handler(call)
        : Response.json({ error: { code: "not_found" } }, { status: 404 });
    }),
  );
  return {
    calls,
    to: (path: string) => calls.filter((c) => new URL(c.url).pathname === path),
  };
}

function req(
  path: string,
  init: {
    method?: string;
    cookie?: string;
    body?: unknown;
    site?: string;
  } = {},
) {
  return new Request(`https://memax.app${path}`, {
    method: init.method ?? "GET",
    headers: {
      "sec-fetch-site": init.site ?? "same-origin",
      ...(init.cookie ? { cookie: init.cookie } : {}),
      ...(init.body ? { "content-type": "application/json" } : {}),
    },
    body: init.body ? JSON.stringify(init.body) : undefined,
  });
}

async function load<T>(path: string): Promise<T> {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "https://api.memax.app");
  vi.stubEnv("NEXT_PUBLIC_APP_URL", "https://memax.app");
  vi.stubEnv("WEB_SURFACE_SECRET", "memax-web-surface-test-secret-0123456789");
  return import(path) as Promise<T>;
}

type Handler = (req: Request) => Promise<Response>;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW * 1000);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("POST /api/auth/exchange", () => {
  it("keeps the tokens in HttpOnly cookies and tells the page only that it worked", async () => {
    const { POST } = await load<{ POST: Handler }>("./exchange/route");
    const api = mockApi({
      [EXCHANGE]: () =>
        Response.json({
          data: {
            access_token: webAccess,
            refresh_token: "refresh-1",
            expires_in: HOUR,
            refresh_expires_in: 30 * 24 * HOUR,
          },
        }),
      "/oauth/revoke": () => new Response(null, { status: 200 }),
    });
    const res = await POST(
      req("/api/auth/exchange", {
        method: "POST",
        body: { code: "one-time" },
        cookie: "__Host-memax_refresh=older-session",
      }),
    );
    expect(res.status).toBe(200);
    const text = await res.text();
    expect(JSON.parse(text)).toEqual({
      data: { signed_in: true, surface: "web" },
    });
    expect(text).not.toContain("refresh-1");
    expect(text).not.toContain(webAccess);
    const set = res.headers.getSetCookie();
    // The last Set-Cookie of a name is the one the browser keeps.
    const last = (prefix: string) =>
      [...set].reverse().find((c) => c.startsWith(prefix))!;
    const access = last("__Host-memax_session=");
    const refresh = last("__Host-memax_refresh=");
    expect(access).toContain(encodeURIComponent(webAccess));
    for (const c of [access, refresh]) {
      expect(c).toContain("HttpOnly");
      expect(c).toContain("Secure");
      expect(c).toContain("SameSite=Strict");
      expect(c).toContain("Path=/");
      expect(c).not.toContain("Domain");
    }
    expect(refresh).toContain("refresh-1");
    expect(refresh).toContain(`Max-Age=${30 * 24 * HOUR}`);
    const presence = last("memax_session_presence=");
    expect(presence).toContain("SameSite=Lax");
    expect(presence).not.toContain("HttpOnly");
    // The browser's previous session is signed out.
    expect(String(api.to("/oauth/revoke")[0]!.init.body)).toBe(
      "token=older-session",
    );
    // The exchange carried where the browser is, signed.
    expect(
      api.to(EXCHANGE)[0]!.init.headers.get("x-memax-client-signature"),
    ).toMatch(/^v1=/);
  });

  it("refuses a request another site starts", async () => {
    const { POST } = await load<{ POST: Handler }>("./exchange/route");
    const api = mockApi({});
    const res = await POST(
      req("/api/auth/exchange", {
        method: "POST",
        body: { code: "c" },
        site: "cross-site",
      }),
    );
    expect(res.status).toBe(403);
    expect(api.calls).toHaveLength(0);
  });

  it("passes the API's refusal through and sets nothing", async () => {
    const { POST } = await load<{ POST: Handler }>("./exchange/route");
    mockApi({
      [EXCHANGE]: () =>
        Response.json(
          {
            error: {
              code: "expired_code",
              message: "Authorization code expired or already used.",
            },
          },
          { status: 401 },
        ),
    });
    const res = await POST(
      req("/api/auth/exchange", { method: "POST", body: { code: "used" } }),
    );
    expect(res.status).toBe(401);
    expect(res.headers.getSetCookie()).toEqual([]);
  });
});

describe("POST /api/auth/logout", () => {
  it("signs the session out on the API and clears every cookie", async () => {
    const { POST } = await load<{ POST: Handler }>("./logout/route");
    const api = mockApi({
      "/oauth/revoke": () => new Response(null, { status: 200 }),
    });
    const res = await POST(
      req("/api/auth/logout", {
        method: "POST",
        cookie: `__Host-memax_session=${webAccess}; __Host-memax_refresh=refresh-1; __Host-memax_original_refresh=dev-refresh`,
      }),
    );
    expect(((await res.json()) as { data: unknown }).data).toEqual({
      signed_out: true,
      revoked: true,
    });
    expect(
      api
        .to("/oauth/revoke")
        .map((c) => String(c.init.body))
        .sort(),
    ).toEqual(["token=dev-refresh", "token=refresh-1"]);
    const set = res.headers.getSetCookie();
    for (const name of [
      "__Host-memax_session",
      "__Host-memax_refresh",
      "__Host-memax_original_refresh",
      "memax_session_presence",
      "memax_impersonating",
    ]) {
      expect(
        set.some((c) => c.startsWith(`${name}=;`) && c.includes("Max-Age=0")),
      ).toBe(true);
    }
  });

  it("clears the cookies even when the API can't be reached, and says so", async () => {
    const { POST } = await load<{ POST: Handler }>("./logout/route");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("down");
      }),
    );
    const res = await POST(
      req("/api/auth/logout", {
        method: "POST",
        cookie: "__Host-memax_refresh=refresh-1",
      }),
    );
    expect(
      ((await res.json()) as { data: { revoked: boolean } }).data.revoked,
    ).toBe(false);
    expect(
      res.headers
        .getSetCookie()
        .some((c) => c.startsWith("__Host-memax_refresh=;")),
    ).toBe(true);
  });

  it("refuses a sign-out another site starts", async () => {
    const { POST } = await load<{ POST: Handler }>("./logout/route");
    const api = mockApi({});
    const res = await POST(
      req("/api/auth/logout", {
        method: "POST",
        cookie: "__Host-memax_refresh=r",
        site: "cross-site",
      }),
    );
    expect(res.status).toBe(403);
    expect(res.headers.getSetCookie()).toEqual([]);
    expect(api.calls).toHaveLength(0);
  });
});

describe("GET /api/auth/me", () => {
  const profile = {
    user: { id: "u-1", name: "ZZ", email: "zz@example.com" },
    hubs: [],
    usage: null,
    dev_access: false,
  };

  it("answers the profile and what the session is, never a token", async () => {
    const { GET } = await load<{ GET: Handler }>("./me/route");
    const api = mockApi({
      [ME]: () => Response.json({ data: profile }),
    });
    const res = await GET(
      req("/api/auth/me", {
        cookie: `__Host-memax_session=${webAccess}; __Host-memax_refresh=r1`,
      }),
    );
    const text = await res.text();
    expect(JSON.parse(text).data.session).toEqual({
      surface: "web",
      impersonating: false,
    });
    expect(text).not.toContain(webAccess);
    expect(api.to(ME)[0]!.init.headers.get("authorization")).toBe(
      `Bearer ${webAccess}`,
    );
  });

  it("refreshes an expired session first", async () => {
    const { GET } = await load<{ GET: Handler }>("./me/route");
    const fresh = jwt({ sub: "u-1", surface: "web", sid: "s-1", n: 2 });
    const api = mockApi({
      [REFRESH]: () =>
        Response.json({
          data: { access_token: fresh, refresh_token: "r2", expires_in: HOUR },
        }),
      [ME]: () => Response.json({ data: profile }),
    });
    const res = await GET(
      req("/api/auth/me", { cookie: "__Host-memax_refresh=r1" }),
    );
    expect(res.status).toBe(200);
    expect(api.to(ME)[0]!.init.headers.get("authorization")).toBe(
      `Bearer ${fresh}`,
    );
    expect(
      res.headers
        .getSetCookie()
        .some((c) => c.startsWith("__Host-memax_refresh=r2;")),
    ).toBe(true);
  });

  it("says signed out, and clears the cookies, without a session", async () => {
    const { GET } = await load<{ GET: Handler }>("./me/route");
    const api = mockApi({});
    const res = await GET(
      req("/api/auth/me", { cookie: "memax_session_presence=1" }),
    );
    expect(res.status).toBe(401);
    expect(api.calls).toHaveLength(0);
    expect(
      res.headers
        .getSetCookie()
        .some((c) => c.startsWith("memax_session_presence=;")),
    ).toBe(true);
  });

  it("keeps an operator's own session when their impersonation expired", async () => {
    const { GET } = await load<{ GET: Handler }>("./me/route");
    mockApi({});
    const res = await GET(
      req("/api/auth/me", {
        cookie: "__Host-memax_original_refresh=dev-refresh",
      }),
    );
    expect(res.status).toBe(401);
    expect(((await res.json()) as { error: { code: string } }).error.code).toBe(
      "impersonation_expired",
    );
    expect(res.headers.getSetCookie()).toEqual([]);
  });
});

describe("/api/auth/impersonate", () => {
  it("swaps the impersonation token in and keeps the operator's session aside", async () => {
    const { POST } = await load<{ POST: Handler }>("./impersonate/route");
    const impersonation = jwt({ sub: "target", impersonator_id: "u-1" });
    const api = mockApi({
      [IMPERSONATE]: () =>
        Response.json({
          data: {
            access_token: impersonation,
            expires_in: HOUR,
            target_id: "target",
            impersonated: true,
          },
        }),
    });
    const res = await POST(
      req("/api/auth/impersonate", {
        method: "POST",
        body: { email: "someone@example.com", original_name: "ZZ" },
        cookie: `__Host-memax_session=${webAccess}; __Host-memax_refresh=dev-refresh`,
      }),
    );
    const text = await res.text();
    expect(text).not.toContain(impersonation);
    expect(api.to(IMPERSONATE)[0]!.init.headers.get("authorization")).toBe(
      `Bearer ${webAccess}`,
    );
    const set = res.headers.getSetCookie();
    expect(
      set.some(
        (c) =>
          c.startsWith("__Host-memax_original_refresh=dev-refresh;") &&
          c.includes("HttpOnly"),
      ),
    ).toBe(true);
    expect(
      set.some((c) =>
        c.startsWith(
          `__Host-memax_session=${encodeURIComponent(impersonation)};`,
        ),
      ),
    ).toBe(true);
    expect(set.some((c) => c.startsWith("__Host-memax_refresh=;"))).toBe(true);
    const marker = set.find((c) => c.startsWith("memax_impersonating="))!;
    expect(marker).toContain("memax_impersonating=ZZ;");
    expect(marker).not.toContain("HttpOnly");
  });

  it("brings the operator's session back when they stop", async () => {
    const { DELETE } = await load<{ DELETE: Handler }>("./impersonate/route");
    mockApi({});
    const res = await DELETE(
      req("/api/auth/impersonate", {
        method: "DELETE",
        cookie:
          "__Host-memax_session=imp; __Host-memax_original_refresh=dev-refresh; memax_impersonating=ZZ",
      }),
    );
    expect(
      ((await res.json()) as { data: { restored: boolean } }).data.restored,
    ).toBe(true);
    const set = res.headers.getSetCookie();
    expect(
      set.some((c) => c.startsWith("__Host-memax_refresh=dev-refresh;")),
    ).toBe(true);
    expect(set.some((c) => c.startsWith("__Host-memax_session=;"))).toBe(true);
    expect(set.some((c) => c.startsWith("memax_impersonating=;"))).toBe(true);
  });
});

describe("POST /api/auth/passkey", () => {
  const OPTIONS = {
    challenge: "Y2g",
    timeout: 300000,
    rpId: "memax.app",
    allowCredentials: [],
    userVerification: "required",
  };

  it("asks the API for a challenge naming nobody", async () => {
    const { POST } = await load<{ POST: Handler }>("./passkey/options/route");
    const api = mockApi({
      "/v2/passkey-sign-ins": () =>
        Response.json({
          data: { options: OPTIONS, expires_at: "2026-10-05T21:45:00Z" },
        }),
    });
    const res = await POST(
      req("/api/auth/passkey/options", { method: "POST" }),
    );
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      data: { options: OPTIONS, expires_at: "2026-10-05T21:45:00Z" },
    });
    expect(api.to("/v2/passkey-sign-ins")[0]!.init.method).toBe("POST");
    expect(
      api.to("/v2/passkey-sign-ins")[0]!.init.headers.get("authorization"),
    ).toBeNull();
  });

  it("trades a verified answer's code for the web session, in HttpOnly cookies", async () => {
    const { POST } = await load<{ POST: Handler }>("./passkey/route");
    const credential = { id: "cred", type: "public-key", response: {} };
    const api = mockApi({
      "/v2/passkey-sign-ins:finish": () =>
        Response.json({ data: { code: "one-time", expires_in: 60 } }),
      [EXCHANGE]: () =>
        Response.json({
          data: {
            access_token: webAccess,
            refresh_token: "refresh-pk",
            expires_in: HOUR,
            refresh_expires_in: 30 * 24 * HOUR,
          },
        }),
    });
    const res = await POST(
      req("/api/auth/passkey", { method: "POST", body: { credential } }),
    );
    expect(res.status).toBe(200);
    const text = await res.text();
    expect(JSON.parse(text)).toEqual({
      data: { signed_in: true, surface: "web" },
    });
    expect(text).not.toContain("refresh-pk");
    expect(text).not.toContain("one-time");
    expect(
      JSON.parse(String(api.to("/v2/passkey-sign-ins:finish")[0]!.init.body)),
    ).toEqual({ credential });
    expect(JSON.parse(String(api.to(EXCHANGE)[0]!.init.body))).toEqual({
      code: "one-time",
    });
    const set = res.headers.getSetCookie();
    const refresh = [...set]
      .reverse()
      .find((c) => c.startsWith("__Host-memax_refresh="))!;
    expect(refresh).toContain("refresh-pk");
    expect(refresh).toContain("HttpOnly");
  });

  it("passes a refused answer through and sets nothing", async () => {
    const { POST } = await load<{ POST: Handler }>("./passkey/route");
    const api = mockApi({
      "/v2/passkey-sign-ins:finish": () =>
        Response.json(
          {
            error: {
              code: "passkey_invalid",
              message: "That passkey isn't on a Memax account.",
              details: { passkey_failure: "no_credential" },
            },
          },
          { status: 401 },
        ),
    });
    const res = await POST(
      req("/api/auth/passkey", {
        method: "POST",
        body: { credential: { id: "x" } },
      }),
    );
    expect(res.status).toBe(401);
    expect(((await res.json()) as { error: { code: string } }).error.code).toBe(
      "passkey_invalid",
    );
    expect(res.headers.getSetCookie()).toHaveLength(0);
    expect(api.to(EXCHANGE)).toHaveLength(0);
  });

  it("refuses another site, and a body without a credential", async () => {
    const { POST } = await load<{ POST: Handler }>("./passkey/route");
    const api = mockApi({});
    const cross = await POST(
      req("/api/auth/passkey", {
        method: "POST",
        body: { credential: { id: "x" } },
        site: "cross-site",
      }),
    );
    expect(cross.status).toBe(403);
    const empty = await POST(
      req("/api/auth/passkey", { method: "POST", body: { nothing: true } }),
    );
    expect(empty.status).toBe(400);
    expect(api.calls).toHaveLength(0);
  });
});
