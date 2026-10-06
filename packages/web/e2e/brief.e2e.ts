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

// The Brief, its editor and history, a compiled file, a hand edit and
// Today on the demo dataset, by keyboard alone: ⌘⇧S compiles, a
// compiled file's settings change in place, 1/2/3 and ↵ resolve a hand
// edit (and Review gets the proposals), ⌥↑ regroups a fact and Done
// keeps a new version, Restore is confirmed inline, R starts Review.
// Then self-baselined screenshots of each board.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });

/** Opens a page once the frame has hydrated and the keymap listens. */
async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

test("the Brief: facts in order, receipts in the margin, where it compiles", async ({
  page,
}) => {
  await open(page, "/memax-v2/brief");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Memax V2 engineering brief",
  );
  await expect(page.getByText("rewritten by Dream at 03:12")).toBeVisible();
  await expect(page.getByRole("heading", { level: 2 })).toContainText([
    "What this is",
    "Decisions",
    "Conventions",
    "Open",
    "Compiled to",
    "Read this week",
    "Sources",
  ]);
  // The stale fact's Verify says why it isn't available yet.
  await expect(page.getByRole("button", { name: "Verify" })).toHaveAttribute(
    "title",
    /isn't available yet/,
  );
  // D2/D3: AGENTS.md is canonical; ChatGPT is live, never "in sync".
  const compiled = page.getByRole("region", { name: "Compiled to" });
  await expect(compiled.getByRole("link")).toHaveText([
    "AGENTS.mdIn sync",
    "CLAUDE.mdIn sync",
    ".cursor/rules/memax-packages-web.mdcDrifted",
    "ChatGPT projectLive over connector",
    "Cursor's copy has one local edit.",
  ]);
  await expect(rail(page).getByRole("status")).toHaveText(
    "Cursor file drifted",
  );
});

test("⌘⇧S compiles; a compiled file's settings change in place", async ({
  page,
}) => {
  await open(page, "/memax-v2/brief");
  await page.keyboard.press("ControlOrMeta+Shift+S");
  await expect(toasts(page)).toContainText("Compiling 4 files");
  const compiled = page.getByRole("region", { name: "Compiled to" });
  await expect(compiled.getByText("Compiling").first()).toBeVisible();
  await expect(compiled.getByText("In sync")).toHaveCount(2);

  // To AGENTS.md by keyboard.
  const agents = compiled.getByRole("link", { name: /^AGENTS\.md/ });
  await tabTo(page, agents, 160);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/memax-v2/brief/targets/agents-md");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("AGENTS.md");
  const file = page.getByRole("region", { name: "Contents of AGENTS.md" });
  await expect(file).toContainText("# Memax V2 engineering brief");
  await expect(file).toContainText(
    "- (Being verified) Ask memax answers with the Haiku tier. [M-0187]",
  );
  await expect(
    page.getByText("Written locally by the memax CLI"),
  ).toBeVisible();

  // Stale facts: Leave out, with the arrows.
  const stale = page.getByRole("radiogroup", { name: "Stale facts" });
  await tabTo(page, stale.getByRole("radio", { name: "Mark them" }), 80);
  await page.keyboard.press("ArrowRight");
  await expect(stale.getByRole("radio", { name: "Leave out" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await expect(toasts(page)).toContainText("Changed how AGENTS.md is written");
  await expect(file).not.toContainText("(Being verified)");

  // The size budget, typed.
  const budget = page.getByRole("textbox", { name: "Size budget" });
  await tabTo(page, budget, 20);
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.type("20");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/of a 20 KB budget/)).toBeVisible();
  // Pull requests aren't available yet, and say so.
  await expect(
    page
      .getByRole("radiogroup", { name: "Delivery" })
      .getByRole("radio", { name: "Pull request" }),
  ).toHaveAttribute("title", "Pull requests aren't available yet.");
});

test("a hand edit: 2 asks to overwrite, 1 then ↵ pulls it into Review", async ({
  page,
}) => {
  await open(page, "/memax-v2/brief/targets/cursor-mdc/drift");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Cursor's rules file has a hand edit",
  );
  await expect(page.getByText("Would update M-0441")).toBeVisible();
  await expect(page.getByText("2 proposals if you pull it")).toBeVisible();
  const choice = page.getByRole("radiogroup", {
    name: "Resolve the hand edit",
  });
  await page.keyboard.press("2");
  await expect(
    choice.getByRole("radio", { name: /Overwrite with the Brief/ }),
  ).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Enter");
  // Overwrite asks first; the safe Cancel takes the focus.
  const confirm = page
    .getByRole("alert")
    .filter({ hasText: "Overwrite the hand edit?" });
  await expect(confirm).toBeVisible();
  await expect(page.getByRole("button", { name: "Cancel" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(confirm).toHaveCount(0);

  await page.locator("#main").focus();
  await page.keyboard.press("1");
  await expect(
    choice.getByRole("radio", { name: /Pull the edit into the Brief/ }),
  ).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "Pulled into Review" }),
  ).toBeVisible();
  await expect(toasts(page)).toContainText("Pulled 2 edits into Review");
  await expect(rail(page).getByRole("status")).toHaveText("3 files in sync");

  const review = page.getByRole("link", { name: "Open Review" }).first();
  await tabTo(page, review, 60);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/memax-v2/review");
  await expect(page.getByText("memax-v2 · 1 of 7")).toBeVisible();
  await expect(
    rail(page).getByRole("link", { name: /^Review/ }),
  ).toHaveAccessibleName("Review 7 waiting on you");
});

