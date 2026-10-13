import { existsSync } from "node:fs";
import path from "node:path";
import { HANDOFF_MISSING, handoffScreensDir } from "./handoff";
import {
  expect,
  settle,
  skipWithoutBrowser,
  test,
  useTheme,
  usingDevServer,
} from "./fixtures";

// The public site against its boards (Landing.png, Pricing.png,
// Security.png and Docs.png in the private handoff, read in place and
// never written), at each board's size in Paper, signed out. Like the
// other *-boards specs it measures how far each page is from its board
// (E2E_BOARDS_MAX_RATIO=0 prints every difference); public.e2e.ts holds
// the pages to their own baselines.
//
// Each page departs from its board on purpose, so each has its own bar,
// a point above what it measured on Oct 11, 2026 (Playwright's ratios,
// rounded up: Landing 0.04, Pricing 0.03, Security 0.03, Docs 0.08):
// - Landing: `npx memax-cli init` (D12), the personal layer's line and
//   transcript row (D1), Cursor reading AGENTS.md and ChatGPT live over
//   its connector (D2/D3), Trust in the nav, and no Undo on the example.
// - Pricing: D9's plans (weekly Dream on Free, handoffs on Pro, more
//   lines per plan), the alpha's note and the yearly toggle, "Coming" on
//   what isn't built, and no buttons that would sell.
// - Security: the placeholders filled, and the sections the board left to
//   "[confirm before publishing]" (who else sees a memory's words, what
//   Forget can't reach, checking it yourself), which push the page past
//   the board's height.
// - Docs: steps that say what the CLI does today (no `memax review`, no
//   `--via pr`, this build's MCP server), all eleven tools, and the tree
//   of the docs site's real pages.

skipWithoutBrowser();

const ENV_RATIO = process.env.E2E_BOARDS_MAX_RATIO;
const dir = handoffScreensDir();

const BOARDS = [
  {
    board: "Landing",
    path: "/welcome",
    heading: "One context. Every agent.",
    height: 2080,
    max: 0.05,
  },
  {
    board: "Pricing",
    path: "/pricing",
    heading: "Free for one project. Fair when it grows.",
    height: 1444,
    max: 0.04,
  },
  {
    board: "Security",
    path: "/security",
    heading: "What Memax keeps, and how it lets go.",
    height: 1800,
    max: 0.04,
  },
  {
    board: "Docs",
    path: "/docs",
    heading: "Quickstart",
    height: 1586,
    max: 0.09,
  },
] as const;

test.beforeAll(() => {
  test.skip(!dir, HANDOFF_MISSING);
});

for (const b of BOARDS) {
  test(`${b.board} against its board`, async ({ page, baseURL }, testInfo) => {
    const updating = ["all", "changed"].includes(
      testInfo.config.updateSnapshots,
    );
    test.skip(updating, "Never rewrite the handoff's boards.");
    test.skip(usingDevServer, "screenshots need the production build");
    test.skip(
      !dir || !existsSync(path.join(dir, `${b.board}.png`)),
      `No ${b.board}.png in the handoff.`,
    );
    await useTheme(page, baseURL, "light");
    await page.setViewportSize({ width: 1440, height: b.height });
    await page.goto(b.path);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(b.heading);
    await settle(page);
    await expect.soft(page).toHaveScreenshot(`${b.board}.png`, {
      maxDiffPixelRatio: ENV_RATIO !== undefined ? Number(ENV_RATIO) : b.max,
      animations: "disabled",
      caret: "hide",
      scale: "css",
    });
  });
}
