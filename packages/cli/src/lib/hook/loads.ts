// The compile-load queue (~/.memax/daemon/loads/): the session-start hook
// writes one small file per compile its agent loaded, after it has
// printed, and whoever holds the CLI's credentials reports them with
// `POST /v2/spaces/{space}/compile-loads`: the running daemon, which
// watches this directory, or a detached `memax hook flush` when none runs.
//
// A queue rather than a request from the hook: the hook never touches the
// network, so a session starts as fast offline as online; the daemon
// already holds a signed-in client and a warm connection; and a load
// queued while offline is still reported later, within the server's day
// (LoadWindow). Each load's Idempotency-Key comes from its fields
// (lib/daemon/loads.ts loadKey), so a load two flushers both send is
// recorded once.
import {
  mkdirSync,
  readdirSync,
  readFileSync,
  renameSync,
  statSync,
  unlinkSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { uniqueName } from "./hash.js";

export interface QueuedLoad {
  /** The space's id, or its slug when the repository isn't linked here. */
  space: string;
  compile: string;
  agent: string;
  session_ref?: string;
  loaded_at: string;
}

/** The server refuses loads older than a day; leave a margin for the clock. */
export const LOAD_MAX_AGE_MS = 23 * 60 * 60_000;
const CLAIM_STALE_MS = 5 * 60_000;

/** Queues loads: one file each, written whole before it appears. */
export function enqueueLoads(dir: string, loads: QueuedLoad[]): void {
  if (loads.length === 0) return;
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  for (const l of loads) {
    const name = uniqueName();
    const tmp = join(dir, `.${name}.tmp`);
    writeFileSync(tmp, JSON.stringify(l), { mode: 0o600 });
    renameSync(tmp, join(dir, `${name}.json`));
  }
}

/** Whether anything waits to be reported. */
export function hasQueuedLoads(dir: string): boolean {
  try {
    return readdirSync(dir).some((n) => n.endsWith(".json"));
  } catch {
    return false;
  }
}

export type ReportOutcome =
  /** Recorded (or already recorded under its key). */
  | "recorded"
  /** The server won't record it (not connected, unknown compile): drop it. */
  | "refused"
  /** Offline, rate limited or a server error: try again later. */
  | "retry";

export interface FlushResult {
  recorded: number;
  refused: number;
  expired: number;
  retry: number;
  /** The spaces of the loads seen, for a warm refresh. */
  spaces: Set<string>;
}

function isLoad(v: unknown): v is QueuedLoad {
  const l = v as QueuedLoad;
  return (
    typeof l === "object" &&
    l !== null &&
    typeof l.space === "string" &&
    typeof l.compile === "string" &&
    typeof l.agent === "string" &&
    typeof l.loaded_at === "string" &&
    (l.session_ref === undefined || typeof l.session_ref === "string")
  );
}

/**
 * Reports every queued load once: claims each file by renaming it (so two
 * flushers never send the same one at once), reports it, then deletes it,
 * or puts it back to try again. Stale claims of a flusher that died are
 * taken back after five minutes.
 */
export async function flushLoads(
  dir: string,
  report: (l: QueuedLoad) => Promise<ReportOutcome>,
  now: () => number = Date.now,
): Promise<FlushResult> {
  const res: FlushResult = {
    recorded: 0,
    refused: 0,
    expired: 0,
    retry: 0,
    spaces: new Set(),
  };
  let names: string[];
  try {
    names = readdirSync(dir);
  } catch {
    return res;
  }
  for (const name of names) {
    // Claims left by a flusher that died go back in the queue.
    if (name.endsWith(".claimed")) {
      const p = join(dir, name);
      try {
        if (now() - statSync(p).mtimeMs > CLAIM_STALE_MS)
          renameSync(p, p.slice(0, -".claimed".length));
      } catch {
        // someone else took it back
      }
    }
  }
  names = readdirSync(dir)
    .filter((n) => n.endsWith(".json"))
    .sort();
  for (const name of names) {
    const path = join(dir, name);
    const claimed = `${path}.claimed`;
    try {
      renameSync(path, claimed);
      // A rename keeps the old time; the claim is fresh from now.
      const t = new Date(now());
      utimesSync(claimed, t, t);
    } catch {
      continue; // another flusher has it
    }
    let load: QueuedLoad | null = null;
    try {
      const raw: unknown = JSON.parse(readFileSync(claimed, "utf8"));
      load = isLoad(raw) ? raw : null;
    } catch {
      load = null;
    }
    if (!load) {
      unlinkSync(claimed);
      res.refused++;
      continue;
    }
    if (now() - Date.parse(load.loaded_at) > LOAD_MAX_AGE_MS) {
      unlinkSync(claimed);
      res.expired++;
      continue;
    }
    res.spaces.add(load.space);
    let outcome: ReportOutcome;
    try {
      outcome = await report(load);
    } catch {
      outcome = "retry";
    }
    if (outcome === "retry") {
      renameSync(claimed, path);
      res.retry++;
    } else {
      unlinkSync(claimed);
      res[outcome]++;
    }
  }
  return res;
}

/** Drops session markers older than a day (lib/hook/seen.ts claimSession). */
export function pruneSessions(
  seenDir: string,
  now: () => number = Date.now,
): void {
  const dir = join(seenDir, "sessions");
  let names: string[];
  try {
    names = readdirSync(dir);
  } catch {
    return;
  }
  for (const n of names) {
    const p = join(dir, n);
    try {
      if (now() - statSync(p).mtimeMs > 24 * 60 * 60_000) unlinkSync(p);
    } catch {
      // gone already
    }
  }
}
