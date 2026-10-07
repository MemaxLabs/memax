// memax status: where the space stands, as on the Cli board. The space,
// how much is kept and waiting, each target's sync state, and what each
// connected agent may do here.
import chalk from "chalk";
import type { Command } from "commander";
import type { Memax, V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { controlRequest } from "../lib/daemon/control.js";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { findLinkedRepo } from "../lib/daemon/registry.js";
import { resolveSpace, SpaceChoiceError } from "../lib/v2-space.js";
import { agentMark, apiFailureMessage, MARK, targetRows } from "./v2-output.js";

/** Kept memories are counted page by page, up to this many. */
const KEPT_PAGES = 3;
const PAGE = 200;

export interface StatusReport {
  space: Pick<V2.Space, "id" | "slug" | "name" | "kind" | "role">;
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
): Promise<{ report: StatusReport; targets: V2.Target[] }> {
  const { space, root } = await resolveSpace(memax, {
    space: o.space,
    cwd: o.cwd,
    paths: o.paths,
  });
  const [targets, kept, review, agents, daemon] = await Promise.all([
    memax.v2.targets.list(space.id),
    soft(countKept(memax, space.id)),
    soft(memax.v2.review.list(space.id, { limit: 1 })),
    soft(memax.v2.agents.listInSpace(space.id)),
    controlRequest(o.paths, { cmd: "status" }, 1_000),
  ]);
  const report: StatusReport = {
    space: {
      id: space.id,
      slug: space.slug,
      name: space.name,
      kind: space.kind,
      role: space.role,
    },
    kept: kept?.n ?? null,
    kept_more: kept?.more ?? false,
    waiting: review?.total ?? null,
    targets: targets.items.map((t) => ({
      id: t.id,
      kind: t.kind,
      label: t.label,
      path: t.path,
      delivery: t.delivery,
      sync_state: t.sync_state,
      open_drift: t.open_drift,
      compile: t.last_compile?.ref,
      delivered_at: t.delivered?.at,
    })),
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
  };
  return { report, targets: targets.items };
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
  if (targets.length === 0) {
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
  const local = targets.some(
    (t) => t.delivery === "local" && t.sync_state !== "off",
  );
  if (local && !r.daemon.linked_here) {
    lines.push(
      "",
      `  ${chalk.yellow(MARK.waiting)} This repository isn't linked, so nothing is written here: ${chalk.bold("memax link")}`,
    );
  } else if (local && !r.daemon.running) {
    lines.push(
      "",
      `  ${chalk.yellow(MARK.waiting)} The daemon isn't running, so files wait to be written: ${chalk.bold("memax daemon start")}`,
    );
  }
  lines.push("");
  return lines;
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
        const { report, targets } = await buildStatus(memax, {
          space: opts.space,
          cwd: process.cwd(),
          paths: daemonPaths(),
        });
        if (opts.format === "json") {
          console.log(JSON.stringify(report, null, 2));
          return;
        }
        for (const line of renderStatus(report, targets)) console.log(line);
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