test("editing the Brief: ⌥↑ regroups, edit in place, a new fact, Done", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await open(page, "/memax-v2/brief");
  await page.keyboard.press("e");
  await expect(page).toHaveURL("/memax-v2/brief/edit");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Editing the Brief",
  );

  // M-0112 up past the top of Conventions: into Decisions.
  const m0112 = page.locator('[data-fact="M-0112"]');
  await m0112.focus();
  await page.keyboard.press("Alt+ArrowUp");
  await page.keyboard.press("Alt+ArrowUp");
  await expect(m0112).toBeFocused();
  await expect(page.getByText("Moved here from Conventions")).toBeVisible();
  const changes = page.getByRole("region", { name: /^Changes/ });
  await expect(changes).toContainText("Moved · M-0112 to Decisions");

  // M-0102's words, in place: ↵ edits, ⌘↵ keeps them for Done.
  const m0102 = page.locator('[data-fact="M-0102"]');
  await tabTo(page, m0102, 30, "Shift+Tab");
  await page.keyboard.press("Enter");
  const words = page.getByRole("textbox", { name: "Edit fact M-0102" });
  await expect(words).toBeFocused();
  await page.keyboard.press("End");
  await page.keyboard.type(" Sessions are never pinned to a machine.");
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(words).toHaveCount(0);
  await expect(changes).toContainText("Edited · M-0102");

  // A new fact in Conventions: its "Add a fact" shows once the focus is
  // in the section, so Tab from its heading to the button.
  await page.getByRole("textbox", { name: "Heading of Conventions" }).focus();
  for (let i = 0; i < 40; i++) {
    const at = await page.evaluate(() => ({
      text: document.activeElement?.textContent ?? "",
      section: document.activeElement
        ?.closest("section")
        ?.querySelector("input")
        ?.getAttribute("aria-label"),
    }));
    if (at.text === "Add a fact" && at.section === "Heading of Conventions") {
      break;
    }
    await page.keyboard.press("Tab");
  }
  await page.keyboard.press("Enter");
  const fresh = page.getByRole("textbox", {
    name: "Add a fact to Conventions",
  });
  await expect(fresh).toBeFocused();
  await page.keyboard.type(
    "Pin shared dependency versions with the pnpm catalog.",
  );
  await expect(changes).toContainText("Added · Conventions");
  await expect(
    page.getByRole("button", { name: "Discard 3 changes" }),
  ).toBeVisible();

  // ⌘↵ is Done, even from the field.
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(page).toHaveURL("/memax-v2/brief");
  await expect(toasts(page)).toContainText("Kept your edits as B-0044");
  await expect(page.getByText("revised by you at 14:40")).toBeVisible();
  await expect(
    page.locator('[data-ref="M-0112"]').locator("xpath=preceding::h2[1]"),
  ).toHaveText("Decisions");
  await expect(
    page
      .getByRole("article")
      .getByText("Pin shared dependency versions with the pnpm catalog."),
  ).toBeVisible();
});

