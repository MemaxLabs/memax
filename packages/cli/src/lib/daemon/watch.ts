// Noticing hand edits. Each directory holding a compiled file is watched
// with fs.watch (a directory, not the file, so an editor that saves
// through a temp file and a rename is still seen), and a rescan stats
// every file now and then to cover what watchers miss: a directory created
// later, an exhausted inotify limit, a network file system. Events are
// debounced per file before the file is checked.
import { watch, type FSWatcher } from "node:fs";
import { lstat } from "node:fs/promises";
import { dirname, join } from "node:path";
import type { Logger } from "./log.js";

export interface WatchOptions {
  /** Quiet time after the last event before a file is checked. */
  debounceMs: number;
  /** A file that keeps changing is still checked this often. */
  maxDelayMs: number;
  rescanMs: number;
  /** false: rescans only (tests, or file systems without events). */
  events: boolean;
}

export const DEFAULT_WATCH: WatchOptions = {
  debounceMs: 400,
  maxDelayMs: 3_000,
  rescanMs: 30_000,
  events: true,
};

interface Pending {
  first: number;
  timer: NodeJS.Timeout;
}

export class RepoWatcher {
  private readonly o: WatchOptions;
  private paths = new Set<string>();
  private dirs = new Map<string, FSWatcher>();
  private seen = new Map<string, string>();
  private pending = new Map<string, Pending>();
  private rescanTimer: NodeJS.Timeout | null = null;
  private stopped = true;
  private warned = false;

  constructor(
    private readonly root: string,
    private readonly onPath: (path: string) => void,
    private readonly log: Logger,
    opts: Partial<WatchOptions> = {},
  ) {
    this.o = { ...DEFAULT_WATCH, ...opts };
  }

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    const tick = () => {
      void this.rescan().finally(() => {
        if (this.stopped) return;
        this.rescanTimer = setTimeout(tick, this.o.rescanMs);
        this.rescanTimer.unref?.();
      });
    };
    this.rescanTimer = setTimeout(tick, this.o.rescanMs);
    this.rescanTimer.unref?.();
  }

  /** The files to watch (repository-relative); new ones are checked now. */
  setPaths(paths: Iterable<string>): void {
    const next = new Set(paths);
    for (const p of next) if (!this.paths.has(p)) this.touch(p);
    for (const p of this.paths) if (!next.has(p)) this.seen.delete(p);
    this.paths = next;
    if (!this.stopped) this.syncWatchers();
  }

  /** Schedules a check of one file, debounced. */
  touch(path: string): void {
    if (this.stopped) return;
    const now = Date.now();
    const p = this.pending.get(path);
    if (p) clearTimeout(p.timer);
    const first = p?.first ?? now;
    const wait = Math.max(
      0,
      Math.min(this.o.debounceMs, first + this.o.maxDelayMs - now),
    );
    const timer = setTimeout(() => {
      this.pending.delete(path);
      this.onPath(path);
    }, wait);
    timer.unref?.();
    this.pending.set(path, { first, timer });
  }

  stop(): void {
    this.stopped = true;
    if (this.rescanTimer) clearTimeout(this.rescanTimer);
    this.rescanTimer = null;
    for (const p of this.pending.values()) clearTimeout(p.timer);
    this.pending.clear();
    for (const w of this.dirs.values()) w.close();
    this.dirs.clear();
  }

  private relDir(path: string): string {
    const d = dirname(path);
    return d === "." ? "" : d;
  }

  private syncWatchers(): void {
    if (!this.o.events) return;
    const want = new Set([...this.paths].map((p) => this.relDir(p)));
    for (const [dir, w] of this.dirs) {
      if (!want.has(dir)) {
        w.close();
        this.dirs.delete(dir);
      }
    }
    for (const dir of want) {
      if (this.dirs.has(dir)) continue;
      try {
        const w = watch(
          join(this.root, dir),
          { persistent: false },
          (_event, name) => {
            if (!name) {
              for (const p of this.paths)
                if (this.relDir(p) === dir) this.touch(p);
              return;
            }
            const rel = dir ? `${dir}/${name}` : String(name);
            if (this.paths.has(rel)) this.touch(rel);
          },
        );
        w.on("error", () => {
          w.close();
          this.dirs.delete(dir); // the next rescan tries again
        });
        this.dirs.set(dir, w);
      } catch (err) {
        const code = (err as NodeJS.ErrnoException).code;
        if (code !== "ENOENT" && !this.warned) {
          this.warned = true;
          this.log.warn(
            "can't watch for edits; checking every few seconds instead",
            { code },
          );
        }
      }
    }
  }

  /** Stats every file; a change since the last look gets a check. */
  async rescan(): Promise<void> {
    if (this.stopped) return;
    this.syncWatchers();
    for (const p of this.paths) {
      let sig = "absent";
      try {
        const st = await lstat(join(this.root, p));
        sig = `${st.ino}:${st.size}:${st.mtimeMs}:${st.isSymbolicLink() ? "l" : "f"}`;
      } catch {
        // absent
      }
      if (this.seen.get(p) !== sig) {
        this.seen.set(p, sig);
        this.touch(p);
      }
    }
  }
}
