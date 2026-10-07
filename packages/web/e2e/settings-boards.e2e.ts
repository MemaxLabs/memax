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

// Settings › Account and Notifications against their boards (Account.png
// and Notifications.png in the private handoff, read in place, never
// written), at the board's size in Paper. Like dream-boards.e2e.ts it
// measures how far the screen is from the board (E2E_BOARDS_MAX_RATIO; 0
// prints the difference); the self-baselines in settings.e2e.ts guard
// it. Security has no board of its own (Security.png is the public Trust
// page), so it isn't here.

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.08);
const dir = handoffScreensDir();

const BOARDS = [
  {
    board: "Account",
    path: "/settings/account",
    heading: "Account",
    height: 1040,
  },
  {
    board: "Notifications",
    path: "/settings/notifications",
    heading: "Notifications",
    height: 1000,
  },
] as const;

test.beforeAll(() => {
  test.skip(!dir, HANDOFF_MISSING);
});

for (const b of BOARDS) {
  test(`${b.board} against its board`, async ({ page, baseURL }, testInfo) => {
    const updating = ["all", "changed"].includes(
      testInfo.config.updateSnapshots,
    );
    test.skip(updating, "Never rewrite the handoff's boards.");
    test.skip(usingDevServer, "screenshots need the production build");
    test.skip(
      !dir || !existsSync(path.join(dir, `${b.board}.png`)),
      `No ${b.board}.png in the handoff.`,
    );
    await optIntoV2(page, baseURL);
    await useTheme(page, baseURL, "light");
    await page.setViewportSize({ width: 1440, height: b.height });
    // The board's rail shows the Personal space.
    await page.goto("/personal/today");
    await page.goto(b.path);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(b.heading);
    await settle(page);
    await expect.soft(page).toHaveScreenshot(`${b.board}.png`, {
      maxDiffPixelRatio: MAX_RATIO,
      animations: "disabled",
      caret: "hide",
      scale: "css",
    });
  });
}
