import { Command } from "commander";
import chalk from "chalk";
import type { DreamRun } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";

interface DreamsListOptions {
  limit?: string;
  hub?: string;
  format?: string;
}

interface DreamsReportOptions {
  hub?: string;
  format?: string;
}

interface DreamsTriggerOptions {
  hub?: string;
  format?: string;
}

function parseLimit(raw: string | undefined): number {
  if (!raw) return 10;
  const limit = Number.parseInt(raw, 10);
  if (!Number.isFinite(limit) || limit <= 0) {
    throw new Error(`Invalid --limit value: ${raw}`);
  }
  return limit;
}

export function summarizeRun(run: DreamRun): string[] {
  const parts: string[] = [];
  if (run.duplicates_merged > 0) parts.push(`${run.duplicates_merged} merged`);
  if (run.contradictions_found > 0)
    parts.push(`${run.contradictions_found} contradictions`);
  if (run.memories_archived > 0)
    parts.push(`${run.memories_archived} archived`);
  if (run.memories_organized > 0)
    parts.push(`${run.memories_organized} organized`);
  if ((run.topics_restructured ?? 0) > 0)
    parts.push(`${run.topics_restructured} restructured`);
  return parts;
}

function printRun(run: DreamRun): void {
  // Pick a color per status. "partial_failed" is amber — the run
  // finished but organize/merge/restructure had LLM errors. "skipped"
  // is gray; the cycle never did real work. Everything else (running,
  // unknown) stays yellow.
  const statusColor =
    run.status === "completed"
      ? chalk.green
      : run.status === "failed"
        ? chalk.red
        : run.status === "partial_failed"
          ? chalk.hex("#ff9900")
          : run.status === "skipped"
            ? chalk.gray
            : chalk.yellow;
  const finished = run.finished_at
    ? new Date(run.finished_at).toISOString()
    : "in progress";
  const summary = summarizeRun(run);

  console.log(
    chalk.bold(run.id),
    statusColor(`[${run.status}]`),
    chalk.gray(`· started ${new Date(run.started_at).toISOString()}`),
  );
  console.log(chalk.gray(`  finished: ${finished}`));
  if (run.mode) {
    console.log(chalk.gray(`  mode: ${run.mode}`));
  }
  console.log(chalk.gray(`  scanned: ${run.memories_scanned}`));
  if (summary.length > 0) {
    console.log(chalk.gray(`  result: ${summary.join(", ")}`));
  } else {
    console.log(chalk.gray("  result: no changes"));
  }
  if (run.report) {
    const firstLine = run.report.split("\n").find((line) => line.trim());
    if (firstLine) {
      console.log(chalk.gray(`  report: ${firstLine}`));
    }
  }
}

function resolveFinishedAt(run: DreamRun): string {
  return run.finished_at
    ? new Date(run.finished_at).toISOString()
    : "in progress";
}

function printReport(report: {
  has_run: boolean;
  message?: string;
  run?: DreamRun;
  intelligence?: {
    latest_run?: {
      merged?: number;
      contradictions_found?: number;
      archived?: number;
      organized?: number;
      restructured?: number;
    };
    pending_review?: {
      contradictions?: number;
      topic_merges?: number;
      topic_restructures?: number;
      total?: number;
    };
  };
  actions?: {
    action_type: string;
    reason: string;
  }[];
}): void {
  if (!report.has_run || !report.run) {
    console.log(
      chalk.yellow(report.message ?? "No dream runs yet for this hub."),
    );
    return;
  }

  const run = report.run;
  const latest = report.intelligence?.latest_run;
  const summary = latest
    ? [
        latest.merged && latest.merged > 0 ? `${latest.merged} merged` : null,
        latest.contradictions_found && latest.contradictions_found > 0
          ? `${latest.contradictions_found} contradictions`
          : null,
        latest.archived && latest.archived > 0
          ? `${latest.archived} archived`
          : null,
        latest.organized && latest.organized > 0
          ? `${latest.organized} organized`
          : null,
        latest.restructured && latest.restructured > 0
          ? `${latest.restructured} restructured`
          : null,
      ].filter((part): part is string => !!part)
    : summarizeRun(run);
  console.log(chalk.bold(run.id), chalk.gray(`[${run.status}]`));
  console.log(
    chalk.gray(`  started: ${new Date(run.started_at).toISOString()}`),
  );
  console.log(chalk.gray(`  finished: ${resolveFinishedAt(run)}`));
  if (run.mode) {
    console.log(chalk.gray(`  mode: ${run.mode}`));
  }
  console.log(chalk.gray(`  scanned: ${run.memories_scanned}`));
  if (summary.length > 0) {
    console.log(chalk.gray(`  result: ${summary.join(", ")}`));
  }
  const pending = report.intelligence?.pending_review;
  if ((pending?.total ?? 0) > 0) {
    const parts: string[] = [];
    if ((pending?.contradictions ?? 0) > 0) {
      parts.push(`${pending?.contradictions} contradictions`);
    }
    if ((pending?.topic_merges ?? 0) > 0) {
      parts.push(`${pending?.topic_merges} topic merges`);
    }
    if ((pending?.topic_restructures ?? 0) > 0) {
      parts.push(`${pending?.topic_restructures} restructures`);
    }
    console.log(chalk.gray(`  pending review: ${parts.join(", ")}`));
  }
  console.log();
  if (run.report) {
    console.log(run.report);
  }
  if (report.actions && report.actions.length > 0) {
    console.log();
    console.log(chalk.gray("Actions"));
    for (const action of report.actions) {
      console.log(chalk.gray(`  • ${action.action_type}: ${action.reason}`));
    }
  }
}

