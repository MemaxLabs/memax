// Regressions for what review found: text around a stray managed block,
// files that aren't UTF-8, a hand edit that must be reported again after a
// person acted, privacy of a user-owned file, symlinks into .git, a daemon
// that holds the lock without answering, and .memax.yml edge cases.
import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { AlreadyRunningError } from "../../src/lib/daemon/control.js";
import {
  readMemaxYmlConfig,
  removeMemaxYmlSpace,
  writeMemaxYmlSpace,
} from "../../src/lib/project-context.js";
import { harness, until, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const observations = () => h.fake.calls("POST", /\/observations$/);
const deliveries = () => h.fake.calls("POST", /\/deliveries$/);
const file = (p: string) => join(h.repo, p);

describe("a file Memax owns is judged whole", () => {
  it("keeps the text around a block when CLAUDE.md stops being user-owned", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "claude_md", { user_owned: true });
    h.link(space);
    writeFileSync(file("CLAUDE.md"), "# My notes\n\nKeep these.\n");
    h.fake.compile(t.id, { "CLAUDE.md": ["@AGENTS.md"] });
    const d = h.daemon();
    await d.syncOnce();
    const mine = readFileSync(file("CLAUDE.md"), "utf8");
    expect(mine).toContain("Keep these.");

    // Someone turns user_owned off: the next run is a whole shim.
    h.fake.entry(t.id).target.settings.user_owned = false;
    h.fake.compile(t.id, {
      "CLAUDE.md": "<!-- Compiled by Memax -->\n@AGENTS.md\n",
    });
    await d.syncOnce();
    expect(readFileSync(file("CLAUDE.md"), "utf8")).toBe(mine);
    // Held, not reported: the person's notes never leave the machine.
    expect(observations()).toHaveLength(0);
    expect(deliveries()).toHaveLength(1);
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "blocked",
    });
    expect(d.snapshot().repos[0].targets[0].detail).toContain(
      "memax link --yes",
    );
  });

  it("holds a CLAUDE.md with a V1 block until Memax manages just the block", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "claude_md");
    h.link(space);
    const v1 =
      "# Repo\n\n<!-- memax:start -->\n## Memax\nUse memax_recall.\n<!-- memax:end -->\n\nMine.\n";
    writeFileSync(file("CLAUDE.md"), v1);
    h.fake.compile(t.id, { "CLAUDE.md": "@AGENTS.md\n" });
    const d = h.daemon();
    await d.syncOnce();
    await d.syncOnce();
    expect(readFileSync(file("CLAUDE.md"), "utf8")).toBe(v1);
    expect(observations()).toHaveLength(0);
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(1); // held, not polled

    // The target becomes the person's (memax link --yes): V2's block
    // replaces V1's in place, and the rest stays.
    await h.memax.v2.targets.update(
      t.id,
      { settings: { user_owned: true } },
      { idempotencyKey: "own-1" },
    );
    h.fake.compile(t.id, { "CLAUDE.md": ["@AGENTS.md"] });
    await d.syncOnce();
    expect(readFileSync(file("CLAUDE.md"), "utf8")).toBe(
      "# Repo\n\n<!-- memax:start -->\n@AGENTS.md\n<!-- memax:end -->\n\nMine.\n",
    );
    expect(h.fake.entry(t.id).target.sync_state).toBe("in_sync");
  });

  it("never mistakes two devices' notes around the same block for an accepted edit", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "claude_md");
    const other = join(h.home, "worktree");
    mkdirSync(other, { recursive: true });
    h.link(space);
    h.link(space, other);
    const block = "<!-- memax:start -->\nV1\n<!-- memax:end -->\n";
    writeFileSync(file("CLAUDE.md"), "# A's notes\n" + block);
    writeFileSync(join(other, "CLAUDE.md"), "# B's notes\n" + block);
    h.fake.compile(t.id, { "CLAUDE.md": "@AGENTS.md\n" });
    const d = h.daemon();
    await d.syncOnce();
    expect(readFileSync(file("CLAUDE.md"), "utf8")).toBe(
      "# A's notes\n" + block,
    );
    expect(readFileSync(join(other, "CLAUDE.md"), "utf8")).toBe(
      "# B's notes\n" + block,
    );
    expect(observations()).toHaveLength(0);
  });
});

describe("a file that isn't UTF-8 text", () => {
  it("is left byte for byte, and the target is held", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "claude_md", { user_owned: true });
    h.link(space);
    const latin1 = Buffer.from("# Caf\xe9 notes\n", "latin1");
    writeFileSync(file("CLAUDE.md"), latin1);
    h.fake.compile(t.id, { "CLAUDE.md": ["@AGENTS.md"] });
    const d = h.daemon();
    await d.syncOnce();
    expect(readFileSync(file("CLAUDE.md")).equals(latin1)).toBe(true);
    expect(deliveries()).toHaveLength(0);
    expect(d.snapshot().repos[0].targets[0]).toMatchObject({
      state: "blocked",
    });
    expect(d.snapshot().repos[0].targets[0].detail).toContain("UTF-8");
  });
});

