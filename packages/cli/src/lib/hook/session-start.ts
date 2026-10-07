// `memax hook session-start`: what the hook prints and what it queues,
// from local files only (plan 25 §7.5). It reads the warm cache the
// daemon keeps, the compiled files on disk and what the agent's last
// session here was told, and works out the block; hook-main.ts prints it,
// then saves what was told and queues the compile loads. Nothing here
// opens a socket.
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, sep } from "node:path";
import { cleanLine } from "../daemon/compiler/sanitize.js";
import type { HookAgent } from "./agents.js";
import { DEFAULT_PATHS, wrapFor } from "./agents.js";
import { readCompiled, type CitedLine, type CompiledFile } from "./compiled.js";
import type { QueuedLoad } from "./loads.js";
import {
  renderBlock,
  type BlockBehind,
  type BlockFile,
  type BlockForget,
  type BlockGate,
} from "./render.js";
import { emptySeen, readSeen, seenPath, type Seen } from "./seen.js";
import { readWarm, type WarmSpace, type WarmTarget } from "./warm.js";

export interface HookPaths {
  /** ~/.memax/daemon */
  dir: string;
  /** repos.json: the linked repositories. */
  repos: string;
  pid: string;
  warm: string;
  loads: string;
  seen: string;
}

/**
 * The daemon's files the hook uses: the same places as daemonPaths()
 * (lib/daemon/paths.ts; a test holds them equal), without loading what
 * that module needs to name the control socket.
 */
export function hookPaths(
  dir: string = join(homedir(), ".memax", "daemon"),
): HookPaths {
  return {
    dir,
    repos: join(dir, "repos.json"),
    pid: join(dir, "daemon.pid"),
    warm: join(dir, "warm.json"),
    loads: join(dir, "loads"),
    seen: join(dir, "seen"),
  };
}

export interface SessionStartInput {
  agent: HookAgent;
  /** The agent's stdin event, parsed ({} when there was none). */
  event: Record<string, unknown>;
  env: Record<string, string | undefined>;
  cwd: string;
  paths: HookPaths;
  now: Date;
}

export interface SessionStartResult {
  /** What to print, wrapped for the agent; "" prints nothing. */
  output: string;
  /** Saved after printing, so the next session hears only what's new. */
  seen?: { path: string; value: Seen };
  /** Queued after printing, for the daemon to report. */
  loads: QueuedLoad[];
  /** Why it printed nothing, for --debug. */
  skipped?: string;
}

interface LinkedRepo {
  root: string;
  space_id: string;
  space_slug: string;
}

/** A first session hears about forgets this recent. */
const FIRST_FORGET_WINDOW_MS = 7 * 24 * 60 * 60_000;
const MAX_FORGETS = 20;
const MAX_GATES = 10;

/** A compile's number (C-0881 → 881); compiles count up per tenant. */
function compileNumber(ref: string): number {
  return Number(ref.slice(2)) || 0;
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v !== "" ? v : undefined;
}

/** The directory the session works in: the event's, the agent's variables, or ours. */
export function sessionDir(
  event: Record<string, unknown>,
  env: Record<string, string | undefined>,
  cwd: string,
): string {
  const roots = event.workspace_roots;
  return (
    str(event.cwd) ??
    (Array.isArray(roots) ? str(roots[0]) : undefined) ??
    str(env.CLAUDE_PROJECT_DIR) ??
    str(env.CURSOR_PROJECT_DIR) ??
    str(env.GEMINI_PROJECT_DIR) ??
    cwd
  );
}

/**
 * Whether another agent runs this Claude Code hook: Cursor loads
 * ~/.claude/settings.json hooks (and Copilot CLI a repository's), and
 * their own hooks speak for them.
 */
export function foreignHost(
  agent: HookAgent,
  event: Record<string, unknown>,
  env: Record<string, string | undefined>,
): string | null {
  if (agent.kind !== "claude-code") return null;
  if (env.CURSOR_VERSION || "cursor_version" in event) return "cursor";
  if ("sessionId" in event && !("session_id" in event)) return "copilot";
  return null;
}

