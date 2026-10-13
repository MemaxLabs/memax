// memax status: where the space stands, as on the Cli board. The space,
// how much is kept and waiting, each target's sync state, and what each
// connected agent may do here; and, when this machine's home is linked
// (memax link --home), its home files. Outside a repository, the
// Personal space the home link names.
import chalk from "chalk";
import type { Command } from "commander";
import type { Memax, V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { homeTargets, repoTargets } from "../lib/daemon/api.js";
import { controlRequest } from "../lib/daemon/control.js";
import { readHomeLink } from "../lib/daemon/home-link.js";
import { movedFiles } from "../lib/home/files.js";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { findLinkedRepo } from "../lib/daemon/registry.js";
import { resolveSpace, SpaceChoiceError } from "../lib/v2-space.js";
import {
  agentMark,
  apiFailureMessage,
  MARK,
  onV1Line,
  targetRows,
} from "./v2-output.js";

/** Kept memories are counted page by page, up to this many. */
const KEPT_PAGES = 3;
const PAGE = 200;

export interface StatusReport {
  space: Pick<V2.Space, "id" | "slug" | "name" | "kind" | "role"> & {
    /** False on V1, where V1's sync writes its files and Memax doesn't. */
    on_v2: boolean;
  };
  /** Null when it couldn't be counted; `kept_more` when it stopped counting. */
  kept: number | null;
  kept_more: boolean;
  waiting: number | null;
  targets: Array<{
    id: string;
    kind: V2.TargetKind;
    label: string;
    path?: string;
    delivery: V2.Delivery;
    sync_state: V2.SyncState;
    open_drift: number;
    compile?: string;
    delivered_at?: string;
  }>;
  agents: Array<{
    id: string;
    agent: V2.AgentKind;
    mark: string;
    name: string;
    state: V2.AgentState;
    autonomy: V2.Autonomy | null;
  }>;
  daemon: { running: boolean; linked_here: boolean };
  /** This machine's home link, when there is one. */
  home?: {
    space: string;
    machine_id: string;
    /** Null when Memax couldn't be asked. */
    machine: Pick<V2.Machine, "label" | "name" | "linked"> | null;
    targets: StatusReport["targets"];
    /** An agent reads another file than the one linked (Codex's override). */
    moved: string[];
    /** The Personal space answered 404 for this session: off this machine. */
    gone: boolean;
    /** False once the Personal space is back on V1: its files stay as they are. */
    on_v2: boolean;
  };
}

function targetRow(t: V2.Target): StatusReport["targets"][number] {
  return {
    id: t.id,
    kind: t.kind,
    label: t.label,
    path: t.path,
    delivery: t.delivery,
    sync_state: t.sync_state,
    open_drift: t.open_drift,
    compile: t.last_compile?.ref,
    delivered_at: t.delivered?.at,
  };
}

async function countKept(
  memax: Memax,
  space: string,
): Promise<{ n: number; more: boolean }> {
  let n = 0;
  let cursor: string | undefined;
  for (let page = 0; page < KEPT_PAGES; page++) {
    const p = await memax.v2.memories.list(space, {
      state: "kept",
      limit: PAGE,
      cursor,
    });
    n += p.items.length;
    if (!p.has_more || !p.next_cursor) return { n, more: false };
    cursor = p.next_cursor;
  }
  return { n, more: true };
}

const soft = <T>(p: Promise<T>): Promise<T | null> => p.catch(() => null);

export async function buildStatus(
  memax: Memax,
  o: { space?: string; cwd: string; paths: DaemonPaths },
): Promise<{
  report: StatusReport;
  targets: V2.Target[];
  homeTargets: V2.Target[];
}> {
  const link = readHomeLink(o.paths);
  let gone = false;
  const { space, root } = await resolveSpace(memax, {
    space: o.space,
    cwd: o.cwd,
    paths: o.paths,
  }).catch((err) => {
    if (!(err instanceof SpaceChoiceError) || o.space || !link) throw err;
    return resolveSpace(memax, { space: link.space_id, paths: o.paths });
  });
  const [all, kept, review, agents, daemon, mine, machines] = await Promise.all(
    [
      memax.v2.targets.list(space.id),
      soft(countKept(memax, space.id)),
      soft(memax.v2.review.list(space.id, { limit: 1 })),
      soft(memax.v2.agents.listInSpace(space.id)),
      controlRequest(o.paths, { cmd: "status" }, 1_000),
      link
        ? memax.v2.targets
            .list(link.space_id, { machine: link.machine_id })
            .catch((err: { status?: number; code?: string }) => {
              gone = err?.status === 404 && err?.code === "not_found";
              return null;
            })
        : null,
      link ? soft(memax.v2.machines.list(link.space_id)) : null,
    ],
  );
  // A repository's targets; this machine's home files are their own section.
  const targets = { items: repoTargets(all.items) };
  const home = link
    ? homeTargets(mine?.items ?? [], link.machine_id)
    : ([] as V2.Target[]);
  const machine = link
    ? (machines?.items.find((m) => m.id === link.machine_id) ?? null)
    : null;
  const report: StatusReport = {
    space: {
      id: space.id,
      slug: space.slug,
      name: space.name,
      kind: space.kind,
      role: space.role,
      on_v2: all.on_v2 !== false,
    },
    kept: kept?.n ?? null,
    kept_more: kept?.more ?? false,
    waiting: review?.total ?? null,
    targets: targets.items.map(targetRow),
    agents: (agents?.items ?? [])
      .filter((a) => a.state !== "disconnected")
      .map((a) => ({
        id: a.id,
        agent: a.agent,
        mark: agentMark(a.agent, a.display_name),
        name: a.display_name,
        state: a.state,
        autonomy:
          a.spaces.find((s) => s.space_id === space.id)?.autonomy ?? null,
      })),
    daemon: {
      running: daemon !== null,
      linked_here:
        !!root && findLinkedRepo(o.paths, root)?.space_id === space.id,
    },
    ...(link
      ? {
          home: {
            space: link.space_slug,
            machine_id: link.machine_id,
            machine: machine
              ? {
                  label: machine.label,
                  name: machine.name,
                  linked: machine.linked,
                }
              : null,
            targets: home.map(targetRow),
            moved: movedFiles(link.home, link.files),
            gone,
            on_v2: mine?.on_v2 !== false,
          },
        }
      : {}),
  };
  return { report, targets: targets.items, homeTargets: home };
}

const KIND: Record<V2.SpaceKind, string> = {
  personal: "Personal",
  project: "Project",
  team: "Team",
};

export function renderStatus(
  r: StatusReport,
  targets: V2.Target[],
  now = new Date(),
  home: V2.Target[] = [],
): string[] {
  const head = [chalk.bold(r.space.slug), KIND[r.space.kind]];
  if (r.kept !== null) head.push(`${r.kept}${r.kept_more ? "+" : ""} kept`);
  if (r.waiting !== null)
    head.push(
      r.waiting > 0
        ? chalk.yellow(`${r.waiting} waiting on you`)
        : "nothing waiting",
    );
  const lines = ["", `  ${head.join(" · ")}`, ""];
  const onV2 = r.space.on_v2;
  if (!onV2) {
    lines.push(onV1Line(r.space));
  } else if (targets.length === 0) {
    lines.push(
      chalk.gray(
        "  No targets yet: add AGENTS.md and the rest from the space's Brief in the app.",
      ),
    );
  } else {
    lines.push(...targetRows(targets, now));
  }
  const agents = r.agents.map(
    (a) =>
      `${a.mark} ${a.state === "paused" ? "paused" : (a.autonomy ?? "read")}`,
  );
  if (agents.length > 0) lines.push("", chalk.gray(`  ${agents.join(" · ")}`));
  if (r.home) lines.push("", ...renderHome(r.home, home, now));
  // Nothing to link or start for files V1 writes.
  const local =
    onV2 &&
    targets.some((t) => t.delivery === "local" && t.sync_state !== "off");
  const homeLive = !!r.home?.on_v2 && home.some((t) => t.sync_state !== "off");
  if (local && !r.daemon.linked_here && r.space.kind !== "personal") {
    lines.push(
      "",
      `  ${chalk.yellow(MARK.waiting)} This repository isn't linked, so nothing is written here: ${chalk.bold("memax link")}`,
    );
  } else if ((local || homeLive) && !r.daemon.running) {
    lines.push(
      "",
      `  ${chalk.yellow(MARK.waiting)} The daemon isn't running, so files wait to be written: ${chalk.bold("memax daemon start")}`,
    );
  }
  lines.push("");
  return lines;
}

/** This machine's home files (memax link --home). */
function renderHome(
  h: NonNullable<StatusReport["home"]>,
  targets: V2.Target[],
  now: Date,
): string[] {
  const who = h.machine ? ` · ${h.machine.label} (${h.machine.name})` : "";
  const lines = [`  This machine's home → ${chalk.bold(h.space)}${who}`];
  if (h.gone) {
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} Your Personal space isn't there for this account, so its files are off this machine. Forget the link: ${chalk.bold("memax unlink --home")}`,
    );
    return lines;
  }
  if (!h.on_v2) {
    lines.push(onV1Line({ slug: h.space, role: "owner" }));
    return lines;
  }
  if (h.machine && !h.machine.linked) {
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} It was unlinked, so nothing is written here. Link it again: ${chalk.bold("memax link --home")}`,
    );
    return lines;
  }
  if (targets.length === 0) {
    lines.push(
      chalk.gray(
        h.machine === null
          ? "  Memax couldn't be asked about its files just now."
          : "  No home files on this machine yet.",
      ),
    );
    return lines;
  }
  return [
    ...lines,
    ...targetRows(targets, now),
    ...h.moved.map((m) => `  ${chalk.yellow(MARK.waiting)} ${m}`),
  ];
}

export function registerStatusCommand(program: Command): void {
  program
    .command("status")
    .description(
      "The space, its compiled files and its agents, and what's waiting on you",
    )
    .option("--space <slug>", "The space (default: this repository's)")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: { space?: string; format?: string }) => {
      const memax = getClient();
      try {
        const { report, targets, homeTargets } = await buildStatus(memax, {
          space: opts.space,
          cwd: process.cwd(),
          paths: daemonPaths(),
        });
        if (opts.format === "json") {
          console.log(JSON.stringify(report, null, 2));
          return;
        }
        for (const line of renderStatus(
          report,
          targets,
          new Date(),
          homeTargets,
        ))
          console.log(line);
      } catch (err) {
        console.error(
          chalk.red(
            `  ${err instanceof SpaceChoiceError ? err.message : apiFailureMessage(err)}`,
          ),
        );
        process.exitCode = 1;
      }
    });
}
