// A target that stopped compiling (`off`): Memax stops writing. A file it
// owns stays where it is; a person can delete it, the daemon never does.
// In a file the person owns (a user-owned CLAUDE.md), Memax takes its own
// block out, unless the block has edits, which it leaves.
import type { V2 } from "memax-sdk";
import { removeManagedBlock } from "./compiler/managed-block.js";
import { blockState, driftHash } from "./compiler/drift.js";
import { knownHashes, userOwned, type DeliverDeps } from "./deliver.js";
import { commitWrite, readDisk } from "./fs-atomic.js";
import { resolveInRepo, validRepoPath } from "./safe-path.js";
import type { LocalState } from "./snapshot.js";

export interface OffResult {
  state: LocalState;
  detail: string;
}

export async function settleOff(
  d: DeliverDeps,
  t: V2.Target,
): Promise<OffResult> {
  if (!userOwned(t) || !t.path || !validRepoPath(t.path)) {
    return { state: "off", detail: "stopped; the file stays where it is" };
  }
  let abs: string;
  try {
    abs = await resolveInRepo(d.root, t.path);
  } catch {
    return { state: "off", detail: "stopped; the file stays where it is" };
  }
  const disk = await readDisk(abs);
  if (disk.kind !== "file") return { state: "off", detail: "stopped" };
  const blocks = blockState(disk.content);
  if (blocks === "none") {
    return {
      state: "off_block_removed",
      detail: "stopped; no Memax block in the file",
    };
  }
  const hash = driftHash(disk.content);
  const ours =
    blocks === "present" &&
    (knownHashes(d, t, t.path).has(hash) ||
      (await d.known.has(t.id, t.path, hash)) === true);
  if (!ours) {
    return {
      state: "off_block_kept",
      detail: "stopped; the Memax block has edits, so it stays",
    };
  }
  const next = removeManagedBlock(disk.content);
  const r = await commitWrite(
    abs,
    next,
    { kind: "content", content: disk.content },
    { mode: disk.mode, hooks: d.hooks },
  );
  if (r === "changed") {
    return {
      state: "off_block_kept",
      detail: "stopped; the file changed, so the block stays for now",
    };
  }
  d.state.dropFile(d.root, t.id, t.path);
  d.log.info("took the Memax block out of a stopped target's file", {
    target: t.label,
    path: t.path,
  });
  return {
    state: "off_block_removed",
    detail: "stopped; Memax took its block out of the file",
  };
}
