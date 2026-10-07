import type { Page, Request } from "@playwright/test";
import {
  expect,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// OAuthConsent in a browser, with the API stubbed at the browser: a person
// without a session is sent to sign in and back; signed in, the request is
// read and answered through /api/proxy as them, and the page follows the
// URL the API answers (the agent's redirect_uri); Cancel's access_denied;
// "Not you?"; V1's retired page sends every browser on; and self-baselines
// of the board's request (the dev fixtures' demo) in Paper and Carbon.

skipWithoutBrowser();

// The agent's redirect_uri, on this web server: Playwright doesn't route
// the request a page navigates to after a fulfilled call, so the "agent"
// is a file the web server serves.
const CALLBACK = "/favicon.svg";

const space = {
  role: "owner",
  disabled: false,
  people: 1,
} as const;

const request = {
  request_id: "req_e2e",
  client_name: "Codex",
  agent_name: "codex",
  scope: "memax:read memax:write",
  expires_at: "2026-10-07T12:00:00Z",
  expires_in: 600,
  person: { name: "ZZ" },
  spaces: [
    {
      ...space,
      id: "11111111-1111-4111-8111-111111111111",
      name: "Personal",
      slug: "personal",
      kind: "personal",
      on_v2: false,
      memories: 12,
      can: ["read_memories", "add", "gate"],
      cannot: ["forget", "other_spaces"],
    },
    {
      ...space,
      id: "22222222-2222-4222-8222-222222222222",
      name: "memax-v2",
      slug: "memax-v2",
      kind: "project",
      on_v2: true,
      memories: 3,
      targets: [{ kind: "agents_md", path: "AGENTS.md" }],
      autonomy: "propose",
      ceiling: "write",
      can: ["read_brief", "propose", "gate"],
      cannot: ["keep", "forget", "other_spaces"],
    },
  ],
};

const me = {
  user: {
    id: "0192a7c0-0000-7000-8000-0000000000aa",
    email: "zz@example.com",
    name: "ZZ",
    display_name: "ZZ",
    avatar_url: "",
  },
  hubs: [],
  session: { surface: "web", impersonating: false },
};

/**
 * Stubs the API at the browser: who is signed in (or nobody), and the
 * consent request's three calls through the web app's proxy. Returns the
 * calls it saw.
 */
async function stubAPI(
  page: Page,
  baseURL: string | undefined,
  { signedIn = true }: { signedIn?: boolean } = {},
) {
  const calls: Request[] = [];
  await page.route("**/api/auth/me", (route) =>
    signedIn
      ? route.fulfill({ json: { data: me } })
      : route.fulfill({
          status: 401,
          json: { error: { code: "unauthorized", message: "Sign in." } },
        }),
  );
  await page.route("**/api/auth/logout", (route) =>
    route.fulfill({ json: { data: { signed_out: true, revoked: true } } }),
  );
  await page.route("**/api/proxy/oauth/authorize/requests/**", (route) => {
    const req = route.request();
    calls.push(req);
    if (req.method() === "GET") {
      return route.fulfill({ json: { data: request } });
    }
    if (req.url().endsWith("/release")) {
      return route.fulfill({ json: { data: { released: true } } });
    }
    const body = JSON.parse(req.postData() ?? "{}") as { decision?: string };
    const back = new URL(CALLBACK, baseURL);
    back.searchParams.set("state", "s1");
    back.searchParams.set(
      body.decision === "deny" ? "error" : "code",
      body.decision === "deny" ? "access_denied" : "code_e2e",
    );
    return route.fulfill({ json: { data: { redirect_to: back.toString() } } });
  });
  return calls;
}

const PAGE = `/oauth/authorize?request=${request.request_id}`;

test("a person signs in first, and comes back to the request", async ({
  page,
  baseURL,
}) => {
  await stubAPI(page, baseURL, { signedIn: false });
  await page.goto(PAGE);
  await expect(page).toHaveURL(`/signin?next=${encodeURIComponent(PAGE)}`);
});

test("a person allows the agent one space, and the browser goes back to it", async ({
  page,
  baseURL,
}) => {
  const calls = await stubAPI(page, baseURL);
  await page.goto(PAGE);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Codex wants to connect to Memax",
  );
  await expect(page.getByText("Signed in as ZZ")).toBeVisible();
  // The space on V2 comes first and is chosen; it says what's true there.
  await expect(page.getByRole("radio", { name: /memax-v2/ })).toBeChecked();
  await expect(page.getByText("compiles AGENTS.md")).toBeVisible();
  await expect(
    page.getByRole("list", { name: "Codex will be able to" }),
  ).toContainText("Propose memories, which wait for you");
  // Personal, still on V1, says what V1 does.
  await page.getByRole("radio", { name: /Personal/ }).check();
  await expect(
    page.getByRole("list", { name: "Codex will be able to" }),
  ).toContainText("Add memories, which are kept as written");
  await page.getByRole("radio", { name: /memax-v2/ }).check();
  await page.getByRole("button", { name: "Allow Codex" }).click();

  await expect(page).toHaveURL(
    new URL(`${CALLBACK}?state=s1&code=code_e2e`, baseURL).toString(),
  );
  const decision = calls.find((c) => c.url().endsWith("/decision"))!;
  expect(JSON.parse(decision.postData() ?? "{}")).toEqual({
    decision: "approve",
    space_id: "22222222-2222-4222-8222-222222222222",
  });
  // Same-origin, through the web app's proxy, which refuses anything else.
  expect((await decision.allHeaders())["sec-fetch-site"]).toBe("same-origin");
});

test("Cancel answers the agent access_denied", async ({ page, baseURL }) => {
  const calls = await stubAPI(page, baseURL);
  await page.goto(PAGE);
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page).toHaveURL(/error=access_denied/);
  const decision = calls.find((c) => c.url().endsWith("/decision"))!;
  expect(JSON.parse(decision.postData() ?? "{}")).toEqual({ decision: "deny" });
});

test("Not you? lets go of the request and signs in again for it", async ({
  page,
  baseURL,
}) => {
  const calls = await stubAPI(page, baseURL);
  await page.goto(PAGE);
  await page.getByRole("button", { name: "Not you?" }).click();
  await expect(page).toHaveURL(/\/signin\?next=/);
  expect(new URL(page.url()).searchParams.get("next")).toBe(PAGE);
  expect(calls.some((c) => c.url().endsWith("/release"))).toBe(true);
});

test("V1's retired consent page sends every browser here with its request", async ({
  page,
  baseURL,
}) => {
  await stubAPI(page, baseURL);
  await page.goto(`/oauth/consent?request_id=${request.request_id}`);
  await expect(page).toHaveURL(
    `/oauth/authorize?request_id=${request.request_id}`,
  );
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Codex wants to connect to Memax",
  );
});

test("a link with no request says so", async ({ page }) => {
  await page.goto("/oauth/authorize");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "This link has no request in it.",
  );
});

for (const theme of ["light", "dark"] as const) {
  test(`OAuthConsent, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 940 });
    await page.goto("/oauth/authorize?request=demo");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Codex wants to connect to Memax",
    );
    await settle(page);
    await expect(page).toHaveScreenshot(`oauth-consent-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}
