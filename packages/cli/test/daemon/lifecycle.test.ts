// The running daemon: its loop, the watcher, restarts and the lock.
import { existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  AlreadyRunningError,
  controlRequest,
} from "../../src/lib/daemon/control.js";
import type { DaemonSnapshot } from "../../src/lib/daemon/snapshot.js";
import { unlinkRepo } from "../../src/lib/daemon/registry.js";
import { harness, until, type Harness } from "./harness.js";

let h: Harness;
beforeEach(async () => {
  h = await harness();
});
afterEach(async () => {
  await h.cleanup();
});

const AGENTS = (n: number) => `# Brief\n\n- Fact ${n}. [M-0${n}]\n`;
const agents = () => readFileSync(join(h.repo, "AGENTS.md"), "utf8");
const observations = () => h.fake.calls("POST", /\/observations$/);

function setup() {
  const space = h.fake.addSpace("memax-v2");
  const t = h.fake.addTarget(space, "agents_md");
  h.link(space);
  return { space, t };
}

describe("the loop", () => {
  it("delivers each new run on its own, and reports a hand edit seen by the watcher", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    const d = h.daemon();
    await d.start();
    await until(
      () => existsSync(join(h.repo, "AGENTS.md")) && agents() === AGENTS(1),
      3_000,
      "the first delivery",
    );
    h.fake.dirty(t.id);
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(2) });
    await until(() => agents() === AGENTS(2), 3_000, "the second delivery");
    await until(
      () => h.fake.entry(t.id).target.sync_state === "in_sync",
      3_000,
      "the ack",
    );

    // Its own write isn't a hand edit; a person's is, and only once.
    await new Promise((r) => setTimeout(r, 200));
    expect(observations()).toHaveLength(0);
    writeFileSync(join(h.repo, "AGENTS.md"), AGENTS(2) + "- Mine.\n");
    await until(() => observations().length === 1, 3_000, "the observation");
    await new Promise((r) => setTimeout(r, 300));
    expect(observations()).toHaveLength(1);
    expect(h.fake.entry(t.id).target.sync_state).toBe("drifted");
  });

  it("catches an edit by rescanning when there are no watcher events", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    const d = h.daemon({
      watch: { events: false, rescanMs: 50, debounceMs: 10, maxDelayMs: 50 },
    });
    await d.start();
    await until(
      () => h.fake.entry(t.id).target.sync_state === "in_sync",
      3_000,
      "delivery",
    );
    writeFileSync(join(h.repo, "AGENTS.md"), AGENTS(1) + "- Mine.\n");
    await until(
      () => observations().length === 1,
      3_000,
      "the rescan's report",
    );
  });

  it("is idempotent across a restart: no writes, acks or reports", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    const first = h.daemon();
    await first.start();
    await until(
      () => h.fake.entry(t.id).target.sync_state === "in_sync",
      3_000,
      "delivery",
    );
    await first.stop();
    const mtime = statSync(join(h.repo, "AGENTS.md")).mtimeMs;
    const calls = h.fake.log.length;

    const second = h.daemon();
    await second.start();
    await new Promise((r) => setTimeout(r, 400));
    await second.stop();
    expect(statSync(join(h.repo, "AGENTS.md")).mtimeMs).toBe(mtime);
    const after = h.fake.log.slice(calls);
    expect(after.filter((c) => c.method !== "GET")).toHaveLength(0);
    expect(after.filter((c) => c.path.endsWith("/preview"))).toHaveLength(0);
  });

  it("reports an edit made while it was stopped, on its next start", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    const first = h.daemon();
    await first.start();
    await until(
      () => h.fake.entry(t.id).target.sync_state === "in_sync",
      3_000,
      "delivery",
    );
    await first.stop();
    writeFileSync(join(h.repo, "AGENTS.md"), AGENTS(1) + "- While it slept.\n");
    const second = h.daemon();
    await second.start();
    await until(() => observations().length === 1, 3_000, "the report");
  });

  it("picks up a link and an unlink without a restart", async () => {
    const space = h.fake.addSpace("memax-v2");
    const t = h.fake.addTarget(space, "agents_md");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    const d = h.daemon();
    await d.start();
    h.link(space);
    await until(
      () => existsSync(join(h.repo, "AGENTS.md")),
      3_000,
      "delivery after link",
    );
    unlinkRepo(h.paths, h.repo);
    await until(() => d.snapshot().repos.length === 0, 3_000, "the unlink");
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(2) });
    await new Promise((r) => setTimeout(r, 300));
    expect(agents()).toBe(AGENTS(1));
  });

  it("backs off while the server fails, and recovers", async () => {
    const { t } = setup();
    h.fake.compile(t.id, { "AGENTS.md": AGENTS(1) });
    h.fake.fail(/\/targets$/, 503, "unavailable", 3);
    const d = h.daemon();
    await d.start();
    await until(
      () => (d.snapshot().repos[0]?.error ?? "").includes("503"),
      3_000,
      "the error",
    );
    await until(() => existsSync(join(h.repo, "AGENTS.md")), 3_000, "recovery");
    expect(
      h.log.lines.filter((l) => l.includes("can't reach the space")),
    ).toHaveLength(1);
  });
});

describe("one daemon per user", () => {
  it("refuses a second daemon, answers on the control socket, and stops through it", async () => {
    setup();
    let stopped = false;
    const d = h.daemon({ onStopped: () => (stopped = true) });
    await d.start();
    expect(readFileSync(h.paths.pid, "utf8").trim()).toBe(String(process.pid));
    await expect(h.daemon().start()).rejects.toBeInstanceOf(
      AlreadyRunningError,
    );

    const snap = (await controlRequest(h.paths, {
      cmd: "status",
    })) as DaemonSnapshot;
    expect(snap.pid).toBe(process.pid);
    expect(snap.repos[0].space_slug).toBe("memax-v2");

    await controlRequest(h.paths, { cmd: "stop" });
    await until(() => stopped, 3_000, "the stop");
    expect(existsSync(h.paths.pid)).toBe(false);
    expect(await controlRequest(h.paths, { cmd: "status" }, 500)).toBeNull();
  });

  it("takes over a socket left behind by a daemon that died", async () => {
    setup();
    // A daemon killed with SIGKILL leaves its socket file: present, but
    // nobody listens on it.
    const { spawn } = await import("node:child_process");
    const { mkdirSync } = await import("node:fs");
    mkdirSync(h.paths.dir, { recursive: true });
    const ghost = spawn(process.execPath, [
      "-e",
      `require("net").createServer().listen(${JSON.stringify(h.paths.socket)})`,
    ]);
    await until(() => existsSync(h.paths.socket), 3_000, "the ghost's socket");
    ghost.kill("SIGKILL");
    await new Promise((r) => ghost.once("exit", r));
    expect(existsSync(h.paths.socket)).toBe(true);
    expect(await controlRequest(h.paths, { cmd: "status" }, 500)).toBeNull();
    const d = h.daemon();
    await d.start();
    expect(await controlRequest(h.paths, { cmd: "status" })).not.toBeNull();
  });
});