function readRegistry(path: string): LinkedRepo[] {
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as {
      repos?: LinkedRepo[];
    };
    return Array.isArray(raw.repos)
      ? raw.repos.filter(
          (r) => typeof r?.root === "string" && typeof r?.space_id === "string",
        )
      : [];
  } catch {
    return [];
  }
}

/** The linked repository holding `dir`, or the git work tree around it. */
export function findRepo(
  dir: string,
  registry: LinkedRepo[],
): { root: string; link?: LinkedRepo } | null {
  let real: string;
  try {
    real = realpathSync(dir);
  } catch {
    return null;
  }
  const link = registry
    .filter((r) => real === r.root || real.startsWith(r.root + sep))
    .sort((a, b) => b.root.length - a.root.length)[0];
  if (link) return { root: link.root, link };
  for (let d = real; ; d = dirname(d)) {
    if (existsSync(join(d, ".git"))) return { root: d };
    if (dirname(d) === d) return null;
  }
}

/**
 * The files the agent loads here: the cached targets of its kinds, else
 * the default paths (a file there counts only if Memax compiled it).
 */
function loadedFiles(
  agent: HookAgent,
  warm: WarmSpace | undefined,
): Array<{ kind: string; path: string; target?: WarmTarget }> {
  const out: Array<{ kind: string; path: string; target?: WarmTarget }> = [];
  for (const kind of agent.loads) {
    const cached = warm?.targets.filter((t) => t.kind === kind) ?? [];
    if (cached.length > 0) {
      for (const t of cached) out.push({ kind, path: t.path, target: t });
    } else if (DEFAULT_PATHS[kind]) {
      out.push({ kind, path: DEFAULT_PATHS[kind] });
    }
  }
  return out;
}

/**
 * What changed in one file since the last session: nothing on a baseline
 * (the agent's first session here), only that it is new when the last
 * session didn't have it (it loads in full), else the cited lines added,
 * changed and gone.
 */
function diffFile(
  file: CompiledFile,
  before: Seen["files"][string] | undefined,
  baseline: boolean,
  target: WarmTarget | undefined,
  forgottenRefs: Set<string>,
): BlockFile {
  const out: BlockFile = {
    path: file.path,
    compile: file.compile,
    added: [],
    changed: [],
    gone: [],
    handEdit: false,
  };
  if (baseline || (before && before.compile === file.compile)) return out;
  if (!before) return { ...out, fresh: true };
  out.was = before.compile;
  // Lines the server didn't compile (a hand-added cite) aren't news, and a
  // forgotten memory's line stays unsaid even while the file still has it.
  const known =
    target?.compile === file.compile && target.refs
      ? new Set(target.refs)
      : null;
  const ours = (l: CitedLine) =>
    l.refs.every((r) => !forgottenRefs.has(r) && (!known || known.has(r)));
  const old = before.lines;
  const keys = new Set(file.lines.map((l) => l.key));
  for (const l of file.lines) {
    if (!ours(l)) continue;
    if (!(l.key in old)) out.added.push(l);
    else if (old[l.key] !== l.hash) out.changed.push(l);
  }
  const goneRefs = new Set<string>();
  for (const k of Object.keys(old)) {
    if (keys.has(k)) continue;
    for (const r of k.split("#")[0].split(","))
      if (!forgottenRefs.has(r)) goneRefs.add(r);
  }
  for (const l of file.lines) for (const r of l.refs) goneRefs.delete(r);
  out.gone = [...goneRefs].sort();
  if (target && (target.sync_state === "drifted" || target.open_drift > 0)) {
    out.handEdit = out.added.length + out.changed.length > 0;
    out.added = [];
    out.changed = [];
  }
  return out;
}

