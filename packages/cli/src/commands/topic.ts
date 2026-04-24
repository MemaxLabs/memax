import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";
import type { TopicTree } from "memax-sdk";

interface TopicListOptions {
  hub?: string;
  verbose?: boolean;
  format?: string;
}

export function topicDisplayCount(
  topic: Pick<TopicTree, "total_memory_count" | "memory_count">,
): number {
  return topic.total_memory_count ?? topic.memory_count ?? 0;
}

export function buildTopicPathMap(
  topics: TopicTree[],
  parentPath = "",
  map = new Map<string, string>(),
): Map<string, string> {
  for (const topic of topics) {
    const path = parentPath ? `${parentPath} / ${topic.name}` : topic.name;
    map.set(topic.id, path);
    if (topic.children?.length) {
      buildTopicPathMap(topic.children, path, map);
    }
  }
  return map;
}

export function flattenTopics(
  topics: TopicTree[],
  output: TopicTree[] = [],
): TopicTree[] {
  for (const topic of topics) {
    output.push(topic);
    if (topic.children?.length) {
      flattenTopics(topic.children, output);
    }
  }
  return output;
}

export function topicDisplayID(id: string, verbose = false): string {
  return verbose ? id : id.slice(0, 8);
}

export function resolveTopicReference(
  topics: TopicTree[],
  ref: string,
): string {
  const normalized = ref.trim().toLowerCase();
  const flat = flattenTopics(topics);

  const exact = flat.find((topic) => topic.id.toLowerCase() === normalized);
  if (exact) {
    return exact.id;
  }

  const prefixMatches = flat.filter((topic) =>
    topic.id.toLowerCase().startsWith(normalized),
  );
  if (prefixMatches.length === 1) {
    return prefixMatches[0].id;
  }
  if (prefixMatches.length > 1) {
    throw new Error(
      `Topic ID prefix is ambiguous. Matches: ${prefixMatches.map((topic) => `${topic.name} (${topic.id.slice(0, 8)})`).join(", ")}`,
    );
  }

  throw new Error(
    "Topic not found. Run `memax topic list` to see available topic IDs.",
  );
}

export async function topicListCommand(
  options: TopicListOptions,
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
    const result = await getClient().topics.list(hubId);

    if (format === "json") {
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      return;
    }

    if (result.topics.length === 0) {
      console.log(
        chalk.gray(
          "No topics yet. Push some memories and run a dream cycle to auto-organize.",
        ),
      );
      if (result.unassigned_count > 0) {
        console.log(
          chalk.yellow(
            `  ${result.unassigned_count} unassigned memories waiting to be organized`,
          ),
        );
      }
      return;
    }

    // Print tree using subtree totals so parent counts reflect the whole branch.
    function printTopic(topic: TopicTree, indent: number) {
      const prefix = "  ".repeat(indent);
      const metaPrefix = "  ".repeat(indent + 1);
      const icon = topic.icon || "folder";
      const count = topicDisplayCount(topic);
      const pin = topic.pinned ? chalk.yellow(" ★") : "";
      const userMod = topic.user_modified ? chalk.blue(" ✎") : "";
      console.log(
        `${prefix}${icon}  ${chalk.bold(topic.name)} ${chalk.gray(`(${count})`)}${pin}${userMod}`,
      );
      console.log(
        `${metaPrefix}${chalk.gray(`id: ${topicDisplayID(topic.id, options.verbose)}`)}`,
      );
      if (topic.children) {
        for (const child of topic.children) {
          printTopic(child, indent + 1);
        }
      }
    }

    for (const topic of result.topics) {
      printTopic(topic, 0);
    }

    if (result.unassigned_count > 0) {
      console.log(
        chalk.gray(`\n  📥 Inbox: ${result.unassigned_count} unassigned`),
      );
    }

    console.log(chalk.gray(`\n  ${result.topics.length} topics`));
  } catch (err) {
    console.error(
      chalk.red(`Failed to list topics: ${(err as Error).message}`),
    );
    process.exit(1);
  }
}

interface TopicCreateOptions {
  parent?: string;
  description?: string;
  icon?: string;
  hub?: string;
}

export async function topicCreateCommand(
  name: string,
  options: TopicCreateOptions,
): Promise<void> {
  try {
    const client = getClient();
    const hubId = options.hub ? await resolveHubID(options.hub) : undefined;
    if (options.hub && !hubId) {
      throw new Error(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      );
    }
    let parentID = options.parent;
    if (parentID) {
      const topics = await client.topics.list(hubId);
      parentID = resolveTopicReference(topics.topics, parentID);
    }
    const topic = await client.topics.create({
      name,
      description: options.description,
      icon: options.icon,
      parent_id: parentID,
      hub_id: hubId,
    });
    console.log(chalk.green("Created topic"), chalk.bold(topic.name));
    console.log(chalk.gray(`  id: ${topic.id}`));
  } catch (err) {
    console.error(
      chalk.red(`Failed to create topic: ${(err as Error).message}`),
    );
    process.exit(1);
  }
}

