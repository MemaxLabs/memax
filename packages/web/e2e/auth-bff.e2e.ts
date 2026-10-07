import { createHmac } from "node:crypto";
import type { Page } from "@playwright/test";
import { expect, optIntoV2, skipWithoutBrowser, test } from "./fixtures";
import { startStack, type Stack } from "./stack";

// The web session end to end, on the real API (plan 25 §5.15, the BFF):
// sign in through the web app's callback, and the session is in HttpOnly
// cookies page script can't read; Review's Keep goes through /api/proxy
// with the cookie's token and the proxy's signature, so the API records it
// as a person on the web (human_web), while the same person's CLI login is
// client-attested; the proxy refreshes and rotates the session, refuses
// requests that don't come from our pages, and signing out ends the
// session on the API.
//
// Needs the API (Go, psql, a Postgres role that can CREATE DATABASE) and a
// web build pointed at it:
//
//   E2E_STACK=1 pnpm --filter @memaxlabs/web test:e2e --project=chromium auth-bff
//
// Under workerd, build with the same settings and run against preview:cf:
//
//   NEXT_PUBLIC_DEV_FIXTURES=1 NEXT_PUBLIC_API_URL=http://127.0.0.1:18080 \
//     NEXT_PUBLIC_APP_URL=http://localhost:8790 pnpm --filter @memaxlabs/web build:cf
//   pnpm --filter @memaxlabs/web preview:cf --port 8790 --ip 127.0.0.1 \
//     --var WEB_SURFACE_SECRET:e2e-web-surface-secret-0123456789abcdef
//   E2E_STACK=1 E2E_APP_URL=http://localhost:8790 E2E_BASE_URL=http://localhost:8790 \
//     pnpm --filter @memaxlabs/web test:e2e --project=chromium auth-bff

skipWithoutBrowser();

const enabled = process.env.E2E_STACK === "1";
test.skip(
  !enabled,
  "Set E2E_STACK=1 to run the web session against the real API (see the header).",
);
test.describe.configure({ mode: "serial" });

let stack: Stack;

test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack();
});

test.afterAll(async () => {
  await stack?.stop();
});

const SESSION = /^(__Host-)?memax_session$/;
const REFRESH = /^(__Host-)?memax_refresh$/;

async function sessionCookies(page: Page) {
  const jar = await page.context().cookies();
  return {
    access: jar.find((c) => SESSION.test(c.name)),
    refresh: jar.find((c) => REFRESH.test(c.name)),
  };
}

/** A CLI login's access token, as the API signs them. */
function cliToken(): string {
  const b64 = (v: unknown) =>
    Buffer.from(JSON.stringify(v)).toString("base64url");
  const now = Math.floor(Date.now() / 1000);
  const body = `${b64({ alg: "HS256", typ: "JWT" })}.${b64({ sub: stack.zz, exp: now + 3600, iat: now, surface: "cli" })}`;
  const sig = createHmac("sha256", stack.jwtSecret)
    .update(body)
    .digest("base64url");
  return `${body}.${sig}`;
}

/** Remember a decision in the team space memax-v2, from the page. */
async function rememberFromPage(page: Page, statement: string) {
  return page.evaluate(async (statement) => {
    const r = await fetch("/api/proxy/v2/spaces/memax-v2/memories", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
      },
      body: JSON.stringify({
        statement,
        section: "decisions",
        kind: "decision",
      }),
    });
    return {
      status: r.status,
      body: (await r.json()) as {
        data?: {
          memory: { ref: string; state: string };
          receipts: { assurance: string; via: string }[];
        };
      },
    };
  }, statement);
}

