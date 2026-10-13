// `memax hook session-start`: what the hook prints and what it queues,
// from local files only (plan 25 §7.5). It reads the warm cache the
// daemon keeps, the compiled files on disk and what the agent's last
// session here was told, and works out the block; hook-main.ts prints it,
// then saves what was told and queues the compile loads. Nothing here
// opens a socket.
//
// On a machine whose home is linked (personal-layer design §7.1), it does
// the same for the agent's own home file (home.json names it, the warm
// cache's `home` entry has its compiles, forgets and gates), against
// what the agent's last session anywhere was told (seen/home/<agent>.json),
// as a Personal section after the repository's; outside a repository, as
// the whole block. Its loads count reads of personal facts.
import { existsSync, readFileSync, realpathSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve, sep } from "node:path";
import { cleanLine } from "../daemon/compiler/sanitize.js";
import { evidenceWords } from "../stale/words.js";
import type { CheckRequest } from "./check-requests.js";
import type { HookAgent } from "./agents.js";
import { DEFAULT_PATHS, OPT_IN_KINDS, wrapFor } from "./agents.js";
import { readCompiled, type CitedLine, type CompiledFile } from "./compiled.js";
import { readHookHome } from "./home.js";
import type { QueuedLoad } from "./loads.js";
import {
  renderBlock,
  type BlockBehind,
  type BlockBranch,
  type BlockFile,
  type BlockForget,
  type BlockGate,
  type BlockSection,
  type BlockVerifying,
} from "./render.js";
import { emptySeen, readSeen, seenPath, type Seen } from "./seen.js";
import {
  readWarm,
  type WarmBranch,
  type WarmSpace,
  type WarmStale,
  type WarmTarget,
} from "./warm.js";

