// `memax hook session-start` as the agents run it: the built CLI in its
// own process, with its own HOME, against the fake /v2 server. A preload
// (preload-events.mjs) records each stdout write and each attempt to open
// a socket, look a host up or start a process, so the tests can say the
// hook prints from local files without touching the network, and that
// the compile load is reported after it, by someone else.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { harness, until, type Harness } from "../daemon/harness.js";
import { header } from "./fixture.js";
import { installHookRoutes, type HookState } from "./fake-hook.js";

const cli = join(import.meta.dirname, "..", "..");
// Its own build, so it never races the daemon's process test over dist/.
const out = join(cli, "node_modules", ".cache", "memax-hook-test", "dist");
const bin = join(out, "bin.js");
const preload = join(import.meta.dirname, "preload-events.mjs");

beforeAll(() => {
  rmSync(out, { recursive: true, force: true });
  execFileSync(
    process.execPath,
    [
      join(cli, "node_modules", "typescript", "bin", "tsc"),
      "-p",
      cli,
      "--outDir",
      out,
    ],
    { stdio: "inherit" },
  );
}, 120_000);

let h: Harness;
let st: HookState;
let space: V2.Space;
const children: ChildProcess[] = [];
let events: string;

beforeEach(async () => {
  h = await harness();
  st = installHookRoutes(h.fake);
  space = h.fake.addSpace("acme-web");
  h.link(space);
  events = join(h.home, "events.log");
  writeFileSync(
    join(h.repo, "AGENTS.md"),
    `${header("C-0881")}\n## Decisions\n- Background jobs run on River. [M-0219]\n`,
  );
  // A waiting gate in the cache, so the first session has something to say.
  writeFileSync(
    h.paths.warm,
    JSON.stringify({
      version: 1,
      spaces: {
        [space.id]: {
          slug: "acme-web",
          updated_at: new Date().toISOString(),
          targets: [],
          forgotten: [],
          gates: [
            {
              ref: "G-0012",
              question: "Fly.io or Railway?",
              agent: "codex",
              asked_at: new Date().toISOString(),
              expires_at: new Date(Date.now() + 86_400_000).toISOString(),
            },
          ],
        },
      },
    }),
  );
});
afterEach(async () => {
  for (const c of children) if (c.exitCode === null) c.kill("SIGKILL");
  children.length = 0;
  await h.cleanup();
});

function env(): NodeJS.ProcessEnv {
  return {
    ...process.env,
    HOME: h.home,
    MEMAX_API_URL: h.fake.url,
    MEMAX_API_KEY: "test-token",
    MEMAX_TEST_EVENTS: events,
    // These tests are about what prints and what opens, not the budget
    // (measured on its own): a loaded CI runner can take more than 50 ms
    // to read an event already written.
    MEMAX_HOOK_STDIN_WAIT_MS: "2000",
    NODE_OPTIONS: `--import ${preload}`,
    XDG_CONFIG_HOME: "",
  };
}

interface Run {
  pid: number;
  code: number | null;
  stdout: string;
  ms: number;
}

function hook(
  event: Record<string, unknown>,
  opts: { closeStdin?: boolean; agent?: string } = {},
): Promise<Run> {
  const started = performance.now();
  const child = spawn(
    process.execPath,
    [bin, "hook", "session-start", "--agent", opts.agent ?? "claude-code"],
    { env: env(), cwd: h.home, stdio: ["pipe", "pipe", "pipe"] },
  );
  children.push(child);
  child.stdin!.write(JSON.stringify(event));
  if (opts.closeStdin !== false) child.stdin!.end();
  let stdout = "";
  child.stdout!.on("data", (b) => (stdout += String(b)));
  return new Promise((resolve) =>
    child.on("exit", (code) =>
      resolve({
        pid: child.pid!,
        code,
        stdout,
        ms: performance.now() - started,
      }),
    ),
  );
}

/** The events one process recorded, in order. */
function eventsOf(pid: number): string[] {
  let raw = "";
  try {
    raw = readFileSync(events, "utf8");
  } catch {
    return [];
  }
  return raw
    .trim()
    .split("\n")
    .filter((l) => l.startsWith(`${pid} `))
    .map((l) => l.slice(String(pid).length + 1));
}

