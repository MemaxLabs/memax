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

// VerifyStale (VerifyStale.png) on the demo dataset, by keyboard: V on
// the stale row in Memories, 1–3 choose, ↵ answers; the answer leaves
// Review, Memories shows it kept, and ⌘Z puts the flag back. Opened from
// Review's flag card and a memory's page too, and self-baselined in both
// themes.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });
const layer = (page: Page) =>
  page.getByRole("dialog", { name: "Verify M-0187" });
const staleRow = (page: Page) =>
  page
    .locator(".mx-row", { hasText: "Ask memax answers with the Haiku tier." })
    .first();

async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

async function verifyFromMemories(page: Page) {
  await open(page, "/memax-v2/memories");
  await staleRow(page).locator(".mx-row-link").focus();
  await page.keyboard.press("v");
  await expect(layer(page)).toBeVisible();
}

test("Verify by keyboard: still true, then Undo", async ({ page }) => {
  await verifyFromMemories(page);
  // The suggestion is chosen; 1 chooses still true, and ↵ answers.
  await expect(
    layer(page).getByRole("radio", { name: /Update it/ }),
  ).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("1");
  await expect(
    layer(page).getByRole("radio", { name: /Still true, keep it/ }),
  ).toHaveAttribute("aria-checked", "true");
  await expect(
    layer(page).getByRole("button", { name: /^Keep it/ }),
  ).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(layer(page)).toBeHidden();
  await expect(toasts(page)).toContainText(
    "Verified M-0187. It's kept as it is.",
  );
  await expect(
    staleRow(page).getByText("Its source changed on Sep 11"),
  ).toBeHidden();
  await expect(
    rail(page).getByRole("link", { name: /^Review/ }),
  ).toHaveAccessibleName("Review 4 waiting on you");

  await page.keyboard.press("ControlOrMeta+z");
  await expect(toasts(page)).toContainText(
    "Undid the verification. M-0187 is stale again.",
  );
  await expect(
    staleRow(page).getByText(/Its source changed on Sep 11/),
  ).toBeVisible();
});

test("Verify keeps the suggested update, and Esc cancels", async ({ page }) => {
  await verifyFromMemories(page);
  await page.keyboard.press("Escape");
  await expect(layer(page)).toBeHidden();
  await staleRow(page).locator(".mx-row-link").focus();
  await page.keyboard.press("v");
  await page.keyboard.press("Enter");
  await expect(toasts(page)).toContainText("Updated M-0187 and verified it.");
  await expect(
    page.getByText("Ask memax answers with the Sonnet tier.").first(),
  ).toBeVisible();
});

test("Verify from Review's flag card and from the memory's page", async ({
  page,
}) => {
  await open(page, "/memax-v2/review");
  for (let i = 0; i < 4; i++) await page.keyboard.press("ArrowDown");
  await expect(page.getByText("memax-v2 · 5 of 5")).toBeVisible();
  await page.keyboard.press("v");
  await expect(layer(page)).toBeVisible();
  await page.keyboard.press("Escape");

  await open(page, "/memax-v2/memories/M-0187");
  await page.keyboard.press("v");
  await expect(layer(page)).toBeVisible();
  await page.keyboard.press("3");
  await expect(
    layer(page).getByRole("button", { name: /^Forget it/ }),
  ).toBeVisible();
});

for (const theme of ["light", "dark"] as const) {
  test(`VerifyStale looks like the board (${theme})`, async ({
    page,
    baseURL,
  }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1100 });
    await verifyFromMemories(page);
    await expect(layer(page).getByText(/Removed from 3 files/)).toBeVisible();
    await settle(page);
    await expect(page).toHaveScreenshot(`verify-stale-${theme}.png`, {
      fullPage: false,
    });
  });
}
