import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";
import { detectProjectContext } from "../lib/project-context.js";
import { buildTopicPathMap, resolveTopicReference } from "./topic.js";

interface RecallOptions {
  tags?: string;
  limit?: string;
  format?: string;
  includeArchived?: boolean;
  noRerank?: boolean;
  topicId?: string;
  hook?: boolean;
  maxTokens?: string;
  hub?: string;
}

const humanExcerptLineLimit = 4;
const plainExcerptLineLimit = 3;
const memoryDivider = "  ───────────────────────────────────────────────";

export function previewLines(text: string, limit: number): string[] {
  return text
    .split("\n")
    .map((line) => line.trimEnd())
    .filter((line) => line.trim() !== "")
    .slice(0, limit);
}

export function hasMorePreviewLines(text: string, limit: number): boolean {
  return (
    text
      .split("\n")
      .map((line) => line.trimEnd())
      .filter((line) => line.trim() !== "").length > limit
  );
}

function styleInlineMarkdown(text: string): string {
  return text
    .replace(/`([^`]+)`/g, (_, code: string) => chalk.cyan(code))
    .replace(/\*\*([^*]+)\*\*/g, (_, bold: string) => chalk.bold(bold))
    .replace(/__([^_]+)__/g, (_, bold: string) => chalk.bold(bold))
    .replace(/\*([^*\n]+)\*/g, (_, italic: string) => chalk.italic(italic))
    .replace(/_([^_\n]+)_/g, (_, italic: string) => chalk.italic(italic));
}

function renderMarkdownLine(line: string): string {
  const trimmed = line.trim();
  if (!trimmed) {
    return "";
  }

  const heading = trimmed.match(/^(#{1,6})\s+(.*)$/);
  if (heading) {
    return chalk.bold(styleInlineMarkdown(heading[2]));
  }

  const quote = trimmed.match(/^>\s?(.*)$/);
  if (quote) {
    return chalk.gray(`│ ${styleInlineMarkdown(quote[1])}`);
  }

  const unordered = trimmed.match(/^[-*+]\s+(.*)$/);
  if (unordered) {
    return `${chalk.gray("•")} ${styleInlineMarkdown(unordered[1])}`;
  }

  const ordered = trimmed.match(/^(\d+)\.\s+(.*)$/);
  if (ordered) {
    return `${chalk.gray(`${ordered[1]}.`)} ${styleInlineMarkdown(ordered[2])}`;
  }

  return styleInlineMarkdown(trimmed);
}

export function renderMarkdownFragment(
  text: string,
  options: { indent?: string; maxLines?: number } = {},
): string[] {
  const indent = options.indent ?? "  ";
  const rawLines = text
    .split("\n")
    .map((line) => line.trimEnd())
    .filter((line) => line.trim() !== "");
  const limited =
    typeof options.maxLines === "number"
      ? rawLines.slice(0, options.maxLines)
      : rawLines;

  const output: string[] = [];
  let inFence = false;
  for (const line of limited) {
    const trimmed = line.trim();
    if (trimmed.startsWith("```")) {
      inFence = !inFence;
      continue;
    }
    const rendered = inFence ? chalk.cyan(trimmed) : renderMarkdownLine(line);
    if (rendered) {
      output.push(`${indent}${rendered}`);
    }
  }
  return output;
}

function printSection(label: string, bodyLines: string[]): void {
  if (bodyLines.length === 0) {
    return;
  }
  console.log(chalk.gray(`  ${label}`));
  for (const line of bodyLines) {
    console.log(line);
  }
}

function formatRecallHeader(memory: {
  title: string;
  kind: string;
  stability: string;
  relevance_score: number;
  age: string;
}): string {
  const score = (memory.relevance_score * 100).toFixed(0);
  return [
    chalk.bold(memory.title),
    chalk.gray(`[${formatClassification(memory)}]`),
    chalk.cyan(`${score}%`),
    chalk.gray(`· ${memory.age}`),
  ].join(" ");
}

