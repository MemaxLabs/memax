// The daemon's checks (epic 3.1, step 4; the stale-detection design §2.1):
// each linked repository's committed tree at HEAD against its space's
// conditions, path_exists for now, reported to Memax as results only (an
// outcome per condition, the commit, and the paths that changed), never a
// file's contents.
//
// # When it may report
//
// Only from the default branch, as pushed: HEAD is the branch origin/HEAD
// names, and HEAD is origin/<default>, so an unpushed commit can't mark
// anything stale; and origin names the space's repository, so a fork's
// clone never reports. On any other branch nothing is checked (branch hints
// for the hook come later).
//
// # When it runs
//
// It stats the refs every few seconds and runs git only when they moved
// (HEAD or the default branch), when the space's conditions changed (its
// feed saw the space move), and every six hours regardless; at most once a
// minute per repository. A run is three git processes however many
// conditions (the branch tips, one `cat-file --batch-check` for the paths,
// one `ls-tree` for every glob's prefix) and a fourth only when a path is
// gone (renames since it last held). It never runs on the delivery path:
// a timer of its own, children at low priority, every read asynchronous.
//
// # What it sends
//
// Only the results that changed since the last report, and a full set at
// most once a day, queued in ~/.memax/daemon/checks/queue/ first (one file
// per report) and sent with the original checked_at, so a report made
// offline arrives within a week; the key comes from the report's fields,
// so a resend is recorded once.
import { createHash } from "node:crypto";
import { mkdirSync, readdirSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { normalizeRepoUrl } from "../project-context.js";
import { failureOf, type DaemonApi } from "./api.js";
import {
  branchTips,
  defaultBranch,
  gitDirs,
  headBranch,
  listPaths,
  originUrl,
  pathsExist,
  refStamp,
  renamesBetween,
  type GitCost,
  type RefTip,
} from "./git-tree.js";
import { globMatcher, globPrefix } from "./glob.js";
import { readJson, writeJsonAtomic } from "./json-file.js";
import type { Logger } from "./log.js";
import type { DaemonPaths } from "./paths.js";
import type { LinkedRepo } from "./registry.js";

export interface CheckTiming {
  /** How often the refs are statted. */
  statMs: number;
  /** The least time between two runs of one repository. */
  minGapMs: number;
  /** A run regardless, this often. */
  scheduleMs: number;
  /** A full set of results, this often. */
  fullMs: number;
  /** The first run after the daemon starts (after its first delivery). */
  firstMs: number;
  /** A glob over more paths than this is unknown. */
  globCap: number;
}

export const CHECK_TIMING: CheckTiming = {
  statMs: 15_000,
  minGapMs: 60_000,
  scheduleMs: 6 * 3_600_000,
  fullMs: 24 * 3_600_000,
  firstMs: 5_000,
  globCap: 20_000,
};

/** Why a repository isn't checked now. */
export type NotChecked =
  | "not_git"
  | "detached"
  | "no_default_branch"
  | "other_branch"
  | "unpushed"
  | "no_repository"
  | "fork";

const NOT_CHECKED: Record<NotChecked, string> = {
  not_git: "not a git repository",
  detached: "HEAD is detached",
  no_default_branch:
    "origin's default branch isn't known (git remote set-head origin --auto)",
  other_branch: "not on the default branch",
  unpushed: "the default branch has commits that aren't pushed",
  no_repository: "the space has no repository to check against",
  fork: "origin isn't the space's repository",
};

export type CheckTrigger = "head_moved" | "schedule" | "conditions" | "verify";

/** Which trigger a run that several asked for says. */
const RANK: Record<CheckTrigger, number> = {
  schedule: 0,
  verify: 1,
  conditions: 2,
  head_moved: 3,
};

/** One condition's result as last reported, without anything a file holds. */
interface Reported {
  hash: string;
  outcome: "holds" | "fails" | "unknown";
  change?: string;
  path?: string;
  renamed_to?: string;
  /** The last commit it held at, here: where a rename is looked for. */
  held?: string;
}

interface CheckFile {
  version: 1;
  root: string;
  space_id: string;
  /** The last commit checked, and what the space's conditions were then. */
  commit: string | null;
  conditions: string | null;
  checked_at: string | null;
  full_at: string | null;
  results: Record<string, Reported>;
}

/** What one run did (for logs, `memax status` and tests). */
export interface CheckRunSummary {
  ran: boolean;
  /** Why it didn't, when it didn't. */
  skipped?: NotChecked | "unchanged" | "no_conditions" | "error";
  commit?: string;
  results?: number;
  /** Results in the report (only those that changed, or a full set). */
  sent?: number;
  /** The memories the report marked stale, when it went out at once. */
  flagged?: string[];
  cost?: GitCost;
  ms?: number;
}

export interface RepoCheckerOptions {
  link: LinkedRepo;
  api: DaemonApi;
  paths: DaemonPaths;
  log: Logger;
  deviceId: string;
  reporter: CheckReporter;
  timing?: Partial<CheckTiming>;
}

/** A condition to check, flat. */
interface PathCondition {
  key: string;
  ref: string;
  id: string;
  hash: string;
  path?: string;
  glob?: string;
  /** A glob's literal folder, and its matcher. */
  prefix?: string;
  match?: (path: string) => boolean;
}

/** A space's conditions as a run uses them, worked out once per fetch. */
interface Prepared {
  conds: V2.SpaceConditions;
  hash: string;
  checks: PathCondition[];
  /** Every condition is one the daemon checks (path_exists, for now). */
  onlyPaths: boolean;
}

const sha = (s: string) => createHash("sha256").update(s).digest("hex");

/** "owner/name" for the repository a remote URL names. */
export function repoKeyOf(url: string): string {
  return normalizeRepoUrl(url).split("/").filter(Boolean).slice(-2).join("/");
}

export class RepoChecker {
  private readonly t: CheckTiming;
  private readonly file: string;
  private stamp = "";
  private statTimer: NodeJS.Timeout | null = null;
  private runTimer: NodeJS.Timeout | null = null;
  private runAt = 0;
  /** The branch tips, read when the refs last moved (no git while they're still). */
  private tips: {
    stamp: string;
    local: RefTip | null;
    origin: RefTip | null;
  } | null = null;
  private running: Promise<CheckRunSummary> | null = null;
  /** What the set timer's run says. */
  private pending: CheckTrigger | null = null;
  /** What was asked for while a run ran: one more after it. */
  private again: CheckTrigger | null = null;
  private lastRunAt = 0;
  private conds: Prepared | null = null;
  private state: CheckFile | null = null;
  private condsAt = 0;
  private condsDirty = true;
  private notChecked: NotChecked | null = null;
  private stopped = false;
  lastRun: CheckRunSummary | null = null;

  constructor(private readonly o: RepoCheckerOptions) {
    this.t = { ...CHECK_TIMING, ...o.timing };
    this.file = join(o.paths.checks, `${sha(o.link.root).slice(0, 24)}.json`);
  }

  start(): void {
    this.statTimer = setInterval(() => this.statRefs(), this.t.statMs);
    this.statTimer.unref?.();
    const d = gitDirs(this.o.link.root);
    this.stamp = d ? refStamp(d) : "";
    this.later("schedule", this.t.firstMs);
  }

  stop(): Promise<void> {
    this.stopped = true;
    if (this.statTimer) clearInterval(this.statTimer);
    if (this.runTimer) clearTimeout(this.runTimer);
    return this.running?.then(() => {}).catch(() => {}) ?? Promise.resolve();
  }

  /** The space moved: its conditions may have changed. */
  conditionsChanged(): void {
    this.condsDirty = true;
    this.soon("conditions");
  }

  /** The refs, statted: a run when HEAD or the default branch moved. */
  private statRefs(): void {
    const d = gitDirs(this.o.link.root);
    const stamp = d ? refStamp(d) : "";
    if (stamp !== this.stamp) {
      this.stamp = stamp;
      this.soon("head_moved");
    }
  }

  /** A run as soon as the minute between runs allows. */
  soon(trigger: CheckTrigger): void {
    this.later(
      trigger,
      Math.max(0, this.lastRunAt + this.t.minGapMs - Date.now()),
    );
  }

  private later(trigger: CheckTrigger, ms: number): void {
    if (this.stopped) return;
    if (!this.pending || RANK[trigger] > RANK[this.pending])
      this.pending = trigger;
    // The earliest asked for wins: a moved HEAD doesn't wait for the
    // six-hourly run already set.
    const at = Date.now() + ms;
    if (this.runTimer && this.runAt <= at) return;
    if (this.runTimer) clearTimeout(this.runTimer);
    this.runAt = at;
    this.runTimer = setTimeout(() => {
      this.runTimer = null;
      const t = this.pending ?? trigger;
      this.pending = null;
      void this.run(t);
    }, ms);
    this.runTimer.unref?.();
  }

  /**
   * One run now (one at a time; the next is set after it: what was asked
   * for meanwhile, a retry after a failure, or the six-hourly run). Never
   * throws: a failure is logged and tried again a minute later.
   */
  run(trigger: CheckTrigger): Promise<CheckRunSummary> {
    if (this.running) {
      if (!this.again || RANK[trigger] > RANK[this.again]) this.again = trigger;
      return this.running;
    }
    let failed = false;
    this.running = this.pass(trigger)
      .catch((err: Error) => {
        failed = true;
        this.o.log.warn("couldn't check a repository", {
          root: this.o.link.root,
          error: err.message,
        });
        return (this.lastRun = {
          ran: false,
          skipped: "error",
        } as CheckRunSummary);
      })
      .finally(() => {
        this.running = null;
        this.lastRunAt = Date.now();
        if (this.stopped) return;
        const next = this.again ?? (failed ? trigger : null);
        this.again = null;
        if (next) this.soon(next);
        // The six-hourly run, unless one is set sooner.
        else this.later("schedule", this.t.scheduleMs);
      });
    return this.running;
  }

  private say(reason: NotChecked | null): void {
    if (reason === this.notChecked) return;
    this.notChecked = reason;
    if (reason)
      this.o.log.info("not checking a repository", {
        root: this.o.link.root,
        why: NOT_CHECKED[reason],
      });
    else
      this.o.log.info("checking a repository", {
        root: this.o.link.root,
        space: this.o.link.space_slug,
      });
  }

  private async pass(trigger: CheckTrigger): Promise<CheckRunSummary> {
    const started = Date.now();
    const cost: GitCost = { processes: 0, bytes: 0 };
    const root = this.o.link.root;
    const skip = (
      why: NonNullable<CheckRunSummary["skipped"]>,
      more: Partial<CheckRunSummary> = {},
    ) => {
      if (why !== "unchanged" && why !== "no_conditions" && why !== "error")
        this.say(why);
      return (this.lastRun = {
        ran: false,
        skipped: why,
        cost,
        ms: Date.now() - started,
        ...more,
      });
    };

    // Where HEAD is, from files; then the tips, in one process.
    const d = gitDirs(root);
    if (!d) return skip("not_git");
    const branch = headBranch(d);
    if (!branch) return skip("detached");
    const def = defaultBranch(d);
    if (!def) return skip("no_default_branch");
    if (branch !== def) return skip("other_branch");
    // The tips as last read, unless the refs moved since.
    const stamp = refStamp(d);
    if (this.tips?.stamp !== stamp) {
      const read = await branchTips(root, branch, cost);
      this.tips = read ? { stamp, ...read } : null;
    }
    const tips = this.tips;
    if (!tips?.local) return skip("not_git");
    if (!tips.origin || tips.origin.commit !== tips.local.commit)
      return skip("unpushed");
    const commit = tips.local.commit;

    // The conditions (fetched when the space moved, or every six hours).
    if (
      this.condsDirty ||
      !this.conds ||
      Date.now() - this.condsAt > this.t.scheduleMs
    ) {
      this.conds = prepare(await this.o.api.conditions(this.o.link.space_id));
      this.condsAt = Date.now();
      this.condsDirty = false;
    }
    const { conds, hash: condsHash, checks, onlyPaths } = this.conds;
    if (!conds.repository) return skip("no_repository");
    const url = originUrl(d);
    if (!url || repoKeyOf(url) !== repoKeyOf(conds.repository))
      return skip("fork");
    this.say(null);

    const state = (this.state ??= this.readState());
    const full =
      !state.full_at || Date.now() - Date.parse(state.full_at) > this.t.fullMs;
    if (
      state.commit === commit &&
      state.conditions === condsHash &&
      !full &&
      trigger !== "verify"
    )
      return skip("unchanged", { commit });

    if (checks.length === 0) {
      this.writeState({
        ...state,
        commit,
        conditions: condsHash,
        checked_at: new Date().toISOString(),
      });
      return skip("no_conditions", { commit });
    }

    // The tree at the commit: the paths in one process, the globs' prefixes in another.
    const exact = [
      ...new Set(checks.filter((c) => c.path).map((c) => c.path!)),
    ];
    const globs = checks.filter((c) => c.glob);
    const exists = await pathsExist(root, commit, exact, cost);
    const tree = globs.length
      ? await listPaths(
          root,
          commit,
          [...new Set(globs.map((c) => c.prefix!))],
          this.t.globCap,
          cost,
        )
      : [];
    // git lists in byte order: a glob reads only the paths under its folder.
    if (tree) tree.sort();

    // Renames, only for paths that are gone, from where they last held.
    const gone = checks.filter((c) => c.path && exists.get(c.path) === false);
    const base = gone
      .map((c) => state.results[c.key]?.held)
      .find((h): h is string => !!h && h !== commit);
    const renamed = base
      ? await renamesBetween(root, base, commit, cost)
      : new Map<string, string>();

    const now: Record<string, Reported> = {};
    for (const c of checks) {
      const prev = state.results[c.key];
      let r: Reported;
      if (c.path) {
        const e = exists.get(c.path);
        if (e === true) r = { hash: c.hash, outcome: "holds", held: commit };
        else if (e === false) {
          const to = prev?.held === base ? renamed.get(c.path) : undefined;
          r = to
            ? {
                hash: c.hash,
                outcome: "fails",
                change: "renamed",
                path: c.path,
                renamed_to: to,
              }
            : {
                hash: c.hash,
                outcome: "fails",
                change: "removed",
                path: c.path,
              };
        } else r = { hash: c.hash, outcome: "unknown" };
      } else if (tree === null) {
        r = { hash: c.hash, outcome: "unknown" };
      } else {
        const prefix = c.prefix!;
        r = anyUnder(tree, prefix, c.match!)
          ? { hash: c.hash, outcome: "holds", held: commit }
          : {
              hash: c.hash,
              outcome: "fails",
              change: "no_match",
              ...(prefix ? { path: prefix } : {}),
            };
      }
      if (r.outcome !== "holds" && prev?.held && prev.hash === c.hash)
        r.held = prev.held;
      now[c.key] = r;
    }

    // Only what changed since the last report, or everything once a day.
    const send = checks.filter(
      (c) => full || !sameResult(state.results[c.key], now[c.key]),
    );
    let flagged: string[] | undefined;
    if (send.length > 0) {
      const input: V2.CheckRunInput = {
        repository: conds.repository,
        branch,
        commit,
        commit_at: tips.local.committedAt,
        checked_at: new Date().toISOString(),
        trigger,
        device: this.o.deviceId,
        complete:
          onlyPaths && checks.every((c) => now[c.key].outcome !== "unknown"),
        results: send.map((c) => resultOf(c, now[c.key])),
      };
      flagged = await this.o.reporter.report(this.o.link.space_id, input);
    }
    // Written when something moved: the commit, the conditions or a result.
    if (
      send.length > 0 ||
      full ||
      state.commit !== commit ||
      state.conditions !== condsHash
    )
      this.writeState({
        ...state,
        commit,
        conditions: condsHash,
        checked_at: new Date().toISOString(),
        full_at: full ? new Date().toISOString() : state.full_at,
        results: now,
      });
    const summary: CheckRunSummary = {
      ran: true,
      commit,
      results: checks.length,
      sent: send.length,
      ...(flagged ? { flagged } : {}),
      cost,
      ms: Date.now() - started,
    };
    this.o.log.info("checked a repository", {
      root,
      commit: commit.slice(0, 7),
      trigger,
      results: checks.length,
      sent: send.length,
      ...(flagged?.length ? { flagged: flagged.join(" ") } : {}),
      git: cost.processes,
      ms: summary.ms,
    });
    return (this.lastRun = summary);
  }

  private readState(): CheckFile {
    const empty: CheckFile = {
      version: 1,
      root: this.o.link.root,
      space_id: this.o.link.space_id,
      commit: null,
      conditions: null,
      checked_at: null,
      full_at: null,
      results: {},
    };
    const f = readJson<Partial<CheckFile>>(this.file, {});
    if (
      f.version !== 1 ||
      f.space_id !== this.o.link.space_id ||
      f.root !== this.o.link.root
    )
      return empty;
    return { ...empty, ...f, results: f.results ?? {} };
  }

  private writeState(f: CheckFile): void {
    this.state = f;
    mkdirSync(this.o.paths.checks, { recursive: true, mode: 0o700 });
    writeJsonAtomic(this.file, f);
  }
}

/**
 * A space's conditions as runs use them: the path_exists ones flat, with
 * each glob's folder and matcher, and a hash of the whole (a run with the
 * same commit and conditions has nothing new to say).
 */
function prepare(conds: V2.SpaceConditions): Prepared {
  const checks: PathCondition[] = [];
  let onlyPaths = true;
  for (const it of conds.items) {
    for (const c of it.conditions) {
      if (c.condition.kind !== "path_exists") {
        onlyPaths = false;
        continue;
      }
      const cond = c.condition as { id?: string; path?: string; glob?: string };
      if (!cond.id || (!cond.path && !cond.glob)) continue;
      const pc: PathCondition = {
        key: `${it.memory}/${cond.id}`,
        ref: it.memory,
        id: cond.id,
        hash: c.hash,
      };
      if (cond.path) pc.path = cond.path;
      else {
        pc.glob = cond.glob;
        pc.prefix = globPrefix(cond.glob!);
        pc.match = globMatcher(cond.glob!);
      }
      checks.push(pc);
    }
  }
  return { conds, hash: sha(JSON.stringify(conds)), checks, onlyPaths };
}

/** Whether a sorted list has a path under prefix that matches. */
function anyUnder(
  sorted: string[],
  prefix: string,
  match: (p: string) => boolean,
): boolean {
  const from = prefix ? `${prefix}/` : "";
  let lo = 0;
  let hi = sorted.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] < from) lo = mid + 1;
    else hi = mid;
  }
  for (let i = lo; i < sorted.length && sorted[i].startsWith(from); i++)
    if (match(sorted[i])) return true;
  return false;
}

