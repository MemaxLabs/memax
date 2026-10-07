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

// Switch to V2 (epic 2.8) on the demo's space still on V1, acme-web:
// Today says what moves; the owner switches it, the switch runs in the
// background and lands in Review's "From V1", where the person's own V1
// memories are kept in one go. Self-baselines of both screens in Paper
// and Carbon (the handoff has no board for them).

skipWithoutBrowser();

const IMPORT = "0192a7c0-0000-7000-8000-0000000001b1";
const FROM_V1 = "You wrote these 7 in V1. Keep them in one go?";

async function openSwitch(page: Page) {
  await page.goto("/acme-web/today");
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "acme-web is still on V1",
  );
}

/** Switches acme-web and waits for Review's "From V1" (the demo forgets on reload). */
async function switchIt(page: Page) {
  await openSwitch(page);
  await page.getByRole("button", { name: "Switch to V2" }).click();
  await expect(page).toHaveURL(
    `/acme-web/review?filter=import&import=${IMPORT}`,
    { timeout: 10_000 },
  );
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(FROM_V1);
}

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

test("a space on V1 switches, and what you wrote is kept in one go", async ({
  page,
}) => {
  await openSwitch(page);
  const moves = page.getByRole("region", { name: "What moves" });
  await expect(moves).toContainText(
    "112 V1 memories become notes (N-): searchable, never compiled. Nothing is lost.",
  );
  await expect(moves).toContainText(
    "Claude Code at Propose and Cursor at Read connect, and each is told on its next response.",
  );
  // The switcher lists it with the spaces on V2.
  await page.getByRole("button", { name: "acme-web, Switch space" }).click();
  await expect(
    page.getByRole("menu").getByRole("menuitem", { name: /^acme-web/ }),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  await page
    .getByRole("radiogroup", { name: "Switch as" })
    .getByRole("radio", { name: "Project" })
    .click();
  await page.getByRole("button", { name: "Switch to V2" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Switching acme-web." }),
  ).toBeVisible();
  await expect(page).toHaveURL(
    `/acme-web/review?filter=import&import=${IMPORT}`,
    { timeout: 10_000 },
  );
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(FROM_V1);
  await expect(page.getByText("N-0001")).toBeVisible();

  await page
    .getByRole("checkbox", { name: "Select the 6 that can be kept in bulk" })
    .check();
  await page.keyboard.press("k");
  await expect(page.getByText("Kept 6. Every file recompiles.")).toBeVisible();
  await expect(
    page.getByRole("checkbox", { name: "Can't be kept in bulk" }),
  ).toHaveCount(1);
});

for (const theme of ["light", "dark"] as const) {
  test(`switch to V2, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 960 });
    await openSwitch(page);
    await settle(page);
    await expect(page).toHaveScreenshot(`switch-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });

  test(`review from V1, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 960 });
    await switchIt(page);
    // The toast fades on its own; wait it out so it isn't captured.
    await expect(page.getByText("acme-web is on V2.")).toBeHidden({
      timeout: 10_000,
    });
    await settle(page);
    await expect(page).toHaveScreenshot(`review-from-v1-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}

test("a phone reads what moves in one column", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 1200 });
  await openSwitch(page);
  await settle(page);
  await expect(page).toHaveScreenshot("switch-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
