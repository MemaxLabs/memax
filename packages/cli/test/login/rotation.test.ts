// The CLI's credentials as refresh tokens rotate (internal/sessions on the
// server): every refresh answers the session's next refresh token and
// retires the one sent, so the CLI must store the new one; processes
// sharing ~/.memax/credentials.json that refresh together all end up with
// the same next token; `memax logout` signs the session out on the server;
// `memax sessions` lists where you are signed in. Against a small fake of
// the server's rotation, grace window and reuse detection.
import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

// The CLI reads ~/.memax at import, so HOME is set first.
const home = mkdtempSync(join(tmpdir(), "memax-rotation-"));
process.env.HOME = home;
delete process.env.MEMAX_API_KEY;
const credFile = join(home, ".memax", "credentials.json");

/** The server: one session whose refresh token rotates. */
class FakeSessions {
  current = "r0";
  /** Retired token → [its successor, when it was retired]. */
  retired = new Map<string, [string, number]>();
  revoked = false;
  n = 0;
  reuse = 0;
  userAgents: string[] = [];
  revokedWith: string[] = [];
  spaceAuth: string[] = [];
  graceMs = 60_000;

  refresh(token: string): { status: number; body: unknown } {
    if (this.revoked) return unauthorized("session_revoked");
    if (token === this.current) {
      const next = `r${++this.n}`;
      this.retired.set(token, [next, Date.now()]);
      this.current = next;
      return this.pair(next);
    }
    const ret = this.retired.get(token);
    if (!ret) return unauthorized("invalid_token");
    if (Date.now() - ret[1] <= this.graceMs) return this.pair(this.current);
    this.revoked = true;
    this.reuse++;
    return unauthorized("session_revoked");
  }

  private pair(refresh: string) {
    return {
      status: 200,
      body: {
        data: {
          access_token: `a-${refresh}`,
          refresh_token: refresh,
          expires_in: 3600,
          refresh_expires_in: 86_400,
        },
      },
    };
  }
}

function unauthorized(code: string) {
  return {
    status: 401,
    body: { error: { code, message: "Sign in again." } },
  };
}

const sessions: V2.Session[] = [
  {
    id: "0199b0f3-2c4e-7000-8000-000000000001",
    surface: "cli",
    client: "memax CLI 0.2.1",
    address: "203.0.113.7",
    city: "Lisbon",
    signed_in_at: "2026-10-06T09:00:00Z",
    last_used_at: "2026-10-07T09:00:00Z",
    expires_at: "2026-11-05T09:00:00Z",
    current: true,
  },
  {
    id: "0199b0f3-2c4e-7000-8000-000000000002",
    surface: "web",
    client: "Chrome on macOS",
    signed_in_at: "2026-10-06T09:00:00Z",
    last_used_at: "2026-10-07T08:00:00Z",
    expires_at: "2026-11-05T09:00:00Z",
    current: false,
  },
];

const PERSONAL = {
  id: "0199b0f3-2c4e-7000-8000-0000000000aa",
  tenant_id: "0199b0f3-2c4e-7000-8000-0000000000bb",
  slug: "personal",
  name: "Personal",
  kind: "personal",
  role: "owner",
};

/** The switch's answers: a V1 space, then switched. */
function switchAnswer(
  method: string,
  url: string,
  raw: string,
): { status: number; body: unknown } {
  const path = url.split("?")[0];
  if (method === "GET" && path === "/v2/spaces") {
    return { status: 200, body: { data: { items: [PERSONAL] } } };
  }
  if (method === "GET" && path === `/v2/spaces/${PERSONAL.id}/switch`) {
    return {
      status: 200,
      body: { data: { space: PERSONAL, state: "v1", step: "space" } },
    };
  }
  if (method === "POST" && path === `/v2/spaces/${PERSONAL.id}:switch`) {
    const to = (JSON.parse(raw) as { to?: string }).to;
    return {
      status: 200,
      body: {
        data: {
          space: { ...PERSONAL, v2_enabled_at: "2026-10-07T09:00:00Z" },
          state: to === "v2" ? "switched" : "off",
          step: "done",
        },
      },
    };
  }
  return { status: 404, body: { error: { code: "not_found" } } };
}

let fake: FakeSessions;
let server: Server;

async function body(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  return Buffer.concat(chunks).toString("utf8");
}