test("history: a version's changes, and Restore confirmed inline", async ({
  page,
}) => {
  await open(page, "/memax-v2/brief/history");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("History");
  const versions = page.getByRole("navigation", { name: "Versions" });
  await expect(versions.getByRole("button")).toHaveText([
    /Dream rewrote itToday, 03:12 · 3 changes$/,
    /You edited one factOct 4, 16:02 · 1 change$/,
    /You added M-0219Oct 2, 10:58 · 1 added$/,
    /You added M-0102Sep 30, 18:40 · 1 added$/,
    /Jiahao added M-0098Sep 29, 11:20 · 1 added$/,
    /The first BriefSep 28, 15:20 · 7 facts$/,
  ]);
  const detail = page.getByRole("region", { name: "B-0043" });
  await expect(detail).toContainText("Reworded");
  await expect(detail).toContainText("Flagged stale");
  await expect(detail).toContainText(
    "M-0436 · written by Claude Code, which may write here",
  );

  await tabTo(
    page,
    versions.getByRole("button", { name: /You edited one fact/ }),
    40,
  );
  await page.keyboard.press("Enter");
  const before = page.getByRole("region", { name: "B-0042" });
  await expect(before).toContainText("Reworded");
  const restore = before.getByRole("button", { name: "Restore this version" });
  await tabTo(page, restore, 20);
  await page.keyboard.press("Enter");
  await expect(before).toContainText("Restore B-0042?");
  await expect(before.getByRole("button", { name: "Cancel" })).toBeFocused();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");
  await expect(toasts(page)).toContainText("Restored B-0042 as B-0044");
});

test("Today: real counts, what's waiting, compiled context, R", async ({
  page,
}) => {
  await open(page, "/memax-v2/today");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "Monday, October 5",
  );
  await expect(
    page.getByText(
      "Overnight, Dream folded 34 notes into 6 facts. Four proposals, one stale fact and a question from Codex are waiting on you.",
    ),
  ).toBeVisible();
  const waiting = page.getByRole("region", { name: /^Waiting on you/ });
  await expect(waiting.getByRole("listitem")).toHaveCount(3);
  await expect(waiting).toContainText("4 proposals · 1 to verify");
  await expect(
    waiting.getByRole("link", { name: "2 more in Review" }),
  ).toBeVisible();
  const context = page.getByRole("region", { name: "Compiled context" });
  await expect(context.getByRole("link")).toHaveText([
    "AGENTS.mdIn sync",
    "CLAUDE.mdIn sync",
    ".cursor/rules/memax-packages-web.mdc1 local edit",
    "ChatGPT projectLive over connector",
  ]);
  await expect(page.getByRole("button", { name: "Hand off" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await page.keyboard.press("r");
  await expect(page).toHaveURL("/memax-v2/review");
});

test("a new space shows three steps to a first compile", async ({ page }) => {
  await open(page, "/memax-web/today");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Nothing here yet",
  );
  await expect(
    page.getByText("npx memax-cli init --space memax-web"),
  ).toBeVisible();
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: "Connect an agent in this repository" }),
  ).toHaveAttribute("aria-current", "step");
});

// Self-baselines: each board on the demo dataset.
const SHOTS = [
  { name: "brief", path: "/memax-v2/brief", height: 1500 },
  {
    name: "brief-edit",
    path: "/memax-v2/brief/edit",
    height: 1300,
    edit: true,
  },
  { name: "brief-history", path: "/memax-v2/brief/history", height: 1100 },
  { name: "target", path: "/memax-v2/brief/targets/agents-md", height: 1240 },
  {
    name: "drift",
    path: "/memax-v2/brief/targets/cursor-mdc/drift",
    height: 1100,
  },
  { name: "today", path: "/memax-v2/today", height: 1000 },
] as const;

/** BriefEdit.png's three changes: an edit, a move, a new fact. */
async function editLikeTheBoard(page: Page) {
  await page.locator('[data-fact="M-0112"]').focus();
  await page.keyboard.press("Alt+ArrowUp");
  await page.keyboard.press("Alt+ArrowUp");
  // "Add a fact" shows once the section has the focus.
  await page.getByRole("textbox", { name: "Heading of Conventions" }).focus();
  await page
    .locator('section:has(input[aria-label="Heading of Conventions"])')
    .getByRole("button", { name: "Add a fact" })
    .click();
  await page.keyboard.type(
    "Pin shared dependency versions with the pnpm catalog.",
  );
  await page.locator('[data-fact="M-0102"]').focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("End");
  await page.keyboard.type(" Sessions are never pinned to a machine.");
}

for (const shot of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    test(`${shot.name}, ${theme}, 1440`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: 1440, height: shot.height });
      await open(page, shot.path);
      if ("edit" in shot) await editLikeTheBoard(page);
      await settle(page);
      await expect(page).toHaveScreenshot(`${shot.name}-${theme}-1440.png`, {
        maxDiffPixelRatio: 0.002,
      });
    });
  }
}

test("today, light, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-v2/today");
  await expect(
    page.getByText("Four proposals and a question are waiting on you."),
  ).toBeVisible();
  await settle(page);
  await expect(page).toHaveScreenshot("today-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
