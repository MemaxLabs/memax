// Real git repositories for the checks' tests: a clone of MemaxLabs/memax
// (a bare origin named so), on main, pushed, in a temporary folder that
// cleanupRepos() removes.
import { execFileSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_NOSYSTEM: "1",
  HOME: tmpdir(),
};

export const git = (cwd: string, ...args: string[]) =>
  execFileSync("git", args, { cwd, env: GIT_ENV, stdio: "pipe" })
    .toString()
    .trim();

const bases: string[] = [];

/** Removes every repository made since the last call (afterEach). */
export function cleanupRepos(): void {
  for (const b of bases.splice(0)) rmSync(b, { recursive: true, force: true });
}

/** A temporary folder that cleanupRepos() removes. */
export function tempDir(prefix = "memax-checks-"): string {
  const base = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  bases.push(base);
  return base;
}

/** A clone of MemaxLabs/memax (a bare origin named so), on main, pushed. */
export function repository(files: Record<string, string>): {
  base: string;
  root: string;
} {
  const base = tempDir();
  const root = join(base, "repo");
  const origin = join(base, "MemaxLabs", "memax.git");
  mkdirSync(root);
  mkdirSync(origin, { recursive: true });
  git(origin, "init", "-q", "--bare", "-b", "main");
  git(root, "init", "-q", "-b", "main");
  write(root, files);
  git(root, "add", "-A");
  git(root, "commit", "-q", "-m", "first");
  git(root, "remote", "add", "origin", origin);
  git(root, "push", "-q", "-u", "origin", "main");
  git(root, "remote", "set-head", "origin", "main");
  return { base, root };
}

export function write(root: string, files: Record<string, string>): void {
  for (const [p, content] of Object.entries(files)) {
    mkdirSync(join(root, p, ".."), { recursive: true });
    writeFileSync(join(root, p), content);
  }
}
