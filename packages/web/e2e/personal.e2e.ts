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

// A Personal space written to two machines (personal-layer design §8),
// on the demo dataset: ziyang-mbp is online with a hand edit in Codex's
// block; home-linux has been offline since Friday, a compile behind, its
// canonical copy stopped. Its files are listed once each across the
// machines, each machine's copy has its page and a switcher, a hand
// edit resolves per machine, and the machines rename and stop compiling
// in place, by keyboard. No board draws these yet, so the screenshots
// are self-baselines in Paper and Carbon.

skipWithoutBrowser();

test.beforeEach(async ({ page, baseURL }) => {
  await optIntoV2(page, baseURL);
});

const rail = (page: Page) => page.getByRole("navigation", { name: "Main" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: "Notifications" });

async function open(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator("html")).toHaveAttribute("data-theme", /.+/);
  await expect(rail(page).getByRole("status")).not.toHaveText("");
  await page.locator("#main").focus();
}

test("Personal's files: once each across machines, how each stands, the rail says where", async ({
  page,
}) => {
  await open(page, "/personal/brief");
  const compiled = page.getByRole("region", { name: "Compiled to" });
  await expect(compiled.getByRole("link")).toHaveText([
    "~/.claude/rules/memax-personal.mdClaude CodeOn 1 of 2 machineshome-linux last online 3 days ago",
    "~/.codex/AGENTS.mdCodexHand edit on ziyang-mbp",
    "~/.memax/personal/AGENTS.mdCanonical copyIn sync on ziyang-mbp",
    "Codex's copy on ziyang-mbp has one local edit.",
  ]);
  await expect(rail(page).getByRole("status")).toHaveText(
    "Hand edit on ziyang-mbp",
  );
  const machines = page.getByRole("region", { name: "Machines" });
  await expect(machines.getByText("2 linked")).toBeVisible();

  // Today counts the same files.
  await open(page, "/personal/today");
  const today = page.getByRole("region", { name: "Compiled context" });
  await expect(today.getByRole("link")).toHaveCount(3);
});

test("one machine's copy: the switcher by keyboard, and Stop compiling here", async ({
  page,
}) => {
  await open(page, "/personal/brief");
  await page
    .getByRole("region", { name: "Compiled to" })
    .getByRole("link", { name: /memax-personal\.md/ })
    .click();
  await expect(page).toHaveURL("/personal/brief/targets/claude-user-machine-1");
  const switcher = page.getByRole("navigation", { name: "Machines" });
  await expect(switcher.getByRole("link")).toHaveCount(2);
  await expect(
    switcher.getByRole("link", { name: "ziyang-mbp, In sync" }),
  ).toHaveAttribute("aria-current", "page");
  const desktop = switcher.getByRole("link", { name: "home-linux, Offline" });
  await tabTo(page, desktop);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/personal/brief/targets/claude-user-machine-2");
  await expect(
    switcher.getByRole("link", { name: "home-linux, Offline" }),
  ).toHaveAttribute("aria-current", "page");

  await page.getByRole("button", { name: "Stop compiling here" }).click();
  await expect(toasts(page)).toContainText(
    "Stopped compiling ~/.claude/rules/memax-personal.md on home-linux.",
  );
  await expect(
    page.getByText(
      "Memax stopped compiling this file on home-linux. Agents there keep reading your Personal space over MCP.",
    ),
  ).toBeVisible();
});

test("a hand edit on one machine: 2 and ↵ overwrite it there, after asking", async ({
  page,
}) => {
  await open(page, "/personal/brief/targets/codex-user-machine-1/drift");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "~/.codex/AGENTS.md has a hand edit on ziyang-mbp",
  );
  await expect(page.getByText("On ziyang-mbp now")).toBeVisible();
  await page.keyboard.press("2");
  await page.keyboard.press("Enter");
  // Overwrite asks first; Cancel has the focus.
  await expect(page.getByRole("button", { name: /Cancel/ })).toBeFocused();
  await page.getByRole("button", { name: "Overwrite", exact: true }).click();
  await expect(toasts(page)).toContainText("Overwrote ~/.codex/AGENTS.md");
  await expect(rail(page).getByRole("status")).toHaveText("On 1 of 2 machines");
});

test("machines rename and stop compiling in place, by keyboard", async ({
  page,
}) => {
  await open(page, "/personal/brief");
  const machines = page.getByRole("region", { name: "Machines" });
  await tabTo(
    page,
    machines.getByRole("button", { name: "Rename ziyang-mbp" }),
  );
  await page.keyboard.press("Enter");
  const field = machines.getByRole("textbox", { name: "Name of ziyang-mbp" });
  await expect(field).toBeFocused();
  await page.keyboard.type("studio-mbp");
  await page.keyboard.press("Enter");
  await expect(toasts(page)).toContainText("Renamed ziyang-mbp to studio-mbp");
  await expect(
    machines.getByRole("button", { name: "Rename studio-mbp" }),
  ).toBeFocused();

  const stop = machines.getByRole("button", {
    name: "Stop compiling on home-linux",
  });
  await tabTo(page, stop);
  await page.keyboard.press("Enter");
  const confirm = machines.getByRole("group", {
    name: "Stop compiling on home-linux",
  });
  await expect(
    confirm.getByText(
      "Memax takes your Personal space off home-linux the next time it's online. Files you edited by hand stay as they are.",
    ),
  ).toBeVisible();
  await expect(confirm.getByRole("button", { name: /Cancel/ })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(stop).toBeFocused();
  await page.keyboard.press("Enter");
  await confirm
    .getByRole("button", { name: "Stop compiling on home-linux" })
    .click();
  await expect(toasts(page)).toContainText("Stopped compiling on home-linux.");
  await expect(machines.getByText("1 linked")).toBeVisible();
  // home-linux's files are off now: the rest are in sync but for the edit.
  await expect(rail(page).getByRole("status")).toHaveText(
    "Hand edit on studio-mbp",
  );
});

// Self-baselines: each new state on the demo dataset, in both themes.
const SHOTS = [
  { name: "personal-brief", path: "/personal/brief", height: 1100 },
  { name: "personal-today", path: "/personal/today", height: 900 },
  {
    name: "personal-target",
    path: "/personal/brief/targets/claude-user-machine-1",
    height: 900,
  },
  {
    name: "personal-block",
    path: "/personal/brief/targets/codex-user-machine-1",
    height: 900,
  },
  {
    name: "personal-drift",
    path: "/personal/brief/targets/codex-user-machine-1/drift",
    height: 1100,
  },
  { name: "personal-memory", path: "/personal/memories/M-0027", height: 800 },
  { name: "personal-account", path: "/settings/account", height: 1360 },
] as const;

for (const shot of SHOTS) {
  for (const theme of ["light", "dark"] as const) {
    test(`${shot.name}, ${theme}, 1440`, async ({ page, baseURL }) => {
      test.skip(usingDevServer, "screenshots need the production build");
      await useTheme(page, baseURL, theme);
      await page.setViewportSize({ width: 1440, height: shot.height });
      // Settings show the space last opened: Personal.
      if (shot.path.startsWith("/settings"))
        await open(page, "/personal/today");
      await open(page, shot.path);
      await settle(page);
      await expect(page).toHaveScreenshot(`${shot.name}-${theme}-1440.png`, {
        maxDiffPixelRatio: 0.002,
      });
    });
  }
}
