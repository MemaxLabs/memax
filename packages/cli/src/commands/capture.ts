import { Command } from "commander";
import chalk from "chalk";
import { getClient, setClientAgent } from "../lib/client.js";

interface CaptureOptions {
  summary?: string;
  agent?: string;
}

/**
 * capture-session reads a session transcript from stdin and pushes it
 * to Memax for fact extraction. The extraction pipeline (Claude Haiku)
 * pulls out key decisions, learnings, and context — each becomes a
 * separate searchable memory.
 *
 * Usage:
 *   memax capture-session --agent claude-code  (reads stdin)
 *   memax capture-session --summary "Implemented auth system with JWT"
 */
export async function captureSessionCommand(
  options: CaptureOptions,
): Promise<void> {
  let content = "";

  // Read from stdin if available
  if (!process.stdin.isTTY) {
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk);
    }
    content = Buffer.concat(chunks).toString("utf-8").trim();
  }

  // If --summary provided, use that as the content (or append to stdin)
  if (options.summary) {
    if (content) {
      content = `## Session Summary\n${options.summary}\n\n## Session Transcript\n${content}`;
    } else {
      content = options.summary;
    }
  }

  if (!content) {
    console.error(
      chalk.red(
        "No session data. Pipe transcript via stdin or use --summary:\n" +
          "  memax capture-session --summary 'Implemented JWT auth'\n" +
          "  cat transcript.md | memax capture-session --agent claude-code",
      ),
    );
    process.exit(1);
  }

  // Truncate very long transcripts (keep first + last sections)
  if (content.length > 20000) {
    const head = content.slice(0, 10000);
    const tail = content.slice(-5000);
    content = head + "\n\n[...transcript truncated...]\n\n" + tail;
  }

  const agent = options.agent ?? "unknown";
  setClientAgent(options.agent);

  try {
    const memory = await getClient().push(content, {
      title: `Session capture (${agent}) — ${new Date().toLocaleDateString()}`,
      contentType: "transcript",
      source: "hook",
      sourceAgent: agent,
    });

    console.log(
      chalk.green("  Session captured."),
      chalk.gray(`Facts will be extracted in the background.`),
    );
    console.log(chalk.gray(`  id: ${memory.id}\n`));
  } catch (err) {
    console.error(chalk.red(`  Capture failed: ${(err as Error).message}\n`));
    process.exit(1);
  }
}

export function registerCaptureSessionCommand(program: Command): void {
  program
    .command("capture-session")
    .description("Capture an agent session — extract decisions and learnings")
    .option("--summary <text>", "Session summary text (alternative to stdin)")
    .option("--agent <name>", "Agent name (claude-code, gemini, etc.)")
    .action(captureSessionCommand);
}
