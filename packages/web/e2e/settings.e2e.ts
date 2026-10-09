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

// Settings › Notifications and Settings › Security on the demo dataset
// (no session, dev fixtures on): the matrix and quiet hours from the
// keyboard, the nav between them, then self-baselined screenshots of
// each in Paper and Carbon. settings-boards.e2e.ts measures Notifications
// against its board.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

test("notifications change from the keyboard alone", async ({ page }) => {
  await page.goto("/settings/notifications");
  const drift = page.getByRole("checkbox", {
    name: "A compiled file drifted: Email",
  });
  await expect(drift).not.toBeChecked();
  await tabTo(page, drift);
  await page.keyboard.press("Space");
  await expect(drift).toBeChecked();

  // Quiet hours: a new start, saved on Enter.
  const from = page.getByRole("textbox", { name: "From" });
  await tabTo(page, from);
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("21:30");
  await page.keyboard.press("Enter");
  await expect(from).toHaveValue("21:30");
  // A time that isn't one is said, and nothing is sent.
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("9pm");
  await page.keyboard.press("Enter");
  await expect(
    page.getByText("Use a 24-hour time, such as 08:00"),
  ).toBeVisible();

  // The settings nav reaches Security.
  await page.getByRole("link", { name: "Security" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Security");
  await expect(
    page.getByText(/^Not confirmed\. Voyage AI keeps API inputs/),
  ).toBeVisible();
});

const SHOTS = [
  {
    name: "notifications",
    path: "/settings/notifications",
    heading: "Notifications",
    height: 1000,
  },
  {
    name: "security",
    path: "/settings/security",
    heading: "Security",
    height: 2320,
  },
] as const;

for (const shot of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    test(`${shot.name}, ${theme}, 1440`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: 1440, height: shot.height });
      await page.goto(shot.path);
      await expect(page.getByRole("heading", { level: 1 })).toHaveText(
        shot.heading,
      );
      await settle(page);
      await expect(page).toHaveScreenshot(`${shot.name}-${theme}-1440.png`, {
        maxDiffPixelRatio: 0.002,
      });
    });
  }
}

test("notifications on a phone, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/settings/notifications");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "Notifications",
  );
  await settle(page);
  await expect(page).toHaveScreenshot("notifications-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
