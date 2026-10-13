// Delivering one run of one target to one repository: read the preview,
// judge every file against the disk, write only what is safe, take out
// files the run no longer writes, and acknowledge what is on disk.
import type { V2 } from "memax-sdk";
import { commandKey, type DaemonApi } from "./api.js";
import { driftHash, manifestSha256 } from "./compiler/drift.js";
import {
  commitWrite,
  readDisk,
  removeIfUnchanged,
  type Disk,
  type FsHooks,
} from "./fs-atomic.js";
import { heldPaths, holdDetail } from "./holds.js";
import type { KnownCompiles } from "./known.js";
import type { Logger } from "./log.js";
import {
  fileHash,
  judge,
  planFrom,
  settleUnknown,
  type Judgement,
  type Plan,
} from "./plan.js";
import type { Place } from "./place.js";
import type { HandEditReporter } from "./report.js";
import { UnsafePathError } from "./safe-path.js";
import type { FileSnapshot, LocalState } from "./snapshot.js";
import type { DeviceState } from "./state.js";
import { agent } from "../agents/agents.js";
import { capOf } from "../agents/loads.js";

export interface DeliverDeps {
  api: DaemonApi;
  state: DeviceState;
  log: Logger;
  deviceId: string;
  /** The place's root: where files go, and the key of its state. */
  root: string;
  /** Which files the target may write here, and how (place.ts). */
  place: Place;
  known: KnownCompiles;
  reporter: HandEditReporter;
  hooks?: FsHooks;
}

export interface DeliverResult {
  state: LocalState;
  detail?: string;
  files: FileSnapshot[];
  /** Try again later (an error, not a hand edit). */
  retry?: boolean;
}

/** The kinds whose file can be the person's, with one Memax block in it. */
const BLOCK_KINDS: ReadonlySet<V2.TargetKind> = new Set([
  "claude_md",
  "gemini_md",
  "claude_user",
  "codex_user",
  "gemini_user",
  "copilot_user",
  "windsurf_user",
]);

/**
 * A tool's limit on a whole file it reads, which the person's own lines
 * share with Memax's block: Windsurf's global rules stop at 6,000
 * characters. Past it the daemon writes nothing and says why.
 */
const FILE_CAPS: Partial<
  Record<V2.TargetKind, { chars: number; tool: string }>
> = {
  windsurf_user: {
    chars: capOf("windsurf", { user: "windsurf_global_rules" }).chars!,
    tool: agent("windsurf").name,
  },
};

/** Why a file can't be written whole within its tool's limit, or null. */
export function overFileCap(
  kind: V2.TargetKind,
  content: string,
): string | null {
  const cap = FILE_CAPS[kind];
  if (!cap || content.length <= cap.chars) return null;
  return `Over ${cap.tool}'s ${cap.chars.toLocaleString("en-US")}-character limit with your own rules (${content.length.toLocaleString("en-US")} characters). Lower the budget, or shorten your own rules.`;
}

/**
 * Whether the person owns the file and Memax manages one block in it: a
 * CLAUDE.md or GEMINI.md they keep, Codex's or Gemini's AGENTS.md at home, or the
 * ~/.claude/CLAUDE.md fallback (the server sets user_owned for those).
 */
export function userOwned(t: {
  kind: V2.TargetKind;
  settings: Pick<V2.TargetSettings, "user_owned">;
}): boolean {
  return BLOCK_KINDS.has(t.kind) && t.settings.user_owned === true;
}

/** The drift hashes Memax is known to have put at `path`, without asking. */
export function knownHashes(
  d: DeliverDeps,
  t: Pick<V2.Target, "id" | "delivered" | "last_compile">,
  path: string,
): Set<string> {
  const set = new Set(d.state.written(d.root, t.id, path));
  // The server's hash and the daemon's agree for every file it judges: a
  // file the person owns by its block on both sides, any other whole (one
  // with a block is held, see plan.ts). So an accepted hand edit's hash
  // is known as it is.
  const base = t.delivered?.files.find((f) => f.path === path);
  if (base) set.add(base.sha256);
  const latest = t.last_compile?.files.find(
    (f) => f.path === path,
  )?.drift_sha256;
  if (latest) set.add(latest);
  return set;
}

export function hasBaseline(
  d: DeliverDeps,
  t: V2.Target,
  path: string,
): boolean {
  // Memax took its version back out (a stopped target, an unlinked or
  // signed-out machine): the next delivery adds it afresh.
  if (d.state.peek(d.root, t.id)?.files[path]?.taken_out_at) return false;
  return (
    d.state.written(d.root, t.id, path).length > 0 ||
    (t.delivered?.files ?? []).some((f) => f.path === path)
  );
}

