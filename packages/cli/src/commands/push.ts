import { Command } from "commander";
import chalk from "chalk";
import { readFileSync, statSync } from "node:fs";
import { basename } from "node:path";
import { getClient, setClientAgent } from "../lib/client.js";
import {
  detectProjectContext,
  readMemaxYmlHub,
} from "../lib/project-context.js";
import { resolveHubID } from "../lib/hubs.js";

const MAX_SIZE = 3 * 1024 * 1024; // 3MB — must match server's MAX_BODY_SIZE default

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

interface PushOptions {
  file?: string;
  tags?: string;
  ttl?: string;
  stdin?: boolean;
  title?: string;
  hint?: string;
  hub?: string;
  agent?: string;
  assistedBy?: string;
}

export async function pushCommand(
  inlineContent: string | undefined,
  options: PushOptions,
): Promise<void> {
  let content: string;
  let title = options.title ?? "";
  let sourcePath = "";

  if (options.file) {
    try {
      // Check file size before reading
      const stat = statSync(options.file);
      if (stat.size > MAX_SIZE) {
        console.error(
          chalk.red(
            `File too large: ${formatSize(stat.size)} (limit: ${formatSize(MAX_SIZE)})`,
          ),
        );
        console.error(
          chalk.gray(
            "  Tip: split large files into smaller sections, or ask your admin to increase MAX_BODY_SIZE on the server.",
          ),
        );
        process.exit(1);
      }
      content = readFileSync(options.file, "utf-8");
      sourcePath = options.file;
      if (!title) {
        title = basename(options.file);
      }
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        console.error(chalk.red(`File not found: ${options.file}`));
      } else if ((err as { exitCode?: number }).exitCode) {
        throw err; // Re-throw our own process.exit calls
      } else {
        console.error(chalk.red(`Error reading file: ${options.file}`));
      }
      process.exit(1);
    }
  } else if (inlineContent) {
    // Positional argument: memax push "some content"
    content = inlineContent;
  } else if (options.stdin || !process.stdin.isTTY) {
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk);
    }
    content = Buffer.concat(chunks).toString("utf-8");
  } else {
    console.error(
      chalk.red(
        'Provide content, --file <path>, or pipe via stdin:\n  memax push "your content here"\n  memax push --file ./doc.md\n  echo "content" | memax push',
      ),
    );
    process.exit(1);
  }

  if (!content.trim()) {
    console.error(chalk.red("No content to push"));
    process.exit(1);
  }

  // Check size before sending
  const contentBytes = Buffer.byteLength(content, "utf-8");
  if (contentBytes > MAX_SIZE) {
    console.error(
      chalk.red(
        `File too large: ${formatSize(contentBytes)} (limit: ${formatSize(MAX_SIZE)})`,
      ),
    );
    console.error(
      chalk.gray(
        "  Tip: split large files into smaller sections, or ask your admin to increase MAX_BODY_SIZE on the server.",
      ),
    );
    process.exit(1);
  }

  const tags = options.tags ? options.tags.split(",").map((t) => t.trim()) : [];

  // Auto-detect URL content: single-line http(s) URL -> content_type "link"
  const trimmed = content.trim();
  const isURL =
    (trimmed.startsWith("http://") || trimmed.startsWith("https://")) &&
    !trimmed.includes("\n");

  let contentType = sourcePath.endsWith(".md") ? "markdown" : "text";
  if (isURL) {
    contentType = "link";
  }

  // Resolve hub: explicit --hub flag → .memax.yml → server default
  const requestedHub = options.hub ?? readMemaxYmlHub() ?? undefined;
  const hubId = requestedHub ? await resolveHubID(requestedHub) : undefined;
  if (requestedHub && !hubId) {
    console.error(
      chalk.red(
        "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
      ),
    );
    process.exit(1);
  }

  try {
    if (options.agent && options.assistedBy) {
      console.error(
        chalk.red(
          "Use either --agent for agent-authored pushes or --assisted-by for human-with-agent-help, not both.",
        ),
      );
      process.exit(1);
    }
    setClientAgent(options.agent);
    const memory = await getClient().push(content, {
      title,
      hint: options.hint ?? "",
      tags,
      source: "cli",
      sourceAgent: options.agent ?? "",
      assistedByAgent: options.assistedBy ?? "",
      initiationType: options.assistedBy ? "human_requested_agent" : undefined,
      sourcePath,
      contentType,
      projectContext: detectProjectContext(),
      hubId,
    });

    console.log(chalk.green("Saved"), chalk.bold(memory.title));
    console.log(
      chalk.gray(
        `  id: ${memory.id}  classification: ${memory.kind}/${memory.stability}  source: ${memory.source}`,
      ),
    );
  } catch (err) {
    console.error(chalk.red(`Push failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

export function registerPushCommand(program: Command): void {
  program
    .command("push [content]")
    .alias("remember")
    .description("Save knowledge to your Memax workspace")
    .option("-f, --file <path>", "File to push")
    .option("-t, --tags <tags>", "Comma-separated tags")
    .option("--title <title>", "Memory title")
    .option(
      "-H, --hint <hint>",
      "Context hint for AI processing (e.g. 'my resume', 'meeting notes')",
    )
    .option("--ttl <duration>", "Auto-archive after duration (e.g., 7d, 30d)")
    .option("--stdin", "Read content from stdin")
    .option("--hub <slug>", "Push to a specific hub explicitly")
    .option(
      "--agent <name>",
      "Source agent identity (e.g., claude-code, cursor)",
    )
    .option(
      "--assisted-by <slug>",
      "Credit a known agent collaborator for a human-authored push",
    )
    .action(pushCommand);
}
