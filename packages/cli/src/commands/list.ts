import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";
import type { ListMemoriesResult, Memory } from "memax-sdk";
import { buildTopicPathMap, resolveTopicReference } from "./topic.js";

interface ListOptions {
  sort?: string;
  limit?: string;
  all?: boolean;
  hub?: string;
  cursor?: string;
  format?: string;
  topicId?: string;
}

function parseLimit(raw: string | undefined): number {
  if (!raw) return 20;
  const limit = Number.parseInt(raw, 10);
  if (!Number.isFinite(limit) || limit <= 0) {
    throw new Error(`Invalid --limit value: ${raw}`);
  }
  return limit;
}

export function buildNextPageCommand(
  options: ListOptions,
  nextCursor: string,
): string {
  const parts = ["memax", "list"];
  if (options.sort && options.sort !== "newest")
    parts.push("--sort", options.sort);
  if (options.limit && options.limit !== "20")
    parts.push("--limit", options.limit);
  if (options.hub) parts.push("--hub", options.hub);
  if (options.topicId) parts.push("--topic-id", options.topicId);
  parts.push("--cursor", `'${nextCursor}'`);
  return parts.join(" ");
}

function printTextList(
  memories: Memory[],
  total: number,
  topicPaths: Map<string, string>,
): void {
  console.log(
    chalk.blue(`${memories.length} memor${memories.length > 1 ? "ies" : "y"}`),
    chalk.gray(`(${total} total)`),
  );
  console.log();

  for (const memory of memories) {
    console.log(
      chalk.bold(memory.title),
      chalk.gray(`[${memory.kind} · ${memory.stability}]`),
      chalk.gray(`· ${memory.source}`),
    );
    const topicPath = memory.topic_id
      ? topicPaths.get(memory.topic_id)
      : undefined;
    if (topicPath) {
      console.log(chalk.gray(`  topic: ${topicPath}`));
    }
    console.log(chalk.gray(`  id: ${memory.id}`));
  }
}

function printJSON(result: ListMemoriesResult): void {
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

export async function listCommand(options: ListOptions): Promise<void> {
  try {
    const limit = parseLimit(options.limit);
    const sort = (options.sort ?? "newest") as "newest" | "relevant";
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
    const client = getClient();

    let topicId = options.topicId;
    if (topicId) {
      if (!hubId) {
        throw new Error(
          "Topic lookup requires a hub. Set an active hub or pass --hub.",
        );
      }
      const topics = await client.topics.list(hubId);
      topicId = resolveTopicReference(topics.topics, topicId);
    }

    const memories: Memory[] = [];
    let cursor = options.cursor ?? "";
    let nextCursor = "";
    let total = 0;

    do {
      const res = await client.memories.list({
        limit,
        sort,
        cursor: cursor || undefined,
        hubId,
        topicId,
      });

      memories.push(...(res.memories ?? []));
      nextCursor = res.next_cursor;
      total = res.total;
      cursor = nextCursor;

      if (!options.all) break;
    } while (cursor);

    const result: ListMemoriesResult = {
      memories,
      next_cursor: options.all ? "" : nextCursor,
      has_more: options.all ? false : nextCursor !== "",
      total,
    };

    const topicPaths =
      hubId && memories.some((memory) => !!memory.topic_id)
        ? buildTopicPathMap((await client.topics.list(hubId)).topics)
        : new Map<string, string>();

    if (format === "json") {
      printJSON(result);
      return;
    }

    if (memories.length === 0) {
      console.log(chalk.yellow("No memories yet. Push your first one:"));
      console.log(chalk.gray("  memax push --file ./README.md"));
      return;
    }

    printTextList(memories, total, topicPaths);

    if (result.has_more) {
      console.log();
      console.log(chalk.gray(`  Showing ${memories.length} of ${total}.`));
      console.log(chalk.gray(`  Next cursor: ${result.next_cursor}`));
      console.log(
        chalk.gray(
          `  Next page: ${buildNextPageCommand(options, result.next_cursor)}`,
        ),
      );
      console.log(chalk.gray("  Use --all to fetch every page."));
    }
  } catch (err) {
    console.error(chalk.red(`List failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerListCommand(program: Command): void {
  program
    .command("list")
    .description("List your memories")
    .option("-s, --sort <sort>", "Sort by: newest, relevant", "newest")
    .option("-l, --limit <n>", "Max results per page", "20")
    .option("--cursor <cursor>", "Continue from a pagination cursor")
    .option("--hub <slug>", "List memories from a specific hub only")
    .option("--topic-id <id>", "Restrict results to memories in this topic")
    .option("--format <format>", "Output format: text, json", "text")
    .option("--all", "Fetch all pages (not just the first)")
    .action(listCommand);
}
