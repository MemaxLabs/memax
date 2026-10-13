// The daemon's checks (epic 3.1, steps 4 and 5; the stale-detection design
// §2.1): each linked repository's committed tree at HEAD against its
// space's conditions (path_exists, file_contains, dep_present and
// file_unchanged; pr_state is GitHub's, and Memax doesn't list it),
// evaluated by the compiler's evaluator (compiler/conditions/, a verbatim
// copy) and reported to Memax as results only: an outcome per condition,
// the commit, and the paths and declared constraints that changed, never a
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
// conditions: the branch tips; one `diff-tree` listing every glob's folder
// and every manifest a bare name means; one `cat-file --batch-command` for
// every path's object, then the contents of the files whose blobs changed
// since the last run (each at most 1 MiB, 4 MiB a run: a result read off
// the same blobs stands, so a run after a push reads only what the push
// changed). A fourth only when a path is gone (renames since it last
// held). It never runs on the delivery path: a timer of its own, children
// at low priority, every read asynchronous.
//
// # What it sends
//
// Only the results that changed since the last report, and a full set at
// most once a day, queued in ~/.memax/daemon/checks/queue/ first (one file
// per report) and sent with the original checked_at, so a report made
// offline arrives within a week; the key comes from the report's fields,
// so a resend is recorded once. Its files hold refs, condition ids, hashes,
// outcomes, paths, declared constraints and blob ids, never a file's
// contents or a condition's text.
import { createHash } from "node:crypto";
import { mkdirSync, readdirSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import type { V2 } from "memax-sdk";
import { normalizeRepoUrl } from "../project-context.js";
import { failureOf, type DaemonApi } from "./api.js";
import {
  checkNeeds,
  evaluate,
  validPath,
  type CheckCondition,
  type CheckEvaluation,
  type CheckNeeds,
  type TreeView,
} from "./compiler/conditions/evaluate.js";
import {
  branchTips,
  defaultBranch,
  gitDirs,
  headBranch,
  listTree,
  originUrl,
  readObjects,
  refStamp,
  renamesBetween,
  type Entries,
  type GitCost,
  type RefTip,
} from "./git-tree.js";
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
  /** A file larger than this isn't read (unknown). */
  fileCap: number;
  /** Nor any past this much in one run, smallest first. */
  readCap: number;
}

/**
 * At most this many rename lookups a run (one `git diff` each, from each
 * distinct commit a gone path last held at): an armed path past them is
 * unknown, never a removal it might not be.
 */
const RENAME_BASES = 3;

export const CHECK_TIMING: CheckTiming = {
  statMs: 15_000,
  minGapMs: 60_000,
  scheduleMs: 6 * 3_600_000,
  fullMs: 24 * 3_600_000,
  firstMs: 5_000,
  globCap: 20_000,
  fileCap: 1024 * 1024,
  readCap: 4 * 1024 * 1024,
};

/** The kinds whose file can be renamed out from under them (CheckChange). */
const FOLLOWS = new Set(["path_exists", "file_contains", "file_unchanged"]);

/** Why a repository isn't checked now. */
export type NotChecked =
  | "not_git"
  | "detached"
  | "no_default_branch"
  | "other_branch"
  | "unpushed"
  | "no_repository"
  | "fork"
  | "on_v1";

const NOT_CHECKED: Record<NotChecked, string> = {
  not_git: "not a git repository",
  detached: "HEAD is detached",
  no_default_branch:
    "origin's default branch isn't known (git remote set-head origin --auto)",
  other_branch: "not on the default branch",
  unpushed: "the default branch has commits that aren't pushed",
  no_repository: "the space has no repository to check against",
  fork: "origin isn't the space's repository",
  on_v1: "the space is on V1 again, and Memax checks only spaces on V2",
};

export type CheckTrigger =
  | "head_moved"
  | "session_start"
  | "schedule"
  | "conditions"
  | "verify";

