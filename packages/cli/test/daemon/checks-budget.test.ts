// The checks' budget (the stale-detection design §6.3): a run over a
// repository of 100,000 files with 2,000 conditions takes at most 3 git
// processes, 150 ms of Node CPU, 8 MiB read and 5 MB more RSS. CPU is
// process time, not wall clock: the checker's own (process.cpuUsage) and
// its git children's (their rusage, from /proc/self/stat's cutime and
// cstime once they're reaped), so a loaded machine moves it less. Wall
// time is printed beside them, not asserted, for the same reason. One
// run on another checker first compiles the code (a process's once, not a
// run's); then the measured checker's first run reports a full set, and
// the runs after it read the tree again and send nothing.
//
// The git processes and bytes don't depend on the machine, so every run
// asserts them, on a tree a tenth the size. The whole budget (100,000
// files, CPU and RSS asserted) is opt-in, since a loaded machine (a CI
// runner beside other jobs) moves CPU and RSS, and building the tree
// takes a minute there:
//
//   MEMAX_CHECKS_BUDGET=1 pnpm --filter memax-cli exec vitest run test/daemon/checks-budget.test.ts
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setFlagsFromString } from "node:v8";
import { runInNewContext } from "node:vm";
import type { V2 } from "memax-sdk";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { DaemonApi } from "../../src/lib/daemon/api.js";
import { CheckReporter, RepoChecker } from "../../src/lib/daemon/checks.js";
import { memoryLogger } from "../../src/lib/daemon/log.js";
import { daemonPaths } from "../../src/lib/daemon/paths.js";

const full = process.env.MEMAX_CHECKS_BUDGET === "1";
const FILES = full ? 100_000 : 10_000;
const DIRS = FILES / 100;
const EXACT = full ? 1_950 : 195;
const GLOBS = full ? 50 : 5;
const RUNS = full ? 20 : 3;

const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_NOSYSTEM: "1",
  HOME: tmpdir(),
};
const git = (cwd: string, args: string[], input?: string) =>
  execFileSync("git", args, {
    cwd,
    env: GIT_ENV,
    input,
    stdio: ["pipe", "pipe", "pipe"],
    maxBuffer: 64 << 20,
  });

const path = (i: number) =>
  `d${String(Math.floor(i / (FILES / DIRS))).padStart(4, "0")}/f${String(i % (FILES / DIRS)).padStart(3, "0")}.go`;

/** This process's reaped children's CPU, in ms (Linux; null elsewhere). */
function childrenCpuMs(): number | null {
  try {
    const stat = readFileSync("/proc/self/stat", "utf8");
    // Fields after the command's closing parenthesis: cutime and cstime
    // are the 16th and 17th fields of the line (14th and 15th after it).
    const rest = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
    const ticks = Number(rest[13]) + Number(rest[14]);
    return ticks * 10; // USER_HZ is 100 on Linux
  } catch {
    return null;
  }
}

// A collection before each measurement, so RSS says what a run holds on
// to rather than when V8 last collected.
setFlagsFromString("--expose-gc");
const gc = runInNewContext("gc") as () => void;

const p95 = (xs: number[]) =>
  [...xs].sort((a, b) => a - b)[Math.ceil(xs.length * 0.95) - 1];
const p50 = (xs: number[]) =>
  [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)];

