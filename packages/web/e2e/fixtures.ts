import { expect, test, type Locator, type Page } from "@playwright/test";

export { expect, test };

/**
 * Call at the top of every spec file: skips the file when global-setup
 * found no usable browser (locally only; CI fails in global-setup).
 */
export function skipWithoutBrowser() {
  const reason = process.env.E2E_BROWSER_UNAVAILABLE;
  test.skip(Boolean(reason), reason);
}

export const usingDevServer = process.env.E2E_SERVER === "dev";

/** Opts the browser into the Ledger UI, like /dev/ui?v=2 does. */
export async function optIntoV2(page: Page, baseURL: string | undefined) {
  if (!baseURL) throw new Error("baseURL is not set");
  await page
    .context()
    .addCookies([{ name: "memax_ui", value: "v2", url: baseURL }]);
}

/**
 * Before a screenshot: no motion (the projects emulate reduced motion
 * too) and every font face loaded, so text never swaps mid-capture.
 */
export async function settle(page: Page) {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    );
  });
}

/** Sets the Paper / Carbon cookie the head script reads before first paint. */
export async function useTheme(
  page: Page,
  baseURL: string | undefined,
  theme: "light" | "dark",
) {
  if (!baseURL) throw new Error("baseURL is not set");
  await page
    .context()
    .addCookies([{ name: "memax_theme", value: theme, url: baseURL }]);
}

/** Presses Tab until `target` has focus, like a keyboard user would. */
export async function tabTo(
  page: Page,
  target: Locator,
  maxPresses = 40,
  key: "Tab" | "Shift+Tab" = "Tab",
) {
  for (let i = 0; i < maxPresses; i++) {
    if (await target.evaluate((el) => el === document.activeElement)) return;
    await page.keyboard.press(key);
  }
  await expect(target).toBeFocused();
}

export async function cookieValue(page: Page, name: string) {
  const cookies = await page.context().cookies();
  return cookies.find((cookie) => cookie.name === name)?.value;
}
