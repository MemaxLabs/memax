import { Command } from "commander";
import chalk from "chalk";
import type { AskOptions as SdkAskOptions } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import { resolveHubID } from "../lib/hubs.js";
import { resolveTopicReference } from "./topic.js";

interface AskOptions {
  model?: string;
  limit?: string;
  locale?: string;
  format?: string;
  hub?: string;
  topicId?: string;
  stream?: boolean;
  noRerank?: boolean;
}

// ---------------------------------------------------------------------------
// Streaming markdown renderer — state machine that styles inline markdown
// as tokens arrive. Normal text prints immediately. When it hits a marker
// like `**`, it holds output until the closing marker, then emits styled.
// ---------------------------------------------------------------------------

type InlineState =
  | "text"
  | "saw_star"
  | "in_italic"
  | "in_bold"
  | "saw_bold_end_star"
  | "in_code";

class StreamingMarkdownRenderer {
  private state: InlineState = "text";
  private buf = "";
  private atLineStart = true;
  private lineBuf = "";
  private inFence = false;
  private skipToNl = false;
  private lineType: "normal" | "heading" | "quote" = "normal";
  private indent = "  ";

  feed(text: string): void {
    for (const ch of text) this.char(ch);
  }

  end(): void {
    if (this.atLineStart && this.lineBuf) {
      process.stdout.write(this.indent + this.lineBuf);
    }
    this.flushInline();
  }

  // --- character dispatch ---

  private char(ch: string): void {
    if (this.skipToNl) {
      if (ch === "\n") {
        this.skipToNl = false;
        this.resetLine();
      }
      return;
    }
    if (this.inFence) {
      if (ch === "\n") {
        this.fenceLine();
      } else {
        this.lineBuf += ch;
      }
      return;
    }
    if (ch === "\n") {
      this.newline();
      return;
    }
    if (this.atLineStart) {
      this.lineStart(ch);
      return;
    }
    this.inline(ch);
  }

  // --- line start detection ---

