// The control socket (~/.memax/daemon/daemon.sock). Listening on it is
// the single-instance lock: the OS frees it when the daemon dies, so a
// stale lock is one nobody answers on. `memax daemon status|stop`,
// `memax link` and `memax compile` talk to the running daemon through it,
// so stopping one never signals a pid that may have been reused.
//
// One request per connection: a JSON line in, a JSON line out.
import { closeSync, openSync, statSync, unlinkSync, writeSync } from "node:fs";
import { createConnection, createServer, type Server } from "node:net";
import { isCode } from "./fs-atomic.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";

export type ControlRequest =
  | { cmd: "status" }
  | { cmd: "stop" }
  | { cmd: "reload" }
  | { cmd: "wake"; space?: string };

export type ControlHandler = (req: ControlRequest) => Promise<unknown>;

export class AlreadyRunningError extends Error {
  constructor(readonly pid: number | undefined) {
    super(
      pid
        ? `the Memax daemon is already running (pid ${pid})`
        : "the Memax daemon is already running",
    );
    this.name = "AlreadyRunningError";
  }
}

const MAX_REQUEST = 64 * 1024;

export class ControlServer {
  private constructor(
    private readonly server: Server,
    private readonly path: string,
  ) {}

  /** Listens on the socket, or throws AlreadyRunningError. */
  static async listen(
    paths: DaemonPaths,
    handler: ControlHandler,
  ): Promise<ControlServer> {
    ensureDaemonDir(paths);
    const server = createServer((sock) => {
      let buf = "";
      sock.setEncoding("utf8");
      sock.setTimeout(5_000, () => sock.destroy());
      sock.on("error", () => sock.destroy());
      sock.on("data", (chunk: string) => {
        buf += chunk;
        if (buf.length > MAX_REQUEST) return sock.destroy();
        const nl = buf.indexOf("\n");
        if (nl < 0) return;
        let req: ControlRequest;
        try {
          req = JSON.parse(buf.slice(0, nl)) as ControlRequest;
        } catch {
          sock.end(JSON.stringify({ ok: false, error: "bad request" }) + "\n");
          return;
        }
        handler(req).then(
          (data) => sock.end(JSON.stringify({ ok: true, data }) + "\n"),
          (err: Error) =>
            sock.end(JSON.stringify({ ok: false, error: err.message }) + "\n"),
        );
      });
    });
    await takeOver(paths, server);
    return new ControlServer(server, paths.socket);
  }

  async close(): Promise<void> {
    await new Promise<void>((resolve) => this.server.close(() => resolve()));
    try {
      unlinkSync(this.path);
    } catch {
      // already gone
    }
  }
}

function listenOn(server: Server, path: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const onError = (err: Error) => {
      server.off("listening", onListening);
      reject(err);
    };
    const onListening = () => {
      server.off("error", onError);
      resolve();
    };
    server.once("error", onError);
    server.once("listening", onListening);
    server.listen(path);
  });
}

/**
 * Takes the socket: listens, and if a dead daemon left its socket file
 * behind, removes it and listens again. The lock file makes sure two
 * daemons starting at once don't both remove a socket and both listen.
 */
async function takeOver(paths: DaemonPaths, server: Server): Promise<void> {
  const lock = acquireLockFile(paths.lock);
  try {
    try {
      await listenOn(server, paths.socket);
      return;
    } catch (err) {
      if (!isCode(err, "EADDRINUSE")) throw err;
    }
    const answer = await controlRequest(paths, { cmd: "status" }, 2_000);
    if (answer) throw new AlreadyRunningError((answer as { pid?: number }).pid);
    unlinkSync(paths.socket);
    await listenOn(server, paths.socket);
  } finally {
    lock();
  }
}

/** An O_EXCL lock file, taken from its holder if it is older than 10 s. */
function acquireLockFile(path: string): () => void {
  for (let attempt = 0; attempt < 50; attempt++) {
    try {
      const fd = openSync(path, "wx", 0o600);
      writeSync(fd, String(process.pid));
      closeSync(fd);
      return () => {
        try {
          unlinkSync(path);
        } catch {
          // gone
        }
      };
    } catch (err) {
      if (!isCode(err, "EEXIST")) throw err;
      try {
        if (Date.now() - statSync(path).mtimeMs > 10_000) unlinkSync(path);
      } catch {
        // removed by its holder meanwhile
      }
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 100);
    }
  }
  throw new AlreadyRunningError(undefined);
}

/**
 * Sends one request to the running daemon. Null when no daemon answers:
 * not running, or a socket left by one that died.
 */
export function controlRequest(
  paths: DaemonPaths,
  req: ControlRequest,
  timeoutMs = 3_000,
): Promise<unknown | null> {
  return new Promise((resolve) => {
    let buf = "";
    let done = false;
    const finish = (v: unknown | null) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      sock.destroy();
      resolve(v);
    };
    const sock = createConnection(paths.socket);
    const timer = setTimeout(() => finish(null), timeoutMs);
    sock.setEncoding("utf8");
    sock.on("connect", () => sock.write(JSON.stringify(req) + "\n"));
    sock.on("data", (chunk: string) => {
      buf += chunk;
      const nl = buf.indexOf("\n");
      if (nl < 0) return;
      try {
        const res = JSON.parse(buf.slice(0, nl)) as {
          ok: boolean;
          data?: unknown;
        };
        finish(res.ok ? (res.data ?? {}) : null);
      } catch {
        finish(null);
      }
    });
    sock.on("error", () => finish(null));
    sock.on("close", () => finish(null));
  });
}
