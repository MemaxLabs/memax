import type { Page } from "@playwright/test";
import {
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  tabTo,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// Review, Memories and a memory's page on the demo dataset: Review by
// keyboard alone (↓↑, K stamps the seal and moves on, E then keep, X
// with a reason, C to compare), the ? sheet's Review keys, walking
// Memories by keyboard, Copy citation, and self-baselined screenshots
// of the boards' states (Review, ReviewEdit, Memories, Memory).

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });
const reviewLink = (page: Page) =>
  rail(page).getByRole("link", { name: /^Review/ });

/** Opens a page once the frame has hydrated and the keymap listens. */
async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

test("Review by keyboard: move, keep, edit then keep, reject", async ({
  page,
}) => {
  await open(page, "/memax-v2/review");
  await expect(page.getByText("memax-v2 · 1 of 5")).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByText("memax-v2 · 2 of 5")).toBeVisible();
  await page.keyboard.press("ArrowUp");
  await expect(page.getByText("memax-v2 · 1 of 5")).toBeVisible();

  // K stamps the seal at once, then the next card takes its place.
  await page.keyboard.press("k");
  await expect(
    page.getByRole("img", { name: /^Kept, Oct 5, M-0430/ }),
  ).toBeVisible();
  await expect(toasts(page)).toContainText("Kept M-0430 · 3 files recompiled");
  await expect(page.getByText("memax-v2 · 1 of 4")).toBeVisible();
  await expect(reviewLink(page)).toHaveAccessibleName(
    "Review 4 waiting on you",
  );
  // The Keep has a receipt, so it can be undone.
  await expect(toasts(page).getByRole("button", { name: "Undo" })).toHaveCount(
    1,
  );

  // ⌘Z: the card comes back as it was, and the toast says so.
  await page.keyboard.press("ControlOrMeta+z");
  await expect(toasts(page)).toContainText(
    "Undid the keep. M-0430 is back in Review.",
  );
  await expect(page.getByText("memax-v2 · 1 of 5")).toBeVisible();
  await expect(reviewLink(page)).toHaveAccessibleName(
    "Review 5 waiting on you",
  );
  await page.keyboard.press("k");
  await expect(page.getByText("memax-v2 · 1 of 4")).toBeVisible();

  // M-0431 is a conflict (its E compares); E on the next proposal: the
  // statement field takes focus, and typed keys are text, not shortcuts.
  await page.keyboard.press("ArrowDown");
  await expect(page.getByText("memax-v2 · 2 of 4")).toBeVisible();
  await page.keyboard.press("e");
  const field = page.getByRole("textbox", { name: "Statement" });
  await expect(field).toBeFocused();
  await page.keyboard.press("End");
  await page.keyboard.type(" Keep the catalog in the root.");
  await expect(page.getByText("memax-v2 · 2 of 4 · editing")).toBeVisible();
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(toasts(page)).toContainText("Kept M-0432");
  await expect(page.getByText("memax-v2 · 2 of 3")).toBeVisible();

  // X: an optional reason, then ↵.
  await page.keyboard.press("x");
  const why = page.getByRole("textbox", { name: "Why, if you'd like to say" });
  await expect(why).toBeFocused();
  await page.keyboard.type("It belongs in the personal space.");
  await page.keyboard.press("Enter");
  await expect(toasts(page)).toContainText("Rejected M-0433");
  await expect(page.getByText("memax-v2 · 2 of 2")).toBeVisible();
  await expect(reviewLink(page)).toHaveAccessibleName(
    "Review 2 waiting on you",
  );
});

test("Review waits for the judge: the working mark, then the Keep", async ({
  page,
}) => {
  // The team space's M-0445 touches a decision in force, and the demo's
  // judge takes a few seconds with it.
  await open(page, "/memax-team/review");
  const checking = page.getByRole("img", { name: "Checking" });
  await expect(checking).toHaveCount(1);
  // Past the team's two questions (gates.e2e.ts) and M-0444.
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.getByText(/· 4 of 4$/)).toBeVisible();
  await page.keyboard.press("k");
  await expect(
    page.getByText(
      "Checking it against the decision in force. It's kept once the check is done.",
    ),
  ).toBeVisible();
  // No spinner: the static working arc, and no error.
  await expect(toasts(page)).not.toContainText("wasn't kept");
  await expect(toasts(page)).toContainText("Kept M-0445", { timeout: 15_000 });
  await expect(checking).toHaveCount(0);
});

