// memax daemon status rendering, and the service units install writes.
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  readDaemonStatus,
  renderDaemonStatus,
  type DaemonStatus,
} from "../../src/commands/daemon-status.js";
import {
  installService,
  planUnit,
  uninstallService,
  type ServiceDeps,
} from "../../src/commands/daemon-service.js";
import { harness, type Harness } from "./harness.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

describe("memax daemon status", () => {
  it("shows each linked repository's targets from the running daemon", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    const cursor = h.fake.addTarget(space, "cursor_mdc");
    const gpt = h.fake.addTarget(space, "chatgpt");
    const gemini = h.fake.addTarget(space, "gemini_md");
    h.link(space);
    h.fake.compile(agents.id, { "AGENTS.md": "# Brief\n" });
    h.fake.compile(cursor.id, { ".cursor/rules/memax-web.mdc": "- x [M-1]\n" });
    h.fake.compile(gpt.id, {});
    h.fake.setState(gemini.id, "off");
    const d = h.daemon();
    await d.start();
    await d.syncOnce();
    const { writeFileSync } = await import("node:fs");
    writeFileSync(
      join(h.repo, ".cursor/rules/memax-web.mdc"),
      "- mine [M-1]\n",
    );
    h.fake.compile(cursor.id, { ".cursor/rules/memax-web.mdc": "- y [M-1]\n" });
    await d.syncOnce();

    const s = await readDaemonStatus(h.paths);
    expect(s.running).toBe(true);
    const text = plain(renderDaemonStatus(s, new Date()));
    expect(text).toContain(
      `● The Memax daemon is running · pid ${process.pid}`,
    );
    expect(text).toContain("→ memax-v2");
    expect(text).toMatch(/● AGENTS\.md\s+in sync\s+C-0\d+ · \d\d:\d\d/);
    expect(text).toMatch(/○ \.cursor\/rules\s+drifted\s+1 local edit/);
    expect(text).toMatch(
      /● ChatGPT project\s+in sync\s+copied out from the app/,
    );
    expect(text).toMatch(
      /- GEMINI\.md\s+off\s+stopped; the file stays where it is/,
    );
  });

  it("shows the last deliveries when the daemon isn't running", async () => {
    const space = h.fake.addSpace("memax-v2");
    const agents = h.fake.addTarget(space, "agents_md");
    h.link(space);
    const run = h.fake.compile(agents.id, { "AGENTS.md": "# Brief\n" });
    const d = h.daemon();
    await d.syncOnce();
    await d.stop();
    const s = await readDaemonStatus(h.paths);
    expect(s).toMatchObject({
      running: false,
      repos: [{ space_slug: "memax-v2" }],
    });
    const text = plain(renderDaemonStatus(s));
    expect(text).toContain(
      "The Memax daemon isn't running. Start it: memax daemon start",
    );
    expect(text).toContain(`AGENTS.md`);
    expect(text).toContain(`last delivery ${run.ref}`);
  });

  it("says how to link when nothing is linked", () => {
    const s: DaemonStatus = { running: false, repos: [] };
    expect(plain(renderDaemonStatus(s))).toContain(
      "No repositories linked on this machine. Link one: memax link",
    );
  });
});

describe("memax daemon install", () => {
  const input = {
    home: "/Users/zz",
    node: "/usr/local/bin/node",
    bin: "/usr/local/lib/node_modules/memax-cli/dist/bin.js",
    uid: 501,
    log: "/Users/zz/.memax/daemon/daemon.log",
  };

  it("writes a launchd agent on macOS", () => {
    const p = planUnit({
      ...input,
      platform: "darwin",
      apiUrl: "https://api.memax.app",
    })!;
    expect(p.path).toBe(
      "/Users/zz/Library/LaunchAgents/app.memax.daemon.plist",
    );
    expect(p.content).toContain("<string>/usr/local/bin/node</string>");
    expect(p.content).toContain("<string>--max-semi-space-size=1</string>");
    expect(p.content).toContain(
      "<string>daemon</string>\n    <string>run</string>",
    );
    expect(p.content).toContain("<key>SuccessfulExit</key>\n    <false/>");
    expect(p.content).toContain("<string>https://api.memax.app</string>");
    expect(p.enable).toEqual([["launchctl", "bootstrap", "gui/501", p.path]]);
  });

  it("writes a systemd user unit on Linux, quoting what systemd would expand", () => {
    const p = planUnit({
      ...input,
      platform: "linux",
      home: "/home/zz",
      bin: "/home/zz/my 100%/bin.js",
      xdgConfigHome: "",
    })!;
    expect(p.path).toBe("/home/zz/.config/systemd/user/memax-daemon.service");
    expect(p.content).toContain(
      'ExecStart="/usr/local/bin/node" "--max-semi-space-size=1" "--optimize-for-size" "/home/zz/my 100%%/bin.js" daemon run',
    );
    expect(p.content).toContain("Restart=on-failure");
    expect(p.content).not.toContain("Environment=");
    expect(p.enable.at(-1)).toEqual([
      "systemctl",
      "--user",
      "enable",
      "--now",
      "memax-daemon.service",
    ]);
    expect(planUnit({ ...input, platform: "win32" })).toBeNull();
  });

  function deps(
    over: Partial<ServiceDeps>,
  ): ServiceDeps & { lines: string[]; ran: string[][] } {
    const home = mkdtempSync(join(tmpdir(), "memax-unit-"));
    const lines: string[] = [];
    const ran: string[][] = [];
    return {
      paths: h.paths,
      bin: input.bin,
      plan: planUnit({ ...input, platform: "linux", home, xdgConfigHome: "" }),
      yes: false,
      interactive: false,
      out: (l) => lines.push(l),
      ask: async () => false,
      run: (c) => {
        ran.push(c);
        return { ok: true, output: "" };
      },
      lines,
      ran,
      ...over,
    };
  }

  it("shows what it writes, and writes nothing without a yes", async () => {
    const d = deps({});
    expect(await installService(d)).toBe(1);
    expect(existsSync(d.plan!.path)).toBe(false);
    expect(d.ran).toEqual([]);
    expect(plain(d.lines)).toContain("This writes");
    expect(plain(d.lines)).toContain("[Service]");
    expect(plain(d.lines)).toContain(
      "systemctl --user enable --now memax-daemon.service",
    );
    expect(plain(d.lines)).toContain("Run it again with --yes");

    const asked = deps({ interactive: true, ask: async () => false });
    expect(await installService(asked)).toBe(1);
    expect(existsSync(asked.plan!.path)).toBe(false);
  });

  it("writes the unit and enables it with a yes, and uninstall reverses it", async () => {
    const d = deps({ yes: true });
    expect(await installService(d)).toBe(0);
    expect(readFileSync(d.plan!.path, "utf8")).toBe(d.plan!.content);
    expect(d.ran).toEqual(d.plan!.enable);
    expect(await uninstallService(d)).toBe(0);
    expect(existsSync(d.plan!.path)).toBe(false);
    expect(d.ran.slice(2)).toEqual([
      ...d.plan!.disable,
      ["systemctl", "--user", "daemon-reload"],
    ]);
  });

  it("refuses to point a service at npx's cache", async () => {
    const d = deps({
      yes: true,
      bin: "/home/zz/.npm/_npx/abc/node_modules/memax-cli/dist/bin.js",
    });
    expect(await installService(d)).toBe(1);
    expect(plain(d.lines)).toContain("npm install -g memax-cli");
  });
});
