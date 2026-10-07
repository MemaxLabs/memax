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

// Dream's edition against its board (DreamEdition.png in the private
// handoff, read in place, never written), at the board's size in Paper.
// Like onboarding-boards.e2e.ts it measures how far the screen is from
// the board (E2E_BOARDS_MAX_RATIO; 0 prints the difference); the
// self-baselines in dream.e2e.ts guard it.

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.08);
const dir = handoffScreensDir();

const BOARDS = [
  {
    board: "DreamEdition",
    path: "/memax-v2/dream/214",
    heading: "Monday, October 5, overnight",
    height: 1400,
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