describe("the checks' budget", () => {
  let base: string;
  let root: string;
  let conditions: V2.SpaceConditions;

  beforeAll(() => {
    base = realpathSync(mkdtempSync(join(tmpdir(), "memax-budget-")));
    root = join(base, "repo");
    git(base, ["init", "-q", "-b", "main", root]);
    // 100,000 files in one commit, through fast-import (one blob, shared).
    const lines = [
      "blob",
      "mark :1",
      "data 11",
      "package x\n",
      "commit refs/heads/main",
      "committer Test <test@example.com> 1760000000 +0000",
      "data 5",
      "init",
    ];
    for (let i = 0; i < FILES; i++) lines.push(`M 100644 :1 ${path(i)}`);
    git(root, ["fast-import", "--quiet"], lines.join("\n") + "\n");
    // Pushed, as far as the clone knows (no network: the refs say so).
    git(root, [
      "remote",
      "add",
      "origin",
      "https://github.com/MemaxLabs/memax.git",
    ]);
    git(root, ["update-ref", "refs/remotes/origin/main", "refs/heads/main"]);
    git(root, [
      "symbolic-ref",
      "refs/remotes/origin/HEAD",
      "refs/remotes/origin/main",
    ]);
    const items: V2.SpaceConditions["items"] = [];
    for (let i = 0; i < EXACT; i++)
      items.push(item(i, { path: path((i * 51) % FILES) }));
    for (let i = 0; i < GLOBS; i++)
      items.push(
        item(EXACT + i, { glob: `d${String(i * 20).padStart(4, "0")}/*.go` }),
      );
    conditions = { space_id: "space-1", repository: "MemaxLabs/memax", items };
    // Generating 100,000 files through fast-import takes seconds; a loaded
    // machine, several times that.
  }, 180_000);

  afterAll(() => rmSync(base, { recursive: true, force: true }));

  it(`runs within 3 git processes and 8 MiB read${full ? ", 150 ms of Node CPU and 5 MB more RSS" : ""}`, async () => {
    let sent = 0;
    const api = {
      conditions: async () => conditions,
      reportChecks: async (_s: string, input: V2.CheckRunInput) => {
        sent += input.results.length;
        return { run: { id: "run" } as V2.CheckRun, flagged: [] };
      },
    } as unknown as DaemonApi;
    const home = realpathSync(
      mkdtempSync(join(tmpdir(), "memax-budget-home-")),
    );
    const log = memoryLogger();
    const warm = daemonPaths(join(home, "warm"));
    await new RepoChecker({
      link: {
        root,
        space_id: "space-1",
        space_slug: "memax-v2",
        created_at: "",
      },
      api,
      paths: warm,
      log,
      deviceId: "device-1",
      reporter: new CheckReporter({ paths: warm, api, log }),
    }).run("conditions");
    sent = 0;
    const paths = daemonPaths(join(home, "daemon"));
    const c = new RepoChecker({
      link: {
        root,
        space_id: "space-1",
        space_slug: "memax-v2",
        created_at: "",
      },
      api,
      paths,
      log,
      deviceId: "device-1",
      reporter: new CheckReporter({ paths, api, log }),
      timing: { minGapMs: 0 },
    });
    try {
      const measure = async (trigger: "conditions" | "verify") => {
        gc();
        const rss = process.memoryUsage().rss;
        const cpu = process.cpuUsage();
        const kids = childrenCpuMs();
        const wall = performance.now();
        const run = await c.run(trigger);
        const used = process.cpuUsage(cpu);
        const kidsAfter = childrenCpuMs();
        return {
          run,
          nodeMs: (used.user + used.system) / 1000,
          gitMs: kids === null || kidsAfter === null ? null : kidsAfter - kids,
          wallMs: performance.now() - wall,
          rssMb: (process.memoryUsage().rss - rss) / 1e6,
        };
      };
      // The first run reports a full set (2,000 results); then each one
      // reads the tree again and sends nothing.
      const first = await measure("conditions");
      expect(first.run).toMatchObject({
        ran: true,
        results: EXACT + GLOBS,
        sent: EXACT + GLOBS,
      });
      expect(sent).toBe(EXACT + GLOBS);
      gc();
      const before = process.memoryUsage().rss;
      const steady = [];
      for (let i = 0; i < RUNS; i++) steady.push(await measure("verify"));
      gc();
      const grew = (process.memoryUsage().rss - before) / 1e6;
      const all = [first, ...steady];
      for (const m of all) {
        expect(m.run.cost?.processes).toBeLessThanOrEqual(3);
        expect(m.run.cost?.bytes).toBeLessThanOrEqual(8 * 1024 * 1024);
      }
      const node = steady.map((m) => m.nodeMs);
      const gitCpu = steady.map((m) => m.gitMs ?? 0);
      const wall = steady.map((m) => m.wallMs);
      console.log(
        [
          `checks over ${FILES} files and ${EXACT + GLOBS} conditions (a first run, then ${RUNS}):`,
          `  git processes per run: ${all.map((m) => m.run.cost?.processes).join(" ")}`,
          `  read from git: ${(first.run.cost!.bytes / 1024).toFixed(0)} KiB (first), ${(steady[0].run.cost!.bytes / 1024).toFixed(0)} KiB (after)`,
          `  Node CPU ms: first ${first.nodeMs.toFixed(1)}, p50 ${p50(node).toFixed(1)}, p95 ${p95(node).toFixed(1)}`,
          `  git CPU ms (children's rusage): first ${first.gitMs ?? "n/a"}, p50 ${p50(gitCpu)}, p95 ${p95(gitCpu)}`,
          `  wall ms (not asserted): first ${first.wallMs.toFixed(0)}, p50 ${p50(wall).toFixed(0)}, p95 ${p95(wall).toFixed(0)}`,
          `  RSS MB more: first ${first.rssMb.toFixed(1)}, over the ${RUNS} after it ${grew.toFixed(1)} (one run's most ${Math.max(...steady.map((m) => m.rssMb)).toFixed(1)})`,
        ].join("\n"),
      );
      if (full) {
        expect(p95(node)).toBeLessThanOrEqual(150);
        expect(first.rssMb).toBeLessThanOrEqual(5);
        expect(Math.max(...steady.map((m) => m.rssMb))).toBeLessThanOrEqual(5);
        expect(grew).toBeLessThanOrEqual(5);
      }
    } finally {
      await c.stop();
      rmSync(home, { recursive: true, force: true });
    }
    // Twenty-one runs over a 100,000-file tree take a few seconds on an
    // idle machine and much longer on a loaded one; the budget is CPU,
    // not this.
  }, 120_000);
});

function item(
  i: number,
  c: { path?: string; glob?: string },
): V2.SpaceConditions["items"][number] {
  return {
    memory: `M-${String(i + 1).padStart(4, "0")}`,
    conditions: [
      {
        condition: {
          id: "c1",
          kind: "path_exists",
          ...c,
          origin: "derived",
          from: "statement",
        } as V2.Condition,
        hash: (i + 1).toString(16).padStart(64, "0"),
        armed: true,
      },
    ],
  };
}
