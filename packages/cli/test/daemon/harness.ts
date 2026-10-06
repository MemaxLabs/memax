// Temp directories, a fake /v2 server and a daemon wired to both.
import { mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Memax } from "memax-sdk";
import { sdkDaemonApi, type DaemonApi } from "../../src/lib/daemon/api.js";
import { Daemon, type DaemonOptions } from "../../src/lib/daemon/daemon.js";
import { memoryLogger } from "../../src/lib/daemon/log.js";
import { daemonPaths, type DaemonPaths } from "../../src/lib/daemon/paths.js";
import { linkRepo } from "../../src/lib/daemon/registry.js";
import { FakeV2 } from "./fake-v2.js";

export interface Harness {
  fake: FakeV2;
  api: DaemonApi;
  memax: Memax;
  paths: DaemonPaths;
  home: string;
  repo: string;
  log: ReturnType<typeof memoryLogger>;
  daemon(opts?: Partial<DaemonOptions>): Daemon;
  link(space: { id: string; slug: string }, root?: string): void;
  cleanup(): Promise<void>;
}

export async function harness(): Promise<Harness> {
  const fake = await new FakeV2().start();
  const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-daemon-")));
  const home = join(base, "home");
  const repo = join(base, "repo");
  const { mkdirSync } = await import("node:fs");
  mkdirSync(repo, { recursive: true });
  const paths = daemonPaths(join(home, ".memax", "daemon"));
  const memax = new Memax({
    apiUrl: fake.url,
    apiKey: "test-token",
    maxRetries: 0,
  });
  const api = sdkDaemonApi(memax);
  const log = memoryLogger();
  const daemons: Daemon[] = [];
  return {
    fake,
    api,
    memax,
    paths,
    home,
    repo,
    log,
    daemon(opts = {}) {
      const d = new Daemon({
        paths,
        api,
        deviceId: "device-1",
        log,
        version: "test",
        apiUrl: fake.url,
        cadence: {
          busyMs: 20,
          idleMs: 40,
          slowMs: 40,
          errorBaseMs: 20,
          errorMaxMs: 100,
          jitter: 0,
        },
        watch: { debounceMs: 20, maxDelayMs: 100, rescanMs: 60 },
        ...opts,
      });
      daemons.push(d);
      return d;
    },
    link(space, root = repo) {
      linkRepo(paths, { root, space_id: space.id, space_slug: space.slug });
    },
    async cleanup() {
      for (const d of daemons) await d.stop().catch(() => {});
      await fake.stop();
      rmSync(base, { recursive: true, force: true });
    },
  };
}

/** Waits until `check` returns true, or fails after `ms`. */
export async function until(
  check: () => boolean | Promise<boolean>,
  ms = 3_000,
  what = "the condition",
): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < ms) {
    if (await check()) return;
    await new Promise((r) => setTimeout(r, 15));
  }
  throw new Error(`timed out waiting for ${what}`);
}
