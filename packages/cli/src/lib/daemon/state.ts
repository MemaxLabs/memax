// What this device wrote (~/.memax/daemon/state.json): per repository and
// target, the run it last acknowledged, and per file the drift hashes it
// wrote and the hand edit it last reported. Hashes and refs only, never
// content, so the file can't leak memory text.
import { readJson, writeJsonAtomic } from "./json-file.js";
import { ensureDaemonDir, type DaemonPaths } from "./paths.js";

export interface FileLocal {
  /** The drift hash this device last wrote to the path. */
  sha?: string;
  /** Earlier hashes it wrote there, newest first. */
  history: string[];
  /** The hash of the hand edit last reported for the path. */
  reported?: string;
  /** The target's version after that report: it moves when a person resolves it. */
  reported_version?: number;
  written_at?: string;
  /**
   * When Memax took what it wrote back out (its block, or its own file at
   * home): a stopped target, an unlinked or signed-out machine. The next
   * delivery starts afresh rather than reading the file as a hand edit.
   */
  taken_out_at?: string;
  /**
   * Memax created the file (it wasn't there before its first write), so a
   * file of the person's that is empty once Memax's block is out is
   * Memax's to remove.
   */
  created?: boolean;
}

export interface TargetLocal {
  /** The run (C-) this device last wrote and acknowledged. */
  compile?: string;
  acked_at?: string;
  /** What `memax daemon status` shows while no daemon runs. */
  kind?: string;
  label?: string;
  files: Record<string, FileLocal>;
}

export interface RepoLocal {
  targets: Record<string, TargetLocal>;
}

interface StateFile {
  version: 1;
  repos: Record<string, RepoLocal>;
}

const HISTORY = 16;

export class DeviceState {
  private data: StateFile;
  private timer: NodeJS.Timeout | null = null;

  constructor(
    private readonly paths: DaemonPaths,
    private readonly saveDelayMs = 200,
  ) {
    const raw = readJson<Partial<StateFile>>(paths.state, {});
    this.data = {
      version: 1,
      repos:
        raw.version === 1 && raw.repos && typeof raw.repos === "object"
          ? raw.repos
          : {},
    };
  }

  target(root: string, targetId: string): TargetLocal {
    const repo = (this.data.repos[root] ??= { targets: {} });
    const t = (repo.targets[targetId] ??= { files: {} });
    t.files ??= {};
    return t;
  }

  peek(root: string, targetId: string): TargetLocal | undefined {
    return this.data.repos[root]?.targets[targetId];
  }

  file(root: string, targetId: string, path: string): FileLocal {
    const t = this.target(root, targetId);
    const f = (t.files[path] ??= { history: [] });
    f.history ??= [];
    return f;
  }

  /** Records that this device wrote `sha` to `path` (`created`: where nothing was). */
  wrote(
    root: string,
    targetId: string,
    path: string,
    sha: string,
    at = new Date(),
    created = false,
  ): void {
    const f = this.file(root, targetId, path);
    if (created) f.created = true;
    if (f.sha && f.sha !== sha) {
      f.history = [
        f.sha,
        ...f.history.filter((h) => h !== f.sha && h !== sha),
      ].slice(0, HISTORY);
    }
    f.sha = sha;
    f.written_at = at.toISOString();
    f.reported = undefined;
    f.reported_version = undefined;
    f.taken_out_at = undefined;
    this.changed();
  }

  /**
   * Records that Memax took what it wrote at `path` back out. The hashes it
   * wrote are kept: a file put back from a backup is still known as ours.
   */
  tookOut(root: string, targetId: string, path: string, at = new Date()): void {
    const f = this.file(root, targetId, path);
    f.taken_out_at = at.toISOString();
    f.reported = undefined;
    f.reported_version = undefined;
    this.changed();
  }

  /** Forgets which run is on disk here, so the next pass delivers again. */
  unacked(root: string, targetId: string): void {
    const t = this.peek(root, targetId);
    if (t?.compile) {
      t.compile = undefined;
      t.acked_at = undefined;
      this.changed();
    }
  }

  /** Every hash this device wrote to `path`. */
  written(root: string, targetId: string, path: string): string[] {
    const f = this.peek(root, targetId)?.files[path];
    if (!f) return [];
    return f.sha ? [f.sha, ...f.history] : [...f.history];
  }

  acked(
    root: string,
    targetId: string,
    compile: string,
    meta: { kind: string; label: string },
    at = new Date(),
  ): void {
    const t = this.target(root, targetId);
    t.compile = compile;
    t.acked_at = at.toISOString();
    t.kind = meta.kind;
    t.label = meta.label;
    this.changed();
  }

  /** Every target this device keeps state for in a repository. */
  targetsOf(root: string): Record<string, TargetLocal> {
    return { ...(this.data.repos[root]?.targets ?? {}) };
  }

  reported(
    root: string,
    targetId: string,
    path: string,
    sha: string,
    meta: { version: number },
  ): void {
    const f = this.file(root, targetId, path);
    f.reported = sha;
    f.reported_version = meta.version;
    this.changed();
  }

  dropFile(root: string, targetId: string, path: string): void {
    const t = this.peek(root, targetId);
    if (t && t.files[path]) {
      delete t.files[path];
      this.changed();
    }
  }

  /** Keeps only these targets of a repository. */
  retainTargets(root: string, ids: Set<string>): void {
    const repo = this.data.repos[root];
    if (!repo) return;
    for (const id of Object.keys(repo.targets)) {
      if (!ids.has(id)) {
        delete repo.targets[id];
        this.changed();
      }
    }
  }

  forgetRepo(root: string): void {
    if (this.data.repos[root]) {
      delete this.data.repos[root];
      this.changed();
    }
  }

  repos(): string[] {
    return Object.keys(this.data.repos);
  }

  private changed(): void {
    if (this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.flush();
    }, this.saveDelayMs);
    this.timer.unref?.();
  }

  flush(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    ensureDaemonDir(this.paths);
    writeJsonAtomic(this.paths.state, this.data);
  }
}
