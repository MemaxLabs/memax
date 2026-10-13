import type { Page } from "@playwright/test";
import {
  expect,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// The public site (plan 25 epic 3.4): Landing, Pricing, Security and Docs.
// Phase 3's accept rule is that every page passes contrast and keyboard
// checks in both themes; axe isn't a dependency here, so these checks are
// written out (as in app-shell.e2e.ts): landmarks and headings, a name on
// every control, WCAG AA contrast for every piece of text against what is
// really behind it, every control reached by Tab with a visible focus
// ring, nothing moving under reduced motion, and the pages open signed
// out without the V2 hint. Then self-baselines in Paper and Carbon;
// public-boards.e2e.ts measures them against the handoff's boards.

skipWithoutBrowser();

const PAGES = [
  { path: "/welcome", heading: "One context. Every agent." },
  { path: "/pricing", heading: "Free for one project. Fair when it grows." },
  { path: "/security", heading: "What Memax keeps, and how it lets go." },
  { path: "/docs", heading: "Quickstart" },
] as const;

const THEMES = ["light", "dark"] as const;

/** Text in the page whose contrast is below WCAG AA, with what it sits on. */
async function lowContrast(page: Page) {
  return page.evaluate(() => {
    type RGBA = [number, number, number, number];
    const parse = (value: string): RGBA | null => {
      const m = value.match(/rgba?\(([^)]+)\)/);
      if (m) {
        const [r, g, b, a = "1"] = m[1]!.split(/[\s,/]+/).filter(Boolean);
        return [Number(r), Number(g), Number(b), Number(a)];
      }
      const c = value.match(/color\(srgb ([^)]+)\)/);
      if (c) {
        const [r, g, b, a = "1"] = c[1]!.split(/[\s/]+/).filter(Boolean);
        return [Number(r) * 255, Number(g) * 255, Number(b) * 255, Number(a)];
      }
      return null;
    };
    const over = (top: RGBA, bottom: RGBA): RGBA => {
      const a = top[3] + bottom[3] * (1 - top[3]);
      if (a === 0) return [0, 0, 0, 0];
      const mix = (i: number) =>
        (top[i]! * top[3] + bottom[i]! * bottom[3] * (1 - top[3])) / a;
      return [mix(0), mix(1), mix(2), a];
    };
    const luminance = ([r, g, b]: RGBA) => {
      const ch = (v: number) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
    };
    const ratio = (a: RGBA, b: RGBA) => {
      const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
      return (hi! + 0.05) / (lo! + 0.05);
    };
    /** What an element is painted on: its backgrounds, composited down to the page. */
    const backdrop = (el: Element): RGBA => {
      const layers: RGBA[] = [];
      for (let node: Element | null = el; node; node = node.parentElement) {
        const bg = parse(getComputedStyle(node).backgroundColor);
        if (bg && bg[3] > 0) {
          layers.push(bg);
          if (bg[3] >= 1) break;
        }
      }
      let colour: RGBA = [255, 255, 255, 1];
      for (const layer of layers.reverse()) colour = over(layer, colour);
      return colour;
    };
    const opacity = (el: Element) => {
      let o = 1;
      for (let node: Element | null = el; node; node = node.parentElement) {
        o *= Number(getComputedStyle(node).opacity);
      }
      return o;
    };
    const failures: string[] = [];
    const walker = document.createTreeWalker(
      document.body,
      NodeFilter.SHOW_TEXT,
    );
    const seen = new Set<Element>();
    for (let text = walker.nextNode(); text; text = walker.nextNode()) {
      const el = text.parentElement;
      if (!el || seen.has(el) || !text.textContent?.trim()) continue;
      seen.add(el);
      if (el.closest("svg, [aria-disabled='true'], :disabled")) continue;
      // Pure decoration is exempt (WCAG 1.4.3): a separator or prompt
      // glyph hidden from assistive technology, with no letter or digit.
      if (
        el.closest("[aria-hidden='true']") &&
        !/[\p{L}\p{N}]/u.test(text.textContent)
      ) {
        continue;
      }
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      if (box.width <= 1 || box.height <= 1 || style.visibility !== "visible")
        continue;
      const fg = parse(style.color);
      if (!fg) continue;
      // Not shown at all (the kept card's cross-faded proposal).
      const alpha = opacity(el);
      if (alpha === 0) continue;
      const bg = backdrop(el);
      const shown: RGBA = over([fg[0], fg[1], fg[2], fg[3] * alpha], bg);
      const size = parseFloat(style.fontSize);
      const bold = Number(style.fontWeight) >= 700;
      const large = size >= 24 || (size >= 18.66 && bold);
      const need = large ? 3 : 4.5;
      const got = ratio(shown, bg);
      if (got < need) {
        failures.push(
          `${got.toFixed(2)} < ${need}: "${text.textContent.trim().slice(0, 40)}" (${style.color} on ${bg.map((v) => Math.round(v)).join(",")})`,
        );
      }
    }
    return failures;
  });
}

