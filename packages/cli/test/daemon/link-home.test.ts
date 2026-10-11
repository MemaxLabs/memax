// memax link --home and memax unlink --home against the fake /v2 server:
// what it refuses before anything happens, the consent, the link, the
// first delivery, and taking the layer off; plus memax compile and memax
// status with a home link, outside any repository.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { compile } from "../../src/commands/compile.js";
import {
  readDaemonStatus,
  renderDaemonStatus,
} from "../../src/commands/daemon-status.js";
import {
  linkHome,
  machineName,
  unlinkHome,
  type HomeDeps,
} from "../../src/commands/link-home.js";
import { buildStatus, renderStatus } from "../../src/commands/status.js";
import { readHomeLink } from "../../src/lib/daemon/home-link.js";
import { OneShotDelivery } from "../../src/lib/daemon/oneshot.js";
import { upsertManagedBlock } from "../../../compiler/src/index.js";
import { harness, type Harness } from "./harness.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

const MINE = "# My Codex notes\n\nPrefer rg to grep.\n";
const BLOCK = [
  "<!-- Compiled by Memax from personal. -->",
  "- British English. [M-0001]",
];

let h: Harness;
let out: string[];
let compiler: NodeJS.Timeout;
beforeEach(async () => {
  h = await harness();
  out = [];
  mkdirSync(join(h.home, ".claude"), { recursive: true });
  mkdirSync(join(h.home, ".codex"), { recursive: true });
  writeFileSync(join(h.home, ".codex", "AGENTS.md"), MINE);
  // The worker: compiles whatever is dirty, as the server's would.
  compiler = setInterval(() => {
    for (const e of h.fake.targets.values()) {
      const t = e.target;
      if (t.sync_state === "off" || t.compiled_gen >= t.dirty_gen) continue;
      const path = t.path!;
      h.fake.compile(t.id, {
        [path]: t.kind === "codex_user" ? BLOCK : BLOCK.join("\n") + "\n",
      });
    }
  }, 10);
});
afterEach(async () => {
  clearInterval(compiler);
  await h.cleanup();
});

const oneShot = () =>
  OneShotDelivery.open({
    paths: h.paths,
    api: h.api,
    deviceId: "device-1",
    log: h.log,
    version: "test",
    apiUrl: h.fake.url,
  });

function deps(over: Partial<HomeDeps> = {}): HomeDeps {
  return {
    memax: h.memax,
    paths: h.paths,
    cwd: h.home,
    out: (l) => out.push(l),
    interactive: true,
    home: h.home,
    env: {},
    platform: "linux",
    deviceId: "4b0e7a2c-5d1f-4c8e-9a6b-3f2d1e0c9b8a",
    hostname: "work-mbp.local",
    confirm: async () => true,
    oneShot,
    pollMs: 20,
    ...over,
  };
}

const machinePosts = () => h.fake.calls("POST", /\/machines$/);