/** Which trigger a run that several asked for says. */
const RANK: Record<CheckTrigger, number> = {
  schedule: 0,
  session_start: 1,
  verify: 2,
  conditions: 3,
  head_moved: 4,
};

/** One condition's result as last reported, without anything a file holds. */
interface Reported {
  hash: string;
  outcome: "holds" | "fails" | "unknown";
  change?: string;
  path?: string;
  renamed_to?: string;
  /** A dependency's declared constraint now (constraint_changed). */
  constraint?: string;
  /** The last commit it held at, here: where a rename is looked for. */
  held?: string;
  /** A bare manifest name's: the manifest it held in. */
  resolved?: string;
  /** The blobs it read (their paths' and ids' hash): the same blobs, the same result. */
  seen?: string;
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

/**
 * What a run found at HEAD on a branch other than the pushed default (or
 * on the default with commits not pushed yet): this machine's alone, kept
 * for the session-start hook and memax check, never reported, and never
 * a branch name sent anywhere.
 */
export interface BranchHints {
  branch: string;
  /** The default branch, with commits that aren't pushed yet. */
  unpushed?: boolean;
  commit: string;
  checked_at: string;
  /**
   * Armed conditions of memories not marked stale that fail at this HEAD
   * but not on the default branch as this machine last checked it.
   */
  items: Array<{
    memory: string;
    condition: V2.Condition;
    evidence?: V2.CheckEvidence;
  }>;
}

/** One condition as `memax check` shows it, with what this machine last found. */
export interface CheckedItem {
  memory: string;
  /** As the space lists it, with its id. */
  condition: V2.Condition;
  /** It held once on the default branch, so a failure marks the memory stale. */
  armed: boolean;
  /** Absent when no run here has checked it (or the daemon doesn't check its kind). */
  outcome?: V2.CheckResultInput["outcome"];
  evidence?: V2.CheckEvidence;
}

/** A repository's checks as the last run here left them. */
export interface CheckView {
  root: string;
  space_id: string;
  /** The default branch's commit last checked here, and when. */
  commit: string | null;
  checked_at: string | null;
  /** Why the latest run checked nothing, in words, when it didn't. */
  not_checked?: string;
  items: CheckedItem[];
  /** What it found at HEAD off the pushed default branch, never reported. */
  branch?: BranchHints;
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
  /** Those whose stale mark (Memax's, from a check) it took off: a revert. */
  unflagged?: string[];
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
  /**
   * The space's memories marked stale and why, each time the conditions
   * are read (the warm cache's stale list, for the session-start hook).
   */
  onStale?(spaceId: string, stale: V2.StaleMemory[]): void;
  /** This repository's branch hints changed, or went (null). */
  onBranch?(root: string, hints: BranchHints | null): void;
}

/** A condition to check, flat. */
interface Check {
  key: string;
  ref: string;
  id: string;
  hash: string;
  /** Its kind and fields, as the evaluator reads them. */
  cond: CheckCondition;
  needs: CheckNeeds;
  /** The file whose rename it follows (FOLLOWS), when it names one. */
  path?: string;
  /** It held once on the default branch, so its failure can mark the memory. */
  armed: boolean;
  /** Its latest hold Memax heard of, from any machine (held_commit). */
  held?: string;
}

/** A space's conditions as a run uses them, worked out once per fetch. */
interface Prepared {
  conds: V2.SpaceConditions;
  hash: string;
  checks: Check[];
  /** Every condition listed is of a kind the daemon checks. */
  everyKind: boolean;
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
  /** Memories forgotten while the daemon ran: never checked or reported again. */
  private readonly gone = new Set<string>();
  /**
   * A Forget took something from this checker: its next run reports even
   * with nothing changed, so Memax hears that this machine checked the
   * space since (the tombstone's checks step).
   */
  private heard = false;
  /** The space is on V1 again, which takes no check reports. */
  private onV1 = false;
  /**
   * A session start asked for the next run, which then runs whatever ran
   * last and reports, naming the agent, whichever trigger it goes out as
   * (one already waiting may outrank session_start).
   */
  private asked = false;
  private agent: V2.AgentKind | undefined;
  /** What the last run off the pushed default branch found, and at what. */
  private branch: BranchHints | null = null;
  private branchKey = "";
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

