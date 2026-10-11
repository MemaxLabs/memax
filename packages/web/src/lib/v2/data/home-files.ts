/**
 * A personal space's home files, as people read them (personal-layer
 * design §8): the server keeps one target per file per linked machine
 * (`~/.codex/AGENTS.md` on work-mbp, and again on home-linux), and the
 * screens group them by file, each with the machines it's on and how it
 * stands on each. Pure: targets and machines in, groups, states and
 * counts out; the catalogue words them (machines-copy.ts).
 */
import { isOnline, machineName, type MachineView } from "./machines";
import {
  isHome,
  syncLineOf,
  targetStatus,
  type TargetKind,
  type TargetMachineRef,
  type TargetView,
} from "./targets";
import type { SpaceKind, SyncLine } from "./types";

/** One home file across the machines it's written on. */
export interface HomeFileGroup {
  /** Stable across renders: the kind and the path. */
  key: string;
  kind: TargetKind;
  /** Relative to the home folder (".codex/AGENTS.md"). */
  path: string | null;
  /** What people see: "~/.codex/AGENTS.md". */
  label: string;
  /** The agent it's written for (registry key); null for the canonical copy. */
  agent: string | null;
  /** One per machine: linked machines first, each by number. */
  targets: TargetView[];
}

/** A row of a list of compiled files: a repository's file, or a home file across machines. */
export type TargetEntry =
  | { kind: "repo"; target: TargetView }
  | { kind: "home"; group: HomeFileGroup };

/**
 * Where each agent with a home adapter reads a Personal space by default
 * (personal-layer design §3.2): Claude Code, Codex, and Gemini's file,
 * which Antigravity reads (and Gemini CLI once it reads AGENTS.md). A
 * machine may resolve another path (CLAUDE_CONFIG_DIR, CODEX_HOME,
 * GEMINI_CLI_HOME).
 */
export const HOME_FILE_OF_AGENT: Readonly<Record<string, string>> = {
  "claude-code": "~/.claude/rules/memax-personal.md",
  codex: "~/.codex/AGENTS.md",
  gemini: "~/.gemini/AGENTS.md",
};

/** The agent a home kind is written for; the canonical copy (agents_md) is for people and agents with no file. */
export function homeAgent(kind: TargetKind): string | null {
  if (kind === "claude_user") return "claude-code";
  if (kind === "codex_user") return "codex";
  if (kind === "gemini_user") return "gemini";
  return null;
}

/** Whether Memax writes one block in a file the person owns (Codex's or Gemini's AGENTS.md), rather than the whole file. */
export function isBlock(target: Pick<TargetView, "kind" | "path">): boolean {
  if (target.kind === "codex_user" || target.kind === "gemini_user")
    return true;
  // Claude Code's fallback for an old Claude Code: a block in ~/.claude/CLAUDE.md.
  return target.kind === "claude_user" && /CLAUDE\.md$/.test(target.path ?? "");
}

const machineOrder = (t: TargetView) =>
  Number((t.machine?.label ?? "").replace(/^machine-/, "")) || 0;

/**
 * The list as people read it: a repository's files as they are, and each
 * home file once, where its first machine's copy was, with every
 * machine's copy in it.
 */
export function groupTargets(targets: readonly TargetView[]): TargetEntry[] {
  const out: TargetEntry[] = [];
  const groups = new Map<string, HomeFileGroup>();
  for (const target of targets) {
    if (!isHome(target)) {
      out.push({ kind: "repo", target });
      continue;
    }
    const key = `${target.kind}:${target.path ?? ""}`;
    const found = groups.get(key);
    if (found) {
      found.targets.push(target);
      continue;
    }
    const group: HomeFileGroup = {
      key,
      kind: target.kind,
      path: target.path,
      label: target.label,
      agent: homeAgent(target.kind),
      targets: [target],
    };
    groups.set(key, group);
    out.push({ kind: "home", group });
  }
  for (const group of groups.values()) {
    group.targets.sort((a, b) => machineOrder(a) - machineOrder(b));
  }
  return out;
}

/** The home files alone, grouped. */
export function homeGroups(targets: readonly TargetView[]): HomeFileGroup[] {
  return groupTargets(targets).flatMap((e) =>
    e.kind === "home" ? [e.group] : [],
  );
}

/** The other machines' copies of a home target's file, itself included, in machine order. */
export function siblingsOf(
  target: TargetView,
  all: readonly TargetView[],
): TargetView[] {
  if (!isHome(target)) return [target];
  return (
    homeGroups(all).find((g) => g.targets.some((t) => t.id === target.id))
      ?.targets ?? [target]
  );
}

/** The machines a screen can name: the list when it loaded, else what the targets say of theirs. */
export function knownMachines(
  targets: readonly TargetView[],
  machines: readonly MachineView[] | null | undefined,
): readonly TargetMachineRef[] {
  if (machines) return machines;
  const seen = new Map<string, TargetMachineRef>();
  for (const t of targets) {
    if (t.machine && !seen.has(t.machine.id)) seen.set(t.machine.id, t.machine);
  }
  return [...seen.values()];
}

