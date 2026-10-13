import type { Locator, Page } from "@playwright/test";
import {
  hiddenUntilHover,
  lowContrast,
  runningAnimations,
  smallTargets,
} from "./a11y";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// The phone (plan 25 epic 3.5; MobileToday, MobileReview, MobileGate,
// MobileAsk and MobileMemory), on the demo: at 390 and 430 wide, in Paper
// and Carbon, every screen passes contrast, has 44px touch targets, hides
// nothing behind a hover a touch screen never has, and holds still under
// reduced motion. Then the gestures with a real (emulated) finger: a
// swipe keeps or rejects through Review's own commands, a vertical drag
// scrolls, one tap answers a gate, and the full-screen Ask has its tab.
// Baselines for each screen; phone-boards.e2e.ts measures them against
// the boards.

skipWithoutBrowser();

const SIZES = [
  { width: 390, height: 844 },
  { width: 430, height: 932 },
] as const;
const THEMES = ["light", "dark"] as const;

test.use({ hasTouch: true, isMobile: true });

/** Opens the full-screen Ask from the tab bar and asks the board's question. */
async function ask(page: Page) {
  await page
    .getByRole("navigation", { name: "Places" })
    .getByRole("button", { name: "Ask" })
    .tap();
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  await expect(dialog).toBeVisible();
  await dialog
    .getByRole("textbox", { name: "Ask" })
    .fill("Why River and not Temporal?");
  await page.keyboard.press("Enter");
  await expect(
    dialog.getByRole("button", { name: "Keep as memory" }),
  ).toBeVisible();
  return dialog;
}

const SCREENS: {
  name: string;
  path: string;
  heading: string | RegExp;
  open?: (page: Page) => Promise<unknown>;
}[] = [
  {
    name: "today",
    path: "/memax-v2/today",
    // Today's heading holds its date line too.
    heading: /Four proposals and a question are waiting on you\.$/,
  },
  { name: "review", path: "/memax-v2/review", heading: "Review" },
  {
    name: "gate",
    path: "/memax-team/review?gate=G-0012",
    heading: "A question for you",
  },
  {
    name: "memory",
    path: "/memax-v2/memories/M-0219",
    heading: /^Background jobs run on River/,
  },
  {
    name: "ask",
    path: "/memax-v2/today",
    heading: /Four proposals and a question are waiting on you\.$/,
    open: ask,
  },
];

/** A finger across `target`: down, eight moves, up (CDP touch events). */
async function swipe(page: Page, target: Locator, dx: number, dy = 0) {
  const box = await target.boundingBox();
  if (!box) throw new Error("nothing to swipe");
  const x = box.x + box.width / 2;
  const y = Math.min(box.y + box.height / 2, 600);
  const cdp = await page.context().newCDPSession(page);
  const touch = (
    type: "touchStart" | "touchMove" | "touchEnd",
    points: { x: number; y: number }[],
  ) => cdp.send("Input.dispatchTouchEvent", { type, touchPoints: points });
  await touch("touchStart", [{ x, y }]);
  for (let i = 1; i <= 8; i++) {
    await touch("touchMove", [{ x: x + (dx * i) / 8, y: y + (dy * i) / 8 }]);
  }
  await touch("touchEnd", []);
  await cdp.detach();
}

for (const size of SIZES) {
  for (const theme of THEMES) {
    test.describe(`${size.width}, ${theme === "light" ? "Paper" : "Carbon"}`, () => {
      test.use({ viewport: size });

      for (const screen of SCREENS) {
        test(screen.name, async ({ page, baseURL }) => {
          await optIntoV2(page, baseURL);
          await useTheme(page, baseURL, theme);
          await page.goto(screen.path);
          await expect(
            page.getByRole("heading", { level: 1 }).filter({ visible: true }),
          ).toHaveText(screen.heading);
          await screen.open?.(page);
          await settle(page);
          // The rail is gone; the tab bar is there.
          await expect(page.locator(".mx-rail")).toBeHidden();
          await expect(
            page.getByRole("navigation", { name: "Places" }).last(),
          ).toBeVisible();
          expect(await lowContrast(page), "contrast").toEqual([]);
          expect(await smallTargets(page), "44px targets").toEqual([]);
          expect(await hiddenUntilHover(page), "hover-only").toEqual([]);
          expect(await runningAnimations(page), "reduced motion").toEqual([]);
          if (usingDevServer) return;
          await expect(page).toHaveScreenshot(
            `${screen.name}-${theme}-${size.width}.png`,
            { maxDiffPixelRatio: 0.002 },
          );
        });
      }
    });
  }
}

