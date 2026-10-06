// `memax daemon run`: the daemon in the foreground, for service managers,
// `memax daemon start` (which runs it detached) and tests.
//
// It is reached from bin.ts without loading the rest of the CLI: the full
// command set pulls in the MCP SDK and friends, about 30 MB of memory a
// process that runs all day shouldn't carry. So no commander here: only
// the daemon and the CLI's auth (lib/client.ts).
import { getClient, setClientFetch } from "./lib/client.js";
import { lightFetch } from "./lib/daemon/http.js";
import { getOrCreateDeviceID, loadConfig } from "./lib/config.js";
import { sdkDaemonApi } from "./lib/daemon/api.js";
import { AlreadyRunningError } from "./lib/daemon/control.js";
import { Daemon } from "./lib/daemon/daemon.js";
import { fileLogger } from "./lib/daemon/log.js";
import {
  daemonPaths,
  daemonUnsupported,
  ensureDaemonDir,
} from "./lib/daemon/paths.js";
import { cliVersion } from "./lib/version.js";

const USAGE = `Usage: memax daemon run

Runs the Memax daemon in the foreground: it writes each space's compiled
files into the repositories linked on this machine (memax link), and
reports hand edits to them. Logs go to ~/.memax/daemon/daemon.log.
Stop it with Ctrl-C, SIGTERM, or memax daemon stop.
`;

export async function runDaemon(argv: string[]): Promise<number> {
  if (argv.includes("--help") || argv.includes("-h")) {
    process.stdout.write(USAGE);
    return 0;
  }
  const unknown = argv.filter((a) => a !== "");
  if (unknown.length > 0) {
    process.stderr.write(
      `memax daemon run takes no arguments (got ${unknown.join(" ")}).\n\n${USAGE}`,
    );
    return 2;
  }
  const unsupported = daemonUnsupported();
  if (unsupported) {
    process.stderr.write(`${unsupported}\n`);
    return 1;
  }
  const paths = daemonPaths();
  ensureDaemonDir(paths);
  const log = fileLogger(paths.log, {
    mirror: process.stderr.isTTY ? process.stderr : undefined,
  });
  setClientFetch(lightFetch);
  const daemon = new Daemon({
    paths,
    api: sdkDaemonApi(getClient()),
    deviceId: getOrCreateDeviceID(),
    log,
    version: cliVersion(),
    apiUrl: loadConfig().api_url,
    onStopped: () => process.exit(0),
  });
  try {
    await daemon.start();
  } catch (err) {
    if (err instanceof AlreadyRunningError) {
      process.stderr.write(`${err.message}.\n`);
      return 0; // the one that runs serves; a service manager mustn't retry
    }
    log.error("couldn't start", { error: (err as Error).message });
    process.stderr.write(
      `The Memax daemon couldn't start: ${(err as Error).message}\n`,
    );
    return 1;
  }
  const stop = (signal: string) => {
    log.info("stopping", { signal });
    void daemon.stop().then(() => process.exit(0));
  };
  process.once("SIGTERM", () => stop("SIGTERM"));
  process.once("SIGINT", () => stop("SIGINT"));
  process.on("SIGHUP", () => daemon.reload());
  process.on("unhandledRejection", (err) => {
    log.error("unexpected error", {
      error: (err as Error)?.message ?? String(err),
    });
  });
  process.on("uncaughtException", (err) => {
    log.error("crashed", { error: err.message });
    void daemon.stop().finally(() => process.exit(1));
  });
  // Runs until a signal or `memax daemon stop`; the control socket keeps
  // the event loop alive.
  return new Promise<number>(() => {});
}
