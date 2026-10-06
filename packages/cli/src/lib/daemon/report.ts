// Reporting a hand edit (POST /v2/targets/{target}/observations): the file
// as it is on disk, once per distinct content until a person resolves it,
// at most once every few seconds per file so an editor's autosave doesn't
// flood Review. The server keeps the content and marks the target drifted;
// a person pulls it back as proposals, overwrites it or stops compiling it
// (DriftResolve).
//
// For a file the person owns (a user-owned CLAUDE.md, or CLAUDE.local.md),
// only Memax's block is sent: the rest is theirs, and it would land in a
// space other people may read. With no block left (or broken markers),
// the report is empty, which the server reads as the block removed.
import type { V2 } from "memax-sdk";
import { commandKey, failureOf, type DaemonApi } from "./api.js";
import { extractManagedBlock } from "./compiler/managed-block.js";
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

/** What is sent for a file the person owns: Memax's block, or nothing. */
export function managedRegion(content: string): string {
  try {
    return extractManagedBlock(content) ?? "";
  } catch {
    return "";
  }
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
    userOwned: boolean,
  ): Promise<ReportOutcome> {
    const { state, root } = this.d;
    const f = state.peek(root, t.id)?.files[path];
    // Reported already, and not resolved since: the target is still
    // drifted, or still at the version the report left it at.
    if (
      f?.reported === hash &&
      (t.sync_state === "drifted" || f.reported_version === t.version)
    ) {
      return "already";
    }
    const since = this.now() - (this.last.get(path) ?? -Infinity);
    if (since < this.gap) {
      this.defer(path, this.gap - since);
      return "deferred";
    }
    const body = userOwned ? managedRegion(content) : content;
    if (body.length > MAX_REPORT_CHARS) {
      // Far beyond any compiled file; say so once per version, and stop.
      state.reported(root, t.id, path, hash, { version: t.version });
      this.d.log.warn("hand edit not reported: the file is too large", {
        target: t.label,
        path,
      });
      return "unreportable";
    }
    this.last.set(path, this.now());
    const key = commandKey("obs", this.d.deviceId, t.id, t.version, path, hash);
    try {
      const res = await this.d.api.observe(
        t.id,
        { path, content: body, device_id: this.d.deviceId },
        key,
      );
      state.reported(root, t.id, path, hash, {
        version: res.target.version,
        observed: res.observation?.observed_sha256,
      });
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
        // The same content would fail again: not until the target changes.
        state.reported(root, t.id, path, hash, { version: t.version });
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