function formatClassification(memory: {
  kind?: string;
  stability?: string;
}): string {
  return [memory.kind, memory.stability].filter(Boolean).join(" · ");
}

function printHumanRecallMemory(
  mem: {
    id: string;
    title: string;
    kind: string;
    stability: string;
    relevance_score: number;
    age: string;
    heading_chain?: string;
    topicPath?: string;
    summary?: string;
    chunk_content: string;
  },
  isLast: boolean,
): void {
  console.log(formatRecallHeader(mem));
  console.log(chalk.gray(`  id: ${mem.id}`));
  if (mem.heading_chain) {
    console.log(chalk.gray(`  ${mem.heading_chain}`));
  }
  if (mem.topicPath) {
    console.log(chalk.gray(`  topic: ${mem.topicPath}`));
  }
  console.log();

  if (mem.summary) {
    printSection(
      "Summary",
      renderMarkdownFragment(mem.summary, { indent: "    " }),
    );
    console.log();
  }

  const excerptLines = renderMarkdownFragment(mem.chunk_content, {
    indent: "    ",
    maxLines: humanExcerptLineLimit,
  });
  printSection("Relevant excerpt", excerptLines);
  if (hasMorePreviewLines(mem.chunk_content, humanExcerptLineLimit)) {
    console.log(chalk.gray("    …"));
  }

  if (!isLast) {
    console.log(chalk.gray(memoryDivider));
  }
  console.log();
}

function printPlainRecallMemory(mem: {
  id: string;
  title: string;
  kind: string;
  stability: string;
  relevance_score: number;
  age: string;
  heading_chain?: string;
  topicPath?: string;
  summary?: string;
  chunk_content: string;
}): void {
  const score = (mem.relevance_score * 100).toFixed(0);
  console.log(
    `${mem.title} [${formatClassification(mem)}] ${score}% · ${mem.age} (${mem.id})`,
  );
  if (mem.heading_chain) {
    console.log(`  ${mem.heading_chain}`);
  }
  if (mem.topicPath) {
    console.log(`  topic: ${mem.topicPath}`);
  }
  if (mem.summary) {
    console.log("  Summary:");
    for (const line of renderMarkdownFragment(mem.summary, {
      indent: "    ",
    })) {
      console.log(line);
    }
  }
  console.log("  Relevant excerpt:");
  for (const line of renderMarkdownFragment(mem.chunk_content, {
    indent: "    ",
    maxLines: plainExcerptLineLimit,
  })) {
    console.log(line);
  }
  if (hasMorePreviewLines(mem.chunk_content, plainExcerptLineLimit)) {
    console.log("    …");
  }
  console.log();
}