test("edit, then keep, waits for the judge when the words touch a decision", async ({
  page,
}) => {
  await open(page, "/memax-team/review");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(page.getByText(/· 4 of 4$/)).toBeVisible();
  await page.keyboard.press("e");
  const field = page.getByRole("textbox", { name: "Statement" });
  await expect(field).toBeFocused();
  await page.keyboard.press("End");
  await page.keyboard.type(" Two days, not one.");
  await page.keyboard.press("ControlOrMeta+Enter");
  // Saved, not kept: the working mark and the line, then the Keep.
  await expect(
    page.getByText(
      "Checking it against the decision in force. It's kept once the check is done.",
    ),
  ).toBeVisible();
  await expect(toasts(page)).toContainText("Kept M-0445", { timeout: 15_000 });
});

test("? lists Review's keys", async ({ page }) => {
  await open(page, "/memax-v2/review");
  await page.keyboard.press("Shift+Slash");
  const sheet = page.getByRole("dialog", { name: "Keyboard" });
  const row = (id: string) => sheet.locator(`[data-binding="${id}"]`);
  await expect(row("review.move")).toHaveText("Next, previous↓↑");
  await expect(row("review.keep")).toHaveText("KeepK");
  await expect(row("review.edit")).toHaveText("Edit, then keepE");
  await expect(row("review.reject")).toHaveText("RejectX");
  await expect(row("review.compare")).toHaveText("Compare a conflictC");
  await expect(row("review.source")).toHaveText("Open the sourceO");
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
});

test("filters in the URL; C compares a conflict and keeps one answer", async ({
  page,
}) => {
  await open(page, "/memax-v2/review");
  await page.getByRole("radio", { name: "Conflicts 1" }).click();
  await expect(page).toHaveURL("/memax-v2/review?filter=conflicts");
  await expect(page.getByText("memax-v2 · 1 of 1")).toBeVisible();
  await page.locator("#main").focus();
  await page.keyboard.press("c");
  await expect(page).toHaveURL("/memax-v2/review/M-0431/compare");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Fly.io or Railway for the v2 API?",
  );
  const answer = page.getByRole("radiogroup", { name: "Resolution" });
  await expect(answer.getByRole("radio", { checked: true })).toContainText(
    "Both, each with its own scope",
  );
  await page.keyboard.press("1");
  await expect(answer.getByRole("radio", { checked: true })).toContainText(
    "Fly.io everywhere",
  );
  await expect(
    page.getByRole("textbox", { name: "The decision, as it will read" }),
  ).toHaveValue("Deploy the v2 API to Fly.io in iad and ams.");
  // "Both" narrows each side, as the server does: two fields.
  await page.keyboard.press("3");
  await expect(
    page.getByRole("textbox", { name: "M-0431, as it will read" }),
  ).toHaveValue("The v2 API runs on Fly.io in iad and ams.");
  await page.keyboard.press("Enter");
  await expect(toasts(page)).toContainText(
    "Kept M-0431 and M-0174, each narrowed",
  );
  await expect(page).toHaveURL("/memax-v2/review");
  await expect(toasts(page).getByRole("button", { name: "Undo" })).toHaveCount(
    1,
  );
});

test("an empty queue is one panel across the sheet", async ({ page }) => {
  await open(page, "/memax-web/review");
  await expect(page.getByText("No proposals waiting.")).toBeVisible();
  await expect(
    page.getByRole("complementary", { name: "Waiting on you" }),
  ).toHaveCount(0);
  await open(page, "/personal/review");
  await expect(
    page.getByText("Last review today at 09:41 · 4 kept, 1 rejected"),
  ).toBeVisible();
});