/**
 * Judges the file at `path` against `latest` (this run's drift hash),
 * asking the target's runs about content it doesn't know. Null when the
 * runs couldn't be read: then nothing is written and nothing reported.
 */
export async function judgeFile(
  d: DeliverDeps,
  t: V2.Target,
  path: string,
  disk: Disk,
  latest: string,
): Promise<Exclude<Judgement, { kind: "unknown" }> | null> {
  const j = judge({
    latest,
    userOwned: userOwned(t),
    disk,
    known: knownHashes(d, t, path),
    hasBaseline: hasBaseline(d, t, path),
  });
  if (j.kind !== "unknown") return j;
  const known = await d.known.has(t.id, path, j.hash);
  return known === null
    ? null
    : (settleUnknown(j, known) as Exclude<Judgement, { kind: "unknown" }>);
}

interface Planned {
  out: V2.PreviewOutput & { path: string };
  abs: string;
  disk: Disk;
  plan: Plan;
}

const blocked = (
  detail: string,
  files: FileSnapshot[] = [],
): DeliverResult => ({
  state: "blocked",
  detail,
  files,
  retry: true,
});

export async function deliverRun(
  d: DeliverDeps,
  t: V2.Target,
  p: V2.TargetPreview,
): Promise<DeliverResult> {
  const run = p.compile!;
  const owned = userOwned(t);
  // Never write a file a pull holds, whatever the rest of the run says.
  // The preview's target is the freshest word on it.
  const held = new Set([...heldPaths(t), ...heldPaths(p.target)]);
  if (p.files.some((f) => f.path && held.has(f.path))) {
    return { state: "held", detail: holdDetail(p.target), files: [] };
  }
  const files: Planned[] = [];
  for (const out of p.files) {
    const refusal = out.path ? d.place.refusal(t, out.path) : "has no path";
    if (refusal) {
      d.log.error("refused a compiled file", {
        target: t.label,
        path: out.path,
        reason: refusal,
      });
      return blocked(`${out.path ?? "a file"} ${refusal}`);
    }
    const path = out.path!;
    // At home: the agent's folder gone, a dotfiles repository, a link.
    const now = await d.place.unwritable(t, path);
    if (now) {
      d.log.warn("left a file alone", { target: t.label, path, reason: now });
      return blocked(now);
    }
    let abs: string;
    try {
      abs = await d.place.resolve(path);
    } catch (err) {
      if (!(err instanceof UnsafePathError)) throw err;
      d.log.error("refused a path outside the repository", {
        target: t.label,
        path,
      });
      return blocked(err.message);
    }
    const disk = await readDisk(abs);
    const j = await judgeFile(d, t, path, disk, out.drift_sha256);
    if (!j)
      return {
        state: "error",
        detail: "couldn't read the target's runs",
        files: [],
        retry: true,
      };
    files.push({
      out: { ...out, path },
      abs,
      disk,
      plan: planFrom(j, out, owned, disk),
    });
  }

  const skipped = files.find((f) => f.plan.kind === "skip");
  if (skipped?.plan.kind === "skip") {
    return {
      state: "blocked",
      detail: `${skipped.out.path} ${skipped.plan.reason}`,
      files: snapshotOf(files),
    };
  }
  if (files.some((f) => f.plan.kind === "hand_edit")) {
    for (const f of files) {
      if (f.plan.kind === "hand_edit" && f.disk.kind === "file") {
        await d.reporter.report(
          t,
          f.out.path,
          f.disk.content,
          f.plan.hash,
          owned,
        );
      }
    }
    return handEdit(files);
  }

  // Within the tool's limit on the whole file, the person's lines included.
  for (const f of files) {
    if (f.plan.kind !== "write") continue;
    const over = overFileCap(t.kind, f.plan.content);
    if (over) {
      d.log.warn("left a file alone", { target: t.label, reason: over });
      return blocked(`${f.out.path}: ${over}`, snapshotOf(files));
    }
  }

  // Every file is safe. Check the hashes first: never write what can't be
  // acknowledged.
  const manifest: Record<string, string> = {};
  for (const f of files) {
    const after =
      f.plan.kind === "write" ? driftHash(f.plan.content) : f.out.drift_sha256;
    if (after !== f.out.drift_sha256) {
      d.log.error("a compiled file's hash doesn't match; update memax-cli", {
        target: t.label,
        path: f.out.path,
      });
      return {
        state: "error",
        detail: "the compiled file's hash doesn't match; update memax-cli",
        files: [],
        retry: true,
      };
    }
    manifest[f.out.path] = after;
  }
  const sha = manifestSha256(manifest);
  if (sha !== run.drift_sha256) {
    d.log.error("a run's hash doesn't match its files; update memax-cli", {
      target: t.label,
      compile: run.ref,
    });
    return {
      state: "error",
      detail: "the run's hash doesn't match its files",
      files: [],
      retry: true,
    };
  }

  let written = 0;
  for (const f of files) {
    if (f.plan.kind !== "write") continue;
    await d.place.ensureParents(f.out.path);
    const r = await commitWrite(f.abs, f.plan.content, f.plan.expect, {
      mode:
        f.plan.mode ??
        (f.disk.kind === "absent" ? d.place.newFileMode : undefined),
      hooks: d.hooks,
    });
    if (r === "changed") {
      // The file changed between the read and the write (a person saved):
      // whatever is there now stays. Judge it afresh.
      d.log.info("held a write: the file changed while Memax was writing", {
        target: t.label,
        path: f.out.path,
      });
      const disk = await readDisk(f.abs);
      const again = await judgeFile(d, t, f.out.path, disk, f.out.drift_sha256);
      if (again?.kind === "hand_edit" && disk.kind === "file") {
        await d.reporter.report(t, f.out.path, disk.content, again.hash, owned);
        f.plan = again;
        return handEdit(files);
      }
      return {
        state: "pending",
        detail: "a file changed while Memax was writing",
        files: [],
        retry: true,
      };
    }
    d.state.wrote(
      d.root,
      t.id,
      f.out.path,
      f.out.drift_sha256,
      new Date(),
      f.disk.kind === "absent",
    );
    written++;
  }
  await removeDropped(d, t, new Set(files.map((f) => f.out.path)));

  const key = commandKey("dlv", d.deviceId, t.id, t.version, run.ref, sha);
  await d.api.deliver(t.id, { compile: run.ref, sha256: sha }, key);
  d.state.acked(d.root, t.id, run.ref, { kind: t.kind, label: t.label });
  if (written > 0) {
    d.log.info("delivered", {
      target: t.label,
      compile: run.ref,
      files: files.length,
      written,
    });
  }
  return {
    state: "in_sync",
    files: files.map((f) => ({ path: f.out.path, state: "in_sync" })),
  };
}