beforeAll(async () => {
  server = createServer((req, res) => {
    void (async () => {
      fake.userAgents.push(String(req.headers["user-agent"] ?? ""));
      const raw = await body(req);
      let out: { status: number; body?: unknown };
      if (req.url === "/v1/auth/refresh") {
        out = fake.refresh(
          (JSON.parse(raw) as { refresh_token: string }).refresh_token,
        );
      } else if (req.url === "/oauth/revoke") {
        fake.revokedWith.push(new URLSearchParams(raw).get("token") ?? "");
        fake.revoked = true;
        out = { status: 200 };
      } else if (req.url === "/v2/sessions") {
        out = { status: 200, body: { data: { items: sessions } } };
      } else if (req.url?.startsWith("/v2/spaces")) {
        // memax switch: every call must carry the current access token.
        fake.spaceAuth.push(String(req.headers.authorization ?? ""));
        out = switchAnswer(req.method ?? "GET", req.url, raw);
      } else {
        out = { status: 404, body: { error: { code: "not_found" } } };
      }
      res.writeHead(out.status, { "Content-Type": "application/json" });
      res.end(out.body === undefined ? "" : JSON.stringify(out.body));
    })();
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  process.env.MEMAX_API_URL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  await new Promise((r) => server.close(r));
});

beforeEach(async () => {
  fake = new FakeSessions();
  mkdirSync(join(home, ".memax"), { recursive: true });
  writeExpired("r0");
  const { resetClient } = await import("../../src/lib/client.js");
  resetClient();
});

/** Credentials whose access token has expired. */
function writeExpired(refresh: string) {
  writeFileSync(
    credFile,
    JSON.stringify({
      access_token: "stale",
      refresh_token: refresh,
      expires_at: Date.now() - 1000,
    }),
  );
}

function stored(): {
  access_token: string;
  refresh_token: string;
  expires_at: number;
} {
  return JSON.parse(readFileSync(credFile, "utf8"));
}

function expire() {
  writeFileSync(
    credFile,
    JSON.stringify({ ...stored(), expires_at: Date.now() - 1000 }),
  );
}

describe("refresh-token rotation in the CLI", () => {
  it("stores each rotated refresh token and presents it next time", async () => {
    const { getAuthHeaders } = await import("../../src/lib/client.js");
    expect(await getAuthHeaders()).toEqual({ Authorization: "Bearer a-r1" });
    expect(stored().refresh_token).toBe("r1");
    expect(stored().expires_at).toBeGreaterThan(Date.now());

    expire();
    expect(await getAuthHeaders()).toEqual({ Authorization: "Bearer a-r2" });
    expect(stored().refresh_token).toBe("r2");
    // Never presented a retired token, so the session never tripped.
    expect(fake.reuse).toBe(0);
    expect(fake.userAgents.every((ua) => ua.startsWith("memax-cli/"))).toBe(
      true,
    );
  });

  it("processes refreshing together end up with the same token", async () => {
    const { getAuthHeaders } = await import("../../src/lib/client.js");
    // The daemon and two MCP servers read the same expired file at once.
    const got = await Promise.all([
      getAuthHeaders(),
      getAuthHeaders(),
      getAuthHeaders(),
    ]);
    expect(new Set(got.map((h) => h.Authorization))).toEqual(
      new Set(["Bearer a-r1"]),
    );
    expect(stored().refresh_token).toBe("r1");
    expect(fake.n).toBe(1);
    // An hour later the shared token still works: nobody holds a stale one.
    expire();
    expect(await getAuthHeaders()).toEqual({ Authorization: "Bearer a-r2" });
    expect(fake.reuse).toBe(0);
  });

  it("a signed-out session falls back to the stale token, which the API refuses", async () => {
    fake.revoked = true;
    const { getAuthHeaders } = await import("../../src/lib/client.js");
    expect(await getAuthHeaders()).toEqual({ Authorization: "Bearer stale" });
    expect(stored().refresh_token).toBe("r0");
  });
});

describe("memax switch on rotated credentials", () => {
  it("refreshes first, stores the next token, and switches with the new access token", async () => {
    const { getClient } = await import("../../src/lib/client.js");
    const { runSwitch } = await import("../../src/lib/switch/run.js");
    const { daemonPaths } = await import("../../src/lib/daemon/paths.js");
    const report = await runSwitch(
      { space: "personal", yes: true },
      { memax: getClient(), paths: daemonPaths(), cwd: home, confirm: null },
    );
    expect(report.outcome).toBe("switched");
    expect(stored().refresh_token).toBe("r1");
    // The list, the status and the switch all went with the rotated token.
    expect(fake.spaceAuth.length).toBeGreaterThanOrEqual(3);
    expect(new Set(fake.spaceAuth)).toEqual(new Set(["Bearer a-r1"]));
    expect(fake.reuse).toBe(0);
  });
});

describe("memax logout", () => {
  it("signs the session out on the server and clears the credentials", async () => {
    const { logoutCommand } = await import("../../src/commands/login.js");
    const logs: string[] = [];
    const log = console.log;
    console.log = (...a: unknown[]) => logs.push(a.join(" "));
    try {
      await logoutCommand();
    } finally {
      console.log = log;
    }
    expect(fake.revokedWith).toEqual(["r0"]);
    expect(stored()).toEqual({});
    expect(logs.join("\n")).toContain("signed out");
  });
});

describe("memax sessions", () => {
  it("lists where you are signed in, this session first", async () => {
    const { renderSessions, sessionLines } =
      await import("../../src/commands/sessions.js");
    const { getClient } = await import("../../src/lib/client.js");
    writeFileSync(
      credFile,
      JSON.stringify({
        access_token: "a",
        refresh_token: "r0",
        expires_at: Date.now() + 3_600_000,
      }),
    );
    const { items } = await getClient().v2.sessions.list();
    const text = renderSessions(items, new Date("2026-10-07T12:00:00Z")).join(
      "\n",
    );
    expect(text).toContain("memax CLI 0.2.1");
    expect(text).toContain("this one · 203.0.113.7, Lisbon");
    expect(text).toContain("Chrome on macOS");
    expect(text.indexOf("memax CLI")).toBeLessThan(text.indexOf("Chrome"));
    expect(sessionLines(items)[0].split("\t").slice(1, 4)).toEqual([
      "cli",
      "memax CLI 0.2.1",
      "current",
    ]);
  });
});
