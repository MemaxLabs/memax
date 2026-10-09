import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// The V2 app frame on the demo dataset (no session, dev fixtures on):
// the rail, the places, the switcher, the phone bars, settings, the
// not-found states, and self-baselined screenshots of the empty states.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: import("@playwright/test").Page) =>
  page.getByRole("navigation", { name: "Main" });

test("the rail lists the six places in order, with real links", async ({
  page,
}) => {
  await page.goto("/memax-v2/today");
  const links = rail(page).getByRole("list").getByRole("link");
  await expect(links).toHaveText([
    "Today",
    /^Review/,
    "Briefs",
    "Memories",
    /^Handoffs/,
    "Agents",
  ]);
  await expect(links.nth(0)).toHaveAttribute("aria-current", "page");
  await expect(links.nth(1)).toHaveAttribute("href", "/memax-v2/review");
  await expect(links.nth(2)).toHaveAttribute("href", "/memax-v2/brief");
  // Review's ochre count and Handoffs' plain one, each read as words.
  await expect(links.nth(1)).toHaveAccessibleName("Review 5 waiting on you");
  await expect(links.nth(4)).toHaveAccessibleName("Handoffs 1 open handoff");
  await expect(rail(page).getByRole("status")).toHaveText(
    "Cursor file drifted",
  );
  await links.nth(3).click();
  await expect(page).toHaveURL("/memax-v2/memories");
  await expect(links.nth(3)).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Memories");
});

test("a team space adds Decisions after Memories", async ({ page }) => {
  await page.goto("/memax-team/decisions");
  const links = rail(page).getByRole("list").getByRole("link");
  await expect(links).toHaveText([
    "Today",
    /^Review/,
    "Briefs",
    "Memories",
    "Decisions",
    /^Handoffs/,
    "Agents",
  ]);
  await expect(links.nth(4)).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Decisions");
});

// Targeted accessibility checks in place of axe (not a dependency here):
// the landmarks, one h1, and a name on every visible control.
test("pages have their landmarks and every control a name", async ({
  page,
}) => {
  for (const path of [
    "/memax-v2/today",
    "/memax-web/review",
    "/memax-v2/memories",
    "/memax-web/agents",
    "/settings/account",
    "/settings/notifications",
    "/settings/security",
  ]) {
    await page.goto(path);
    await expect(page.getByRole("main")).toHaveCount(1);
    await expect(rail(page)).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Skip to content" }),
    ).toHaveCount(1);
    await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
    for (const role of ["button", "link", "radio"] as const) {
      for (const control of await page.getByRole(role).all()) {
        if (!(await control.isVisible())) continue;
        await expect(control, `${path} ${role}`).toHaveAccessibleName(/\S/);
      }
    }
  }
});

test("a bare space opens on its Today", async ({ page }) => {
  await page.goto("/memax-v2");
  await expect(page).toHaveURL("/memax-v2/today");
});

test("the switcher lists spaces and switches to the same place", async ({
  page,
}) => {
  await page.goto("/memax-v2/memories");
  await page.getByRole("button", { name: "memax-v2, Switch space" }).click();
  const menu = page.getByRole("menu");
  await expect(menu.getByRole("menuitem")).toHaveText([
    /^Personal/,
    /^memax-v2/,
    /^memax-web/,
    /^Memax team/,
    // Still on V1: its Today offers the switch (switch.e2e.ts).
    /^acme-web/,
    "New space",
    "Join with an invite link",
  ]);
  await menu.getByRole("menuitem", { name: /^memax-web/ }).click();
  await expect(page).toHaveURL("/memax-web/memories");

  // By keyboard: letters are the menu's (no G T), ⌘4 switches and closes.
  await expect(menu).toBeHidden();
  const trigger = page.getByRole("button", { name: "memax-web, Switch space" });
  await trigger.focus();
  await page.keyboard.press("Enter");
  await expect(menu).toBeVisible();
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect(page).toHaveURL("/memax-web/memories");
  await page.keyboard.press("ControlOrMeta+4");
  await expect(page).toHaveURL("/memax-team/memories");
  await expect(menu).toBeHidden();
});

test("pages say what's missing, inside the frame", async ({ page }) => {
  await page.goto("/typo/today");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "There's no space called typo that you can open.",
  );
  await expect(rail(page)).toBeVisible();

  await page.goto("/memax-v2/decisions");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "There's no page at this address.",
  );
  await expect(rail(page)).toBeVisible();

  // A reserved word is never a space.
  await page.goto("/home/today");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "There's no page at this address.",
  );
  await expect(rail(page)).toHaveCount(0);
});

test("below 1024px the rail keeps its names behind icons", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 900 });
  await page.goto("/memax-v2/today");
  const link = rail(page).getByRole("link", { name: /^Review/ });
  await expect(link).toBeVisible();
  expect((await link.boundingBox())?.width).toBeLessThanOrEqual(40);
  await expect(
    rail(page).getByRole("button", { name: "Ask or remember" }),
  ).toBeVisible();
});

test("on a phone, a bottom bar replaces the rail", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-v2/today");
  await expect(rail(page)).toBeHidden();
  const bar = page.getByRole("navigation", { name: "Places" });
  await expect(bar.getByRole("link")).toHaveText([
    "Today",
    /^Review/,
    "Agents",
  ]);
  await bar.getByRole("button", { name: "Ask" }).click();
  await expect(
    page.getByRole("dialog", { name: "Ask or remember" }),
  ).toBeVisible();
});

test("settings switch Paper, Carbon and System", async ({ page }) => {
  await page.goto("/settings/account");
  await expect(
    rail(page).getByRole("link", { name: "Settings" }),
  ).toHaveAttribute("aria-current", "page");
  const theme = page.getByRole("radiogroup", { name: "Theme" });
  await theme.getByRole("radio", { name: "Carbon" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(theme.getByRole("radio", { name: "Carbon" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  // Arrow keys move through the group (roving focus).
  await theme.getByRole("radio", { name: "Carbon" }).focus();
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

// Self-baselines: the empty states of the demo's new project space.
const SHOTS = [
  // EmptySpace.png: a new space shows its three steps to a first compile.
  { place: "today", heading: "Nothing here yet" },
  { place: "review", heading: "Review" },
  { place: "memories", heading: "Memories" },
] as const;

for (const { place, heading } of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    for (const width of [1440, 390] as const) {
      test(`${place} empty state, ${theme}, ${width}`, async ({
        page,
        baseURL,
      }) => {
        test.skip(usingDevServer, "screenshots need the production build");
        await useTheme(page, baseURL, theme);
        await page.setViewportSize({
          width,
          height: width === 390 ? 844 : 1000,
        });
        await page.goto(`/memax-web/${place}`);
        await expect(page.getByRole("heading", { level: 1 })).toHaveText(
          heading,
        );
        await settle(page);
        await expect(page).toHaveScreenshot(
          `shell-${place}-${theme}-${width}.png`,
          { maxDiffPixelRatio: 0.002 },
        );
      });
    }
  }
}
