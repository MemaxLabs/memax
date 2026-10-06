import { chromium } from "@playwright/test";

/**
 * Checks once that Chromium can launch. If it can't (not installed, or
 * missing system libraries on a machine without root), the tests skip
 * with the reason locally and the run fails in CI, where a missing
 * browser is a pipeline bug. Tests read E2E_BROWSER_UNAVAILABLE (see
 * fixtures.ts); workers inherit the environment set here.
 */
export default async function globalSetup() {
  try {
    const browser = await chromium.launch();
    await browser.close();
  } catch (error) {
    const reason = `Chromium can't launch: ${String(error).split("\n")[0]}. Run: pnpm --filter @memaxlabs/web exec playwright install --with-deps chromium`;
    if (process.env.CI) throw new Error(reason);
    process.env.E2E_BROWSER_UNAVAILABLE = reason;
    console.warn(`[e2e] skipping: ${reason}`);
  }
}
