import { describe, expect, it } from "vitest";
import { buildSyncSourcePath } from "./import.js";

describe("buildSyncSourcePath", () => {
  it("uses the declared sync root rather than process cwd", () => {
    expect(
      buildSyncSourcePath(
        "/workspaces/memax/docs",
        "/workspaces/memax/docs/adr/001.md",
      ),
    ).toBe("adr/001.md");
  });

  it("rejects files outside the sync root", () => {
    expect(() =>
      buildSyncSourcePath(
        "/workspaces/memax/docs",
        "/workspaces/memax/README.md",
      ),
    ).toThrow("file is outside sync root");
  });
});
