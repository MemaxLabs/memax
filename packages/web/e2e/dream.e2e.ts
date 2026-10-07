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

// Dream's edition (DreamEdition.png) on the demo dataset: opened from
// Today's card, an Undo and Restore all, C to compare the conflict it
// found, and Settings' Dream panel. Then self-baselined screenshots; the
// board itself is compared in dream-boards.e2e.ts.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });

async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

test("Today's card opens the edition, which lights Today in the rail", async ({
  page,
}) => {
  await open(page, "/memax-v2/today");
  await page.getByRole("link", { name: "Read edition No. 214" }).click();
  await expect(page).toHaveURL("/memax-v2/dream/214");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Monday, October 5, overnight",
  );
  await expect(rail(page).getByRole("link", { name: /Today/ })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("undo a fold, restore what faded, and compare the conflict with C", async ({
  page,
}) => {
  await open(page, "/memax-v2/dream");
  const folded = page.getByRole("region", {
    name: /^Folded into what you kept/,
  });
  const first = folded.getByRole("listitem").first();
  await first.hover();
  await first.getByRole("button", { name: "Undo" }).click();
  await expect(toasts(page)).toContainText(
    "Undid it. M-0219 is as it was before Dream.",
  );
  await expect(folded.getByRole("listitem")).toHaveCount(1);

  await page
    .getByRole("region", { name: /^Faded/ })
    .getByRole("button", { name: "Restore all" })
    .click();
  await expect(toasts(page)).toContainText("Restored 11 memories.");
  await expect(page.getByRole("region", { name: /^Faded/ })).toHaveCount(0);

  await page.locator("#main").focus();
  await page.keyboard.press("c");
  await expect(page).toHaveURL("/memax-v2/review/M-0431/compare");
});

test("Dream settings: the morning email and the zone", async ({ page }) => {
  await open(page, "/memax-v2/dream/214");
  await page.getByRole("link", { name: "Dream settings" }).click();
  await expect(page).toHaveURL("/settings/account#dream");
  const panel = page.getByRole("region", { name: "Dream" });
  await panel.getByRole("radio", { name: "Off" }).click();
  await expect(toasts(page)).toContainText("Dream settings changed.");
  await expect(panel.getByRole("radio", { name: "Off" })).toBeChecked();
});

const SHOTS = [
  { theme: "light", width: 1440, height: 1400 },
  { theme: "dark", width: 1440, height: 1400 },
  { theme: "light", width: 390, height: 844 },
] as const;

for (const shot of SHOTS) {
  test(`the edition, ${shot.theme}, ${shot.width}`, async ({
    page,
    baseURL,
  }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, shot.theme);
    await page.setViewportSize({ width: shot.width, height: shot.height });
    // A phone has no rail to wait on: the heading says the page is up.
    await page.goto("/memax-v2/dream/214");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Monday, October 5, overnight",
    );
    await settle(page);
    await expect(page).toHaveScreenshot(
      `dream-${shot.theme}-${shot.width}.png`,
      { maxDiffPixelRatio: 0.002 },
    );
  });
}
