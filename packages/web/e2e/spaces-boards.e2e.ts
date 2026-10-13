import { existsSync } from "node:fs";
import path from "node:path";
import type { Page } from "@playwright/test";
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

// Epic 3.3's screens against their boards (Search.png, SpaceSwitcher.png,
// NewSpace.png in the private handoff, read in place, never written), at
// each board's size in Paper. Like onboarding-boards.e2e.ts it measures
// how far each screen is from its board (E2E_BOARDS_MAX_RATIO; 0 prints
// the difference); the self-baselines in spaces.e2e.ts guard them.
// EmptySpace.png is measured in onboarding-boards.e2e.ts.

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.08);
const dir = handoffScreensDir();

const BOARDS: {
  board: string;
  path: string;
  height: number;
  open: (page: Page) => Promise<void>;
}[] = [
  {
    board: "Search",
    path: "/memax-v2/search?q=deploy",
    height: 1100,
    open: async (page) => {
      await expect(
        page.getByRole("region", { name: "Memories" }),
      ).toBeVisible();
    },
  },
  {
    board: "SpaceSwitcher",
    path: "/memax-v2/today",
    height: 1000,
    open: async (page) => {
      await page
        .getByRole("button", { name: "memax-v2, Switch space" })
        .click();
      await expect(page.getByRole("menu")).toBeVisible();
      // The board's pointer rests on nothing: the current space is lit.
      await page.mouse.move(1300, 980);
    },
  },
  {
    board: "NewSpace",
    path: "/memax-v2/today",
    height: 1000,
    open: async (page) => {
      await page
        .getByRole("button", { name: "memax-v2, Switch space" })
        .click();
      await page.getByRole("menuitem", { name: "New space" }).click();
      const dialog = page.getByRole("dialog", { name: "New space" });
      await expect(dialog).toBeVisible();
      // The board's form: memax-web, for MemaxLabs/memax-web.
      await dialog
        .getByRole("textbox", { name: "Repository" })
        .fill("MemaxLabs/memax-web");
      await page.mouse.move(1300, 980);
    },
  },
];

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
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await b.open(page);
    await settle(page);
    await expect.soft(page).toHaveScreenshot(`${b.board}.png`, {
      maxDiffPixelRatio: MAX_RATIO,
      animations: "disabled",
      caret: "hide",
      scale: "css",
    });
  });
}
