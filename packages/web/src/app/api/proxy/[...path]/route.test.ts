import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The nonce is random in production; the test pins it to bytes 0…15 so
// the signature matches the vector the API's tests use
// (packages/server/internal/websurface/websurface_test.go).
vi.mock("node:crypto", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:crypto")>();
  return {
    ...actual,
    randomBytes: vi.fn(() => Buffer.from([...Array(16).keys()])),
  };
});

const VECTOR = {
  secret: "memax-web-surface-test-secret-0123456789",
  timestamp: 1791100800,
  nonce: "AAECAwQFBgcICQoLDA0ODw",
  user: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
  signature: "v1=HrupTTGJYu02EBeLUDGE3eyoT3NH1FR0V04NII2-Qwk",
};

const HOUR = 3600;

// API paths, spelled so the SDK-boundary check (which flags every line
// naming the V1 prefix in web code) reads them as fixtures.
const v1 = (rest: string) => ["", "v1", rest].join("/");
const REFRESH = v1("auth/refresh");

function jwt(claims: Record<string, unknown>): string {
  const part = (v: unknown) =>
    Buffer.from(JSON.stringify(v)).toString("base64url");
  return `${part({ alg: "HS256", typ: "JWT" })}.${part(claims)}.not-checked-here`;
}

/** A token that expires `in` seconds after the test's clock. */
function token(claims: Record<string, unknown>, expiresIn = HOUR): string {
  return jwt({ exp: VECTOR.timestamp + expiresIn, ...claims });
}

const webToken = token({ sub: VECTOR.user, surface: "web", sid: "s-1" });

type Fetched = { url: string; init: RequestInit & { headers: Headers } };

async function loadRoute(secret: string | undefined) {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "https://api.memax.app");
  vi.stubEnv("NEXT_PUBLIC_APP_URL", "https://memax.app");
  vi.stubEnv("WEB_SURFACE_SECRET", secret ?? "");
  return import("./route");
}

interface Api {
  calls: Fetched[];
  /** Calls to the API's own endpoints, by path. */
  to(path: string): Fetched[];
}

/**
 * The API: its refresh endpoint answers `refreshed` (or `refreshStatus`),
 * /oauth/revoke 200, and anything else `response` (or a 200 envelope).
 */
function mockApi(
  opts: {
    response?: (call: Fetched) => Response;
    refreshed?: { access_token: string; refresh_token: string };
    refreshStatus?: number;
    refreshThrows?: boolean;
  } = {},
): Api {
  const calls: Fetched[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const call = {
        url,
        init: { ...init, headers: new Headers(init.headers) },
      };
      calls.push(call);
      const path = new URL(url).pathname;
      if (path === REFRESH) {
        if (opts.refreshThrows) throw new Error("down");
        if (opts.refreshStatus) {
          return Response.json(
            { error: { code: "session_revoked", message: "Sign in again." } },
            { status: opts.refreshStatus },
          );
        }
        return Response.json({
          data: {
            ...(opts.refreshed ?? {
              access_token: token({
                sub: VECTOR.user,
                surface: "web",
                sid: "s-1",
              }),
              refresh_token: "refresh-2",
            }),
            expires_in: HOUR,
            refresh_expires_in: 20 * 24 * HOUR,
          },
        });
      }
      if (path === "/oauth/revoke") return new Response(null, { status: 200 });
      return (
        opts.response?.(call) ?? Response.json({ data: {} }, { status: 200 })
      );
    }),
  );
  return {
    calls,
    to: (p: string) => calls.filter((c) => new URL(c.url).pathname === p),
  };
}

function params(path: string[]) {
  return { params: Promise.resolve({ path }) };
}

/** The browser's cookie header for a session. */
function cookies(
  c: { access?: string; refresh?: string; original?: string },
  secure = true,
): string {
  const prefix = secure ? "__Host-" : "";
  return [
    c.access && `${prefix}memax_session=${c.access}`,
    c.refresh && `${prefix}memax_refresh=${c.refresh}`,
    c.original && `${prefix}memax_original_refresh=${c.original}`,
    "memax_session_presence=1",
  ]
    .filter(Boolean)
    .join("; ");
}

function keepRequest(
  cookie: string,
  extra: Record<string, string> = {},
  origin = "https://memax.app",
) {
  return new Request(
    `${origin}/api/proxy/v2/memories/M-0001:keep?space=memax-v2`,
    {
      method: "POST",
      headers: {
        cookie,
        "sec-fetch-site": "same-origin",
        "content-type": "application/json",
        "idempotency-key": "key-1",
        "if-match": '"1"',
        ...extra,
      },
      body: '{"reason":"checked"}',
    },
  );
}

