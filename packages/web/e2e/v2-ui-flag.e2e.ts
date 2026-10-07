import { randomUUID } from "node:crypto";
import type { Browser, Page } from "@playwright/test";
import { cookieValue, expect, skipWithoutBrowser, test } from "./fixtures";
import { startStack, type Stack } from "./stack";

// The per-person V2 UI flag end to end, on the real API (plan 25 E1,
// internal/v2ui): no dev route anywhere. A person with a space on the V2
// record signs in and lands in V2, whichever sign-in page they used; a V1
// person lands in V1 and V2's pages stay shut to them; an operator turns
// V2 on from the admin panel and the person's next load of a V2 page is
// V2, and turning it off sends them back.
//
// Needs the API (Go, psql, a Postgres role that can CREATE DATABASE) and a
// web build pointed at it, as auth-bff.e2e.ts does:
//
//   E2E_STACK=1 pnpm --filter @memaxlabs/web test:e2e --project=chromium v2-ui-flag

skipWithoutBrowser();

const enabled = process.env.E2E_STACK === "1";
test.skip(
  !enabled,
  "Set E2E_STACK=1 to run the V2 UI flag against the real API (see the header).",
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

/** A V1 person, as V1 makes them: an account and a personal hub. */
function v1Person(name: string): string {
  const id = randomUUID();
  const hub = randomUUID();
  stack.query(
    `INSERT INTO users (id, email, name) VALUES ('${id}', '${name}-${id.slice(0, 8)}@v2-ui.test', '${name}')`,
  );
  stack.query(
    `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ('${hub}', 'Personal', 'personal-${id.slice(0, 8)}', 'personal', '${id}', 'personal')`,
  );
  stack.query(
    `INSERT INTO hub_members (hub_id, user_id, role) VALUES ('${hub}', '${id}', 'owner')`,
  );
  return id;
}

/** Where V1 starts for a signed-in browser: /home, then a hub. */
const V1_HOME = /^\/(home|h\/.+)$/;

async function settledOn(page: Page, path: RegExp) {
  await page.waitForURL((url) => path.test(url.pathname), {
    timeout: 30_000,
  });
}

/** A browser signed in on V1's own page, as an operator. */
async function operatorPage(browser: Browser, baseURL: string) {
  const operator = v1Person("operator");
  stack.query(
    `INSERT INTO admin_roles (user_id, role) VALUES ('${operator}', 'super_admin')`,
  );
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  await page.goto(`/auth/callback?code=${stack.webCode(operator)}`);
  await settledOn(page, V1_HOME);
  return page;
}

test("a person with a space on V2 signs in and lands in V2", async ({
  page,
  browser,
  baseURL,
}) => {
  test.setTimeout(120_000);
  // devseed's memax-v2 is on the V2 record, so ZZ sees V2 (rule a). The
  // browser has no hint until the sign-in sets it.
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();
  await page.goto(`/signin/callback?code=${stack.webCode()}`);
  await settledOn(page, /^\/(memax-v2\/(today|review)|setup\/import)$/);
  expect(await cookieValue(page, "memax_ui")).toBe("v2");
  // V2's pages open from now on.
  await page.goto("/memax-v2/today");
  await expect(page).toHaveURL(/\/memax-v2\/today$/);

  // Signing in on V1's own page sets the hint as well, so a V2 link
  // works next.
  const v1Side = await browser.newContext({ baseURL });
  const other = await v1Side.newPage();
  await other.goto(`/auth/callback?code=${stack.webCode()}`);
  await settledOn(other, V1_HOME);
  expect(await cookieValue(other, "memax_ui")).toBe("v2");
  await other.goto("/memax-v2/brief");
  await expect(other).toHaveURL(/\/memax-v2\/brief$/);
  await v1Side.close();

  // Signing out clears it: the next person on this browser starts from
  // their own flag.
  await page.evaluate(() => fetch("/api/auth/logout", { method: "POST" }));
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();
  // Signed out, a V2 link asks to sign in and comes back after.
  await page.goto("/memax-v2/today");
  await expect(page).toHaveURL(
    `/signin?${new URLSearchParams({ next: "/memax-v2/today" })}`,
  );
});

test("a V1 person lands in V1, and V2's pages stay shut", async ({ page }) => {
  test.setTimeout(120_000);
  const ada = v1Person("ada");
  // A V2 page asked for on the way in doesn't open for them.
  await page.goto(
    `/signin/callback?code=${stack.webCode(ada)}&next=/setup/import`,
  );
  await settledOn(page, V1_HOME);
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();
  // A V2 link: the proxy reads the flag once more on the sign-in page,
  // which sends them on to V1's home.
  await page.goto("/setup/import");
  await settledOn(page, V1_HOME);
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();
  // The device sign-in still opens for them.
  await page.goto("/device?code=WQRT-4821");
  await expect(page).toHaveURL("/device?code=WQRT-4821");
});

test("an operator turns V2 on, and the person's next load is V2; off sends them back", async ({
  page,
  browser,
  baseURL,
}) => {
  test.setTimeout(180_000);
  const bo = v1Person("bo");
  await page.goto(`/signin/callback?code=${stack.webCode(bo)}`);
  await settledOn(page, V1_HOME);
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();

  // The operator finds them in the admin panel and turns V2 on.
  const ops = await operatorPage(browser, baseURL!);
  await ops.goto(`/admin/users/${bo}`);
  const card = ops.getByRole("region", { name: "V2 UI" });
  await expect(card).toContainText("They have no space on V2.", {
    timeout: 30_000,
  });
  await card.getByRole("button", { name: "Turn on" }).click();
  await expect(card).toContainText("An operator turned it on.");
  expect(
    stack.query(
      `SELECT v2_ui::text || ' ' || (SELECT count(*) FROM admin_audit WHERE resource_id = '${bo}' AND action = 'v2_ui') FROM users WHERE id = '${bo}'`,
    ),
  ).toBe("true 1");

  // Their next load of a V2 page is V2: the sign-in page reads the flag
  // (which sets the hint) and goes on, with nothing to sign in again.
  await page.goto("/setup/import");
  await settledOn(page, /^\/setup\/import$/);
  expect(await cookieValue(page, "memax_ui")).toBe("v2");
  await page.goto("/settings/account");
  await expect(page).toHaveURL("/settings/account");

  // Off wins over every rule: the next profile read clears the hint, and
  // V2's pages send them back to V1.
  await card.getByRole("button", { name: "Turn off" }).click();
  await expect(card).toContainText(
    "An operator turned it off. This wins over every rule.",
  );
  await page.reload();
  await expect
    .poll(() => cookieValue(page, "memax_ui"), { timeout: 20_000 })
    .toBeUndefined();
  await page.goto("/setup/import");
  await settledOn(page, V1_HOME);
  await ops.context().close();
});
