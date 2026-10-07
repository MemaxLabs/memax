import { existsSync } from "node:fs";
import path from "node:path";
import { HANDOFF_MISSING, handoffScreensDir } from "./handoff";
import {
  expect,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// OAuthConsent against its board (OAuthConsent.png in the private
// handoff, read in place, never written), at the board's size in Paper,
// with the board's request (the dev fixtures' demo). The demo is the
// board's data, so this one holds a tighter bar than the first session's
// boards: under 5% of pixels (E2E_BOARDS_MAX_RATIO; 0 prints the
// difference). The self-baselines in oauth-consent.e2e.ts guard both
// themes; there is no Carbon board.

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_BOARDS_MAX_RATIO ?? 0.05);
const dir = handoffScreensDir();

test.beforeAll(() => {
  test.skip(!dir, HANDOFF_MISSING);
});

test("OAuthConsent against its board", async ({ page, baseURL }, testInfo) => {
  const updating = ["all", "changed"].includes(testInfo.config.updateSnapshots);
  test.skip(updating, "Never rewrite the handoff's boards.");
  test.skip(usingDevServer, "screenshots need the production build");
  test.skip(
    !dir || !existsSync(path.join(dir, "OAuthConsent.png")),
    "No OAuthConsent.png in the handoff.",
  );
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 1440, height: 940 });
  await page.goto("/oauth/authorize?request_id=demo&consent_token=demo");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Codex wants to connect to Memax",
  );
  await settle(page);
  await expect.soft(page).toHaveScreenshot("OAuthConsent.png", {
    maxDiffPixelRatio: MAX_RATIO,
    animations: "disabled",
    caret: "hide",
    scale: "css",
  });
});