export async function recallCommand(
  query: string | undefined,
  options: RecallOptions,
): Promise<void> {
  if (!query) {
    // Read from stdin if no query argument
    if (!process.stdin.isTTY) {
      const chunks: Buffer[] = [];
      for await (const chunk of process.stdin) {
        chunks.push(chunk);
      }
      query = Buffer.concat(chunks).toString("utf-8").trim();
    }
    if (!query) {
      console.error(chalk.red('Provide a query: memax recall "your question"'));
      process.exit(1);
    }
  }

  const limit = parseInt(options.limit ?? "10", 10);
  const hubId = options.hub
    ? await resolveHubID(options.hub)
    : getActiveHubID() || undefined;
  if (options.hub && !hubId) {
    console.error(
      chalk.red(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      ),
    );
    process.exit(1);
  }
  let topicId = options.topicId;
  if (topicId) {
    if (!hubId) {
      console.error(
        chalk.red(
          "Topic lookup requires a hub. Set an active hub or pass --hub.",
        ),
      );
      process.exit(1);
    }
    try {
      const topics = await getClient().topics.list(hubId);
      topicId = resolveTopicReference(topics.topics, topicId);
    } catch (err) {
      console.error(chalk.red((err as Error).message));
      process.exit(1);
    }
  }

  try {
    const result = await getClient().recall(query, {
      limit,
      includeArchived: options.includeArchived ?? false,
      noRerank: options.noRerank,
      topicId,
      source: options.hook ? "hook" : "cli",
      workingDir: process.cwd(),
      projectContext: detectProjectContext(),
      hubId,
    });

    const memories = result.memories ?? [];
    const topicPathMap =
      hubId && memories.some((mem) => !!mem.topic_id)
        ? buildTopicPathMap((await getClient().topics.list(hubId)).topics)
        : new Map<string, string>();
    const memoriesWithTopics = memories.map((mem) => ({
      ...mem,
      topicPath: mem.topic_id
        ? topicPathMap.get(mem.topic_id) || mem.topic_name
        : mem.topic_name,
    }));

    if (options.format === "json") {
      console.log(
        JSON.stringify({ ...result, memories: memoriesWithTopics }, null, 2),
      );
      return;
    }

    // Hook mode: output clean context for agent injection
    if (options.hook) {
      if (memoriesWithTopics.length === 0) return;
      let output = "<memax-context>\n";
      output += "## Relevant Context (from Memax)\n\n";
      for (const mem of memoriesWithTopics) {
        const heading = mem.heading_chain ? ` — ${mem.heading_chain}` : "";
        output += `### ${mem.title} [${formatClassification(mem)}, ${mem.age}]${heading}\n`;
        if (mem.topicPath) {
          output += `Topic: ${mem.topicPath}\n`;
        }
        if (mem.summary) {
          output += `Summary: ${mem.summary}\n\n`;
        }
        output += mem.chunk_content + "\n\n";
      }
      output += "</memax-context>";

      // Truncate to fit within token budget (1 token ≈ 4 characters)
      const maxTokens = options.maxTokens
        ? parseInt(options.maxTokens, 10)
        : undefined;
      if (maxTokens) {
        const maxChars = maxTokens * 4;
        if (output.length > maxChars) {
          output =
            output.substring(0, maxChars - 50) +
            "\n\n[truncated to fit token budget]\n</memax-context>";
        }
      }

      console.log(output);
      return;
    }

    // Pipe-friendly plain text output when stdout is not a TTY
    if (!process.stdout.isTTY) {
      if (memoriesWithTopics.length === 0) {
        return;
      }
      for (const mem of memoriesWithTopics) {
        printPlainRecallMemory(mem);
      }
      return;
    }

    // Human-readable output
    if (memoriesWithTopics.length === 0) {
      console.log(chalk.yellow("No results found."));
      console.log(
        chalk.gray(
          `  Searched ${result.query_metadata.total_candidates} chunks in ${result.query_metadata.latency_ms}ms`,
        ),
      );
      return;
    }

    console.log(
      chalk.blue(
        `${memoriesWithTopics.length} result${memoriesWithTopics.length > 1 ? "s" : ""}`,
      ),
      chalk.gray(
        `(${result.query_metadata.total_candidates} chunks searched, ${result.query_metadata.latency_ms}ms)`,
      ),
    );
    console.log();

    for (const [index, mem] of memoriesWithTopics.entries()) {
      printHumanRecallMemory(mem, index === memoriesWithTopics.length - 1);
    }

    console.log(chalk.gray("  Use memax show <id> to read the full memory."));
  } catch (err) {
    console.error(chalk.red(`Recall failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerRecallCommand(program: Command): void {
  program
    .command("recall [query]")
    .description("Ask your knowledge a question")
    .option("-t, --tags <tags>", "Filter by tags")
    .option("-l, --limit <n>", "Max results", "10")
    .option("--format <format>", "Output format: text, json", "text")
    .option("--no-rerank", "Skip reranking (faster, uses local scoring only)")
    .option("--topic-id <id>", "Restrict results to memories in this topic")
    .option("--hook", "Output in agent-injectable format")
    .option("--max-tokens <number>", "Maximum tokens to output (approximate)")
    .option("--include-archived", "Include archived memories")
    .option("--hub <slug>", "Boost a hub in ranking (defaults to active hub)")
    .action(recallCommand);
}
