import { existsSync } from "node:fs";
import path from "node:path";
import { PREVIEWS } from "@memaxlabs/ledger/previews";
import { HANDOFF_MISSING, handoffPreviewsDir } from "./handoff";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  usingDevServer,
} from "./fixtures";

// /dev/ledger/components against the handoff: every preview, in both
// themes, screenshotted at 2× and compared with
// design-system/previews/<Name>-<theme>.png (HANDOFF §9, design review
// §3.9). The PNGs are read in place from the private memax-internal
// checkout and never written: with --update-snapshots, or without the
// checkout, this file skips.
//
// Threshold. The handoff PNGs were rendered by the design canvas, not
// by this build's Chromium, so text never lands on exactly the same
// subpixels: glyph edges differ, and long lines drift by a pixel as
// advances round differently. Phase 0 measured 0.15–3.3% of pixels
// differing across the 54 previews (plan §12, Phase 0 log). Through
// Playwright's comparator (which skips pixels it classifies as
// antialiased, with a per-pixel YIQ tolerance of 0.2) this gallery
// measures 0.07–2.16%, mean 0.93% (Cite and Diff, long serif lines, are
// the worst). We allow 3.5% of an artboard: just above the worst
// either measurement saw, and well under what a changed colour token or
// a moved panel produces. It's a check against the design, not the
// regression guard; component tests and the shell's self-baselines are.
// E2E_HANDOFF_MAX_RATIO overrides it (0 prints every preview's count).

skipWithoutBrowser();

const MAX_RATIO = Number(process.env.E2E_HANDOFF_MAX_RATIO ?? 0.035);
const dir = handoffPreviewsDir();

test.beforeAll(() => {
  test.skip(!dir, HANDOFF_MISSING);
});

test.beforeEach(async ({ page, baseURL }, testInfo) => {
  const updating = ["all", "changed"].includes(testInfo.config.updateSnapshots);
  test.skip(updating, "Never rewrite the handoff's PNGs.");
  test.skip(usingDevServer, "screenshots need the production build");
  await optIntoV2(page, baseURL);
});

test("the gallery mounts every preview in both themes", async ({ page }) => {
  await page.goto("/dev/ledger/components");
  for (const preview of PREVIEWS) {
    for (const theme of ["light", "dark"]) {
      const box = page.locator(
        `[data-preview="${preview.name}"][data-preview-theme="${theme}"]`,
      );
      await expect(box).toHaveCount(1);
      const size = await box.boundingBox();
      expect(size, `${preview.name} ${theme}`).toMatchObject({
        width: preview.width,
        height: preview.height,
      });
    }
  }
});

for (const preview of PREVIEWS) {
  for (const theme of ["light", "dark"] as const) {
    const file = `${preview.name}-${theme}.png`;
    test(`${preview.name} (${theme}) matches the handoff`, async ({ page }) => {
      test.skip(
        !dir || !existsSync(path.join(dir, file)),
        `No ${file} in the handoff.`,
      );
      await page.goto("/dev/ledger/components");
      await settle(page);
      const box = page.locator(
        `[data-preview="${preview.name}"][data-preview-theme="${theme}"]`,
      );
      await box.scrollIntoViewIfNeeded();
      await expect.soft(box).toHaveScreenshot(file, {
        maxDiffPixelRatio: MAX_RATIO,
        animations: "disabled",
        caret: "hide",
        scale: "device",
      });
    });
  }
}
