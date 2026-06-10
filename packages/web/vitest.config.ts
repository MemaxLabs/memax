import { defineConfig } from "vitest/config";
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