/** How one machine's copy of a home file stands. */
export type MachineFileState =
  | { kind: "in_sync" }
  | { kind: "compiling" }
  /** Compiled, and the machine is online: its daemon writes it shortly. */
  | { kind: "waiting" }
  /** Compiled, but the machine hasn't been online since `since` (null: never yet). */
  | { kind: "offline"; since: string | null }
  | { kind: "drifted"; edits: number }
  | { kind: "held"; proposals: number }
  /** Stopped on this machine; the machine is still linked. */
  | { kind: "stopped" }
  /** The machine was unlinked (`since`), so every file of it stopped. */
  | { kind: "unlinked"; since: string | null };

export function machineFileState(
  target: TargetView,
  machine: MachineView | undefined,
  now: Date,
): MachineFileState {
  if (machine && !machine.linked) {
    return { kind: "unlinked", since: machine.unlinkedAt };
  }
  const status = targetStatus(target);
  switch (status.kind) {
    case "off":
      return { kind: "stopped" };
    case "drifted":
      return { kind: "drifted", edits: status.edits };
    case "held":
      return { kind: "held", proposals: status.proposals };
    case "compiling":
      return { kind: "compiling" };
    case "pending":
      // Without the machine (its list didn't load), say only that it waits.
      return machine && !isOnline(machine, now)
        ? { kind: "offline", since: machine.lastSeenAt }
        : { kind: "waiting" };
    default:
      return { kind: "in_sync" };
  }
}

/** Whether a state is a copy Memax still writes: not stopped, not on an unlinked machine. */
export function isLive(state: MachineFileState): boolean {
  return state.kind !== "stopped" && state.kind !== "unlinked";
}

/** One machine's copy, as a group's switcher and status read it. */
export interface MachineCopy {
  target: TargetView;
  machine: MachineView | undefined;
  /** What the person calls the machine, its label added where two share a name. */
  name: string;
  state: MachineFileState;
}

export function machineCopies(
  group: HomeFileGroup,
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): MachineCopy[] {
  const named = knownMachines(group.targets, machines);
  return group.targets.map((target) => {
    const machine = machines?.find((m) => m.id === target.machine?.id);
    const ref = target.machine ?? { id: target.id, label: "", name: "" };
    return {
      target,
      machine,
      name: machineName(machine ?? ref, named),
      state: machineFileState(target, machine, now),
    };
  });
}

/** How a home file stands across its machines (a list row's status). */
export type HomeFileStatus =
  /** On every machine it's written to; `machine` names the one when there's one. */
  | { kind: "in_sync"; machines: number; machine: string | null }
  | { kind: "drifted"; machines: string[] }
  | { kind: "held"; machines: string[]; proposals: number }
  | { kind: "compiling" }
  /**
   * The latest compile is on `reached` of `of` machines. `offline` names a
   * machine that's behind because it hasn't been online; otherwise
   * `waiting` names one whose daemon hasn't written it yet.
   */
  | {
      kind: "behind";
      reached: number;
      of: number;
      offline: { machine: string; since: string | null } | null;
      waiting: string | null;
    }
  /** Stopped on every machine, or every machine it was on is unlinked. */
  | { kind: "off" };

export function homeFileStatus(
  group: HomeFileGroup,
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): HomeFileStatus {
  const live = machineCopies(group, machines, now).filter((c) =>
    isLive(c.state),
  );
  if (live.length === 0) return { kind: "off" };
  const drifted = live.filter((c) => c.state.kind === "drifted");
  if (drifted.length > 0) {
    return { kind: "drifted", machines: drifted.map((c) => c.name) };
  }
  const held = live.filter((c) => c.state.kind === "held");
  if (held.length > 0) {
    const refs = new Set(held.flatMap((c) => c.target.holding));
    return {
      kind: "held",
      machines: held.map((c) => c.name),
      proposals: Math.max(1, refs.size),
    };
  }
  if (live.some((c) => c.state.kind === "compiling")) {
    return { kind: "compiling" };
  }
  const reached = live.filter((c) => c.state.kind === "in_sync").length;
  if (reached === live.length) {
    return {
      kind: "in_sync",
      machines: live.length,
      machine: live.length === 1 ? live[0]!.name : null,
    };
  }
  const offline = live.find((c) => c.state.kind === "offline");
  const waiting = live.find((c) => c.state.kind === "waiting");
  return {
    kind: "behind",
    reached,
    of: live.length,
    offline:
      offline && offline.state.kind === "offline"
        ? { machine: offline.name, since: offline.state.since }
        : null,
    waiting: offline ? null : (waiting?.name ?? null),
  };
}

/**
 * The machine's copy a group's row opens: one that needs the person (a
 * hand edit, then a hold), else the first machine it's written to.
 */
export function primaryTarget(
  group: HomeFileGroup,
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): TargetView {
  const copies = machineCopies(group, machines, now);
  return (
    copies.find((c) => c.state.kind === "drifted") ??
    copies.find((c) => c.state.kind === "held") ??
    copies.find((c) => isLive(c.state)) ??
    copies[0]!
  ).target;
}

