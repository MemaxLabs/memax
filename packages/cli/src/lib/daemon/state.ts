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
  /** The hash the server recorded for that report (its own drift hash). */
  observed?: string;
  written_at?: string;
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

  /** Records that this device wrote `sha` to `path`. */
  wrote(
    root: string,
    targetId: string,
    path: string,
    sha: string,
    at = new Date(),
  ): void {
    const f = this.file(root, targetId, path);
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
    f.observed = undefined;
    this.changed();
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
    meta: { version: number; observed?: string },
  ): void {
    const f = this.file(root, targetId, path);
    f.reported = sha;
    f.reported_version = meta.version;
    f.observed = meta.observed;
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