export function sessionStart(i: SessionStartInput): SessionStartResult {
  const nothing = (skipped: string): SessionStartResult => ({
    output: "",
    loads: [],
    skipped,
  });
  const host = foreignHost(i.agent, i.event, i.env);
  if (host) return nothing(`running under ${host}, whose own hook speaks`);

  const dir = sessionDir(i.event, i.env, i.cwd);
  const repo = findRepo(dir, readRegistry(i.paths.repos));
  if (!repo) return nothing("not in a repository");
  const warmFile = readWarm(i.paths.warm);
  const warm = repo.link ? warmFile.spaces[repo.link.space_id] : undefined;

  const files: Array<{ file: CompiledFile; target?: WarmTarget }> = [];
  for (const f of loadedFiles(i.agent, warm)) {
    const file = readCompiled(join(repo.root, f.path), f.path);
    if (file) files.push({ file, target: f.target });
  }
  if (files.length === 0 && !warm) return nothing("no compiled files here");

  const space =
    repo.link?.space_slug ?? warm?.slug ?? files[0]?.file.space ?? "";
  const seenFile = seenPath(i.paths.seen, repo.root, i.agent.kind);
  const before = readSeen(seenFile);
  const first = before === null;
  const prev = before ?? emptySeen();
  const nowMs = i.now.getTime();

  // Forgets: every one since the last session; a first session, the last week's.
  const forgets = warm?.forgotten ?? [];
  const forgottenRefs = new Set(
    forgets.flatMap((f) => [f.ref, ...f.with]).filter((r) => r !== "space"),
  );
  const toldForgets = new Set(prev.forgotten);
  const newForgets: BlockForget[] = forgets
    .filter((f) =>
      first
        ? nowMs - Date.parse(f.at) < FIRST_FORGET_WINDOW_MS
        : !toldForgets.has(f.id),
    )
    .slice(0, MAX_FORGETS)
    .map((f) => ({ ref: f.ref, with: f.with, at: f.at }));

  // Gates still waiting that this agent hasn't heard of here.
  const toldGates = new Set(prev.gates);
  const waiting = (warm?.gates ?? []).filter(
    (g) => Date.parse(g.expires_at) > nowMs,
  );
  const newGates: BlockGate[] = waiting
    .filter((g) => !toldGates.has(g.ref))
    .slice(0, MAX_GATES)
    .map((g) => ({
      ref: g.ref,
      question: cleanLine(g.question).text,
      agent: g.agent,
      expires_at: g.expires_at,
    }));

  // Files whose newest compile hasn't reached this machine (C- numbers
  // only grow; a file newer than the cache means the cache is behind).
  const toldBehind = new Set(prev.behind);
  const behind: BlockBehind[] = [];
  for (const { file, target } of files) {
    if (
      target?.compile &&
      compileNumber(target.compile) > compileNumber(file.compile) &&
      !toldBehind.has(target.compile)
    )
      behind.push({
        path: file.path,
        here: file.compile,
        latest: target.compile,
        sync_state: target.sync_state,
      });
  }

  // A session that saw no compiled file here is a baseline too.
  const baseline = first || Object.keys(prev.files).length === 0;
  const blockFiles = files.map(({ file, target }) =>
    diffFile(file, prev.files[file.path], baseline, target, forgottenRefs),
  );
  const block = renderBlock({
    space,
    files: blockFiles,
    forgotten: newForgets,
    gates: newGates,
    behind,
    maxTokens: i.agent.maxTokens,
  });

  const seen: Seen = {
    version: 1,
    at: i.now.toISOString(),
    files: Object.fromEntries(
      files.map(({ file }) => [
        file.path,
        {
          compile: file.compile,
          lines: Object.fromEntries(file.lines.map((l) => [l.key, l.hash])),
        },
      ]),
    ),
    forgotten: [
      ...forgets.map((f) => f.id),
      ...prev.forgotten.filter((id) => !forgets.some((f) => f.id === id)),
    ],
    gates: [
      ...waiting.map((g) => g.ref),
      ...prev.gates.filter((r) => !waiting.some((g) => g.ref === r)),
    ],
    behind: [...behind.map((b) => b.latest), ...prev.behind],
  };

  const source = str(i.event.source) ?? "startup";
  const session = str(i.event.session_id) ?? str(i.event.sessionId);
  const loadedAt = i.now.toISOString();
  const loads: QueuedLoad[] =
    source === "compact"
      ? []
      : files.map(({ file }) => ({
          space: repo.link?.space_id ?? file.space,
          compile: file.compile,
          agent: i.agent.kind,
          ...(session ? { session_ref: session.slice(0, 255) } : {}),
          loaded_at: loadedAt,
        }));

  return {
    output: wrapFor(i.agent.format, block),
    seen: { path: seenFile, value: seen },
    loads,
    ...(block === "" ? { skipped: first ? "first session" : "no news" } : {}),
  };
}
