// What an agent's last session in a repository was told
// (~/.memax/daemon/seen/<repo>-<agent>.json): the compile of each file it
// loaded with a hash per cited line, and the forgets, gates and pending
// compiles the hook already mentioned. Hashes and refs only, never words.
// One file per repository and agent, so two agents starting at once never
// overwrite each other's.
import {
  closeSync,
  mkdirSync,
  openSync,
  readFileSync,
  renameSync,
  statSync,
  unlinkSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { shortHash, uniqueName } from "./hash.js";

export interface SeenFile {
  compile: string;
  /** Cite key → line hash. */
  lines: Record<string, string>;
}

export interface Seen {
  version: 1;
  at: string;
  files: Record<string, SeenFile>;
  /** Tombstones already told. */
  forgotten: string[];
  /** Waiting gates already told. */
  gates: string[];
  /** Compiles already told as not on disk yet. */
  behind: string[];
}

const KEEP = 200;

export function emptySeen(): Seen {
  return {
    version: 1,
    at: "",
    files: {},
    forgotten: [],
    gates: [],
    behind: [],
  };
}

const tag = shortHash;

export function seenPath(dir: string, root: string, agent: string): string {
  return join(dir, `${tag(root)}-${agent.replace(/[^a-z0-9-]/gi, "_")}.json`);
}

export function readSeen(path: string): Seen | null {
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as Partial<Seen>;
    if (raw.version !== 1 || !raw.files) return null;
    return {
      ...emptySeen(),
      ...raw,
      forgotten: raw.forgotten ?? [],
      gates: raw.gates ?? [],
      behind: raw.behind ?? [],
    } as Seen;
  } catch {
    return null;
  }
}

export function writeSeen(path: string, dir: string, seen: Seen): void {
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const trimmed: Seen = {
    ...seen,
    forgotten: seen.forgotten.slice(0, KEEP),
    gates: seen.gates.slice(0, KEEP),
    behind: seen.behind.slice(0, 20),
  };
  const tmp = `${path}.${uniqueName()}.tmp`;
  try {
    writeFileSync(tmp, JSON.stringify(trimmed), { mode: 0o600 });
    renameSync(tmp, path);
  } catch (err) {
    try {
      unlinkSync(tmp);
    } catch {
      // never created
    }
    throw err;
  }
}

/**
 * Claims one session start: false when the same session and event already
 * ran within `windowMs` (the plugin's hook and a settings hook, say, or
 * Cursor running Claude Code's hooks), so only one of them prints.
 */
export function claimSession(
  dir: string,
  session: string,
  source: string,
  now = Date.now(),
  windowMs = 30_000,
): boolean {
  const sessions = join(dir, "sessions");
  mkdirSync(sessions, { recursive: true, mode: 0o700 });
  const path = join(sessions, tag(`${session}\u0000${source}`));
  try {
    closeSync(openSync(path, "wx", 0o600));
    return true;
  } catch {
    try {
      if (now - statSync(path).mtimeMs < windowMs) return false;
      const t = new Date(now);
      utimesSync(path, t, t); // an old one: take it again
      return true;
    } catch {
      return true;
    }
  }
}
