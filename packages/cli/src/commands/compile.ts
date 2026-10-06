// memax compile: compile every target of the space now, then wait for the
// files to reach this machine and say where each stands (CompileDone).
// The running daemon writes them; without one, this command does, for a
// linked repository.
import { randomUUID } from "node:crypto";
import chalk from "chalk";
import type { Command } from "commander";
import type { Memax, V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { getOrCreateDeviceID, loadConfig } from "../lib/config.js";
import { sdkDaemonApi } from "../lib/daemon/api.js";
import { controlRequest } from "../lib/daemon/control.js";
import { fileLogger } from "../lib/daemon/log.js";
import { OneShotDelivery } from "../lib/daemon/oneshot.js";
import {
  daemonPaths,
  daemonUnsupported,
  type DaemonPaths,
} from "../lib/daemon/paths.js";
import { findLinkedRepo } from "../lib/daemon/registry.js";
import { resolveSpace, SpaceChoiceError } from "../lib/v2-space.js";
import { cliVersion } from "../lib/version.js";
import { apiFailureMessage, MARK, targetRows } from "./v2-output.js";

export interface CompileOptions {
  space?: string;
  via?: string;
  format?: string;
  timeout?: string;
}

export interface CompileDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  out: (line: string) => void;
  /** Opens in-process delivery when no daemon runs (tests swap it). */
  oneShot?: () => Promise<OneShotDelivery | null>;
  pollMs?: number;
}

