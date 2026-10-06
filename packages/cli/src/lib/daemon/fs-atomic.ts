// Reading and replacing compiled files without ever losing a hand edit.
//
// A write goes to a temp file in the same directory and is renamed over
// the target, so a reader sees the old file or the new one, never half.
// Right before the rename the daemon reads the file again: if it isn't
// exactly what the write was planned against (a person saved in between),
// the write is dropped and the edit stays. A file that was absent is
// created with link(2), which fails rather than replace one that appeared
// in the meantime. What remains is the microseconds between that last
// read and the rename itself; nothing in userspace can close it.
import { randomBytes } from "node:crypto";
import { constants } from "node:fs";
import { link, lstat, open, rename, unlink } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

export type Disk =
  | { kind: "absent" }
  | {
      kind: "file";
      content: string;
      mode: number;
      size: number;
      mtimeMs: number;
      ino: number;
    }
  | { kind: "symlink" }
  | { kind: "other" }
  | { kind: "too_large"; size: number };

/** Compiled files are tens of KiB; anything near this isn't one. */
export const MAX_FILE_BYTES = 2 * 1024 * 1024;

const NOFOLLOW = constants.O_NOFOLLOW ?? 0;
const NONBLOCK = constants.O_NONBLOCK ?? 0;

/** Reads a file without following a symlink or blocking on a FIFO. */
export async function readDisk(abs: string): Promise<Disk> {
  let st;
  try {
    st = await lstat(abs);
  } catch (err) {
    if (isCode(err, "ENOENT", "ENOTDIR")) return { kind: "absent" };
    throw err;
  }
  if (st.isSymbolicLink()) return { kind: "symlink" };
  if (!st.isFile()) return { kind: "other" };
  if (st.size > MAX_FILE_BYTES) return { kind: "too_large", size: st.size };
  let fh;
  try {
    fh = await open(abs, constants.O_RDONLY | NOFOLLOW | NONBLOCK);
  } catch (err) {
    if (isCode(err, "ENOENT")) return { kind: "absent" };
    if (isCode(err, "ELOOP")) return { kind: "symlink" };
    throw err;
  }
  try {
    const fst = await fh.stat();
    if (!fst.isFile()) return { kind: "other" };
    if (fst.size > MAX_FILE_BYTES) return { kind: "too_large", size: fst.size };
    const content = (await fh.readFile()).toString("utf8");
    return {
      kind: "file",
      content,
      mode: fst.mode & 0o7777,
      size: fst.size,
      mtimeMs: fst.mtimeMs,
      ino: fst.ino,
    };
  } finally {
    await fh.close();
  }
}

/** What the file must still be, right before the rename. */
export type Expect = { kind: "absent" } | { kind: "content"; content: string };

export interface FsHooks {
  /** Called between writing the temp file and the last check (tests). */
  beforeCommit?(abs: string): void | Promise<void>;
}

/**
 * Writes `content` to `abs` if the file is still as `expect` says.
 * `mode` is kept from the file it replaces. Returns "changed" (and writes
 * nothing) when the file changed since it was read.
 */
export async function commitWrite(
  abs: string,
  content: string,
  expect: Expect,
  opts: { mode?: number; hooks?: FsHooks } = {},
): Promise<"written" | "changed"> {
  const tmp = join(
    dirname(abs),
    `.${basename(abs)}.memax-${randomBytes(4).toString("hex")}.tmp`,
  );
  const fh = await open(tmp, "wx", 0o666);
  try {
    await fh.writeFile(content, "utf8");
    if (opts.mode !== undefined) await fh.chmod(opts.mode);
    await fh.sync();
  } catch (err) {
    await fh.close();
    await unlink(tmp).catch(() => {});
    throw err;
  }
  await fh.close();
  try {
    await opts.hooks?.beforeCommit?.(abs);
    if (!(await stillAsExpected(abs, expect))) return "changed";
    if (expect.kind === "absent") {
      if ((await createExclusive(tmp, abs)) === "exists") return "changed";
    } else {
      await rename(tmp, abs);
    }
  } finally {
    await unlink(tmp).catch(() => {}); // gone after a rename
  }
  await syncDir(dirname(abs));
  return "written";
}

/**
 * Removes `abs` if it still holds exactly `content`. Returns "changed"
 * when it holds anything else, and "absent" when it is already gone.
 */
export async function removeIfUnchanged(
  abs: string,
  content: string,
): Promise<"removed" | "changed" | "absent"> {
  const cur = await readDisk(abs);
  if (cur.kind === "absent") return "absent";
  if (cur.kind !== "file" || cur.content !== content) return "changed";
  await unlink(abs);
  return "removed";
}

async function stillAsExpected(abs: string, expect: Expect): Promise<boolean> {
  const cur = await readDisk(abs);
  if (expect.kind === "absent") return cur.kind === "absent";
  return cur.kind === "file" && cur.content === expect.content;
}

/** Puts tmp at abs only if abs doesn't exist, atomically where it can. */
async function createExclusive(
  tmp: string,
  abs: string,
): Promise<"created" | "exists"> {
  try {
    await link(tmp, abs);
    return "created";
  } catch (err) {
    if (isCode(err, "EEXIST")) return "exists";
    if (
      !isCode(
        err,
        "EPERM",
        "ENOTSUP",
        "EOPNOTSUPP",
        "EXDEV",
        "ENOSYS",
        "EMLINK",
      )
    )
      throw err;
  }
  // No hard links on this file system: check, then rename.
  if ((await readDisk(abs)).kind !== "absent") return "exists";
  await rename(tmp, abs);
  return "created";
}

async function syncDir(dir: string): Promise<void> {
  try {
    const fh = await open(dir, constants.O_RDONLY);
    try {
      await fh.sync();
    } finally {
      await fh.close();
    }
  } catch {
    // Not every platform syncs a directory; the rename is still atomic.
  }
}

export function isCode(err: unknown, ...codes: string[]): boolean {
  const code = (err as NodeJS.ErrnoException | undefined)?.code;
  return code !== undefined && codes.includes(code);
}
