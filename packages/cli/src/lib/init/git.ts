// Which repository lines are the repository's (plan 25 §5.16, GitInject):
// instruction files that arrive on a pull request's branch, or a fork's,
// are someone else's words until they reach the default branch. So when
// init runs on another branch than the default one (a feature branch, a
// checked-out pull request, a detached HEAD), a statement whose lines
// aren't on the default branch arrives external: quarantined, reviewed on
// the web, never kept in bulk. On the default branch, or in a repository
// with nothing to compare against, every line is the repository's.
// Uncommitted edits on the default branch are the person's own work in
// progress and count as the repository's.
import { execFileSync } from "node:child_process";
import { cleanLine } from "../daemon/compiler/sanitize.js";

export type Git = (args: string[], cwd: string) => string | null;

/** Runs git, or returns null when it fails. */
export const runGit: Git = (args, cwd) => {
  try {
    return execFileSync("git", args, {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
      timeout: 5_000,
    });
  } catch {
    return null;
  }
};

export interface BranchContext {
  /** The checked-out branch, or "HEAD" when detached. */
  branch: string | null;
  /** The default branch to compare with ("origin/main", "main"), if any. */
  base: string | null;
  /** On the default branch: every line is the repository's. */
  onBase: boolean;
  /** The commit HEAD is at, for sources. */
  commit: string | null;
}

export function branchContext(root: string, git: Git = runGit): BranchContext {
  const branch =
    git(["rev-parse", "--abbrev-ref", "HEAD"], root)?.trim() || null;
  const commit = git(["rev-parse", "HEAD"], root)?.trim() || null;
  let base =
    git(
      ["symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"],
      root,
    )?.trim() || null;
  if (!base) {
    for (const b of ["main", "master", "origin/main", "origin/master"]) {
      if (git(["rev-parse", "--verify", "--quiet", b], root)) {
        base = b;
        break;
      }
    }
  }
  const short = base?.replace(/^origin\//, "") ?? null;
  return { branch, base, onBase: !base || branch === short, commit };
}

/** A line as the branch check compares it: clean, without list or quote markers. */
function norm(line: string): string {
  return cleanLine(line)
    .text.replace(/^(?:>\s?)+/, "")
    .replace(/^(?:[-*+]|\d{1,9}[.)])\s+(?:\[[ xX]\]\s+)?/, "")
    .trim();
}

/**
 * The lines of a file on the default branch, or null when every line
 * counts as the repository's (init runs on the default branch, or there
 * is none). An empty set: the file isn't on the default branch at all.
 */
export function baseLines(
  ctx: BranchContext,
  root: string,
  repoPath: string,
  git: Git = runGit,
): Set<string> | null {
  if (ctx.onBase || !ctx.base) return null;
  const content = git(["show", `${ctx.base}:${repoPath}`], root);
  if (content === null) return new Set();
  return new Set(
    content
      .split(/\r\n|\r|\n/)
      .map(norm)
      .filter(Boolean),
  );
}

/** Whether every line of a statement is on the default branch. */
export function onBase(lines: Set<string> | null, raw: string[]): boolean {
  if (lines === null) return true;
  return raw.every((r) => {
    const t = norm(r);
    return !t || lines.has(t);
  });
}
