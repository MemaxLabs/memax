// The daemon: one feed per linked space, one RepoDelivery and watcher per
// linked repository, the control socket, the pid file and the state file.
import { watch, type FSWatcher } from "node:fs";
import { unlinkSync, writeFileSync } from "node:fs";
import { basename } from "node:path";
import { failureOf, type DaemonApi } from "./api.js";
import { ControlServer, type ControlRequest } from "./control.js";
import { PollingFeed, type Cadence, type TargetFeed } from "./feed.js";
import type { FsHooks } from "./fs-atomic.js";
import type { Logger } from "./log.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";
import { readRegistry, registryStamp, type LinkedRepo } from "./registry.js";
import { RepoDelivery } from "./repo.js";
import type { DaemonSnapshot } from "./snapshot.js";
import { DeviceState } from "./state.js";
import { RepoWatcher, type WatchOptions } from "./watch.js";

export interface DaemonOptions {
  paths: DaemonPaths;
  api: DaemonApi;
  deviceId: string;
  log: Logger;
  version: string;
  apiUrl: string;
  cadence?: Partial<Cadence>;
  watch?: Partial<WatchOptions>;
  hooks?: FsHooks;
  /** Called after a stop request, once everything is closed. */
  onStopped?(): void;
}

interface Repo {
  link: LinkedRepo;
  delivery: RepoDelivery;
  watcher: RepoWatcher;
}

interface Space {
  slug: string;
  roots: Set<string>;
  feed: TargetFeed;
  lastError?: string;
}

export class Daemon {
  readonly state: DeviceState;
  private repos = new Map<string, Repo>();
  private spaces = new Map<string, Space>();
  private stamp = -1;
  private dirWatch: FSWatcher | null = null;
  private control: ControlServer | null = null;
  private running = false;
  private stopping: Promise<void> | null = null;
  private readonly startedAt = new Date().toISOString();

  constructor(private readonly o: DaemonOptions) {
    ensureDaemonDir(o.paths);
    this.state = new DeviceState(o.paths);
  }

  /** Takes the lock and starts polling, watching and serving. */
  async start(): Promise<void> {
    this.control = await ControlServer.listen(this.o.paths, (req) =>
      this.handle(req),
    );
    writeFileSync(this.o.paths.pid, `${process.pid}\n`, { mode: 0o600 });
    this.running = true;
    this.reload();
    try {
      this.dirWatch = watch(
        this.o.paths.dir,
        { persistent: false },
        (_e, name) => {
          if (!name || String(name) === basename(this.o.paths.repos))
            this.reload();
        },
      );
      this.dirWatch.on("error", () => {
        this.dirWatch?.close();
        this.dirWatch = null; // each poll still checks the registry
      });
    } catch {
      this.dirWatch = null;
    }
    this.o.log.info("started", {
      pid: process.pid,
      version: this.o.version,
      repos: this.repos.size,
    });
  }

  private async handle(req: ControlRequest): Promise<unknown> {
    switch (req.cmd) {
      case "status":
        return this.snapshot();
      case "reload":
        this.reload();
        return {};
      case "wake":
        this.wake(req.space);
        return {};
      case "stop":
        setImmediate(() => void this.stop().then(() => this.o.onStopped?.()));
        return { pid: process.pid };
    }
  }

  /** Re-reads the registry and adds or drops repositories and spaces. */
  reload(): void {
    const stamp = registryStamp(this.o.paths);
    if (stamp === this.stamp) return;
    this.stamp = stamp;
    const links = new Map(readRegistry(this.o.paths).map((l) => [l.root, l]));
    for (const [root, repo] of this.repos) {
      const link = links.get(root);
      if (!link || link.space_id !== repo.link.space_id)
        this.dropRepo(root, !link);
    }
    for (const link of links.values())
      if (!this.repos.has(link.root)) this.addRepo(link);
  }

  private addRepo(link: LinkedRepo): void {
    let watcher: RepoWatcher | null = null;
    const delivery = new RepoDelivery(link, {
      api: this.o.api,
      state: this.state,
      log: this.o.log,
      deviceId: this.o.deviceId,
      hooks: this.o.hooks,
      recheck: (p) => watcher?.touch(p),
    });
    watcher = new RepoWatcher(
      link.root,
      (p) => void delivery.check(p),
      this.o.log,
      this.o.watch,
    );
    this.repos.set(link.root, { link, delivery, watcher });
    let space = this.spaces.get(link.space_id);
    if (!space) {
      space = {
        slug: link.space_slug,
        roots: new Set(),
        feed: this.feedFor(link.space_id),
      };
      this.spaces.set(link.space_id, space);
      if (this.running) space.feed.start();
    }
    space.roots.add(link.root);
    if (this.running) watcher.start();
    space.feed.wake();
    this.o.log.info("watching a repository", {
      root: link.root,
      space: link.space_slug,
    });
  }

