// `memax hook session-start` and `memax hook flush`, reached from bin.ts
// without loading the rest of the CLI: a session-start hook runs every
// time an agent starts, and its budget (plan 25 §7.5) is 100 ms from
// process start to exit. So this file imports Node built-ins only, and the
// critical path is local files: read the warm cache, the compiled files
// and what the last session was told, print, exit. The compile loads are
// queued after printing, and reported by the daemon, or by a detached
// `memax hook flush` when no daemon runs.
import {
  appendFileSync,
  existsSync,
  fstatSync,
  mkdirSync,
  read,
  readFileSync,
  statSync,
  writeSync,
} from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { hookAgent } from "./lib/hook/agents.js";
import { enqueueLoads } from "./lib/hook/loads.js";
import { claimSession, writeSeen } from "./lib/hook/seen.js";
import { hookPaths, sessionStart } from "./lib/hook/session-start.js";

const USAGE = `Usage: memax hook session-start [--agent <id>] [--debug]
       memax hook flush

session-start prints a short <memax-context> block for an agent's session
start hook: what changed in this repository's compiled files since the
agent's last session here, what a person forgot, and decisions waiting
for a person. It reads local files only, and prints nothing when nothing
changed. --agent is claude-code (default), codex, gemini, cursor or
copilot, and sets the output format.

flush reports the compile loads session-start queued (the daemon does
this when it runs).
`;

/** How long to wait for the agent's stdin event before going without it. */
const STDIN_WAIT_MS = 50;

function flag(argv: string[], name: string): string | undefined {
  const i = argv.indexOf(name);
  if (i >= 0) return argv[i + 1];
  const eq = argv.find((a) => a.startsWith(`${name}=`));
  return eq?.slice(name.length + 1);
}

/**
 * The agent's JSON event on stdin. Agents write it and close the pipe; a
 * complete object ends the wait at once, and a pipe nobody writes to
 * costs at most STDIN_WAIT_MS.
 */
export function readEvent(
  ms = STDIN_WAIT_MS,
): Promise<Record<string, unknown>> {
  // Agents pass a pipe (or a socket pair); a terminal or /dev/null has no
  // event, and reading a terminal would take a person's keystrokes.
  try {
    const st = fstatSync(0);
    if (!st.isFIFO() && !st.isSocket() && !st.isFile())
      return Promise.resolve({});
  } catch {
    return Promise.resolve({});
  }
  return new Promise((resolve) => {
    const chunks: Buffer[] = [];
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve(parse(Buffer.concat(chunks).toString("utf8")) ?? {});
    };
    const timer = setTimeout(finish, ms);
    const buf = Buffer.alloc(64 * 1024);
    const step = () =>
      read(0, buf, 0, buf.length, null, (err, n) => {
        if (done) return;
        if (err && (err as NodeJS.ErrnoException).code === "EAGAIN") {
          setTimeout(step, 2);
          return;
        }
        if (err || n === 0) return finish();
        chunks.push(Buffer.from(buf.subarray(0, n)));
        if (parse(Buffer.concat(chunks).toString("utf8"))) return finish();
        step();
      });
    step();
  });
}

function parse(text: string): Record<string, unknown> | null {
  const t = text.trim();
  if (!t.startsWith("{")) return null;
  try {
    const v: unknown = JSON.parse(t);
    return v && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

function print(text: string): void {
  if (text === "") return;
  const buf = Buffer.from(text, "utf8");
  let off = 0;
  while (off < buf.length) {
    try {
      off += writeSync(1, buf, off);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EAGAIN") throw err;
    }
  }
}

/** Whether the daemon runs: its pid file names a live `daemon run`. */
function daemonAlive(pidFile: string): boolean {
  let pid: number;
  try {
    pid = Number(readFileSync(pidFile, "utf8").trim());
    process.kill(pid, 0);
  } catch {
    return false;
  }
  if (process.platform !== "linux") return true;
  try {
    return /\bdaemon\u0000run\b/.test(
      readFileSync(`/proc/${pid}/cmdline`, "utf8"),
    );
  } catch {
    return false;
  }
}

/** Starts `memax hook flush` on its own, unless one is already at it. */
async function spawnFlush(lock: string): Promise<void> {
  try {
    if (Date.now() - statSync(lock).mtimeMs < 60_000) return;
  } catch {
    // no flusher running
  }
  const bin = fileURLToPath(new URL("./bin.js", import.meta.url));
  if (!existsSync(bin)) return;
  const { spawn } = await import("node:child_process");
  const child = spawn(process.execPath, [bin, "hook", "flush"], {
    detached: true,
    stdio: "ignore",
    env: process.env,
  });
  child.on("error", () => {});
  child.unref();
}

function logError(dir: string, err: unknown): void {
  try {
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    appendFileSync(
      join(dir, "hook.log"),
      `${new Date().toISOString()} session-start: ${(err as Error)?.message ?? String(err)}\n`,
      { mode: 0o600 },
    );
  } catch {
    // nowhere to say it; the session starts regardless
  }
}

/** The session-start hook. It always exits 0: a hook must never stop a session. */
export async function hookSessionStart(argv: string[]): Promise<number> {
  const started = performance.now();
  const debug = argv.includes("--debug");
  const paths = hookPaths();
  try {
    const agent = hookAgent(flag(argv, "--agent") ?? "claude-code");
    const event = await readEvent();
    const session =
      typeof event.session_id === "string"
        ? event.session_id
        : typeof event.sessionId === "string"
          ? event.sessionId
          : "";
    const source = typeof event.source === "string" ? event.source : "";
    if (
      session &&
      !claimSession(paths.seen, `${agent.kind}:${session}`, source)
    ) {
      if (debug) process.stderr.write("memax hook: this session already ran\n");
      return 0;
    }
    const res = sessionStart({
      agent,
      event,
      env: process.env,
      cwd: process.cwd(),
      paths,
      now: new Date(),
    });
    print(res.output);
    // Off the critical path: the agent has its block.
    if (res.seen) writeSeen(res.seen.path, paths.seen, res.seen.value);
    if (res.loads.length > 0) {
      enqueueLoads(paths.loads, res.loads);
      if (!daemonAlive(paths.pid))
        await spawnFlush(join(paths.loads, ".flush.lock"));
    }
    if (debug)
      process.stderr.write(
        `memax hook: ${res.skipped ?? "printed"}; ${res.loads.length} load(s) queued; ${Math.round(performance.now() - started)} ms\n`,
      );
  } catch (err) {
    logError(paths.dir, err);
    if (debug) process.stderr.write(`memax hook: ${(err as Error).message}\n`);
  }
  return 0;
}

export async function runHook(sub: string, argv: string[]): Promise<number> {
  if (argv.includes("--help") || argv.includes("-h")) {
    process.stdout.write(USAGE);
    return 0;
  }
  if (sub === "session-start") return hookSessionStart(argv);
  if (sub === "flush") {
    const { runFlush } = await import("./lib/hook/flush.js");
    return runFlush(argv);
  }
  process.stderr.write(USAGE);
  return 2;
}