function sameResult(a: Reported | undefined, b: Reported): boolean {
  return (
    !!a &&
    a.hash === b.hash &&
    a.outcome === b.outcome &&
    a.change === b.change &&
    a.path === b.path &&
    a.renamed_to === b.renamed_to
  );
}

function resultOf(c: PathCondition, r: Reported): V2.CheckResultInput {
  return {
    memory: c.ref,
    condition: c.id,
    hash: c.hash,
    outcome: r.outcome,
    ...(r.outcome === "fails"
      ? {
          evidence: {
            change: r.change as V2.CheckEvidence["change"],
            ...(r.path ? { path: r.path } : {}),
            ...(r.renamed_to ? { renamed_to: r.renamed_to } : {}),
          },
        }
      : {}),
  };
}

// --- The report queue ---

interface QueuedReport {
  space: string;
  key: string;
  input: V2.CheckRunInput;
}

/** A report older than this isn't sent: the server takes a week. */
const REPORT_WINDOW_MS = 7 * 24 * 3_600_000;

/** A report's Idempotency-Key, from what it says. */
export function reportKey(space: string, input: V2.CheckRunInput): string {
  return `memax-cli-chk-${sha(JSON.stringify([space, input])).slice(0, 48)}`;
}

/** What a failed send means for the report. */
function retryable(err: unknown): boolean {
  const f = failureOf(err);
  return (
    f.status === 0 ||
    f.status === 401 ||
    f.status === 408 ||
    f.status === 429 ||
    f.status >= 500
  );
}

