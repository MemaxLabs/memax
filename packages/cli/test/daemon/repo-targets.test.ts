// A personal space's home files (root: home) are written by a machine's
// home link, never into a repository: the daemon's repository delivery and
// `memax compile` read a space's targets without them.
import type { Memax, V2 } from "memax-sdk";
import { describe, expect, it } from "vitest";
import { repoTargets, sdkDaemonApi } from "../../src/lib/daemon/api.js";

const repo = {
  id: "t1",
  kind: "agents_md",
  root: "repo",
  path: "AGENTS.md",
} as V2.Target;
const old = { id: "t0", kind: "claude_md", path: "CLAUDE.md" } as V2.Target; // a server from before roots
const home = {
  id: "t2",
  kind: "agents_md",
  root: "home",
  path: ".memax/personal/AGENTS.md",
  machine: { id: "m1", label: "machine-1", name: "work-mbp" },
} as V2.Target;

describe("a repository's targets", () => {
  it("leave out every home file, even the canonical copy named AGENTS.md", () => {
    expect(repoTargets([repo, old, home])).toEqual([repo, old]);
  });

  it("are what the daemon lists", async () => {
    const memax = {
      v2: { targets: { list: async () => ({ items: [repo, home] }) } },
    } as unknown as Memax;
    expect(await sdkDaemonApi(memax).listTargets("personal")).toEqual([repo]);
  });
});
