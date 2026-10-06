// What to do with one compiled file, given what is on disk (rule 6:
// Memax never writes over a hand edit).
//
// The daemon writes a file only when it is absent, already holds what this
// run writes, or holds something Memax wrote: the baseline the server
// believes is on disk, a hash this device wrote, or (checked by the
// caller against the target's runs) the output of an earlier run, as after
// a `git pull` or a branch switch. Anything else is a person's edit.
//
// A file the person owns (a user-owned CLAUDE.md) is judged by its managed
// block alone, and only the block is replaced: every byte outside the
// markers stays as it is. Any other file is judged whole. A file that
// isn't UTF-8 text is never touched: it couldn't be written back intact.
import { upsertManagedBlock } from "./compiler/managed-block.js";
import {
  blockState,
  driftHash,
  managedInner,
  wholeHash,
} from "./compiler/drift.js";
import type { Disk, Expect } from "./fs-atomic.js";

/** What a file on disk is, relative to what Memax wrote there. */
export type Judgement =
  | { kind: "absent" }
  /** Holds this run's output (line endings aside). */
  | { kind: "current" }
  /** Holds something Memax wrote: safe to write over. */
  | { kind: "ours"; hash: string }
  /** The person's own file before Memax added its block: safe to add it. */
  | { kind: "fresh" }
  /** Not a hash Memax knows offhand: ask the target's runs. */
  | { kind: "unknown"; hash: string }
  | { kind: "hand_edit"; hash: string }
  /** The daemon won't touch this file at all. */
  | { kind: "skip"; reason: string };

export interface JudgeInput {
  /** The drift hash this run writes to the path. */
  latest: string;
  userOwned: boolean;
  disk: Disk;
  /** Drift hashes Memax is known to have written to this path. */
  known: Set<string>;
  /** Whether Memax ever had a version of this path (server or device). */
  hasBaseline: boolean;
}

export function judge(i: JudgeInput): Judgement {
  const d = i.disk;
  switch (d.kind) {
    case "absent":
      return { kind: "absent" };
    case "symlink":
      return {
        kind: "skip",
        reason: "is a symlink; Memax doesn't write through links",
      };
    case "other":
      return { kind: "skip", reason: "isn't a regular file" };
    case "too_large":
      return {
        kind: "skip",
        reason: "is over 2 MiB, too large to be a compiled file",
      };
    case "not_text":
      return {
        kind: "skip",
        reason: "isn't UTF-8 text; Memax leaves it as it is",
      };
  }
  if (i.userOwned) {
    const blocks = blockState(d.content);
    if (blocks === "broken")
      return { kind: "hand_edit", hash: driftHash(d.content) };
    if (blocks === "none" && !i.hasBaseline) return { kind: "fresh" };
  }
  // A file the person owns is judged by its block; any other by all of it.
  const h = fileHash(d.content, i.userOwned);
  if (h === i.latest) return { kind: "current" };
  return i.known.has(h)
    ? { kind: "ours", hash: h }
    : { kind: "unknown", hash: h };
}

/**
 * The hash a file is judged by: its managed block's when the person owns
 * it, otherwise the whole file's (line endings and a BOM aside), so text
 * around a stray block is never mistaken for Memax's.
 */
export function fileHash(content: string, userOwned: boolean): string {
  return userOwned ? driftHash(content) : wholeHash(content);
}

/** After the runs were asked about an unknown hash. */
export function settleUnknown(j: Judgement, knownCompile: boolean): Judgement {
  if (j.kind !== "unknown") return j;
  return knownCompile
    ? { kind: "ours", hash: j.hash }
    : { kind: "hand_edit", hash: j.hash };
}

export type Plan =
  | { kind: "write"; content: string; expect: Expect; mode?: number }
  | { kind: "current" }
  | { kind: "hand_edit"; hash: string }
  | { kind: "skip"; reason: string };

/** The write a judgement allows, for this run's file `out`. */
export function planFrom(
  j: Exclude<Judgement, { kind: "unknown" }>,
  out: { content: string },
  userOwned: boolean,
  disk: Disk,
): Plan {
  switch (j.kind) {
    case "current":
    case "hand_edit":
    case "skip":
      return j;
  }
  const current = disk.kind === "file" ? disk.content : "";
  const expect: Expect =
    disk.kind === "file"
      ? { kind: "content", content: disk.content }
      : { kind: "absent" };
  const mode = disk.kind === "file" ? disk.mode : undefined;
  if (!userOwned) return { kind: "write", content: out.content, expect, mode };
  const inner = managedInner(out.content);
  if (inner === null) {
    return {
      kind: "skip",
      reason: "the compiled file has no managed block to apply",
    };
  }
  return {
    kind: "write",
    content: upsertManagedBlock(current, inner),
    expect,
    mode,
  };
}
