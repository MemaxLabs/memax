// The daemon: one feed per linked space, one RepoDelivery and watcher per
// linked repository, and, when this machine's home is linked, one more of
// each for its home files (a feed of this machine's targets alone); the
// control socket, the pid file and the state file.
import { watch, type FSWatcher } from "node:fs";
import { readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { basename } from "node:path";
import { failureOf, type DaemonApi, type TargetPoll } from "./api.js";
import { CheckReporter, RepoChecker, type CheckTiming } from "./checks.js";
import { LoadReporter } from "./loads.js";
import {
  ControlServer,
  type ControlRequest,
  type HomeOffReason,
} from "./control.js";
import { PollingFeed, type Cadence, type TargetFeed } from "./feed.js";
import type { FsHooks } from "./fs-atomic.js";
import {
  homeLinkStamp,
  readHomeLink,
  removeHomeLink,
  type HomeFile,
  type HomeLink,
} from "./home-link.js";
import { homeBlockFile, homePlace } from "./home-place.js";
import type { Logger } from "./log.js";
import type { OffTarget } from "./off.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";
import { readRegistry, registryStamp, type LinkedRepo } from "./registry.js";
import { RepoDelivery } from "./repo.js";
import type { DaemonSnapshot, HomeSnapshot } from "./snapshot.js";
import { DeviceState } from "./state.js";
import { RepoWatcher, type WatchOptions } from "./watch.js";
import { forgottenRefs, HOME, WarmCache } from "./warm.js";

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
  /** How often the warm cache may fetch a space's forgets and gates (tests). */
  warmRefreshMs?: number;
  /** The checks' cadence (checks.ts; tests). */
  checks?: Partial<CheckTiming>;
  /** Called after a stop request, once everything is closed. */
  onStopped?(): void;
}

interface Repo {
  link: LinkedRepo;
  delivery: RepoDelivery;
  watcher: RepoWatcher;
  /** Its conditions against the committed tree, apart from its delivery. */
  checker: RepoChecker;
}

interface Space {
  slug: string;
  roots: Set<string>;
  feed: TargetFeed;
  lastError?: string;
  /** Switched back to V1, as the last poll said. */
  onV1?: boolean;
}

/** This machine's home link: its own delivery, watcher and feed. */
interface Home {
  link: HomeLink;
  delivery: RepoDelivery;
  watcher: RepoWatcher;
  feed: TargetFeed;
  lastError?: string;
  /** Switched back to V1, as the last poll said. */
  onV1?: boolean;
  /** The layer is off because this machine was signed out. */
  signedOut: boolean;
  /** The layer is off because the Personal space answered 404. */
  gone: boolean;
}

/**
 * A 404 from the API itself (`not_found`): the Personal space or this
 * machine isn't there for this session (forgotten, deleted, another
 * person's). Never a proxy's 404 or any other failure.
 */
function definitelyGone(err: unknown): boolean {
  const f = failureOf(err);
  return f.status === 404 && f.code === "not_found";
}

const OFF_LOG: Record<HomeOffReason, string> = {
  unlinked: "took the Personal layer off this machine: it was unlinked",
  signed_out: "took the Personal layer off this machine: it was signed out",
  other_person:
    "took the Personal layer off this machine: someone else signed in",
  gone: "took the Personal layer off this machine: the Personal space isn't there any more",
};

/** A home file as home.json has it, for taking it off without a poll. */
function offTarget(f: HomeFile): OffTarget {
  return {
    id: f.target_id,
    kind: f.kind,
    path: f.path,
    root: "home",
    label: `~/${f.path}`,
    settings: { user_owned: homeBlockFile(f.kind, f.path) },
  };
}

export class Daemon {
  readonly state: DeviceState;
  /** The session-start hook's cache: compiles, forgets and gates per space. */
  readonly warm: WarmCache;
  private readonly loads: LoadReporter;
  private readonly reports: CheckReporter;
  private repos = new Map<string, Repo>();
  private spaces = new Map<string, Space>();
  private home: Home | null = null;
  private stamp = "";
  private homeStamp = "";
  private dirWatch: FSWatcher | null = null;
  private control: ControlServer | null = null;
  private running = false;
  private stopping: Promise<void> | null = null;
  private readonly startedAt = new Date().toISOString();

