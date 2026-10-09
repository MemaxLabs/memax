// What the daemon reports about each linked repository (`memax daemon
// status`, over the control socket). No content, only states and refs.
import type { V2 } from "memax-sdk";

/** Where a target's files stand on this device. */
export type LocalState =
  /** On disk as the latest run wrote them. */
  | "in_sync"
  /** A run this device hasn't written yet. */
  | "pending"
  /** A person edited a file; Memax won't write over it. */
  | "hand_edit"
  /** A pulled hand edit's proposals wait in Review; the file stays as it is. */
  | "held"
  /** Delivered, but a file isn't on disk; the next compile writes it again. */
  | "missing"
  /** An earlier compile is on disk (a pull or a branch switch). */
  | "older"
  /** The daemon won't write it: a symlink, an unsafe path, a broken block. */
  | "blocked"
  /** Stopped; the file stays where it is. */
  | "off"
  /** Stopped; Memax took its block out of the person's file. */
  | "off_block_removed"
  /** Stopped; the block had edits, so Memax left it. */
  | "off_block_kept"
  /** Not written on this device (MCP, copy-out, pull request). */
  | "remote"
  /** Nothing compiled yet. */
  | "waiting"
  | "error";

export interface FileSnapshot {
  path: string;
  state: LocalState;
  detail?: string;
}

export interface TargetSnapshot {
  id: string;
  kind: V2.TargetKind;
  label: string;
  path?: string;
  delivery: V2.Delivery;
  sync_state: V2.SyncState;
  open_drift: number;
  /** The latest run. */
  compile?: string;
  /** What this device last wrote and acknowledged. */
  here?: { compile?: string; at?: string };
  state: LocalState;
  detail?: string;
  files: FileSnapshot[];
}

export interface RepoSnapshot {
  root: string;
  space_id: string;
  space_slug: string;
  polled_at?: string;
  error?: string;
  targets: TargetSnapshot[];
}

export interface DaemonSnapshot {
  pid: number;
  started_at: string;
  version: string;
  api_url: string;
  repos: RepoSnapshot[];
  /** The process's memory in MiB, for `memax daemon status --format json`. */
  memory?: { rss: number; heap_used: number };
  /** Set while `memax compile` delivers in place of a daemon. */
  oneshot?: boolean;
}
