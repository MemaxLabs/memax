import { existsSync } from "node:fs";
import path from "node:path";
import { HANDOFF_MISSING, handoffScreensDir } from "./handoff";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// VerifyStale against its board (VerifyStale.png in the private handoff,
// read in place, never written): the layer over Memories at the board's
// size in Paper, opened with V on M-0187. Like dream-boards.e2e.ts it
// measures how far the screen is from the board (E2E_BOARDS_MAX_RATIO; 0
// prints the difference); the self-baselines in verify.e2e.ts guard it.

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.08);
const dir = handoffScreensDir();

test.beforeAll(() => {
  test.skip(!dir, HANDOFF_MISSING);
});

test("VerifyStale against its board", async ({ page, baseURL }, testInfo) => {
  const updating = ["all", "changed"].includes(testInfo.config.updateSnapshots);
  test.skip(updating, "Never rewrite the handoff's boards.");
  test.skip(usingDevServer, "screenshots need the production build");
  test.skip(
    !dir || !existsSync(path.join(dir, "VerifyStale.png")),
    "No VerifyStale.png in the handoff.",
  );
  await optIntoV2(page, baseURL);
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto("/memax-v2/memories");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Memories");
  await page
    .locator(".mx-row", { hasText: "Ask memax answers with the Haiku tier." })
    .first()
    .locator(".mx-row-link")
    .focus();
  await page.keyboard.press("v");
  const layer = page.getByRole("dialog", { name: "Verify M-0187" });
  await expect(layer.getByText(/Removed from 3 files/)).toBeVisible();
  await settle(page);
  await expect.soft(page).toHaveScreenshot("VerifyStale.png", {
    maxDiffPixelRatio: MAX_RATIO,
    animations: "disabled",
    caret: "hide",
    scale: "css",
  });
});