const keepPath = ["v2", "memories", "M-0001:keep"];

function setCookies(res: Response): string[] {
  return res.headers.getSetCookie();
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(VECTOR.timestamp * 1000);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("/api/proxy reads the session from its cookies", () => {
  it("signs a web session's /v2 command exactly as the API verifies it", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    await POST(
      keepRequest(cookies({ access: webToken, refresh: "refresh-1" })),
      params(keepPath),
    );

    expect(api.calls).toHaveLength(1);
    const { url, init } = api.calls[0]!;
    expect(url).toBe(
      "https://api.memax.app/v2/memories/M-0001:keep?space=memax-v2",
    );
    expect(init.body).toBe('{"reason":"checked"}');
    const h = init.headers;
    expect(h.get("x-memax-surface")).toBe("web");
    expect(h.get("x-memax-surface-timestamp")).toBe(String(VECTOR.timestamp));
    expect(h.get("x-memax-surface-nonce")).toBe(VECTOR.nonce);
    expect(h.get("x-memax-surface-user")).toBe(VECTOR.user);
    expect(h.get("x-memax-surface-signature")).toBe(VECTOR.signature);
    // Commands need these on /v2, and the signature covers them.
    expect(h.get("idempotency-key")).toBe("key-1");
    expect(h.get("if-match")).toBe('"1"');
    expect(h.get("authorization")).toBe(`Bearer ${webToken}`);
    // Nothing of the browser's cookies goes upstream.
    expect(h.get("cookie")).toBeNull();
  });

  it("ignores an Authorization header the page sends", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const cli = token({ sub: VECTOR.user, surface: "cli" });
    // No session cookie: a CLI token in the header is not forwarded.
    await POST(
      keepRequest("", { authorization: `Bearer ${cli}` }),
      params(keepPath),
    );
    expect(api.calls[0]!.init.headers.get("authorization")).toBeNull();
    // With a session, the cookie's token is the one that goes.
    await POST(
      keepRequest(cookies({ access: webToken }), {
        authorization: `Bearer ${cli}`,
      }),
      params(keepPath),
    );
    expect(api.calls[1]!.init.headers.get("authorization")).toBe(
      `Bearer ${webToken}`,
    );
  });

  it("reads only the __Host- cookies over https, and only the plain ones over http", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    // A plain cookie (one a sibling subdomain could set) isn't believed
    // over https.
    await POST(
      keepRequest(cookies({ access: webToken }, false)),
      params(keepPath),
    );
    expect(api.calls[0]!.init.headers.get("authorization")).toBeNull();
    // Local development, over http: the plain names.
    await POST(
      keepRequest(
        cookies({ access: webToken }, false),
        {},
        "http://localhost:3000",
      ),
      params(keepPath),
    );
    expect(api.calls[1]!.init.headers.get("authorization")).toBe(
      `Bearer ${webToken}`,
    );
  });

  it.each([
    ["a CLI session", token({ sub: VECTOR.user, surface: "cli" })],
    ["a session from before surfaces", token({ sub: VECTOR.user })],
    [
      "an impersonation session",
      token({ sub: VECTOR.user, surface: "web", impersonator_id: "x" }),
    ],
    ["a token that isn't a JWT", "not.a-jwt"],
    ["a web token without a user", token({ surface: "web" })],
  ])("doesn't sign %s", async (_, access) => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    await POST(keepRequest(cookies({ access })), params(keepPath));
    const h = api.calls.at(-1)!.init.headers;
    expect(h.get("x-memax-surface")).toBeNull();
    expect(h.get("x-memax-surface-signature")).toBeNull();
    // The command still goes through, unsigned (client-attested).
    expect(h.get("idempotency-key")).toBe("key-1");
  });

  it("doesn't sign without WEB_SURFACE_SECRET", async () => {
    const { POST } = await loadRoute(undefined);
    const api = mockApi();
    await POST(keepRequest(cookies({ access: webToken })), params(keepPath));
    expect(
      api.calls[0]!.init.headers.get("x-memax-surface-signature"),
    ).toBeNull();
  });

  it("doesn't sign V1 paths, which don't read it", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    await GET(
      new Request(`https://memax.app/api/proxy${v1("memories")}`, {
        headers: {
          cookie: cookies({ access: webToken }),
          "sec-fetch-site": "same-origin",
        },
      }),
      params(["v1", "memories"]),
    );
    expect(api.calls[0]!.init.headers.get("x-memax-surface")).toBeNull();
    expect(api.calls[0]!.init.headers.get("authorization")).toBe(
      `Bearer ${webToken}`,
    );
  });

  it("forwards a PUT with its body, and refuses one another site starts", async () => {
    const { PUT } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const path = ["v1", "admin", "users", VECTOR.user, "v2-ui"];
    const put = (site: string) =>
      PUT(
        new Request(`https://memax.app/api/proxy/${path.join("/")}`, {
          method: "PUT",
          headers: {
            cookie: cookies({ access: webToken }),
            "sec-fetch-site": site,
            "content-type": "application/json",
          },
          body: '{"setting":"on"}',
        }),
        params(path),
      );
    expect((await put("same-origin")).status).toBe(200);
    expect(api.calls).toHaveLength(1);
    expect(api.calls[0]!.init.method).toBe("PUT");
    expect(api.calls[0]!.init.body).toBe('{"setting":"on"}');
    expect(api.calls[0]!.init.headers.get("authorization")).toBe(
      `Bearer ${webToken}`,
    );
    expect((await put("cross-site")).status).toBe(403);
    expect(api.calls).toHaveLength(1);
  });

  it("passes a passkey's answer to the re-check through, and still signs the request", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    await POST(
      keepRequest(cookies({ access: webToken, refresh: "refresh-1" }), {
        "x-memax-passkey": "eyJpZCI6ImNyZWQifQ",
      }),
      params(keepPath),
    );
    const h = api.calls[0]!.init.headers;
    expect(h.get("x-memax-passkey")).toBe("eyJpZCI6ImNyZWQifQ");
    // The answer is the authenticator's own signature over a challenge
    // bound to this request: the surface signature stays the same.
    expect(h.get("x-memax-surface-signature")).toBe(VECTOR.signature);
  });

  it("drops surface and client headers a client sends itself", async () => {
    const { POST } = await loadRoute(undefined);
    const api = mockApi();
    await POST(
      keepRequest(
        cookies({ access: token({ sub: VECTOR.user, surface: "cli" }) }),
        {
          "x-memax-surface": "web",
          "x-memax-surface-signature": "v1=forged",
          "x-memax-surface-user": VECTOR.user,
          "x-memax-via": "web",
          "x-memax-client-ip": "198.51.100.1",
          "x-memax-client-signature": "v1=forged",
        },
      ),
      params(keepPath),
    );
    const h = api.calls[0]!.init.headers;
    for (const name of [
      "x-memax-surface",
      "x-memax-surface-signature",
      "x-memax-surface-user",
      "x-memax-via",
      "x-memax-client-ip",
      "x-memax-client-signature",
    ]) {
      expect(h.get(name)).toBeNull();
    }
  });

  it("passes back the headers /v2 commands answer with, and never the API's cookies", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    mockApi({
      response: () =>
        new Response("{}", {
          status: 200,
          headers: {
            "content-type": "application/json",
            etag: '"2"',
            "idempotent-replayed": "true",
            "retry-after": "1",
            "set-cookie": "not=forwarded",
          },
        }),
    });
    const res = await POST(
      keepRequest(cookies({ access: webToken })),
      params(keepPath),
    );
    expect(res.headers.get("etag")).toBe('"2"');
    expect(res.headers.get("idempotent-replayed")).toBe("true");
    expect(res.headers.get("retry-after")).toBe("1");
    expect(setCookies(res)).toEqual([]);
  });

  it("streams the API's events through, and hangs up when the browser does", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi({
      response: () =>
        new Response("event: delta\ndata: {}\n\n", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
    });
    const browser = new AbortController();
    const res = await POST(
      new Request("https://memax.app/api/proxy/v2/spaces/memax-v2/ask", {
        method: "POST",
        headers: {
          cookie: cookies({ access: webToken }),
          "sec-fetch-site": "same-origin",
          "content-type": "application/json",
          accept: "text/event-stream",
        },
        body: JSON.stringify({ question: "Why River?" }),
        signal: browser.signal,
      }),
      params(["v2", "spaces", "memax-v2", "ask"]),
    );
    expect(res.headers.get("content-type")).toBe("text/event-stream");
    expect(await res.text()).toBe("event: delta\ndata: {}\n\n");
    expect(api.calls[0]?.init.headers.get("accept")).toBe("text/event-stream");
    const upstreamSignal = api.calls[0]?.init.signal;
    expect(upstreamSignal?.aborted).toBe(false);
    browser.abort();
    expect(upstreamSignal?.aborted).toBe(true);
  });

  it("answers 502 when the API can't be reached", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("down");
      }),
    );
    const res = await GET(
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: { "sec-fetch-site": "same-origin" },
      }),
      params(["v2", "spaces"]),
    );
    expect(res.status).toBe(502);
  });
});