function handEdit(files: Planned[]): DeliverResult {
  const n = files.filter((f) => f.plan.kind === "hand_edit").length;
  return {
    state: "hand_edit",
    detail: n === 1 ? "1 local edit" : `${n} local edits`,
    files: snapshotOf(files),
  };
}

function snapshotOf(files: Planned[]): FileSnapshot[] {
  const state: Record<Plan["kind"], LocalState> = {
    hand_edit: "hand_edit",
    skip: "blocked",
    current: "in_sync",
    write: "pending",
  };
  return files.map((f) => ({
    path: f.out.path,
    state: state[f.plan.kind],
    detail: f.plan.kind === "skip" ? f.plan.reason : undefined,
  }));
}

/**
 * Takes out files an earlier run wrote and this one doesn't (a scoped rule
 * whose facts were forgotten), but only while they hold exactly what
 * Memax wrote. A file with a hand edit stays.
 */
async function removeDropped(
  d: DeliverDeps,
  t: V2.Target,
  current: Set<string>,
): Promise<void> {
  const before = new Set([
    ...Object.keys(d.state.peek(d.root, t.id)?.files ?? {}),
    ...(t.delivered?.files ?? []).map((f) => f.path),
  ]);
  for (const path of before) {
    if (current.has(path) || d.place.refusal(t, path)) continue;
    let abs: string;
    try {
      abs = await d.place.resolve(path);
    } catch {
      continue;
    }
    const disk = await readDisk(abs);
    if (
      disk.kind === "file" &&
      knownHashes(d, t, path).has(fileHash(disk.content, false))
    ) {
      if ((await removeIfUnchanged(abs, disk.content)) === "removed") {
        d.log.info("removed a file the target no longer writes", {
          target: t.label,
          path,
        });
      }
    } else if (disk.kind === "file") {
      d.log.info(
        "left a file the target no longer writes: it has a hand edit",
        { target: t.label, path },
      );
    }
    d.state.dropFile(d.root, t.id, path);
  }
}
