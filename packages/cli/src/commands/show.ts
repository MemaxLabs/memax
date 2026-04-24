import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { buildTopicPathMap } from "./topic.js";
import { renderMarkdownFragment } from "./recall.js";

export async function showCommand(
  id: string,
  options: { format?: string },
): Promise<void> {
  if (!id) {
    console.error(chalk.red("Provide a memory ID: memax show <id>"));
    process.exit(1);
  }

  try {
    const client = getClient();
    const memory = await client.memories.get(id);
    // `memax show <id>` is explicit user intent — signal the view so
    // the memory's decay multiplier is reinforced. Fire-and-forget so
    // the signal never blocks the display path; a failed ping means
    // this one view won't count, not a user-facing error.
    void client.memories.trackAccessed(id).catch(() => {});
    const topicPath =
      memory.hub_id && memory.topic_id
        ? buildTopicPathMap(
            (await client.topics.list(memory.hub_id)).topics,
          ).get(memory.topic_id)
        : undefined;

    if (options.format === "json") {
      console.log(JSON.stringify(memory, null, 2));
      return;
    }

    console.log(chalk.bold(memory.title));
    console.log(
      chalk.gray(
        `${memory.kind}/${memory.stability} · ${memory.source} · v${memory.version} · ${memory.state}`,
      ),
    );
    if (topicPath) {
      console.log(chalk.gray(`topic: ${topicPath}`));
    }
    if (memory.tags?.length) {
      console.log(chalk.gray(`tags: ${memory.tags.join(", ")}`));
    }
    console.log(chalk.gray(`id: ${memory.id}`));
    console.log();
    if (memory.content) {
      for (const line of renderMarkdownFragment(memory.content, {
        indent: "",
      })) {
        console.log(line);
      }
    }
  } catch (err) {
    console.error(chalk.red(`Show failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerShowCommand(program: Command): void {
  program
    .command("show <id>")
    .alias("get")
    .description("Show a specific memory")
    .option("--format <format>", "Output format: text, json", "text")
    .action(showCommand);
}
