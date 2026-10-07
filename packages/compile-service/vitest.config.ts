import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    // worker.test.ts starts workerd, which takes a few seconds.
    testTimeout: 30_000,
    hookTimeout: 30_000,
  },
});
