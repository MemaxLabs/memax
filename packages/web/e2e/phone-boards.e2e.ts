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

// The phone against its boards (MobileToday.png, MobileReview.png,
// MobileGate.png, MobileAsk.png and MobileMemory.png in the private
// handoff, read in place and never written): 390×844 at 2×, as they were
// drawn, in Paper, on the demo. Like the other *-boards specs it measures
// how far each screen is from its board (E2E_BOARDS_MAX_RATIO=0 prints
// every difference); phone.e2e.ts holds the screens to their baselines.
//
// What differs on purpose, so each board has its own bar:
// - Today: the demo's own Dream edition and waiting rows (the real data's
//   words), and the receipt's space.
// - Review: no keycaps on a phone's buttons (there are no keys), the
//   Previous and Next under the card, and what the card touches below.
// - Gate: the demo's question is in Memax team, and nothing is chosen
//   before the person chooses; Withdraw sits under Answer, and the line
//   under the card is the gate's session and expiry (handoffs aren't
//   built). Review stays the current tab: that's where the gate is.
// - Ask: the demo's answer and sources, and Copy with citations under
//   Keep as memory.
// - Memory: back to Memories, and the demo memory's full lineage.

skipWithoutBrowser();

const ENV_RATIO = process.env.E2E_BOARDS_MAX_RATIO;
const dir = handoffScreensDir();

test.use({
  viewport: { width: 390, height: 844 },
  deviceScaleFactor: 2,
  hasTouch: true,
  isMobile: true,
});

async function ask(page: Page) {
  await page
    .getByRole("navigation", { name: "Places" })
    .getByRole("button", { name: "Ask" })
    .tap();
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  await dialog
    .getByRole("textbox", { name: "Ask" })
    .fill("Why River and not Temporal?");
  await page.keyboard.press("Enter");
  await expect(
    dialog.getByRole("button", { name: "Keep as memory" }),
  ).toBeVisible();
  // The field had the focus to type; the board shows it at rest.
  await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
}

const BOARDS: {
  board: string;
  path: string;
  open?: (page: Page) => Promise<void>;
  max: number;
}[] = [
  { board: "MobileToday", path: "/memax-v2/today", max: 0.12 },
  { board: "MobileReview", path: "/memax-v2/review", max: 0.12 },
  { board: "MobileGate", path: "/memax-team/review?gate=G-0012", max: 0.12 },
  { board: "MobileAsk", path: "/memax-v2/today", open: ask, max: 0.12 },
  { board: "MobileMemory", path: "/memax-v2/memories/M-0219", max: 0.12 },
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
    await page.goto(b.path);
    await expect(
      page.getByRole("heading", { level: 1 }).filter({ visible: true }),
    ).toBeVisible();
    await b.open?.(page);
    await settle(page);
    await expect.soft(page).toHaveScreenshot(`${b.board}.png`, {
      maxDiffPixelRatio: ENV_RATIO !== undefined ? Number(ENV_RATIO) : b.max,
      animations: "disabled",
      caret: "hide",
      scale: "device",
    });
  });
}