/**
 * Check reports, queued on disk before they're sent, so one made offline
 * or signed out goes out later (with its original checked_at) and a crash
 * loses none. One flush at a time; a minute's retry for what's left.
 */
export class CheckReporter {
  private readonly dir: string;
  private retry: NodeJS.Timeout | null = null;
  private flushing: Promise<void> | null = null;

  constructor(
    private readonly o: {
      paths: DaemonPaths;
      api: DaemonApi;
      log: Logger;
      retryMs?: number;
    },
  ) {
    this.dir = join(o.paths.checks, "queue");
  }

  start(): void {
    this.retry = setInterval(() => void this.flush(), this.o.retryMs ?? 60_000);
    this.retry.unref?.();
    void this.flush();
  }

  async stop(): Promise<void> {
    if (this.retry) clearInterval(this.retry);
    await this.flushing?.catch(() => {});
  }

  /**
   * Queues a report and sends it now: the memories it marked stale, or
   * undefined when it waits in the queue.
   */
  async report(
    space: string,
    input: V2.CheckRunInput,
  ): Promise<string[] | undefined> {
    const q: QueuedReport = { space, key: reportKey(space, input), input };
    mkdirSync(this.dir, { recursive: true, mode: 0o700 });
    writeJsonAtomic(join(this.dir, `${q.key}.json`), q);
    await this.flushing?.catch(() => {});
    return this.send(q);
  }