/** A process whose command line says `daemon run`, so the hook sees a daemon. */
function fakeDaemonPid(): number {
  const c = spawn(
    process.execPath,
    ["-e", "setInterval(() => {}, 1e9)", "daemon", "run"],
    {
      stdio: "ignore",
    },
  );
  children.push(c);
  writeFileSync(h.paths.pid, `${c.pid}\n`);
  return c.pid!;
}

const startup = () => ({
  session_id: `sess-${Math.random().toString(16).slice(2)}`,
  cwd: h.repo,
  hook_event_name: "SessionStart",
  source: "startup",
});

describe("memax hook session-start, as a process", () => {
  it("prints from local files without opening a socket, and the daemon reports the load", async () => {
    const d = h.daemon();
    await d.start();
    fakeDaemonPid(); // the daemon here runs in the test's process
    const r = await hook(startup());
    expect(r.code).toBe(0);
    expect(r.stdout).toContain('<memax-context space="acme-web">');
    expect(r.stdout).toContain('G-0012 "Fly.io or Railway?"');
    const seen = eventsOf(r.pid);
    expect(seen).toContain("print");
    // No socket, no lookup and no process: not before printing, not after.
    expect(seen.filter((e) => e !== "print")).toEqual([]);
    await until(() => st.loads.length === 1, 5_000, "the daemon's report");
    expect(st.loads[0]).toMatchObject({
      space: space.id,
      body: { compile: "C-0881", agent: "claude-code" },
    });
  });

  it("starts a flush after printing when no daemon runs, and the flush reports the load", async () => {
    const r = await hook(startup());
    expect(r.code).toBe(0);
    const seen = eventsOf(r.pid);
    const print = seen.indexOf("print");
    const spawned = seen.findIndex((e) => e.startsWith("spawn "));
    expect(print).toBeGreaterThanOrEqual(0);
    expect(spawned).toBeGreaterThan(print);
    expect(seen[spawned]).toMatch(/ hook flush$/);
    expect(seen.filter((e) => e === "socket" || e === "dns")).toEqual([]);
    await until(() => st.loads.length === 1, 10_000, "the flush's report");
    // The recorder sees sockets: the flush, another process, opened one.
    const others = readFileSync(events, "utf8")
      .trim()
      .split("\n")
      .filter((l) => !l.startsWith(`${r.pid} `));
    expect(others.some((l) => l.endsWith(" socket"))).toBe(true);
    // The flush also refreshed the cache for the next session.
    await until(
      () =>
        JSON.parse(readFileSync(h.paths.warm, "utf8")).spaces[space.id]
          ?.targets !== undefined,
      5_000,
      "the cache",
    );
  });

  it("starts one flush for sessions that start together", async () => {
    const a = await hook(startup());
    const b = await hook(startup());
    const spawns = [...eventsOf(a.pid), ...eventsOf(b.pid)].filter((e) =>
      e.startsWith("spawn "),
    );
    expect(spawns).toHaveLength(1);
    await until(() => st.loads.length >= 1, 10_000, "the flush's report");
  });

  it("isn't held by a stdin pipe the agent leaves open", async () => {
    fakeDaemonPid();
    const r = await hook(startup(), { closeStdin: false });
    expect(r.code).toBe(0);
    expect(r.stdout).toContain("<memax-context");
    expect(r.ms).toBeLessThan(5_000);
  });

  it("prints once when two hooks run for the same session", async () => {
    fakeDaemonPid();
    const e = startup();
    const [a, b] = await Promise.all([hook(e), hook(e)]);
    expect(
      [a.stdout, b.stdout].filter((s) => s.includes("<memax-context")),
    ).toHaveLength(1);
  });

  it("prints nothing and exits 0 outside a repository", async () => {
    fakeDaemonPid();
    mkdirSync(join(h.home, "elsewhere"), { recursive: true });
    const r = await hook({ ...startup(), cwd: join(h.home, "elsewhere") });
    expect(r).toMatchObject({ code: 0, stdout: "" });
  });
});
