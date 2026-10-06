import type { Page } from "@playwright/test";
import { expect, optIntoV2, skipWithoutBrowser, tabTo, test } from "./fixtures";

// The central keymap, end to end: G sequences, ⌘K (ControlOrMeta, so
// it's Ctrl here on Linux) with focus return, the `?` sheet generated
// from the registry, ⌘1–⌘9, Remember and Ask by keyboard alone, Undo,
// and the IME guard.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

/** Waits until the frame has hydrated and the keymap listens. */
async function openFrame(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(
    page.getByRole("navigation", { name: "Main" }).getByRole("status"),
  ).not.toHaveText("");
  // Focus the sheet, as a person clicking into the page would.
  await page.locator("#main").focus();
}

test("G sequences go to the places", async ({ page }) => {
  await openFrame(page, "/memax-v2/agents");
  for (const [second, place] of [
    ["t", "today"],
    ["r", "review"],
    ["b", "brief"],
    ["m", "memories"],
  ] as const) {
    await page.keyboard.press("g");
    await page.keyboard.press(second);
    await expect(page).toHaveURL(`/memax-v2/${place}`);
  }
});

test("R on Today starts Review; N on Memories opens Remember", async ({
  page,
}) => {
  await openFrame(page, "/memax-v2/today");
  await page.keyboard.press("r");
  await expect(page).toHaveURL("/memax-v2/review");
  await openFrame(page, "/memax-v2/memories");
  await page.keyboard.press("n");
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  await expect(dialog.getByRole("textbox", { name: "Remember" })).toBeFocused();
});

test("? opens the sheet the registry generates", async ({ page }) => {
  await openFrame(page, "/memax-v2/review");
  await page.keyboard.press("Shift+Slash");
  const sheet = page.getByRole("dialog", { name: "Keyboard" });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByRole("heading", { level: 3 })).toHaveText([
    "Everywhere",
    "Review",
    "A memory or the Brief",
    "On one page",
  ]);
  const row = (id: string) => sheet.locator(`[data-binding="${id}"]`);
  await expect(row("go.today")).toHaveText("Go to TodayGT");
  await expect(row("command.open")).toHaveText("Ask or rememberCtrl+K");
  await expect(row("space.switch")).toHaveText("Switch spaceCtrl+1toCtrl+9");
  await expect(row("review.move")).toHaveText("Next, previous↓↑");
  await expect(row("memory.forget")).toHaveText(
    "Forgetno shortcut, on purpose",
  );
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
});

test("⌘K opens Ask, Escape closes it, and focus comes back", async ({
  page,
}) => {
  await openFrame(page, "/memax-v2/today");
  const memories = page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link", { name: "Memories" });
  // Shift+Tab from the sheet walks back into the rail.
  await page.locator("#main").focus();
  await tabTo(page, memories, 12, "Shift+Tab");
  await page.keyboard.press("ControlOrMeta+k");
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("textbox", { name: "Ask" })).toBeFocused();
  // Typing G T in the field is text, not navigation.
  await page.keyboard.type("gt");
  await expect(page).toHaveURL("/memax-v2/today");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(memories).toBeFocused();

  // ⌘K from the field closes it too.
  await page.keyboard.press("ControlOrMeta+k");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("ControlOrMeta+k");
  await expect(dialog).toBeHidden();
  await expect(memories).toBeFocused();
});

test("the rail's field opens ⌘K as well", async ({ page }) => {
  await openFrame(page, "/memax-v2/today");
  await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("button", { name: "Ask or remember" })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Ask or remember" }),
  ).toBeVisible();
});

test("Ask streams a cited answer; ⌘↵ keeps it and ⌘Z undoes", async ({
  page,
}) => {
  await openFrame(page, "/memax-v2/today");
  await page.keyboard.press("ControlOrMeta+k");
  await page.keyboard.type("Why did we pick River over Temporal?");
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  await expect(
    dialog.getByText("Answer from memax-v2 · 3 sources"),
  ).toBeVisible();
  await expect(dialog.locator(".mx-answer")).toContainText(
    "it still matches the code.",
  );
  await expect(
    dialog.getByRole("button", { name: "Keep as memory" }),
  ).toBeVisible();
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(dialog).toBeHidden();
  const toasts = page.getByRole("region", { name: "Notifications" });
  await expect(toasts).toContainText("Kept M-0439 · 3 files recompiled");
  await page.keyboard.press("ControlOrMeta+z");
  await expect(toasts).toContainText("Undone: M-0439 isn't kept.");
});

test("Remember shows the near-duplicate, then keeps on Enter", async ({
  page,
}) => {
  await openFrame(page, "/memax-v2/today");
  await page.keyboard.press("ControlOrMeta+k");
  await page.keyboard.press("Tab");
  const dialog = page.getByRole("dialog", { name: "Ask or remember" });
  const field = dialog.getByRole("textbox", { name: "Remember" });
  await expect(field).toBeFocused();
  await page.keyboard.type(
    "Pin shared dependency versions with the pnpm catalog.",
  );
  await expect(
    dialog.getByRole("button", { name: "Keep M-0432" }),
  ).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("region", { name: "Notifications" }),
  ).toContainText(/Kept M-04\d\d · 3 files recompiled/);
});

test("⌘1–⌘9 switch space", async ({ page }) => {
  await openFrame(page, "/memax-v2/review");
  await page.keyboard.press("ControlOrMeta+1");
  await expect(page).toHaveURL("/personal/review");
  await page.locator("#main").focus();
  await page.keyboard.press("ControlOrMeta+4");
  await expect(page).toHaveURL("/memax-team/review");
});

test("the IME guard: keys that compose text never navigate", async ({
  page,
}) => {
  await openFrame(page, "/memax-v2/agents");
  // What a Chinese IME sends while composing: keydowns with isComposing
  // (and keyCode 229, which Safari uses for the committing Enter).
  await page.evaluate(() => {
    for (const key of ["g", "t", "?"]) {
      document.getElementById("main")?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key,
          bubbles: true,
          isComposing: true,
          keyCode: 229,
        }),
      );
    }
  });
  await page.waitForTimeout(300);
  await expect(page).toHaveURL("/memax-v2/agents");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  // The same keys without a composition do navigate.
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect(page).toHaveURL("/memax-v2/today");
});
