// What the daemon refuses to write: paths out of the repository, files a
// target's kind never writes, symlinks, and anything under .git.
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  outputRefusal,
  resolveInRepo,
  validRepoPath,
} from "../../src/lib/daemon/safe-path.js";
import { harness, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

describe("paths", () => {
  it.each([
    ["../evil.md", false],
    ["/etc/passwd", false],
    ["a/../../evil.md", false],
    ["./AGENTS.md", false],
    ["a//b.md", false],
    [".git/hooks/pre-commit", false],
    ["sub/.GIT/config", false],
    [".memax.yml", false],
    ["packages/web/AGENTS.md", true],
    [".cursor/rules/memax-web.mdc", true],
    ["docs\\AGENTS.md", false],
    ["AGENTS.md\u0000", false],
    ["ÄGENTS.md", false],
  ])("validRepoPath(%j) is %s", (p, ok) => {
    expect(validRepoPath(p)).toBe(ok);
  });

  it("allows only the files a target's kind writes", () => {
    const agents = { kind: "agents_md" as const, path: "AGENTS.md" };
    const cursor = { kind: "cursor_mdc" as const, path: ".cursor/rules" };
    const claude = { kind: "claude_md" as const, path: "CLAUDE.md" };
    expect(outputRefusal(agents, "AGENTS.md")).toBeNull();
    expect(outputRefusal(agents, "package.json")).not.toBeNull();
    expect(outputRefusal(agents, "docs/AGENTS.md")).not.toBeNull(); // not the target's own path
    expect(outputRefusal(claude, "CLAUDE.md")).toBeNull();
    expect(outputRefusal(claude, ".envrc")).not.toBeNull();
    expect(
      outputRefusal(cursor, ".cursor/rules/memax-packages-web.mdc"),
    ).toBeNull();
    expect(outputRefusal(cursor, ".cursor/rules/style.mdc")).not.toBeNull();
    expect(outputRefusal(cursor, ".cursor/rules/memax-x.md")).not.toBeNull();
    expect(
      outputRefusal(cursor, ".cursor/rules/sub/memax-x.mdc"),
    ).not.toBeNull();
    expect(
      outputRefusal({ kind: "chatgpt", path: undefined }, "x.md"),
    ).not.toBeNull();
  });

  it("refuses a directory that is a symlink out of the repository", async () => {
    const outside = mkdtempSync(join(tmpdir(), "memax-outside-"));
    mkdirSync(join(h.repo, ".cursor"));
    symlinkSync(outside, join(h.repo, ".cursor", "rules"));
    await expect(
      resolveInRepo(h.repo, ".cursor/rules/memax-x.mdc"),
    ).rejects.toThrow(/outside the repository/);
    // A symlink that stays inside is fine.
    mkdirSync(join(h.repo, "real-rules"));
    mkdirSync(join(h.repo, "inner"));
    symlinkSync(join(h.repo, "real-rules"), join(h.repo, "inner", "rules"));
    await expect(
      resolveInRepo(h.repo, "inner/rules/memax-x.mdc"),
    ).resolves.toBe(join(h.repo, "inner/rules/memax-x.mdc"));
  });
});

describe("the daemon", () => {
  it("refuses a run that writes outside the target's files, writing nothing", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "cursor_mdc");
    h.link(space);
    h.fake.compile(t.id, {
      ".cursor/rules/memax-web.mdc": "- ok [M-1]\n",
      ".cursor/rules/../../.git/hooks/pre-commit": "#!/bin/sh\necho owned\n",
    });
    const d = h.daemon();
    await d.syncOnce();
    expect(existsSync(join(h.repo, ".cursor"))).toBe(false);
    expect(existsSync(join(h.repo, ".git"))).toBe(false);
    expect(h.fake.calls("POST", /\/deliveries$/)).toHaveLength(0);
    expect(d.snapshot().repos[0].targets[0].state).toBe("blocked");
  });

  it("never follows a symlinked directory out of the repository", async () => {
    const outside = mkdtempSync(join(tmpdir(), "memax-outside-"));
    symlinkSync(outside, join(h.repo, ".cursor"));
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "cursor_mdc");
    h.link(space);
    h.fake.compile(t.id, { ".cursor/rules/memax-web.mdc": "- ok [M-1]\n" });
    await h.daemon().syncOnce();
    expect(existsSync(join(outside, "rules"))).toBe(false);
    expect(h.fake.calls("POST", /\/deliveries$/)).toHaveLength(0);
  });

  it("never writes through a symlinked file, and leaves the link alone", async () => {
    const outside = join(
      mkdtempSync(join(tmpdir(), "memax-outside-")),
      "bashrc",
    );
    writeFileSync(outside, "export PATH=/usr/bin\n");
    symlinkSync(outside, join(h.repo, "AGENTS.md"));
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    expect(readFileSync(outside, "utf8")).toBe("export PATH=/usr/bin\n");
    expect(h.fake.calls("POST", /\/observations$/)).toHaveLength(0);
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "blocked",
    });
    expect(d.snapshot().repos[0].targets[0].detail).toContain("symlink");
    // It waits for a person: no preview on every poll meanwhile.
    await d.syncOnce();
    await d.syncOnce();
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(1);
  });

  it("keeps file contents and credentials out of its log", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    const secret = "SENTINEL-the-words-of-a-memory";
    h.fake.compile(t.id, { "AGENTS.md": `- ${secret} [M-1]\n` });
    const d = h.daemon();
    await d.syncOnce();
    writeFileSync(join(h.repo, "AGENTS.md"), `- ${secret}, edited [M-1]\n`);
    h.fake.compile(t.id, { "AGENTS.md": `- ${secret} again [M-1]\n` });
    await d.syncOnce();
    const log = h.log.lines.join("");
    expect(log).toContain("reported a hand edit");
    expect(log).not.toContain("SENTINEL");
    expect(log).not.toContain("test-token");
  });
});
