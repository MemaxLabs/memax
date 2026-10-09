import { configDefaults, defineConfig } from "vitest/config";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  test: {
    // Default environment is node — pure-fn tests under src/lib/ run fast
    // without a DOM. Hook/component tests opt in to jsdom by adding the
    // `// @vitest-environment jsdom` directive at the top of the test file
    // (convention: name them `*.dom.test.{ts,tsx}` for discoverability).
    environment: "node",
    setupFiles: ["./vitest.setup.ts"],
    // Above Testing Library's 5 s wait (vitest.setup.ts), so one slow wait
    // on a loaded CI runner fails its own assertion, not the whole test.
    testTimeout: 15_000,
    // OpenNext's and Wrangler's build output (build:cf) holds copies of
    // the app.
    exclude: [...configDefaults.exclude, ".open-next/**", ".wrangler/**"],
  },
  // Use the automatic JSX runtime so React components imported from
  // @memaxlabs/ui (which don't import React explicitly, relying on the
  // new runtime) render under vitest's esbuild transformer instead of
  // crashing with "React is not defined".
  esbuild: {
    jsx: "automatic",
  },
  resolve: {
    alias: {
      "@": path.resolve(rootDir, "src"),
    },
  },
});