test.describe("gestures, 390", () => {
  test.use({ viewport: SIZES[0] });

  test.beforeEach(async ({ page, baseURL }) => {
    await optIntoV2(page, baseURL);
  });

  const card = (page: Page) => page.locator(".mx-review").first();
  const position = (page: Page) =>
    page.locator("header").filter({ hasText: "Review" }).first();

  test("a swipe right keeps, through Review's own Keep", async ({ page }) => {
    await page.goto("/memax-v2/review");
    await expect(position(page)).toContainText("1 of 5");
    await expect(
      page.getByText("Swipe right to keep, left to reject"),
    ).toBeVisible();
    await swipe(page, card(page), 220);
    await expect(
      page.getByText("Kept M-0430 · 3 files recompiled"),
    ).toBeVisible();
    await expect(position(page)).toContainText("1 of 4");
  });

  test("a swipe left rejects, and Undo puts it back", async ({ page }) => {
    await page.goto("/memax-v2/review");
    await expect(position(page)).toContainText("1 of 5");
    await swipe(page, card(page), -220);
    await expect(page.getByText("Rejected M-0430")).toBeVisible();
    await expect(position(page)).toContainText("1 of 4");
    // The toast's Undo is a finger's target too.
    expect(await smallTargets(page), "44px targets").toEqual([]);
    // Tapped at once: a swipe leaves no fling behind to swallow the tap.
    await page.getByRole("button", { name: "Undo" }).tap();
    await expect(
      page.getByText("Undid the rejection. M-0430 is back in Review."),
    ).toBeVisible();
    await expect(position(page)).toContainText("1 of 5");
  });

  test("a vertical drag scrolls, and a short one decides nothing", async ({
    page,
  }) => {
    await page.goto("/memax-v2/review");
    await expect(position(page)).toContainText("1 of 5");
    await swipe(page, card(page), 30, -260);
    await swipe(page, card(page), 40);
    await page.waitForTimeout(800);
    await expect(position(page)).toContainText("1 of 5");
    await expect(page.getByText(/^Kept M-0430/)).toHaveCount(0);
    await expect(page.getByText(/^Rejected M-0430/)).toHaveCount(0);
  });

  test("a gate answers on one tap of Answer", async ({ page }) => {
    await page.goto("/memax-team/review?gate=G-0012");
    const gate = page.locator(".mx-gate");
    await gate.getByRole("radio", { name: /Railway/ }).tap();
    await gate.getByRole("button", { name: "Answer" }).tap();
    await expect(
      page.getByText(/^Kept M-\d+ as your answer to G-0012/),
    ).toBeVisible();
  });

  test("Ask fills the screen with its own tab current, and a tab leaves it", async ({
    page,
  }) => {
    await page.goto("/memax-v2/review");
    const dialog = await ask(page);
    const tabs = dialog.getByRole("navigation", { name: "Places" });
    await expect(tabs.getByRole("button", { name: "Ask" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    // It covers the page, from the top to the tab bar.
    const layer = await dialog.boundingBox();
    expect(layer?.y).toBe(0);
    expect(layer?.height).toBe(SIZES[0].height);
    await tabs.getByRole("link", { name: "Today" }).tap();
    await expect(dialog).toBeHidden();
    await expect(page).toHaveURL(/\/memax-v2\/today$/);
  });

  test("a memory's page reads as the board: back, its ID, Edit, Cite and Forget", async ({
    page,
  }) => {
    await page.goto("/memax-v2/memories/M-0219");
    const header = page.locator("header").filter({ hasText: "M-0219" }).first();
    await expect(
      header.getByRole("link", { name: "Back to Memories" }),
    ).toBeVisible();
    for (const name of ["Edit", "Cite", "Forget"]) {
      await expect(
        page.getByRole("button", { name, exact: true }),
      ).toBeVisible();
    }
    await expect(
      page.getByRole("button", { name: "Move to space" }),
    ).toBeHidden();
  });
});