  /**
   * Memories forgotten (rule 7): their conditions go from memory and their
   * results from the state file now, and no run in flight reports them.
   */
  forget(refs: string[]): void {
    const refsGone = refs.map((r) => r.toUpperCase());
    for (const r of refsGone) this.gone.add(r);
    const hinted = this.branch;
    if (hinted?.items.some((i) => this.gone.has(i.memory.toUpperCase())))
      this.hints({
        ...hinted,
        items: hinted.items.filter(
          (i) => !this.gone.has(i.memory.toUpperCase()),
        ),
      });
    if (this.conds?.checks.some((c) => this.gone.has(c.ref))) {
      this.conds = null;
      this.condsDirty = true;
    }
    const state = (this.state ??= this.readState());
    const keys = Object.keys(state.results).filter((k) =>
      this.gone.has(k.slice(0, k.indexOf("/"))),
    );
    if (keys.length === 0) return;
    this.heard = true;
    this.condsDirty = true;
    this.soon("conditions");
    const results = { ...state.results };
    for (const k of keys) delete results[k];
    this.writeState({ ...state, results });
    this.o.log.info("dropped a forgotten memory's checks", {
      root: this.o.link.root,
      memories: [...new Set(keys.map((k) => k.slice(0, k.indexOf("/"))))].join(
        " ",
      ),
    });
  }

  /** The repository is unlinked: its state file goes, with all it held. */
  discard(): void {
    this.state = null;
    remove(this.file);
  }

  /**
   * The space was switched back to V1, or is on V2 again (a run soon, with
   * the conditions read again). A Forget reaches the state file either way.
   */
  setOnV1(on: boolean): void {
    if (on === this.onV1) return;
    this.onV1 = on;
    if (!on) this.conditionsChanged();
  }

