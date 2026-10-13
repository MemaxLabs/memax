// The change memax verify shows from this clone: the commit at or before
// the failing one that last touched the condition's paths, and its diff.
import { afterEach, describe, expect, it } from "vitest";
import {
  localChange,
  MAX_DIFF_LINES,
} from "../../src/lib/stale/local-change.js";
import { cleanupRepos, git, repository, write } from "../daemon/git-repo.js";

afterEach(cleanupRepos);

describe("the change in this clone", () => {
  it("finds the rename behind a failure at a later commit, with its diff", async () => {
    const { root } = repository({
      "internal/ask/config.go": "package ask\n\nconst Tiers = 3\n",
      "README.md": "# x\n",
    });
    git(root, "mv", "internal/ask/config.go", "internal/ask/tiers.go");
    git(root, "commit", "-q", "-m", "Split the ask tiers");
    const renamed = git(root, "rev-parse", "--short", "HEAD");
    write(root, { "README.md": "# y\n" });
    git(root, "commit", "-qam", "Unrelated");
    const at = git(root, "rev-parse", "HEAD");

    const c = await localChange(root, at, [
      "internal/ask/config.go",
      "internal/ask/tiers.go",
    ]);
    expect(c).toMatchObject({
      kind: "change",
      commit: renamed,
      author: "Test",
      subject: "Split the ask tiers",
      more: 0,
    });
    if (c?.kind !== "change") throw new Error("no change");
    expect(c.lines).toContain("rename from internal/ask/config.go");
    expect(c.lines).toContain("rename to internal/ask/tiers.go");
    expect(c.lines.join("\n")).not.toContain("README");
  });

  it("caps the diff, and counts what it leaves out", async () => {
    const { root } = repository({ "big.txt": "start\n" });
    const lines = Array.from({ length: 200 }, (_, i) => `line ${i}`).join("\n");
    write(root, { "big.txt": `${lines}\n` });
    git(root, "commit", "-qam", "Grow it");
    const c = await localChange(root, git(root, "rev-parse", "HEAD"), [
      "big.txt",
    ]);
    if (c?.kind !== "change") throw new Error("no change");
    expect(c.lines).toHaveLength(MAX_DIFF_LINES);
    expect(c.more).toBeGreaterThan(100);
  });

  it("says when the failing commit isn't in this clone, and nothing for a bad id", async () => {
    const { root } = repository({ "a.txt": "a\n" });
    expect(await localChange(root, "f".repeat(40), ["a.txt"])).toEqual({
      kind: "missing",
    });
    expect(await localChange(root, "HEAD; rm -rf /", ["a.txt"])).toBeNull();
    expect(
      await localChange(root, git(root, "rev-parse", "HEAD"), []),
    ).toBeNull();
  });

  it("reads a glob as a glob, and a path as nothing but itself", async () => {
    const { root } = repository({ "src/a.go": "package a\n", "*.go": "x\n" });
    git(root, "rm", "-q", "src/a.go");
    git(root, "commit", "-qm", "Drop a.go");
    const at = git(root, "rev-parse", "HEAD");
    const viaGlob = await localChange(root, at, ["src/**/*.go"], true);
    expect(viaGlob).toMatchObject({ kind: "change", subject: "Drop a.go" });
    // A literal `*.go` is the file of that name, never a pattern.
    const literal = await localChange(root, at, ["*.go"]);
    expect(literal).toMatchObject({ kind: "change", subject: "first" });
  });
});
