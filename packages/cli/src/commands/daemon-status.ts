// `memax daemon status`: whether the daemon runs, the repositories linked
// on this machine, where each target stands here, and the last delivery.
import chalk from "chalk";
import type { V2 } from "memax-sdk";
import { controlRequest } from "../lib/daemon/control.js";
import type { DaemonPaths } from "../lib/daemon/paths.js";
import { readRegistry } from "../lib/daemon/registry.js";
import type {
  DaemonSnapshot,
  LocalState,
  RepoSnapshot,
  TargetSnapshot,
} from "../lib/daemon/snapshot.js";
import { DeviceState } from "../lib/daemon/state.js";
import {
  clock,
  MARK,
  order,
  pad,
  syncMark,
  syncWord,
  tildify,
} from "./v2-output.js";

export interface DaemonStatus {
  running: boolean;
  pid?: number;
  started_at?: string;
  version?: string;
  repos: RepoSnapshot[];
}

/** From the running daemon, or else from the registry and the state file. */
export async function readDaemonStatus(
  paths: DaemonPaths,
): Promise<DaemonStatus> {
  const snap = (await controlRequest(
    paths,
    { cmd: "status" },
    2_000,
  )) as DaemonSnapshot | null;
  if (snap) {
    return {
      running: true,
      pid: snap.pid,
      started_at: snap.started_at,
      version: snap.version,
      repos: snap.repos,
    };
  }
  // Not running: what this machine last wrote, from the state file.
  const state = new DeviceState(paths);
  const repos = readRegistry(paths).map((l): RepoSnapshot => {
    const targets = Object.entries(state.targetsOf(l.root))
      .filter(([, t]) => t.compile)
      .map(
        ([id, t]): TargetSnapshot => ({
          id,
          kind: (t.kind ?? "agents_md") as V2.TargetKind,
          label: t.label ?? (Object.keys(t.files).join(", ") || id.slice(0, 8)),
          delivery: "local",
          sync_state: "in_sync",
          open_drift: 0,
          state: "in_sync",
          here: { compile: t.compile, at: t.acked_at },
          files: Object.keys(t.files).map((path) => ({
            path,
            state: "in_sync" as LocalState,
          })),
        }),
      )
      .sort((a, b) => a.label.localeCompare(b.label));
    return {
      root: l.root,
      space_id: l.space_id,
      space_slug: l.space_slug,
      targets,
    };
  });
  return { running: false, repos };
}

const WORD: Record<LocalState, string> = {
  in_sync: "in sync",
  pending: "writing",
  hand_edit: "drifted",
  missing: "missing",
  older: "older",
  blocked: "held",
  off: "off",
  off_block_removed: "off",
  off_block_kept: "off",
  remote: "",
  waiting: "waiting",
  error: "error",
};

function mark(t: TargetSnapshot): string {
  switch (t.state) {
    case "in_sync":
      return chalk.green(MARK.kept);
    case "hand_edit":
      return chalk.yellow(MARK.waiting);
    case "blocked":
    case "error":
      return chalk.red("✗");
    case "off":
    case "off_block_kept":
    case "off_block_removed":
      return chalk.gray(MARK.off);
    case "remote":
      return syncMark(t.sync_state);
    default:
      return chalk.gray(MARK.working);
  }
}

const REMOTE: Partial<Record<V2.Delivery, string>> = {
  mcp: "agents read it over MCP",
  copy: "copied out from the app",
  pr: "pull requests (not available yet)",
};

function detail(t: TargetSnapshot, now: Date): string {
  if (t.state === "remote") return REMOTE[t.delivery] ?? "";
  if (t.state === "in_sync" && t.here?.compile) {
    return [t.here.compile, clock(t.here.at, now)].filter(Boolean).join(" · ");
  }
  return t.detail ?? "";
}

export function renderDaemonStatus(
  s: DaemonStatus,
  now = new Date(),
): string[] {
  const lines: string[] = [""];
  if (s.running) {
    const since = s.started_at ? ` · since ${clock(s.started_at, now)}` : "";
    lines.push(
      `  ${chalk.green(MARK.kept)} The Memax daemon is running · pid ${s.pid}${since}`,
    );
  } else {
    lines.push(
      `  ${chalk.gray(MARK.off)} The Memax daemon isn't running. Start it: ${chalk.bold("memax daemon start")}`,
    );
  }
  if (s.repos.length === 0) {
    lines.push(
      "",
      chalk.gray(
        "  No repositories linked on this machine. Link one: memax link",
      ),
      "",
    );
    return lines;
  }
  for (const r of s.repos) {
    lines.push("");
    const polled = r.polled_at
      ? chalk.gray(` · checked ${clock(r.polled_at, now)}`)
      : "";
    lines.push(`  ${chalk.bold(tildify(r.root))} → ${r.space_slug}${polled}`);
    if (r.error) lines.push(chalk.yellow(`    ${r.error}`));
    if (r.targets.length === 0 && !s.running) {
      lines.push(chalk.gray("    Nothing written here yet."));
    }
    const width = Math.max(14, ...r.targets.map((t) => t.label.length + 2));
    for (const t of [...r.targets].sort(
      (a, b) => order(a) - order(b) || a.label.localeCompare(b.label),
    )) {
      const word =
        t.state === "remote" ? syncWord(t.sync_state) : WORD[t.state];
      const where = s.running
        ? detail(t, now)
        : t.here?.compile
          ? `last delivery ${t.here.compile} · ${clock(t.here.at, now)}`
          : "";
      lines.push(
        `    ${mark(t)} ${pad(t.label, width)}${pad(word, 10)}${chalk.gray(where)}`.trimEnd(),
      );
    }
  }
  lines.push("");
  return lines;
}
