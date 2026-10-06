import { afterEach } from "vitest";

// Vitest runs without globals, so Testing Library can't register its own
// cleanup. Unmount after every test in the jsdom files.
if (typeof document !== "undefined") {
  const { cleanup } = await import("@testing-library/react");
  afterEach(() => cleanup());
}
