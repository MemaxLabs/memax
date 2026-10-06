// The daemon as a process: `memax daemon run` from the built CLI, with its
// own HOME, against the fake server. Signals, the lock between processes,
// the pid file, and how much memory it holds.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
} from "vitest";
import { startDaemon, stopDaemon } from "../../src/commands/daemon-control.js";
import { readDaemonStatus } from "../../src/commands/daemon-status.js";
import { harness, until, type Harness } from "./harness.js";

const cli = join(import.meta.dirname, "..", "..");
const bin = join(cli, "dist", "bin.js");

beforeAll(() => {
  // The process runs the built CLI; build it from this source.
  execFileSync(
    process.execPath,
    [join(cli, "node_modules", "typescript", "bin", "tsc"), "-p", cli],
    { stdio: "inherit" },
  );
}, 120_000);

let h: Harness;
const children: ChildProcess[] = [];
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  for (const c of children) if (c.exitCode === null) c.kill("SIGKILL");
  await h.cleanup();
});
afterAll(() => {});

function env(): NodeJS.ProcessEnv {
  return {
    ...process.env,
    HOME: h.home,
    MEMAX_API_URL: h.fake.url,
    MEMAX_API_KEY: "test-token",
    XDG_CONFIG_HOME: "",
  };
}

function run(): ChildProcess {
  const child = spawn(process.execPath, [bin, "daemon", "run"], {
    env: env(),
    stdio: ["ignore", "pipe", "pipe"],
  });
  children.push(child);
  return child;
}

const exited = (c: ChildProcess) =>
  new Promise<number | null>((r) =>
    c.exitCode !== null ? r(c.exitCode) : c.once("exit", (code) => r(code)),
  );

function rssKiB(pid: number): number | null {
  try {
    const m = readFileSync(`/proc/${pid}/status`, "utf8").match(
      /^VmRSS:\s+(\d+) kB/m,
    );
    return m ? Number(m[1]) : null;
  } catch {
    return null;
  }
}

describe("memax daemon run", () => {
  it("delivers, holds the lock against a second process, and stops cleanly on SIGTERM", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.link(space);
    h.fake.compile(t.id, {
      "AGENTS.md": "# Brief\n\n- Jobs run on River. [M-0219]\n",
    });

    const d = run();
    await until(
      () => h.fake.entry(t.id).target.sync_state === "in_sync",
      10_000,
      "the delivery",
    );
    expect(readFileSync(join(h.repo, "AGENTS.md"), "utf8")).toContain("River");
    expect(readFileSync(h.paths.pid, "utf8").trim()).toBe(String(d.pid));

    const second = run();
    let err = "";
    second.stderr!.on("data", (b) => (err += String(b)));
    expect(await exited(second)).toBe(0);
    expect(err).toContain("already running");

    const status = await readDaemonStatus(h.paths);
    expect(status).toMatchObject({ running: true, pid: d.pid });
    expect(status.repos[0].targets[0]).toMatchObject({
      label: "AGENTS.md",
      state: "in_sync",
    });

    const rss = rssKiB(d.pid!);
    if (rss !== null)
      console.log(
        `daemon RSS after delivering: ${(rss / 1024).toFixed(1)} MiB`,
      );

    d.kill("SIGTERM");
    expect(await exited(d)).toBe(0);
    expect(existsSync(h.paths.pid)).toBe(false);
    expect(existsSync(h.paths.socket)).toBe(false);
    const state = JSON.parse(readFileSync(h.paths.state, "utf8"));
    expect(state.repos[h.repo].targets[t.id].compile).toBe(
      h.fake.entry(t.id).target.last_compile!.ref,
    );
    expect(readFileSync(h.paths.log, "utf8")).toMatch(/INFO stopped/);
  }, 30_000);

  it("starts detached with memax daemon start and stops with memax daemon stop", async () => {
    h.fake.addSpace("memax-v2");
    const out: string[] = [];
    const launch = () => {
      const child = spawn(process.execPath, [bin, "daemon", "run"], {
        env: env(),
        detached: true,
        stdio: "ignore",
      });
      child.unref();
      children.push(child);
    };
    expect(
      await startDaemon({ paths: h.paths, out: (l) => out.push(l), launch }),
    ).toBe(0);
    expect(out.join("\n")).toContain("Started the Memax daemon");
    expect(
      await startDaemon({ paths: h.paths, out: (l) => out.push(l), launch }),
    ).toBe(0);
    expect(out.join("\n")).toContain("already running");
    expect(await stopDaemon({ paths: h.paths, out: (l) => out.push(l) })).toBe(
      0,
    );
    expect(out.join("\n")).toContain("Stopped the Memax daemon.");
    expect((await readDaemonStatus(h.paths)).running).toBe(false);
    expect(await stopDaemon({ paths: h.paths, out: (l) => out.push(l) })).toBe(
      0,
    );
    expect(out.at(-1)).toContain("isn't running");
  }, 30_000);
});
