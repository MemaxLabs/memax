// Starting and stopping the daemon (memax daemon start | stop), shared by
// the daemon commands and the service install.
import { spawn } from "node:child_process";
import {
  closeSync,
  existsSync,
  openSync,
  readFileSync,
  unlinkSync,
} from "node:fs";
import { homedir } from "node:os";
import { fileURLToPath } from "node:url";
import chalk from "chalk";
import { controlRequest } from "../lib/daemon/control.js";
import {
  daemonPaths,
  daemonUnsupported,
  ensureDaemonDir,
  type DaemonPaths,
} from "../lib/daemon/paths.js";
import type { DaemonSnapshot } from "../lib/daemon/snapshot.js";
import { tildify } from "./v2-output.js";

type Out = (line: string) => void;

/** The `memax` entry that runs the daemon on its own (dist/bin.js). */
export function binPath(): string {
  return fileURLToPath(new URL("../bin.js", import.meta.url));
}

/**
 * V8 settings for a process that idles all day: a 1 MiB young generation
 * and memory over speed. They save about 3 MiB of private memory.
 */
export const DAEMON_NODE_FLAGS = [
  "--max-semi-space-size=1",
  "--optimize-for-size",
];

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function status(
  paths: DaemonPaths,
  ms = 1_500,
): Promise<DaemonSnapshot | null> {
  return (await controlRequest(
    paths,
    { cmd: "status" },
    ms,
  )) as DaemonSnapshot | null;
}

function logTail(paths: DaemonPaths, n = 6): string[] {
  try {
    return readFileSync(paths.log, "utf8").trimEnd().split("\n").slice(-n);
  } catch {
    return [];
  }
}

export interface StartDeps {
  paths: DaemonPaths;
  out: Out;
  /** Starts `memax daemon run` detached (tests swap it). */
  launch?: (paths: DaemonPaths) => void;
  waitMs?: number;
}

function launchDetached(paths: DaemonPaths): void {
  // Its stderr goes to the log, so even a crash before the first log line
  // leaves a trace there.
  const fd = openSync(paths.log, "a", 0o600);
  try {
    const child = spawn(
      process.execPath,
      [...DAEMON_NODE_FLAGS, binPath(), "daemon", "run"],
      {
        detached: true,
        stdio: ["ignore", "ignore", fd],
        cwd: homedir(),
        env: process.env,
      },
    );
    child.unref();
  } finally {
    closeSync(fd);
  }
}

export async function startDaemon(d: StartDeps): Promise<number> {
  const unsupported = daemonUnsupported();
  if (unsupported) {
    d.out(chalk.yellow(`  ${unsupported}`));
    return 1;
  }
  let running = await status(d.paths);
  // `memax compile` holds the lock while it writes files; wait for it.
  for (let i = 0; running?.oneshot && i < 200; i++) {
    await sleep(150);
    running = await status(d.paths, 500);
  }
  if (running) {
    d.out(
      `  ${chalk.green("●")} The Memax daemon is already running · pid ${running.pid}`,
    );
    return 0;
  }
  ensureDaemonDir(d.paths);
  (d.launch ?? launchDetached)(d.paths);
  const deadline = Date.now() + (d.waitMs ?? 8_000);
  while (Date.now() < deadline) {
    await sleep(150);
    const snap = await status(d.paths, 500);
    if (snap) {
      d.out(`  ${chalk.green("✓")} Started the Memax daemon · pid ${snap.pid}`);
      d.out(chalk.gray(`    Logs: ${tildify(d.paths.log)}`));
      if (snap.repos.length === 0)
        d.out(
          chalk.gray("    No repositories linked yet. Link one: memax link"),
        );
      return 0;
    }
  }
  d.out(chalk.red("  The Memax daemon didn't start."));
  for (const line of logTail(d.paths)) d.out(chalk.gray(`    ${line}`));
  return 1;
}

export async function stopDaemon(d: {
  paths: DaemonPaths;
  out: Out;
  waitMs?: number;
}): Promise<number> {
  const running = await status(d.paths);
  if (!running) {
    if (existsSync(d.paths.pid)) unlinkSync(d.paths.pid); // left by a crash
    d.out(chalk.gray("  The Memax daemon isn't running."));
    return 0;
  }
  await controlRequest(d.paths, { cmd: "stop" }, 3_000);
  let deadline = Date.now() + (d.waitMs ?? 10_000);
  let signalled = false;
  while ((await status(d.paths, 300)) !== null) {
    if (Date.now() > deadline) {
      if (signalled) {
        d.out(
          chalk.red(
            `  The Memax daemon (pid ${running.pid}) didn't stop. Stop it with: kill ${running.pid}`,
          ),
        );
        return 1;
      }
      // It answered a moment ago, so this pid is the daemon's own.
      process.kill(running.pid, "SIGTERM");
      signalled = true;
      deadline = Date.now() + 5_000;
    }
    await sleep(150);
  }
  d.out(`  ${chalk.green("✓")} Stopped the Memax daemon.`);
  return 0;
}
