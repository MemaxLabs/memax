import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Pure tests (CSS scans, string catalogues, helpers) run in node.
    // Component tests opt in to jsdom with `// @vitest-environment jsdom`
    // and are named `*.dom.test.tsx`, as in packages/web.
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
  },
  esbuild: {
    jsx: "automatic",
  },
});