export interface CompileReport {
  space: string;
  requested: string[];
  writer: "daemon" | "compile" | null;
  timed_out: boolean;
  targets: Array<{
    id: string;
    label: string;
    kind: V2.TargetKind;
    delivery: V2.Delivery;
    sync_state: V2.SyncState;
    compile?: string;
    status?: V2.CompileStatus;
    files: string[];
  }>;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Whether a requested target is done: compiled, and on disk if it can be. */
function settled(t: V2.Target, gen: number, delivering: boolean): boolean {
  if (t.sync_state === "off") return true;
  if (t.compiled_gen < gen) return false;
  if (t.last_compile?.status === "failed") return true;
  if (t.delivery !== "local" || !delivering) return true;
  return t.sync_state === "in_sync" || t.sync_state === "drifted";
}

export async function compile(
  o: CompileOptions,
  d: CompileDeps,
): Promise<number> {
  if (o.via === "pr") {
    d.out(
      chalk.yellow(
        "  Compiling through pull requests is not available yet (Phase 4). Leave out --via to write the files here.",
      ),
    );
    return 1;
  }
  if (o.via !== undefined && o.via !== "local") {
    d.out(
      chalk.red(
        `  --via takes pr (not available yet); without it, the files are written here.`,
      ),
    );
    return 2;
  }
  const timeoutMs = Math.max(1, Number(o.timeout ?? 30)) * 1000;
  const { space, root } = await resolveSpace(d.memax, {
    space: o.space,
    cwd: d.cwd,
    paths: d.paths,
  });
  let targets = (await d.memax.v2.targets.list(space.id)).items;
  if (targets.length === 0) {
    d.out(
      chalk.yellow(
        `  ${space.slug} has no targets yet. Add AGENTS.md and the rest from its Brief in the app.`,
      ),
    );
    return 1;
  }
  const want = new Map<string, number>();
  for (const t of targets) {
    if (t.sync_state === "off") continue;
    try {
      const r = await d.memax.v2.targets.compile(
        t.id,
        {},
        { idempotencyKey: randomUUID(), via: "cli" },
      );
      want.set(t.id, r.target.dirty_gen);
    } catch (err) {
      if ((err as { status?: number }).status !== 409) throw err; // stopped meanwhile
    }
  }

  // Who writes the files here: the daemon, this command, or nobody.
  const daemon = await controlRequest(
    d.paths,
    { cmd: "wake", space: space.id },
    1_000,
  );
  const linkedHere =
    !!root && findLinkedRepo(d.paths, root)?.space_id === space.id;
  let writer: CompileReport["writer"] = daemon ? "daemon" : null;
  let local: OneShotDelivery | null = null;
  if (!daemon && linkedHere && !daemonUnsupported()) {
    local = await (d.oneShot ?? (() => openOneShot(d)))();
    writer = local ? "compile" : "daemon";
  }

  const deadline = Date.now() + timeoutMs;
  let timedOut = false;
  try {
    for (;;) {
      if (local) await local.sync(space.id);
      targets = (await d.memax.v2.targets.list(space.id)).items;
      const delivering = writer !== null && linkedHere;
      if (
        targets.every(
          (t) => !want.has(t.id) || settled(t, want.get(t.id)!, delivering),
        )
      )
        break;
      if (Date.now() > deadline) {
        timedOut = true;
        break;
      }
      await sleep(d.pollMs ?? 1_000);
    }
  } finally {
    await local?.close();
  }

  const report: CompileReport = {
    space: space.slug,
    requested: [...want.keys()],
    writer: linkedHere ? writer : null,
    timed_out: timedOut,
    targets: targets.map((t) => ({
      id: t.id,
      label: t.label,
      kind: t.kind,
      delivery: t.delivery,
      sync_state: t.sync_state,
      compile: t.last_compile?.ref,
      status: t.last_compile?.status,
      files: (t.last_compile?.files ?? []).map((f) => f.path ?? f.label ?? ""),
    })),
  };
  if (o.format === "json") {
    d.out(JSON.stringify(report, null, 2));
  } else {
    for (const line of renderCompile(report, targets, linkedHere)) d.out(line);
  }
  return timedOut ? 1 : 0;
}

export function renderCompile(
  r: CompileReport,
  targets: V2.Target[],
  linkedHere: boolean,
): string[] {
  const live = targets.filter(
    (t) =>
      t.sync_state !== "off" &&
      t.last_compile &&
      t.last_compile.status !== "failed",
  );
  const files = live.reduce(
    (n, t) => n + (t.last_compile?.files.length ?? 0),
    0,
  );
  const lines = [""];
  if (r.timed_out) {
    lines.push(
      `  ${chalk.yellow(MARK.working)} ${chalk.bold(r.space)} is still compiling. Check again with: memax status`,
    );
  } else {
    lines.push(
      `  ${chalk.green(MARK.done)} ${chalk.bold(r.space)} is compiled into ${files} ${files === 1 ? "file" : "files"}.`,
    );
  }
  lines.push("", ...targetRows(targets));
  for (const t of targets.filter((x) => x.last_compile?.status === "failed")) {
    lines.push(
      chalk.red(
        `  ✗ ${t.label}: the last compile failed${t.last_compile?.error ? ` (${t.last_compile.error})` : ""}; the one before stays.`,
      ),
    );
  }
  if (targets.some((t) => t.sync_state === "drifted")) {
    lines.push(
      "",
      chalk.gray(
        "  Memax won't write over a hand edit. Pull it in, overwrite it or stop compiling it in the app.",
      ),
    );
  }
  const local = targets.some(
    (t) => t.delivery === "local" && t.sync_state !== "off",
  );
  lines.push("");
  if (local && !linkedHere) {
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} Nothing was written here: this repository isn't linked. Link it: ${chalk.bold("memax link")}`,
    );
  } else if (local && r.writer === "compile") {
    lines.push(
      chalk.gray(
        "  Written here by memax compile. Start the daemon so each Keep reaches them on its own: memax daemon start",
      ),
    );
  } else if (local) {
    lines.push(
      chalk.gray(
        "  Written here by the daemon. Commit them to share them with your team and your cloud agents.",
      ),
    );
  }
  lines.push("");
  return lines;
}

function openOneShot(d: CompileDeps): Promise<OneShotDelivery | null> {
  return OneShotDelivery.open({
    paths: d.paths,
    api: sdkDaemonApi(d.memax),
    deviceId: getOrCreateDeviceID(),
    log: fileLogger(d.paths.log),
    version: cliVersion(),
    apiUrl: loadConfig().api_url,
  });
}

export function registerCompileCommand(program: Command): void {
  program
    .command("compile")
    .description(
      "Compile every target of the space now, and write the files here",
    )
    .option("--space <slug>", "The space (default: this repository's)")
    .option("--via <how>", "pr: through a pull request (not available yet)")
    .option("--timeout <seconds>", "How long to wait for the files", "30")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: CompileOptions) => {
      try {
        process.exitCode = await compile(opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          out: (l) => console.log(l),
        });
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
