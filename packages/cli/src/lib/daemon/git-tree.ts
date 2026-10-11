// Git plumbing for the daemon's checks (the stale-detection design §2.1):
// what a repository's committed tree at a commit holds, never its working
// copy. Every child runs at low priority with lazy fetching off (a partial
// clone never touches the network: a missing blob is unknown) and without
// optional locks (it never contends with the person's own git), and only
// asynchronously, so the daemon's deliveries never wait for one.
import { spawn } from "node:child_process";
import { readFileSync, statSync } from "node:fs";
import { isAbsolute, join, resolve } from "node:path";
import { setPriority } from "node:os";

/** Where a clone keeps its git files (a worktree's are split in two). */
export interface GitDirs {
  /** HEAD lives here (.git, or .git/worktrees/<name> for a worktree). */
  gitDir: string;
  /** Refs, packed-refs and config live here. */
  commonDir: string;
}

/** The git directories of the repository at root, or null when it has none. */
export function gitDirs(root: string): GitDirs | null {
  const dotGit = join(root, ".git");
  let gitDir: string;
  try {
    if (statSync(dotGit).isDirectory()) {
      gitDir = dotGit;
    } else {
      const m = /^gitdir:\s*(.+)$/m.exec(readFileSync(dotGit, "utf8"));
      if (!m) return null;
      gitDir = resolve(root, m[1].trim());
    }
  } catch {
    return null;
  }
  let commonDir = gitDir;
  try {
    const c = readFileSync(join(gitDir, "commondir"), "utf8").trim();
    commonDir = isAbsolute(c) ? c : resolve(gitDir, c);
  } catch {
    // not a worktree
  }
  return { gitDir, commonDir };
}

function read(path: string): string | null {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return null;
  }
}

/** The branch a symbolic ref file names ("main" for "ref: refs/heads/main"). */
function symref(path: string, prefix: string): string | null {
  const m = /^ref:\s*(\S+)/.exec(read(path) ?? "");
  return m && m[1].startsWith(prefix) ? m[1].slice(prefix.length) : null;
}

/** The checked-out branch, from HEAD (null when detached). */
export function headBranch(d: GitDirs): string | null {
  return symref(join(d.gitDir, "HEAD"), "refs/heads/");
}

/** The branch origin/HEAD names: the default branch, as last fetched. */
export function defaultBranch(d: GitDirs): string | null {
  return symref(
    join(d.commonDir, "refs", "remotes", "origin", "HEAD"),
    "refs/remotes/origin/",
  );
}

/** origin's URL, from the clone's config (a file read, not a process). */
export function originUrl(d: GitDirs): string | null {
  const config = read(join(d.commonDir, "config"));
  if (!config) return null;
  let inOrigin = false;
  for (const raw of config.split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith("[")) {
      inOrigin = /^\[remote\s+"origin"\]$/.test(line);
      continue;
    }
    const m = inOrigin ? /^url\s*=\s*(.+)$/.exec(line) : null;
    if (m) return m[1].trim();
  }
  return null;
}

/**
 * Changes whenever HEAD, the default branch, its remote-tracking ref or
 * the packed refs move: what the daemon stats every few seconds, so it runs
 * no git while the refs are still.
 */
export function refStamp(d: GitDirs): string {
  const branch = headBranch(d) ?? "";
  const def = defaultBranch(d) ?? "";
  const files = [
    join(d.gitDir, "HEAD"),
    join(d.commonDir, "packed-refs"),
    join(d.commonDir, "refs", "remotes", "origin", "HEAD"),
    join(d.commonDir, "reftable", "tables.list"),
    ...(branch ? [join(d.commonDir, "refs", "heads", branch)] : []),
    ...(def ? [join(d.commonDir, "refs", "remotes", "origin", def)] : []),
  ];
  return files
    .map((f) => {
      try {
        const st = statSync(f);
        return `${st.ino}:${st.mtimeMs}:${st.size}`;
      } catch {
        return "-";
      }
    })
    .join("|");
}

/** What a git child cost: the bytes it sent back. */
export interface GitCost {
  processes: number;
  bytes: number;
}

export interface GitResult {
  code: number;
  stdout: Buffer;
  stderr: string;
}

/** The environment every check's git runs in (built once: the daemon's doesn't change). */
let env: NodeJS.ProcessEnv | null = null;
function gitEnv(): NodeJS.ProcessEnv {
  env ??= {
    ...process.env,
    GIT_NO_LAZY_FETCH: "1",
    GIT_OPTIONAL_LOCKS: "0",
    GIT_TERMINAL_PROMPT: "0",
    LC_ALL: "C",
  };
  return env;
}

/**
 * Runs one git command in root, at low priority, feeding it input, and
 * stops reading (and kills it) past maxBytes of output: the answer is
 * then null, and the check says unknown.
 */
