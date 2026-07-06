import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";
import type { Topic } from "memax-sdk";
import { resolveTopicReference, topicDisplayID } from "./topic-shared.js";

// Archive lifecycle commands: `memax topic archive`, `memax topic restore`,
// `memax topic archived`. Kept out of topic.ts to respect the ~300-line
// command-file cap; shared helpers live in topic-shared.ts so imports flow
// one way (topic.ts → topic-archive.ts → topic-shared.ts, never backwards).

async function resolveHubOption(hub?: string): Promise<string | undefined> {
  const hubId = hub ? await resolveHubID(hub) : getActiveHubID() || undefined;
  if (hub && !hubId) {
    throw new Error(
      "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
    );
  }
  return hubId;
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Resolve a topic reference against the ARCHIVED flat list (id or unique
 * id-prefix). Restore targets are invisible in the active tree, so the
 * shared resolveTopicReference (which walks the tree) cannot find them.
 */
function resolveArchivedTopicReference(archived: Topic[], ref: string): string {
  const normalized = ref.trim().toLowerCase();
  const exact = archived.find((topic) => topic.id.toLowerCase() === normalized);
  if (exact) {
    return exact.id;
  }
  const prefixMatches = archived.filter((topic) =>
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
    "Archived topic not found. Run `memax topic archived` to see archived topics.",
  );
}

export async function topicArchiveCommand(
  topicId: string,
  options: { hub?: string } = {},
): Promise<void> {
  try {
    const client = getClient();
    const hubId = await resolveHubOption(options.hub);

    let resolvedTopicID = topicId;
    if (!UUID_RE.test(topicId)) {
      const topics = await client.topics.list(hubId);
      resolvedTopicID = resolveTopicReference(topics.topics, topicId);
    }

    const result = await client.topics.archive(resolvedTopicID, hubId);
    const descendants = (result.archived_count ?? 1) - 1;
    const suffix =
      descendants > 0
        ? chalk.gray(
            ` (+${descendants} subtopic${descendants === 1 ? "" : "s"})`,
          )
        : "";
    console.log(chalk.green("Topic archived") + suffix);
    console.log(
      chalk.gray(
        "  Restore anytime with `memax topic restore " +
          `${resolvedTopicID.slice(0, 8)}\``,
      ),
    );
  } catch (err) {
    console.error(chalk.red(`Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function topicRestoreCommand(
  topicId: string,
  options: { hub?: string } = {},
): Promise<void> {
  try {
    const client = getClient();
    const hubId = await resolveHubOption(options.hub);

    let resolvedTopicID = topicId;
    if (!UUID_RE.test(topicId)) {
      const archived = await client.topics.listArchived(hubId);
      resolvedTopicID = resolveArchivedTopicReference(archived.topics, topicId);
    }

    const result = await client.topics.restore(resolvedTopicID, hubId);
    const descendants = (result.restored_count ?? 1) - 1;
    const suffix =
      descendants > 0
        ? chalk.gray(
            ` (+${descendants} subtopic${descendants === 1 ? "" : "s"})`,
          )
        : "";
    console.log(chalk.green("Topic restored") + suffix);
  } catch (err) {
    console.error(chalk.red(`Failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export async function topicArchivedListCommand(
  options: { hub?: string; verbose?: boolean; format?: string } = {},
): Promise<void> {
  try {
    const format = options.format ?? "text";
    if (format !== "text" && format !== "json") {
      throw new Error(`Unsupported --format value: ${format}`);
    }
    const client = getClient();
    const hubId = await resolveHubOption(options.hub);
    const result = await client.topics.listArchived(hubId);

    if (format === "json") {
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      return;
    }

    if (result.topics.length === 0) {
      console.log(chalk.gray("No archived topics."));
      return;
    }

    for (const topic of result.topics) {
      const archivedAt = topic.archived_at
        ? new Date(topic.archived_at).toISOString().slice(0, 10)
        : "";
      console.log(
        `  ${topic.icon || "folder"}  ${chalk.bold(topic.name)} ${chalk.gray(
          `archived ${archivedAt}`,
        )}`,
      );
      console.log(
        `    ${chalk.gray(`id: ${topicDisplayID(topic.id, options.verbose)}`)}`,
      );
    }
    console.log(
      chalk.gray(
        `\n  ${result.topics.length} archived topic${result.topics.length === 1 ? "" : "s"} — restore with \`memax topic restore <id>\``,
      ),
    );
  } catch (err) {
    console.error(
      chalk.red(`Failed to list archived topics: ${(err as Error).message}`),
    );
    process.exit(1);
  }
}