export interface HookPaths {
  /** ~/.memax/daemon */
  dir: string;
  /** repos.json: the linked repositories. */
  repos: string;
  /** home.json: this machine's home link. */
  home: string;
  pid: string;
  warm: string;
  loads: string;
  seen: string;
  /** Check requests for the daemon (check-requests.ts). */
  checkRequests: string;
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
    home: join(dir, "home.json"),
    pid: join(dir, "daemon.pid"),
    warm: join(dir, "warm.json"),
    loads: join(dir, "loads"),
    seen: join(dir, "seen"),
    checkRequests: join(dir, "checks", "requests"),
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
  /** The same for the Personal section (seen/home/<agent>.json). */
  homeSeen?: { path: string; value: Seen };
  /** Queued after printing, for the daemon to report. */
  loads: QueuedLoad[];
  /** Queued after printing: the daemon checks this repository soon. */
  checkRequest?: CheckRequest;
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
const MAX_VERIFYING = 10;
/** A branch's hints older than this aren't said: the daemon hasn't looked since. */
const BRANCH_HINT_MS = 24 * 60 * 60_000;

/** What a repository section adds: what's being verified, and this branch's hints. */
interface Checked {
  stale?: WarmStale[];
  branch?: WarmBranch;
}

/** The branch checked out in root, from .git/HEAD (files only; a worktree too). */
export function currentBranch(root: string): string | null {
  try {
    let git = join(root, ".git");
    if (statSync(git).isFile()) {
      const m = /^gitdir:\s*(.+)$/m.exec(readFileSync(git, "utf8"));
      if (!m) return null;
      git = resolve(root, m[1].trim());
    }
    const head = readFileSync(join(git, "HEAD"), "utf8").trim();
    return head.startsWith("ref: refs/heads/")
      ? head.slice("ref: refs/heads/".length)
      : null;
  } catch {
    return null;
  }
}

/** Why a memory is stale, in words: the first cause the hook can say. */
function staleWhy(x: WarmStale): string {
  for (const c of x.causes) {
    if (c.kind === "check")
      return evidenceWords(c.condition, c.evidence, c.branch);
    if (c.kind === "date")
      return `its date to check again, ${c.stale_after.slice(0, 10)}, has passed.`;
  }
  return "it was marked stale; check it against its sources.";
}

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
 * the default paths (a file there counts only if Memax compiled it). An
 * opt-in kind (GEMINI.md) falls back only for a space not cached here: a
 * cached space without that target compiles none, whatever file sits at
 * the path.
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
    } else if (DEFAULT_PATHS[kind] && !(warm && OPT_IN_KINDS.has(kind))) {
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

/** One section's news, what to save, and the compiles its agent loaded. */
interface Part {
  block: BlockSection;
  seen: { path: string; value: Seen };
  loaded: Array<{ space: string; compile: string }>;
  first: boolean;
}

/**
 * Works out one section (the repository's, or the Personal space's on this
 * machine) against what the agent's last session was told.
 */
function part(
  now: Date,
  files: Array<{ file: CompiledFile; target?: WarmTarget }>,
  warm: WarmSpace | undefined,
  space: string,
  spaceId: string | undefined,
  seenFile: string,
  checked: Checked = {},
): Part {
  const before = readSeen(seenFile);
  const first = before === null;
  const prev = before ?? emptySeen();
  const nowMs = now.getTime();

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

  // Memories marked stale since the last session here (all of them in a
  // first one), never one forgotten since.
  const told = new Set(prev.verifying ?? []);
  const staleKey = (x: WarmStale) => `${x.ref}@${x.since}`;
  const stale = (checked.stale ?? []).filter((x) => !forgottenRefs.has(x.ref));
  const shown = stale
    .filter((x) => !told.has(staleKey(x)))
    .slice(0, MAX_VERIFYING);
  const verifying: BlockVerifying[] = shown.map((x) => ({
    ref: x.ref,
    why: staleWhy(x),
  }));
  // This branch's hints not told before, from a check within a day.
  const toldHints = new Set(prev.hints ?? []);
  const b = checked.branch;
  const hintKey = (h: WarmBranch["items"][number]) =>
    `${b!.branch}\0${h.memory}\0${h.condition.id ?? ""}\0${h.evidence?.change ?? ""}`;
  const hints = b
    ? b.items.filter(
        (h) => !forgottenRefs.has(h.memory) && !toldHints.has(hintKey(h)),
      )
    : [];
  const branch: BlockBranch | undefined =
    b && hints.length > 0
      ? {
          name: b.branch,
          ...(b.unpushed ? { unpushed: true } : {}),
          at: b.checked_at,
          hints: hints.slice(0, MAX_VERIFYING).map((h) => ({
            ref: h.memory,
            why: evidenceWords(h.condition, h.evidence, "this branch"),
          })),
        }
      : undefined;

  // A session that saw no compiled file here is a baseline too.
  const baseline = first || Object.keys(prev.files).length === 0;
  const blockFiles = files.map(({ file, target }) =>
    diffFile(file, prev.files[file.path], baseline, target, forgottenRefs),
  );

  const seen: Seen = {
    version: 1,
    at: now.toISOString(),
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
    // Told and still marked stale stays told; one verified since drops out
    // (a cache with no stale list yet keeps what was told).
    verifying: checked.stale
      ? [
          ...shown.map(staleKey),
          ...stale.map(staleKey).filter((k) => told.has(k)),
        ]
      : (prev.verifying ?? []),
    hints: [
      ...new Set([
        ...hints.slice(0, MAX_VERIFYING).map(hintKey),
        ...(prev.hints ?? []),
      ]),
    ],
  };
  return {
    block: {
      space,
      files: blockFiles,
      forgotten: newForgets,
      gates: newGates,
      behind,
      ...(verifying.length > 0 ? { verifying } : {}),
      ...(branch ? { branch } : {}),
    },
    seen: { path: seenFile, value: seen },
    loaded: files.map(({ file }) => ({
      space: spaceId ?? file.space,
      compile: file.compile,
    })),
    first,
  };
}

/** The repository's section, or null with why. */
function repoPart(
  i: SessionStartInput,
  repo: { root: string; link?: LinkedRepo },
  warmFile: ReturnType<typeof readWarm>,
): Part | string {
  const warm = repo.link ? warmFile.spaces[repo.link.space_id] : undefined;
  // V1 writes its files again: they aren't Memax's to speak for.
  if (warm?.on_v1) return "the space is on V1 again";
  const files: Array<{ file: CompiledFile; target?: WarmTarget }> = [];
  for (const f of loadedFiles(i.agent, warm)) {
    const file = readCompiled(join(repo.root, f.path), f.path);
    if (file) files.push({ file, target: f.target });
  }
  if (files.length === 0 && !warm) return "no compiled files here";
  const space =
    repo.link?.space_slug ?? warm?.slug ?? files[0]?.file.space ?? "";
  // This branch's hints, when the daemon checked the branch checked out
  // now, within a day.
  const hints = repo.link ? warmFile.branches?.[repo.root] : undefined;
  const branch =
    hints &&
    hints.branch === currentBranch(repo.root) &&
    i.now.getTime() - Date.parse(hints.checked_at) < BRANCH_HINT_MS
      ? hints
      : undefined;
  return part(
    i.now,
    files,
    warm,
    space,
    repo.link?.space_id,
    seenPath(i.paths.seen, repo.root, i.agent.kind),
    { stale: warm?.stale, branch },
  );
}

/** The Personal section: the agent's own home file on this machine. */
function homePart(
  i: SessionStartInput,
  warmFile: ReturnType<typeof readWarm>,
): Part | null {
  if (i.agent.home.length === 0) return null;
  const home = readHookHome(i.paths.home);
  if (!home) return null;
  const warm =
    warmFile.home?.space_id === home.space_id ? warmFile.home : undefined;
  if (warm?.on_v1) return null;
  const files: Array<{ file: CompiledFile; target?: WarmTarget }> = [];
  for (const f of home.files) {
    if (!i.agent.home.includes(f.kind)) continue;
    // A file another agent reads (Antigravity's ~/.gemini/AGENTS.md, before
    // Gemini CLI's settings list it) isn't one this agent loads.
    if (f.read_by && !f.read_by.includes(i.agent.kind)) continue;
    const file = readCompiled(f.abs, `~/${f.path}`);
    if (!file) continue;
    const target = warm?.targets.find(
      (t) => t.kind === f.kind && t.path === f.path,
    );
    files.push({ file, target });
  }
  if (files.length === 0) return null;
  return part(
    i.now,
    files,
    warm,
    home.space_slug,
    home.space_id,
    join(
      i.paths.seen,
      "home",
      `${i.agent.kind.replace(/[^a-z0-9-]/gi, "_")}.json`,
    ),
  );
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
  const warmFile = readWarm(i.paths.warm);
  const inRepo = repo ? repoPart(i, repo, warmFile) : "not in a repository";
  const atHome = homePart(i, warmFile);
  if (typeof inRepo === "string" && !atHome) return nothing(inRepo);
  const parts = [typeof inRepo === "string" ? null : inRepo, atHome];

  const block = renderBlock({
    repo: typeof inRepo === "string" ? undefined : inRepo.block,
    personal: atHome?.block,
    maxTokens: i.agent.maxTokens,
  });

  const source = str(i.event.source) ?? "startup";
  const session = str(i.event.session_id) ?? str(i.event.sessionId);
  const loadedAt = i.now.toISOString();
  const loads: QueuedLoad[] =
    source === "compact"
      ? []
      : parts.flatMap((p) =>
          (p?.loaded ?? []).map((l) => ({
            space: l.space,
            compile: l.compile,
            agent: i.agent.kind,
            ...(session ? { session_ref: session.slice(0, 255) } : {}),
            loaded_at: loadedAt,
          })),
        );
  const first = parts.every((p) => !p || p.first);
  // The daemon checks the repository soon, for this agent (a linked one
  // on V2; a compaction is the same session).
  const checkRequest: CheckRequest | undefined =
    repo?.link && typeof inRepo !== "string" && source !== "compact"
      ? { root: repo.root, agent: i.agent.kind, at: loadedAt }
      : undefined;

  return {
    output: wrapFor(i.agent.format, block),
    ...(checkRequest ? { checkRequest } : {}),
    ...(typeof inRepo === "string" ? {} : { seen: inRepo.seen }),
    ...(atHome ? { homeSeen: atHome.seen } : {}),
    loads,
    ...(block === "" ? { skipped: first ? "first session" : "no news" } : {}),
  };
}