test("Memories: filters, search, more, and rows by keyboard", async ({
  page,
}) => {
  await open(page, "/memax-v2/memories");
  const panel = page.getByRole("region", { name: "Memories" });
  await expect(panel.getByRole("heading", { level: 2 })).toHaveText([
    "Decisions",
    "Conventions",
    "Preferences",
  ]);
  await expect(page.getByText("Showing 11 of 222")).toBeVisible();

  // ↓↑ walk the rows' links; ↵ opens one.
  const first = panel.getByRole("link").first();
  await tabTo(page, first);
  await page.keyboard.press("ArrowDown");
  await expect(panel.getByRole("link").nth(1)).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/memax-v2/memories/M-0431");
  await page.goBack();

  // Show 50 more: focus lands on the first row that arrived.
  await page.getByRole("button", { name: "Show 50 more" }).click();
  await expect(page.getByText("Open question")).toBeVisible();
  await expect(page.locator(".mx-row-link:focus")).toHaveCount(1);

  await page.getByRole("radio", { name: "Waiting 5" }).click();
  await expect(page).toHaveURL("/memax-v2/memories?filter=waiting");
  await page.getByRole("searchbox", { name: "Search memories" }).fill("pnpm");
  await expect(panel.getByRole("link")).toHaveText([
    "Pin shared dependency versions with the pnpm catalog.",
  ]);
});

test("a memory's page: Copy citation, Edit, and not found", async ({
  page,
  context,
  baseURL,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await open(page, "/memax-v2/memories/M-0219");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Background jobs run on River, Postgres‑backed. We do not use Temporal.",
  );
  await expect(
    page.getByRole("img", { name: /^Kept, Oct 2, M-0219/ }),
  ).toBeVisible();
  await page.keyboard.press("ControlOrMeta+Shift+C");
  await expect(toasts(page)).toContainText("Copied [M-0219] and its link");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    `[M-0219] ${baseURL}/memax-v2/memories/M-0219`,
  );
  // Forget is there, has no key, and says why it isn't available yet.
  await expect(page.getByRole("button", { name: "Forget" })).toHaveAttribute(
    "title",
    "Forget arrives with propagation",
  );
  await page.locator("#main").focus();
  await page.keyboard.press("e");
  await expect(page.getByRole("textbox", { name: "Statement" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("textbox", { name: "Statement" })).toHaveCount(0);

  await open(page, "/memax-v2/memories/M-9999");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "There's no memory with this ID that you can open.",
  );
});

// Self-baselines: the boards' states on the demo dataset.
const SHOTS = [
  { name: "review", path: "/memax-v2/review", width: 1440, height: 920 },
  {
    name: "review-edit",
    path: "/memax-v2/review",
    width: 1440,
    height: 920,
    edit: true,
  },
  { name: "memories", path: "/memax-v2/memories", width: 1440, height: 1100 },
  {
    name: "memory",
    path: "/memax-v2/memories/M-0219",
    width: 1440,
    height: 1020,
  },
  {
    name: "compare",
    path: "/memax-v2/review/M-0431/compare",
    width: 1440,
    height: 1100,
  },
] as const;

for (const shot of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    test(`${shot.name}, ${theme}, ${shot.width}`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: shot.width, height: shot.height });
      await open(page, shot.path);
      if ("edit" in shot) {
        await page.keyboard.press("e");
        const field = page.getByRole("textbox", { name: "Statement" });
        await field.fill(
          "MCP write tools ask for confirmation with input_required when a person is present. Otherwise the write becomes a proposal.",
        );
        await page
          .getByRole("textbox", { name: "Why you changed it" })
          .fill(
            "Cloud agents run with nobody watching, so they can't confirm in place.",
          );
        await field.focus();
      }
      await settle(page);
      await expect(page).toHaveScreenshot(
        `records-${shot.name}-${theme}-${shot.width}.png`,
        {
          maxDiffPixelRatio: 0.002,
        },
      );
    });
  }
}

test("review, light, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-v2/review");
  await expect(page.getByText("memax-v2 · 1 of 5")).toBeVisible();
  await settle(page);
  await expect(page).toHaveScreenshot("records-review-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