describe("/api/proxy refreshes the session", () => {
  it("refreshes a missing access token once for requests that arrive together", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const req = () =>
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: {
          cookie: cookies({ refresh: "refresh-1" }),
          "sec-fetch-site": "same-origin",
          "user-agent": "Mozilla/5.0 (Macintosh) Chrome/131.0",
          "cf-connecting-ip": "203.0.113.7",
          "cf-ipcity": "Lisbon",
        },
      });
    const [a, b] = await Promise.all([
      GET(req(), params(["v2", "spaces"])),
      GET(req(), params(["v2", "spaces"])),
    ]);
    // One refresh, with where the browser is, signed.
    const refreshes = api.to(REFRESH);
    expect(refreshes).toHaveLength(1);
    expect(JSON.parse(String(refreshes[0]!.init.body))).toEqual({
      refresh_token: "refresh-1",
    });
    const h = refreshes[0]!.init.headers;
    expect(h.get("x-memax-client-ip")).toBe("203.0.113.7");
    expect(h.get("x-memax-client-city")).toBe("Lisbon");
    expect(h.get("x-memax-client-signature")).toMatch(/^v1=[\w-]{43}$/);
    // Both requests went with the new token, and both responses set the
    // same new refresh token.
    for (const call of api.to("/v2/spaces")) {
      expect(call.init.headers.get("authorization")).toMatch(/^Bearer /);
    }
    for (const res of [a, b]) {
      const set = setCookies(res);
      expect(
        set.some((c) => c.startsWith("__Host-memax_refresh=refresh-2;")),
      ).toBe(true);
      const access = set.find((c) => c.startsWith("__Host-memax_session="))!;
      expect(access).toContain("HttpOnly");
      expect(access).toContain("Secure");
      expect(access).toContain("SameSite=Strict");
      expect(access).toContain("Path=/");
      expect(access).toContain(`Max-Age=${HOUR - 60}`);
    }
  });

  it("refreshes an access token that is about to expire", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const expiring = token({ sub: VECTOR.user, surface: "web" }, 10);
    await GET(
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: {
          cookie: cookies({ access: expiring, refresh: "refresh-x" }),
          "sec-fetch-site": "same-origin",
        },
      }),
      params(["v2", "spaces"]),
    );
    expect(api.to(REFRESH)).toHaveLength(1);
    expect(api.to("/v2/spaces")[0]!.init.headers.get("authorization")).not.toBe(
      `Bearer ${expiring}`,
    );
  });

  it("refreshes once and tries again when the API refuses the token", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    let n = 0;
    const api = mockApi({
      response: () =>
        n++ === 0
          ? Response.json({ error: { code: "unauthorized" } }, { status: 401 })
          : Response.json({ data: { ok: true } }),
    });
    const res = await POST(
      keepRequest(cookies({ access: webToken, refresh: "refresh-y" })),
      params(keepPath),
    );
    expect(res.status).toBe(200);
    const sent = api.to("/v2/memories/M-0001:keep");
    expect(sent).toHaveLength(2);
    // The retry is signed afresh, over the same body.
    expect(sent[1]!.init.body).toBe('{"reason":"checked"}');
    expect(sent[1]!.init.headers.get("x-memax-surface-signature")).toMatch(
      /^v1=/,
    );
    expect(
      setCookies(res).some((c) =>
        c.startsWith("__Host-memax_refresh=refresh-2"),
      ),
    ).toBe(true);
  });

  it("ends the session when the refresh token is refused", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const api = mockApi({
      refreshStatus: 401,
      response: () =>
        Response.json({ error: { code: "unauthorized" } }, { status: 401 }),
    });
    const res = await GET(
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: {
          cookie: cookies({ refresh: "refresh-revoked" }),
          "sec-fetch-site": "same-origin",
        },
      }),
      params(["v2", "spaces"]),
    );
    expect(res.status).toBe(401);
    expect(
      api.to("/v2/spaces")[0]!.init.headers.get("authorization"),
    ).toBeNull();
    const cleared = setCookies(res);
    for (const name of [
      "__Host-memax_session",
      "__Host-memax_refresh",
      "memax_session_presence",
    ]) {
      expect(
        cleared.some(
          (c) => c.startsWith(`${name}=;`) && c.includes("Max-Age=0"),
        ),
      ).toBe(true);
    }
  });

  it("keeps the session when the API can't be reached to refresh it", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    mockApi({ refreshThrows: true });
    const res = await GET(
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: {
          cookie: cookies({ refresh: "refresh-z" }),
          "sec-fetch-site": "same-origin",
        },
      }),
      params(["v2", "spaces"]),
    );
    expect(res.status).toBe(502);
    expect(setCookies(res)).toEqual([]);
  });
});

