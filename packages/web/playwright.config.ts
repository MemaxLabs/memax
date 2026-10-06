import { defineConfig, devices } from "@playwright/test";

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
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
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
        },
      },
});