  /** Sends one report; deletes it unless it should be tried again. */
  private async send(q: QueuedReport): Promise<string[] | undefined> {
    const file = join(this.dir, `${q.key}.json`);
    const age = Date.now() - Date.parse(q.input.checked_at ?? "");
    if (!(age < REPORT_WINDOW_MS)) {
      remove(file);
      return undefined;
    }
    try {
      const r = await this.o.api.reportChecks(q.space, q.input, q.key);
      remove(file);
      if (r.flagged.length)
        this.o.log.info("a check marked memories stale", {
          flagged: r.flagged.join(" "),
          run: r.run.id,
        });
      return r.flagged;
    } catch (err) {
      if (retryable(err)) return undefined;
      remove(file);
      const f = failureOf(err);
      this.o.log.warn("Memax refused a check report", {
        status: f.status,
        code: f.code,
      });
      return undefined;
    }
  }

  /** Every queued report, oldest first. */
  flush(): Promise<void> {
    if (this.flushing) return this.flushing;
    this.flushing = (async () => {
      let names: string[] = [];
      try {
        names = readdirSync(this.dir)
          .filter((n) => n.endsWith(".json"))
          .sort();
      } catch {
        return;
      }
      for (const n of names) {
        const q = readJson<QueuedReport | null>(join(this.dir, n), null);
        if (!q?.input) {
          remove(join(this.dir, n));
          continue;
        }
        await this.send(q);
      }
    })().finally(() => {
      this.flushing = null;
    });
    return this.flushing;
  }
}

function remove(file: string): void {
  try {
    unlinkSync(file);
  } catch {
    // gone already
  }
}
