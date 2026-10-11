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

/**
 * What a check's git must not inherit: a repository other than root's
 * (GIT_DIR and the like), or pathspecs read some other way, which would
 * read a glob pathspec as a literal path, and every bare manifest name
 * into "gone".
 */
const DROPPED = [
  "GIT_DIR",
  "GIT_WORK_TREE",
  "GIT_COMMON_DIR",
  "GIT_INDEX_FILE",
  "GIT_NAMESPACE",
  "GIT_LITERAL_PATHSPECS",
  "GIT_GLOB_PATHSPECS",
  "GIT_NOGLOB_PATHSPECS",
  "GIT_ICASE_PATHSPECS",
];

/** The environment every check's git runs in (built once: the daemon's doesn't change). */
let env: NodeJS.ProcessEnv | null = null;
function gitEnv(): NodeJS.ProcessEnv {
  if (env) return env;
  env = {
    ...process.env,
    GIT_NO_LAZY_FETCH: "1",
    GIT_OPTIONAL_LOCKS: "0",
    GIT_TERMINAL_PROMPT: "0",
    LC_ALL: "C",
  };
  for (const k of DROPPED) delete env[k];
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

/** What a path is in a commit's tree, as `cat-file` reads it. */
export interface ObjectInfo {
  type: "blob" | "tree" | "commit";
  id: string;
  size: number;
}

/** Each path's object at the commit: null when nothing is there, undefined when git couldn't say. */
export type Entries = Map<string, ObjectInfo | null | undefined>;

/** The caps on what a run reads of blobs. */
export interface ReadCaps {
  /** A blob larger than this isn't read. */
  fileBytes: number;
  /** Nor any past this many bytes in all, smallest first. */
  totalBytes: number;
}

const INFO = /^([0-9a-f]{40}|[0-9a-f]{64}) (blob|tree|commit) (\d+)$/;

function infoOf(line: string): ObjectInfo | null | undefined {
  if (line.endsWith(" missing")) return null;
  const m = INFO.exec(line);
  return m
    ? { id: m[1], type: m[2] as ObjectInfo["type"], size: Number(m[3]) }
    : undefined;
}

/** The blob ids to read: the ones asked for, under the caps, smallest first. */
function chosen(ids: string[], entries: Entries, caps: ReadCaps): string[] {
  const size = new Map<string, number>();
  for (const e of entries.values())
    if (e?.type === "blob") size.set(e.id, e.size);
  const want = [...new Set(ids)]
    .filter((id) => (size.get(id) ?? Infinity) <= caps.fileBytes)
    .sort((a, b) => size.get(a)! - size.get(b)! || (a < b ? -1 : 1));
  const out: string[] = [];
  let total = 0;
  for (const id of want) {
    if (total + size.get(id)! > caps.totalBytes) break;
    total += size.get(id)!;
    out.push(id);
  }
  return out;
}

/** The bytes a contents answer for these blobs takes: headers, contents, newlines. */
function expected(ids: string[], entries: Entries): number {
  const size = new Map<string, number>();
  for (const e of entries.values())
    if (e?.type === "blob") size.set(e.id, e.size);
  return ids.reduce((n, id) => n + (size.get(id) ?? 0) + id.length + 32, 0);
}

/**
 * Contents answers ("<id> blob <size>\n<contents>\n", or "<id> missing\n")
 * as they arrive: each blob goes to onBlob once whole, a view of the
 * chunk it came in (copied only when it spans chunks), and nothing keeps
 * it after, so a run holds about one blob at a time, not all it reads.
 */
class ContentStream {
  private head = "";
  /** Bytes of the current answer still to come (its newline included); -1 reading a header. */
  private need = -1;
  private id = "";
  private size = 0;
  private got = 0;
  private parts: Buffer[] = [];

  constructor(
    private left: number,
    private readonly onBlob: (id: string, contents: Buffer) => void,
  ) {}

  push(b: Buffer): void {
    let at = 0;
    while (at < b.length && this.left > 0) {
      if (this.need < 0) {
        const nl = b.indexOf(10, at);
        if (nl < 0) {
          this.head += b.toString("latin1", at);
          return;
        }
        this.head += b.toString("latin1", at, nl);
        at = nl + 1;
        const h = infoOf(this.head);
        this.head = "";
        if (!h) {
          this.left--; // missing: not in this clone
          continue;
        }
        if (h.type !== "blob") {
          this.left = 0;
          return;
        }
        this.id = h.id;
        this.size = h.size;
        this.need = h.size + 1;
        this.got = 0;
        continue;
      }
      const k = Math.min(this.need, b.length - at);
      const body = Math.min(k, this.size - this.got);
      if (body > 0) {
        this.parts.push(b.subarray(at, at + body));
        this.got += body;
      }
      at += k;
      this.need -= k;
      if (this.need === 0) {
        const blob =
          this.parts.length === 1
            ? this.parts[0]
            : Buffer.concat(this.parts, this.size);
        this.parts = [];
        this.need = -1;
        this.left--;
        this.onBlob(this.id, blob);
      }
    }
  }
}

/**
 * What each path is at commit, and the contents of the blobs `want` picks
 * once it knows them (only those under the caps), each handed to onBlob as
 * it arrives, in one `git cat-file --batch-command` (Git 2.36 and later):
 * every path's info, then the blobs' contents. Older git takes two
 * processes, `--batch-check` then `--batch`.
 */
export async function readObjects(
  root: string,
  commit: string,
  paths: string[],
  want: (entries: Entries) => string[],
  onBlob: (id: string, contents: Buffer) => void,
  caps: ReadCaps,
  cost?: GitCost,
): Promise<Entries> {
  const entries: Entries = new Map();
  // A path git can't take on one line can't be asked about.
  const asked: string[] = [];
  for (const p of paths)
    if (/[\n\r]/.test(p)) entries.set(p, undefined);
    else asked.push(p);
  if (asked.length > 0) {
    const ok = await batchCommand(
      root,
      commit,
      asked,
      want,
      onBlob,
      caps,
      entries,
      cost,
    );
    if (ok) return entries;
    // Git before 2.36: the infos, then the contents.
    const r = await runGit(root, ["cat-file", "--buffer", "--batch-check"], {
      input: asked.map((p) => `${commit}:${p}\n`).join(""),
      cost,
      maxBytes: asked.length * 160 + 4096,
    });
    const lines = r && r.code === 0 ? r.stdout.toString().split("\n") : [];
    asked.forEach((p, i) =>
      entries.set(p, lines[i] === undefined ? undefined : infoOf(lines[i])),
    );
  }
  const ids = chosen(want(entries), entries, caps);
  if (ids.length > 0) {
    const r = await runGit(root, ["cat-file", "--buffer", "--batch"], {
      input: ids.map((id) => `${id}\n`).join(""),
      cost,
      maxBytes: expected(ids, entries) + 4096,
    });
    if (r && r.code === 0) new ContentStream(ids.length, onBlob).push(r.stdout);
  }
  return entries;
}

/**
 * One `cat-file --batch-command --buffer`: the infos and a flush; once
 * they're all back, the contents `want` picks and a flush, each blob to
 * onBlob as it arrives. False when this git has no --batch-command.
 */
function batchCommand(
  root: string,
  commit: string,
  paths: string[],
  want: (entries: Entries) => string[],
  onBlob: (id: string, contents: Buffer) => void,
  caps: ReadCaps,
  entries: Entries,
  cost?: GitCost,
): Promise<boolean> {
  return new Promise((done) => {
    const child = spawn(
      "git",
      ["-C", root, "cat-file", "--batch-command", "--buffer"],
      { env: gitEnv(), stdio: ["pipe", "pipe", "pipe"] },
    );
    if (cost) cost.processes++;
    try {
      if (child.pid) setPriority(child.pid, 10);
    } catch {
      // not allowed here: it runs at the daemon's priority
    }
    let head: Buffer = Buffer.alloc(0);
    let infos = 0;
    let limit = paths.length * 160 + 4096;
    let size = 0;
    let over = false;
    let contents: ContentStream | null = null;
    child.stdout.on("data", (b: Buffer) => {
      if (over) return;
      size += b.length;
      if (cost) cost.bytes += b.length;
      if (size > limit) {
        over = true;
        child.kill();
        return;
      }
      if (contents) {
        contents.push(b);
        return;
      }
      head = head.length ? Buffer.concat([head, b]) : b;
      let at = 0;
      let nl = head.indexOf(10);
      while (nl >= 0 && infos < paths.length) {
        entries.set(paths[infos++], infoOf(head.toString("latin1", at, nl)));
        at = nl + 1;
        nl = head.indexOf(10, at);
      }
      head = head.subarray(at);
      if (infos < paths.length) return;
      const ids = chosen(want(entries), entries, caps);
      limit += expected(ids, entries) + 4096;
      contents = new ContentStream(ids.length, onBlob);
      if (ids.length)
        child.stdin.write(
          ids.map((id) => `contents ${id}\n`).join("") + "flush\n",
        );
      child.stdin.end();
      if (head.length) contents.push(head);
      head = Buffer.alloc(0);
    });
    child.stderr.on("data", () => {});
    child.stdin.on("error", () => {});
    child.on("error", () => done(false));
    child.on("close", (code) => {
      if (infos === 0 && code !== 0) return done(false); // no --batch-command here
      for (const p of paths) if (!entries.has(p)) entries.set(p, undefined);
      done(true);
    });
    child.stdin.write(
      paths.map((p) => `info ${commit}:${p}\n`).join("") + "flush\n",
    );
  });
}

/** The empty tree, in each object format: what a listing diffs a commit against. */
const EMPTY_TREE: Record<number, string> = {
  40: "4b825dc642cb6eb9a060e54bf8d69288fbee4904",
  64: "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321",
};

/** What listTree read. */
export interface Listing {
  /** The files listed, in git's (byte) order; null when git couldn't say. */
  paths: string[] | null;
  /** More than the cap: a glob over them is unknown. */
  over: boolean;
}

/**
 * The files under the folders, and every file with one of the names
 * anywhere (a bare manifest name), at commit, in one `git diff-tree`
 * against the empty tree, since ls-tree takes no glob pathspec and a bare
 * name needs one. A folder of "" lists the whole tree.
 */
export async function listTree(
  root: string,
  commit: string,
  prefixes: string[],
  names: string[],
  cap: number,
  cost?: GitCost,
): Promise<Listing> {
  const whole = prefixes.includes("");
  const specs = whole
    ? []
    : [
        ...prefixes.map((p) => `:(literal)${p}`),
        ...names.map((n) => `:(glob)**/${n}`),
      ];
  if (!whole && specs.length === 0) return { paths: [], over: false };
  const empty = EMPTY_TREE[commit.length];
  if (!empty) return { paths: null, over: false };
  const r = await runGit(
    root,
    [
      "diff-tree",
      "-r",
      "-z",
      "--name-only",
      "--no-renames",
      empty,
      commit,
      ...(specs.length ? ["--", ...specs] : []),
    ],
    { cost, maxBytes: cap * 256 + names.length * 64 * 1024 },
  );
  if (!r || r.code !== 0) return { paths: null, over: false };
  const paths = r.stdout.toString().split("\0").filter(Boolean);
  return { paths, over: paths.length > cap };
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