  private dropRepo(root: string, forget: boolean): void {
    const repo = this.repos.get(root);
    if (!repo) return;
    repo.watcher.stop();
    repo.delivery.stop();
    this.repos.delete(root);
    if (forget) this.state.forgetRepo(root);
    const space = this.spaces.get(repo.link.space_id);
    space?.roots.delete(root);
    if (space && space.roots.size === 0) {
      void space.feed.stop();
      this.spaces.delete(repo.link.space_id);
    }
    this.o.log.info("stopped watching a repository", { root });
  }

  private feedFor(spaceId: string): TargetFeed {
    return new PollingFeed(
      {
        fetch: (signal) => this.o.api.listTargets(spaceId, signal),
        probe: (signal) => this.o.api.changeToken(spaceId, signal),
        onTargets: async (targets) => {
          this.reload();
          const space = this.spaces.get(spaceId);
          if (!space) return { busy: false };
          if (space.lastError) {
            this.o.log.info("reached the space again", { space: space.slug });
            space.lastError = undefined;
          }
          let busy = false;
          for (const root of space.roots) {
            const repo = this.repos.get(root);
            if (!repo) continue;
            repo.delivery.setError(undefined);
            const r = await repo.delivery.sync(targets);
            busy = busy || r.busy;
            repo.watcher.setPaths(repo.delivery.trackedPaths());
          }
          return { busy };
        },
        onError: (err, retryInMs) => {
          const space = this.spaces.get(spaceId);
          if (!space) return;
          const f = failureOf(err);
          const message = errorMessage(f.status, f.code, space.slug);
          for (const root of space.roots)
            this.repos.get(root)?.delivery.setError(message);
          if (space.lastError !== message) {
            space.lastError = message;
            this.o.log.warn("can't reach the space", {
              space: space.slug,
              code: f.code,
              status: f.status,
              retry_s: Math.round(retryInMs / 1000),
            });
          }
        },
      },
      this.o.cadence,
    );
  }

  /** Polls now: one space, or all of them. */
  wake(spaceIdOrSlug?: string): void {
    for (const [id, s] of this.spaces) {
      if (!spaceIdOrSlug || id === spaceIdOrSlug || s.slug === spaceIdOrSlug)
        s.feed.wake();
    }
  }

  /**
   * One pass without the loop: poll every space once and deliver. For
   * `memax compile` when no daemon runs, and for tests.
   */
  async syncOnce(): Promise<DaemonSnapshot> {
    this.reload();
    for (const [spaceId, space] of this.spaces) {
      try {
        const targets = await this.o.api.listTargets(spaceId);
        for (const root of space.roots) {
          const repo = this.repos.get(root);
          if (!repo) continue;
          repo.delivery.setError(undefined);
          await repo.delivery.sync(targets);
        }
      } catch (err) {
        const f = failureOf(err);
        for (const root of space.roots)
          this.repos
            .get(root)
            ?.delivery.setError(errorMessage(f.status, f.code, space.slug));
      }
    }
    return this.snapshot();
  }

  /** Checks one file now, as a watcher event would (tests). */
  async checkFile(root: string, path: string): Promise<void> {
    await this.repos.get(root)?.delivery.check(path);
  }

  snapshot(): DaemonSnapshot {
    return {
      pid: process.pid,
      started_at: this.startedAt,
      version: this.o.version,
      api_url: this.o.apiUrl,
      repos: [...this.repos.values()]
        .map((r) => r.delivery.snapshot())
        .sort((a, b) => a.root.localeCompare(b.root)),
    };
  }

  stop(): Promise<void> {
    this.stopping ??= this.shutdown();
    return this.stopping;
  }

  private async shutdown(): Promise<void> {
    this.running = false;
    this.dirWatch?.close();
    await Promise.all([...this.spaces.values()].map((s) => s.feed.stop()));
    for (const r of this.repos.values()) {
      r.watcher.stop();
      r.delivery.stop();
    }
    // Let a delivery that is writing finish its file before the state is saved.
    await Promise.all([...this.repos.values()].map((r) => r.delivery.idle()));
    this.state.flush();
    if (this.control) {
      await this.control.close();
      try {
        unlinkSync(this.o.paths.pid);
      } catch {
        // gone
      }
    }
    this.o.log.info("stopped");
  }
}

function errorMessage(status: number, code: string, slug: string): string {
  if (status === 401) return "signed out; run memax login";
  if (status === 404) return `${slug} isn't one of your spaces any more`;
  if (status === 403) return `you can't read ${slug}'s targets`;
  if (status === 0) return "can't reach Memax; trying again";
  return `Memax answered ${status} (${code}); trying again`;
}
