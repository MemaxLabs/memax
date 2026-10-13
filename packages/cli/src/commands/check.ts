// memax check: runs this repository's condition checks now, as the daemon
// does them (lib/daemon/checks.ts, the stale-detection design §2.3), and
// says what each kept memory's conditions found on the default branch and
// what Memax marked stale. Through the running daemon, or in this process
// when none runs. Only results leave the machine, as always.
import chalk from "chalk";
import type { Command } from "commander";
import type { Memax } from "memax-sdk";
import { getClient } from "../lib/client.js";
import type {
  CheckedItem,
  CheckRunSummary,
  CheckView,
} from "../lib/daemon/checks.js";
import { controlRequest } from "../lib/daemon/control.js";
import { defaultBranch, gitDirs } from "../lib/daemon/git-tree.js";
import { daemonPaths, type DaemonPaths } from "../lib/daemon/paths.js";
import { findLinkedRepo } from "../lib/daemon/registry.js";
import { gitRoot } from "../lib/project-context.js";
import { conditionWords, evidenceWords } from "../lib/stale/words.js";
import { openOneShot } from "./compile.js";
import { apiFailureMessage, clock, MARK, tildify } from "./v2-output.js";

export interface CheckOptions {
  format?: string;
}

export interface CheckAnswer {
  run: CheckRunSummary;
  view: CheckView;
}

export interface CheckDeps {
  memax: Memax;
  paths: DaemonPaths;
  cwd: string;
  out(line: string): void;
  /** The checks in this process when no daemon runs (tests replace it). */
  oneShot?: () => Promise<{
    check(root: string): Promise<CheckAnswer | null>;
    close(): Promise<void>;
  } | null>;
}

/** A run reads git and sends one report: generous, but not forever. */
const CHECK_TIMEOUT_MS = 120_000;

export async function check(o: CheckOptions, d: CheckDeps): Promise<number> {
  const root = gitRoot(d.cwd);
  const link = root ? findLinkedRepo(d.paths, root) : null;
  if (!link) {
    d.out(
      chalk.yellow(
        root
          ? `  This repository isn't linked, so Memax checks nothing here. Link it: ${chalk.bold("memax link")}`
          : "  Run memax check in a repository linked with memax link.",
      ),
    );
    return 1;
  }
  let answer = (await controlRequest(
    d.paths,
    { cmd: "check", root: link.root },
    CHECK_TIMEOUT_MS,
  )) as Partial<CheckAnswer> | null;
  if (!answer) {
    const local = await (d.oneShot ?? (() => openOneShot(d)))();
    if (local) {
      try {
        answer = await local.check(link.root);
      } finally {
        await local.close();
      }
    }
  }
  if (!answer?.run || !answer.view) {
    d.out(
      chalk.yellow(
        `  The daemon here didn't run the checks; it may be older than this CLI. Restart it (${chalk.bold("memax daemon stop")}, then ${chalk.bold("memax daemon start")}) and try again.`,
      ),
    );
    return 1;
  }
  const { run, view } = answer as CheckAnswer;
  const dirs = gitDirs(link.root);
  const branch = (dirs && defaultBranch(dirs)) ?? "the default branch";
  if (o.format === "json") {
    d.out(
      JSON.stringify(
        { space: link.space_slug, root: link.root, branch, run, view },
        null,
        2,
      ),
    );
  } else {
    for (const line of renderCheck(link.space_slug, branch, run, view, d.paths))
      d.out(line);
  }
  return run.skipped === "error" ? 1 : 0;
}

/** What a run found, as the terminal shows it. */
export function renderCheck(
  slug: string,
  branch: string,
  run: CheckRunSummary,
  view: CheckView,
  paths: Pick<DaemonPaths, "log">,
  now = new Date(),
): string[] {
  const at = view.commit
    ? ` · ${branch} at ${view.commit.slice(0, 7)}, checked ${clock(view.checked_at ?? undefined, now)}`
    : "";
  const lines = ["", `  ${chalk.bold(tildify(view.root))} → ${slug}${at}`, ""];
  if (run.skipped === "error") {
    lines.push(
      chalk.yellow(
        `  The checks couldn't run here. The daemon's log says why: ${tildify(paths.log)}`,
      ),
      "",
    );
    return lines;
  }
  if (view.not_checked) {
    lines.push(
      `  ${chalk.yellow(MARK.waiting)} Not checked here: ${view.not_checked}.`,
    );
    if (view.items.some((i) => i.outcome))
      lines.push("", chalk.gray("  As the last run here found them:"));
  } else if (view.items.length === 0) {
    lines.push(
      chalk.gray(
        `  No kept memory in ${slug} has conditions for this repository yet.`,
      ),
    );
  }
  const shown = view.not_checked
    ? view.items.filter((i) => i.outcome)
    : view.items;
  const width = Math.max(0, ...shown.map((i) => i.memory.length));
  for (const item of shown) lines.push(...itemLines(item, width, branch, run));

  const flagged = run.flagged ?? [];
  if (flagged.length > 0) {
    const refs = flagged.join(", ");
    const one = flagged.length === 1;
    lines.push(
      "",
      `  ${chalk.yellow(MARK.waiting)} Memax marked ${refs} stale. Verify ${one ? "it" : "each"}: ${chalk.bold(`memax verify ${one ? flagged[0] : "<M-id>"}`)}`,
    );
  } else if (run.ran && (run.sent ?? 0) > 0 && run.flagged === undefined) {
    lines.push(
      "",
      chalk.gray(
        "  Memax couldn't be reached, so the results wait here; the daemon sends them later.",
      ),
    );
  }
  lines.push("");
  return lines;
}

function itemLines(
  item: CheckedItem,
  width: number,
  branch: string,
  run: CheckRunSummary,
): string[] {
  const ref = item.memory.padEnd(width);
  const what = conditionWords(item.condition);
  switch (item.outcome) {
    case "holds":
      return [`  ${chalk.green(MARK.kept)} ${ref}  ${what}`];
    case "fails": {
      const why = evidenceWords(item.condition, item.evidence, branch);
      const marks = item.armed
        ? run.flagged?.includes(item.memory)
          ? " Memax marked it stale."
          : ""
        : ` It never held on ${branch}, so it marks nothing.`;
      return [
        `  ${chalk.yellow(MARK.waiting)} ${ref}  ${what}`,
        `  ${" ".repeat(width + 4)}${chalk.gray(`${why}${marks}`)}`,
      ];
    }
    case "unknown":
      return [
        `  ${chalk.gray(MARK.working)} ${ref}  ${what}${chalk.gray(" · couldn't tell from this clone")}`,
      ];
    default:
      return [
        `  ${chalk.gray(MARK.off)} ${ref}  ${what}${chalk.gray(
          item.condition.kind === "pr_state"
            ? " · GitHub's to check"
            : " · not checked here yet",
        )}`,
      ];
  }
}

export function registerCheckCommand(program: Command): void {
  program
    .command("check")
    .description(
      "Check this repository against what its space's memories rely on, now",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: CheckOptions) => {
      try {
        process.exitCode = await check(opts, {
          memax: getClient(),
          paths: daemonPaths(),
          cwd: process.cwd(),
          out: (l) => console.log(l),
        });
      } catch (err) {
        console.error(chalk.red(`  ${apiFailureMessage(err, "memax check")}`));
        process.exitCode = 1;
      }
    });
}
