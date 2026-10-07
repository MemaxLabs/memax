import type { Page } from "@playwright/test";
import {
  cookieValue,
  expect,
  optIntoV2,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// The first session (Phase 2 epic 2.1) on the demo dataset: the CLI's
// device code confirmed by keyboard, init's import followed from FirstRun
// into the cleanup and settled, what agrees kept in bulk, the routing
// around them, and self-baselines of each screen in Paper and Carbon.

skipWithoutBrowser();

const IMPORT = "0192a7c0-0000-7000-8000-0000000001a1";

/** Opens a page once it has hydrated, so the keymap listens. */
async function open(page: Page, path: string, ready: string | RegExp) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(ready);
}

test.describe("with the opt-in", () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await optIntoV2(page, baseURL);
  });

  test("CliAuth confirms the device's code with Enter", async ({ page }) => {
    await open(page, "/device?code=wqrt-4821", "Connect the memax CLI");
    await expect(
      page.getByRole("group", { name: "Code WQRT-4821" }),
    ).toBeVisible();
    await expect(
      page.getByText("12 seconds ago, from 203.0.113.4"),
    ).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Your terminal is signing in.",
    );
    // The demo CLI collects its session a moment later.
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "The memax CLI is signed in.",
      { timeout: 10_000 },
    );
  });

  test("CliAuth asks for a code when the link has none", async ({ page }) => {
    await open(page, "/device", "Connect the memax CLI");
    await page.getByLabel("Code").fill("wqrt 4821");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL("/device?code=WQRT-4821");
    await expect(
      page.getByRole("group", { name: "Code WQRT-4821" }),
    ).toBeVisible();
  });

  test("FirstRun goes on to the cleanup, which settles by keyboard", async ({
    page,
  }) => {
    await open(page, "/setup", "Give every agent the same context.");
    await expect(page).toHaveURL("/setup/import");
    await expect(page.getByRole("region", { name: /zsh/ })).toContainText(
      "6 files, 3 conflicts, 1 Brief.",
    );
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(
      `/setup/cleanup?space=memax-v2&import=${IMPORT}`,
    );
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "6 files, 3 conflicts, 1 Brief.",
    );
    await page.keyboard.press("1");
    await expect(
      page
        .getByRole("radiogroup", { name: "Test command" })
        .getByRole("radio")
        .first(),
    ).toHaveAttribute("aria-checked", "true");
    await page.keyboard.press("Enter");
    await expect(
      page.getByText("Kept M-0501. The others were rejected."),
    ).toBeVisible();
    // R reviews the rest.
    await page.keyboard.press("r");
    await expect(page).toHaveURL(
      `/memax-v2/review?filter=import&import=${IMPORT}`,
    );
  });

  test("ReviewImport keeps what agrees with K", async ({ page }) => {
    await open(
      page,
      "/memax-v2/review?filter=import",
      "33 imports from 6 files",
    );
    await page
      .getByRole("checkbox", { name: "Select the 25 that can be kept in bulk" })
      .check();
    await expect(page.getByText("25 selected")).toBeVisible();
    await page.locator("#main").focus();
    await page.keyboard.press("k");
    await expect(
      page.getByText("Kept 25. Every file recompiles."),
    ).toBeVisible();
    await expect(
      page.getByText("Select the 0 that can be kept in bulk"),
    ).toBeVisible();
  });

  test("Connect's primary action goes on to FirstRun", async ({ page }) => {
    await open(
      page,
      "/setup/agents?space=memax-v2",
      "Which agents should share this context?",
    );
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL("/setup/import?space=memax-v2");
  });
});

test.describe("without the opt-in", () => {
  test("the device sign-in opens for any browser", async ({ page }) => {
    await page.goto("/device?code=WQRT-4821");
    await expect(page).toHaveURL("/device?code=WQRT-4821");
    await expect(
      page.getByRole("group", { name: "Code WQRT-4821" }),
    ).toBeVisible();
    expect(await cookieValue(page, "memax_ui")).toBeUndefined();
    await page.goto("/signin");
    await expect(
      page.getByRole("heading", { level: 2, name: "Sign in" }),
    ).toBeVisible();
  });

  test("the setup screens stay behind it: signed out, they ask to sign in", async ({
    page,
  }) => {
    await page.goto("/setup/import");
    await expect(page).toHaveURL(
      `/signin?${new URLSearchParams({ next: "/setup/import" })}`,
    );
    expect(await cookieValue(page, "memax_ui")).toBeUndefined();
  });
});

// Self-baselines of each screen, at the board's size, in both themes.
const SHOTS = [
  {
    name: "signin",
    path: "/signin",
    heading: "The context layer you own.",
    height: 900,
  },
  {
    name: "cliauth",
    path: "/device?code=WQRT-4821",
    heading: "Connect the memax CLI",
    height: 900,
  },
  {
    name: "connect",
    path: "/setup/agents?space=memax-v2",
    heading: "Which agents should share this context?",
    height: 960,
  },
  {
    name: "firstrun",
    path: "/setup/import?space=memax-v2",
    heading: "Give every agent the same context.",
    height: 900,
  },
  {
    name: "firstrun-waiting",
    path: "/setup/import?space=memax-web",
    heading: "Give every agent the same context.",
    height: 900,
  },
  {
    name: "cleanup",
    path: "/setup/cleanup?space=memax-v2",
    heading: "6 files, 3 conflicts, 1 Brief.",
    height: 1120,
  },
  {
    name: "review-import",
    path: "/memax-v2/review?filter=import",
    heading: "33 imports from 6 files",
    height: 1000,
  },
  {
    name: "compile-done",
    path: "/setup/done?space=memax-v2",
    heading: "memax-v2 is compiled into 3 files.",
    height: 1040,
  },
] as const;

for (const shot of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    test(`${shot.name}, ${theme}, 1440`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await optIntoV2(page, baseURL);
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: 1440, height: shot.height });
      await open(page, shot.path, shot.heading);
      await settle(page);
      await expect(page).toHaveScreenshot(
        `onboarding-${shot.name}-${theme}-1440.png`,
        { maxDiffPixelRatio: 0.002 },
      );
    });
  }
}
