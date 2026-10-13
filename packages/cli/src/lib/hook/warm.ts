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
import type { V2 } from "memax-sdk";

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
  /**
   * Switched back to V1: its files are V1's again, so the hook says
   * nothing about them. Absent while the space is on V2.
   */
  on_v1?: true;
  /**
   * Its memories marked stale now, the latest flag first, and why (a
   * date, or a condition and what changed on the default branch): what
   * the hook says is being verified. Absent until a check read them.
   */
  stale?: WarmStale[];
}

/** A memory marked stale, as the conditions list says it: no words. */
export interface WarmStale {
  ref: string;
  /** When it was marked stale. */
  since: string;
  causes: V2.StaleCause[];
}

/**
 * What the daemon's last check found at a linked repository's HEAD off
 * the pushed default branch: conditions that hold on the default branch
 * and fail here. This machine's alone, never sent anywhere.
 */
export interface WarmBranch {
  branch: string;
  unpushed?: boolean;
  commit: string;
  checked_at: string;
  items: Array<{
    memory: string;
    condition: V2.Condition;
    evidence?: V2.CheckEvidence;
  }>;
}

/**
 * This machine's home link (memax link --home): the Personal space's
 * files on this machine, with paths relative to the home folder, and that
 * space's forgets and waiting gates. Apart from `spaces`, which holds
 * what linked repositories compile.
 */
export interface WarmHome extends WarmSpace {
  space_id: string;
  machine_id: string;
}

export interface WarmFile {
  version: typeof WARM_VERSION;
  spaces: Record<string, WarmSpace>;
  /** Absent when this machine's home isn't linked. */
  home?: WarmHome;
  /** Linked repositories' branch hints, by root; only off the pushed default. */
  branches?: Record<string, WarmBranch>;
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
