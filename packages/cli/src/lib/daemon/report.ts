// Reporting a hand edit (POST /v2/targets/{target}/observations): the file
// as it is on disk, once per distinct content, at most once every few
// seconds per file so an editor's autosave doesn't flood Review. The server
// keeps the content and marks the target drifted; a person pulls it back
// as proposals, overwrites it or stops compiling it (DriftResolve).
import type { V2 } from "memax-sdk";
import { commandKey, failureOf, type DaemonApi } from "./api.js";
import type { Logger } from "./log.js";
import type { DeviceState } from "./state.js";

/** The server takes up to 1 MiB of observed content. */
const MAX_REPORT_CHARS = 1_048_576;

export type ReportOutcome =
  | "reported"
  | "already"
  | "deferred"
  | "unreportable"
  | "failed";

export interface ReporterDeps {
  api: DaemonApi;
  state: DeviceState;
  log: Logger;
  deviceId: string;
  root: string;
  /** Called when a deferred report is due: check the file again. */
  recheck(path: string): void;
  minGapMs?: number;
  now?: () => number;
}

export class HandEditReporter {
  private last = new Map<string, number>();
  private pending = new Map<string, NodeJS.Timeout>();
  private readonly gap: number;
  private readonly now: () => number;

  constructor(private readonly d: ReporterDeps) {
    this.gap = d.minGapMs ?? 10_000;
    this.now = d.now ?? Date.now;
  }

  async report(
    t: V2.Target,
    path: string,
    content: string,
    hash: string,
  ): Promise<ReportOutcome> {
    const { state, root } = this.d;
    if (state.peek(root, t.id)?.files[path]?.reported === hash)
      return "already";
    const since = this.now() - (this.last.get(path) ?? -Infinity);
    if (since < this.gap) {
      this.defer(path, this.gap - since);
      return "deferred";
    }
    if (content.length > MAX_REPORT_CHARS || content.includes("\u0000")) {
      // Not text Memax compiles; say so once, and stop asking.
      state.reported(root, t.id, path, hash);
      this.d.log.warn(
        "hand edit not reported: the file isn't text Memax can read back",
        {
          target: t.label,
          path,
        },
      );
      return "unreportable";
    }
    this.last.set(path, this.now());
    const key = commandKey("obs", this.d.deviceId, t.id, t.version, path, hash);
    try {
      const res = await this.d.api.observe(
        t.id,
        { path, content, device_id: this.d.deviceId },
        key,
      );
      state.reported(root, t.id, path, hash);
      this.d.log.info(
        res.drifted
          ? "reported a hand edit"
          : "file matches what Memax delivered",
        {
          target: t.label,
          path,
          sha: hash.slice(0, 12),
          changes: res.observation?.changeset.changes.length,
        },
      );
      return "reported";
    } catch (err) {
      const f = failureOf(err);
      if (f.status === 400 || f.status === 422) {
        state.reported(root, t.id, path, hash); // the same content would fail again
      } else {
        this.defer(path, Math.max(this.gap, f.retryAfterMs ?? 0));
      }
      this.d.log.warn("couldn't report a hand edit", {
        target: t.label,
        path,
        code: f.code,
        status: f.status,
      });
      return "failed";
    }
  }

  private defer(path: string, ms: number): void {
    if (this.pending.has(path)) return;
    const timer = setTimeout(() => {
      this.pending.delete(path);
      this.d.recheck(path);
    }, ms);
    timer.unref?.();
    this.pending.set(path, timer);
  }

  stop(): void {
    for (const t of this.pending.values()) clearTimeout(t);
    this.pending.clear();
  }
}