describe("memax link --home", () => {
  it.each([
    [{ platform: "win32" as const }, "doesn't run on Windows yet"],
    [{ env: { CI: "true" } }, "doesn't run under CI"],
    [{ interactive: false }, "add --yes to link without asking"],
  ])("refuses before asking anything: %o", async (over, says) => {
    h.fake.addPersonalSpace();
    expect(await linkHome({}, deps(over))).toBe(1);
    expect(plain(out)).toContain(says);
    expect(h.fake.log).toHaveLength(0);
    expect(readHomeLink(h.paths)).toBeNull();
  });

  it("says what it would write and stops under --dry-run", async () => {
    h.fake.addPersonalSpace();
    expect(await linkHome({ dryRun: true }, deps({ interactive: false }))).toBe(
      0,
    );
    const text = plain(out);
    expect(text).toContain("~/.claude/rules/memax-personal.md");
    expect(text).toContain("one Memax block; your 3 lines stay as they are");
    expect(text).toContain("Nothing was linked or written (--dry-run).");
    expect(machinePosts()).toHaveLength(0);
    expect(readHomeLink(h.paths)).toBeNull();
  });

  it("links nothing when the person says no", async () => {
    h.fake.addPersonalSpace();
    const asked: string[] = [];
    const code = await linkHome(
      {},
      deps({
        confirm: async (q) => {
          asked.push(q);
          return false;
        },
      }),
    );
    expect(code).toBe(0);
    expect(asked[0]).toContain("memax unlink --home takes them off. [Y/n]");
    expect(machinePosts()).toHaveLength(0);
    expect(existsSync(join(h.home, ".claude", "rules"))).toBe(false);
  });

  it("refuses a Personal space still on V1 with memories, pointing at memax switch", async () => {
    const sp = h.fake.addPersonalSpace();
    delete sp.v2_enabled_at;
    h.fake.extra.push((method, path) =>
      method === "GET" && path === `/v2/spaces/${sp.id}/switch`
        ? {
            status: 200,
            data: {
              preview: { notes: { total: 4 } },
            } as unknown as V2.SpaceSwitch,
          }
        : null,
    );
    expect(await linkHome({ yes: true }, deps())).toBe(1);
    expect(plain(out)).toContain("memax switch --space personal");
    expect(machinePosts()).toHaveLength(0);
  });

  it("links the machine with the approved files, writes home.json, and waits for the first files", async () => {
    const sp = h.fake.addPersonalSpace();
    expect(await linkHome({ yes: true }, deps())).toBe(0);

    const body = machinePosts()[0].body as V2.LinkMachineInput;
    expect(body).toEqual({
      device_id: "4b0e7a2c-5d1f-4c8e-9a6b-3f2d1e0c9b8a",
      name: "work-mbp",
      platform: "linux",
      files: [
        { kind: "claude_user", path: ".claude/rules/memax-personal.md" },
        { kind: "codex_user", path: ".codex/AGENTS.md" },
        { kind: "agents_md", path: ".memax/personal/AGENTS.md" },
      ],
    });
    const link = readHomeLink(h.paths)!;
    expect(link).toMatchObject({
      space_id: sp.id,
      space_slug: "personal",
      machine_id: h.fake.machines[0].id,
      home: h.home,
    });
    expect(link.files.map((f) => [f.kind, f.abs])).toEqual([
      ["claude_user", join(h.home, ".claude/rules/memax-personal.md")],
      ["codex_user", join(h.home, ".codex/AGENTS.md")],
      ["agents_md", join(h.home, ".memax/personal/AGENTS.md")],
    ]);
    // Delivered in this process, since no daemon runs.
    expect(
      readFileSync(join(h.home, ".claude/rules/memax-personal.md"), "utf8"),
    ).toBe(BLOCK.join("\n") + "\n");
    expect(readFileSync(join(h.home, ".codex/AGENTS.md"), "utf8")).toBe(
      upsertManagedBlock(MINE, BLOCK),
    );
    const text = plain(out);
    expect(text).toContain(
      "Linked this machine (machine-1, work-mbp) to personal.",
    );
    expect(text).toContain("Written into this machine's home by memax compile");
  });

  it("names the machine from the short host name", () => {
    expect(machineName("ana-mbp.local")).toBe("ana-mbp");
    expect(machineName("")).toBe("this machine");
  });
});

describe("memax unlink --home", () => {
  it("unlinks on the server, takes the layer off byte for byte and removes home.json", async () => {
    h.fake.addPersonalSpace();
    await linkHome({ yes: true }, deps());
    out = [];
    expect(
      await unlinkHome({
        memax: h.memax,
        paths: h.paths,
        out: (l) => out.push(l),
        oneShot,
      }),
    ).toBe(0);
    expect(h.fake.calls("POST", /:unlink$/)).toHaveLength(1);
    expect(h.fake.machines[0].linked).toBe(false);
    expect(existsSync(join(h.home, ".claude/rules/memax-personal.md"))).toBe(
      false,
    );
    expect(existsSync(join(h.home, ".memax/personal/AGENTS.md"))).toBe(false);
    expect(readFileSync(join(h.home, ".codex/AGENTS.md"), "utf8")).toBe(MINE);
    expect(readHomeLink(h.paths)).toBeNull();
    const text = plain(out);
    expect(text).toContain("Unlinked this machine's home from personal.");
    expect(text).toContain(
      "~/.codex/AGENTS.md  Memax's block taken out; your lines stay",
    );
  });

  it("signed out, still takes it off here and says the machine stays listed", async () => {
    h.fake.addPersonalSpace();
    await linkHome({ yes: true }, deps());
    h.fake.fail(/:unlink$/, 401, "unauthorized");
    out = [];
    await unlinkHome({
      memax: h.memax,
      paths: h.paths,
      out: (l) => out.push(l),
      oneShot,
    });
    expect(existsSync(join(h.home, ".claude/rules/memax-personal.md"))).toBe(
      false,
    );
    expect(plain(out)).toContain("Memax still lists this machine");
  });

  it("changes nothing when Memax can't be reached", async () => {
    h.fake.addPersonalSpace();
    await linkHome({ yes: true }, deps());
    h.fake.fail(/:unlink$/, 503, "unavailable");
    out = [];
    expect(
      await unlinkHome({
        memax: h.memax,
        paths: h.paths,
        out: (l) => out.push(l),
        oneShot,
      }),
    ).toBe(1);
    expect(existsSync(join(h.home, ".claude/rules/memax-personal.md"))).toBe(
      true,
    );
    expect(readHomeLink(h.paths)).not.toBeNull();
  });
});

