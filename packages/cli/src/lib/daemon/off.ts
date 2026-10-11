// A target that stopped compiling (`off`): Memax stops writing.
//
// - In a file the person owns (a user-owned CLAUDE.md, Codex's AGENTS.md
//   at home), Memax takes its own block out, unless the block has edits,
//   which it leaves. Removing the block gives the file back byte for byte
//   (removeManagedBlock).
// - A file Memax owns whole stays where it is in a repository (it is
//   committed, and a person deletes it there). At home (a machine
//   unlinked, a file stopped there, a machine signed out) Memax removes
//   it while it holds exactly what Memax wrote, and leaves it when it has
//   hand edits.
//
// A signed-out machine can't ask the server about a hash it doesn't
// know, so it decides from what this device wrote and the last targets it
// read (`ask: false`): anything else stays.
import type { V2 } from "memax-sdk";
import { removeManagedBlock } from "./compiler/managed-block.js";
import { blockState, driftHash } from "./compiler/drift.js";
import { knownHashes, userOwned, type DeliverDeps } from "./deliver.js";
import { commitWrite, readDisk, removeIfUnchanged } from "./fs-atomic.js";
import { fileHash } from "./plan.js";
import type { LocalState } from "./snapshot.js";

export interface OffResult {
  state: LocalState;
  detail: string;
}

/** What settling a stopped target reads of it. */
export type OffTarget = Pick<
  V2.Target,
  "id" | "kind" | "path" | "root" | "label"
> &
  Partial<Pick<V2.Target, "delivered" | "last_compile">> & {
    settings: Pick<V2.TargetSettings, "user_owned">;
  };

const STAYS: OffResult = {
  state: "off",
  detail: "stopped; the file stays where it is",
};

export async function settleOff(
  d: DeliverDeps,
  t: OffTarget,
  opts: { ask?: boolean } = {},
): Promise<OffResult> {
  const ask = opts.ask ?? true;
  if (!t.path || d.place.refusal(t, t.path)) return STAYS;
  if (userOwned(t)) return takeBlockOut(d, t, t.path, ask);
  if (d.place.removesOwnedWhenOff) return takeFileOut(d, t, t.path, ask);
  return STAYS;
}

/** Whether `hash` at `path` is something Memax wrote for `t`. */
async function ours(
  d: DeliverDeps,
  t: OffTarget,
  path: string,
  hash: string,
  ask: boolean,
): Promise<boolean> {
  if (knownHashes(d, t, path).has(hash)) return true;
  return ask && (await d.known.has(t.id, path, hash)) === true;
}

async function takeBlockOut(
  d: DeliverDeps,
  t: OffTarget,
  path: string,
  ask: boolean,
): Promise<OffResult> {
  let abs: string;
  try {
    abs = await d.place.resolve(path);
  } catch {
    return STAYS;
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
  if (
    blocks !== "present" ||
    !(await ours(d, t, path, driftHash(disk.content), ask))
  ) {
    return {
      state: "off_block_kept",
      detail: "stopped; the Memax block has edits, so it stays",
    };
  }
  const next = removeManagedBlock(disk.content);
  // A file Memax made for its block, with nothing of the person's in it:
  // as it was before, it isn't there.
  if (next === "" && d.state.peek(d.root, t.id)?.files[path]?.created) {
    if ((await removeIfUnchanged(abs, disk.content)) === "changed") {
      return {
        state: "off_block_kept",
        detail: "stopped; the file changed, so the block stays for now",
      };
    }
    d.state.tookOut(d.root, t.id, path);
    d.log.info("removed the file Memax made for its block", {
      target: t.label,
      path,
    });
    return {
      state: "off_block_removed",
      detail: "stopped; Memax took out the file it made",
    };
  }
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
  d.state.tookOut(d.root, t.id, path);
  d.log.info("took the Memax block out of a stopped target's file", {
    target: t.label,
    path,
  });
  return {
    state: "off_block_removed",
    detail: "stopped; Memax took its block out of the file",
  };
}

async function takeFileOut(
  d: DeliverDeps,
  t: OffTarget,
  path: string,
  ask: boolean,
): Promise<OffResult> {
  let abs: string;
  try {
    abs = await d.place.resolve(path);
  } catch {
    return STAYS;
  }
  const disk = await readDisk(abs);
  if (disk.kind === "absent") {
    return { state: "off_file_removed", detail: "stopped; the file is gone" };
  }
  if (
    disk.kind !== "file" ||
    !(await ours(d, t, path, fileHash(disk.content, false), ask))
  ) {
    return {
      state: "off_file_kept",
      detail: "stopped; the file has hand edits, so it stays",
    };
  }
  if ((await removeIfUnchanged(abs, disk.content)) === "changed") {
    return {
      state: "off_file_kept",
      detail: "stopped; the file changed, so it stays for now",
    };
  }
  d.state.tookOut(d.root, t.id, path);
  d.log.info("removed a stopped target's file", { target: t.label, path });
  return {
    state: "off_file_removed",
    detail: "stopped; Memax took its file out",
  };
}