  /**
   * An agent's session started here (the hook's request): a run soon
   * whatever ran last, which reports even with nothing new, naming the
   * agent, so the memory's page says when it was checked and for whom.
   */
  sessionStarted(agent?: V2.AgentKind): void {
    this.asked = true;
    if (agent) this.agent = agent;
    this.soon("session_start");
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

    // A session start's request is this run's, whatever it says.
    const asked = this.asked;
    const agent = asked ? this.agent : undefined;
    this.asked = false;
    this.agent = undefined;
    if (this.onV1) return this.hints(null, skip("on_v1"));
    // Where HEAD is, from files; then the tips, in one process.
    const d = gitDirs(root);
    if (!d) return this.hints(null, skip("not_git"));
    const branch = headBranch(d);
    if (!branch) return this.hints(null, skip("detached"));
    const def = defaultBranch(d);
    if (!def) return this.hints(null, skip("no_default_branch"));
    // The tips as last read, unless the refs moved since.
    const stamp = refStamp(d);
    if (this.tips?.stamp !== stamp) {
      const read = await branchTips(root, branch, cost);
      this.tips = read ? { stamp, ...read } : null;
    }
    const tips = this.tips;
    if (!tips?.local) return this.hints(null, skip("not_git"));
    const commit = tips.local.commit;
    // Only the default branch as pushed reports; anywhere else the run's
    // results stay on this machine, as hints.
    const pushed = !!tips.origin && tips.origin.commit === commit;
    const local: NotChecked | null =
      branch !== def ? "other_branch" : pushed ? null : "unpushed";

    // The conditions (fetched when the space moved, or every six hours),
    // and with them what's marked stale, for the hook.
    if (
      this.condsDirty ||
      !this.conds ||
      Date.now() - this.condsAt > this.t.scheduleMs
    ) {
      this.conds = prepare(await this.o.api.conditions(this.o.link.space_id));
      this.condsAt = Date.now();
      this.condsDirty = false;
      this.o.onStale?.(
        this.o.link.space_id,
        (this.conds.conds.stale ?? []).filter(
          (x) => !this.gone.has(x.memory.toUpperCase()),
        ),
      );
    }
    const { conds, hash: condsHash, everyKind } = this.conds;
    // A memory forgotten since the conditions were read isn't checked.
    const checks = this.conds.checks.filter((c) => !this.gone.has(c.ref));
    if (!conds.repository) return this.hints(null, skip("no_repository"));
    const url = originUrl(d);
    if (!url || repoKeyOf(url) !== repoKeyOf(conds.repository))
      return this.hints(null, skip("fork"));

    const state = (this.state ??= this.readState());
    if (local) {
      // Off the pushed default branch: the same reading, kept here.
      const key = `${branch}\0${commit}\0${condsHash}`;
      if (key !== this.branchKey || trigger === "verify" || asked) {
        const now =
          checks.length > 0
            ? await this.evaluateAt(root, commit, checks, state, cost)
            : {};
        const stale = new Set(
          (conds.stale ?? []).map((x) => x.memory.toUpperCase()),
        );
        const items = checks
          .filter(
            (c) =>
              c.armed &&
              !stale.has(c.ref) &&
              now[c.key]?.outcome === "fails" &&
              state.results[c.key]?.outcome !== "fails",
          )
          .map((c) => {
            const evidence = evidenceOf(now[c.key]);
            return {
              memory: c.ref,
              condition: c.cond as V2.Condition,
              ...(evidence ? { evidence } : {}),
            };
          });
        this.branchKey = key;
        this.hints({
          branch,
          ...(local === "unpushed" ? { unpushed: true } : {}),
          commit,
          checked_at: new Date().toISOString(),
          items,
        });
      }
      return skip(local, { commit, results: checks.length });
    }
    this.say(null);
    this.hints(null);
    const full =
      !state.full_at || Date.now() - Date.parse(state.full_at) > this.t.fullMs;
    if (
      state.commit === commit &&
      state.conditions === condsHash &&
      !full &&
      !this.heard &&
      trigger !== "verify" &&
      !asked
    )
      return skip("unchanged", { commit });

    if (checks.length === 0) {
      // The Forget took its last condition: a run of nothing says this
      // machine checked the space since.
      if (this.heard) {
        await this.o.reporter.report(this.o.link.space_id, {
          repository: conds.repository,
          branch,
          commit,
          commit_at: tips.local.committedAt,
          checked_at: new Date().toISOString(),
          trigger,
          device: this.o.deviceId,
          complete: everyKind,
          results: [],
        });
        this.heard = false;
      }
      this.writeState({
        ...state,
        commit,
        conditions: condsHash,
        checked_at: new Date().toISOString(),
      });
      return skip("no_conditions", { commit });
    }

    const now = await this.evaluateAt(root, commit, checks, state, cost);

    // A memory forgotten while this run read the tree: nothing of it is
    // reported or kept.
    for (const c of checks) if (this.gone.has(c.ref)) delete now[c.key];
    // Only what changed since the last report, or everything once a day.
    const send = checks.filter(
      (c) =>
        now[c.key] && (full || !sameResult(state.results[c.key], now[c.key])),
    );
    let flagged: string[] | undefined;
    let unflagged: string[] | undefined;
    // A session start's run reports even with nothing new: it says this
    // machine checked when that agent started.
    const confirm = this.heard || asked;
    if (send.length > 0 || confirm) {
      const input: V2.CheckRunInput = {
        repository: conds.repository,
        branch,
        commit,
        commit_at: tips.local.committedAt,
        checked_at: new Date().toISOString(),
        // What the memory's page says ("when Codex started"), even when a
        // run already waiting took the request in.
        trigger: asked ? "session_start" : trigger,
        ...(agent ? { agent } : {}),
        device: this.o.deviceId,
        complete:
          everyKind && checks.every((c) => now[c.key]?.outcome !== "unknown"),
        results: send.map((c) => resultOf(c, now[c.key])),
      };
      const sent = await this.o.reporter.report(this.o.link.space_id, input);
      flagged = sent?.flagged;
      unflagged = sent?.unflagged;
      this.heard = false;
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
      ...(unflagged?.length ? { unflagged } : {}),
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

  /**
   * Every check at a commit: one listing of the globs' folders and the
   * manifests a bare name means, then one cat-file for every path's
   * object and the contents of the files whose blobs changed since the
   * last run (a result read off the same blobs stands), then a rename
   * lookup for files gone since they last held. The default branch's
   * results are `state`'s; off it, they're read but never kept.
   */
  private async evaluateAt(
    root: string,
    commit: string,
    checks: Check[],
    state: CheckFile,
    cost: GitCost,
  ): Promise<Record<string, Reported>> {
    // What the conditions need of the tree at the commit: one listing of
    // the globs' folders and the manifests a bare name means, then one
    // cat-file for every path's object and the contents of the files whose
    // blobs changed since the last run.
    const prefixes = new Set<string>();
    const names = new Set<string>();
    for (const c of checks) {
      for (const x of c.needs.prefixes) prefixes.add(x);
      for (const x of c.needs.names) names.add(x);
    }
    const listing =
      prefixes.size || names.size
        ? await listTree(
            root,
            commit,
            [...prefixes],
            [...names],
            this.t.globCap,
            cost,
          )
        : { paths: [], over: false };
    // git lists in byte order: a glob reads only the paths under its folder.
    const listed = listing.paths ? [...listing.paths].sort() : null;
    const named = new Map<string, string[]>();
    if (listed)
      for (const n of names) {
        const tail = `/${n}`;
        named.set(
          n,
          listed.filter((x) => x === n || x.endsWith(tail)),
        );
      }
    /** The files whose contents each condition reads, worked out once. */
    const files = new Map<Check, readonly string[]>();
    for (const c of checks)
      files.set(
        c,
        c.needs.names.length === 0
          ? c.needs.texts
          : [
              ...c.needs.texts,
              ...c.needs.names.flatMap((n) => named.get(n) ?? []),
            ],
      );
    const filesOf = (c: Check) => files.get(c)!;
    const asked = new Set<string>();
    for (const c of checks) {
      for (const x of c.needs.entries) asked.add(x);
      for (const x of filesOf(c)) asked.add(x);
    }
    // A result read off the same blobs as last time stands, unread. The
    // rest are answered as each blob arrives: a file_contains on its bytes
    // then and there, a manifest kept as text; the blob itself isn't kept.
    const stands = new Map<string, Reported>();
    const needles = new Map<string, Array<{ text: string; word: boolean }>>();
    const manifests = new Set<string>();
    const found = new Map<string, boolean>();
    const texts = new Map<string, string>();
    const needle = (id: string, text: string, word: boolean) =>
      `${id}\0${word ? 1 : 0}\0${text}`;
    const entries: Entries = asked.size
      ? await readObjects(
          root,
          commit,
          [...asked],
          (es) => {
            const ids: string[] = [];
            for (const c of checks) {
              const files = filesOf(c);
              if (files.length === 0) continue;
              const prev = state.results[c.key];
              const seen = blobsSeen(files, es);
              if (
                seen &&
                prev?.hash === c.hash &&
                prev.seen === seen &&
                prev.outcome !== "unknown"
              ) {
                stands.set(c.key, prev);
                continue;
              }
              for (const f of files) {
                const e = es.get(f);
                if (e?.type !== "blob") continue;
                ids.push(e.id);
                if (c.cond.kind === "file_contains") {
                  const list = needles.get(e.id) ?? [];
                  list.push({ text: c.cond.text!, word: c.cond.word === true });
                  needles.set(e.id, list);
                } else manifests.add(e.id);
              }
            }
            return ids;
          },
          (id, blob) => {
            for (const n of needles.get(id) ?? [])
              found.set(
                needle(id, n.text, n.word),
                bytesContain(blob, n.text, n.word),
              );
            if (manifests.has(id)) texts.set(id, blob.toString("utf8"));
          },
          { fileBytes: this.t.fileCap, totalBytes: this.t.readCap },
          cost,
        )
      : new Map();
    const blobAt = (x: string) => {
      const e = entries.get(x);
      return e?.type === "blob" ? e.id : undefined;
    };
    const view: TreeView = {
      entry: (x) => entries.get(x),
      text: (x) => {
        const id = blobAt(x);
        return id ? texts.get(id) : undefined;
      },
      under: () => (listed && !listing.over ? listed : undefined),
      named: (n) => named.get(n),
      contains: (x, text, word) => {
        const id = blobAt(x);
        return id ? found.get(needle(id, text, word)) : undefined;
      },
    };

    const now: Record<string, Reported> = {};
    for (const c of checks) {
      const prev = state.results[c.key];
      const same = prev?.hash === c.hash ? prev : undefined;
      const stood = stands.get(c.key);
      const r: Reported = stood
        ? withoutHeld(stood)
        : reportedOf(
            c.hash,
            evaluate(
              c.cond,
              view,
              same?.resolved ? { resolved: same.resolved } : undefined,
            ),
          );
      if (r.outcome !== "unknown" && !stood) {
        const seen = blobsSeen(filesOf(c), entries);
        if (seen) r.seen = seen;
      }
      if (r.outcome === "holds") r.held = commit;
      else if (same?.held) r.held = same.held;
      now[c.key] = r;
    }

    // Renames, only for files that are gone, from where each last held:
    // here, or as Memax heard of it from another machine (held_commit).
    const gone = checks.filter(
      (c) =>
        c.path && now[c.key].change === "removed" && now[c.key].path === c.path,
    );
    const baseOf = new Map<string, string>();
    for (const c of gone) {
      const prev = state.results[c.key];
      const b = (prev?.hash === c.hash ? prev.held : undefined) ?? c.held;
      if (b && b !== commit) baseOf.set(c.key, b);
    }
    const renames = new Map<string, Map<string, string> | null>();
    for (const b of [...new Set(baseOf.values())].slice(0, RENAME_BASES))
      renames.set(b, await renamesBetween(root, b, commit, cost));
    for (const c of gone) {
      const b = baseOf.get(c.key);
      const moves = b ? renames.get(b) : undefined;
      const to = moves?.get(c.path!);
      const r = now[c.key];
      // Gone since it held, and git couldn't say where (the commit it held
      // at isn't here, or past the run's rename lookups), or it went where
      // Memax's evidence can't name: a move read as a removal would mark a
      // source's rename stale.
      if (c.armed && b && (!moves || (to && !validPath(to))))
        now[c.key] = {
          hash: c.hash,
          outcome: "unknown",
          ...(r.held ? { held: r.held } : {}),
        };
      else if (to && validPath(to))
        now[c.key] = { ...r, change: "renamed", renamed_to: to };
    }

    return now;
  }

  /**
   * Keeps the branch hints a run found (or none, back on the pushed
   * default branch, or where nothing is checked), telling onBranch when
   * they changed; returns `summary` for the run's return.
   */
  private hints<T>(h: BranchHints | null, summary?: T): T {
    const was = this.branch;
    this.branch = h;
    if (JSON.stringify(was) !== JSON.stringify(h))
      this.o.onBranch?.(this.o.link.root, h);
    if (!h) this.branchKey = "";
    return summary as T;
  }

  /**
   * The space's conditions with what the last run here found: an outcome
   * only for a condition as it is defined now (a redefined one starts
   * over), and none for a memory forgotten since.
   */
  view(): CheckView {
    const state = (this.state ??= this.readState());
    const items: CheckedItem[] = [];
    for (const it of this.conds?.conds.items ?? []) {
      if (this.gone.has(it.memory.toUpperCase())) continue;
      for (const c of it.conditions) {
        const r = c.condition.id
          ? state.results[`${it.memory}/${c.condition.id}`]
          : undefined;
        const item: CheckedItem = {
          memory: it.memory,
          condition: c.condition,
          armed: c.armed,
        };
        if (r && r.hash === c.hash) {
          item.outcome = r.outcome;
          const evidence = evidenceOf(r);
          if (evidence) item.evidence = evidence;
        }
        items.push(item);
      }
    }
    const skipped = this.lastRun?.skipped;
    return {
      root: this.o.link.root,
      space_id: this.o.link.space_id,
      commit: state.commit,
      checked_at: state.checked_at,
      ...(skipped && skipped in NOT_CHECKED
        ? { not_checked: NOT_CHECKED[skipped as NotChecked] }
        : {}),
      items,
      ...(this.branch ? { branch: this.branch } : {}),
    };
  }

  private writeState(f: CheckFile): void {
    this.state = f;
    mkdirSync(this.o.paths.checks, { recursive: true, mode: 0o700 });
    writeJsonAtomic(this.file, f);
  }
}

/** What a result says changed, as the API types it. */
function evidenceOf(r: Reported | undefined): V2.CheckEvidence | undefined {
  if (!r?.change) return undefined;
  return {
    change: r.change as V2.CheckEvidence["change"],
    ...(r.path ? { path: r.path } : {}),
    ...(r.renamed_to ? { renamed_to: r.renamed_to } : {}),
    ...(r.constraint ? { constraint: r.constraint } : {}),
  };
}

/**
 * A space's conditions as runs use them: each flat, with what it needs of
 * the tree, and a hash of the whole (a run with the same commit and
 * conditions has nothing new to say).
 */
function prepare(conds: V2.SpaceConditions): Prepared {
  const checks: Check[] = [];
  let everyKind = true;
  for (const it of conds.items) {
    for (const c of it.conditions) {
      // The evaluator reads a condition's kind and fields, and passes over
      // its id, origin and from.
      const cond = c.condition as CheckCondition & { id?: string };
      const id = cond.id;
      const needs = id ? checkNeeds(cond) : null;
      if (!id || !needs) {
        everyKind = false;
        continue;
      }
      const path = cond.path;
      checks.push({
        key: `${it.memory}/${id}`,
        ref: it.memory,
        id,
        hash: c.hash,
        cond,
        needs,
        ...(FOLLOWS.has(cond.kind) && path ? { path } : {}),
        armed: c.armed,
        ...(c.held_commit ? { held: c.held_commit } : {}),
      });
    }
  }
  return { conds, hash: sha(JSON.stringify(conds)), checks, everyKind };
}

/** An evaluation as the state keeps it. */
function reportedOf(hash: string, e: CheckEvaluation): Reported {
  const r: Reported = { hash, outcome: e.outcome };
  if (e.evidence) {
    r.change = e.evidence.change;
    if (e.evidence.path) r.path = e.evidence.path;
    if (e.evidence.renamed_to) r.renamed_to = e.evidence.renamed_to;
    if (e.evidence.constraint) r.constraint = e.evidence.constraint;
  }
  if (e.resolved) r.resolved = e.resolved;
  return r;
}

function withoutHeld(r: Reported): Reported {
  const { held: _held, ...rest } = r;
  void _held;
  return rest;
}

/**
 * The blobs a condition read, as one hash of their paths and ids: the same
 * blobs give the same result. Undefined when one isn't a blob git could
 * read (gone, a folder, couldn't say): that is evaluated again.
 */
function blobsSeen(
  files: readonly string[],
  entries: Entries,
): string | undefined {
  if (files.length === 0) return undefined;
  if (files.length === 1) {
    const e = entries.get(files[0]);
    return e?.type === "blob" ? e.id : undefined;
  }
  const parts: string[] = [];
  for (const f of files) {
    const e = entries.get(f);
    if (e?.type !== "blob") return undefined;
    parts.push(`${f}\0${e.id}`);
  }
  return sha(parts.join("\n")).slice(0, 40);
}

const isWordByte = (b: number) =>
  (b >= 48 && b <= 57) ||
  (b >= 65 && b <= 90) ||
  (b >= 97 && b <= 122) ||
  b === 95;

/**
 * containsText on a blob's bytes (the evaluator's whole-word rule, an ASCII
 * letter, digit or _ on neither side), without decoding the blob: for
 * UTF-8, the same answer.
 */
export function bytesContain(
  blob: Buffer,
  text: string,
  word: boolean,
): boolean {
  const needle = Buffer.from(text, "utf8");
  for (let i = blob.indexOf(needle); i >= 0; i = blob.indexOf(needle, i + 1)) {
    if (!word) return true;
    const before = i > 0 ? blob[i - 1] : -1;
    const after =
      i + needle.length < blob.length ? blob[i + needle.length] : -1;
    if (!isWordByte(before) && !isWordByte(after)) return true;
  }
  return false;
}

function sameResult(a: Reported | undefined, b: Reported): boolean {
  return (
    !!a &&
    a.hash === b.hash &&
    a.outcome === b.outcome &&
    a.change === b.change &&
    a.path === b.path &&
    a.renamed_to === b.renamed_to &&
    a.constraint === b.constraint
  );
}

function resultOf(c: Check, r: Reported): V2.CheckResultInput {
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
            ...(r.constraint ? { constraint: r.constraint } : {}),
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

/** What a report sent at once marked stale, and whose mark it took off. */
export interface Sent {
  flagged: string[];
  unflagged: string[];
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
   * Memories forgotten in a space (rule 7): the reports waiting to go
   * lose their results, now. A report left with others is rewritten under
   * its new key (it says less, so it is a new report); one only about
   * them is dropped.
   */
  forget(space: string, refs: string[]): void {
    const gone = new Set(refs.map((r) => r.toUpperCase()));
    let names: string[] = [];
    try {
      names = readdirSync(this.dir).filter((n) => n.endsWith(".json"));
    } catch {
      return;
    }
    for (const n of names) {
      const file = join(this.dir, n);
      const q = readJson<QueuedReport | null>(file, null);
      if (!q?.input || q.space !== space) continue;
      const results = q.input.results.filter(
        (r) => !gone.has(r.memory.toUpperCase()),
      );
      if (results.length === q.input.results.length) continue;
      remove(file);
      if (results.length === 0) continue;
      const input = { ...q.input, results };
      const key = reportKey(space, input);
      writeJsonAtomic(join(this.dir, `${key}.json`), { space, key, input });
    }
  }

  /**
   * Queues a report and sends it now: the memories it marked stale, or
   * undefined when it waits in the queue.
   */
  async report(
    space: string,
    input: V2.CheckRunInput,
  ): Promise<Sent | undefined> {
    const q: QueuedReport = { space, key: reportKey(space, input), input };
    mkdirSync(this.dir, { recursive: true, mode: 0o700 });
    writeJsonAtomic(join(this.dir, `${q.key}.json`), q);
    await this.flushing?.catch(() => {});
    return this.send(q);
  }

  /** Sends one report; deletes it unless it should be tried again. */
  private async send(q: QueuedReport): Promise<Sent | undefined> {
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
      // An older server answers without it.
      const unflagged = r.unflagged ?? [];
      if (unflagged.length)
        this.o.log.info("a check took memories' stale mark off", {
          unflagged: unflagged.join(" "),
          run: r.run.id,
        });
      return { flagged: r.flagged, unflagged };
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
