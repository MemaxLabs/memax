// `memax hook flush`: what the daemon does for the session-start hook, once,
// for a machine where no daemon runs. The hook starts it detached after it
// has printed: it reports the queued compile loads, refreshes the warm
// cache of the spaces they came from (so the next session start knows of
// new forgets and gates), and exits. One at a time (a lock file); a load
// it can't send stays queued for the next one, or for the daemon.
import { closeSync, mkdirSync, openSync, statSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { getClient, setClientFetch } from "../client.js";
import { sdkDaemonApi, type DaemonApi } from "../daemon/api.js";
import { liveDaemonPid } from "../daemon/control.js";
import { lightFetch } from "../daemon/http.js";
import { reportLoad } from "../daemon/loads.js";
import { fileLogger, type Logger } from "../daemon/log.js";
import { daemonPaths, type DaemonPaths } from "../daemon/paths.js";
import { readRegistry } from "../daemon/registry.js";
import { WarmCache } from "../daemon/warm.js";
import { flushLoads, pruneSessions, type FlushResult } from "./loads.js";

const LOCK_STALE_MS = 60_000;

function takeLock(path: string): (() => void) | null {
  const open = () => {
    closeSync(openSync(path, "wx", 0o600));
    return () => {
      try {
        unlinkSync(path);
      } catch {
        // already gone
      }
    };
  };
  try {
    return open();
  } catch {
    try {
      if (Date.now() - statSync(path).mtimeMs < LOCK_STALE_MS) return null;
      unlinkSync(path);
      return open();
    } catch {
      return null;
    }
  }
}

/**
 * Reports the queue and refreshes the cache of the linked spaces it
 * names. Exported for tests, which pass a fake server's API.
 */
export async function flushOnce(
  paths: DaemonPaths,
  api: DaemonApi,
  log: Logger,
  daemonRuns: () => boolean,
): Promise<FlushResult | null> {
  mkdirSync(paths.loads, { recursive: true, mode: 0o700 });
  const release = takeLock(join(paths.loads, ".flush.lock"));
  if (!release) return null;
  try {
    const r = await flushLoads(paths.loads, (l) => reportLoad(api, l));
    if (r.recorded + r.refused + r.expired + r.retry > 0)
      log.info("hook flush: reported compile loads", {
        recorded: r.recorded,
        refused: r.refused,
        expired: r.expired,
        retry: r.retry,
      });
    if (!daemonRuns()) {
      const links = readRegistry(paths);
      const warm = new WarmCache({ paths, api, log });
      warm.retain(new Set(links.map((l) => l.space_id)));
      const spaces = new Map(
        links
          .filter((l) => r.spaces.has(l.space_id))
          .map((l) => [l.space_id, l.space_slug]),
      );
      for (const [id, slug] of spaces) {
        try {
          warm.setTargets(id, slug, await api.listTargets(id));
          await warm.refresh(id, slug);
        } catch {
          // offline: the cache stays as it was
        }
      }
      warm.flush();
    }
    pruneSessions(paths.seen);
    return r;
  } finally {
    release();
  }
}

export async function runFlush(_argv: string[]): Promise<number> {
  const paths = daemonPaths();
  setClientFetch(lightFetch);
  const api = sdkDaemonApi(getClient());
  try {
    await flushOnce(
      paths,
      api,
      fileLogger(paths.log),
      () => liveDaemonPid(paths.pid) !== null,
    );
  } catch {
    // The queue stays for the next try.
  }
  return 0;
}
