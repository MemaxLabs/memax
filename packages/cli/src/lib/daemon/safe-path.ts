// Where the daemon may write. A compiled file's path comes from the
// server, and the repository's own tree may hold symlinks, so both are
// checked before anything touches the disk:
//
//   - the path is a RepoPath (relative, ASCII, no `.` or `..` segments);
//   - it is a file the target's kind writes (AGENTS.md, CLAUDE.md,
//     `.cursor/rules/memax-<area>.mdc`, …), so a bad server can't write a
//     git hook, `.envrc` or `package.json`;
//   - it never enters `.git` or replaces `.memax.yml`;
//   - every directory on the way resolves inside the repository, so a
//     symlinked `.cursor` can't lead out of it.
import { lstat, mkdir, realpath } from "node:fs/promises";
import { join, sep } from "node:path";
import type { V2 } from "memax-sdk";

const REPO_PATH = /^[A-Za-z0-9._/-]+$/;
const MAX_PATH = 300;

/** The same rule as the server's ledger.ValidPath, plus the repo's own files. */
export function validRepoPath(p: string): boolean {
  if (p === "" || p.length > MAX_PATH || !REPO_PATH.test(p)) return false;
  const segments = p.split("/");
  for (const s of segments) {
    if (s === "" || s === "." || s === "..") return false;
    const lower = s.toLowerCase(); // case-insensitive file systems too
    if (lower === ".git" || lower === ".memax.yml") return false;
  }
  return true;
}

const basename = (p: string) => p.slice(p.lastIndexOf("/") + 1);

const SHIM_FILES: Partial<Record<V2.TargetKind, string[]>> = {
  claude_md: ["CLAUDE.md", "CLAUDE.local.md"],
  gemini_md: ["GEMINI.md"],
};

const SCOPED_EXT: Partial<Record<V2.TargetKind, string>> = {
  cursor_mdc: ".mdc",
  copilot: ".instructions.md",
  windsurf: ".md",
  claude_rules: ".md",
};

const SCOPED_NAME = /^memax-[a-z0-9][a-z0-9-]{0,80}$/;

/**
 * Why `file` isn't something `target` may write, or null when it is.
 */
export function outputRefusal(
  target: Pick<V2.Target, "kind" | "path">,
  file: string,
): string | null {
  if (!target.path || !validRepoPath(target.path)) {
    return "the target has no valid path";
  }
  if (!validRepoPath(file)) return "isn't a path inside the repository";
  const kind = target.kind;
  if (kind === "agents_md") {
    return file === target.path && basename(file) === "AGENTS.md"
      ? null
      : "isn't the target's AGENTS.md";
  }
  const shim = SHIM_FILES[kind];
  if (shim) {
    return file === target.path && shim.includes(basename(file))
      ? null
      : `isn't the target's ${shim.join(" or ")}`;
  }
  const ext = SCOPED_EXT[kind];
  if (ext) {
    const prefix = target.path + "/";
    const name = file.startsWith(prefix) ? file.slice(prefix.length) : "";
    if (
      name.includes("/") ||
      !name.endsWith(ext) ||
      !SCOPED_NAME.test(name.slice(0, -ext.length))
    ) {
      return `isn't a memax-<area>${ext} file in ${target.path}`;
    }
    return null;
  }
  return "the target writes no files";
}

/** Thrown when a path would leave the repository. */
export class UnsafePathError extends Error {
  constructor(
    readonly path: string,
    reason: string,
  ) {
    super(`${path}: ${reason}`);
    this.name = "UnsafePathError";
  }
}

function inside(root: string, real: string): boolean {
  return real === root || real.startsWith(root + sep);
}

/**
 * The absolute path of `rel` under the repository, after checking that
 * each directory that exists on the way resolves inside `rootReal` (a
 * real path). The file itself isn't resolved: it is read and replaced
 * without following a symlink.
 */
export async function resolveInRepo(
  rootReal: string,
  rel: string,
): Promise<string> {
  if (!validRepoPath(rel)) {
    throw new UnsafePathError(rel, "isn't a path inside the repository");
  }
  const segments = rel.split("/");
  let dir = rootReal;
  for (const s of segments.slice(0, -1)) {
    dir = join(dir, s);
    let st;
    try {
      st = await lstat(dir);
    } catch {
      break; // the rest doesn't exist yet; ensureParents creates it
    }
    if (st.isSymbolicLink()) {
      const real = await realpath(dir).catch(() => "");
      if (!real || !inside(rootReal, real)) {
        throw new UnsafePathError(
          rel,
          "a directory on the way leads outside the repository",
        );
      }
      continue;
    }
    if (!st.isDirectory()) {
      throw new UnsafePathError(rel, "a file is in the way of its directory");
    }
  }
  return join(rootReal, ...segments);
}

/**
 * Creates the missing directories of `rel`, one at a time, refusing any
 * that turns out to be a symlink out of the repository.
 */
export async function ensureParents(
  rootReal: string,
  rel: string,
): Promise<void> {
  const segments = rel.split("/").slice(0, -1);
  let dir = rootReal;
  for (const s of segments) {
    dir = join(dir, s);
    try {
      await mkdir(dir, { mode: 0o755 });
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
    }
    const real = await realpath(dir);
    if (!inside(rootReal, real)) {
      throw new UnsafePathError(
        rel,
        "a directory on the way leads outside the repository",
      );
    }
  }
}
