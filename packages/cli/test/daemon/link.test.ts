import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { link, unlink, type LinkDeps } from "../../src/commands/link.js";
import { readRegistry } from "../../src/lib/daemon/registry.js";
import { readMemaxYmlConfig } from "../../src/lib/project-context.js";
import { harness, type Harness } from "./harness.js";

let h: Harness;
let out: string[];
beforeEach(async () => {
  h = await harness();
  out = [];
  execFileSync("git", ["init", "-q", h.repo]);
});
afterEach(async () => {
  await h.cleanup();
});

const deps = (cwd = h.repo, interactive = false): LinkDeps => ({
  memax: h.memax,
  paths: h.paths,
  cwd,
  out: (l) => out.push(l),
  interactive,
});
// eslint-disable-next-line no-control-regex
const text = () => out.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

describe("memax link", () => {
  it("writes .memax.yml at the repository root and registers the repository", async () => {
    const space = h.fake.addSpace("memax-v2");
    h.fake.addTarget(space, "agents_md");
    h.fake.addTarget(space, "chatgpt");
    mkdirSync(join(h.repo, "packages", "web"), { recursive: true });
    expect(
      await link({ space: "memax-v2" }, deps(join(h.repo, "packages", "web"))),
    ).toBe(0);

    expect(readFileSync(join(h.repo, ".memax.yml"), "utf8")).toContain(
      "space: memax-v2\n",
    );
    expect(existsSync(join(h.repo, "packages", "web", ".memax.yml"))).toBe(
      false,
    );
    expect(readRegistry(h.paths)).toEqual([
      {
        root: h.repo,
        space_id: space.id,
        space_slug: "memax-v2",
        created_at: expect.any(String),
      },
    ]);
    expect(text()).toContain("Linked");
    expect(text()).toContain("Writes      AGENTS.md");
    expect(text()).not.toContain("ChatGPT");
    expect(text()).toContain("memax daemon start");

    // Again: nothing changes.
    const first = readRegistry(h.paths)[0].created_at;
    await link({}, deps());
    expect(readRegistry(h.paths)[0].created_at).toBe(first);
  });

  it("keeps the other lines of an existing .memax.yml, and unlink takes only its own out", async () => {
    const space = h.fake.addSpace("memax-v2");
    writeFileSync(
      join(h.repo, ".memax.yml"),
      "hub: team-hub\nproject_id: github.com/acme/app\n",
    );
    await link({ space: space.slug }, deps());
    expect(readMemaxYmlConfig(h.repo)).toEqual({
      hub: "team-hub",
      project_id: "github.com/acme/app",
      space: "memax-v2",
    });

    expect(
      await unlink({ paths: h.paths, cwd: h.repo, out: (l) => out.push(l) }),
    ).toBe(0);
    expect(readFileSync(join(h.repo, ".memax.yml"), "utf8")).toBe(
      "hub: team-hub\nproject_id: github.com/acme/app\n",
    );
    expect(readRegistry(h.paths)).toEqual([]);
    expect(text()).toContain("The files Memax wrote stay as they are.");
  });

  it("unlink removes a .memax.yml that only link wrote", async () => {
    const space = h.fake.addSpace("memax-v2");
    await link({ space: space.id }, deps());
    await unlink({ paths: h.paths, cwd: h.repo, out: () => {} });
    expect(existsSync(join(h.repo, ".memax.yml"))).toBe(false);
  });

  it("finds the space by the git remote", async () => {
    const space = h.fake.addSpace("memax-v2");
    h.fake.addSpace("other");
    space.repository = "https://github.com/MemaxLabs/memax.git";
    execFileSync("git", [
      "-C",
      h.repo,
      "remote",
      "add",
      "origin",
      "git@github.com:memaxlabs/memax.git",
    ]);
    await link({}, deps());
    expect(readRegistry(h.paths)[0].space_slug).toBe("memax-v2");
  });

  it("asks which space when it can't tell, and says how outside a TTY", async () => {
    h.fake.addSpace("a");
    h.fake.addSpace("b");
    await expect(link({}, deps())).rejects.toThrow(
      /memax link --space <slug>. Your spaces: a, b/,
    );
    await expect(link({ space: "nope" }, deps())).rejects.toThrow(
      /nope isn't one of your spaces/,
    );
  });

  it("refuses outside a git repository", async () => {
    const outside = join(h.home, "not-a-repo");
    mkdirSync(outside, { recursive: true });
    expect(await link({ space: "x" }, deps(outside))).toBe(1);
    expect(text()).toContain("inside a git repository");
  });

  it("offers to manage one block in a CLAUDE.md the person already has", async () => {
    const space = h.fake.addSpace("memax-v2");
    const claude = h.fake.addTarget(space, "claude_md");
    const agents = h.fake.addTarget(space, "agents_md");
    writeFileSync(join(h.repo, "CLAUDE.md"), "# Mine\n");
    writeFileSync(join(h.repo, "AGENTS.md"), "# Also mine\n");
    await link({ space: space.slug, yes: true }, deps());
    const patch = h.fake.calls(
      "PATCH",
      new RegExp(`/v2/targets/${claude.id}$`),
    );
    expect(patch).toHaveLength(1);
    expect(patch[0].body).toMatchObject({ settings: { user_owned: true } });
    expect(patch[0].headers["if-match"]).toBe('"1"');
    expect(patch[0].headers["x-memax-via"]).toBe("cli");
    expect(h.fake.entry(claude.id).target.settings.user_owned).toBe(true);
    expect(text()).toContain("Memax manages one block in CLAUDE.md");
    expect(text()).toContain(
      "AGENTS.md is already here and Memax didn't write it",
    );
    expect(h.fake.calls("PATCH", new RegExp(agents.id))).toHaveLength(0);
  });

  it("says how to sign in when signed out", async () => {
    h.fake.addSpace("memax-v2");
    const { Memax } = await import("memax-sdk");
    const signedOut = new Memax({ apiUrl: h.fake.url, maxRetries: 0 });
    await expect(
      link({}, { ...deps(), memax: signedOut }),
    ).rejects.toMatchObject({ status: 401 });
  });
});
