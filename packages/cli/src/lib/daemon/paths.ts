// Where the daemon keeps its files: ~/.memax/daemon/.
import { createHash } from "node:crypto";
import { chmodSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { getConfigDir } from "../config.js";

export interface DaemonPaths {
  dir: string;
  /** The linked repositories (`memax link`). */
  repos: string;
  /** What this device wrote: run refs and drift hashes, never content. */
  state: string;
  pid: string;
  log: string;
  /** The control socket; whoever listens on it is the one daemon. */
  socket: string;
  /** Held only while taking the socket over from a dead daemon. */
  lock: string;
}

// sun_path is 104 bytes on macOS and 108 on Linux.
const MAX_SOCKET_PATH = 100;

export function daemonPaths(
  dir: string = join(getConfigDir(), "daemon"),
): DaemonPaths {
  return {
    dir,
    repos: join(dir, "repos.json"),
    state: join(dir, "state.json"),
    pid: join(dir, "daemon.pid"),
    log: join(dir, "daemon.log"),
    socket: socketPath(dir),
    lock: join(dir, "daemon.lock"),
  };
}

function socketPath(dir: string): string {
  const inDir = join(dir, "daemon.sock");
  if (Buffer.byteLength(inDir) <= MAX_SOCKET_PATH) return inDir;
  // A home directory too deep for a socket path: a short name in /tmp,
  // unique per daemon directory.
  const tag = createHash("sha256").update(dir).digest("hex").slice(0, 12);
  return join("/tmp", `memax-daemon-${tag}.sock`);
}

/**
 * Why the daemon can't run on this platform, or null when it can. It needs
 * a Unix socket for its lock and control, which Windows doesn't offer
 * the same way (named pipes): a follow-up.
 */
export function daemonUnsupported(platform = process.platform): string | null {
  return platform === "win32"
    ? "The Memax daemon doesn't run on Windows yet. Your agents still read the space over MCP."
    : null;
}

/** Creates the daemon directory, readable by this user only. */
export function ensureDaemonDir(paths: DaemonPaths): void {
  mkdirSync(paths.dir, { recursive: true, mode: 0o700 });
  try {
    chmodSync(paths.dir, 0o700);
  } catch {
    // Not ours to change (an unusual mount); the files keep their own modes.
  }
}