test("the web session: cookies only the server reads, human_web Keeps, rotation, CSRF, sign-out", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(180_000);
  await optIntoV2(page, baseURL);

  // Sign in: the code the API redirected to the web app, traded by the
  // web app's server.
  await page.goto(
    `/signin/callback?code=${stack.webCode()}&next=/memax-v2/review`,
  );
  await page.waitForURL((url) => url.pathname === "/memax-v2/review", {
    timeout: 30_000,
  });

  // The session is in HttpOnly, SameSite=Strict cookies; page script sees
  // neither them nor any token.
  const { access, refresh } = await sessionCookies(page);
  expect(access?.httpOnly).toBe(true);
  expect(access?.sameSite).toBe("Strict");
  expect(refresh?.httpOnly).toBe(true);
  expect(refresh?.sameSite).toBe("Strict");
  const visible = await page.evaluate(() => ({
    cookie: document.cookie,
    storage: JSON.stringify({ ...localStorage, ...sessionStorage }),
  }));
  expect(visible.cookie).not.toMatch(
    /(^|;\s*)(__Host-)?memax_(session|refresh)=/,
  );
  expect(visible.cookie).not.toContain(access!.value);
  expect(visible.storage).not.toContain(access!.value);
  expect(visible.storage).not.toContain(refresh!.value);
  expect(visible.storage).not.toMatch(/eyJ[\w-]+\.eyJ/); // no JWT anywhere
  expect(
    stack.query(
      `SELECT kind FROM sessions WHERE user_id = '${stack.zz}' AND revoked_at IS NULL`,
    ),
  ).toBe("web");

  // Keep from Review: through the proxy, with the cookie's token and the
  // proxy's signature, so the receipt says a person on the web.
  await expect(page.getByText("M-0430").first()).toBeVisible({
    timeout: 30_000,
  });
  await page.locator("#main").focus();
  await page.keyboard.press("k");
  await expect
    .poll(
      () =>
        stack.query(
          `SELECT r.assurance || ' ' || r.via FROM v2.receipts r WHERE r.object_ref = 'M-0430' AND r.action = 'kept'`,
        ),
      { timeout: 20_000 },
    )
    .toBe("human_web web");

  // A decision remembered from the page, through the proxy: human_web.
  const fromWeb = await rememberFromPage(
    page,
    "Sessions rotate their refresh tokens on every use.",
  );
  expect(fromWeb.status).toBe(201);
  expect(fromWeb.body.data?.memory.state).toBe("kept");
  expect(fromWeb.body.data?.receipts.at(-1)?.assurance).toBe("human_web");
  // The same person's CLI login, straight at the API: client-attested.
  const fromCLI = await fetch(`${stack.apiUrl}/v2/spaces/memax-v2/memories`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${cliToken()}`,
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
    },
    body: JSON.stringify({
      statement: "The CLI decides this alone.",
      section: "decisions",
      kind: "decision",
    }),
  });
  const cliBody = (await fromCLI.json()) as {
    data: { receipts: { assurance: string; via: string }[] };
  };
  expect(cliBody.data.receipts.at(-1)?.assurance).toBe("client_attested");

  // Rotation: with the access cookie gone, the next proxied call refreshes
  // the session; the refresh cookie changes and the old token is retired.
  await page.context().clearCookies({ name: access!.name });
  const read = await page.evaluate(
    async () => (await fetch("/api/proxy/v2/spaces")).status,
  );
  expect(read).toBe(200);
  const rotated = await sessionCookies(page);
  expect(rotated.access?.value).toBeTruthy();
  expect(rotated.refresh?.value).not.toBe(refresh!.value);
  expect(
    Number(
      stack.query(
        `SELECT generation FROM sessions WHERE user_id = '${stack.zz}' AND revoked_at IS NULL`,
      ),
    ),
  ).toBeGreaterThanOrEqual(1);
  // The database has the token's hash only, never the token.
  expect(
    stack.query(
      `SELECT count(*) FROM sessions WHERE refresh_token IS NOT NULL`,
    ),
  ).toBe("0");
  expect(
    stack.query(
      `SELECT count(*) FROM sessions WHERE refresh_token_hash = encode(sha256('${rotated.refresh!.value}'::bytea), 'hex')`,
    ),
  ).toBe("1");

  // Switch to V2 (Today's Switch) on the rotated cookie session: a V1
  // space of ZZ's switches through the proxy.
  const v1Space = `acme-${Date.now()}`;
  stack.query(
    `WITH h AS (INSERT INTO hubs (name, slug, hub_type, owner_id, space_kind) VALUES ('${v1Space}', '${v1Space}', 'team', '${stack.zz}', 'team') RETURNING id)
     INSERT INTO hub_members (hub_id, user_id, role) SELECT id, '${stack.zz}', 'owner' FROM h RETURNING hub_id`,
  );
  const switched = await page.evaluate(async (slug) => {
    const r = await fetch(`/api/proxy/v2/spaces/${slug}:switch`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
      },
      body: JSON.stringify({
        to: "v2",
        kind: "project",
        repository: "acme/web",
      }),
    });
    return {
      status: r.status,
      body: (await r.json()) as { data?: { state: string } },
    };
  }, v1Space);
  expect(switched.status).toBe(200);
  expect(switched.body.data?.state).toBe("switched");
  expect(
    stack.query(
      `SELECT v2_enabled_at IS NOT NULL FROM hubs WHERE slug = '${v1Space}'`,
    ),
  ).toBe("t");

  // CSRF: a request with the cookies that didn't come from our pages (no
  // Origin, no Sec-Fetch-Site) is refused, and so is a form another site
  // submits (Strict cookies don't go, and Sec-Fetch-Site says cross-site).
  const forged = await page.request.post(
    "/api/proxy/v2/spaces/memax-v2/memories",
    {
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": "forged-1",
      },
      data: { statement: "Forged.", section: "decisions" },
    },
  );
  expect(forged.status()).toBe(403);
  expect(
    ((await forged.json()) as { error: { code: string } }).error.code,
  ).toBe("csrf_refused");
  const other = await page.context().newPage();
  const target = new URL(baseURL!);
  const otherSite = new URL(baseURL!);
  otherSite.hostname =
    target.hostname === "localhost" ? "127.0.0.1" : "localhost";
  await other.goto(`${otherSite.origin}/`);
  await other.evaluate((action) => {
    const form = document.createElement("form");
    form.method = "POST";
    form.action = action;
    document.body.appendChild(form);
    form.submit();
  }, `${target.origin}/api/auth/logout`);
  await other.waitForLoadState();
  await other.close();
  expect(
    stack.query(
      `SELECT count(*) FROM sessions WHERE user_id = '${stack.zz}' AND revoked_at IS NULL`,
    ),
  ).toBe("1");
  expect((await sessionCookies(page)).refresh?.value).toBe(
    rotated.refresh!.value,
  );

  // Sign out: the session ends on the API and every cookie goes.
  const out = await page.evaluate(async () =>
    (await fetch("/api/auth/logout", { method: "POST" })).json(),
  );
  expect(out).toEqual({ data: { signed_out: true, revoked: true } });
  const after = await sessionCookies(page);
  expect(after.access).toBeUndefined();
  expect(after.refresh).toBeUndefined();
  expect(
    stack.query(
      `SELECT revoked_reason FROM sessions WHERE user_id = '${stack.zz}' AND kind = 'web'`,
    ),
  ).toBe("signed_out");
  const me = await page.evaluate(
    async () => (await fetch("/api/auth/me")).status,
  );
  expect(me).toBe(401);
});
