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

// Settings › Account (Account.png) on the demo dataset (no session, dev
// fixtures on): the profile, a passkey renamed in its dialog, a session
// signed out, and Forget's dialog that waits for the typed email. Then
// self-baselined screenshots; settings-boards.e2e.ts measures the page
// against its board, and passkeys.e2e.ts adds and uses a real passkey.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });

async function open(page: Page) {
  await page.goto("/settings/account");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Account");
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
}

test("profile, passkeys, sessions and Forget's typed email", async ({
  page,
}) => {
  await open(page);
  const profile = page.getByRole("region", { name: "Profile" });
  const update = profile.getByRole("button", { name: "Update" });
  await expect(update).toBeDisabled();
  await profile.getByRole("textbox", { name: "Name" }).fill("Ziyang Z. Zeng");
  await update.click();
  await expect(toasts(page)).toContainText("Your profile is updated.");

  // The passkey's row opens its dialog: rename it there.
  await page
    .getByRole("region", { name: "Sign in with" })
    .getByRole("button", { name: "Manage" })
    .click();
  const passkeys = page.getByRole("dialog", { name: "Passkeys" });
  const row = passkeys.getByRole("listitem").first();
  await row.getByRole("button", { name: "Rename" }).click();
  await row
    .getByRole("textbox", { name: "Name for iCloud Keychain" })
    .fill("Work laptop");
  await row.getByRole("button", { name: "Done" }).click();
  await expect(toasts(page)).toContainText("Renamed to Work laptop.");
  await page.keyboard.press("Escape");
  await expect(passkeys).toBeHidden();

  // Sign the phone out.
  const sessions = page.getByRole("region", { name: "Signed in on" });
  const phone = sessions.getByText("Safari on iOS").locator("..");
  await phone.getByRole("button", { name: "Sign out" }).click();
  await expect(toasts(page)).toContainText("Safari on iOS is signed out.");
  await expect(sessions.getByText("Safari on iOS")).toHaveCount(0);

  // Forget waits for the account's email, typed.
  await page.getByRole("button", { name: "Forget account" }).click();
  const forget = page.getByRole("dialog", { name: "Forget your account?" });
  const confirm = forget.getByRole("button", { name: "Forget account" });
  await expect(confirm).toBeDisabled();
  await forget
    .getByRole("textbox", { name: "Type ziyang@example.com to confirm" })
    .fill("ziyang@example.com");
  await expect(confirm).toBeEnabled();
  await forget.getByRole("button", { name: "Cancel" }).click();
  await expect(forget).toBeHidden();
  // Its scrim goes with it: the page takes clicks again.
  await profile.getByRole("textbox", { name: "Name" }).click();
  await expect(profile.getByRole("textbox", { name: "Name" })).toBeFocused();
});

for (const theme of ["light", "dark"] as const) {
  test(`account, ${theme}, 1440`, async ({ page, baseURL }) => {
    test.skip(usingDevServer, "screenshots need the production build");
    await useTheme(page, baseURL, theme);
    await page.setViewportSize({ width: 1440, height: 1040 });
    await open(page);
    await settle(page);
    await expect(page).toHaveScreenshot(`account-${theme}-1440.png`, {
      maxDiffPixelRatio: 0.002,
    });
  });
}

test("account on a phone, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await open(page);
  await settle(page);
  await expect(page).toHaveScreenshot("account-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