describe("reporting a hand edit", () => {
  it("reports the same edit again once a person stopped and restarted the target", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    writeFileSync(file("AGENTS.md"), "# Brief, mine\n");
    h.fake.compile(t.id, { "AGENTS.md": "# Brief 2\n" });
    await d.syncOnce();
    expect(observations()).toHaveLength(1);

    await h.memax.v2.targets.stop(t.id, {}, { idempotencyKey: "stop-1" });
    await d.syncOnce();
    await h.memax.v2.targets.update(
      t.id,
      { enabled: true },
      { idempotencyKey: "on-1" },
    );
    h.fake.compile(t.id, { "AGENTS.md": "# Brief 3\n" });
    // Past the reporter's per-file gap.
    await new Promise((r) => setTimeout(r, 50));
    const fresh = h.daemon(); // a new process, as after a restart
    await d.stop();
    await fresh.syncOnce();
    expect(readFileSync(file("AGENTS.md"), "utf8")).toBe("# Brief, mine\n");
    expect(observations()).toHaveLength(2);
    expect(h.fake.entry(t.id).target.sync_state).toBe("drifted");
  });

  it("holds a hand edit without polling the preview while its report keeps failing", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    writeFileSync(file("AGENTS.md"), "# Mine\n");
    h.fake.fail(/\/observations$/, 500, "internal_error", 10);
    h.fake.compile(t.id, { "AGENTS.md": "# Brief 2\n" });
    await d.syncOnce();
    const previews = h.fake.calls("GET", /\/preview$/).length;
    await d.syncOnce();
    await d.syncOnce();
    expect(h.fake.calls("GET", /\/preview$/)).toHaveLength(previews);
    expect(readFileSync(file("AGENTS.md"), "utf8")).toBe("# Mine\n");
    expect(deliveries()).toHaveLength(1);
  });

  it("sends only Memax's block for a file the person owns", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "claude_md", {
      path: "CLAUDE.local.md",
      user_owned: true,
    });
    h.link(space);
    writeFileSync(file("CLAUDE.local.md"), "# Private: my salary notes\n");
    h.fake.compile(t.id, { "CLAUDE.local.md": ["@AGENTS.md"] });
    const d = h.daemon();
    await d.syncOnce();
    const edited = readFileSync(file("CLAUDE.local.md"), "utf8").replace(
      "@AGENTS.md",
      "@AGENTS.md\n- mine, in the block",
    );
    writeFileSync(file("CLAUDE.local.md"), edited);
    h.fake.compile(t.id, { "CLAUDE.local.md": ["@AGENTS.md", "- next"] });
    await d.syncOnce();
    expect(observations()).toHaveLength(1);
    const sent = (observations()[0].body as { content: string }).content;
    expect(sent).toContain("- mine, in the block");
    expect(sent).not.toContain("salary");
    expect(sent.startsWith("<!-- memax:start -->")).toBe(true);
  });
});

describe("paths", () => {
  it("never follows a symlinked directory into .git", async () => {
    execFileSync("git", ["init", "-q", h.repo]);
    symlinkSync(join(h.repo, ".git"), file(".cursor"));
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "cursor_mdc");
    h.link(space);
    h.fake.compile(t.id, { ".cursor/rules/memax-web.mdc": "- x [M-1]\n" });
    await h.daemon().syncOnce();
    expect(() =>
      readFileSync(join(h.repo, ".git", "rules", "memax-web.mdc")),
    ).toThrow();
    expect(deliveries()).toHaveLength(0);
  });
});

describe("the lock", () => {
  it("won't take over from a daemon that is alive but not answering", async () => {
    mkdirSync(h.paths.dir, { recursive: true });
    // A stopped daemon: it holds the socket and the pid file, and answers nothing.
    const ghost = spawn(process.execPath, [
      "-e",
      `require("net").createServer(() => {}).listen(${JSON.stringify(h.paths.socket)})`,
      "daemon",
      "run",
    ]);
    try {
      await until(
        () => {
          try {
            readFileSync(h.paths.socket);
            return false;
          } catch (e) {
            return (e as NodeJS.ErrnoException).code !== "ENOENT";
          }
        },
        5_000,
        "the ghost's socket",
      );
      writeFileSync(h.paths.pid, `${ghost.pid}\n`);
      const err = await h
        .daemon()
        .start()
        .catch((e: unknown) => e);
      expect(err).toBeInstanceOf(AlreadyRunningError);
      expect((err as Error).message).toContain("isn't answering");
    } finally {
      ghost.kill("SIGKILL");
    }
  }, 15_000);
});

describe(".memax.yml", () => {
  it("never reads a value across lines, and fills an empty space: line", () => {
    writeFileSync(file(".memax.yml"), "space:\nhub: team\n");
    expect(readMemaxYmlConfig(h.repo)).toEqual({ hub: "team" });
    writeMemaxYmlSpace(h.repo, "memax-v2");
    expect(readFileSync(file(".memax.yml"), "utf8")).toBe(
      "space: memax-v2\nhub: team\n",
    );
  });

  it("keeps a person's comments when unlink takes its line out", () => {
    writeFileSync(
      file(".memax.yml"),
      "# Ours: see the wiki\nspace: memax-v2\n",
    );
    removeMemaxYmlSpace(h.repo);
    expect(readFileSync(file(".memax.yml"), "utf8")).toBe(
      "# Ours: see the wiki\n",
    );
  });
});
