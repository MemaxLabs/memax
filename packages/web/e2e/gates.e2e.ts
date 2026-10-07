import type { Page } from "@playwright/test";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// Decision gates on the demo dataset: the Memax team space's two
// questions (Ledger.png's open questions) head Review's queue and
// Today's "Waiting on you". Answered by keyboard alone (a digit, ↵ to
// confirm, ↵ again), withdrawn inline, opened from Today and Activity,
// and self-baselined: a gate card in Review, and Today's panel, in
// Paper and Carbon at 1440 and in Paper at 390.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });
const gate = (page: Page) => page.locator(".mx-gate");

/** Opens a page once the frame has hydrated and the keymap listens. */
async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

/** The demo's judge settles M-0445 after a few seconds: wait, so rows don't change mid-capture. */
async function judged(page: Page) {
  await expect(page.getByRole("img", { name: "Checking" })).toHaveCount(0, {
    timeout: 15_000,
  });
}

test("Review answers a question by keyboard, then the next card", async ({
  page,
}) => {
  await open(page, "/memax-team/review");
  await expect(page.getByText("Memax team · 1 of 4")).toBeVisible();
  // The rail counts the questions with the proposals.
  await expect(
    rail(page).getByRole("link", { name: /^Review/ }),
  ).toHaveAccessibleName("Review 4 waiting on you");
  await expect(gate(page)).toContainText("Claude Code is waiting on you");
  await page.keyboard.press("2");
  await expect(gate(page).getByRole("radio").nth(1)).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await page.keyboard.press("Enter");
  await expect(gate(page).getByRole("status")).toHaveText(
    "Answer “Align with the core set”? This becomes a kept decision by you, and compiles into every file.",
  );
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("img", { name: /^Kept, Oct 5, M-0439/ }),
  ).toBeVisible();
  await expect(toasts(page)).toContainText(
    "Kept M-0439 as your answer to G-0011",
  );
  await expect(
    toasts(page).getByRole("button", { name: "Open M-0439" }),
  ).toBeVisible();
  await expect(toasts(page).getByRole("button", { name: "Undo" })).toHaveCount(
    0,
  );
  await expect(page.getByText("Memax team · 1 of 3")).toBeVisible();
  await expect(gate(page)).toContainText("Codex is waiting on you");
  await expect(
    rail(page).getByRole("link", { name: /^Review/ }),
  ).toHaveAccessibleName("Review 3 waiting on you");
  // The decision is kept, in Memories, by you.
  await toasts(page).getByRole("button", { name: "Open M-0439" }).click();
  await expect(page).toHaveURL("/memax-team/memories/M-0439");
});

test("Withdraw is quiet and confirmed inline; Esc backs out", async ({
  page,
}) => {
  await open(page, "/memax-team/review");
  await gate(page).getByRole("button", { name: "Withdraw" }).click();
  await expect(gate(page).getByRole("status")).toContainText(
    "Withdraw G-0011?",
  );
  await page.keyboard.press("Escape");
  await expect(gate(page).getByRole("status")).toHaveCount(0);
  await gate(page).getByRole("button", { name: "Withdraw" }).click();
  await gate(page).getByRole("button", { name: "Withdraw" }).click();
  await expect(toasts(page)).toContainText(
    "Withdrew G-0011. Claude Code hears it on its next read.",
  );
  await expect(page.getByText("Memax team · 1 of 3")).toBeVisible();
});

test("Today lists the questions, and one opens its card in Review", async ({
  page,
}) => {
  await open(page, "/memax-team/today");
  const waiting = page.getByRole("region", { name: /^Waiting on you/ });
  await expect(waiting).toContainText("2 questions · 2 proposals");
  await waiting
    .getByRole("link", { name: "Which deploy target should the v2 API use?" })
    .click();
  await expect(page).toHaveURL("/memax-team/review?gate=G-0012");
  await expect(page.getByText("Memax team · 2 of 4")).toBeVisible();
  await expect(gate(page)).toContainText("Codex is waiting on you");
});

test("Activity's asked row opens the question", async ({ page }) => {
  await open(page, "/memax-team/activity");
  await page.getByRole("link", { name: "G-0012" }).click();
  await expect(page).toHaveURL("/memax-team/review?gate=G-0012");
  await expect(gate(page)).toContainText(
    "Which deploy target should the v2 API use?",
  );
});

for (const theme of ["light", "dark"] as const) {
  test(`a gate card in Review, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1000 });
    // Codex's deploy-target question, as the DecisionGate preview draws it.
    await open(page, "/memax-team/review?gate=G-0012");
    await expect(page.getByText("Memax team · 2 of 4")).toBeVisible();
    await page.keyboard.press("1");
    await judged(page);
    await settle(page);
    await expect(page).toHaveScreenshot(`gates-review-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });

  test(`Today's Waiting on you with questions, ${theme}, 1440`, async ({
    page,
    baseURL,
  }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await open(page, "/memax-team/today");
    const waiting = page.getByRole("region", { name: /^Waiting on you/ });
    await expect(waiting).toContainText("G-0012");
    await settle(page);
    await expect(waiting).toHaveScreenshot(`gates-today-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}

test("a gate card in Review, light, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-team/review?gate=G-0012");
  await expect(page.getByText("Memax team · 2 of 4")).toBeVisible();
  await page.locator("#main").focus();
  await page.keyboard.press("1");
  await settle(page);
  await expect(page).toHaveScreenshot("gates-review-light-390.png", {
    maxDiffPixelRatio: 0.002,
    fullPage: true,
  });
});

test("Today's Waiting on you with questions, light, 390", async ({
  page,
  baseURL,
}) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-team/today");
  const waiting = page.getByRole("region", { name: /^Waiting on you/ });
  await expect(waiting).toContainText("G-0012");
  await settle(page);
  await expect(waiting).toHaveScreenshot("gates-today-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