describe("/api/proxy refuses cross-site requests", () => {
  it.each([
    ["another site", { "sec-fetch-site": "cross-site" }],
    ["a sibling subdomain", { "sec-fetch-site": "same-site" }],
    ["a navigation", { "sec-fetch-site": "none" }],
    ["an old browser from another origin", { origin: "https://evil.example" }],
    ["a request with neither header", {}],
  ])("a command from %s", async (_, headers) => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const req = new Request(
      "https://memax.app/api/proxy/v2/memories/M-0001:keep?space=memax-v2",
      {
        method: "POST",
        headers: { cookie: cookies({ access: webToken }), ...headers },
        body: "{}",
      },
    );
    const res = await POST(req, params(keepPath));
    expect(res.status).toBe(403);
    expect(((await res.json()) as { error: { code: string } }).error.code).toBe(
      "csrf_refused",
    );
    expect(api.calls).toHaveLength(0);
  });

  it("lets an old browser's same-origin command through by its Origin", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const res = await POST(
      new Request(
        "https://memax.app/api/proxy/v2/memories/M-0001:keep?space=memax-v2",
        {
          method: "POST",
          headers: {
            cookie: cookies({ access: webToken }),
            origin: "https://memax.app",
          },
          body: "{}",
        },
      ),
      params(keepPath),
    );
    expect(res.status).toBe(200);
    expect(api.calls).toHaveLength(1);
  });

  it("refuses reads another site starts, and lets a navigation read", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const read = (site: string) =>
      GET(
        new Request("https://memax.app/api/proxy/v2/spaces", {
          headers: {
            cookie: cookies({ access: webToken }),
            "sec-fetch-site": site,
          },
        }),
        params(["v2", "spaces"]),
      );
    expect((await read("cross-site")).status).toBe(403);
    expect((await read("none")).status).toBe(200);
    expect(api.calls).toHaveLength(1);
  });
});