export async function dreamsListCommand(
  options: DreamsListOptions,
): Promise<void> {
  try {
    const limit = parseLimit(options.limit);
    const format = options.format ?? "text";
    if (format !== "text" && format !== "json") {
      throw new Error(`Unsupported --format value: ${format}`);
    }

    const hubId = options.hub
      ? await resolveHubID(options.hub)
      : getActiveHubID() || undefined;
    if (options.hub && !hubId) {
      throw new Error(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      );
    }

    const response = await getClient().dreams.list({ limit, hubId });
    const runs = response.runs;

    if (format === "json") {
      // Expose the full envelope (including next_cursor) so
      // scripts can paginate. Breaking change from the pre-3.7
      // shape — callers that parsed a bare array should read
      // response.runs instead.
      process.stdout.write(`${JSON.stringify(response, null, 2)}\n`);
      return;
    }

    if (runs.length === 0) {
      console.log(chalk.yellow("No dream runs yet for this hub."));
      console.log(
        chalk.gray(
          "  Dreams run automatically at night after enough knowledge accumulates.",
        ),
      );
      return;
    }

    console.log(
      chalk.blue(`${runs.length} dream run${runs.length === 1 ? "" : "s"}`),
    );
    console.log();
    for (const run of runs) {
      printRun(run);
      console.log();
    }
    if (response.next_cursor) {
      console.log(
        chalk.gray(
          `More runs available — rerun with --limit ${limit} once cursor support lands in the CLI.`,
        ),
      );
    }
  } catch (err) {
    console.error(chalk.red(`Dream list failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function dreamsReportCommand(
  options: DreamsReportOptions,
): Promise<void> {
  try {
    const format = options.format ?? "text";
    if (format !== "text" && format !== "json") {
      throw new Error(`Unsupported --format value: ${format}`);
    }

    const hubId = options.hub
      ? await resolveHubID(options.hub)
      : getActiveHubID() || undefined;
    if (options.hub && !hubId) {
      throw new Error(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      );
    }

    const report = await getClient().dreams.report(hubId);
    if (format === "json") {
      process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
      return;
    }
    printReport(report);
  } catch (err) {
    console.error(chalk.red(`Dream report failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function dreamsTriggerCommand(
  options: DreamsTriggerOptions,
): Promise<void> {
  try {
    const format = options.format ?? "text";
    if (format !== "text" && format !== "json") {
      throw new Error(`Unsupported --format value: ${format}`);
    }

    const hubId = options.hub
      ? await resolveHubID(options.hub)
      : getActiveHubID() || undefined;
    if (options.hub && !hubId) {
      throw new Error(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      );
    }

    const result = await getClient().dreams.trigger(hubId);

    if (format === "json") {
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      return;
    }

    if (result.status === "queued") {
      console.log(chalk.green("Dream cycle queued."));
    } else {
      console.log(chalk.green(`Dream cycle status: ${result.status}`));
    }
  } catch (err) {
    console.error(chalk.red(`Dream trigger failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerDreamsCommands(program: Command): void {
  const dreamsCmd = program
    .command("dreams")
    .alias("dream")
    .description("Inspect dream runs and consolidation history");

  dreamsCmd
    .command("list")
    .description("List recent dream runs")
    .option("-l, --limit <n>", "Max dream runs", "10")
    .option("--hub <slug>", "Scope to a hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(dreamsListCommand);

  dreamsCmd
    .command("report")
    .description("Show the latest dream report for the current hub")
    .option("--hub <slug>", "Scope to a hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(dreamsReportCommand);

  dreamsCmd
    .command("trigger")
    .description("Trigger a dream cycle for the current hub")
    .option("--hub <slug>", "Scope to a hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(dreamsTriggerCommand);
}
