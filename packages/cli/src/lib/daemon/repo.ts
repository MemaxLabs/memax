// One linked repository: its targets as last polled, what is on its disk,
// and the work on it, one thing at a time (a delivery and a hand-edit
// check of the same file never interleave).
import { existsSync } from "node:fs";
import type { V2 } from "memax-sdk";
import { failureOf, type DaemonApi } from "./api.js";
import {
  deliverRun,
  hasBaseline,
  judgeFile,
  userOwned,
  type DeliverDeps,
} from "./deliver.js";
import { readDisk, type FsHooks } from "./fs-atomic.js";
import { heldPaths, holdDetail } from "./holds.js";
import { KnownCompiles } from "./known.js";
import type { Logger } from "./log.js";
import { settleOff } from "./off.js";
import type { LinkedRepo } from "./registry.js";
import { HandEditReporter } from "./report.js";
import { outputRefusal, resolveInRepo } from "./safe-path.js";
import type { FileSnapshot, RepoSnapshot, TargetSnapshot } from "./snapshot.js";
import type { DeviceState } from "./state.js";

export interface RepoDeps {
  api: DaemonApi;
  state: DeviceState;
  log: Logger;
  deviceId: string;
  hooks?: FsHooks;
  /** Called when a file should be checked again later (deferred report). */
  recheck?(path: string): void;
}

const isLocal = (t: V2.Target) =>
  t.delivery === "local" && !!t.path && t.kind !== "chatgpt";

export class RepoDelivery {
  readonly link: LinkedRepo;
  private readonly d: DeliverDeps;
  private targets = new Map<string, V2.Target>();
  private snaps = new Map<string, TargetSnapshot>();
  private fileNotes = new Map<string, FileSnapshot>();
  private queue: Promise<unknown> = Promise.resolve();
  private retryAt = new Map<
    string,
    { at: number; failures: number; blocked: boolean; version: number }
  >();
  private goodOf = new Map<string, { failed: string; good: string }>();
  private polledAt?: string;
  private error?: string;

  constructor(link: LinkedRepo, deps: RepoDeps) {
    this.link = link;
    const reporter = new HandEditReporter({
      api: deps.api,
      state: deps.state,
      log: deps.log,
      deviceId: deps.deviceId,
      root: link.root,
      recheck: (p) => deps.recheck?.(p),
    });
    this.d = {
      api: deps.api,
      state: deps.state,
      log: deps.log,
      deviceId: deps.deviceId,
      root: link.root,
      known: new KnownCompiles(deps.api),
      reporter,
      hooks: deps.hooks,
    };
  }