  constructor(private readonly o: DaemonOptions) {
    ensureDaemonDir(o.paths);
    this.state = new DeviceState(o.paths);
    this.warm = new WarmCache({
      paths: o.paths,
      api: o.api,
      log: o.log,
      minRefreshMs: o.warmRefreshMs,
      onForgotten: (spaceId, refs) => this.forgotten(spaceId, refs),
    });
    this.loads = new LoadReporter({ paths: o.paths, api: o.api, log: o.log });
    this.reports = new CheckReporter({
      paths: o.paths,
      api: o.api,
      log: o.log,
    });
  }

  /** Takes the lock and starts polling, watching and serving. */
  async start(): Promise<void> {
    this.control = await ControlServer.listen(this.o.paths, (req) =>
      this.handle(req),
    );
    writeFileSync(this.o.paths.pid, `${process.pid}\n`, { mode: 0o600 });
    this.running = true;
    this.reload();
    // What an earlier pass (syncOnce) added before the daemon started.
    for (const s of this.spaces.values()) s.feed.start();
    for (const r of this.repos.values()) {
      r.watcher.start();
      r.checker.start();
    }
    this.home?.feed.start();
    this.home?.watcher.start();
    // Forgets the cache heard of before (a run that stopped halfway): the
    // checks hold nothing of them.
    for (const [id, s] of Object.entries(this.warm.snapshot().spaces))
      if (s.forgotten.length) this.forgotten(id, forgottenRefs(s.forgotten));
    this.loads.start();
    this.reports.start();
    try {
      this.dirWatch = watch(
        this.o.paths.dir,
        { persistent: false },
        (_e, name) => {
          const file = name ? String(name) : "";
          if (
            !file ||
            file === basename(this.o.paths.repos) ||
            file === basename(this.o.paths.home)
          )
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
      home: this.home !== null,
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
      case "home_off":
        return this.homeOff(req.reason);
      case "stop":
        setImmediate(() => void this.stop().then(() => this.o.onStopped?.()));
        return { pid: process.pid };
    }
  }

  /** Re-reads the registry and adds or drops repositories and spaces. */
  reload(): void {
    this.reloadHome();
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
    this.warm.retain(new Set(this.spaces.keys()));
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
    const checker = new RepoChecker({
      link,
      api: this.o.api,
      paths: this.o.paths,
      log: this.o.log,
      deviceId: this.o.deviceId,
      reporter: this.reports,
      timing: this.o.checks,
    });
    this.repos.set(link.root, { link, delivery, watcher, checker });
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
    // A second repository of a space already on V1 isn't checked either.
    checker.setOnV1(space.onV1 ?? false);
    if (this.running) {
      watcher.start();
      checker.start();
    }
    space.feed.wake();
    this.o.log.info("watching a repository", {
      root: link.root,
      space: link.space_slug,
    });
  }

  /** Re-reads home.json: adds, replaces or drops the home link. */
  private reloadHome(): void {
    const stamp = homeLinkStamp(this.o.paths);
    if (stamp === this.homeStamp) return;
    this.homeStamp = stamp;
    const link = readHomeLink(this.o.paths);
    if (this.home && JSON.stringify(this.home.link) !== JSON.stringify(link))
      this.dropHome();
    if (link && !this.home) this.addHome(link);
    if (!link) this.warm.clearHome();
  }

  private addHome(link: HomeLink): void {
    let watcher: RepoWatcher | null = null;
    const delivery = new RepoDelivery(
      {
        root: link.home,
        space_id: link.space_id,
        space_slug: link.space_slug,
        created_at: link.created_at,
      },
      {
        api: this.o.api,
        state: this.state,
        log: this.o.log,
        deviceId: this.o.deviceId,
        hooks: this.o.hooks,
        recheck: (p) => watcher?.touch(p),
        place: homePlace(link),
      },
    );
    watcher = new RepoWatcher(
      link.home,
      (p) => void delivery.check(p),
      this.o.log,
      this.o.watch,
    );
    const home: Home = {
      link,
      delivery,
      watcher,
      signedOut: false,
      gone: false,
      feed: new PollingFeed(
        {
          fetch: (signal) =>
            this.o.api.machineTargets(link.space_id, link.machine_id, signal),
          probe: (signal) => this.o.api.changeToken(link.space_id, signal),
          onTargets: async (poll, info) => {
            this.reload();
            if (this.home !== home) return { busy: false };
            if (home.lastError || home.signedOut || home.gone) {
              this.o.log.info("reached the Personal space again", {
                space: link.space_slug,
              });
            }
            home.lastError = undefined;
            home.signedOut = false;
            home.gone = false;
            delivery.setError(undefined);
            // The hook's Personal section reads these, and the space's
            // forgets and gates (fetched when something changed).
            this.warm.setHome(link, poll);
            if (info.changed) this.warm.requestRefresh(HOME, link.space_slug);
            this.switched(home, poll, link.space_slug);
            const r = await delivery.sync(poll.targets, poll.onV2);
            watcher!.setPaths(delivery.trackedPaths());
            return r;
          },
          onError: (err, retryInMs) => {
            if (this.home !== home) return;
            if (definitelyGone(err)) this.homeGone();
            const f = failureOf(err);
            const message = errorMessage(f.status, f.code, link.space_slug);
            delivery.setError(message);
            if (home.lastError !== message) {
              home.lastError = message;
              this.o.log.warn("can't reach the Personal space", {
                space: link.space_slug,
                code: f.code,
                status: f.status,
                retry_s: Math.round(retryInMs / 1000),
              });
            }
          },
        },
        this.o.cadence,
      ),
    };
    this.home = home;
    if (this.running) {
      home.feed.start();
      watcher.start();
    }
    home.feed.wake();
    this.o.log.info("writing this machine's home files", {
      space: link.space_slug,
      files: link.files.length,
    });
  }

  private dropHome(): void {
    const home = this.home;
    if (!home) return;
    this.home = null;
    home.watcher.stop();
    home.delivery.stop();
    void home.feed.stop();
    this.o.log.info("stopped writing this machine's home files", {
      space: home.link.space_slug,
    });
  }

  /**
   * Takes the Personal layer off this machine now (personal-layer design
   * §6.6), from what this device wrote and nothing else: no request, so it
   * works signed out. Files and blocks with hand edits stay. `unlinked`
   * and `other_person` also remove home.json; `signed_out` and `gone` keep
   * it, so the next poll that succeeds writes everything again.
   */
  async homeOff(reason: HomeOffReason): Promise<HomeSnapshot | null> {
    this.reloadHome();
    const home = this.home;
    if (!home) return null;
    if (reason === "signed_out") home.signedOut = true;
    if (reason === "gone") home.gone = true;
    const results = await home.delivery.takeOff(home.link.files.map(offTarget));
    this.o.log.info(OFF_LOG[reason], {
      removed: results.filter((r) => r.state.endsWith("_removed")).length,
      kept: results.filter((r) => r.state.endsWith("_kept")).length,
    });
    const snap = this.homeSnapshot(home);
    if (reason === "unlinked" || reason === "other_person") {
      removeHomeLink(this.o.paths);
      this.warm.clearHome();
      this.homeStamp = homeLinkStamp(this.o.paths);
      this.dropHome();
    }
    this.state.flush();
    return snap;
  }

  /**
   * The server refused this machine's refresh token (lib/client.ts
   * onSignedOut): take the layer off, once, until a poll succeeds again.
   * Transient failures never get here.
   */
  signedOut(): void {
    if (!this.home || this.home.signedOut) return;
    this.home.signedOut = true;
    void this.homeOff("signed_out").catch((err: Error) =>
      this.o.log.error("couldn't take the Personal layer off", {
        error: err.message,
      }),
    );
  }

  /**
   * The Personal space answered a definitive 404: nothing is left to
   * deliver, and its words mustn't stay on this machine. Once, until a poll
   * succeeds again.
   */
  private homeGone(): void {
    if (!this.home || this.home.gone) return;
    this.home.gone = true;
    void this.homeOff("gone").catch((err: Error) =>
      this.o.log.error("couldn't take the Personal layer off", {
        error: err.message,
      }),
    );
  }

  private homeSnapshot(home: Home): HomeSnapshot {
    return {
      ...home.delivery.snapshot(),
      machine_id: home.link.machine_id,
      ...(home.signedOut ? { signed_out: true } : {}),
      ...(home.gone ? { gone: true } : {}),
    };
  }

  private dropRepo(root: string, forget: boolean): void {
    const repo = this.repos.get(root);
    if (!repo) return;
    repo.watcher.stop();
    repo.delivery.stop();
    // Unlinked: its check state goes too, where no later Forget could reach it.
    void repo.checker.stop().then(() => {
      if (forget) repo.checker.discard();
    });
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
        onTargets: async (poll, info) => {
          this.reload();
          const space = this.spaces.get(spaceId);
          if (!space) return { busy: false };
          this.warm.setTargets(spaceId, space.slug, poll);
          if (info.changed) {
            this.warm.requestRefresh(spaceId, space.slug);
            // Its conditions may have changed: every change to them
            // writes a receipt, which moves the space's change token.
            for (const root of space.roots)
              this.repos.get(root)?.checker.conditionsChanged();
          }
          if (space.lastError) {
            this.o.log.info("reached the space again", { space: space.slug });
            space.lastError = undefined;
          }
          this.switched(space, poll, space.slug);
          let busy = false;
          for (const root of space.roots) {
            const repo = this.repos.get(root);
            if (!repo) continue;
            repo.delivery.setError(undefined);
            repo.checker.setOnV1(!poll.onV2);
            const r = await repo.delivery.sync(poll.targets, poll.onV2);
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

  /** Says once that a space is on V1 again, and once that it is back. */
  private switched(
    s: { onV1?: boolean },
    poll: TargetPoll,
    slug: string,
  ): void {
    const was = s.onV1 ?? false;
    s.onV1 = !poll.onV2;
    if (s.onV1 && !was)
      this.o.log.info("the space is on V1 again: leaving its files to V1", {
        space: slug,
      });
    else if (!s.onV1 && was)
      this.o.log.info("the space is on V2 again: writing its files", {
        space: slug,
      });
  }

  /** Polls now: one space, or all of them. */
  wake(spaceIdOrSlug?: string): void {
    for (const [id, s] of this.spaces) {
      if (!spaceIdOrSlug || id === spaceIdOrSlug || s.slug === spaceIdOrSlug)
        s.feed.wake();
    }
    const home = this.home?.link;
    if (
      home &&
      (!spaceIdOrSlug ||
        home.space_id === spaceIdOrSlug ||
        home.space_slug === spaceIdOrSlug)
    )
      this.home?.feed.wake();
  }

  /**
   * One pass without the loop: poll every space (or only one) and deliver. For
   * `memax compile` when no daemon runs, and for tests.
   */
  async syncOnce(only?: string): Promise<DaemonSnapshot> {
    this.reload();
    for (const [spaceId, space] of this.spaces) {
      if (only && spaceId !== only) continue;
      try {
        const poll = await this.o.api.listTargets(spaceId);
        this.warm.setTargets(spaceId, space.slug, poll);
        await this.warm.refresh(spaceId, space.slug);
        this.switched(space, poll, space.slug);
        for (const root of space.roots) {
          const repo = this.repos.get(root);
          if (!repo) continue;
          repo.delivery.setError(undefined);
          repo.checker.setOnV1(!poll.onV2);
          await repo.delivery.sync(poll.targets, poll.onV2);
        }
      } catch (err) {
        const f = failureOf(err);
        for (const root of space.roots)
          this.repos
            .get(root)
            ?.delivery.setError(errorMessage(f.status, f.code, space.slug));
      }
    }
    const home = this.home;
    if (home && (!only || only === home.link.space_id)) {
      try {
        const poll = await this.o.api.machineTargets(
          home.link.space_id,
          home.link.machine_id,
        );
        home.signedOut = false;
        home.gone = false;
        home.delivery.setError(undefined);
        this.warm.setHome(home.link, poll);
        await this.warm.refresh(HOME, home.link.space_slug);
        this.switched(home, poll, home.link.space_slug);
        await home.delivery.sync(poll.targets, poll.onV2);
      } catch (err) {
        if (definitelyGone(err) && !home.gone) {
          home.gone = true;
          await this.homeOff("gone");
        }
        const f = failureOf(err);
        home.delivery.setError(
          errorMessage(f.status, f.code, home.link.space_slug),
        );
      }
    }
    this.warm.flush();
    return this.snapshot();
  }

  /**
   * Memories forgotten in a space: its repositories' checks drop their
   * conditions and results, and its waiting reports their results, now
   * rather than at the next run (rule 7: they hold the memory's paths).
   */
  private forgotten(spaceId: string, refs: string[]): void {
    const space = this.spaces.get(spaceId);
    for (const root of space?.roots ?? [])
      this.repos.get(root)?.checker.forget(refs);
    this.reports.forget(spaceId, refs);
  }

  /** Runs a repository's checks now (tests, and `memax status`'s view of them). */
  checker(root: string): RepoChecker | undefined {
    return this.repos.get(root)?.checker;
  }

  /** Checks one file now, as a watcher event would (tests). */
  async checkFile(root: string, path: string): Promise<void> {
    const home = this.home?.link.home === root ? this.home : undefined;
    await (home ?? this.repos.get(root))?.delivery.check(path);
  }

  snapshot(): DaemonSnapshot {
    return {
      pid: process.pid,
      started_at: this.startedAt,
      version: this.o.version,
      api_url: this.o.apiUrl,
      memory: memoryMiB(),
      repos: [...this.repos.values()]
        .map((r) => r.delivery.snapshot())
        .sort((a, b) => a.root.localeCompare(b.root)),
      ...(this.home ? { home: this.homeSnapshot(this.home) } : {}),
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
    await this.home?.feed.stop();
    await this.loads.stop();
    await Promise.all([...this.repos.values()].map((r) => r.checker.stop()));
    await this.reports.stop();
    await this.warm.stop();
    for (const r of [
      ...this.repos.values(),
      ...(this.home ? [this.home] : []),
    ]) {
      r.watcher.stop();
      r.delivery.stop();
    }
    // Let a delivery that is writing finish its file before the state is saved.
    await Promise.all([...this.repos.values()].map((r) => r.delivery.idle()));
    await this.home?.delivery.idle();
    this.state.flush();
    if (this.control) {
      // The pid file first, then the socket: once nothing answers, no pid
      // file names this process.
      try {
        // Only our own: a daemon that took over writes its pid here.
        if (
          readFileSync(this.o.paths.pid, "utf8").trim() === String(process.pid)
        ) {
          unlinkSync(this.o.paths.pid);
        }
      } catch {
        // gone
      }
      await this.control.close();
    }
    this.o.log.info("stopped");
  }
}

function memoryMiB(): { rss: number; heap_used: number } {
  const m = process.memoryUsage();
  const mib = (n: number) => Math.round((n / 1048576) * 10) / 10;
  return { rss: mib(m.rss), heap_used: mib(m.heapUsed) };
}

function errorMessage(status: number, code: string, slug: string): string {
  if (status === 401) return "signed out; run memax login";
  if (status === 404) return `${slug} isn't one of your spaces any more`;
  if (status === 403) return `you can't read ${slug}'s targets`;
  if (status === 0) return "can't reach Memax; trying again";
  return `Memax answered ${status} (${code}); trying again`;
}