for (const p of PAGES) {
  test.describe(p.path, () => {
    test("opens signed out, without the V2 hint, with its landmarks and headings", async ({
      page,
    }) => {
      const res = await page.goto(p.path);
      expect(res?.status()).toBe(200);
      await expect(page).toHaveURL(p.path);
      await expect(page.getByRole("heading", { level: 1 })).toHaveText(
        p.heading,
      );
      await expect(page.getByRole("main")).toHaveCount(1);
      await expect(page.getByRole("banner")).toHaveCount(1);
      // Headings never skip a level.
      const levels = await page
        .locator("h1, h2, h3, h4, h5, h6")
        .evaluateAll((hs) => hs.map((h) => Number(h.tagName[1])));
      for (let i = 1; i < levels.length; i++) {
        expect(levels[i]! - levels[i - 1]!, `heading ${i}`).toBeLessThanOrEqual(
          1,
        );
      }
      for (const role of ["button", "link", "radio", "searchbox"] as const) {
        for (const control of await page.getByRole(role).all()) {
          if (!(await control.isVisible())) continue;
          await expect(control, `${p.path} ${role}`).toHaveAccessibleName(/\S/);
        }
      }
      // Static and described for search and sharing.
      await expect(page.locator('meta[name="description"]')).toHaveAttribute(
        "content",
        /\S/,
      );
      await expect(page.locator('meta[property="og:image"]')).toHaveAttribute(
        "content",
        /\/images\/og-memax\.png$/,
      );
      await expect(page.locator('link[rel="canonical"]')).toHaveCount(1);
    });

    for (const theme of THEMES) {
      test(`passes contrast in ${theme === "light" ? "Paper" : "Carbon"}`, async ({
        page,
        baseURL,
      }) => {
        await useTheme(page, baseURL, theme);
        for (const width of [1440, 390]) {
          await page.setViewportSize({ width, height: 900 });
          await page.goto(p.path);
          await expect(page.locator("html")).toHaveAttribute(
            "data-theme",
            theme,
          );
          await settle(page);
          expect(await lowContrast(page), `${p.path} at ${width}`).toEqual([]);
        }
      });
    }

    for (const theme of THEMES) {
      test(`reaches every control by keyboard in ${theme === "light" ? "Paper" : "Carbon"}, each with a visible focus ring`, async ({
        page,
        baseURL,
      }) => {
        await useTheme(page, baseURL, theme);
        await page.goto(p.path);
        await settle(page);
        const tabbable = await page.evaluate(
          () =>
            [
              ...document.querySelectorAll<HTMLElement>(
                "a[href], button:not(:disabled), input:not(:disabled), [tabindex]:not([tabindex='-1'])",
              ),
            ].filter((el) => {
              const box = el.getBoundingClientRect();
              return (
                el.tabIndex >= 0 &&
                box.width > 0 &&
                box.height > 0 &&
                getComputedStyle(el).visibility === "visible" &&
                !el.closest("[inert]")
              );
            }).length,
        );
        const reached = new Set<string>();
        for (let i = 0; i < tabbable + 2; i++) {
          await page.keyboard.press("Tab");
          const focus = await page.evaluate(() => {
            const el = document.activeElement as HTMLElement | null;
            // next dev's own overlay isn't the page's.
            if (!el || el === document.body || el.tagName === "NEXTJS-PORTAL") {
              return null;
            }
            // A Field draws its ring on its box, around the input.
            const drawn = el.closest(".mx-field-box") ?? el;
            const style = getComputedStyle(drawn);
            const box = el.getBoundingClientRect();
            const ring =
              (style.outlineStyle !== "none" &&
                parseFloat(style.outlineWidth) >= 2) ||
              style.boxShadow !== "none";
            return {
              key: `${el.tagName}:${el.textContent?.trim().slice(0, 30)}:${box.x},${box.y + window.scrollY}`,
              ring,
              inView: box.bottom > 0 && box.top < window.innerHeight,
            };
          });
          if (!focus) continue;
          expect(focus.ring, `${theme}: focus ring on ${focus.key}`).toBe(true);
          expect(
            focus.inView,
            `${theme}: ${focus.key} scrolled into view`,
          ).toBe(true);
          reached.add(focus.key);
        }
        expect(reached.size, `${theme}: every tabbable control`).toBe(tabbable);
      });
    }

    test("holds still under reduced motion", async ({ page }) => {
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.goto(p.path);
      await settle(page);
      const running = await page.evaluate(() =>
        document
          .getAnimations()
          .filter((a) => a.playState === "running")
          .map((a) => (a as CSSAnimation).animationName ?? "transition"),
      );
      expect(running).toEqual([]);
    });
  });
}

