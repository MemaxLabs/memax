import {
  cookieValue,
  expect,
  skipWithoutBrowser,
  tabTo,
  test,
} from "./fixtures";

// Keyboard smoke test of the dev UI toggle: the memax_ui gate, the
// /dev/ui switch, and the specimen's theme control and V1 link, driven
// with Tab, the arrow keys and Enter only.

skipWithoutBrowser();

test("V2 paths send a browser without memax_ui=v2 to the V1 home", async ({
  page,
}) => {
  for (const path of [
    "/dev/ledger/tokens",
    "/memax-v2/today",
    "/setup/import",
  ]) {
    await page.goto(path);
    await expect(page).toHaveURL("/");
  }
  // Sign-in and device confirmation are open to everyone: the CLI sends
  // any person to /device, and V1 has neither page.
  for (const path of ["/signin", "/device"]) {
    await page.goto(path);
    await expect(page).toHaveURL(path);
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

  // Paper / Carbon / System is a Ledger Segmented: one tab stop (the
  // checked option), arrow keys to choose.
  const html = page.locator("html");
  const carbon = page.getByRole("radio", { name: "Carbon" });
  const system = page.getByRole("radio", { name: "System" });
  await expect(system).toHaveAttribute("aria-checked", "true");
  await tabTo(page, system);
  await page.keyboard.press("ArrowLeft");
  await expect(carbon).toBeFocused();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(carbon).toHaveAttribute("aria-checked", "true");
  expect(await cookieValue(page, "memax_theme")).toBe("dark");

  // The choice survives a reload, applied before hydration.
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(carbon).toHaveAttribute("aria-checked", "true");

  await tabTo(page, carbon);
  await page.keyboard.press("ArrowRight");
  await expect(system).toHaveAttribute("aria-checked", "true");
  await expect(html).toHaveAttribute("data-theme", "light");
  expect(await cookieValue(page, "memax_theme")).toBeUndefined();

  await tabTo(page, page.getByRole("link", { name: "Switch to V1" }));
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/");
  expect(await cookieValue(page, "memax_ui")).toBeUndefined();

  await page.goto("/dev/ledger/tokens");
  await expect(page).toHaveURL("/");
});
