import { defineConfig, devices } from "@playwright/test";
import { handoffPreviewsDir, handoffScreensDir } from "./e2e/handoff";
import { STACK_API_URL, STACK_SURFACE_SECRET } from "./e2e/stack";

// Playwright for packages/web: V2 visual regression and keyboard smoke
// tests (plan §6.8). Run with `pnpm --filter @memaxlabs/web test:e2e`;
// it is deliberately not part of `pnpm test`.
//
// By default it builds a production bundle that keeps the /dev fixture
// pages (NEXT_PUBLIC_DEV_FIXTURES=1) and serves it with `next start`, so
// screenshots match what CI sees. Knobs:
//   E2E_BASE_URL=http://host:port  test a server that's already running
//   E2E_SERVER=dev                 use `next dev` (quick local loops; the
//                                  screenshot test is skipped there)
//   E2E_PORT=3100                  port for the server Playwright starts
//
// CI needs Chromium and its system libraries:
//   pnpm --filter @memaxlabs/web exec playwright install --with-deps chromium
// Without a usable browser the tests skip locally and fail in CI.

const port = Number(process.env.E2E_PORT ?? 3100);
const externalBaseURL = process.env.E2E_BASE_URL;
const baseURL = externalBaseURL ?? `http://localhost:${port}`;
const devServer = process.env.E2E_SERVER === "dev";
// E2E_STACK=1 runs e2e/auth-bff.e2e.ts against the real API (e2e/stack.ts
// starts it): the build points at it and shares its WEB_SURFACE_SECRET.
// The demo specs are unaffected: a browser without a session still sees
// the demo dataset.
const stackEnv: Record<string, string> =
  process.env.E2E_STACK === "1"
    ? {
        NEXT_PUBLIC_API_URL: STACK_API_URL,
        NEXT_PUBLIC_APP_URL: baseURL,
        WEB_SURFACE_SECRET: STACK_SURFACE_SECRET,
      }
    : {};

export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.e2e.ts",
  snapshotPathTemplate:
    "{testDir}/__screenshots__/{testFileName}/{arg}-{projectName}-{platform}{ext}",
  globalSetup: "./e2e/global-setup.ts",
  // One server, a small machine: run the files one after another.
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [["github"], ["html", { open: "never" }]]
    : [["list"]],
  use: {
    baseURL,
    locale: "en-US",
    timezoneId: "UTC",
    colorScheme: "light",
    trace: "retain-on-failure",
  },
  expect: {
    // Tight: the specimen is a component-level fixture.
    toHaveScreenshot: { maxDiffPixelRatio: 0.001, animations: "disabled" },
  },
  projects: [
    {
      name: "chromium",
      testIgnore: [
        "**/ledger-components.e2e.ts",
        "**/onboarding-boards.e2e.ts",
        "**/dream-boards.e2e.ts",
        "**/settings-boards.e2e.ts",
      ],
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        reducedMotion: "reduce",
      },
    },
    {
      // The component gallery against the handoff's 2× preview PNGs,
      // read in place from the private memax-internal checkout (never
      // copied here). See e2e/ledger-components.e2e.ts.
      name: "handoff",
      testMatch: "**/ledger-components.e2e.ts",
      snapshotPathTemplate: `${handoffPreviewsDir() ?? "{testDir}/__handoff_missing__"}/{arg}{ext}`,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        deviceScaleFactor: 2,
        reducedMotion: "reduce",
      },
    },
    {
      // The first session's screens against their 1× boards
      // (screens/png), read in place like the previews; it measures how
      // far each is from its board (e2e/onboarding-boards.e2e.ts).
      name: "boards",
      testMatch: [
        "**/onboarding-boards.e2e.ts",
        "**/dream-boards.e2e.ts",
        "**/settings-boards.e2e.ts",
      ],
      snapshotPathTemplate: `${handoffScreensDir() ?? "{testDir}/__handoff_missing__"}/{arg}{ext}`,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        deviceScaleFactor: 1,
        reducedMotion: "reduce",
      },
    },
  ],
  webServer: externalBaseURL
    ? undefined
    : {
        command: devServer
          ? `pnpm exec next dev --turbopack -p ${port}`
          : `pnpm exec next build && pnpm exec next start -p ${port}`,
        url: `${baseURL}/`,
        reuseExistingServer: !process.env.CI,
        timeout: 15 * 60 * 1000,
        env: {
          NEXT_PUBLIC_DEV_FIXTURES: "1",
          NEXT_TELEMETRY_DISABLED: "1",
          ...stackEnv,
        },
      },
});