test("the skip link jumps past the header", async ({ page }) => {
  await page.goto("/pricing");
  await page.keyboard.press("Tab");
  const skip = page.getByRole("link", { name: "Skip to content" });
  await expect(skip).toBeFocused();
  await expect(skip).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page.locator("main")).toBeFocused();
});

test("Pricing switches to yearly prices from the keyboard", async ({
  page,
}) => {
  await page.goto("/pricing");
  const monthly = page.getByRole("radio", { name: "Monthly" });
  await monthly.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("radio", { name: "Yearly" })).toBeChecked();
  const pro = page.getByRole("article", { name: "Pro" });
  await expect(pro).toContainText("$120 a year");
  await expect(pro).toContainText("2 months free");
  // Nothing to buy while billing is closed.
  await expect(pro.getByRole("button")).toHaveCount(0);
  await expect(pro).toContainText("Coming soon");
});

test("Docs filters its tree as you type", async ({ page }) => {
  await page.goto("/docs");
  const search = page.getByRole("searchbox", { name: "Search the docs" });
  await search.focus();
  await page.keyboard.type("memax");
  const tree = page.getByRole("navigation", { name: "Docs" });
  await expect(tree.getByRole("status")).toHaveText("3 pages match");
  await expect(tree.getByRole("link")).toHaveText([
    "memax init",
    "memax compile",
    "memax connect",
  ]);
});

test("How it works moves on to its section", async ({ page }) => {
  await page.goto("/welcome");
  await page.getByRole("link", { name: "How it works" }).click();
  await expect(page).toHaveURL(/#how-it-works$/);
  await expect(
    page.getByRole("heading", {
      name: "Kept once. Read natively, everywhere.",
    }),
  ).toBeInViewport();
});

test("a Chinese browser reads the site in Chinese", async ({
  browser,
  baseURL,
}) => {
  const context = await browser.newContext({ locale: "zh-CN", baseURL });
  const page = await context.newPage();
  await page.goto("/pricing");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "一个项目免费。 规模变大，价格公道。",
  );
  await expect(page.locator("html")).toHaveAttribute("lang", "zh");
  await context.close();
});

for (const p of PAGES) {
  for (const theme of THEMES) {
    test(`${p.path}, ${theme}, 1440`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto(p.path);
      await expect(page.getByRole("heading", { level: 1 })).toHaveText(
        p.heading,
      );
      await settle(page);
      await expect(page).toHaveScreenshot(
        `${p.path.slice(1)}-${theme}-1440.png`,
        { fullPage: true, maxDiffPixelRatio: 0.002 },
      );
    });
  }
  test(`${p.path}, light, 390`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, "light");
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(p.path);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(p.heading);
    await settle(page);
    await expect(page).toHaveScreenshot(`${p.path.slice(1)}-light-390.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}