  private lineStart(ch: string): void {
    this.lineBuf += ch;
    const b = this.lineBuf;

    // fence ```
    if (/^`{1,2}$/.test(b)) return;
    if (b.startsWith("```")) {
      this.inFence = true;
      this.skipToNl = true;
      this.lineBuf = "";
      this.atLineStart = false;
      return;
    }

    // heading #{1,6}<space>
    if (/^#{1,6}$/.test(b)) return;
    if (/^#{1,6} $/.test(b)) {
      this.emit(this.indent);
      this.lineType = "heading";
      this.enterInline();
      return;
    }

    // blockquote >
    if (b === ">") return;
    if (b === "> ") {
      this.emit(this.indent + chalk.gray("│ "));
      this.lineType = "quote";
      this.enterInline();
      return;
    }

    // unordered list - or +
    if (b === "-" || b === "+") return;
    if (b === "- " || b === "+ ") {
      this.emit(this.indent + chalk.gray("•") + " ");
      this.enterInline();
      return;
    }

    // * can be bullet or emphasis
    if (b === "*") return;
    if (b === "* ") {
      this.emit(this.indent + chalk.gray("•") + " ");
      this.enterInline();
      return;
    }
    if (b === "**") {
      this.emit(this.indent);
      this.enterInline();
      this.state = "in_bold";
      this.buf = "";
      return;
    }
    if (b.length === 2 && b[0] === "*" && b[1] !== "*" && b[1] !== " ") {
      this.emit(this.indent);
      this.enterInline();
      this.state = "in_italic";
      this.buf = b[1];
      return;
    }

    // ordered list: digits . space
    if (/^\d+$/.test(b)) return;
    if (/^\d+\.$/.test(b)) return;
    const ol = b.match(/^(\d+)\. $/);
    if (ol) {
      this.emit(this.indent + chalk.gray(ol[1] + ".") + " ");
      this.enterInline();
      return;
    }

    // not a block element — flush as normal text
    this.emit(this.indent);
    const saved = this.lineBuf;
    this.enterInline();
    for (const c of saved) this.inline(c);
  }

  // --- inline state machine ---

  private inline(ch: string): void {
    switch (this.state) {
      case "text":
        if (ch === "*") {
          this.state = "saw_star";
        } else if (ch === "`") {
          this.state = "in_code";
          this.buf = "";
        } else {
          this.emitChar(ch);
        }
        break;

      case "saw_star":
        if (ch === "*") {
          this.state = "in_bold";
          this.buf = "";
        } else {
          this.state = "in_italic";
          this.buf = ch;
        }
        break;

      case "in_italic":
        if (ch === "*") {
          this.emit(chalk.italic(this.buf));
          this.buf = "";
          this.state = "text";
        } else {
          this.buf += ch;
        }
        break;

      case "in_bold":
        if (ch === "*") {
          this.state = "saw_bold_end_star";
        } else {
          this.buf += ch;
        }
        break;

      case "saw_bold_end_star":
        if (ch === "*") {
          this.emit(chalk.bold(this.buf));
          this.buf = "";
          this.state = "text";
        } else {
          this.buf += "*" + ch;
          this.state = "in_bold";
        }
        break;

      case "in_code":
        if (ch === "`") {
          this.emit(chalk.cyan(this.buf));
          this.buf = "";
          this.state = "text";
        } else {
          this.buf += ch;
        }
        break;
    }
  }

  // --- helpers ---

  private newline(): void {
    if (this.atLineStart) {
      if (this.lineBuf) this.emit(this.indent + this.lineBuf);
      this.emit("\n");
      this.resetLine();
      return;
    }
    this.flushInline();
    this.emit("\n");
    this.resetLine();
  }

  private fenceLine(): void {
    if (this.lineBuf.trim() === "```") {
      this.inFence = false;
    } else {
      this.emit(this.indent + chalk.cyan(this.lineBuf) + "\n");
    }
    this.lineBuf = "";
  }

  private flushInline(): void {
    // Emit any buffered incomplete markers as raw text
    switch (this.state) {
      case "saw_star":
        this.emitChar("*");
        break;
      case "in_italic":
        this.emitChar("*");
        this.emit(this.buf);
        break;
      case "in_bold":
        this.emit("**" + this.buf);
        break;
      case "saw_bold_end_star":
        this.emit("**" + this.buf + "*");
        break;
      case "in_code":
        this.emitChar("`");
        this.emit(this.buf);
        break;
    }
    this.state = "text";
    this.buf = "";
  }

  private resetLine(): void {
    this.atLineStart = true;
    this.lineBuf = "";
    this.lineType = "normal";
    this.state = "text";
    this.buf = "";
  }

  private enterInline(): void {
    this.atLineStart = false;
    this.lineBuf = "";
  }

  private emitChar(ch: string): void {
    if (this.lineType === "heading") {
      this.emit(chalk.bold(ch));
    } else if (this.lineType === "quote") {
      this.emit(chalk.gray(ch));
    } else {
      this.emit(ch);
    }
  }

  private emit(s: string): void {
    process.stdout.write(s);
  }
}

// ---------------------------------------------------------------------------
// Command implementation
// ---------------------------------------------------------------------------

export async function askCommand(
  query: string | undefined,
  options: AskOptions,
): Promise<void> {
  if (!query) {
    if (!process.stdin.isTTY) {
      const chunks: Buffer[] = [];
      for await (const chunk of process.stdin) chunks.push(chunk);
      query = Buffer.concat(chunks).toString("utf-8").trim();
    }
    if (!query) {
      console.error(
        chalk.red('Provide a question: memax ask "how does auth work?"'),
      );
      process.exit(1);
    }
  }

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

  const askOptions: SdkAskOptions = {
    limit: parseInt(options.limit ?? "10", 10),
    model: (options.model ?? "auto") as SdkAskOptions["model"],
    locale: options.locale as SdkAskOptions["locale"],
    noRerank: options.noRerank,
    hubId,
    topicId,
  };

  if (options.format === "json") {
    try {
      const result = await getClient().ask(query, askOptions);
      console.log(JSON.stringify(result, null, 2));
    } catch (err) {
      console.error(chalk.red(`Ask failed: ${(err as Error).message}`));
      process.exit(1);
    }
    return;
  }

  if (options.stream === false) {
    try {
      await noStreamAsk(query, askOptions);
    } catch (err) {
      console.error(chalk.red(`Ask failed: ${(err as Error).message}`));
      process.exit(1);
    }
    return;
  }

  try {
    await streamAsk(query, askOptions);
  } catch (err) {
    console.error(chalk.red(`Ask failed: ${(err as Error).message}`));
    process.exit(1);
  }
}

async function noStreamAsk(
  query: string,
  askOptions: SdkAskOptions,
): Promise<void> {
  const isTTY = process.stdout.isTTY;
  const result = await getClient().ask(query, askOptions);

  if (!isTTY) {
    if (result.answer) console.log(result.answer);
    return;
  }

  // Sources
  if (result.sources.length > 0) {
    console.log(
      chalk.gray(
        `  ${result.sources.length} source${result.sources.length > 1 ? "s" : ""} found`,
      ),
    );
    const tagWidth = String(result.sources.length).length + 2;
    for (let i = 0; i < result.sources.length; i++) {
      const s = result.sources[i];
      const score = ((s.relevance_score ?? 0) * 100).toFixed(0);
      const tag = `[${i + 1}]`.padStart(tagWidth);
      const classification = [s.kind, s.stability].filter(Boolean).join("/");
      console.log(
        chalk.gray(`    ${tag} ${s.title} [${classification}] ${score}%`),
      );
    }
    console.log();
  }

  // Render answer with markdown
  if (result.answer) {
    const renderer = new StreamingMarkdownRenderer();
    renderer.feed(result.answer);
    renderer.end();
    process.stdout.write("\n");
  }

  // Metadata
  const meta: string[] = [];
  if (result.metadata.model) meta.push(result.metadata.model);
  if (result.metadata.answer_tokens)
    meta.push(`${result.metadata.answer_tokens} tokens`);
  if (result.metadata.total_latency_ms)
    meta.push(`${result.metadata.total_latency_ms}ms`);
  if (meta.length > 0) {
    console.log(chalk.gray(`  ${meta.join(" · ")}`));
  }
}

async function streamAsk(
  query: string,
  askOptions: SdkAskOptions,
): Promise<void> {
  const isTTY = process.stdout.isTTY;
  const renderer = isTTY ? new StreamingMarkdownRenderer() : null;

  return new Promise<void>((resolve, reject) => {
    let answerText = "";
    let timer: ReturnType<typeof setTimeout> | undefined;

    const done = () => {
      if (timer) clearTimeout(timer);
      resolve();
    };
    const fail = (err: Error) => {
      if (timer) clearTimeout(timer);
      reject(err);
    };

    timer = setTimeout(done, 120_000);

    const controller = getClient().memories.askStream(
      query,
      askOptions,
      (event: string, raw: unknown) => {
        const data = raw as Record<string, unknown>;

        switch (event) {
          case "sources": {
            const sources = data as unknown as Array<{
              title: string;
              kind?: string;
              stability?: string;
              relevance_score: number;
            }>;
            if (isTTY && sources.length > 0) {
              console.log(
                chalk.gray(
                  `  ${sources.length} source${sources.length > 1 ? "s" : ""} found`,
                ),
              );
              const tagWidth = String(sources.length).length + 2; // "[N]"
              for (let i = 0; i < sources.length; i++) {
                const s = sources[i];
                const score = (s.relevance_score * 100).toFixed(0);
                const tag = `[${i + 1}]`.padStart(tagWidth);
                const classification = [s.kind, s.stability]
                  .filter(Boolean)
                  .join("/");
                console.log(
                  chalk.gray(
                    `    ${tag} ${s.title} [${classification}] ${score}%`,
                  ),
                );
              }
              console.log();
            }
            break;
          }
          case "delta": {
            const text = (data as { text: string }).text;
            answerText += text;
            if (renderer) {
              renderer.feed(text);
            }
            break;
          }
          case "done": {
            if (renderer) {
              renderer.end();
              process.stdout.write("\n");
              const meta: string[] = [];
              if (data.model) meta.push(String(data.model));
              if (data.tokens) meta.push(`${data.tokens} tokens`);
              if (data.cached) meta.push("cached");
              if (meta.length > 0) {
                console.log(chalk.gray(`\n  ${meta.join(" · ")}`));
              }
            } else if (answerText) {
              console.log(answerText);
            }
            done();
            break;
          }
          case "error": {
            const msg =
              (data as { message?: string }).message ?? "Unknown error";
            if (isTTY) {
              console.error(chalk.red(`\n  ${msg}`));
            } else {
              console.error(msg);
            }
            controller.abort();
            fail(new Error(msg));
            break;
          }
        }
      },
    );
  });
}

export function registerAskCommand(program: Command): void {
  program
    .command("ask [question]")
    .description(
      "Ask a question — get an AI-synthesized answer from your knowledge",
    )
    .option("-m, --model <model>", "LLM model: auto, haiku, sonnet", "auto")
    .option("-l, --limit <n>", "Max source memories to use", "10")
    .option("--locale <locale>", "Response language: en, zh")
    .option("--format <format>", "Output format: text, json", "text")
    .option("--hub <slug>", "Ask within a specific hub only")
    .option("--topic-id <id>", "Restrict sources to memories in this topic")
    .option("--no-rerank", "Skip reranking for source retrieval")
    .option("--no-stream", "Wait for full answer instead of streaming")
    .action(askCommand);
}
