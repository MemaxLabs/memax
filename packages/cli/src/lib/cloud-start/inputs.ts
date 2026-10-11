// What a cloud start reads from the sandbox (cloud warm-start design
// §3.2): the repository, the compiled files the agent loads (their
// headers and drift hashes, never their words), the commit and branch (as
// files, without spawning git), the credential and where the API is. And
// whether this is a person's own machine instead, where the local hook
// speaks.
import { existsSync, readFileSync, realpathSync, statSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import type { HookAgent } from "../hook/agents.js";
import { DEFAULT_PATHS } from "../hook/agents.js";
import { parseCompiled, type CompiledFile } from "../hook/compiled.js";
import { driftHash } from "../daemon/compiler/drift.js";

/** AGENTS.md is held to 32 KiB; a person's own file may be bigger. */
const MAX_BYTES = 256 * 1024;

export const DEFAULT_API_URL = "https://api.memax.app";

type Env = Record<string, string | undefined>;

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v.trim() : undefined;
}

/**
 * The repository the session works in: the git work tree around the
 * event's `cwd`, else around CLAUDE_PROJECT_DIR, else around the working
 * directory (`/workspace` on Copilot); null when there is none.
 */
export function repoRoot(
  event: Record<string, unknown>,
  env: Env,
  cwd: string,
): string | null {
  const dir = str(event.cwd) ?? str(env.CLAUDE_PROJECT_DIR) ?? cwd;
  let real: string;
  try {
    real = realpathSync(isAbsolute(dir) ? dir : resolve(cwd, dir));
  } catch {
    return null;
  }
  for (let d = real; ; d = dirname(d)) {
    if (existsSync(join(d, ".git"))) return d;
    if (dirname(d) === d) return null;
  }
}

export interface LoadedFile {
  file: CompiledFile;
  /** The drift hash the server compares with what its compile wrote. */
  drift: string;
}

function readHead(abs: string): string | null {
  try {
    const st = statSync(abs);
    if (!st.isFile() || st.size > MAX_BYTES) return null;
    return readFileSync(abs, "utf8");
  } catch {
    return null;
  }
}

/**
 * The compiled files the agent loads at session start, at their default
 * paths (CLAUDE.md, then AGENTS.md, for Claude Code): only those whose
 * header says Memax compiled them.
 */
export function loadedFiles(root: string, agent: HookAgent): LoadedFile[] {
  const out: LoadedFile[] = [];
  for (const kind of agent.loads) {
    const path = DEFAULT_PATHS[kind];
    if (!path) continue;
    const content = readHead(join(root, path));
    if (content === null) continue;
    const file = parseCompiled(path, content);
    if (file) out.push({ file, drift: driftHash(content) });
  }
  return out;
}

export interface GitState {
  commit?: string;
  branch?: string;
}

const SHA = /^[0-9a-f]{40}(?:[0-9a-f]{24})?$/;

/** The repository's git directory: `.git`, or where a `.git` file points. */
function gitDir(root: string): string | null {
  const dot = join(root, ".git");
  try {
    if (statSync(dot).isDirectory()) return dot;
    const m = /^gitdir:\s*(.+)$/m.exec(readFileSync(dot, "utf8"));
    return m ? resolve(root, m[1].trim()) : null;
  } catch {
    return null;
  }
}

function readRef(dir: string, common: string, ref: string): string | undefined {
  for (const base of [dir, common]) {
    try {
      const v = readFileSync(join(base, ref), "utf8").trim();
      if (SHA.test(v)) return v;
    } catch {
      // not a loose ref here
    }
  }
  try {
    for (const line of readFileSync(join(common, "packed-refs"), "utf8").split(
      "\n",
    )) {
      const [sha, name] = line.trim().split(" ");
      if (name === ref && SHA.test(sha)) return sha;
    }
  } catch {
    // no packed refs
  }
  return undefined;
}

/** HEAD's commit and branch, read as files; nothing on any doubt. */
export function gitState(root: string): GitState {
  const dir = gitDir(root);
  if (!dir) return {};
  let common = dir;
  try {
    common = resolve(dir, readFileSync(join(dir, "commondir"), "utf8").trim());
  } catch {
    // not a linked work tree
  }
  let head: string;
  try {
    head = readFileSync(join(dir, "HEAD"), "utf8").trim();
  } catch {
    return {};
  }
  if (SHA.test(head)) return { commit: head };
  const m = /^ref:\s*(refs\/heads\/(.+))$/.exec(head);
  if (!m) return {};
  const branch = m[2].length <= 255 ? m[2] : undefined;
  return { commit: readRef(dir, common, m[1]), branch };
}

/**
 * Whether this is a person's own machine: the daemon's folder or the
 * CLI's credentials are in its home. There the local session-start hook
 * speaks (Copilot CLI runs a repository's `.github/hooks` too).
 */
export function personsMachine(home: string): boolean {
  return (
    existsSync(join(home, ".memax", "daemon")) ||
    existsSync(join(home, ".memax", "credentials.json"))
  );
}

/**
 * The start key, from MEMAX_START_KEY. Without one, a Claude Code cloud
 * session still asks, since its proxy may attach a network secret for the
 * API's host; a 401 then means it isn't set up. Elsewhere, nothing to ask
 * with.
 */
export function credential(env: Env): { key: string } | null {
  const key = str(env.MEMAX_START_KEY);
  if (key) return { key };
  if (env.CLAUDE_CODE_REMOTE === "true") return { key: "" };
  return null;
}

/** The API's origin: MEMAX_API_URL (staging, a local server), else Memax's. */
export function apiUrl(env: Env): URL | null {
  try {
    const u = new URL(str(env.MEMAX_API_URL) ?? DEFAULT_API_URL);
    if (u.protocol !== "https:" && u.protocol !== "http:") return null;
    return u;
  } catch {
    return null;
  }
}
