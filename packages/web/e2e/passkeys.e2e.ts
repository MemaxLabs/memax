import { createHmac } from "node:crypto";
import type { CDPSession, Page } from "@playwright/test";
import { expect, optIntoV2, skipWithoutBrowser, test } from "./fixtures";
import { startStack, type Stack } from "./stack";

// Passkeys end to end, on the real API and Chromium's virtual
// authenticator (plan 25 §5.15): add one in Settings › Account right after
// signing in, sign out, sign in with it, then Keep a team decision (D15)
// in Review: the API asks for the passkey again (403 needs_passkey with a
// challenge bound to that Keep), the page asks, and the same request goes
// again with the answer, so the receipt says human_web_verified.
//
// Needs the API (Go, psql, a Postgres role that can CREATE DATABASE) and a
// web build pointed at it, as auth-bff.e2e.ts does:
//
//   E2E_STACK=1 pnpm --filter @memaxlabs/web test:e2e --project=chromium passkeys
//
// Under workerd, build with the same settings and run against preview:cf
// (the API's APP_BASE_URL, and so the passkeys' origin, follows
// E2E_APP_URL):
//
//   NEXT_PUBLIC_DEV_FIXTURES=1 NEXT_PUBLIC_API_URL=http://127.0.0.1:18080 \
//     NEXT_PUBLIC_APP_URL=http://localhost:8790 pnpm --filter @memaxlabs/web build:cf
//   pnpm --filter @memaxlabs/web preview:cf --port 8790 --ip 127.0.0.1 \
//     --var WEB_SURFACE_SECRET:e2e-web-surface-secret-0123456789abcdef
//   E2E_STACK=1 E2E_APP_URL=http://localhost:8790 E2E_BASE_URL=http://localhost:8790 \
//     pnpm --filter @memaxlabs/web test:e2e --project=chromium passkeys

skipWithoutBrowser();

const enabled = process.env.E2E_STACK === "1";
test.skip(
  !enabled,
  "Set E2E_STACK=1 to run passkeys against the real API (see the header).",
);
test.describe.configure({ mode: "serial" });

let stack: Stack;
const TEAM = `acme-${Date.now()}`;

test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack();
});

test.afterAll(async () => {
  await stack?.stop();
});

/** A platform authenticator that holds discoverable credentials and verifies its user. */
async function virtualAuthenticator(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send(
    "WebAuthn.addVirtualAuthenticator",
    {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    },
  );
  return { cdp, authenticatorId };
}

async function credentials(cdp: CDPSession, authenticatorId: string) {
  const { credentials } = await cdp.send("WebAuthn.getCredentials", {
    authenticatorId,
  });
  return credentials;
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

test("add a passkey, sign in with it, and confirm a Keep with it", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(240_000);
  await optIntoV2(page, baseURL);
  const { cdp, authenticatorId } = await virtualAuthenticator(page);
  const toasts = page.getByRole("region", { name: "Notifications" });

  // Signed in a moment ago, so adding a way in needs nothing more.
  await page.goto(
    `/signin/callback?code=${stack.webCode()}&next=/settings/account`,
  );
  await page.waitForURL((url) => url.pathname === "/settings/account", {
    timeout: 30_000,
  });

  // A team space of ZZ's on the V2 record, switched from the page before
  // any passkey (the seeded memax-v2 is a project).
  stack.query(
    `WITH h AS (INSERT INTO hubs (name, slug, hub_type, owner_id, space_kind) VALUES ('${TEAM}', '${TEAM}', 'team', '${stack.zz}', 'team') RETURNING id)
     INSERT INTO hub_members (hub_id, user_id, role) SELECT id, '${stack.zz}', 'owner' FROM h RETURNING hub_id`,
  );
  const switched = await page.evaluate(async (slug) => {
    const r = await fetch(`/api/proxy/v2/spaces/${slug}:switch`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
      },
      body: JSON.stringify({ to: "v2", kind: "team" }),
    });
    return r.status;
  }, TEAM);
  expect(switched).toBe(200);

  const signIn = page.getByRole("region", { name: "Sign in with" });
  await signIn.getByRole("button", { name: "Add a passkey" }).click();
  await expect(toasts).toContainText(
    "Passkey added. Decisions that need you will ask for it.",
    { timeout: 20_000 },
  );
  await expect(signIn.getByRole("button", { name: "Manage" })).toBeVisible();
  const held = await credentials(cdp, authenticatorId);
  expect(held).toHaveLength(1);
  expect(held[0]!.isResidentCredential).toBe(true);
  expect(
    stack.query(
      `SELECT count(*) || ' ' || bool_and(created_session_id IS NOT NULL) FROM v2.passkeys WHERE person_id = '${stack.zz}'`,
    ),
  ).toBe("1 true");

  // Sign out, then back in with the passkey alone: no email, no provider.
  const out = await page.evaluate(async () =>
    (await fetch("/api/auth/logout", { method: "POST" })).json(),
  );
  expect(out).toMatchObject({ data: { signed_out: true } });
  await page.goto(`/signin?next=${encodeURIComponent("/settings/account")}`);
  await page.getByRole("button", { name: "Use a passkey" }).click();
  await page.waitForURL((url) => url.pathname === "/settings/account", {
    timeout: 30_000,
  });
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Account");
  expect(
    stack.query(
      `SELECT (last_used_at IS NOT NULL) || ' ' || sign_count FROM v2.passkeys WHERE person_id = '${stack.zz}'`,
    ),
  ).toMatch(/^true \d+$/);
  expect(
    stack.query(
      `SELECT count(*) FROM sessions WHERE user_id = '${stack.zz}' AND kind = 'web' AND revoked_at IS NULL`,
    ),
  ).toBe("1");

  // A decision for the team space, sent from the person's CLI: D15 makes
  // it a proposal, since only the person on the web may keep it.
  const statement = "Ask for the passkey before every team decision.";
  const proposed = await fetch(`${stack.apiUrl}/v2/spaces/${TEAM}/memories`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${cliToken()}`,
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
    },
    body: JSON.stringify({ statement, section: "decisions", kind: "decision" }),
  });
  const body = (await proposed.json()) as {
    data: { memory: { ref: string; state: string } };
  };
  expect(proposed.status).toBe(201);
  expect(body.data.memory.state).toBe("proposed");
  const ref = body.data.memory.ref;

  // Keep it from Review: the API asks for the passkey, the page asks,
  // the authenticator answers, and the Keep goes again with the answer.
  await page.goto(`/${TEAM}/review`);
  await page.getByText(statement).first().click({ timeout: 30_000 });
  await page.locator("#main").focus();
  await page.keyboard.press("k");
  const check = page.getByRole("dialog", { name: "Confirm it's you" });
  await expect(check).toBeVisible({ timeout: 20_000 });
  await check.getByRole("button", { name: "Use passkey" }).click();
  await expect(check).toBeHidden({ timeout: 20_000 });
  await expect
    .poll(
      () =>
        stack.query(
          `SELECT r.assurance || ' ' || r.via FROM v2.receipts r JOIN hubs h ON h.id = r.space_id
            WHERE h.slug = '${TEAM}' AND r.object_ref = '${ref}' AND r.action = 'kept'`,
        ),
      { timeout: 20_000 },
    )
    .toBe("human_web_verified web");
  // One challenge, used once, for this Keep alone.
  expect(
    stack.query(
      `SELECT count(*) FILTER (WHERE used_at IS NOT NULL) || '/' || count(*) FROM v2.passkey_challenges WHERE purpose = 'check' AND person_id = '${stack.zz}'`,
    ),
  ).toBe("1/1");
});
