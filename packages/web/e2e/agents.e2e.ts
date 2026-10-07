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

// Activity, Agents, an agent's page, Connect an agent and Settings ›
// Agents and keys on the demo dataset (no session, dev fixtures on):
// keyboard-only flows, then self-baselined screenshots of each board.

skipWithoutBrowser();

const CODEX = "/memax-v2/agents/0192a7c0-0000-7000-8000-0000000000c2";

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

test("autonomy changes from the keyboard alone", async ({ page }) => {
  await page.goto("/memax-v2/agents");
  const claudeCode = page.getByRole("group", { name: "Claude Code" });
  const write = claudeCode.getByRole("radio", { name: "Write" });
  await expect(write).toHaveAttribute("aria-checked", "true");
  // The group is one tab stop, on the checked level.
  await tabTo(page, write);
  await page.keyboard.press("ArrowLeft");
  const propose = claudeCode.getByRole("radio", { name: "Propose" });
  await expect(propose).toBeFocused();
  await expect(propose).toHaveAttribute("aria-checked", "true");
  await expect(
    page.getByText(
      "Claude Code proposes in memax-v2 now. Its writes wait in Review.",
    ),
  ).toBeVisible();

  // A raise waits for the server, then says so.
  const cursor = page.getByRole("group", { name: "Cursor" });
  await tabTo(page, cursor.getByRole("radio", { name: "Read" }));
  await page.keyboard.press("ArrowRight");
  await expect(cursor.getByRole("radio", { name: "Propose" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await expect(
    page.getByText(
      "Cursor proposes in memax-v2 now. Its writes wait in Review.",
    ),
  ).toBeVisible();

  // The agent's page reads the same connection.
  await page.getByRole("link", { name: /^Cursor/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Cursor");
  await expect(
    page
      .getByRole("radiogroup", { name: "Autonomy for Cursor" })
      .getByRole("radio", { name: "Propose" }),
  ).toHaveAttribute("aria-checked", "true");
});

test("Connect an agent opens, traps focus and gives it back", async ({
  page,
  context,
  baseURL,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"], {
    origin: baseURL,
  });
  await page.goto("/memax-v2/agents");
  const open = page.getByRole("button", { name: "Connect an agent" });
  await tabTo(page, open);
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Connect an agent" });
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL("/memax-v2/agents?overlay=connect");
  // It starts on the first agent that isn't connected and runs in a terminal.
  const opencode = dialog.getByRole("radio", { name: /^OpenCode/ });
  await expect(opencode).toBeFocused();
  await expect(opencode).toHaveAttribute("aria-checked", "true");
  await expect(
    dialog.getByText("npx memax-cli connect opencode --space memax-v2"),
  ).toBeVisible();
  // Arrows choose; ↑ jumps a row of four.
  await page.keyboard.press("ArrowUp");
  await expect(dialog.getByRole("radio", { name: /^ChatGPT/ })).toBeFocused();
  await expect(dialog.getByText("4 · Add it in ChatGPT")).toBeVisible();
  // Tab stays inside the layer.
  for (let i = 0; i < 12; i++) await page.keyboard.press("Tab");
  expect(
    await dialog.evaluate((el) => el.contains(document.activeElement)),
  ).toBe(true);
  // Escape closes it and gives focus back to the button that opened it.
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(page).toHaveURL("/memax-v2/agents");
  await expect(open).toBeFocused();

  // ↵ on a choice connects: it copies the command and closes.
  await page.keyboard.press("Enter");
  await expect(dialog).toBeVisible();
  await expect(opencode).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();
  await expect(
    page.getByText(
      "Copied the command. Run it where OpenCode lives, and it shows up here once it connects.",
    ),
  ).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    "npx memax-cli connect opencode --space memax-v2",
  );
  // Its scrim goes with it: the page takes clicks again, and the dialog
  // opens afresh.
  await open.click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  await open.click();
  await expect(dialog).toBeVisible();
});

test("Activity moves row by row, and Enter opens the memory", async ({
  page,
}) => {
  await page.goto("/memax-v2/activity");
  const today = page.getByRole("list", { name: "Today" });
  const first = today.getByRole("listitem").first();
  await tabTo(page, first);
  await expect(first).toContainText("Codex asked you a question");
  await page.keyboard.press("ArrowDown");
  const second = today.getByRole("listitem").nth(1);
  await expect(second).toBeFocused();
  await expect(second).toContainText(
    "Claude Code verified a memory against the code. Still true.",
  );
  await page.keyboard.press("End");
  await expect(
    page
      .getByRole("list", { name: "Saturday, October 3" })
      .getByRole("listitem"),
  ).toBeFocused();
  await page.keyboard.press("Home");
  await expect(first).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/memax-v2/memories/M-0219");
});

test("Disconnect is confirmed inline, and Escape backs out", async ({
  page,
}) => {
  await page.goto(CODEX);
  const disconnect = page.getByRole("button", { name: "Disconnect" });
  await disconnect.click();
  const confirm = page.getByRole("region", { name: "Disconnect Codex?" });
  await expect(confirm).toContainText(
    "Its OAuth grant is revoked now, so its next request is refused.",
  );
  await expect(confirm.getByRole("button", { name: /Cancel/ })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(confirm).toBeHidden();
  await expect(disconnect).toBeFocused();
  await disconnect.click();
  await confirm.getByRole("button", { name: "Disconnect Codex" }).click();
  await expect(
    page.getByText("Disconnected Codex. Its credential is revoked."),
  ).toBeVisible();
  await expect(page.locator(".mx-page-lede")).toHaveText(
    "It's disconnected: its credential is revoked, and Memax refuses its requests.",
  );
});

// Self-baselines of each board's route on the demo dataset.
const SHOTS = [
  {
    name: "activity",
    path: "/memax-v2/activity",
    heading: "Activity",
    height: 1160,
  },
  { name: "agents", path: "/memax-v2/agents", heading: "Agents", height: 980 },
  { name: "agent-codex", path: CODEX, heading: "Codex", height: 1240 },
  {
    name: "connect-agent",
    path: "/memax-v2/agents?overlay=connect",
    heading: "Agents",
    height: 980,
    dialog: true,
  },
  {
    name: "keys",
    path: "/settings/keys",
    heading: "Agents and keys",
    height: 1000,
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
      if ("dialog" in shot) {
        await expect(
          page.getByRole("dialog", { name: "Connect an agent" }),
        ).toBeVisible();
      }
      await settle(page);
      await expect(page).toHaveScreenshot(`${shot.name}-${theme}-1440.png`, {
        maxDiffPixelRatio: 0.002,
      });
    });
  }
}

test("agents on a phone, 390", async ({ page, baseURL }) => {
  test.skip(usingDevServer, "screenshots need the production build");
  await useTheme(page, baseURL, "light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/memax-v2/agents");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Agents");
  await settle(page);
  await expect(page).toHaveScreenshot("agents-light-390.png", {
    maxDiffPixelRatio: 0.002,
  });
});
