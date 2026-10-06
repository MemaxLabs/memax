import {
  cookieValue,
  expect,
  skipWithoutBrowser,
  tabTo,
  test,
} from "./fixtures";

// Keyboard smoke test of the dev UI toggle: the memax_ui gate, the
// /dev/ui switch, and the specimen's theme control and V1 link, driven
// with Tab, Enter and Space only.

skipWithoutBrowser();

test("V2 paths send a browser without memax_ui=v2 to the V1 home", async ({
  page,
}) => {
  for (const path of ["/dev/ledger/tokens", "/memax-v2/today", "/signin"]) {
    await page.goto(path);
    await expect(page).toHaveURL("/");
  }
});

test("switch to V2, change theme and switch back, by keyboard", async ({
  page,
}) => {
  await page.goto("/dev/ui?v=2");
  await expect(page).toHaveURL("/dev/ledger/tokens");
  expect(await cookieValue(page, "memax_ui")).toBe("v2");
  await expect(
    page.getByRole("heading", { level: 1, name: "Tokens and type" }),
  ).toBeVisible();

  const html = page.locator("html");
  const carbon = page.getByRole("button", { name: "Carbon" });
  await tabTo(page, carbon);
  await page.keyboard.press("Enter");
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(carbon).toHaveAttribute("aria-pressed", "true");
  expect(await cookieValue(page, "memax_theme")).toBe("dark");

  // The choice survives a reload, applied before hydration.
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(carbon).toHaveAttribute("aria-pressed", "true");

  const system = page.getByRole("button", { name: "System" });
  await tabTo(page, system);
  await page.keyboard.press("Space");
  await expect(system).toHaveAttribute("aria-pressed", "true");
  await expect(html).toHaveAttribute("data-theme", "light");
  expect(await cookieValue(page, "memax_theme")).toBeUndefined();

  await tabTo(page, page.getByRole("link", { name: "Switch to V1" }));
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/");
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();

  await page.goto("/dev/ledger/tokens");
  await expect(page).toHaveURL("/");
});