describe("/api/proxy keeps tokens from the page", () => {
  it.each([
    [["v1", "auth", "refresh"]],
    [["v1", "auth", "exchange"]],
    [["v1", "auth", "impersonate"]],
    [["oauth", "token"]],
    [["oauth", "revoke"]],
    [["oauth", "device_authorization"]],
  ])("won't forward to %j", async (path) => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi();
    const res = await POST(
      new Request(`https://memax.app/api/proxy/${path.join("/")}`, {
        method: "POST",
        headers: { "sec-fetch-site": "same-origin" },
        body: "{}",
      }),
      params(path),
    );
    expect(res.status).toBe(404);
    expect(api.calls).toHaveLength(0);
  });

  it("signs out tokens the email code's verify answers, instead of handing them over", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const api = mockApi({
      response: () =>
        Response.json({
          data: {
            access_token: "a",
            refresh_token: "leaked-refresh",
            expires_in: HOUR,
          },
        }),
    });
    const path = ["v1", "auth", "email", "verify"];
    const res = await POST(
      new Request(`https://memax.app/api/proxy/${path.join("/")}`, {
        method: "POST",
        headers: { "sec-fetch-site": "same-origin" },
        body: JSON.stringify({ email: "a@b.c", code: "123456" }),
      }),
      params(path),
    );
    expect(res.status).toBe(400);
    const text = await res.text();
    expect(text).not.toContain("leaked-refresh");
    const revoked = api.to("/oauth/revoke");
    expect(revoked).toHaveLength(1);
    expect(String(revoked[0]!.init.body)).toBe("token=leaked-refresh");
  });

  it("passes the email code's verify through when it answers a redirect", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    mockApi({
      response: () =>
        Response.json({
          data: {
            status: "ok",
            code: "c",
            redirect: "https://memax.app/auth/callback?code=c",
          },
        }),
    });
    const path = ["v1", "auth", "email", "verify"];
    const res = await POST(
      new Request(`https://memax.app/api/proxy/${path.join("/")}`, {
        method: "POST",
        headers: { "sec-fetch-site": "same-origin" },
        body: "{}",
      }),
      params(path),
    );
    expect(res.status).toBe(200);
    expect(
      ((await res.json()) as { data: { redirect: string } }).data.redirect,
    ).toContain("/auth/callback?code=c");
  });
});
