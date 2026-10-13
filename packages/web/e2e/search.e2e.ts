import type { Page } from "@playwright/test";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// Search across spaces (Search.png) on the demo dataset: the matches by
// kind, the facets, ↓ from the field into the matches, Ask instead (⌘↵),
// and the not-found pages' way into it. Self-baselined in both themes
// and on a phone; spaces-boards.e2e.ts measures it against its board.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const field = (page: Page) =>
  page.getByRole("searchbox", { name: "Search everything" });

async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
}

test("Search finds across spaces, filters, and asks instead", async ({
  page,
}) => {
  await open(page, "/memax-v2/search?q=deploy");
  const meta = page
    .getByRole("main")
    .getByRole("status")
    .filter({ hasText: "results" });
  await expect(meta).toHaveText(
    "8 results for “deploy” in three spaces · 9 raw notes hidden",
  );
  // Search lights no place in the rail.
  await expect(rail(page).locator('[aria-current="page"]')).toHaveCount(0);

  // ↓ from the field goes to the first match, ↓ again to the next.
  await field(page).focus();
  await page.keyboard.press("ArrowDown");
  await expect(
    page.getByRole("link", { name: /Deploy the v2 API to Fly.io/ }),
  ).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(
    page.getByRole("link", { name: /Deploy the v2 API to Railway/ }),
  ).toBeFocused();

  // Raw notes on, then Memax team off.
  const filters = page.getByRole("complementary", { name: "Filters" });
  await filters.getByRole("checkbox", { name: /Raw notes/ }).check();
  await expect(page.getByRole("region", { name: "Raw notes" })).toBeVisible();
  await expect(meta).toHaveText("17 results for “deploy” in three spaces");
  await filters.getByRole("checkbox", { name: /Memax team/ }).uncheck();
  await expect(page.getByRole("region", { name: "Decisions" })).toHaveCount(0);

  // Typing settles into the address; ⌘↵ asks the words instead.
  await field(page).fill("railway previews");
  await expect(page).toHaveURL("/memax-v2/search?q=railway%20previews");
  await field(page).focus();
  await page.keyboard.press("ControlOrMeta+Enter");
  const command = page.getByRole("dialog", { name: "Ask or remember" });
  await expect(command).toBeVisible();
  await expect(command.getByRole("textbox", { name: "Ask" })).toHaveValue(
    "railway previews",
  );
});

test("a memory that isn't there offers Search", async ({ page }) => {
  await open(page, "/memax-v2/memories/M-9999");
  await page.getByRole("link", { name: "Search memories" }).click();
  await expect(page).toHaveURL("/memax-v2/search");
  await expect(field(page)).toBeFocused();
});

for (const theme of ["light", "dark"] as const) {
  test(`Search looks like the board (${theme})`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1100 });
    await open(page, "/memax-v2/search?q=deploy");
    await expect(page.getByRole("region", { name: "Memories" })).toBeVisible();
    await page.mouse.move(1300, 1050);
    await settle(page);
    await expect(page).toHaveScreenshot(`search-${theme}.png`, {
      animations: "disabled",
      caret: "hide",
    });
  });
}

test("Search on a phone", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  // A phone has the top bar, not the rail and its status line.
  await page.goto("/memax-v2/search?q=deploy");
  await expect(page.getByRole("region", { name: "Memories" })).toBeVisible();
  // The rows stack once the page knows it's narrow.
  await expect(page.locator(".mx-row.is-stacked").first()).toBeVisible();
  await settle(page);
  await expect(page).toHaveScreenshot("search-light-390.png", {
    animations: "disabled",
    caret: "hide",
  });
});
