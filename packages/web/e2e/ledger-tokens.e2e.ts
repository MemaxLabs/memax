import {
  expect,
  optIntoV2,
  skipWithoutBrowser,
  test,
  usingDevServer,
} from "./fixtures";

// /dev/ledger/tokens: the first visual-regression fixture for the Ledger
// UI, plus the checks a screenshot can't make (theme before paint,
// fonts, CSS isolation).

skipWithoutBrowser();

const THEMES = ["light", "dark"] as const;

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

for (const theme of THEMES) {
  test(`specimen matches its ${theme} screenshot`, async ({
    page,
    baseURL,
  }) => {
    // next dev draws its own indicator over the page.
    test.skip(usingDevServer, "screenshots need the production build");
    await page
      .context()
      .addCookies([{ name: "memax_theme", value: theme, url: baseURL }]);
    await page.goto("/dev/ledger/tokens");
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    await page.evaluate(() => document.fonts.ready);
    await expect(page).toHaveScreenshot(`ledger-tokens-${theme}.png`, {
      fullPage: true,
    });
  });
}

test("applies the stored theme before first paint", async ({
  page,
  baseURL,
}) => {
  await page.addInitScript(() => {
    document.addEventListener("DOMContentLoaded", () => {
      (window as unknown as { themeAtParse?: string }).themeAtParse =
        document.documentElement.dataset.theme;
    });
  });
  await page
    .context()
    .addCookies([{ name: "memax_theme", value: "dark", url: baseURL }]);
  await page.goto("/dev/ledger/tokens");
  const atParse = await page.evaluate(
    () => (window as unknown as { themeAtParse?: string }).themeAtParse,
  );
  expect(atParse).toBe("dark");
});

test("follows the system theme when none is stored", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/dev/ledger/tokens");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.emulateMedia({ colorScheme: "light" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

test("uses the Ledger faces, with real italics", async ({ page }) => {
  await page.goto("/dev/ledger/tokens");
  await page.evaluate(() => document.fonts.ready);
  const loaded = await page.evaluate(() =>
    [...document.fonts]
      .filter((face) => face.status === "loaded")
      .map((face) => `${face.family.replace(/["']/g, "")} ${face.style}`),
  );
  expect(loaded).toEqual(
    expect.arrayContaining([
      "ledgerSerif normal",
      "ledgerSerif italic",
      "ledgerSans normal",
      "ledgerMono normal",
      "ledgerBrand normal",
    ]),
  );
  // tokens.css declares its own faces; they must never be fetched.
  expect(
    loaded.filter((face) =>
      /^(Newsreader|Gloock|Schibsted Grotesk|IBM Plex Mono) /.test(face),
    ),
  ).toEqual([]);
  const proposed = page.locator(".memory-proposed").first();
  await expect(proposed).toHaveCSS("font-style", "italic");
  expect(
    await proposed.evaluate((el) => getComputedStyle(el).fontFamily),
  ).toMatch(/^"?ledgerSerif"?,/);
});

test("preloads only Newsreader roman and Schibsted Grotesk", async ({
  page,
}) => {
  test.skip(usingDevServer, "next dev doesn't emit font preloads");
  await page.goto("/dev/ledger/tokens");
  // Map each preloaded file back to the @font-face that uses it.
  const preloaded = await page.evaluate(() => {
    const faceBySrc = new Map<string, string>();
    for (const sheet of [...document.styleSheets]) {
      for (const rule of [...sheet.cssRules]) {
        if (!(rule instanceof CSSFontFaceRule)) continue;
        const src = rule.style.getPropertyValue("src");
        const url = /url\(["']?([^"')]+)["']?\)/.exec(src)?.[1];
        if (!url) continue;
        const family = rule.style.getPropertyValue("font-family");
        const style = rule.style.getPropertyValue("font-style") || "normal";
        faceBySrc.set(
          new URL(url, sheet.href ?? location.href).pathname,
          `${family.replace(/["']/g, "")} ${style}`,
        );
      }
    }
    return [...document.querySelectorAll('link[rel="preload"][as="font"]')]
      .map((link) => new URL(link.getAttribute("href") ?? "", location.href))
      .map((url) => faceBySrc.get(url.pathname) ?? url.pathname);
  });
  expect(preloaded.sort()).toEqual(["ledgerSans normal", "ledgerSerif normal"]);
});

test("loads no V1 stylesheet, not even as a preload", async ({ page }) => {
  await page.goto("/dev/ledger/tokens");
  const hrefs = await page
    .locator('link[rel="stylesheet"], link[rel="preload"][as="style"]')
    .evaluateAll((links) => links.map((link) => link.getAttribute("href")));
  expect(hrefs.length).toBeGreaterThan(0);
  for (const href of hrefs) {
    const css = await (await page.request.get(href ?? "")).text();
    // Tailwind's variables and V1's glass surfaces mark V1 CSS.
    expect(css, href ?? "").not.toMatch(/--tw-|\.glass\b|tailwindcss/);
  }
});
