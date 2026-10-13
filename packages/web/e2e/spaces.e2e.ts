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

// The space switcher (SpaceSwitcher.png) and New space (NewSpace.png) on
// the demo dataset, by keyboard where the boards say so, landing on the
// new space's EmptySpace; and the places of a space with nothing in it,
// which show its steps. Self-baselined in both themes;
// spaces-boards.e2e.ts measures them against the boards.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const switcher = (page: Page, space = "memax-v2") =>
  page.getByRole("button", { name: `${space}, Switch space` });
const newSpace = (page: Page) =>
  page.getByRole("dialog", { name: "New space" });

async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
}

test("the switcher opens New space by keyboard, and the space is made", async ({
  page,
}) => {
  await open(page, "/memax-v2/today");
  await switcher(page).focus();
  await page.keyboard.press("Enter");
  const menu = page.getByRole("menu");
  await expect(menu).toBeVisible();
  await page.keyboard.press("End");
  await page.keyboard.press("ArrowUp");
  await expect(menu.getByRole("menuitem", { name: "New space" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(newSpace(page)).toBeVisible();

  // A second personal space is said before anything is sent.
  await newSpace(page)
    .getByRole("radio", { name: /^Personal/ })
    .click();
  await expect(
    newSpace(page).getByText(
      "You have one already: Personal. Each person has one.",
    ),
  ).toBeVisible();
  await newSpace(page)
    .getByRole("radio", { name: /^Project/ })
    .click();

  await newSpace(page)
    .getByRole("textbox", { name: "Repository" })
    .fill("MemaxLabs/acme-api");
  await expect(
    newSpace(page).getByRole("textbox", { name: "Name" }),
  ).toHaveValue("acme-api");
  await newSpace(page).getByRole("textbox", { name: "Name" }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/acme-api/today");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Nothing here yet",
  );
  await expect(
    page.getByText("acme-api · Project space · created just now"),
  ).toBeVisible();
  // The switcher lists it, with nothing in it yet.
  await switcher(page, "acme-api").click();
  await expect(page.getByRole("menuitem", { name: /^acme-api/ })).toContainText(
    "Project · new, no agents yet",
  );
});

test("a space with nothing in it shows its steps in every place", async ({
  page,
}) => {
  for (const [place, title] of [
    ["review", "Review"],
    ["memories", "Memories"],
    ["brief", "Brief"],
  ] as const) {
    await open(page, `/memax-web/${place}`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(title);
    await expect(
      page.getByRole("region", { name: "Three steps to a first compile" }),
    ).toBeVisible();
  }
});

for (const theme of ["light", "dark"] as const) {
  test(`the switcher looks like the board (${theme})`, async ({
    page,
    baseURL,
  }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await open(page, "/memax-v2/today");
    await switcher(page).click();
    await expect(page.getByRole("menu")).toBeVisible();
    await page.mouse.move(1300, 980);
    await settle(page);
    await expect(page).toHaveScreenshot(`switcher-${theme}.png`, {
      animations: "disabled",
      caret: "hide",
    });
  });

  test(`New space looks like the board (${theme})`, async ({
    page,
    baseURL,
  }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1000 });
    // The layer may open before the page settles, and a modal hides the
    // rest of the page from the accessibility tree: wait for the layer.
    await page.goto("/memax-v2/today?overlay=new-space");
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    await expect(newSpace(page)).toBeVisible();
    await newSpace(page)
      .getByRole("textbox", { name: "Repository" })
      .fill("MemaxLabs/memax-web");
    await page.mouse.move(1300, 980);
    await settle(page);
    await expect(page).toHaveScreenshot(`new-space-${theme}.png`, {
      animations: "disabled",
      caret: "hide",
    });
  });
}