  private serial<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.queue.then(fn, fn);
    this.queue = next.catch(() => {});
    return next;
  }

  /** Handles a fresh list of the space's targets; busy asks to poll soon. */
  sync(targets: V2.Target[]): Promise<{ busy: boolean }> {
    return this.serial(async () => {
      this.polledAt = new Date().toISOString();
      if (!existsSync(this.link.root)) {
        this.error = `the repository isn't at ${this.link.root} any more; run memax unlink there, or link it again`;
        return { busy: false };
      }
      this.error = undefined;
      this.targets = new Map(targets.map((t) => [t.id, t]));
      this.d.state.retainTargets(this.link.root, new Set(this.targets.keys()));
      for (const id of this.snaps.keys())
        if (!this.targets.has(id)) this.snaps.delete(id);
      let busy = false;
      for (const t of targets) {
        try {
          busy = (await this.syncTarget(t)) || busy;
        } catch (err) {
          const f = failureOf(err);
          this.backoff(t);
          this.note(t, {
            state: "error",
            detail: `couldn't deliver (${f.code})`,
          });
          this.d.log.warn("delivery failed; trying again soon", {
            target: t.label,
            code: f.code,
            status: f.status,
          });
        }
      }
      return { busy };
    });
  }

  private async syncTarget(t: V2.Target): Promise<boolean> {
    if (!isLocal(t)) {
      this.note(t, { state: "remote" });
      return false;
    }
    if (t.sync_state === "off") {
      this.note(t, await settleOff(this.d, t));
      return false;
    }
    const inFlight =
      t.sync_state === "compiling" || t.sync_state === "pending_delivery";
    if (t.sync_state === "drifted") {
      const n = t.open_drift;
      this.note(t, {
        state: "hand_edit",
        detail: n === 1 ? "1 local edit" : `${n} local edits`,
      });
      return false;
    }
    // A pull holds the file until its proposals are kept or rejected.
    if (heldPaths(t).length > 0) {
      this.note(t, { state: "held", detail: holdDetail(t) });
      return false;
    }
    const last = t.last_compile;
    if (!last) {
      this.note(t, { state: "waiting", detail: "nothing compiled yet" });
      return inFlight;
    }
    // The run to have on disk: the latest, or the latest good one when the
    // latest failed (known once a preview has said which).
    let want: string | undefined = last.ref;
    if (last.status === "failed") {
      const g = this.goodOf.get(t.id);
      want = g && g.failed === last.ref ? g.good : undefined;
    }
    const here = this.d.state.peek(this.link.root, t.id)?.compile;
    // Nothing to write, unless the server says a delivery is due anyway
    // (a hold lifted by rejects alone: the same run, over the edit).
    if (
      want &&
      here === want &&
      t.delivered?.compile === want &&
      t.sync_state !== "pending_delivery"
    ) {
      this.note(t, this.noteFromFiles(t, { state: "in_sync" }));
      return false;
    }
    // Backing off after a failure: wait it out. A file the daemon won't
    // write (a symlink, say) waits for a person, so it never asks the feed
    // to poll fast; it is tried again on the next full poll after the wait,
    // or at once when the target changes (a person resolved something).
    const retry = this.retryAt.get(t.id);
    if (retry && retry.version !== t.version) this.retryAt.delete(t.id);
    else if (retry && Date.now() < retry.at) return inFlight && !retry.blocked;

    const p = await this.d.api.preview(t.id);
    if (!p.compile) {
      this.note(t, { state: "waiting", detail: "nothing compiled yet" });
      return inFlight;
    }
    if (last.status === "failed")
      this.goodOf.set(t.id, { failed: last.ref, good: p.compile.ref });
    const res = await deliverRun(this.d, t, p);
    // Held for a person: a file it won't write, or a hand edit (the server
    // turns the target drifted; if it didn't agree, don't loop on it).
    const blocked = res.state === "blocked" || res.state === "hand_edit";
    if (res.retry || blocked) this.backoff(t, blocked);
    else this.retryAt.delete(t.id);
    this.note(t, res);
    return !blocked && (inFlight || res.state === "pending");
  }

  private backoff(t: V2.Target, blocked = false): void {
    const prev = this.retryAt.get(t.id);
    const failures = (prev?.version === t.version ? prev.failures : 0) + 1;
    const ms = Math.min(5 * 60_000, 2_000 * 2 ** (failures - 1));
    this.retryAt.set(t.id, {
      at: Date.now() + ms,
      failures,
      blocked,
      version: t.version,
    });
  }

  /** Checks one file after a watcher event or a rescan. */
  check(path: string): Promise<void> {
    return this.serial(async () => {
      const t = [...this.targets.values()].find(
        (x) => isLocal(x) && !outputRefusal(x, path),
      );
      if (!t || t.sync_state === "off" || !t.last_compile) return;
      const out = t.last_compile.files.find((f) => f.path === path);
      let abs: string;
      try {
        abs = await resolveInRepo(this.link.root, path);
      } catch {
        return;
      }
      const disk = await readDisk(abs);
      if (disk.kind === "absent") {
        if (hasBaseline(this.d, t, path))
          this.fileNotes.set(path, {
            path,
            state: "missing",
            detail: "the next compile writes it again",
          });
        return;
      }
      // What should be there: the latest run's file, or (after a failed
      // compile) what was delivered.
      const latest =
        out?.drift_sha256 ??
        t.delivered?.files.find((f) => f.path === path)?.sha256 ??
        "";
      const j = await judgeFile(this.d, t, path, disk, latest);
      if (!j) return; // couldn't ask the runs; the next rescan tries again
      if (j.kind === "hand_edit" && disk.kind === "file") {
        this.fileNotes.set(path, { path, state: "hand_edit" });
        await this.d.reporter.report(
          t,
          path,
          disk.content,
          j.hash,
          userOwned(t),
        );
        return;
      }
      const notes: Partial<Record<typeof j.kind, FileSnapshot>> = {
        current: { path, state: "in_sync" },
        ours: {
          path,
          state: "older",
          detail: "an earlier compile; the next one updates it",
        },
        fresh: { path, state: "pending" },
        skip: {
          path,
          state: "blocked",
          detail: j.kind === "skip" ? j.reason : undefined,
        },
      };
      const note = notes[j.kind];
      if (note) this.fileNotes.set(path, note);
    });
  }

  /** Every file the watcher should look at. */
  trackedPaths(): string[] {
    const out = new Set<string>();
    for (const t of this.targets.values()) {
      if (!isLocal(t) || t.sync_state === "off") continue;
      for (const f of t.last_compile?.files ?? []) if (f.path) out.add(f.path);
      for (const f of t.delivered?.files ?? []) out.add(f.path);
      for (const p of Object.keys(
        this.d.state.peek(this.link.root, t.id)?.files ?? {},
      ))
        out.add(p);
    }
    return [...out].filter((p) =>
      [...this.targets.values()].some((t) => !outputRefusal(t, p)),
    );
  }

  private note(
    t: V2.Target,
    s: Pick<TargetSnapshot, "state" | "detail"> & { files?: FileSnapshot[] },
  ): void {
    const local = this.d.state.peek(this.link.root, t.id);
    this.snaps.set(t.id, {
      id: t.id,
      kind: t.kind,
      label: t.label,
      path: t.path,
      delivery: t.delivery,
      sync_state: t.sync_state,
      open_drift: t.open_drift,
      compile: t.last_compile?.ref,
      here: local?.compile
        ? { compile: local.compile, at: local.acked_at }
        : undefined,
      state: s.state,
      detail: s.detail,
      files: s.files ?? [],
    });
    for (const f of s.files ?? []) this.fileNotes.set(f.path, f);
  }

  /** An in-sync target, unless a file check found otherwise. */
  private noteFromFiles(
    t: V2.Target,
    s: Pick<TargetSnapshot, "state" | "detail">,
  ) {
    const files = (t.last_compile?.files ?? [])
      .filter((f) => f.path)
      .map(
        (f) =>
          this.fileNotes.get(f.path!) ?? {
            path: f.path!,
            state: "in_sync" as const,
          },
      );
    const odd = files.find((f) => f.state !== "in_sync");
    return odd
      ? { state: odd.state, detail: odd.detail, files }
      : { ...s, files };
  }

  snapshot(): RepoSnapshot {
    return {
      root: this.link.root,
      space_id: this.link.space_id,
      space_slug: this.link.space_slug,
      polled_at: this.polledAt,
      error: this.error,
      targets: [...this.snaps.values()].sort((a, b) =>
        a.label.localeCompare(b.label),
      ),
    };
  }

  setError(message: string | undefined): void {
    this.error = message;
  }

  /** Resolves once the work already queued is done. */
  idle(): Promise<void> {
    return this.queue.then(() => {});
  }

  stop(): void {
    this.d.reporter.stop();
  }
}