function describeSkipReason(reason: string): string {
  switch (reason) {
    case "not_owned":
      return "you do not own this memory";
    case "not_found":
      return "memory not found";
    case "already_at_target":
      return "already in the destination topic";
    case "source_delete_forbidden":
      return "you do not have permission to remove this memory from its source hub";
    default:
      return reason;
  }
}

export async function topicSetCommand(
  memoryId: string,
  options: { topic: string },
): Promise<void> {
  try {
    const client = getClient();
    const memory = await client.memories.get(memoryId);
    const topics = await client.topics.list(memory.hub_id);
    const topicID = resolveTopicReference(topics.topics, options.topic);
    // Route through the unified move contract (memories.batchMove) so CLI
    // moves share the same authoritative semantics as the web picker and
    // drag/drop — atomic replace, not confidence-gated.
    const result = await client.memories.batchMove([memoryId], {
      hubId: memory.hub_id,
      topicId: topicID,
    });
    if (result.moved === 1) {
      console.log(chalk.green("Memory topic updated"));
      return;
    }
    const skipped = result.skipped[0];
    if (skipped) {
      throw new Error(
        `Memory could not be moved — ${describeSkipReason(skipped.reason)}`,
      );
    }
    throw new Error("Memory could not be moved");
  } catch (err) {
    console.error(chalk.red(`Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function topicClearCommand(
  memoryId: string,
  options: { topic?: string },
): Promise<void> {
  try {
    const client = getClient();
    const memory = await client.memories.get(memoryId);
    if (!memory.topic_id) {
      throw new Error("Memory does not have a topic to clear");
    }
    if (options.topic) {
      // Optional safety check: if caller specified a topic, confirm the memory
      // is actually in it before clearing.
      const topics = await client.topics.list(memory.hub_id);
      const requestedTopicID = resolveTopicReference(
        topics.topics,
        options.topic,
      );
      if (requestedTopicID !== memory.topic_id) {
        throw new Error(
          `Memory is not assigned to topic ${options.topic} (currently in topic ${memory.topic_id})`,
        );
      }
    }
    // "Clear topic" is batchMove with only a hub target — the store atomically
    // deletes the memory_topics row without inserting a new one. Same unified
    // contract as web TopicLocation.handleRemove.
    const result = await client.memories.batchMove([memoryId], {
      hubId: memory.hub_id,
    });
    if (result.moved === 1) {
      console.log(chalk.green("Memory topic cleared"));
      return;
    }
    const skipped = result.skipped[0];
    if (skipped) {
      throw new Error(
        `Memory topic could not be cleared — ${describeSkipReason(skipped.reason)}`,
      );
    }
    throw new Error("Memory topic could not be cleared");
  } catch (err) {
    console.error(chalk.red(`Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function topicDeleteCommand(
  topicId: string,
  options: { hub?: string } = {},
): Promise<void> {
  try {
    const client = getClient();
    const hubId = options.hub
      ? await resolveHubID(options.hub)
      : getActiveHubID() || undefined;
    if (options.hub && !hubId) {
      throw new Error(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      );
    }
    let resolvedTopicID = topicId;

    if (
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        topicId,
      )
    ) {
      const topics = await client.topics.list(hubId);
      resolvedTopicID = resolveTopicReference(topics.topics, topicId);
    }

    await client.topics.delete(resolvedTopicID);
    console.log(chalk.green("Topic deleted"));
  } catch (err) {
    console.error(chalk.red(`Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerTopicCommands(program: Command): void {
  const topicCmd = program
    .command("topic")
    .alias("topics")
    .description("Browse and manage knowledge topics");

  topicCmd
    .command("list")
    .description("List your topic tree")
    .option("--hub <slug>", "Scope to a hub")
    .option("--verbose", "Show full topic IDs")
    .option("--format <format>", "Output format: text, json", "text")
    .action(topicListCommand);

  topicCmd
    .command("create <name>")
    .description("Create a new topic")
    .option("-p, --parent <id-or-prefix>", "Parent topic ID or unique prefix")
    .option("-d, --description <text>", "Topic description")
    .option("-i, --icon <name>", "Lucide icon name")
    .option("--hub <slug>", "Hub to create in")
    .action(topicCreateCommand);

  topicCmd
    .command("set <memory-id>")
    .alias("add")
    .description("Set the topic for a memory")
    .requiredOption("-t, --topic <id>", "Topic ID or unique prefix")
    .action(topicSetCommand);

  topicCmd
    .command("clear <memory-id>")
    .alias("remove")
    .description("Clear the topic from a memory")
    .option(
      "-t, --topic <id>",
      "Topic ID or unique prefix (optional if the memory already has one)",
    )
    .action(topicClearCommand);

  topicCmd
    .command("delete <topic-id>")
    .description("Delete a topic")
    .option("--hub <slug>", "Scope topic prefix resolution to a hub")
    .action(topicDeleteCommand);
}
