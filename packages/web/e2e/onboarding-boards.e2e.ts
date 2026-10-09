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

// The first session's screens against their boards (screens/png in the
// private handoff, read in place, never written), at each board's size
// in Paper. The demo dataset isn't the boards' exact data everywhere
// (the demo's agents, a drifted Cursor file, Cleanup's words where the
// boards disagree with each other), so this measures how far each screen
// is from its board rather than guarding it; the self-baselines in
// onboarding.e2e.ts do that. E2E_BOARDS_MAX_RATIO sets the bar (0 prints
// every screen's difference).

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.08);
const dir = handoffScreensDir();

const BOARDS = [
  {
    board: "SignIn",
    path: "/signin",
    heading: "The context layer you own.",
    height: 900,
  },
  {
    board: "CliAuth",
    path: "/device?code=WQRT-4821",
    heading: "Connect the memax CLI",
    height: 900,
  },
  {
    board: "Connect",
    path: "/setup/agents?space=memax-v2",
    heading: "Which agents should share this context?",
    height: 960,
  },
  {
    board: "FirstRun",
    path: "/setup/import?space=memax-v2",
    heading: "Give every agent the same context.",
    height: 900,
  },
  {
    board: "Cleanup",
    path: "/setup/cleanup?space=memax-v2",
    heading: "6 files, 3 conflicts, 1 Brief.",
    height: 1120,
  },
  {
    board: "ReviewImport",
    path: "/memax-v2/review?filter=import",
    heading: "33 imports from 6 files",
    height: 1000,
  },
  {
    board: "CompileDone",
    path: "/setup/done?space=memax-v2",
    heading: "memax-v2 is compiled into 3 files.",
    height: 1040,
  },
  {
    board: "EmptySpace",
    path: "/memax-web/today",
    heading: "Nothing here yet",
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
    await page.goto(b.path);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(b.heading);
    if (b.board === "ReviewImport") {
      // The board's selection: everything that agrees, less one.
      await page
        .getByRole("checkbox", { name: /^Select the \d+ that can be kept/ })
        .check();
      await page.getByRole("checkbox", { name: "Select it" }).nth(6).uncheck();
    }
    if (b.board === "Cleanup") {
      await page
        .getByRole("radiogroup", { name: "Test command" })
        .getByRole("radio")
        .first()
        .click();
    }
    await settle(page);
    await expect.soft(page).toHaveScreenshot(`${b.board}.png`, {
      maxDiffPixelRatio: MAX_RATIO,
      animations: "disabled",
      caret: "hide",
      scale: "css",
    });
  });
}
