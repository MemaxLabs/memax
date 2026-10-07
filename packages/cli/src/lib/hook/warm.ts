// The warm-start cache (~/.memax/daemon/warm.json): what the session-start
// hook knows about each linked space without asking the network. The
// daemon writes it (lib/daemon/warm.ts) after every poll that saw the
// space change; `memax hook flush` refreshes it when no daemon runs.
//
// Refs, states and times only, never memory text: the words the hook
// prints come from the compiled files on disk, which Forget rewrites. The
// one exception is a waiting gate's question (an agent's words, not a
// memory), shortened, and dropped as soon as the gate ends.
import { readFileSync } from "node:fs";

export const WARM_VERSION = 1;

export interface WarmTarget {
  kind: string;
  /** Repository-relative, e.g. `AGENTS.md`. */
  path: string;
  label: string;
  sync_state: string;
  /** The latest good compile (C-), when there is one. */
  compile?: string;
  compiled_at?: string;
  /** The memories that compile holds; lines citing anything else aren't the server's. */
  refs?: string[];
  /** Files with a hand edit waiting in Review. */
  open_drift: number;
}

export interface WarmForget {
  /** The tombstone. */
  id: string;
  /** The forgotten memory (M-…), or `space`. */
  ref: string;
  /** Forgotten with it in the same Forget. */
  with: string[];
  at: string;
}

export interface WarmGate {
  ref: string;
  /** The asking agent's question, cleaned and shortened. */
  question: string;
  agent?: string;
  asked_at: string;
  expires_at: string;
}

export interface WarmSpace {
  slug: string;
  updated_at: string;
  targets: WarmTarget[];
  /** Newest first, the last 30 days. */
  forgotten: WarmForget[];
  /** Waiting for a person, newest first. */
  gates: WarmGate[];
}

export interface WarmFile {
  version: typeof WARM_VERSION;
  spaces: Record<string, WarmSpace>;
}

export function emptyWarm(): WarmFile {
  return { version: WARM_VERSION, spaces: {} };
}

/** The cache, or an empty one when it is missing, unreadable or from another version. */
export function readWarm(path: string): WarmFile {
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as Partial<WarmFile>;
    if (
      raw.version === WARM_VERSION &&
      raw.spaces &&
      typeof raw.spaces === "object"
    )
      return raw as WarmFile;
  } catch {
    // missing or half-written by an older CLI: nothing cached
  }
  return emptyWarm();
}
