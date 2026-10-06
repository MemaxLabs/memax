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

function jwt(claims: Record<string, unknown>): string {
  const part = (v: unknown) =>
    Buffer.from(JSON.stringify(v)).toString("base64url");
  return `${part({ alg: "HS256", typ: "JWT" })}.${part(claims)}.not-checked-here`;
}

const webToken = jwt({ sub: VECTOR.user, surface: "web" });

type Fetched = { url: string; init: RequestInit & { headers: Headers } };

async function loadRoute(secret: string | undefined) {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "https://api.memax.app");
  if (secret === undefined) vi.stubEnv("WEB_SURFACE_SECRET", "");
  else vi.stubEnv("WEB_SURFACE_SECRET", secret);
  return import("./route");
}

function mockUpstream(response?: Response) {
  const calls: Fetched[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: Fetched["init"]) => {
      calls.push({ url, init });
      return (
        response ??
        new Response(JSON.stringify({ data: {} }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
    }),
  );
  return calls;
}

function params(path: string[]) {
  return { params: Promise.resolve({ path }) };
}

function keepRequest(token: string, extra: Record<string, string> = {}) {
  return new Request(
    "https://memax.app/api/proxy/v2/memories/M-0001:keep?space=memax-v2",
    {
      method: "POST",
      headers: {
        authorization: `Bearer ${token}`,
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

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(VECTOR.timestamp * 1000);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("/api/proxy web-surface signing", () => {
  it("signs a web session's /v2 command exactly as the API verifies it", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    const calls = mockUpstream();
    await POST(keepRequest(webToken), params(keepPath));

    expect(calls).toHaveLength(1);
    const { url, init } = calls[0]!;
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
  });

  it("signs reads too, over an empty body", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const calls = mockUpstream();
    await GET(
      new Request("https://memax.app/api/proxy/v2/spaces", {
        headers: { authorization: `Bearer ${webToken}` },
      }),
      params(["v2", "spaces"]),
    );
    const h = calls[0]!.init.headers;
    expect(h.get("x-memax-surface-signature")).toMatch(/^v1=[\w-]{43}$/);
    expect(calls[0]!.init.body).toBeUndefined();
  });

  it.each([
    ["a CLI session", jwt({ sub: VECTOR.user, surface: "cli" })],
    ["a session from before surfaces", jwt({ sub: VECTOR.user })],
    [
      "an impersonation session",
      jwt({ sub: VECTOR.user, impersonator_id: "x" }),
    ],
    ["an API key", "mxk_0123456789abcdef"],
    ["a token that isn't a JWT", "not.a-jwt"],
    ["a web token without a user", jwt({ surface: "web" })],
  ])("doesn't sign %s", async (_, token) => {
    const { POST } = await loadRoute(VECTOR.secret);
    const calls = mockUpstream();
    await POST(keepRequest(token), params(keepPath));
    const h = calls[0]!.init.headers;
    expect(h.get("x-memax-surface")).toBeNull();
    expect(h.get("x-memax-surface-signature")).toBeNull();
    // The command still goes through, unsigned (client-attested).
    expect(h.get("idempotency-key")).toBe("key-1");
  });

  it("doesn't sign without WEB_SURFACE_SECRET", async () => {
    const { POST } = await loadRoute(undefined);
    const calls = mockUpstream();
    await POST(keepRequest(webToken), params(keepPath));
    expect(calls[0]!.init.headers.get("x-memax-surface-signature")).toBeNull();
  });

  it("doesn't sign V1 paths, which don't read it", async () => {
    const { GET } = await loadRoute(VECTOR.secret);
    const calls = mockUpstream();
    const v1Path = ["v1", "memories"];
    await GET(
      new Request(`https://memax.app/api/proxy/${v1Path.join("/")}`, {
        headers: { authorization: `Bearer ${webToken}` },
      }),
      params(v1Path),
    );
    expect(calls[0]!.init.headers.get("x-memax-surface")).toBeNull();
  });

  it("drops surface headers a client sends itself", async () => {
    const { POST } = await loadRoute(undefined);
    const calls = mockUpstream();
    await POST(
      keepRequest(jwt({ sub: VECTOR.user, surface: "cli" }), {
        "x-memax-surface": "web",
        "x-memax-surface-signature": "v1=forged",
        "x-memax-surface-user": VECTOR.user,
        "x-memax-via": "web",
      }),
      params(keepPath),
    );
    const h = calls[0]!.init.headers;
    for (const name of [
      "x-memax-surface",
      "x-memax-surface-signature",
      "x-memax-surface-user",
      "x-memax-via",
    ]) {
      expect(h.get(name)).toBeNull();
    }
  });

  it("passes back the headers /v2 commands answer with", async () => {
    const { POST } = await loadRoute(VECTOR.secret);
    mockUpstream(
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
    );
    const res = await POST(keepRequest(webToken), params(keepPath));
    expect(res.headers.get("etag")).toBe('"2"');
    expect(res.headers.get("idempotent-replayed")).toBe("true");
    expect(res.headers.get("retry-after")).toBe("1");
    expect(res.headers.get("set-cookie")).toBeNull();
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
      new Request("https://memax.app/api/proxy/v2/spaces"),
      params(["v2", "spaces"]),
    );
    expect(res.status).toBe(502);
  });
});