export function runGit(
  root: string,
  args: string[],
  o: {
    input?: string;
    maxBytes?: number;
    cost?: GitCost;
    signal?: AbortSignal;
  } = {},
): Promise<GitResult | null> {
  return new Promise((done) => {
    const child = spawn("git", ["-C", root, ...args], {
      env: gitEnv(),
      stdio: ["pipe", "pipe", "pipe"],
      signal: o.signal,
    });
    if (o.cost) o.cost.processes++;
    try {
      if (child.pid) setPriority(child.pid, 10);
    } catch {
      // not allowed here: it runs at the daemon's priority
    }
    const chunks: Buffer[] = [];
    let size = 0;
    let over = false;
    let stderr = "";
    child.stdout.on("data", (b: Buffer) => {
      if (over) return;
      size += b.length;
      if (o.cost) o.cost.bytes += b.length;
      if (o.maxBytes !== undefined && size > o.maxBytes) {
        over = true;
        child.kill();
        return;
      }
      chunks.push(b);
    });
    child.stderr.on("data", (b: Buffer) => {
      if (stderr.length < 2000) stderr += b.toString();
    });
    child.on("error", () => done(null));
    child.on("close", (code) => {
      if (over) return done(null);
      done({ code: code ?? 1, stdout: Buffer.concat(chunks), stderr });
    });
    child.stdin.on("error", () => {});
    child.stdin.end(o.input ?? "");
  });
}

/** A branch tip and its commit time, as for-each-ref reads them. */
export interface RefTip {
  commit: string;
  committedAt: string;
}

/**
 * The tips of refs/heads/<branch> and refs/remotes/origin/<branch>, in one
 * process (packed or loose, files or reftable).
 */
export async function branchTips(
  root: string,
  branch: string,
  cost?: GitCost,
): Promise<{ local: RefTip | null; origin: RefTip | null } | null> {
  const local = `refs/heads/${branch}`;
  const remote = `refs/remotes/origin/${branch}`;
  const r = await runGit(
    root,
    [
      "for-each-ref",
      "--format=%(refname)%00%(objectname)%00%(committerdate:iso-strict)",
      local,
      remote,
    ],
    { cost, maxBytes: 4096 },
  );
  if (!r || r.code !== 0) return null;
  const tips: Record<string, RefTip> = {};
  for (const line of r.stdout.toString().split("\n")) {
    const [name, commit, at] = line.split("\0");
    if (name && commit && at)
      tips[name] = { commit, committedAt: new Date(at).toISOString() };
  }
  return { local: tips[local] ?? null, origin: tips[remote] ?? null };
}

/**
 * Whether each path is in the tree at commit (a file, a folder or a
 * submodule), in one `git cat-file --batch-check`: true, false, or null
 * when git couldn't say.
 */
export async function pathsExist(
  root: string,
  commit: string,
  paths: string[],
  cost?: GitCost,
): Promise<Map<string, boolean | null>> {
  const out = new Map<string, boolean | null>();
  if (paths.length === 0) return out;
  const r = await runGit(
    root,
    ["cat-file", "--buffer", "--batch-check=%(objectname) %(objecttype)"],
    {
      input: paths.map((p) => `${commit}:${p}\n`).join(""),
      cost,
      maxBytes: paths.length * 128 + 4096,
    },
  );
  const lines = r && r.code === 0 ? r.stdout.toString().split("\n") : [];
  paths.forEach((p, i) => {
    const line = lines[i];
    if (line === undefined) out.set(p, null);
    else if (line.endsWith(" missing")) out.set(p, false);
    else
      out.set(
        p,
        /^[0-9a-f]{40,64} (blob|tree|commit)$/.test(line) ? true : null,
      );
  });
  return out;
}

/**
 * The paths under the prefixes at commit, in one `git ls-tree`; null past
 * cap entries (a glob over that much is unknown) or when git couldn't say.
 */
export async function listPaths(
  root: string,
  commit: string,
  prefixes: string[],
  cap: number,
  cost?: GitCost,
): Promise<string[] | null> {
  const whole = prefixes.includes("");
  const r = await runGit(
    root,
    [
      "ls-tree",
      "-r",
      "-z",
      "--name-only",
      commit,
      ...(whole ? [] : ["--", ...prefixes]),
    ],
    { cost, maxBytes: cap * 256 },
  );
  if (!r || r.code !== 0) return null;
  const paths = r.stdout.toString().split("\0").filter(Boolean);
  return paths.length > cap ? null : paths;
}

/**
 * The files renamed between two commits (90% similar or more), old → new,
 * in one `git diff`; null when git couldn't say (the older commit isn't in
 * this clone: a shallow one, or another machine's newer hold).
 */
export async function renamesBetween(
  root: string,
  from: string,
  to: string,
  cost?: GitCost,
): Promise<Map<string, string> | null> {
  const out = new Map<string, string>();
  const r = await runGit(
    root,
    ["diff", "-z", "-M90%", "--name-status", "--diff-filter=R", from, to],
    {
      cost,
      maxBytes: 4 * 1024 * 1024,
    },
  );
  if (!r || r.code !== 0) return null;
  const parts = r.stdout.toString().split("\0");
  for (let i = 0; i + 2 < parts.length; i += 3) {
    if (parts[i].startsWith("R")) out.set(parts[i + 1], parts[i + 2]);
  }
  return out;
}