describe("memax compile and memax status with a home link", () => {
  it("compile, outside a repository, compiles and delivers this machine's home files", async () => {
    const sp = h.fake.addPersonalSpace();
    await linkHome({ yes: true }, deps());
    // Another machine's file in the same space is never this one's.
    const other = h.fake.addMachine(sp, undefined, "home-mini");
    h.fake.addHomeTarget(sp, other, "claude_user");
    out = [];
    const code = await compile(
      { format: "json" },
      {
        memax: h.memax,
        paths: h.paths,
        cwd: h.home,
        out: (l) => out.push(l),
        oneShot,
        pollMs: 20,
      },
    );
    expect(code).toBe(0);
    const report = JSON.parse(out.join("\n"));
    expect(report.space).toBe("personal");
    expect(report.home).toBe(true);
    expect(report.writer).toBe("compile");
    expect(
      report.targets.map((t: { label: string }) => t.label).sort(),
    ).toEqual([
      "~/.claude/rules/memax-personal.md",
      "~/.codex/AGENTS.md",
      "~/.memax/personal/AGENTS.md",
    ]);
    expect(h.fake.calls("POST", /:compile$/)).toHaveLength(3);
  });

  it("status shows this machine's home, outside a repository too", async () => {
    h.fake.addPersonalSpace();
    await linkHome({ yes: true }, deps());
    const { report, targets, homeTargets } = await buildStatus(h.memax, {
      cwd: h.home,
      paths: h.paths,
    });
    expect(report.space.slug).toBe("personal");
    expect(report.home).toMatchObject({
      space: "personal",
      machine: { label: "machine-1", name: "work-mbp", linked: true },
    });
    expect(report.home!.targets).toHaveLength(3);
    const text = plain(renderStatus(report, targets, new Date(), homeTargets));
    expect(text).toContain(
      "This machine's home → personal · machine-1 (work-mbp)",
    );
    expect(text).toContain("~/.codex/AGENTS.md");
    expect(text).not.toContain("Codex reads");

    // The person makes an override file, which Codex reads instead: said,
    // never moved silently.
    writeFileSync(join(h.home, ".codex", "AGENTS.override.md"), "# for now\n");
    const moved = await buildStatus(h.memax, { cwd: h.home, paths: h.paths });
    expect(
      plain(
        renderStatus(
          moved.report,
          moved.targets,
          new Date(),
          moved.homeTargets,
        ),
      ),
    ).toContain(
      "Codex reads ~/.codex/AGENTS.override.md now; run memax link --home to move Memax's block there.",
    );

    // memax daemon status, with no daemon: what this machine last wrote.
    const daemon = plain(
      renderDaemonStatus(await readDaemonStatus(h.paths), new Date()),
    );
    expect(daemon).toContain("This machine's home → personal");
    expect(daemon).toContain("~/.claude/rules/memax-personal.md");

    h.fake.unlinkMachine(h.fake.machines[0].id);
    const after = await buildStatus(h.memax, { cwd: h.home, paths: h.paths });
    expect(
      plain(
        renderStatus(
          after.report,
          after.targets,
          new Date(),
          after.homeTargets,
        ),
      ),
    ).toContain(
      "It was unlinked, so nothing is written here. Link it again: memax link --home",
    );
  });
});