/**
 * The status line for a personal space's home files (plan §6.4, design
 * §8): a hand edit first ("Hand edit on work-mbp"), then a hold, then a
 * compile running, then how many machines have the latest compile ("In
 * sync on 2 machines", "On 1 of 2 machines"). A machine counts as in sync
 * when every file it holds is. Null when nothing is written to any
 * machine.
 */
export function homeSyncLine(
  targets: readonly TargetView[],
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): SyncLine | null {
  const copies = homeGroups(targets)
    .flatMap((g) => machineCopies(g, machines, now))
    .filter((c) => isLive(c.state));
  if (copies.length === 0) return null;
  const drifted = copies.find((c) => c.state.kind === "drifted");
  if (drifted) return { kind: "machine-drifted", machine: drifted.name };
  const held = copies.find((c) => c.state.kind === "held");
  if (held) return { kind: "machine-held", machine: held.name };
  if (copies.some((c) => c.state.kind === "compiling")) {
    return { kind: "compiling" };
  }
  const byMachine = new Map<string, boolean>();
  for (const c of copies) {
    const id = c.target.machine?.id ?? c.target.id;
    byMachine.set(
      id,
      (byMachine.get(id) ?? true) && c.state.kind === "in_sync",
    );
  }
  const of = byMachine.size;
  const reached = [...byMachine.values()].filter(Boolean).length;
  return reached === of
    ? { kind: "machines-in-sync", machines: of }
    : { kind: "machines-behind", reached, of };
}

const ATTENTION = new Set<SyncLine["kind"]>([
  "drifted",
  "held",
  "machine-drifted",
  "machine-held",
]);

/**
 * The space's status line from all its targets: a repository's files
 * (syncLineOf) and a personal space's machines (homeSyncLine), whatever
 * needs a person first. A personal space with nothing written anywhere
 * says it isn't on any machine yet.
 */
export function spaceSyncLine(
  space: { kind: SpaceKind },
  targets: readonly TargetView[],
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): SyncLine | null {
  const repo = syncLineOf(targets);
  const home = homeSyncLine(targets, machines, now);
  if (repo && ATTENTION.has(repo.kind)) return repo;
  if (home && ATTENTION.has(home.kind)) return home;
  if (repo?.kind === "compiling" || home?.kind === "compiling") {
    return { kind: "compiling" };
  }
  if (repo && home) {
    if (repo.kind !== "files-in-sync") return repo;
    if (home.kind !== "machines-in-sync") return home;
    return {
      kind: "files-in-sync",
      files: repo.files + homeGroups(liveHome(targets)).length,
    };
  }
  const line = repo ?? home;
  if (line) return line;
  if (space.kind !== "personal") return null;
  // A machine linked with every file stopped has nothing to call in sync;
  // with no machine linked (or every one unlinked), it's on none.
  const linked = machines
    ? machines.some((m) => m.linked)
    : targets.some(isHome);
  return linked ? null : { kind: "no-machines" };
}

function liveHome(targets: readonly TargetView[]): TargetView[] {
  return targets.filter((t) => isHome(t) && t.syncState !== "off");
}

/**
 * The overview's file counts: each repository file once, and each home
 * file once whatever the number of machines it's on, in sync when it is
 * on every one.
 */
export function targetCounts(
  targets: readonly TargetView[],
  machines: readonly MachineView[] | null | undefined,
  now: Date,
): { total: number; inSync: number } {
  let total = 0;
  let inSync = 0;
  for (const entry of groupTargets(targets)) {
    if (entry.kind === "repo") {
      if (entry.target.syncState === "off") continue;
      total += 1;
      if (targetStatus(entry.target).kind === "in_sync") inSync += 1;
      continue;
    }
    const status = homeFileStatus(entry.group, machines, now);
    if (status.kind === "off") continue;
    total += 1;
    if (status.kind === "in_sync") inSync += 1;
  }
  return { total, inSync };
}

/**
 * How far a memory reaches through the targets that hold it: files
 * counted once (a home file once across its machines) and the machines
 * those home files are on (none for a repository's files).
 */
export function reachOf(reached: readonly TargetView[]): {
  files: number;
  machines: number;
} {
  const entries = groupTargets(reached);
  const machines = new Set(
    reached.flatMap((t) => (isHome(t) && t.machine ? [t.machine.id] : [])),
  );
  return { files: entries.length, machines: machines.size };
}

/**
 * The agents that read a machine's home files, in the order its files
 * list them (each file's read_by, or the agent it's written for).
 */
export function machineAgents(
  machine: { id: string },
  targets: readonly TargetView[],
): string[] {
  const agents = new Set<string>();
  for (const t of targets) {
    if (!isHome(t) || t.machine?.id !== machine.id || t.syncState === "off") {
      continue;
    }
    const own = homeAgent(t.kind);
    const readers = t.readBy.length ? t.readBy : own ? [own] : [];
    for (const agent of readers) agents.add(agent);
  }
  return [...agents];
}
