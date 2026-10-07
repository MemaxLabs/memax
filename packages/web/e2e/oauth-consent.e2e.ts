import type { Page, Request } from "@playwright/test";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// OAuthConsent in a browser: the request read through /api/proxy (the
// API stubbed at the browser), the forms posted to the API and the
// redirect followed back to the agent, Cancel's access_denied, the route
// from V1's page for an opted-in browser, and self-baselines of the
// board's request (the dev fixtures' demo) in Paper and Carbon.

skipWithoutBrowser();

const API = "https://api.memax.test";
// The agent's redirect_uri, on this web server (see stubAPI).
const CALLBACK = "/favicon.svg";

const request = {
  session_id: "req_e2e",
  csrf_token: "tok_e2e",
  client_name: "Codex",
  agent_name: "codex",
  resource: `${API}/mcp`,
  submit_url: `${API}/oauth/authorize/consent`,
  expires_at: "2026-10-07T12:00:00Z",
  hubs: [
    {
      id: "11111111-1111-4111-8111-111111111111",
      name: "Personal",
      slug: "personal",
      role: "owner",
      hub_type: "personal",
      memory_count: 12,
      checked: true,
      disabled: false,
      capability_label: "",
      supported_permissions: [],
      space_kind: "personal",
      on_v2: false,
      people_count: 1,
      can: ["read_memories", "add", "gate"],
      cannot: ["forget", "other_spaces"],
    },
    {
      id: "22222222-2222-4222-8222-222222222222",
      name: "memax-v2",
      slug: "memax-v2",
      role: "owner",
      hub_type: "team",
      memory_count: 0,
      checked: true,
      disabled: false,
      capability_label: "",
      supported_permissions: [],
      space_kind: "project",
      on_v2: true,
      people_count: 1,
      kept_count: 3,
      targets: [{ kind: "agents_md", path: "AGENTS.md" }],
      autonomy: "propose",
      can: ["read_brief", "propose", "gate"],
      cannot: ["keep", "forget", "other_spaces"],
    },
  ],
  permissions: [],
  not_requested: [],
  person: { name: "ZZ" },
  consent_scope: "memax:read memax:propose",
  expires_in: 600,
};

/**
 * Stubs the API at the browser: the request through the web app's proxy
 * and the consent post, answered as the API does, with a redirect to the
 * agent's callback. Playwright doesn't route the request a fulfilled
 * redirect leads to, so the "agent" is a file this web server serves.
 * Returns the posts it saw.
 */
async function stubAPI(page: Page, baseURL: string | undefined) {
  const posts: Request[] = [];
  await page.route("**/api/proxy/oauth/authorize/consent-request**", (route) =>
    route.fulfill({ json: { data: request } }),
  );
  await page.route(`${API}/oauth/authorize/consent`, (route) => {
    const req = route.request();
    posts.push(req);
    const form = new URLSearchParams(req.postData() ?? "");
    const back = new URL(CALLBACK, baseURL);
    back.searchParams.set("state", "s1");
    if (form.get("decision") === "deny") {
      back.searchParams.set("error", "access_denied");
    } else {
      back.searchParams.set("code", "code_e2e");
    }
    return route.fulfill({
      status: 303,
      headers: { location: back.toString() },
    });
  });
  return posts;
}

const PAGE = `/oauth/authorize?request_id=${request.session_id}&consent_token=${request.csrf_token}`;

test("a person allows the agent one space, and the browser goes back to it", async ({
  page,
  baseURL,
}) => {
  const posts = await stubAPI(page, baseURL);
  await page.goto(PAGE);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Codex wants to connect to Memax",
  );
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
  expect(new URL(page.url()).searchParams.get("code")).toBe("code_e2e");
  expect(posts).toHaveLength(1);
  const form = new URLSearchParams(posts[0]!.postData() ?? "");
  expect([...form.entries()]).toEqual([
    ["session_id", "req_e2e"],
    ["csrf_token", "tok_e2e"],
    ["ui", "v2"],
    ["permission", "memax:read"],
    ["permission", "memax:propose"],
    ["hub_id", "22222222-2222-4222-8222-222222222222"],
    ["decision", "approve"],
  ]);
  // The API's Fetch Metadata check reads where the post came from.
  const headers = await posts[0]!.allHeaders();
  expect(headers.origin).toBe(new URL(baseURL!).origin);
});

test("Cancel answers the agent access_denied", async ({ page, baseURL }) => {
  const posts = await stubAPI(page, baseURL);
  await page.goto(PAGE);
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page).toHaveURL(/error=access_denied/);
  const form = new URLSearchParams(posts[0]!.postData() ?? "");
  expect(form.get("decision")).toBe("deny");
  expect(form.get("csrf_token")).toBe("tok_e2e");
  expect(form.getAll("hub_id")).toEqual([]);
});

test("V1's consent page sends an opted-in browser here with its request", async ({
  page,
  baseURL,
}) => {
  await stubAPI(page, baseURL);
  await optIntoV2(page, baseURL);
  await page.goto(
    `/oauth/consent?request_id=${request.session_id}&consent_token=${request.csrf_token}`,
  );
  await expect(page).toHaveURL(PAGE);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Codex wants to connect to Memax",
  );
});

test("an ended request says so", async ({ page }) => {
  await page.goto("/oauth/authorize?ended=expired");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "This request expired.",
  );
});

for (const theme of ["light", "dark"] as const) {
  test(`OAuthConsent, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 940 });
    await page.goto("/oauth/authorize?request_id=demo&consent_token=demo");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Codex wants to connect to Memax",
    );
    await settle(page);
    await expect(page).toHaveScreenshot(`oauth-consent-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}
